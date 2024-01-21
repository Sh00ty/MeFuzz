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
	saveTimeout    = 5 * time.Minute
	bufferMaxSize  = 1
)

// На каждый оценщик поднимается один воркер
// это интуитивно тк там один поток оценивает
// еще таким образом удобно вводить/выводить оценщика
// также выглядит как здравая балансировка
type Worker struct {
	cancel context.CancelFunc

	newTestcasesChan chan entities.Testcase
	performanceChan  chan<- Event

	evaler evalClnt
	db     fuzzInfoDB

	tcBuf []entities.Testcase
}

func newWorker(
	newTestcasesChan chan entities.Testcase,
	performanceChan chan<- Event,
	db fuzzInfoDB,
	evaler evalClnt,
) *Worker {
	if bufferMaxSize < 1 {
		panic("buffer size must be greater then 0")
	}
	return &Worker{
		newTestcasesChan: newTestcasesChan,
		performanceChan:  performanceChan,
		db:               db,
		evaler:           evaler,
		tcBuf:            make([]entities.Testcase, 0, bufferMaxSize),
	}
}

func (w *Worker) Run(ctx context.Context) {
	ctx, w.cancel = context.WithCancel(ctx)

	ticker := time.NewTicker(saveTimeout)
	for {
		select {
		case <-ticker.C:
			w.performanceChan <- Event{
				Event: NeedMoreFuzzers,
			}
			w.flush()
		case testcase := <-w.newTestcasesChan:
			w.tcBuf = append(w.tcBuf, testcase)
			if uint(len(w.tcBuf)) == bufferMaxSize {
				w.flush()
			}
		case <-ctx.Done():
			logger.Infof("worker stoped")
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
			w.performanceChan <- Event{
				Event:    ElementDeleted,
				NodeType: entities.Evaler,
				Element:  w.evaler.GetElementID(),
			}
			for _, tc := range w.tcBuf {
				logger.Infof("resend tc %d to testcase chan", tc.ID)
				w.newTestcasesChan <- tc
			}
			return
		}
		if err != nil {
			logger.Errorf(err, "try [%d]: failed to evaluate %d testcases", i, len(w.tcBuf))
			continue
		}

		logger.Debugf("evaled new testcases")
		for _, ed := range evalData {
			fmt.Printf("%v", ed)
		}
		w.db.AddTestcases(w.tcBuf, evalData)
		break
	}

	w.tcBuf = w.tcBuf[:0]
}
