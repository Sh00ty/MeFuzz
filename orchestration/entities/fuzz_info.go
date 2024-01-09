package entities

import (
	"math"
	"time"
)

type (
	OnNodeID    uint16
	NodeID      uint32
	ElementType int8
)

const (
	Undefined ElementType = iota
	// с брокером хотя бы один фаззер точно поднимается
	Broker
	Fuzzer
	Evaler
)

type Flags uint8

const (
	Compressed Flags = 1 << iota
	_
	Master
	NewTestCase
	_
	Configuration
	Evaluation
)

const (
	covSizePow2 = 16
	CovSize     = 1 << covSizePow2
)

var (
	MasterFuzzerID = ElementID{
		NodeID:   0,
		OnNodeID: math.MaxUint16,
	}
)

func (f Flags) Has(flag Flags) bool {
	return flag == f&flag
}

func (f Flags) Add(flag Flags) Flags {
	return f | flag
}

type ElementID struct {
	NodeID   NodeID
	OnNodeID OnNodeID
}

type Testcase struct {
	ID         uint64
	FuzzerID   ElementID
	InputData  []byte
	Execs      uint32
	CorpusSize uint32
	CreatedAt  time.Time
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

type EvaluatingData struct {
	Cov      Coverage
	HasCrash bool
	NewCov   uint
}

type Coverage [CovSize]byte
