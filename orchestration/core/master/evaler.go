package master

import (
	"context"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"time"
)

type evalClnt interface {
	// Evaluate - оценивает полученные тест-кейсы
	Evaluate(testCases []entities.Testcase) ([]entities.EvaluatingData, error)
}

const (
	evalRetryCount = 1
	saveTimeout    = 5 * time.Minute
	bufferMaxSize  = 128
)

// На каждый оценщик поднимается один воркер
// это интуитивно тк там один поток оценивает
// еще таким образом удобно вводить/выводить оценщика
// также выглядит как здравая балансировка
type Worker struct {
	cancel context.CancelFunc

	newTestcasesChan <-chan entities.Testcase
	performanceChan  chan<- Event

	evaler evalClnt
	db     fuzzInfoDB

	tcBuf []entities.Testcase
}

func newWorker(
	newTestcasesChan <-chan entities.Testcase,
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
		if err != nil {
			logger.Errorf(err, "try [%d]: failed to evaluate %d testcases", i, len(w.tcBuf))
			continue
		}
		w.db.AddTestcases(w.tcBuf, evalData)
		return
	}

	w.tcBuf = w.tcBuf[:0]
}
