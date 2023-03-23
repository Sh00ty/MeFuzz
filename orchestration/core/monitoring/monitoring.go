package monitoring

import (
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"time"
)

type evaluator interface {
	// Evaluate - оценивает полученные тест-кейсы
	Evaluate(testCases []entities.Testcase, len uint) ([]entities.EvaluatingData, error)
}

type fuzzInfoDB interface {
	AddFuzzer(fuzzer entities.Fuzzer) error
	AddTestcases(idList []entities.FuzzerID, tcList []entities.Testcase, evalDataList []entities.EvaluatingData)
}

const (
	evalRetryCount = 3
)

type monitoring struct {
	recvMsgChan <-chan entities.FuzzerMessage
	closeChan   chan struct{}
	isClosed    bool
	evaler      evaluator
	db          fuzzInfoDB
	tcBuf       []entities.Testcase
	fuzzerIDBuf []entities.FuzzerID
	bufPtr      uint
	saveTimeout time.Duration
}

func New(
	recvMsgChan <-chan entities.FuzzerMessage,
	evaler evaluator,
	db fuzzInfoDB,
	bufferMaxSize uint,
	saveTimeout time.Duration,
) *monitoring {
	if bufferMaxSize < 1 {
		panic("buffer size must be greater then 0")
	}
	return &monitoring{
		recvMsgChan: recvMsgChan,
		evaler:      evaler,
		db:          db,
		closeChan:   make(chan struct{}),
		tcBuf:       make([]entities.Testcase, bufferMaxSize),
		fuzzerIDBuf: make([]entities.FuzzerID, bufferMaxSize),
		saveTimeout: saveTimeout,
	}
}

// Run -. (запускать в отдельной горутине)
func (m *monitoring) Run() {
	ticker := time.NewTicker(m.saveTimeout)
	for {
		select {
		case <-ticker.C:
			m.saveBuf()
			m.bufPtr = 0
			ticker.Reset(m.saveTimeout)
		case msg := <-m.recvMsgChan:
			switch msg.Info.Kind() {
			case entities.Configuration:
				if err := m.db.AddFuzzer(entities.Fuzzer{
					ID:            msg.From,
					Configuration: msg.Info.(entities.FuzzerConf),
					Registered:    time.Now(),
					Testcases:     make(map[uint64]struct{}, 0),
				}); err != nil {
					logger.Errorf(err, "failed to add new fuzzer: %v", msg.From)
				}
				logger.Infof("ADDED NEW FUZZER: %v", msg.From)
				continue
			case entities.NewTestCase:
				m.fuzzerIDBuf[m.bufPtr] = msg.From
				m.tcBuf[m.bufPtr] = msg.Info.(entities.Testcase)
				m.bufPtr++
				if m.bufPtr == uint(len(m.tcBuf)) {
					m.saveBuf()
					m.bufPtr = 0
					ticker.Reset(m.saveTimeout)
				}
			}
		case <-m.closeChan:
			return
		}
	}
}

func (m *monitoring) Close() {
	if !m.isClosed {
		m.isClosed = true
		m.closeChan <- struct{}{}
		close(m.closeChan)
	}
}

func (m *monitoring) saveBuf() {
	if m.bufPtr == 0 {
		return
	}
	for i := 0; i < evalRetryCount; i++ {
		evalData, err := m.evaler.Evaluate(m.tcBuf, m.bufPtr)
		if err != nil {
			logger.Errorf(err, "failed to evaluate %d testcases", m.bufPtr)
			continue
		}
		m.db.AddTestcases(m.fuzzerIDBuf[:m.bufPtr], m.tcBuf[:m.bufPtr], evalData)
		return
	}
}
