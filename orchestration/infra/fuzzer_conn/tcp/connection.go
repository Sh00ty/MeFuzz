package tcp

import (
	"encoding/binary"
	"fmt"
	"github.com/pkg/errors"
	"io"
	"log"
	"math"
	"net"
	"orchestration/entities"
	"orchestration/infra/utils/compression"
	"orchestration/infra/utils/hashing"
	"orchestration/infra/utils/logger"
	"orchestration/infra/utils/msgpack"
	"os"
	"sync"
	"time"
)

const (
	protocol            = "tcp"
	expectedNodesNumber = 100
	masterClientID      = math.MaxInt16
)

var (
	ErrConnIsClosed = errors.New("connection closed")
)

type connection struct {
	conn     net.Conn
	nodeID   entities.NodeID
	nodeType entities.NodeType
}

func (conn *connection) RecvMessage() ([]byte, error) {
	msgLenBytes := make([]byte, 4)
	_, err := conn.conn.Read(msgLenBytes)
	if err != nil {
		return nil, err
	}
	msgLen := binary.BigEndian.Uint32(msgLenBytes)
	logger.Debugf("received %d bytes", msgLen)
	msg := make([]byte, msgLen)
	_, err = conn.conn.Read(msg)
	return msg, err
}

func (conn *connection) SendMessage(msg []byte) error {
	if err := binary.Write(conn.conn, binary.BigEndian, uint32(len(msg))); err != nil {
		return err
	}
	n, err := conn.conn.Write(msg)
	logger.Debugf("sent %d bytes", n)
	return err
}

func (conn *connection) String() string {
	return fmt.Sprintf("{nodeID=%v, type=%v}", conn.nodeID, conn.nodeType)
}

func (conn *connection) Close() {
	if err := conn.conn.Close(); err != nil {
		log.Printf("failed to close connection: %v", conn)
	}
}

type listener struct {
	addr        string
	port        uint16
	tcpListener net.TCPListener
}

type tcpClient struct {
	listener             listener
	connMap              map[entities.NodeID]connection
	sendChanMap          map[entities.NodeID]chan<- entities.FuzzerMessage
	recvMsgChan          chan entities.FuzzerMessage
	sendLimit            uint32
	recvLimit            uint32
	ioTimeout            time.Duration
	tcpDialTimeout       time.Duration
	mu                   sync.RWMutex
	closedConnectionChan chan entities.NodeID
}

func NewTcpClient(
	sendLimit uint32,
	recvLimit uint32,
	ioTimeout time.Duration,
	tcpDialTimeout time.Duration,
) *tcpClient {
	return &tcpClient{
		listener:             listener{},
		recvLimit:            recvLimit,
		recvMsgChan:          make(chan entities.FuzzerMessage, expectedNodesNumber*recvLimit),
		ioTimeout:            ioTimeout,
		tcpDialTimeout:       tcpDialTimeout,
		sendLimit:            sendLimit,
		sendChanMap:          make(map[entities.NodeID]chan<- entities.FuzzerMessage, 0),
		connMap:              make(map[entities.NodeID]connection, 0),
		closedConnectionChan: make(chan entities.NodeID, expectedNodesNumber),
	}
}

func (srv *tcpClient) clearConnection(conn connection) {
	srv.mu.Lock()
	delete(srv.sendChanMap, conn.nodeID)
	delete(srv.connMap, conn.nodeID)
	srv.mu.Unlock()
	conn.Close()
	srv.closedConnectionChan <- conn.nodeID
}

func (*tcpClient) WaitForIncomingConnections() {}

func (srv *tcpClient) Connect(nodeType entities.NodeType, nodeID entities.NodeID, addr string, port uint16) error {
	conn, err := net.DialTimeout(protocol, fmt.Sprintf("%s:%d", addr, port), srv.tcpDialTimeout)
	if err != nil {
		return errors.WithStack(err)
	}
	switch nodeType {
	case entities.Broker:
		return srv.brokerHello(connection{
			conn:     conn,
			nodeID:   nodeID,
			nodeType: nodeType,
		})
	case entities.Evaler:
		if closeErr := conn.Close(); closeErr != nil {
			return errors.Wrap(closeErr, "failed to close unimplemented evaler connection")
		}
		return errors.New("evaler type is unimplemented")
	}
	if closeErr := conn.Close(); closeErr != nil {
		return errors.Wrap(closeErr, "failed to close undefined connection")
	}
	return errors.New("undefined type of node to connect")
}

