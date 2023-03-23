package monitoring

import (
	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"
	"orchestration/core/monitoring/mocks"
	"orchestration/entities"
	"testing"
	"time"
)

func TestMonitoringRun(t *testing.T) {
	recvMsgChan := make(chan entities.FuzzerMessage)
	evaler := &mocks.Evaler{}
	evaler.Test(t)
	infoDB := &mocks.FuzzInfoDB{}
	infoDB.Test(t)
	bufferMaxsize := uint(2)
	timeout := time.Second
	monitoring := New(recvMsgChan, evaler, infoDB, bufferMaxsize, timeout)
	config := entities.FuzzerConf{
		MutatorID:  "mut1",
		ScheduleID: "Sch1",
		PoolID:     "Pook1",
		IsConcolic: true,
	}
	fuzzer1ID := entities.FuzzerID{
		ClientID: 10,
		NodeID:   5,
	}
	go monitoring.Run()

	t.Run("new configurations", func(t *testing.T) {
		msg := entities.FuzzerMessage{
			From: fuzzer1ID,
			Info: config,
		}
		infoDB.On("AddFuzzer", mock.MatchedBy(func(fuzzer entities.Fuzzer) bool {
			if fuzzer.ID != fuzzer1ID {
				return false
			}

			return true
		})).Once().Return(nil)

		recvMsgChan <- msg
		infoDB.On("AddFuzzer", mock.MatchedBy(func(fuzzer entities.Fuzzer) bool {
			if fuzzer.ID != fuzzer1ID {
				return false
			}
			return isEqualConfs(config, fuzzer.Configuration)
		})).Once().Return(nil)
		recvMsgChan <- msg
		time.Sleep(70 * time.Millisecond)
	})

	t.Run("new configuration with error", func(t *testing.T) {
		msg := entities.FuzzerMessage{
			From: fuzzer1ID,
			Info: config,
		}
		infoDB.On("AddFuzzer", mock.MatchedBy(func(fuzzer entities.Fuzzer) bool {
			if fuzzer.ID != fuzzer1ID {
				return false
			}

			return true
		})).Once().Return(nil)

		recvMsgChan <- msg
		infoDB.On("AddFuzzer", mock.MatchedBy(func(fuzzer entities.Fuzzer) bool {
			if fuzzer.ID != fuzzer1ID {
				return false
			}
			return isEqualConfs(config, fuzzer.Configuration)
		})).Once().Return(errors.Errorf("err"))
		recvMsgChan <- msg
		time.Sleep(70 * time.Millisecond)
	})

	tc1 := entities.Testcase{
		InputHash:  10,
		InputData:  []byte{1, 2, 3, 4, 5},
		Execs:      9,
		CorpusSize: 12,
		CreatedAt:  time.Now(),
	}
	tc2 := entities.Testcase{
		InputHash:  19,
		InputData:  []byte{1},
		Execs:      19,
		CorpusSize: 2,
		CreatedAt:  time.Now(),
	}
	evalData := []entities.EvaluatingData{
		{
			Cov:      [entities.CovSize]byte{},
			HasCrash: true,
		},
		{
			Cov: [entities.CovSize]byte{},
		},
	}

	t.Run("new testcase by ticker", func(t *testing.T) {
		msg := entities.FuzzerMessage{
			From: fuzzer1ID,
			Info: tc1,
		}
		evaler.On("Evaluate", []entities.Testcase{tc1, {}}, uint(1)).Once().
			Return(evalData[:1], nil)
		infoDB.On("AddTestcases", []entities.FuzzerID{fuzzer1ID}, []entities.Testcase{tc1}, evalData[:1]).Once()
		recvMsgChan <- msg
		time.Sleep(2 * time.Second)

	})
	t.Run("new testcase by len", func(t *testing.T) {
		msg1 := entities.FuzzerMessage{
			From: fuzzer1ID,
			Info: tc1,
		}
		msg2 := entities.FuzzerMessage{
			From: fuzzer1ID,
			Info: tc2,
		}
		evaler.On("Evaluate", []entities.Testcase{tc1, tc2}, uint(2)).Once().
			Return(evalData, nil)
		infoDB.On("AddTestcases", []entities.FuzzerID{fuzzer1ID, fuzzer1ID}, []entities.Testcase{tc1, tc2}, evalData).Once()
		recvMsgChan <- msg1
		recvMsgChan <- msg2
		time.Sleep(70 * time.Millisecond)
	})
	t.Run("with eval error", func(t *testing.T) {
		msg1 := entities.FuzzerMessage{
			From: fuzzer1ID,
			Info: tc1,
		}
		msg2 := entities.FuzzerMessage{
			From: fuzzer1ID,
			Info: tc2,
		}
		evaler.On("Evaluate", []entities.Testcase{tc1, tc2}, uint(2)).Once().
			Return(nil, errors.New("eval error"))
		evaler.On("Evaluate", []entities.Testcase{tc1, tc2}, uint(2)).Once().
			Return(evalData, nil)
		infoDB.On("AddTestcases", []entities.FuzzerID{fuzzer1ID, fuzzer1ID}, []entities.Testcase{tc1, tc2}, evalData).Once()
		recvMsgChan <- msg1
		recvMsgChan <- msg2
		time.Sleep(70 * time.Millisecond)
	})
	monitoring.Close()
	// смотрим чтобы не сломалось
	monitoring.Close()
}

func isEqualConfs(c1 entities.FuzzerConf, c2 entities.FuzzerConf) bool {
	if c1.IsConcolic != c2.IsConcolic {
		return false
	}
	if c1.IsHavoc != c2.IsHavoc {
		return false
	}
	if c1.ScheduleID != c2.ScheduleID {
		return false
	}
	if c1.ForkMode != c2.ForkMode {
		return false
	}
	if c1.MutatorID != c2.MutatorID {
		return false
	}
	if c1.PoolID != c2.PoolID {
		return false
	}
	return true
}
