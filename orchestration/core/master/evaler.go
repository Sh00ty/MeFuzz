package master

import (
	"context"
	"errors"
	"fmt"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"time"
)

type evalClnt interface {
	// Evaluate - оценивает полученные тест-кейсы
	Evaluate(testCases []entities.Testcase) ([]entities.EvaluatingData, error)
	GetElementID() entities.ElementID
}

const (
	evalRetryCount = 1
	saveTimeout    = 1 * time.Minute
	bufferMaxSize  = 10
)

// На каждый оценщик поднимается один воркер
// это интуитивно тк там один поток оценивает
// еще таким образом удобно вводить/выводить оценщика
// также выглядит как здравая балансировка
type Worker struct {
	cancel context.CancelFunc

	testcaseChan           chan entities.Testcase
	configurationEventChan chan<- Event

	evaler evalClnt
	db     fuzzInfoDB

	tcBuf []entities.Testcase
}

func newWorker(
	testcaseChan chan entities.Testcase,
	configurationEventChan chan<- Event,
	db fuzzInfoDB,
	evaler evalClnt,
) *Worker {
	if bufferMaxSize < 1 {
		panic("buffer size must be greater then 0")
	}
	return &Worker{
		testcaseChan:           testcaseChan,
		configurationEventChan: configurationEventChan,
		db:                     db,
		evaler:                 evaler,
		tcBuf:                  make([]entities.Testcase, 0, bufferMaxSize),
	}
}

func (w *Worker) Run(ctx context.Context) {
	ctx, w.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(saveTimeout)
	secondeChance := true
	for {
		select {
		case <-ticker.C:
			w.flush()
			if !secondeChance {
				w.configurationEventChan <- Event{
					Event: NeedMoreFuzzers,
				}
				secondeChance = true
				continue
			}
			secondeChance = false
		case testcase := <-w.testcaseChan:
			w.tcBuf = append(w.tcBuf, testcase)
			if uint(len(w.tcBuf)) == bufferMaxSize {
				w.flush()
			}
			secondeChance = true
			ticker.Reset(saveTimeout)
		case <-ctx.Done():
			logger.Infof("eval worker stoped")
			return
		}
	}
}

func (w *Worker) Close() {
	w.cancel()
}

func (w *Worker) flush() {
	if len(w.tcBuf) == 0 {
		return
	}

	for i := 0; i < evalRetryCount+1; i++ {
		evalData, err := w.evaler.Evaluate(w.tcBuf)
		if errors.Is(err, ErrStopElement) {
			w.cancel()
			w.configurationEventChan <- Event{
				Event:    ElementDeleted,
				NodeType: entities.Evaler,
				Element:  w.evaler.GetElementID(),
			}
			for _, tc := range w.tcBuf {
				logger.Infof("resend tc %d to testcase chan", tc.ID)
				w.testcaseChan <- tc
			}
			return
		}
		if err != nil {
			logger.Errorf(err, "try [%d]: failed to evaluate %d testcases", i, len(w.tcBuf))
			continue
		}

		logger.Debugf("evaled new testcases")
		for i, ed := range evalData {
			fmt.Printf("%v%v\n", w.tcBuf[i], ed)
		}
		w.db.AddTestcases(w.tcBuf, evalData)
		break
	}

	w.tcBuf = w.tcBuf[:0]
}
