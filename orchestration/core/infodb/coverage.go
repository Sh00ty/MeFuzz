package infodb

import (
	"math"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/exp/rand"
	"gonum.org/v1/gonum/spatial/vptree"
)

const (
	// сколько ближайших точек находить
	nearestDataNeeded = 2
)

// CovData - хранит данные по покрытию и расстанию различных конфигураций фаззеров
type CovData struct {
	mu sync.RWMutex
	// храним фаззера вместе с конфигурациями
	// сделано для того чтобы смотреть статистику именно по конфигурациям
	// но одинаковые конфигурации на разных машинах пока что разные тут фаззеры
	// тк они в теории могут давать достаточно разносторонее покрытие
	Fuzzers map[fuzzConfKey]*Fuzzer
	// необходимо быстро находить ближайшего по расстоянию покрытия соседа
	// дерево позволяющее делать быстрые запросы на ближайших соседей
	Vptree *vptree.Tree
	// время последнего обновления (в том числе и создания)
	LastUpdate time.Time
}

// fuzzConfKey - однозначно идентифицирует фаззер в расчете на длительный период
// (у фаззера может поменяться конфигурация)
type fuzzConfKey struct {
	FuzzerID entities.FuzzerID
	Config   entities.FuzzerConf
}

func NewCovData() *CovData {
	return &CovData{
		Fuzzers:    make(map[fuzzConfKey]*Fuzzer, 0),
		LastUpdate: time.Now(),
	}
}

// Под бдшным мьютексом
func (d *CovData) AddFuzzer(fuzzer entities.Fuzzer) {
	d.Fuzzers[fuzzConfKey{
		FuzzerID: fuzzer.ID,
		Config:   fuzzer.Configuration,
	}] = &Fuzzer{
		Cov:           [entities.CovSize]float64{},
		TestcaseCount: [entities.CovSize]uint{},
		mu:            &sync.Mutex{},
	}
}

// Под бдшным мьютексом
func (d *CovData) ChangeFuzzerConfig(fuzzerID entities.FuzzerID, conf entities.FuzzerConf) {
	key := fuzzConfKey{
		FuzzerID: fuzzerID,
		Config:   conf,
	}

	if _, exists := d.Fuzzers[key]; exists {
		return
	}
	d.Fuzzers[key] = &Fuzzer{
		Cov:           [entities.CovSize]float64{},
		TestcaseCount: [entities.CovSize]uint{},
		mu:            &sync.Mutex{},
	}
}

func (d *CovData) AddTestcaseCoverage(fuzzerID entities.FuzzerID, config entities.FuzzerConf, cov entities.Coverage) {
	key := fuzzConfKey{
		FuzzerID: fuzzerID,
		Config:   config,
	}
	fuzzer, exists := d.Fuzzers[key]
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
			fuzzer.Cov[j] += float64(tr)
		}
	}
	fuzzer.mu.Unlock()

	logger.Debugf("added coverage for fuzzerID %v and conf %v", fuzzerID, config)
}

type Fuzzer struct {
	mu *sync.Mutex
	// оригинальное покрытие за весь процесс фаззинга
	Cov [entities.CovSize]float64
	// кол-во тест-кейсов фаззера на каждый элемент бранч
	// для того чтобы уровнять дистанию от много создающих фаззеров
	// так же для того чтобы редкое покрытие вносило больший импакт
	TestcaseCount [entities.CovSize]uint
	// квадрат длины вектора покрытия, нельзя сравнивать расстояние между фаззерами
	// если один фаззер не будет никуда двигаться
	// поэтому стоит отсекать такие моменты с помощью расстояние от начала координат
	// ну и в целом оно дает представление о том что фаззер стоит на месте и никуда не двигается
	SqNorm float64
	// Хранит значения SqNorm которое было в прошлой итерации, таким образом можно оценивать
	// на сколько все это дело сдвинулось
	PrevSqNorm float64
	// вспомогательная структура для хранения фаззера в дереве
	// внутри нее покрытие не за все время, а за переод до ее создания
	vpFuzzer *vpfuzzer
}

// vpfuzzer - вспомогательная структура, для подсчета расстояний на основе дерева
type vpfuzzer struct {
	Key      fuzzConfKey
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
func (d *CovData) UpdateDistances() (err error) {
	d.mu.Lock()
	// приводим покрытие в правильный формат, тк исходный формат не совсем валидный для анализа
	vpFuzzers := make([]vptree.Comparable, 0, len(d.Fuzzers))
	for key, fuzzer := range d.Fuzzers {
		var (
			cov    = [entities.CovSize]float64{}
			sqNorm = float64(0)
		)
		fuzzer.mu.Lock()
		for i := 0; i < entities.CovSize; i++ {
			cov[i] = fuzzer.Cov[i] / math.Max(float64(fuzzer.TestcaseCount[i]), 1)
			sqNorm += cov[i] * cov[i]
		}
		vp := vpfuzzer{
			Key:      key,
			Coverage: cov,
		}
		fuzzer.vpFuzzer = &vp
		fuzzer.PrevSqNorm = fuzzer.SqNorm
		fuzzer.SqNorm = sqNorm

		fuzzer.mu.Unlock()

		d.Fuzzers[key] = fuzzer
		vpFuzzers = append(vpFuzzers, &vp)
	}
	d.Vptree, err = vptree.New(vpFuzzers, len(d.Fuzzers), rand.NewSource(uint64(time.Now().Unix())))
	d.LastUpdate = time.Now()
	d.mu.Unlock()

	if err != nil {
		return errors.Wrap(err, "failed to build vptree")
	}
	return nil
}

// GetNearestDistances - функция пока что для отладки
func (d *CovData) GetNearestDistances() {
	for key, fuzzer := range d.Fuzzers {
		logger.Debugf("Norm for %v = %f", key.FuzzerID, math.Sqrt(fuzzer.SqNorm))

		keeper := vptree.NewNKeeper(nearestDataNeeded + 1)
		d.Vptree.NearestSet(keeper, fuzzer.vpFuzzer)
		for i, res := range keeper.Heap {
			r := res.Comparable.(*vpfuzzer)
			logger.Debugf("the closest [%d] for %v is %v; dist=%f", i, key.FuzzerID, r.Key.FuzzerID, res.Dist)
		}
	}
}
