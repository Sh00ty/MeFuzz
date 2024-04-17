package master

import (
	"context"
	"fmt"
	"math"
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
введем следующие метрики:
	1) Кол-во тест-кейсов которое сейчас оценивается
	2) Если тикер в мониторинге срабатывает, то можно увеличивать кол-во фаззеров
(по-другому это значит, что оценщики простаивают тк проходит много времени в которое они не занимаются отправкой)

1 метрика помогает идентифицировать момент, когда оценщики перестают справляться с тест-кейсами фаззеров
2 метрика говорит о том, что оценщики простаивают без каких либо тест-кейсов
*/

type NodeEvent uint8

//go:generate stringer -type NodeEvent
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
	EventChanBuf      = 256
	TestcaseChanBuf   = 1024

	analyzeInterval = time.Minute
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
	CreateAnalyze() (infodb.Analyze, error)
}

type worker interface {
	Stop()
}

type noopWorker struct{}

func (noopWorker) Stop() {}

type Master struct {
	ctx context.Context

	db           fuzzInfoDB
	testcaseChan chan entities.Testcase
	eventChan    chan Event

	isTest             bool
	mu                 sync.Mutex
	nodes              map[entities.NodeID]*Node
	lastRebalanceTime  time.Time
	lastRebalanceEvent Event
	lastAddedNode      entities.NodeID
	fuzzers            map[entities.ElementID]worker
	evalers            map[entities.ElementID]worker
}

