package infodb

import (
	"math"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"sort"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/exp/rand"
	"gonum.org/v1/gonum/spatial/vptree"
)

// CovData - хранит данные по покрытию и расстанию различных конфигураций фаззеров
type CovData struct {
	mu sync.RWMutex
	// храним фаззера вместе с конфигурациями
	// сделано для того чтобы смотреть статистику именно по конфигурациям
	// но одинаковые конфигурации на разных машинах пока что разные тут фаззеры
	// тк они в теории могут давать достаточно разносторонее покрытие
	Fuzzers map[entities.ElementID]*fuzzer
	// необходимо быстро находить ближайшего по расстоянию покрытия соседа
	// дерево позволяющее делать быстрые запросы на ближайших соседей
	Vptree *vptree.Tree
	// время последнего обновления (в том числе и создания)
	LastUpdate time.Time
}

func NewCovData() *CovData {
	return &CovData{
		Fuzzers:    make(map[entities.ElementID]*fuzzer, 0),
		LastUpdate: time.Now(),
	}
}

// Под бдшным мьютексом
func (d *CovData) AddFuzzer(f Fuzzer) {
	d.mu.Lock()
	d.Fuzzers[f.ID] = &fuzzer{
		Cov:           [entities.CovSize]uint32{},
		TestcaseCount: [entities.CovSize]uint{},
		mu:            &sync.Mutex{},
	}
	d.mu.Unlock()
}

func (d *CovData) DeleteFuzzer(id entities.ElementID) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, exists := d.Fuzzers[id]
	if !exists {
		return errors.Errorf("fuzzer %d doesn't exists", id)
	}
	delete(d.Fuzzers, id)
	return nil
}

func (d *CovData) AddTestcaseCoverage(fuzzerID entities.ElementID, config entities.FuzzerConf, cov entities.Coverage) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	fuzzer, exists := d.Fuzzers[fuzzerID]
	if !exists {
		logger.ErrorMessage("not found fuzzer with id=%v and conf=%v", fuzzerID, config)
		return
	}

	fuzzer.mu.Lock()
	for j, tr := range cov {
		if tr != 0 {
			// кажется из-за наложенния и циклов идея
			// хотя бы не невалиданая
			fuzzer.TestcaseCount[j]++
			fuzzer.Cov[j] += uint32(tr)
		}
	}
	fuzzer.mu.Unlock()

	logger.Debugf("added coverage for fuzzerID %v and conf %v", fuzzerID, config)
}

type fuzzer struct {
	mu *sync.Mutex
	// оригинальное покрытие за весь процесс фаззинга
	Cov [entities.CovSize]uint32
	// кол-во тест-кейсов фаззера на каждый элемент бранч
	// для того чтобы уровнять дистанию от много создающих фаззеров
	// так же для того чтобы редкое покрытие вносило больший импакт
	TestcaseCount [entities.CovSize]uint
	// квадрат длины вектора покрытия, нельзя сравнивать расстояние между фаззерами
	// если один фаззер не будет никуда двигаться
	// поэтому стоит отсекать такие моменты с помощью расстояние от начала координат
	// ну и в целом оно дает представление о том что фаззер стоит на месте и никуда не двигается
	SqNorm float64
	// вспомогательная структура для хранения фаззера в дереве
	// внутри нее покрытие не за все время, а за переод до ее создания
	vpFuzzer *vpfuzzer
}

// vpfuzzer - вспомогательная структура, для подсчета расстояний на основе дерева
type vpfuzzer struct {
	ID       entities.ElementID
	Coverage [entities.CovSize]float64
}

// Distance считает расстояние до c
func (p *vpfuzzer) Distance(c vptree.Comparable) float64 {
	var (
		q    = c.(*vpfuzzer)
		dist float64
	)
	for k, p := range p.Coverage {
		diff := p - q.Coverage[k]
		dist += diff * diff
	}
	return math.Sqrt(dist)
}

