package master

import (
	"context"
	"fmt"
	"orchestration/core/infodb"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
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
	id              entities.NodeID
	elements        []Element
	cores           int64
	fuzzerByAllTime uint32
}

type Element struct {
	OnNodeID entities.OnNodeID
	Type     entities.ElementType
}

type fuzzInfoDB interface {
	AddTestcases(tcList []entities.Testcase, evalDataList []entities.EvaluatingData)
	AddFuzzer(fuzzer infodb.Fuzzer) error
	GetCovData() (map[entities.ElementID]infodb.Fuzzer, *infodb.CovData)
}

type Master struct {
	ctx context.Context

	db           fuzzInfoDB
	testcaseChan chan entities.Testcase
	eventChan    chan Event

	mu                sync.Mutex
	nodes             map[entities.NodeID]*Node
	lastRebalanceTime time.Time
	fuzzers           map[entities.ElementID]struct{}
	evalers           map[entities.ElementID]struct{}
}

func NewMaster(ctx context.Context, db fuzzInfoDB) *Master {
	master := &Master{
		ctx:          ctx,
		db:           db,
		eventChan:    make(chan Event, EventChanBuf),
		testcaseChan: make(chan entities.Testcase),
		nodes:        make(map[entities.NodeID]*Node, 0),
		evalers:      make(map[entities.ElementID]struct{}, 0),
		fuzzers:      make(map[entities.ElementID]struct{}, 0),
	}

	go func() {
		for {
			select {
			case <-master.ctx.Done():
				return
			case event := <-master.eventChan:
				logger.Infof("got new event: %v", event)
				switch event.Event {
				case ElementDeleted:
					master.elementDeleted(event)
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-master.ctx.Done():
				return
			case <-time.After(30 * time.Second):
				_, cov := master.db.GetCovData()
				err := cov.UpdateDistances()
				if err != nil {
					logger.Errorf(err, "failed to update distances")
				}
				cov.GetNearestDistances()
			}
		}
	}()

	return master
}

func (m *Master) GetEventChan() chan<- Event {
	return m.eventChan
}

func (m *Master) GetTestcaseChan() chan entities.Testcase {
	return m.testcaseChan
}

func (m *Master) elementDeleted(e Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch e.NodeType {
	case entities.Evaler:
		delete(m.evalers, e.Element)
		node, exists := m.nodes[e.Element.NodeID]
		if !exists {
			logger.Infof("node %v does not exist, can't delete its element", e.Element)
		}
		slices.DeleteFunc(node.elements, func(el Element) bool {
			return el.OnNodeID == e.Element.OnNodeID && el.Type == e.NodeType
		})
		logger.Infof("deleted evaler %v", e.Element)
	}
}

var (
	ErrStopElement = errors.New("element stopped")
	ErrMaxElements = errors.New("riched maximum number of elements")
)

// спрашиваем что поднять, обновляем уже после подъема???
func (m *Master) SetupNewNode(nodeID entities.NodeID, cores int64) (NodeSetUp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	logger.Infof("new node: %v with %d cores", nodeID, cores)

	node, exists := m.nodes[nodeID]
	if !exists {
		if cores == 0 {
			cores = 1
		}

		node = &Node{
			id:       nodeID,
			elements: make([]Element, 0, cores),
			cores:    cores,
		}
		m.nodes[nodeID] = node
	} else {
		if node.cores == int64(len(node.elements)) {
			return NodeSetUp{}, errors.Wrapf(ErrMaxElements, "on node %d", nodeID)
		}
	}

	newElements := make([]Element, 0, cores)

	if len(m.evalers) == 0 {
		evaler := Element{
			OnNodeID: entities.OnNodeID(uuid.New().ID()),
			Type:     entities.Evaler,
		}
		m.evalers[entities.ElementID{
			NodeID:   nodeID,
			OnNodeID: evaler.OnNodeID,
		}] = struct{}{}
		newElements = append(newElements, evaler)
	}

	// experement
	if len(newElements) == 0 {
		fuzzer := Element{
			OnNodeID: entities.OnNodeID(node.fuzzerByAllTime + 1),
			Type:     entities.Fuzzer,
		}
		node.fuzzerByAllTime++
		m.fuzzers[entities.ElementID{
			NodeID:   nodeID,
			OnNodeID: fuzzer.OnNodeID,
		}] = struct{}{}
		newElements = append(newElements, fuzzer)
		m.newFuzzer(entities.ElementID{
			NodeID:   nodeID,
			OnNodeID: fuzzer.OnNodeID,
		}, entities.FuzzerConf{
			MutatorID: "mut1",
			ForkMode:  true,
		})
	}
	//TODO: сохранить количество ядер
	// TODO: some hard hard logic here
	// тут выдовать конфигурации тоже можно
	// можно смело фаззер добавлять, если что добалансим??

	m.lastRebalanceTime = time.Now()
	node.elements = append(node.elements, newElements...)

	return NodeSetUp{
		Event:    NewElement,
		Elements: newElements,
	}, nil
}

func (m *Master) newFuzzer(id entities.ElementID, conf entities.FuzzerConf) {
	if err := m.db.AddFuzzer(infodb.Fuzzer{
		ID:            id,
		Configuration: conf,
		Registered:    time.Now(),
		Testcases:     make(map[uint64]struct{}, 0),
	}); err != nil {
		logger.Errorf(err, "failed to add new fuzzer: %v", id)
	}
	logger.Infof("ADDED NEW FUZZER: %v", id)
}

func (m *Master) StartEvaler(evaler evalClnt) {
	w := newWorker(m.testcaseChan, m.eventChan, m.db, evaler)
	go w.Run(m.ctx)
	logger.Infof("started new evaler: %v", evaler)
}

func (m *Master) Ctx() context.Context {
	return m.ctx
}
