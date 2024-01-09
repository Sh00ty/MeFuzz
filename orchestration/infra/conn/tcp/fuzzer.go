package tcp

import (
	"context"
	"orchestration/core/master"
	"orchestration/entities"
	"orchestration/infra/utils/hashing"
	"orchestration/infra/utils/logger"
	"orchestration/infra/utils/msgpack"
	"time"
)

type Fuzzer struct {
	conn                 MultiplexedConnection
	onNodeIDs            entities.OnNodeID
	performanceEventChan chan<- master.Event
}

func NewFuzzer(
	conn MultiplexedConnection,
	onNodeIDs entities.OnNodeID,
	performanceEventChan chan<- master.Event,
) *Fuzzer {
	return &Fuzzer{
		conn:                 conn,
		onNodeIDs:            onNodeIDs,
		performanceEventChan: performanceEventChan,
	}
}

func (f *Fuzzer) Start(ctx context.Context, recvMsgChan chan entities.Testcase) {
	for {
		out := newTestcase{}
		if err := f.conn.Recv(&out); err != nil {
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
		case recvMsgChan <- tc:
		default:
			f.performanceEventChan <- master.Event{
				Event:    master.NeedMoreEvalers,
				NodeType: entities.Broker,
			}
			select {
			case recvMsgChan <- tc:
			case <-ctx.Done():
				logger.Infof("closed fuzzer %v %v", f.conn.conn.NodeID, f.onNodeIDs)
				return
			}
		}
	}
}
