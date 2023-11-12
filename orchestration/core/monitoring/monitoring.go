package monitoring

import (
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"time"
)

type evaluator interface {
	// Evaluate - оценивает полученные тест-кейсы
	Evaluate(testCases []entities.Testcase) ([]entities.EvaluatingData, error)
}

type fuzzInfoDB interface {
	AddFuzzer(fuzzer entities.Fuzzer) error
	AddTestcases(idList []entities.FuzzerID, tcList []entities.Testcase, evalDataList []entities.EvaluatingData)
}

const (
	evalRetryCount = 3
)

// эта штука очень хорошо и правильно скейлится
// получается что можно делать воркер пул из мониторов
// если начинается просад по времени
type monitoring struct {
	recvMsgChan <-chan entities.FuzzerMessage

	closeChan chan struct{}
	isClosed  bool

	evaler evaluator
	db     fuzzInfoDB

	tcBuf         []entities.Testcase
	fuzzerIDBuf   []entities.FuzzerID
	bufferMaxSize uint

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
		recvMsgChan:   recvMsgChan,
		evaler:        evaler,
		db:            db,
		closeChan:     make(chan struct{}),
		tcBuf:         make([]entities.Testcase, 0, bufferMaxSize),
		fuzzerIDBuf:   make([]entities.FuzzerID, 0, bufferMaxSize),
		saveTimeout:   saveTimeout,
		bufferMaxSize: bufferMaxSize,
	}
}

// Run -. (запускать в отдельной горутине)
func (m *monitoring) Run() {
	ticker := time.NewTicker(m.saveTimeout)
	for {
		select {
		case <-ticker.C:
			m.saveBuf()
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
				m.fuzzerIDBuf = append(m.fuzzerIDBuf, msg.From)
				m.tcBuf = append(m.tcBuf, msg.Info.(entities.Testcase))

				if uint(len(m.tcBuf)) == m.bufferMaxSize {
					m.saveBuf()
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
	if len(m.tcBuf) == 0 {
		return
	}

	tcBuf := m.tcBuf
	m.tcBuf = make([]entities.Testcase, 0, m.bufferMaxSize)

	fuzzerIDBuf := m.fuzzerIDBuf
	m.fuzzerIDBuf = make([]entities.FuzzerID, 0, m.bufferMaxSize)

	go func() {
		for i := 0; i < evalRetryCount; i++ {
			evalData, err := m.evaler.Evaluate(tcBuf)
			if err != nil {
				logger.Errorf(err, "try [%d]: failed to evaluate %d testcases", i, len(tcBuf))
				continue
			}
			m.db.AddTestcases(fuzzerIDBuf, tcBuf, evalData)
			return
		}
	}()

}
