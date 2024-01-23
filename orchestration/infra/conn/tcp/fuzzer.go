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
	conn                   MultiplexedConnection
	onNodeIDs              entities.OnNodeID
	configurationEventChan chan<- master.Event
	testcaseChan           chan entities.Testcase
}

func NewFuzzer(
	conn MultiplexedConnection,
	onNodeIDs entities.OnNodeID,
	configurationEventChan chan<- master.Event,
	testcaseChan chan entities.Testcase,
) *Fuzzer {
	return &Fuzzer{
		conn:                   conn,
		onNodeIDs:              onNodeIDs,
		configurationEventChan: configurationEventChan,
		testcaseChan:           testcaseChan,
	}
}

func (f *Fuzzer) Start(ctx context.Context) {
	for {
		outBytes, err := f.conn.RecvBytes()
		if err != nil {
			if errors.Is(err, ErrConnectionClosed) {
				f.configurationEventChan <- master.Event{
					Event:    master.ElementDeleted,
					NodeType: entities.Fuzzer,
					Element: entities.ElementID{
						NodeID:   f.conn.conn.NodeID,
						OnNodeID: f.onNodeIDs,
					},
				}
				logger.Infof("fuzzer connection on conn %v closed", f.conn)
				return
			}
			logger.Errorf(err, "failed to recv output message")
			continue
		}
		out := &newTestcase{}
		if err := msgpack.UnmarshalEnum(outBytes, out); err != nil {
			logger.Errorf(err, "failed to unmarshal new testcase")
		}

		inputData := msgpack.CovertTo[int, byte](out.Input.Input)
		tc := entities.Testcase{
			ID: hashing.MakeHash(inputData),
			FuzzerID: entities.ElementID{
				NodeID:   f.conn.conn.NodeID,
				OnNodeID: f.onNodeIDs,
			},
			InputData:  inputData,
			Execs:      uint64(out.Executions),
			CorpusSize: uint64(out.CorpusSize),
			CreatedAt:  time.Unix(int64(out.Timestamp.Secs), int64(out.Timestamp.Nsec)).In(time.Local),
		}

		select {
		case f.testcaseChan <- tc:
		default:
			f.configurationEventChan <- master.Event{
				Event:    master.NeedMoreEvalers,
				NodeType: entities.Fuzzer,
			}
			select {
			case f.testcaseChan <- tc:
			case <-ctx.Done():
				logger.Infof("closed fuzzer %v %v", f.conn.conn.NodeID, f.onNodeIDs)
				return
			}
		}
	}
}
