package monitoring

import (
	"orchestration/core/master"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"time"
)

type evalClnt interface {
	// Evaluate - оценивает полученные тест-кейсы
	Evaluate(testCases []entities.Testcase) ([]entities.EvaluatingData, error)
}

type fuzzInfoDB interface {
	AddTestcases(tcList []entities.Testcase, evalDataList []entities.EvaluatingData)
}

const (
	evalRetryCount = 1
	bufferMaxSize  = 128
)

// На каждый оценщик поднимается один воркер
// это интуитивно тк там один поток оценивает
// еще таким образом удобно вводить/выводить оценщика
// также выглядит как здравая балансировка
type Worker struct {
	newTestcasesChan <-chan entities.Testcase
	closeChan        chan struct{}
	performanceChan  chan<- master.Event

	evaler evalClnt
	db     fuzzInfoDB

	tcBuf []entities.Testcase

	saveTimeout time.Duration
}

func New(
	newTestcasesChan <-chan entities.Testcase,
	performanceChan chan<- master.Event,
	db fuzzInfoDB,
	evaler evalClnt,
	saveTimeout time.Duration,
) *Worker {
	if bufferMaxSize < 1 {
		panic("buffer size must be greater then 0")
	}
	return &Worker{
		newTestcasesChan: newTestcasesChan,
		performanceChan:  performanceChan,
		db:               db,
		evaler:           evaler,
		closeChan:        make(chan struct{}),
		tcBuf:            make([]entities.Testcase, 0, bufferMaxSize),
		saveTimeout:      saveTimeout,
	}
}

// Run -. (запускать в отдельной горутине)
func (w *Worker) Run() {
	ticker := time.NewTicker(w.saveTimeout)
	for {
		select {
		case <-ticker.C:
			w.performanceChan <- master.Event{
				Event: master.NeedMoreFuzzers,
			}
			w.flush()
		case testcase := <-w.newTestcasesChan:
			w.tcBuf = append(w.tcBuf, testcase)
			if uint(len(w.tcBuf)) == bufferMaxSize {
				w.flush()
			}
		case <-w.closeChan:
			return
		}
	}
}

func (w *Worker) Close() {
	close(w.closeChan)
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
