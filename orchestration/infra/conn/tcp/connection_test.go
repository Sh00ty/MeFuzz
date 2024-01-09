package tcp

// import (
// 	"fmt"
// 	"github.com/stretchr/testify/assert"
// 	"io"
// 	"net"
// 	"orchestration/entities"
// 	"orchestration/infra/utils/hashing"
// 	"orchestration/infra/utils/msgpack"
// 	"testing"
// 	"time"
// )

// func TestConnectionCreation(t *testing.T) {
// 	nodeID := uint32(12)
// 	recvLimit := uint32(3)
// 	sendLimit := uint32(4)
// 	confs := []FuzzerConfiguration{
// 		{
// 			MutatorID:   "mut1",
// 			SchedulerID: "sch1",
// 		},
// 		{
// 			MutatorID:   "mut2",
// 			SchedulerID: "sch2",
// 		},
// 	}
// 	clnt := NewTcpClient(sendLimit, recvLimit, 100*time.Millisecond, time.Second)

// 	t.Run("created established connection", func(t *testing.T) {
// 		go func() {
// 			l, conn := accept(t, "1337")
// 			conn = greet(t, conn, "mephi.ru")
// 			conn = recvMasterHello(t, conn, sendLimit, recvLimit, nodeID)
// 			conn = sendAccept(t, conn, nodeID, confs)
// 			conn.Close()
// 			_ = l.Close()
// 		}()
// 		time.Sleep(time.Second)
// 		err := clnt.Connect(entities.Broker, entities.NodeID(nodeID), "127.0.0.1", 1337)
// 		assert.NoError(t, err)
// 		event := <-clnt.GetRecvMessageChan()
// 		assert.Equal(t, event, entities.FuzzerMessage{
// 			From: entities.FuzzerID{
// 				ClientID: 1,
// 				NodeID:   12,
// 			},
// 			Info: entities.FuzzerConf{
// 				MutatorID:  entities.MutatorID(confs[0].MutatorID),
// 				ScheduleID: entities.ScheduleID(confs[0].SchedulerID),
// 			},
// 		})
// 		event = <-clnt.GetRecvMessageChan()
// 		assert.Equal(t, event, entities.FuzzerMessage{
// 			From: entities.FuzzerID{
// 				ClientID: 2,
// 				NodeID:   12,
// 			},
// 			Info: entities.FuzzerConf{
// 				MutatorID:  entities.MutatorID(confs[1].MutatorID),
// 				ScheduleID: entities.ScheduleID(confs[1].SchedulerID),
// 			},
// 		})
// 		cc := <-clnt.closedConnectionChan
// 		assert.Equal(t, entities.NodeID(12), cc)
// 	})
// 	nodeID++
// 	t.Run("wrong greet message encoding", func(t *testing.T) {
// 		go func(tt *testing.T) {
// 			listener, conn := accept(t, "1337")
// 			connHello := brokerConnectHello{
// 				BrokerShmemDescription: brokerShMemDescription{
// 					Size: 100,
// 					ID:   shmemID{},
// 				},
// 			}
// 			c := msgpack.New()
// 			helloBytes, err := c.MarshalEnum(connHello)
// 			if err != nil {
// 				tt.Fatalf("can't marshal connhello: %v", err)
// 			}
// 			helloBytes = append([]byte{1, 2, 3, 4, 5, 222, 234}, helloBytes...)
// 			if err := conn.SendMessage(helloBytes); err != nil {
// 				tt.Fatal(err)
// 			}
// 			_, err = conn.RecvMessage()
// 			assert.Error(tt, err)
// 			assert.ErrorIs(tt, err, io.EOF)
// 			_ = listener.Close()
// 		}(t)
// 		time.Sleep(time.Second)
// 		err := clnt.Connect(entities.Broker, entities.NodeID(nodeID), "127.0.0.1", 1337)
// 		assert.Error(t, err)
// 	})
// 	nodeID++
// 	t.Run("wrong accept message encoding", func(t *testing.T) {
// 		go func() {
// 			l, conn := accept(t, "1337")
// 			conn = greet(t, conn, "mephi.ru")
// 			conn = recvMasterHello(t, conn, sendLimit, recvLimit, nodeID)
// 			ma := masterAccepted{
// 				BrokerID:             132034,
// 				FuzzerConfigurations: confs,
// 			}
// 			maData, err := msgpack.New().MarshalEnum(ma)
// 			if err != nil {
// 				t.Fatal(err)
// 			}
// 			if err := conn.SendMessage(maData); err != nil {
// 				t.Fatal(err)
// 			}
// 			_ = l.Close()
// 		}()
// 		time.Sleep(time.Second)
// 		err := clnt.Connect(entities.Broker, entities.NodeID(nodeID), "127.0.0.1", 1337)
// 		assert.Error(t, err)
// 	})
// }

