package master

import (
	"context"
	"errors"
	"fmt"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
)

type fuzzClnt interface {
	// Evaluate - оценивает полученные тест-кейсы
	GetTestcase(ctx context.Context) (entities.Testcase, error)
	GetElementID() entities.ElementID
	worker
}

type Fuzzer struct {
	cancel context.CancelFunc

	testcaseChan           chan entities.Testcase
	configurationEventChan chan<- Event

	fuzzer      fuzzClnt
	nodeIDStr   string
	onNodeIDStr string
}

func newFuzzer(
	testcaseChan chan entities.Testcase,
	configurationEventChan chan<- Event,
	fuzzer fuzzClnt,
) *Fuzzer {
	if bufferMaxSize < 1 {
		panic("buffer size must be greater then 0")
	}
	return &Fuzzer{
		testcaseChan:           testcaseChan,
		configurationEventChan: configurationEventChan,
		fuzzer:                 fuzzer,
		nodeIDStr:              fmt.Sprintf("%d", fuzzer.GetElementID().NodeID),
		onNodeIDStr:            fmt.Sprintf("%d", fuzzer.GetElementID().OnNodeID),
	}
}

func (f *Fuzzer) Run(ctx context.Context) {
	ctx, f.cancel = context.WithCancel(ctx)
	for {
		tc, err := f.fuzzer.GetTestcase(ctx)
		if errors.Is(err, ErrStopElement) {
			f.cancel()
			f.configurationEventChan <- Event{
				Event:    ElementDeleted,
				NodeType: entities.Fuzzer,
				Element:  f.fuzzer.GetElementID(),
			}
			return
		}
		if err != nil {
			logger.Errorf(err, "failed to get testcase from fuzzer %v", f.fuzzer.GetElementID())
			continue
		}
		generatedTestcaseCount.WithLabelValues(f.nodeIDStr, f.onNodeIDStr).Inc()

		select {
		case f.testcaseChan <- tc:
		default:
			f.configurationEventChan <- Event{
				Event:    NeedMoreEvalers,
				NodeType: entities.Fuzzer,
			}
			select {
			case f.testcaseChan <- tc:
			case <-ctx.Done():
				logger.Infof("closed fuzzer %v", f.fuzzer.GetElementID())
				return
			}
		}
		testcaseInChan.Inc()
	}
}

func (e *Fuzzer) Stop() {
	e.cancel()
}