func NewMaster(ctx context.Context, db fuzzInfoDB) *Master {
	master := &Master{
		ctx:          ctx,
		db:           db,
		isTest:       true,
		eventChan:    make(chan Event, EventChanBuf),
		testcaseChan: make(chan entities.Testcase, TestcaseChanBuf),
		nodes:        make(map[entities.NodeID]*Node, 0),
		evalers:      make(map[entities.ElementID]worker, 0),
		fuzzers:      make(map[entities.ElementID]worker, 0),
	}

	go func() {
		for {
			select {
			case <-master.ctx.Done():
				return
			case event := <-master.eventChan:
				logger.Infof("got new event: %v", event)

				masterEventCount.WithLabelValues(
					event.Event.String(),
					event.Element.String(),
				).Inc()

				switch event.Event {
				case ElementDeleted:
					master.elementDeleted(event)
				}
			}
		}
	}()

	go func() {
		analTicker := time.NewTicker(analyzeInterval)
		defer analTicker.Stop()
		for {
			select {
			case <-master.ctx.Done():
				return
			case <-analTicker.C:
				an, err := master.db.CreateAnalyze()
				if err != nil {
					logger.Error(err)
				}
				advice := master.startAnalysis(an)
				if advice == nil {
					continue
				}
				logger.ErrorMessage("analysis advice is %+v", advice)
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

	node, exists := m.nodes[e.Element.NodeID]
	if !exists {
		logger.Infof("node %v does not exist, can't delete its element", e.Element)
	}
	node.elements = slices.DeleteFunc(node.elements, func(el Element) bool {
		return el.OnNodeID == e.Element.OnNodeID && el.Type == e.NodeType
	})

	if e.NodeType == entities.Evaler {
		delete(m.evalers, e.Element)
		totalEvalerCount.Dec()
		logger.Infof("deleted evaler %v", e.Element)
		return
	}

	delete(m.fuzzers, e.Element)
	totalFuzzerCount.Dec()
	logger.Infof("deleted fuzzer %v", e.Element)

}

var (
	ErrStopElement = errors.New("element stopped")
	ErrMaxElements = errors.New("riched maximum number of elements")
)

// спрашиваем что поднять, обновляем уже после подъема???
func (m *Master) SetupNewNode(nodeID entities.NodeID, cores int64, manualRole string) (NodeSetUp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cores == 0 {
		cores = 1
	}

	logger.Debugf("node %v with cores %d and role %s want to connect", nodeID, cores, manualRole)

	node, exists := m.nodes[nodeID]
	if !exists {
		node = &Node{
			id:       nodeID,
			elements: make([]Element, 0, cores),
			cores:    cores,
		}
		m.nodes[nodeID] = node

		logger.Infof("new node: %v with %d cores", nodeID, cores)
	}
	// if node.cores == int64(len(node.elements)) {
	// 	return NodeSetUp{}, errors.Wrapf(ErrMaxElements, "on node %d", nodeID)
	// }
	lastElementInd := len(node.elements)

	defer func() {
		m.lastRebalanceTime = time.Now()
	}()

	switch manualRole {
	case "fuzz":
		m.newFuzzer(node, entities.FuzzerConf{
			MutatorID: "mut1",
			ForkMode:  true,
		})
		return NodeSetUp{
			Event:    NewElement,
			Elements: node.elements[lastElementInd:],
		}, nil
	case "eval":
		m.newEvaler(node)
		return NodeSetUp{
			Event:    NewElement,
			Elements: node.elements[lastElementInd:],
		}, nil
	}

	switch {
	case len(m.evalers) == 0:
		m.newEvaler(node)
	case len(m.testcaseChan) > int(math.Ceil(0.90*TestcaseChanBuf)):
		m.newEvaler(node)
	}
	if !m.isTest {
		for i := int64(len(node.elements)); i < cores; i++ {
			m.newFuzzer(node, entities.FuzzerConf{
				MutatorID: "mut1",
				ForkMode:  true,
			})
		}
	} else {
		if lastElementInd != 0 {
			m.newFuzzer(node, entities.FuzzerConf{
				MutatorID: "mut1",
				ForkMode:  true,
			})
		}
	}
	// TODO: some hard hard logic here
	// тут выдовать конфигурации тоже можно
	// можно смело фаззер добавлять, если что добалансим??

	return NodeSetUp{
		Event:    NewElement,
		Elements: node.elements[lastElementInd:],
	}, nil
}

func (m *Master) newEvaler(node *Node) {
	evaler := Element{
		OnNodeID: entities.OnNodeID(uuid.New().ID()),
		Type:     entities.Evaler,
	}
	m.evalers[entities.ElementID{
		NodeID:   node.id,
		OnNodeID: evaler.OnNodeID,
	}] = &noopWorker{}
	node.elements = append(node.elements, evaler)

	logger.Infof("ADDED NEW EVALER TO DB")
}

func (m *Master) newFuzzer(node *Node, conf entities.FuzzerConf) {
	fuzzer := Element{
		OnNodeID: entities.OnNodeID(node.fuzzerByAllTime + 1),
		Type:     entities.Fuzzer,
	}
	node.fuzzerByAllTime++
	id := entities.ElementID{
		NodeID:   node.id,
		OnNodeID: fuzzer.OnNodeID,
	}
	m.fuzzers[id] = &noopWorker{}
	node.elements = append(node.elements, fuzzer)

	if err := m.db.AddFuzzer(infodb.Fuzzer{
		ID:            id,
		Configuration: conf,
		Registered:    time.Now(),
	}); err != nil {
		logger.Errorf(err, "failed to add new fuzzer: %v", id)
	}
	logger.Infof("ADDED NEW FUZZER TO DB: %v", id)
}

func (m *Master) StartEvaler(evaler evalClnt) {
	e := newEvaler(m.testcaseChan, m.eventChan, m.db, evaler)

	m.mu.Lock()
	m.evalers[evaler.GetElementID()] = e
	m.mu.Unlock()

	go e.Run(m.ctx)

	totalEvalerCount.Inc()
	logger.Infof("started new evaler: %v", evaler)
}

func (m *Master) StartFuzzer(fuzzer fuzzClnt) {
	f := newFuzzer(m.testcaseChan, m.eventChan, fuzzer)

	m.mu.Lock()
	m.evalers[fuzzer.GetElementID()] = f
	m.mu.Unlock()

	go f.Run(m.ctx)

	totalFuzzerCount.Inc()
	logger.Infof("started new fuzzer: %v", fuzzer)
}

func (m *Master) StopFuzzer(id entities.ElementID) {

}

func (m *Master) Ctx() context.Context {
	return m.ctx
}
