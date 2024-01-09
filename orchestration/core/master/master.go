package master

import (
	"context"
	"fmt"
	"orchestration/core/infodb"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"sync"
	"time"
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
	NewNode
	NodeDeleted
	NewElement
	ElementDeleted
	NeedMoreEvalers
	NeedMoreFuzzers

	// время в течении которого игнорируем все
	// ивенты о перебалансировки, тк они кажется пойдут лавиной
	RebalaceThreshold = time.Minute
	EventChanBuf      = 512
)

func (e NodeEvent) String() string {
	switch e {
	case NewNode:
		return "NewNode"
	case NodeDeleted:
		return "NodeDeleted"
	case NewElement:
		return "NewElement"
	case ElementDeleted:
		return "ElementDeleted"
	case NeedMoreEvalers:
		return "NeedMoreEvalers"
	case NeedMoreFuzzers:
		return "NeedMoreFuzzers"
	}
	return "Unknown"
}

type (
	Payload interface{}
	Event   struct {
		Element  entities.ElementID
		NodeType entities.ElementType
		Event    NodeEvent

		Payload Payload
	}

	NodeSetUp struct {
		Event    NodeEvent
		Elements []Element
		// в случае фаззера нужно же конфигурацию кинуть
		Confugurations []entities.FuzzerConf
	}
)

func (e Event) String() string {
	return fmt.Sprintf("{event=%s, nodeType=%v, element=%v}", e.Event, e.NodeType, e.Element)
}

type Node struct {
	id       entities.NodeID
	elements []Element
}

type Element struct {
	OnNodeID entities.OnNodeID
	Type     entities.ElementType
}

type fuzzInfoDB interface {
	AddTestcases(tcList []entities.Testcase, evalDataList []entities.EvaluatingData)
	AddFuzzer(fuzzer infodb.Fuzzer) error
}

type Master struct {
	ctx context.Context

	mu        sync.Mutex
	eventChan chan Event
	nodeList  []*Node

	db           fuzzInfoDB
	testcaseChan chan entities.Testcase

	lastRebalanceTime time.Time
	fuzzers           []entities.ElementID
	evalers           []entities.ElementID
}

func NewMaster(ctx context.Context, db fuzzInfoDB) *Master {
	master := &Master{
		ctx:       ctx,
		db:        db,
		eventChan: make(chan Event, EventChanBuf),
		nodeList:  make([]*Node, 0),
		evalers:   make([]entities.ElementID, 0),
		fuzzers:   make([]entities.ElementID, 0),
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-master.eventChan:
				logger.Infof("got new event: %v", event)
			}
		}
	}()

	return master
}

func (m *Master) GetEventChan() chan<- Event {
	return m.eventChan
}

// спрашиваем что поднять, обновляем уже после подъема???
func (m *Master) SetupNewNode(nodeID entities.NodeID, cores int64) NodeSetUp {
	m.mu.Lock()
	defer m.mu.Unlock()

	logger.Infof("new node: %v with %d cores", nodeID, cores)

	node := &Node{
		id:       nodeID,
		elements: make([]Element, 0, cores),
	}
	m.nodeList = append(m.nodeList, node)

	if cores == 0 {
		cores = 1
	}
	nodesCreated := 0

	if len(m.evalers) == 0 {
		evaler := Element{
			OnNodeID: entities.OnNodeID(time.Now().Unix()),
			Type:     entities.Evaler,
		}
		m.evalers = append(m.evalers, entities.ElementID{
			NodeID:   nodeID,
			OnNodeID: evaler.OnNodeID,
		})
		node.elements = append(node.elements, evaler)
		nodesCreated++
	}

	//TODO: сохранить количество ядер
	// TODO: some hard hard logic here
	// тут выдовать конфигурации тоже можно
	// можно смело фаззер добавлять, если что добалансим??

	m.lastRebalanceTime = time.Now()

	return NodeSetUp{
		Event:    NewElement,
		Elements: node.elements,
	}
}

func (m *Master) newFuzzer(e Event) {
	if err := m.db.AddFuzzer(infodb.Fuzzer{
		ID:            e.Element,
		Configuration: e.Payload.(entities.FuzzerConf),
		Registered:    time.Now(),
		Testcases:     make(map[uint64]struct{}, 0),
	}); err != nil {
		logger.Errorf(err, "failed to add new fuzzer: %v", e.Element)
	}
	logger.Infof("ADDED NEW FUZZER: %v", e.Element)
}

func (m *Master) StartEvaler(evaler evalClnt) {
	w := newWorker(m.testcaseChan, m.eventChan, m.db, evaler)
	go w.Run(m.ctx)
	logger.Infof("started new evaler: %v", evaler)
}

func (m *Master) Ctx() context.Context {
	return m.ctx
}