// func accept(t *testing.T, port string) (net.Listener, connection) {
// 	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%s", port))
// 	if err != nil {
// 		t.Fatalf("failed to init tcp server: %v", err)
// 	}
// 	newConn, err := listener.Accept()
// 	if err != nil {
// 		t.Fatalf("can't accept connection: %v", err)
// 	}
// 	return listener, connection{conn: newConn}
// }

// func greet(t *testing.T, conn connection, hostname string) connection {
// 	connHello := brokerConnectHello{
// 		Hostname: hostname,
// 		BrokerShmemDescription: brokerShMemDescription{
// 			Size: 100,
// 			ID:   shmemID{},
// 		},
// 	}
// 	helloBytes, err := msgpack.New().MarshalEnum(connHello)
// 	if err != nil {
// 		t.Fatalf("can't marshal connhello: %v", err)
// 	}
// 	if err := conn.SendMessage(helloBytes); err != nil {
// 		t.Fatal(err)
// 	}
// 	return conn
// }

// func recvMasterHello(t *testing.T, conn connection, recvLimit, sendLimit, brokerID uint32) connection {
// 	rd, err := conn.RecvMessage()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	masterHello := masterNodeHello{}
// 	if err := msgpack.UnmarshalEnum(rd, &masterHello); err != nil {
// 		t.Fatal(err)package tcp

// import (
// 	"fmt"
// 	"github.com/stretchr/testify/assert"
// 	"io"
// 	"net"
// 	"orchestration/entities"
// 	"orchestration/infra/utils/hashing"
// 	"orchestration/infra/utils/msgpack"
// 	"testing"
// 	"time"
// )

// func TestConnectionCreation(t *testing.T) {
// 	nodeID := uint32(12)
// 	recvLimit := uint32(3)
// 	sendLimit := uint32(4)
// 	confs := []FuzzerConfiguration{
// 		{
// 			MutatorID:   "mut1",
// 			SchedulerID: "sch1",
// 		},
// 		{
// 			MutatorID:   "mut2",
// 			SchedulerID: "sch2",
// 		},
// 	}
// 	clnt := NewTcpClient(sendLimit, recvLimit, 100*time.Millisecond, time.Second)

