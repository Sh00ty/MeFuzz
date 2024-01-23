package tcp

import (
	"encoding/binary"
	"fmt"
	"net"
	"orchestration/entities"
	"orchestration/infra/utils/compression"
	"orchestration/infra/utils/logger"
	"orchestration/infra/utils/msgpack"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/pkg/errors"
)

var (
	ErrConnectionClosed = errors.New("connection closed")
)

type connection struct {
	conn        net.Conn
	estableshed *atomic.Bool
	NodeID      entities.NodeID
}

func nodeIDFromAddr(addr string) entities.NodeID {
	addr, _, _ = strings.Cut(addr, ":")
	var (
		nodeID  uint32
		ipParts = strings.Split(addr, ".")
	)

	for i, partStr := range ipParts {
		part, err := strconv.ParseUint(partStr, 10, 8)
		if err != nil {
			logger.Fatalf(err.Error())
		}
		nodeID |= uint32(part) << (8 * (3 - uint32(i)))
	}
	return entities.NodeID(nodeID)
}

func Connect(addr string, port uint16) (connection, error) {
	netConn, err := net.DialTimeout(Protocol, fmt.Sprintf("%s:%d", addr, port), TcpDialTimeout)
	if err != nil {
		return connection{}, errors.WithStack(err)
	}

	return connection{
		conn:        netConn,
		NodeID:      nodeIDFromAddr(addr),
		estableshed: new(atomic.Bool),
	}, nil
}

func (conn *connection) RecvMessage() ([]byte, error) {
	msgLenBytes := make([]byte, 4)
	_, err := conn.conn.Read(msgLenBytes)
	if err != nil {
		return nil, err
	}
	msgLen := binary.BigEndian.Uint32(msgLenBytes)
	msgBytes := make([]byte, msgLen)
	_, err = conn.conn.Read(msgBytes)

	logger.Debugf("received %d bytes from %v", msgLen, conn.NodeID)
	return msgBytes, err
}

func (conn *connection) SendMessage(msg []byte) error {

	if err := binary.Write(conn.conn, binary.BigEndian, uint32(len(msg))); err != nil {
		return err
	}
	n, err := conn.conn.Write(msg)

	logger.Debugf("sent %d bytes to %v", n, conn.NodeID)
	return err
}

func (conn *connection) String() string {
	return fmt.Sprintf("{nodeID=%v}", conn.NodeID)
}

func (conn *connection) close() {
	if err := conn.conn.Close(); err != nil {
		logger.ErrorMessage("failed to close connection: %s", conn)
	}
}

type MultiplexedConnection struct {
	conn      connection
	converter msgpack.Converter
	recvChan  chan []byte
}

func (c *MultiplexedConnection) Recv(out interface{}) error {
	bytes, ok := <-c.recvChan
	if !ok {
		return errors.Wrapf(ErrConnectionClosed, "connection closed: %v", c.conn)
	}
	return msgpack.Unmarshal(bytes, out)
}

func (c *MultiplexedConnection) RecvBytes() ([]byte, error) {
	bytes, ok := <-c.recvChan
	if !ok {
		return nil, errors.Wrapf(ErrConnectionClosed, "connection closed: %v", c.conn)
	}
	return bytes, nil
}

func (c *MultiplexedConnection) Send(onNodeID entities.OnNodeID, flags entities.Flags, msg interface{}) error {
	bytes, err := c.converter.Marshal(msg)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal message: %v", msg)
	}
	err = c.send(onNodeID, flags, bytes)
	if errors.Is(err, net.ErrClosed) {
		return ErrConnectionClosed
	}
	return err
}

func (c *MultiplexedConnection) SendEnum(onNodeID entities.OnNodeID, flags entities.Flags, msg msgpack.Namer) error {
	bytes, err := c.converter.MarshalEnum(msg)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal message: %v", msg)
	}
	err = c.send(onNodeID, flags, bytes)
	if errors.Is(err, net.ErrClosed) {
		return ErrConnectionClosed
	}
	return err
}

func (c *MultiplexedConnection) send(onNodeID entities.OnNodeID, flags entities.Flags, msg []byte) error {
	m := TcpMasterMessage{
		ClientID: uint32(onNodeID),
	}
	flags = flags.Add(entities.Master)

	msg, compressed, err := compression.Compress(msg)
	if err != nil {
		return errors.Wrap(err, "failed to comress multiplex message")
	}
	if compressed {
		flags = flags.Add(entities.Compressed)
	}

	m.Flags = uint32(flags)
	m.Payload = msgpack.CovertTo[byte, int](msg)

	encoded, err := c.converter.Marshal(m)
	if err != nil {
		return errors.Wrap(err, "failed to marshal multiplex message")
	}

	if !c.conn.estableshed.Load() {
		c.spin()
	}
	return c.conn.SendMessage(encoded)
}

func (c *MultiplexedConnection) spin() {
	const backoff = 128

	i := 0
	for !c.conn.estableshed.Load() {
		i++
		if i == backoff {
			i = 0
			runtime.Gosched()
		}
	}
	fmt.Println("after spin")
}

func (c *MultiplexedConnection) Close() {
	c.conn.close()
	close(c.recvChan)
}

func (c *MultiplexedConnection) Strign() string {
	return c.conn.String()
}