func (srv *tcpClient) brokerHello(brokerConn connection) error {
	// пока соединени не установится или не провалится мы блокируем доступ к этому блоку
	srv.mu.Lock()
	defer srv.mu.Unlock()

	if _, exists := srv.connMap[brokerConn.nodeID]; exists {
		return errors.Errorf("connection %v already exists", brokerConn)
	}

	// получаем приветсвие от брокера (он всем его отправляет)
	brokerGreetBytes, err := brokerConn.RecvMessage()
	if err != nil {
		brokerConn.Close()
		return errors.WithMessage(err, "can't read broker hello")
	}
	brokerGreetMsg := brokerConnectHello{}
	if err = msgpack.UnmarshalEnum(brokerGreetBytes, &brokerGreetMsg); err != nil {
		brokerConn.Close()
		return errors.WithMessage(err, "can't unmarshal broker hello")
	}
	logger.Infof("M2B greeting from %s", brokerGreetMsg.Hostname)

	masterHost, err := os.Hostname()
	if err != nil {
		log.Printf("error while getting master hostname, pasted 'externalhost' insted")
		masterHost = "external_host"
	}

	// отправляем брокеру конфигурацию
	masterHello := masterNodeHello{
		MasterHostname: masterHost,
		// для брокера наоборот будет
		// мы отправляем по sendlimit а он принимает по recv limit
		RecvLimit: srv.sendLimit,
		SendLimit: srv.recvLimit,
		BrokerID:  uint32(brokerConn.nodeID),
	}
	// создаем конвертер из наших структур в msgpack
	converter := msgpack.New()
	mhBytes, err := converter.MarshalEnum(masterHello)
	if err != nil {
		brokerConn.Close()
		return errors.WithMessage(err, "failed to marshal master node hello message")
	}
	if err = brokerConn.SendMessage(mhBytes); err != nil {
		brokerConn.Close()
		return errors.WithMessage(err, "failed to send master node hello message")
	}

	// смотрим принял ли брокер наши настройки (он в ответ отправляем id который мы ему дали)
	masterAcceptedBytes, err := brokerConn.RecvMessage()
	if err != nil {
		brokerConn.Close()
		return errors.WithMessage(err, "failed to read master accepted message")
	}
	ma := masterAccepted{}
	if err = msgpack.UnmarshalEnum(masterAcceptedBytes, &ma); err != nil {
		brokerConn.Close()
		return errors.WithMessage(err, "failed to unmarshal master accepted message")
	}
	if entities.NodeID(ma.BrokerID) != brokerConn.nodeID {
		return errors.Errorf("got trash message: [ma.BrokerID != srv.conns] (%d!=%d)", ma.BrokerID, brokerConn.nodeID)
	}
	srv.recvFuzzerConfigurations(brokerConn.nodeID, ma.FuzzerConfigurations)
	// все готово к работе, добавляем это соединение и идем дальше
	sendChan := make(chan entities.FuzzerMessage, srv.sendLimit)
	srv.sendChanMap[brokerConn.nodeID] = sendChan
	srv.connMap[brokerConn.nodeID] = brokerConn
	go srv.handleBrokerConnection(brokerConn, sendChan)
	return nil
}

