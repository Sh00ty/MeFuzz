package tcp

import (
	"io"
	"net"
	"orchestration/core/master"
	"orchestration/entities"
	"orchestration/infra/utils/compression"
	"orchestration/infra/utils/logger"
	"orchestration/infra/utils/msgpack"

	"github.com/pkg/errors"
)

const (
	listenAddr = "127.0.0.1:9090"
)

type Srv struct {
	l      net.Listener
	master *master.Master
	closed chan struct{}
}

func NewSrv(m *master.Master) *Srv {
	l, err := net.Listen(Protocol, listenAddr)
	if err != nil {
		logger.Fatalf("failed to listen %s on : %s: %v", Protocol, listenAddr, err)
	}
	logger.Debugf("started listening on %s", listenAddr)

	srv := &Srv{
		l:      l,
		closed: make(chan struct{}),
		master: m,
	}

	go func() {
		for {
			netConn, err := l.Accept()
			if err != nil {
				select {
				case <-srv.closed:
					logger.Debugf("closed server on %s", listenAddr)
				default:
					logger.Errorf(err, "failed to accept new tcp connection")
				}
				return
			}
			nodeID := nodeIDFromAddr(netConn.RemoteAddr().String())
			conn := Connection{
				conn:   netConn,
				NodeID: nodeID,
			}
			coresBytes, err := conn.RecvMessage()
			if err != nil {
				logger.Errorf(err, "failed to receive cores message from new node %v", nodeID)
				conn.close()
				continue
			}
			cores := cores{}
			if err = msgpack.Unmarshal(coresBytes, &cores); err != nil {
				logger.Errorf(err, "failed to unmarshal cores count from node %d", nodeID)
				conn.close()
				continue
			}
			go srv.handleConnection(conn, cores.Cores)
		}
	}()

	return srv
}

func (s *Srv) handleConnection(conn Connection, cores int64) {
	recvByElement := s.setup(conn, cores)
	converter := msgpack.New()

	for {
		msgBytes, err := conn.RecvMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				logger.Infof("hadnler for connection %d stopped", conn.NodeID)
				return
			}
			logger.Errorf(err, "failed to receive message from connection %d", conn.NodeID)
			continue
		}
		msg := TcpMasterMessage{}
		err = converter.Unmarshal(msgBytes, &msg)
		if err != nil {
			logger.Errorf(
				err,
				"failed to unmarshal multiplexer master message from connection %d",
				conn.NodeID,
			)
			continue
		}

		decompressedMsg := msgpack.CovertTo[int, byte](msg.Payload)
		flags := entities.Flags(msg.Flags)
		if flags.Has(entities.Compressed) {
			decompressedMsg, err = compression.DeCompress(decompressedMsg)
			if err != nil {
				logger.Errorf(
					err,
					"failed to decompress multiplexer payload from connection %d and client %d",
					conn.NodeID, msg.ClientID,
				)
				continue
			}
		}

		select {
		case recvByElement[entities.OnNodeID(msg.ClientID)] <- decompressedMsg:
		default:
			logger.ErrorMessage(
				"node %d client %d missed message with len %d",
				conn.NodeID, msg.ClientID, len(decompressedMsg),
			)
		}
	}
}

func (s *Srv) setup(conn Connection, cores int64) map[entities.OnNodeID]chan []byte {
	setup := s.master.SetupNewNode(conn.NodeID, cores)
	recvByElement := make(map[entities.OnNodeID]chan []byte, len(setup.Elements))
	for _, set := range setup.Elements {
		switch set.Type {
		case entities.Evaler:
			ch := make(chan []byte)
			recvByElement[set.OnNodeID] = ch
			evaler := NewEvaler(
				MultiplexedConnection{
					conn:      conn,
					recvChan:  ch,
					converter: msgpack.New(),
				},
				set.OnNodeID,
				s.master.GetEventChan(),
			)
			s.master.StartEvaler(evaler)
		case entities.Broker:
			logger.Debugf("started fake broker")
		case entities.Fuzzer:
			logger.Debugf("started fake fuzzer")
		}
	}
	return recvByElement
}

func (s *Srv) Close() {
	close(s.closed)
	if err := s.l.Close(); err != nil {
		logger.Errorf(err, "failed to close master listener")
	}
}
