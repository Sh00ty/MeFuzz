package tcp

import (
	"context"
	"io"
	"math"
	"net"
	"orchestration/core/master"
	"orchestration/entities"
	"orchestration/infra/utils/compression"
	"orchestration/infra/utils/logger"
	"orchestration/infra/utils/msgpack"
	"sync"
	"time"

	"github.com/pkg/errors"
)

const (
	// нереалистичный id для мастера, чтобы он не пересекался с клиентами
	masterClientID      = math.MaxInt16
	Protocol            = "tcp"
	ExpectedNodesNumber = 100
	// размер буфера для отправки сообщений
	// если достич его, то мастер начинает стоять в блокировке
	// тк не успевает ничего обработать
	SendLimit      uint32        = 10
	RecvLimit      uint32        = 10
	IoTimeout      time.Duration = 100 * time.Millisecond
	TcpDialTimeout time.Duration = time.Second
)

type (
	HandlerMessage struct {
		Payload  []byte
		Flag     entities.Flags
		NodeID   entities.NodeID
		ClientID entities.OnNodeID
	}

	OnConnect func(ctx context.Context, conn Connection, recvMsgChan chan entities.Testcase) error
	Handler   func(ctx context.Context, recvMsgChan chan entities.Testcase, msg HandlerMessage) error
)

type Client struct {
	h         Handler
	onConnect OnConnect

	mu sync.RWMutex
	// храним для каждой ноды соединение с ней
	connMap     map[entities.NodeID]Connection
	sendChanMap map[entities.NodeID]chan<- entities.Testcase

	// в этот канал присылаются все сообщений со всех нод
	recvMsgChan chan entities.Testcase
	// контролируем обрывы соединений и удаление лишних нод
	performanceEventChan chan master.Event
}

func NewTcpClient(
	onConnect OnConnect,
	handler Handler,
	performanceEventChan chan master.Event,
) *Client {
	return &Client{
		h:         handler,
		onConnect: onConnect,

		connMap:     make(map[entities.NodeID]Connection, ExpectedNodesNumber),
		sendChanMap: make(map[entities.NodeID]chan<- entities.Testcase, ExpectedNodesNumber),

		recvMsgChan:          make(chan entities.Testcase, ExpectedNodesNumber*RecvLimit),
		performanceEventChan: performanceEventChan,
	}
}

func (c *Client) clearConnection(conn Connection) {
	c.mu.Lock()
	delete(c.connMap, conn.NodeID)
	delete(c.sendChanMap, conn.NodeID)
	c.mu.Unlock()

	c.performanceEventChan <- master.Event{
		Event:   master.NodeDeleted,
		Element: entities.ElementID{NodeID: conn.NodeID},
	}
}

func (c *Client) Connect(ctx context.Context, elementType entities.ElementType, addr string, port uint16) error {

	conn, err := Connect(addr, port)
	if err != nil {
		return err
	}
	// пока соединени не установится или не провалится мы блокируем доступ к этому блоку
	c.mu.Lock()

	if _, exists := c.connMap[conn.NodeID]; exists {
		c.mu.Unlock()

		logger.ErrorMessage("connection %v already exists", conn)
		return nil
	}
	if err = c.onConnect(ctx, conn, c.recvMsgChan); err != nil {
		c.mu.Unlock()
		return errors.New("undefined type of node to connect")
	}

	// все готово к работе, добавляем это соединение и идем дальше
	sendChan := make(chan entities.Testcase, SendLimit)
	c.connMap[conn.NodeID] = conn
	c.performanceEventChan <- master.Event{
		Event:    master.NewNode,
		NodeType: elementType,
		Element: entities.ElementID{
			NodeID: conn.NodeID,
		},
	}

	c.mu.Unlock()

	c.Handle(ctx, conn, sendChan)
	return nil
}

func (c *Client) Send(ctx context.Context, nodeID entities.NodeID, msg entities.Testcase) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if sendChan, exists := c.sendChanMap[nodeID]; exists {
		select {
		case sendChan <- msg:
		case <-ctx.Done():
			return errors.Errorf("failed to send message: chan is fulled; msg=%v", msg)
		}
	}
	return errors.Errorf("conn with id=%v doesn't exists", nodeID)
}

func (c *Client) GetRecvMessageChan() <-chan entities.Testcase {
	return c.recvMsgChan
}

func (c *Client) Handle(ctx context.Context, conn Connection, input <-chan entities.Testcase) {
	defer c.clearConnection(conn)

	converter := msgpack.New()
	for {
		// получаем сообщения
		for i := uint32(0); i < RecvLimit; i++ {
			msgBytes, err := conn.RecvMessage()
			if err != nil {
				// если закрыто соединение, значит выходим из данного цикла
				if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
					logger.Infof("connection %v seems to be closed", conn)
					return
				}
				logger.Errorf(err, "failed to read message from connection %v", conn)
				continue
			}
			msg := TcpMasterMessage{}
			if err := msgpack.Unmarshal(msgBytes, &msg); err != nil {
				logger.Errorf(err, "failed to unmarshal tcp message: conn=%v", conn)
				continue
			}
			flags := entities.Flags(msg.Flags)
			if !flags.Has(entities.Master) {
				logger.Infof("received message without M2B flag; msg=%v", msg)
				continue
			}

			payload := msgpack.CovertTo[int, byte](msg.Payload)

			if flags.Has(entities.Compressed) {
				payload, err = compression.DeCompress(payload)
				if err != nil {
					logger.Errorf(err, "failed to decomptess msg=%v", msg)
					continue
				}
			}

			err = c.h(ctx, c.recvMsgChan, HandlerMessage{
				Payload:  payload,
				Flag:     flags,
				NodeID:   conn.NodeID,
				ClientID: entities.OnNodeID(msg.ClientID),
			})
			if err != nil {
				logger.Errorf(err, "failed to handle message from conn %v", conn)
			}
		}

		for i := uint32(0); i < SendLimit; i++ {
			select {
			case msg, valid := <-input:
				if !valid {
					logger.Infof("connection %v closed by master", conn)
					return
				}
				msgBytes, err := converter.Marshal(msg)
				if err != nil {
					logger.Errorf(err, "failed to marshal message: msg=%v", msg)
					continue
				}
				compressed, _, err := compression.Compress(msgBytes)
				if err != nil {
					logger.Errorf(err, "failed to compress msg=%v", msg)
					continue
				}
				flags := entities.Master
				if len(compressed) != len(msgBytes) {
					flags = flags.Add(entities.Compressed)
				}

				tcpMasterMsgBytes, err := converter.Marshal(
					TcpMasterMessage{
						Payload:  msgpack.CovertTo[byte, int](compressed),
						ClientID: masterClientID,
						Flags:    uint32(flags),
					})
				if err != nil {
					logger.Errorf(err, "failed to marshal tcp master message: msg=%v", msg)
					continue
				}
				if err := conn.SendMessage(tcpMasterMsgBytes); err != nil {
					if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
						logger.Infof("connection %v closed by broker", conn)
						return
					}
					logger.Errorf(err, "failed to send tcp message error: conn=%v", conn)
					continue
				}
			case <-ctx.Done():
				return
			default:
			}
		}
		time.Sleep(IoTimeout)
	}
}