func (srv *tcpClient) handleBrokerConnection(conn connection, sendChan <-chan entities.FuzzerMessage) {
	converter := msgpack.New()
	for {
		// получаем сообщения от брокера
		for i := uint32(0); i < srv.recvLimit; i++ {
			msgBytes, err := conn.RecvMessage()
			if err != nil {
				// если закрыто соединение, значит выходим из данного цикла
				if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
					logger.Infof("connection %v seems to be closed", conn)
					srv.clearConnection(conn)
					return
				}
				logger.Errorf(err, "tcpMasterMessage read error")
				continue
			}
			msg := tcpMasterMessage{}
			if err := msgpack.Unmarshal(msgBytes, &msg); err != nil {
				logger.Errorf(err, "failed to unmarshal tcp message: conn=%v", conn)
				continue
			}
			flag := entities.FuzzInfoKind(msg.Flags)
			if !flag.Has(entities.Master) {
				logger.Infof("received message without M2B flag; msg=%v", msg)
				continue
			}
			payload := msgpack.CovertTo[int, byte](msg.Payload)

			if flag.Has(entities.Compressed) {
				payload, err = compression.DeCompress(payload)
				if err != nil {
					logger.Errorf(err, "failed to decomptess msg=%v", msg)
					continue
				}
			}
			fuzzerMsg := entities.FuzzerMessage{
				From: entities.FuzzerID{
					NodeID:   conn.nodeID,
					ClientID: entities.ClientID(msg.ClientID),
				}}
			switch {
			case flag.Has(entities.NewTestCase):
				tcTmp := newTestcase{}
				if err := msgpack.UnmarshalEnum(payload, &tcTmp); err != nil {
					logger.Errorf(err, "failed to umnmarshal newTestCase; msg=%v", msg)
					continue
				}
				inputData := msgpack.CovertTo[int, byte](tcTmp.Input.Input)
				fuzzerMsg.Info = entities.Testcase{
					InputData:  inputData,
					InputHash:  hashing.MakeHash(inputData),
					Execs:      tcTmp.Executions,
					CorpusSize: tcTmp.CorpusSize,
					CreatedAt:  time.Unix(int64(tcTmp.Timestamp.Secs), int64(tcTmp.Timestamp.Nsec)).In(time.Local),
				}
			}
			srv.recvMsgChan <- fuzzerMsg
		}

		for i := uint32(0); i < srv.sendLimit; i++ {
			// тк не факт что есть что кинуть =>
			// сделаем через select чтобы не повиснуть
			select {
			case msg, opened := <-sendChan:
				if !opened {
					srv.clearConnection(conn)
					logger.Infof("connection %v closed by master", conn)
					return
				}
				msgBytes, err := converter.Marshal(msg)
				if err != nil {
					logger.Errorf(err, "failed to marshal message: msg=%v", msg)
					continue
				}
				compressed, err := compression.Compress(msgBytes)
				if err != nil {
					logger.Errorf(err, "failed to compress msg=%v", msg)
					continue
				}
				flags := uint32(entities.Master)
				if len(compressed) != len(msgBytes) {
					flags = flags | uint32(entities.Compressed)
				}
				flags = flags | uint32(msg.Info.Kind())

				tcpMasterMsgBytes, err := converter.Marshal(
					tcpMasterMessage{
						Payload:  msgpack.CovertTo[byte, int](compressed),
						ClientID: masterClientID,
						Flags:    flags,
					})
				if err != nil {
					logger.Errorf(err, "failed to marshal tcp master message: msg=%v", msg)
					continue
				}
				if err := conn.SendMessage(tcpMasterMsgBytes); err != nil {
					// если закрыто соединение, значит выходим из данного цикла
					if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
						logger.Infof("connection %v closed by broker", conn)
						srv.clearConnection(conn)
						return
					}
					logger.Errorf(err, "failed to send tcp message error: conn=%v", conn)
					continue
				}
			default:
			}
		}
		time.Sleep(srv.ioTimeout)
	}
}

func (srv *tcpClient) Send(nodeID entities.NodeID, msg entities.FuzzerMessage) error {
	srv.mu.RLock()
	defer srv.mu.RUnlock()

	if sendChan, exists := srv.sendChanMap[nodeID]; exists {
		select {
		case sendChan <- msg:
		default:
			return errors.Errorf("failed to send message: chan is fulled; msg=%v", msg)
		}
	}
	return errors.Wrapf(ErrConnIsClosed, "conn with id=%v doesn't exists", nodeID)
}

func (srv *tcpClient) GetRecvMessageChan() <-chan entities.FuzzerMessage {
	return srv.recvMsgChan
}

func (srv *tcpClient) HandleEvalerConnection() error {
	return nil
}

func (srv *tcpClient) recvFuzzerConfigurations(nodeID entities.NodeID, configurations []FuzzerConfiguration) {
	for i, conf := range configurations {
		srv.recvMsgChan <- entities.FuzzerMessage{
			Info: entities.FuzzerConf{
				MutatorID:  entities.MutatorID(conf.MutatorID),
				ScheduleID: entities.ScheduleID(conf.SchedulerID),
			},
			From: entities.FuzzerID{
				ClientID: entities.ClientID(i + 1),
				NodeID:   nodeID,
			},
		}
	}
}
