package performance

import (
	"context"
	"orchestration/entities"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/exp/slices"
)

/*
давайте введем следующие метрики:
	1) Кол-во тест-кейсов которое сейчас оценивается
	2) Если тикер в мониторинге срабатывает, то можно увеличивать кол-во фаззеров
(по-другому это значит, что оценщики простаивают тк проходит много времени в которое они не занимаются отправкой)

1 метрика помогает идентифицировать момент, когда оценщики перестают справляться с тест-кейсами фаззеров
2 метрика говорит о том, что оценщики простаивают без каких либо тест-кейсов
*/

type NodeEvent uint8

const (
	Unknown NodeEvent = iota
	New
	Deleted

	OldEvalerThreshold = time.Minute
	LatencyTimeout     = time.Minute
	// TODO: подобрать оптимальное
	MaxBurstEvalSeedCount = 250
	EvalSeedLimit         = 500
)

type Event struct {
	NodeID   entities.NodeID
	NodeType entities.NodeType
	Event    NodeEvent
}

type node struct {
	lim    *Limiter
	nodeID entities.NodeID
}

// сделать в конекшенах канал с событиями подписки и отписки нод
// чтобы их тут ...
type nodeBalancer struct {
	eventChan chan Event
	mu        sync.RWMutex
	ptr       atomic.Int64
	evalNodes []node
}

func NewNodeBalancer(ctx context.Context, eventChanBuf uint) *nodeBalancer {
	balancer := &nodeBalancer{
		eventChan: make(chan Event, eventChanBuf),
		evalNodes: make([]node, 0),
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-balancer.eventChan:
				switch event.NodeType {
				case entities.Evaler:
					balancer.evalNodeEvent(event)
				}
			}
		}
	}()

	return balancer
}

func (b *nodeBalancer) evalNodeEvent(e Event) {
	b.mu.Lock()
	switch e.Event {
	case New:
		b.evalNodes = append(b.evalNodes, node{
			nodeID: entities.NodeID(e.NodeID),
			lim:    NewLimiter(EvalSeedLimit, MaxBurstEvalSeedCount),
		})
	case Deleted:
		slices.DeleteFunc(b.evalNodes, func(n node) bool {
			return n.nodeID == e.NodeID
		})
	}
	b.mu.Unlock()
}

func (b *nodeBalancer) GetEventChan() chan<- Event {
	return b.eventChan
}

func (b *nodeBalancer) GetEvalNode(seedCount int32) (entities.NodeID, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for i := 0; i < len(b.evalNodes); i++ {
		ptr := b.ptr.Add(1) % int64(len(b.evalNodes))
		if b.evalNodes[ptr].lim.AllowN(time.Now(), int(seedCount)) {
			return b.evalNodes[ptr].nodeID, true
		}
	}
	return 0, false
}
