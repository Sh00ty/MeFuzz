package tcp

import (
	"context"
	"orchestration/core/master"
	"orchestration/entities"
	"orchestration/infra/utils/hashing"
	"orchestration/infra/utils/logger"
	"orchestration/infra/utils/msgpack"
	"time"

	"github.com/pkg/errors"
)

type Fuzzer struct {
	conn                 MultiplexedConnection
	onNodeIDs            entities.OnNodeID
	performanceEventChan chan<- master.Event
	recvMsgChan          chan entities.Testcase
}

func NewFuzzer(
	conn MultiplexedConnection,
	onNodeIDs entities.OnNodeID,
	performanceEventChan chan<- master.Event,
	recvMsgChan chan entities.Testcase,
) *Fuzzer {
	return &Fuzzer{
		conn:                 conn,
		onNodeIDs:            onNodeIDs,
		performanceEventChan: performanceEventChan,
		recvMsgChan:          recvMsgChan,
	}
}

func (f *Fuzzer) Start(ctx context.Context) {
	for {
		out := newTestcase{}
		if err := f.conn.Recv(&out); err != nil {
			if errors.Is(err, ErrConnectionClosed) {
				logger.Infof("fuzzer connection on conn %v closed", f.conn)
				return
			}
			logger.Errorf(err, "failed to recv output message")
			continue
		}
		inputData := msgpack.CovertTo[int, byte](out.Input.Input)
		tc := entities.Testcase{
			ID: hashing.MakeHash(inputData),
			FuzzerID: entities.ElementID{
				NodeID:   f.conn.conn.NodeID,
				OnNodeID: f.onNodeIDs,
			},
			InputData:  inputData,
			Execs:      out.Executions,
			CorpusSize: out.CorpusSize,
			CreatedAt:  time.Unix(int64(out.Timestamp.Secs), int64(out.Timestamp.Nsec)).In(time.Local),
		}

		select {
		case f.recvMsgChan <- tc:
		default:
			f.performanceEventChan <- master.Event{
				Event:    master.NeedMoreEvalers,
				NodeType: entities.Broker,
			}
			select {
			case f.recvMsgChan <- tc:
			case <-ctx.Done():
				logger.Infof("closed fuzzer %v %v", f.conn.conn.NodeID, f.onNodeIDs)
				return
			}
		}
	}
}