// UpdateDistances обновляет дерево с расстояниями между фаззерами и так-же обновляет
// квадрат нормы для векторов покрытия
func (d *CovData) updateDistances() (err error) {

	// приводим покрытие в правильный формат, тк исходный формат не совсем валидный для анализа
	vpFuzzers := make([]vptree.Comparable, 0, len(d.Fuzzers))
	for id, fuzzer := range d.Fuzzers {
		fuzzer.mu.Lock()
		var (
			cov    = [entities.CovSize]float64{}
			sqNorm = float64(0)
		)

		for i := 0; i < entities.CovSize; i++ {
			cov[i] = float64(fuzzer.Cov[i]) / math.Max(float64(fuzzer.TestcaseCount[i]), 1)
			sqNorm += cov[i] * cov[i]
		}
		vp := vpfuzzer{
			ID:       id,
			Coverage: cov,
		}
		fuzzer.vpFuzzer = &vp
		fuzzer.SqNorm = sqNorm

		vpFuzzers = append(vpFuzzers, &vp)
		fuzzer.mu.Unlock()
	}
	d.Vptree, err = vptree.New(vpFuzzers, len(d.Fuzzers), rand.NewSource(uint64(time.Now().Unix())))
	d.LastUpdate = time.Now()

	if err != nil {
		return errors.Wrap(err, "failed to build vptree")
	}
	return nil
}

type Distance struct {
	A    entities.ElementID
	B    entities.ElementID
	Dist float64
}

type Norm struct {
	Norm float64
	ID   entities.ElementID
}

// getNearestDistances - функция пока что для отладки
func (d *CovData) getNearestDistances(fuzzerID entities.ElementID, topN int, buf []Distance) {
	fuzzer := d.Fuzzers[fuzzerID]
	// +1 тк там еще сама точка будет учавствовать
	keeper := vptree.NewNKeeper(topN + 1)
	d.Vptree.NearestSet(keeper, fuzzer.vpFuzzer)

	k := 0
	for _, dst := range keeper.Heap {
		b := dst.Comparable.(*vpfuzzer).ID
		buf[k] = Distance{
			A:    fuzzerID,
			B:    b,
			Dist: dst.Dist,
		}
		if b == fuzzerID {
			continue
		}
		k++
	}
}

type Analyze struct {
	// distances in sorted order (Asc)
	Distances []Distance
	// norms in sorted order (Asc)
	Norms []Norm
}

func (d *CovData) CreateAnalyze(topN int) (Analyze, error) {
	t := time.Now()
	defer func() {
		analyzeCreationTime.Observe(float64(time.Since(t).Milliseconds()))
	}()
	d.mu.RLock()

	err := d.updateDistances()
	if err != nil {
		d.mu.RUnlock()
		return Analyze{}, err
	}

	if topN > len(d.Fuzzers)-1 {
		topN = len(d.Fuzzers) - 1
	}
	if topN <= 0 {
		d.mu.RUnlock()
		return Analyze{}, nil
	}

	an := Analyze{
		Norms:     make([]Norm, 0, len(d.Fuzzers)),
		Distances: make([]Distance, 0, len(d.Fuzzers)*topN),
	}
	type pair struct {
		A entities.ElementID
		B entities.ElementID
	}
	distSet := make(map[pair]struct{}, len(d.Fuzzers)*topN)
	buf := make([]Distance, topN)
	for id, f := range d.Fuzzers {
		an.Norms = append(an.Norms, Norm{
			ID:   id,
			Norm: math.Sqrt(f.SqNorm),
		})
		d.getNearestDistances(id, topN, buf)
		for _, dst := range buf {
			dst.A, dst.B = min(dst.A, dst.B), max(dst.A, dst.B)
			p := pair{
				A: dst.A,
				B: dst.B,
			}
			if _, exists := distSet[p]; exists {
				continue
			}
			distSet[p] = struct{}{}
			an.Distances = append(an.Distances, dst)
		}
	}
	d.mu.RUnlock()

	// кажется оч быстро тк фазеров не оч много
	sort.Slice(an.Norms, func(i, j int) bool {
		return an.Norms[i].Norm < an.Norms[j].Norm
	})
	// кажется оч быстро тк фазеров не оч много
	sort.Slice(an.Distances, func(i, j int) bool {
		return an.Distances[i].Dist < an.Distances[j].Dist
	})

	return an, nil
}

func max(a, b entities.ElementID) entities.ElementID {
	if a.NodeID > b.NodeID {
		return a
	}
	if b.NodeID > a.NodeID {
		return b
	}
	if a.OnNodeID > b.OnNodeID {
		return a
	}
	return b
}

func min(a, b entities.ElementID) entities.ElementID {
	if a.NodeID < b.NodeID {
		return a
	}
	if b.NodeID < a.NodeID {
		return b
	}
	if a.OnNodeID < b.OnNodeID {
		return a
	}
	return b
}
