package entities

import (
	"math"
	"time"
)

type ClientID uint
type NodeID uint64
type NodeType int8

const (
	Undefined NodeType = iota
	Broker
	Evaler
)

type FuzzInfoKind uint8

const (
	Compressed FuzzInfoKind = 1 << iota
	_
	Master
	NewTestCase
	_
	Configuration
)

const (
	covSizePow2 = 16
	CovSize     = 1 << covSizePow2
)

var (
	MasterFuzzerID = FuzzerID{
		ClientID: math.MaxInt16,
		NodeID:   math.MaxInt16,
	}
)

func (f FuzzInfoKind) Has(flag FuzzInfoKind) bool {
	return flag == f&flag
}

type FuzzerID struct {
	ClientID ClientID
	NodeID   NodeID
}

type Fuzzer struct {
	ID            FuzzerID
	Configuration FuzzerConf
	BugsFound     uint
	Registered    time.Time
	Testcases     map[uint64]struct{}
}

type FuzzerMessage struct {
	From FuzzerID
	Info FuzzerInformation
}

type FuzzerInformation interface {
	Kind() FuzzInfoKind
}

type Testcase struct {
	InputData  []byte
	InputHash  uint64
	Execs      uint32
	CorpusSize uint32
	CreatedAt  time.Time
}

func (Testcase) Kind() FuzzInfoKind {
	return NewTestCase
}

type (
	MutatorID  string
	ScheduleID string
	// PoolID - алгоритмы хранения тест-кейсов
	PoolID string
)

type FuzzerConf struct {
	MutatorID  MutatorID
	ScheduleID ScheduleID
	PoolID     PoolID
	IsHavoc    bool
	IsConcolic bool
	ForkMode   bool
}

func (FuzzerConf) Kind() FuzzInfoKind {
	return Configuration
}

type EvaluatingData struct {
	Cov      Coverage
	HasCrash bool
	NewCov   uint
}

type Coverage [CovSize]byte

func (cov Coverage) Add(newCov []int16) {
	minLen := 0
	if len(cov) < len(newCov) {
		minLen = len(cov)
	} else {
		minLen = len(newCov)
	}
	for i := 0; i < minLen; i++ {
		cov[i] += byte(newCov[i])
	}
}
