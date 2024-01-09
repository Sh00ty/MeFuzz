package tcp

import (
	"encoding/binary"
	"fmt"
	"net"
	"orchestration/entities"
	"orchestration/infra/utils/compression"
	"orchestration/infra/utils/logger"
	"orchestration/infra/utils/msgpack"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

type Connection struct {
	conn   net.Conn
	NodeID entities.NodeID
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

func Connect(addr string, port uint16) (Connection, error) {
	netConn, err := net.DialTimeout(Protocol, fmt.Sprintf("%s:%d", addr, port), TcpDialTimeout)
	if err != nil {
		return Connection{}, errors.WithStack(err)
	}

	return Connection{
		conn:   netConn,
		NodeID: nodeIDFromAddr(addr),
	}, nil
}

func (conn *Connection) RecvMessage() ([]byte, error) {
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

func (conn *Connection) SendMessage(msg []byte) error {

	if err := binary.Write(conn.conn, binary.BigEndian, uint32(len(msg))); err != nil {
		return err
	}
	n, err := conn.conn.Write(msg)

	logger.Debugf("sent %d bytes to %v", n, conn.NodeID)
	return err
}

func (conn *Connection) String() string {
	return fmt.Sprintf("{nodeID=%v}", conn.NodeID)
}

func (conn *Connection) close() {
	if err := conn.conn.Close(); err != nil {
		logger.ErrorMessage("failed to close connection: %s", conn)
	}
}

type MultiplexedConnection struct {
	conn      Connection
	converter msgpack.Converter
	recvChan  chan []byte
}

func (c *MultiplexedConnection) Recv(out interface{}) error {
	bytes, ok := <-c.recvChan
	if !ok {
		return errors.Errorf("connection closed: %v", c.conn)
	}
	return c.converter.Unmarshal(bytes, out)
}

func (c *MultiplexedConnection) RecvEnum(out *msgpack.Namer) error {
	bytes, ok := <-c.recvChan
	if !ok {
		return errors.Errorf("connection closed: %v", c.conn)
	}
	return msgpack.UnmarshalEnum(bytes, out)
}

func (c *MultiplexedConnection) Send(onNodeID entities.OnNodeID, flags entities.Flags, msg interface{}) error {
	bytes, err := c.converter.Marshal(msg)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal message: %v", msg)
	}
	return c.send(onNodeID, flags, bytes)
}

func (c *MultiplexedConnection) SendEnum(onNodeID entities.OnNodeID, flags entities.Flags, msg msgpack.Namer) error {
	bytes, err := c.converter.MarshalEnum(msg)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal message: %v", msg)
	}
	return c.send(onNodeID, flags, bytes)
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
	return c.conn.SendMessage(encoded)
}

func (c *MultiplexedConnection) Close() {
	c.conn.close()
	close(c.recvChan)
}