// 	t.Run("created established connection", func(t *testing.T) {
// 		go func() {
// 			l, conn := accept(t, "1337")
// 			conn = greet(t, conn, "mephi.ru")
// 			conn = recvMasterHello(t, conn, sendLimit, recvLimit, nodeID)
// 			conn = sendAccept(t, conn, nodeID, confs)
// 			conn.Close()
// 			_ = l.Close()
// 		}()
// 		time.Sleep(time.Second)
// 		err := clnt.Connect(entities.Broker, entities.NodeID(nodeID), "127.0.0.1", 1337)
// 		assert.NoError(t, err)
// 		event := <-clnt.GetRecvMessageChan()
// 		assert.Equal(t, event, entities.FuzzerMessage{
// 			From: entities.FuzzerID{
// 				ClientID: 1,
// 				NodeID:   12,
// 			},
// 			Info: entities.FuzzerConf{
// 				MutatorID:  entities.MutatorID(confs[0].MutatorID),
// 				ScheduleID: entities.ScheduleID(confs[0].SchedulerID),
// 			},
// 		})
// 		event = <-clnt.GetRecvMessageChan()
// 		assert.Equal(t, event, entities.FuzzerMessage{
// 			From: entities.FuzzerID{
// 				ClientID: 2,
// 				NodeID:   12,
// 			},
// 			Info: entities.FuzzerConf{
// 				MutatorID:  entities.MutatorID(confs[1].MutatorID),
// 				ScheduleID: entities.ScheduleID(confs[1].SchedulerID),
// 			},
// 		})
// 		cc := <-clnt.closedConnectionChan
// 		assert.Equal(t, entities.NodeID(12), cc)
// 	})
// 	nodeID++
// 	t.Run("wrong greet message encoding", func(t *testing.T) {
// 		go func(tt *testing.T) {
// 			listener, conn := accept(t, "1337")
// 			connHello := brokerConnectHello{
// 				BrokerShmemDescription: brokerShMemDescription{
// 					Size: 100,
// 					ID:   shmemID{},
// 				},
// 			}
// 			c := msgpack.New()
// 			helloBytes, err := c.MarshalEnum(connHello)
// 			if err != nil {
// 				tt.Fatalf("can't marshal connhello: %v", err)
// 			}
// 			helloBytes = append([]byte{1, 2, 3, 4, 5, 222, 234}, helloBytes...)
// 			if err := conn.SendMessage(helloBytes); err != nil {
// 				tt.Fatal(err)
// 			}
// 			_, err = conn.RecvMessage()
// 			assert.Error(tt, err)
// 			assert.ErrorIs(tt, err, io.EOF)
// 			_ = listener.Close()
// 		}(t)
// 		time.Sleep(time.Second)
// 		err := clnt.Connect(entities.Broker, entities.NodeID(nodeID), "127.0.0.1", 1337)
// 		assert.Error(t, err)
// 	})
// 	nodeID++
// 	t.Run("wrong accept message encoding", func(t *testing.T) {
// 		go func() {
// 			l, conn := accept(t, "1337")
// 			conn = greet(t, conn, "mephi.ru")
// 			conn = recvMasterHello(t, conn, sendLimit, recvLimit, nodeID)
// 			ma := masterAccepted{
// 				BrokerID:             132034,
// 				FuzzerConfigurations: confs,
// 			}
// 			maData, err := msgpack.New().MarshalEnum(ma)
// 			if err != nil {
// 				t.Fatal(err)
// 			}
// 			if err := conn.SendMessage(maData); err != nil {
// 				t.Fatal(err)
// 			}
// 			_ = l.Close()
// 		}()
// 		time.Sleep(time.Second)
// 		err := clnt.Connect(entities.Broker, entities.NodeID(nodeID), "127.0.0.1", 1337)
// 		assert.Error(t, err)
// 	})
// }

// func accept(t *testing.T, port string) (net.Listener, connection) {
// 	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%s", port))
// 	if err != nil {
// 		t.Fatalf("failed to init tcp server: %v", err)
// 	}
// 	newConn, err := listener.Accept()
// 	if err != nil {
// 		t.Fatalf("can't accept connection: %v", err)
// 	}
// 	return listener, connection{conn: newConn}
// }

// func greet(t *testing.T, conn connection, hostname string) connection {
// 	connHello := brokerConnectHello{
// 		Hostname: hostname,
// 		BrokerShmemDescription: brokerShMemDescription{
// 			Size: 100,
// 			ID:   shmemID{},
// 		},
// 	}
// 	helloBytes, err := msgpack.New().MarshalEnum(connHello)
// 	if err != nil {
// 		t.Fatalf("can't marshal connhello: %v", err)
// 	}
// 	if err := conn.SendMessage(helloBytes); err != nil {
// 		t.Fatal(err)
// 	}
// 	return conn
// }

// func recvMasterHello(t *testing.T, conn connection, recvLimit, sendLimit, brokerID uint32) connection {
// 	rd, err := conn.RecvMessage()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	masterHello := masterNodeHello{}
// 	if err := msgpack.UnmarshalEnum(rd, &masterHello); err != nil {
// 		t.Fatal(err)
// 	}

// 	assert.Equal(t, masterHello.BrokerID, brokerID)
// 	assert.Equal(t, masterHello.RecvLimit, recvLimit)
// 	assert.Equal(t, masterHello.SendLimit, sendLimit)
// 	return conn
// }

