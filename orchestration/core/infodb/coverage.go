package infodb

import (
	"math"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"sync"
	"time"

	"github.com/influxdata/tdigest"
	"github.com/okhowang/clusters"
	"github.com/pkg/errors"
)

// CovData - хранит данные по покрытию и расстанию различных конфигураций фаззеров
type CovData struct {
	mu sync.RWMutex
	// храним фаззера вместе с конфигурациями
	// сделано для того чтобы смотреть статистику именно по конфигурациям
	// но одинаковые конфигурации на разных машинах пока что разные тут фаззеры
	// тк они в теории могут давать достаточно разносторонее покрытие
	Fuzzers map[entities.ElementID]*fuzzer
}

func NewCovData() *CovData {
	return &CovData{
		Fuzzers: make(map[entities.ElementID]*fuzzer, 0),
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
			if fuzzer.Cov[j] == 0 {
				fuzzer.Cov[j] = 1
				fuzzer.TestcaseCount[j] = 1
			}
			// fuzzer.TestcaseCount[j]++
			// fuzzer.Cov[j] += uint32(tr)
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
}

type ClusteringData struct {
	Clusters map[int][]entities.ElementID
	Noice    []entities.ElementID
}

type Norm [2]float64

func (n Norm) Norm() float64 {
	return n[1]
}

func (n Norm) Speed() float64 {
	return math.Abs(n[1] - n[0])
}

type Analyze struct {
	// norms in sorted order (Asc)
	Norms map[entities.ElementID]Norm
	// data after clustering algorithm
	ClusteringData ClusteringData
	// coverages of all fuzzers
	Coverages map[entities.ElementID][]float64
}

var distFn = func(f1, f2 []float64) float64 {
	var dst float64
	for i := range f1 {
		diff := f1[i] - f2[i]
		dst += diff * diff
	}
	return math.Sqrt(dst)
}

// UpdateDistances обновляет дерево с расстояниями между фаззерами и так-же обновляет
// квадрат нормы для векторов покрытия
func (d *CovData) calculate() ([]entities.ElementID, [][]float64, map[entities.ElementID]Norm) {

	var (
		fuzzerIDs = make([]entities.ElementID, 0, len(d.Fuzzers))
		coverages = make([][]float64, 0, len(d.Fuzzers))
		norms     = make(map[entities.ElementID]Norm, len(d.Fuzzers))
	)

	for id, fuzzer := range d.Fuzzers {
		fuzzer.mu.Lock()
		var (
			cov    = make([]float64, entities.CovSize)
			sqNorm = float64(0)
		)
		for i := 0; i < entities.CovSize; i++ {
			cov[i] = float64(fuzzer.Cov[i]) / math.Max(float64(fuzzer.TestcaseCount[i]), 1)
			sqNorm += cov[i] * cov[i]
		}
		coverages = append(coverages, cov)
		fuzzerIDs = append(fuzzerIDs, id)
		norm := Norm{math.Sqrt(fuzzer.SqNorm), math.Sqrt(sqNorm)}
		norms[id] = norm
		fuzzer.SqNorm = sqNorm

		fuzzer.mu.Unlock()
	}

	return fuzzerIDs, coverages, norms
}

func (d *CovData) getClusters(fuzzerIDs []entities.ElementID, fuzzerCovMat [][]float64, eps float64) (ClusteringData, error) {
	dbscan, err := clusters.DBSCAN(2, eps, 4, distFn)
	if err != nil {
		return ClusteringData{}, err
	}
	err = dbscan.Learn(fuzzerCovMat)
	if err != nil {
		return ClusteringData{}, err
	}

	clusters := dbscan.Guesses()
	res := ClusteringData{
		Clusters: make(map[int][]entities.ElementID, len(clusters)),
	}
	for fuzzInd, cluster := range clusters {
		if cluster != -1 {
			res.Clusters[cluster] = append(res.Clusters[cluster], fuzzerIDs[fuzzInd])
			continue
		}
		res.Noice = append(res.Noice, fuzzerIDs[fuzzInd])
	}
	return res, nil
}

func (d *CovData) CreateAnalyze() (Analyze, error) {
	t := time.Now()
	defer func() {
		analyzeCreationTime.Observe(float64(time.Since(t).Milliseconds()))
	}()
	d.mu.RLock()
	defer d.mu.RUnlock()

	fuzzerIDs, coverages, norms := d.calculate()
	td := tdigest.New()
	for i := 0; i < len(coverages); i++ {
		for j := i; j < len(coverages); j++ {
			td.Add(distFn(coverages[i], coverages[j]), 1)
		}
	}
	if len(coverages) == 0 {
		return Analyze{}, nil
	}

	clusteringData, err := d.getClusters(fuzzerIDs, coverages, td.Quantile(0.4))
	if err != nil {
		return Analyze{}, err
	}
	an := Analyze{
		ClusteringData: clusteringData,
		Norms:          norms,
		Coverages:      make(map[entities.ElementID][]float64, len(coverages)),
	}
	for i, cov := range coverages {
		an.Coverages[fuzzerIDs[i]] = cov
	}

	return an, nil
}
