package monitoring

import (
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"time"
)

type evalClnt interface {
	// Evaluate - оценивает полученные тест-кейсы
	Evaluate(testCases []entities.Testcase) ([]entities.EvaluatingData, error)
}

type fuzzInfoDB interface {
	AddFuzzer(fuzzer entities.Fuzzer) error
	AddTestcases(tcList []entities.Testcase, evalDataList []entities.EvaluatingData)
}

const (
	evalRetryCount = 1
	bufferMaxSize  = 128
)

// эта штука очень хорошо и правильно скейлится
// получается что можно делать воркер пул из мониторов
// если начинается просад по времени
type monitoring struct {
	recvMsgChan <-chan entities.FuzzerMessage

	closeChan chan struct{}

	evaler evalClnt
	db     fuzzInfoDB

	tcBuf []entities.Testcase

	saveTimeout time.Duration
}

func New(
	recvMsgChan <-chan entities.FuzzerMessage,
	db fuzzInfoDB,
	saveTimeout time.Duration,
) *monitoring {
	if bufferMaxSize < 1 {
		panic("buffer size must be greater then 0")
	}
	return &monitoring{
		recvMsgChan: recvMsgChan,
		db:          db,
		closeChan:   make(chan struct{}),
		tcBuf:       make([]entities.Testcase, 0, bufferMaxSize),
		saveTimeout: saveTimeout,
	}
}

// Run -. (запускать в отдельной горутине)
func (m *monitoring) Run() {
	ticker := time.NewTicker(m.saveTimeout)
	for {
		select {
		case <-ticker.C:
			m.flush()
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
			case entities.NewTestCase:
				m.tcBuf = append(m.tcBuf, msg.Info.(entities.Testcase))

				if uint(len(m.tcBuf)) == bufferMaxSize {
					m.flush()
				}
			}
		case <-m.closeChan:
			return
		}
	}
}

func (m *monitoring) Close() {
	select {
	case <-m.closeChan:
		return
	default:
	}

	close(m.closeChan)
}

func (m *monitoring) flush() {
	if len(m.tcBuf) == 0 {
		return
	}

	for i := 0; i < evalRetryCount+1; i++ {
		evalData, err := m.evaler.Evaluate(m.tcBuf)
		if err != nil {
			logger.Errorf(err, "try [%d]: failed to evaluate %d testcases", i, len(m.tcBuf))
			continue
		}
		m.db.AddTestcases(m.tcBuf, evalData)
		return
	}

	m.tcBuf = m.tcBuf[:0]
}