// func sendAccept(t *testing.T, conn connection, brokerID uint32, confs []FuzzerConfiguration) connection {
// 	ma := masterAccepted{
// 		BrokerID:             brokerID,
// 		FuzzerConfigurations: confs,
// 	}
// 	maData, err := msgpack.New().MarshalEnum(ma)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if err := conn.SendMessage(maData); err != nil {
// 		t.Fatal(err)
// 	}
// 	return conn
// }

// func TestHandlingBrokerConnection(t *testing.T) {
// 	nodeID := uint32(12)
// 	recvLimit := uint32(3)
// 	sendLimit := uint32(4)
// 	confs := []FuzzerConfiguration{
// 		{
// 			MutatorID:   "mut1",
// 			SchedulerID: "sch1",
// 		},
// 		{
// 			MutatorID:   "mut2",
// 			SchedulerID: "sch2",
// 		},
// 	}
// 	srv := NewTcpClient(sendLimit, recvLimit, 100*time.Millisecond, time.Second)
// 	tn := time.Now()
// 	tc := newTestcase{
// 		Input: input{
// 			Input: []int{1, 2, 3, 4, 5, 6},
// 		},
// 		Executions: 10,
// 		Timestamp: timestamp{
// 			Secs: uint64(tn.Unix()),
// 			Nsec: uint64(tn.Nanosecond()),
// 		},
// 	}

// 	t.Run("recved testcase", func(t *testing.T) {
// 		go func(t *testing.T) {
// 			l, conn := accept(t, "1337")
// 			conn = greet(t, conn, "mephi.ru")
// 			conn = recvMasterHello(t, conn, sendLimit, recvLimit, nodeID)
// 			conn = sendAccept(t, conn, nodeID, confs)
// 			data := encodeTestcase(t, tc)
// 			if err := conn.SendMessage(data); err != nil {
// 				t.Fatal(err)
// 			}
// 			conn.Close()
// 			_ = l.Close()
// 		}(t)
// 		time.Sleep(time.Second)
// 		err := srv.Connect(entities.Broker, entities.NodeID(nodeID), "127.0.0.1", 1337)
// 		assert.NoError(t, err)
// 		event := <-srv.GetRecvMessageChan()
// 		assert.Equal(t, event, entities.FuzzerMessage{
// 			From: entities.FuzzerID{
// 				ClientID: 1,
// 				NodeID:   12,
// 			},
// 			Info: entities.FuzzerConf{
// 				MutatorID:  entities.MutatorID(confs[0].MutatorID),
// 				ScheduleID: entities.ScheduleID(confs[0].SchedulerID),
// 			},
// 		})
// 		event = <-srv.GetRecvMessageChan()
// 		assert.Equal(t, event, entities.FuzzerMessage{
// 			From: entities.FuzzerID{
// 				ClientID: 2,
// 				NodeID:   12,
// 			},
// 			Info: entities.FuzzerConf{
// 				MutatorID:  entities.MutatorID(confs[1].MutatorID),
// 				ScheduleID: entities.ScheduleID(confs[1].SchedulerID),
// 			},
// 		})

// 		event = <-srv.GetRecvMessageChan()
// 		assert.Equal(t, event, entities.FuzzerMessage{
// 			From: entities.FuzzerID{
// 				ClientID: 1,
// 				NodeID:   12,
// 			},
// 			Info: entities.Testcase{
// 				InputData: []byte{1, 2, 3, 4, 5, 6},
// 				Execs:     10,
// 				CreatedAt: time.Unix(tn.Unix(), int64(tn.Nanosecond())).In(time.Local),
// 				InputHash: hashing.MakeHash([]byte{1, 2, 3, 4, 5, 6}),
// 			},
// 		})
// 		cc := <-srv.closedConnectionChan
// 		assert.Equal(t, entities.NodeID(12), cc)
// 	})
// }

// func encodeTestcase(t *testing.T, tc newTestcase) []byte {
// 	data, err := msgpack.New().MarshalEnum(tc)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tmm := tcpMasterMessage{
// 		Flags:    uint32(entities.Master) | uint32(entities.NewTestCase),
// 		Payload:  msgpack.CovertTo[byte, int](data),
// 		ClientID: 1,
// 	}
// 	dataMM, err := msgpack.New().Marshal(tmm)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	return dataMM
// }

