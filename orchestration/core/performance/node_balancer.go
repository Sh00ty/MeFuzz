package performance

import (
	"context"
	"math"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"sync/atomic"
	"time"
)

/*
давайте введем следующие метрики:
	1) Кол-во тест-кейсов которое сейчас оценивается
	2) Если тикер в мониторинге срабатывает, то можно увеличивать кол-во фаззеров
(по-другому это значит, что оценщики простаивают тк проходит много времени в которое они не занимаются отправкой)

1 метрика помогает идентифицировать момент, когда оценщики перестают справляться с тест-кейсами фаззеров
2 метрика говорит о том, что оценщики простаивают без каких либо тест-кейсов

TODO: получается, что кол-во мониторов ограничивает кол-во оценщиков, что делать?
*/

type NodeEvent uint8

const (
	Unknown NodeEvent = iota
	New
	Deleted

	OldEvalerThreshold = time.Minute
	LatencyTimeout     = time.Minute

	MaxEvalSeedCount = 10_000
)

type Event struct {
	NodeID   entities.NodeID
	NodeType entities.NodeType
	Event    NodeEvent
}

type evalNodeLoad struct {
	NodeID         entities.NodeID
	seedsInProcess atomic.Int32
}

// сделать в конекшенах канал с событиями подписки и отписки нод
// чтобы их тут ...
type nodeBalancer struct {
	eventChan chan NodeEvent
	// храним сколько в данный момент времени в оценщиках обрабатывается
	seedsInEvalProgress atomic.Int32
}

func NewNodeBalancer(ctx context.Context, eventChanBuf uint) *nodeBalancer {
	balancer := &nodeBalancer{
		eventChan: make(chan NodeEvent, eventChanBuf),
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-balancer.eventChan:
				switch event {
				case New:
				case Deleted:
				}
			}
		}
	}()

	return balancer
}

func (b *nodeBalancer) GetEventChan() chan<- NodeEvent {
	return b.eventChan
}

func (b *nodeBalancer) AddEvalNode() error {
	return nil
}

func (b *nodeBalancer) GiveEvalNode(seedCount int32) (entities.NodeID, func()) {
	min := int32(math.MaxInt)
	ind := 0
	for i, load := range b.evalNodeLoad {
		if load.seedsInProcess.Load() < min {
			min = load.seedsInProcess.Load()
			ind = i
		}
	}
	nodeLoad := b.evalNodeLoad[ind]
	nodeLoad.seedsInProcess.Add(seedCount)
	b.seedsInEvalProgress.Add(seedCount)

	if b.seedsInEvalProgress.Load() >= MaxEvalSeedCount {
		if err := b.AddEvalNode(); err != nil {
			logger.Errorf(err, "failed to make rebalance and add eval node")
		}
	}
	return nodeLoad.NodeID, func() {
		b.seedsInEvalProgress.Add(-seedCount)
	}
}
