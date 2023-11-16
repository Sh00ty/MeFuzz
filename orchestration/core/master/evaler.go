package master

import (
	"context"
	"errors"
	"fmt"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type evalClnt interface {
	// Evaluate - оценивает полученные тест-кейсы
	Evaluate(testCases []entities.Testcase) ([]entities.EvaluatingData, error)
	GetElementID() entities.ElementID
	worker
}

const (
	evalRetryCount = 1
	saveTimeout    = 5 * time.Second
	bufferMaxSize  = 32
)

type Evaler struct {
	cancel context.CancelFunc

	testcaseChan           chan entities.Testcase
	configurationEventChan chan<- Event

	evaler evalClnt
	db     fuzzInfoDB

	nodeIDStr   string
	onNodeIDStr string

	tcBuf []entities.Testcase
}

func newEvaler(
	testcaseChan chan entities.Testcase,
	configurationEventChan chan<- Event,
	db fuzzInfoDB,
	evaler evalClnt,
) *Evaler {
	if bufferMaxSize < 1 {
		panic("buffer size must be greater then 0")
	}
	return &Evaler{
		testcaseChan:           testcaseChan,
		configurationEventChan: configurationEventChan,
		db:                     db,
		evaler:                 evaler,
		tcBuf:                  make([]entities.Testcase, 0, bufferMaxSize),
		nodeIDStr:              fmt.Sprintf("%d", evaler.GetElementID().NodeID),
		onNodeIDStr:            fmt.Sprintf("%d", evaler.GetElementID().OnNodeID),
	}
}

func (e *Evaler) Run(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(saveTimeout)
	secondeChance := true
	for {
		select {
		case <-ticker.C:
			e.flush()
			if !secondeChance {
				e.configurationEventChan <- Event{
					Event: NeedMoreFuzzers,
				}
				secondeChance = true
				continue
			}
			secondeChance = false
		case testcase := <-e.testcaseChan:
			testcaseInChan.Dec()

			e.tcBuf = append(e.tcBuf, testcase)
			if uint(len(e.tcBuf)) == bufferMaxSize {
				e.flush()
			}
			secondeChance = true
			ticker.Reset(saveTimeout)
		case <-ctx.Done():
			logger.Infof("eval worker stoped")
			return
		}
	}
}

func (e *Evaler) Stop() {
	e.cancel()
}

func (e *Evaler) flush() {
	if len(e.tcBuf) == 0 {
		return
	}

	for i := 0; i < evalRetryCount+1; i++ {
		timer := prometheus.NewTimer(evaluationTime.WithLabelValues(
			e.nodeIDStr,
			e.onNodeIDStr,
		))

		evalData, err := e.evaler.Evaluate(e.tcBuf)

		timer.ObserveDuration()

		if errors.Is(err, ErrStopElement) {
			e.cancel()
			e.configurationEventChan <- Event{
				Event:    ElementDeleted,
				NodeType: entities.Evaler,
				Element:  e.evaler.GetElementID(),
			}
			for _, tc := range e.tcBuf {
				logger.Infof("resend tc %d to testcase chan", tc.ID)
				e.testcaseChan <- tc
				testcaseInChan.Inc()
			}
			return
		}
		if err != nil {
			logger.Errorf(err, "try [%d]: failed to evaluate %d testcases", i, len(e.tcBuf))
			continue
		}

		evaludatedTestcases.
			WithLabelValues(e.nodeIDStr, e.onNodeIDStr).
			Add(float64(len(e.tcBuf)))

		e.db.AddTestcases(e.tcBuf, evalData)
		break
	}

	e.tcBuf = e.tcBuf[:0]
}