// func sendAccept(t *testing.T, conn connection, brokerID uint32, confs []FuzzerConfiguration) connection {
// 	ma := masterAccepted{
// 		BrokerID:             brokerID,
// 		FuzzerConfigurations: confs,
// 	}
// 	maData, err := msgpack.New().MarshalEnum(ma)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if err := conn.SendMessage(maData); err != nil {
// 		t.Fatal(err)
// 	}
// 	return conn
// }

// func TestHandlingBrokerConnection(t *testing.T) {
// 	nodeID := uint32(12)
// 	recvLimit := uint32(3)
// 	sendLimit := uint32(4)
// 	confs := []FuzzerConfiguration{
// 		{
// 			MutatorID:   "mut1",
// 			SchedulerID: "sch1",
// 		},
// 		{
// 			MutatorID:   "mut2",
// 			SchedulerID: "sch2",
// 		},
// 	}
// 	srv := NewTcpClient(sendLimit, recvLimit, 100*time.Millisecond, time.Second)
// 	tn := time.Now()
// 	tc := newTestcase{
// 		Input: input{
// 			Input: []int{1, 2, 3, 4, 5, 6},
// 		},
// 		Executions: 10,
// 		Timestamp: timestamp{
// 			Secs: uint64(tn.Unix()),
// 			Nsec: uint64(tn.Nanosecond()),
// 		},
// 	}

// 	t.Run("recved testcase", func(t *testing.T) {
// 		go func(t *testing.T) {
// 			l, conn := accept(t, "1337")
// 			conn = greet(t, conn, "mephi.ru")
// 			conn = recvMasterHello(t, conn, sendLimit, recvLimit, nodeID)
// 			conn = sendAccept(t, conn, nodeID, confs)
// 			data := encodeTestcase(t, tc)
// 			if err := conn.SendMessage(data); err != nil {
// 				t.Fatal(err)
// 			}
// 			conn.Close()
// 			_ = l.Close()
// 		}(t)
// 		time.Sleep(time.Second)
// 		err := srv.Connect(entities.Broker, entities.NodeID(nodeID), "127.0.0.1", 1337)
// 		assert.NoError(t, err)
// 		event := <-srv.GetRecvMessageChan()
// 		assert.Equal(t, event, entities.FuzzerMessage{
// 			From: entities.FuzzerID{
// 				ClientID: 1,
// 				NodeID:   12,
// 			},
// 			Info: entities.FuzzerConf{
// 				MutatorID:  entities.MutatorID(confs[0].MutatorID),
// 				ScheduleID: entities.ScheduleID(confs[0].SchedulerID),
// 			},
// 		})
// 		event = <-srv.GetRecvMessageChan()
// 		assert.Equal(t, event, entities.FuzzerMessage{
// 			From: entities.FuzzerID{
// 				ClientID: 2,
// 				NodeID:   12,
// 			},
// 			Info: entities.FuzzerConf{
// 				MutatorID:  entities.MutatorID(confs[1].MutatorID),
// 				ScheduleID: entities.ScheduleID(confs[1].SchedulerID),
// 			},
// 		})

// 		event = <-srv.GetRecvMessageChan()
// 		assert.Equal(t, event, entities.FuzzerMessage{
// 			From: entities.FuzzerID{
// 				ClientID: 1,
// 				NodeID:   12,
// 			},
// 			Info: entities.Testcase{
// 				InputData: []byte{1, 2, 3, 4, 5, 6},
// 				Execs:     10,
// 				CreatedAt: time.Unix(tn.Unix(), int64(tn.Nanosecond())).In(time.Local),
// 				InputHash: hashing.MakeHash([]byte{1, 2, 3, 4, 5, 6}),
// 			},
// 		})
// 		cc := <-srv.closedConnectionChan
// 		assert.Equal(t, entities.NodeID(12), cc)
// 	})
// }

// func encodeTestcase(t *testing.T, tc newTestcase) []byte {
// 	data, err := msgpack.New().MarshalEnum(tc)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tmm := tcpMasterMessage{
// 		Flags:    uint32(entities.Master) | uint32(entities.NewTestCase),
// 		Payload:  msgpack.CovertTo[byte, int](data),
// 		ClientID: 1,
// 	}
// 	dataMM, err := msgpack.New().Marshal(tmm)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	return dataMM
// }
