package entities

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type (
	OnNodeID    uint32
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
	Compressed    Flags = 0x1
	Master        Flags = 0x4
	NewTestCase   Flags = 0x8
	Evaluation    Flags = 0x16
	Configuration Flags = 0x32
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

func (e ElementID) String() string {
	return fmt.Sprintf("%d-%d", e.NodeID, e.OnNodeID)
}

type Testcase struct {
	ID         uint64
	FuzzerID   ElementID
	InputData  []byte
	Execs      uint64
	CorpusSize uint64
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

func (e EvaluatingData) String() string {
	s := strings.Builder{}
	s.WriteString("\nCov: [ ")
	for i, c := range e.Cov {
		if c != 0 {
			s.WriteString(fmt.Sprintf("%d:%d ", i, c))
		}
	}
	s.WriteString(fmt.Sprintf("]\nHasCrash=%t\n", e.HasCrash))
	return s.String()
}

type Coverage [CovSize]byte
