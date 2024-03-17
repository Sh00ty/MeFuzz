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
	onNodeID               entities.OnNodeID
	configurationEventChan chan<- master.Event
	testcaseChan           chan entities.Testcase
}

func NewFuzzer(
	conn MultiplexedConnection,
	onNodeID entities.OnNodeID,
	configurationEventChan chan<- master.Event,
	testcaseChan chan entities.Testcase,
) *Fuzzer {
	return &Fuzzer{
		conn:                   conn,
		onNodeID:               onNodeID,
		configurationEventChan: configurationEventChan,
		testcaseChan:           testcaseChan,
	}
}

func (f *Fuzzer) GetTestcase(ctx context.Context) (entities.Testcase, error) {
	outBytes, err := f.conn.RecvBytes()
	if err != nil {
		if errors.Is(err, ErrConnectionClosed) {
			logger.Infof("fuzzer connection on conn %v closed", f.conn)
			return entities.Testcase{}, master.ErrStopElement
		}
		return entities.Testcase{}, errors.Wrap(err, "failed to recv output message")
	}
	out := &newTestcase{}
	if err := msgpack.UnmarshalEnum(outBytes, out); err != nil {
		return entities.Testcase{}, errors.Wrap(err, "failed to unmarshal new testcase")
	}

	inputData := msgpack.CovertTo[int, byte](out.Input.Input)
	return entities.Testcase{
		ID: hashing.MakeHash(inputData),
		FuzzerID: entities.ElementID{
			NodeID:   f.conn.conn.NodeID,
			OnNodeID: f.onNodeID,
		},
		InputData:  inputData,
		Execs:      uint64(out.Executions),
		CorpusSize: uint64(out.CorpusSize),
		CreatedAt:  time.Unix(int64(out.Timestamp.Secs), int64(out.Timestamp.Nsec)).In(time.Local),
	}, nil
}

func (f *Fuzzer) GetElementID() entities.ElementID {
	return entities.ElementID{
		NodeID:   f.conn.conn.NodeID,
		OnNodeID: f.onNodeID,
	}
}

func (f *Fuzzer) Stop() {
	f.conn.Close()
}
