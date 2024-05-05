package tcp

import (
	"io"
	"net"
	"orchestration/core/master"
	"orchestration/entities"
	"orchestration/infra/utils/compression"
	"orchestration/infra/utils/logger"
	"orchestration/infra/utils/msgpack"
	"sync/atomic"

	"github.com/pkg/errors"
)

const (
	listenAddr = ":9990"
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
					logger.Infof("closed server on %s", listenAddr)
				default:
					logger.Errorf(err, "failed to accept new tcp connection")
				}
				return
			}
			nodeID := nodeIDFromAddr(netConn.RemoteAddr().String())
			conn := connection{
				conn:        netConn,
				NodeID:      nodeID,
				estableshed: new(atomic.Bool),
			}
			coresBytes, err := conn.RecvMessage()
			if err != nil {
				logger.Errorf(err, "failed to receive cores message from new node %v", nodeID)
				conn.close()
				continue
			}
			initMsg := initMsg{}
			if err = msgpack.Unmarshal(coresBytes, &initMsg); err != nil {
				logger.Errorf(err, "failed to unmarshal cores count from node %d", nodeID)
				conn.close()
				continue
			}
			go srv.handleConnection(conn, initMsg)
		}
	}()

	return srv
}

func (s *Srv) handleConnection(conn connection, initMsg initMsg) {
	recvByElement, err := s.setup(conn, initMsg.Cores, initMsg.ManualRole)
	if err != nil {
		conn.close()
		if errors.Is(err, master.ErrMaxElements) {
			logger.Infof(err.Error())
			return
		}
		logger.Fatalf("failed to setup connection, abotring: %v", err)
	}
	var (
		ctx       = s.master.Ctx()
		converter = msgpack.New()
	)
	for {
		if ctx.Err() != nil {
			logger.Infof("handler for connection %v stoped: %v", conn, ctx.Err())
			return
		}
		msgBytes, err := conn.RecvMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				for el, ch := range recvByElement {
					logger.Infof("connection {%d:%d} closed", conn.NodeID, el)
					close(ch)
				}
				conn.close()
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
		flags := flags(msg.Flags)
		if flags.Has(Compressed) {
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
		case <-ctx.Done():
			logger.ErrorMessage(
				"master ctx stoped: node %d client %d missed message with len %d",
				conn.NodeID, msg.ClientID, len(decompressedMsg),
			)
		}
	}
}

func (s *Srv) setup(conn connection, cores int64, manualRole string) (map[entities.OnNodeID]chan []byte, error) {
	setup, err := s.master.SetupNewNode(conn.NodeID, cores, manualRole)
	if err != nil {
		return nil, err
	}
	var (
		recvByElement     = make(map[entities.OnNodeID]chan []byte, len(setup.Elements))
		nodeConfiguration = nodeConfiguration{
			Elements: make([]element, 0, len(setup.Elements)),
		}
	)

	for _, set := range setup.Elements {
		ch := make(chan []byte)
		recvByElement[set.OnNodeID] = ch

		switch set.Type {
		case entities.Evaler:
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
			nodeConfiguration.Elements = append(nodeConfiguration.Elements, element{
				OnNodeID: uint32(set.OnNodeID),
				Kind:     int64(entities.Evaler),
			})
		case entities.Fuzzer:
			fuzzer := NewFuzzer(
				MultiplexedConnection{
					conn:      conn,
					recvChan:  ch,
					converter: msgpack.New(),
				},
				set.OnNodeID,
				s.master.GetEventChan(),
				s.master.GetTestcaseChan(),
			)

			s.master.StartFuzzer(fuzzer)
			nodeConfiguration.Elements = append(nodeConfiguration.Elements, element{
				OnNodeID: uint32(set.OnNodeID),
				Kind:     int64(entities.Fuzzer),
				FuzzerConfiguration: &fuzzerConfiguration{
					MutatorID:   "mutator1",
					SchedulerID: "scheduler1",
				},
			})
		}
	}

	nc, err := msgpack.New().Marshal(nodeConfiguration)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal node configuration")
	}
	if err = conn.SendMessage(nc); err != nil {
		return nil, errors.Wrap(err, "failed to send node configuration message")
	}
	conn.estableshed.Store(true)
	return recvByElement, nil
}

func (s *Srv) Close() {
	close(s.closed)
	if err := s.l.Close(); err != nil {
		logger.Errorf(err, "failed to close master listener")
	}
}
