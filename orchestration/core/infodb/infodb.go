package infodb

import (
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

type Fuzzer struct {
	ID                     entities.ElementID
	Configuration          entities.FuzzerConf
	BugsFound              uint
	TotalSaveTestcaseCount uint
	Registered             time.Time
}

type fuzzInfoDB struct {
	// сохраняем тест-кейсы для общей картины
	seedPool *seedPool
	// общее покрытие собранное тест-кейсами пришедшими в мастер
	generalCoverage [entities.CovSize]bool
	generalCovMu    *sync.Mutex

	mu *sync.RWMutex
	// все зарегистрированные фаззеры
	fuzzerMap map[entities.ElementID]Fuzzer
	// информация необходимая для оценки схожести покрытий
	covData *CovData
}

// New - создает базу данных с динамической информацией для фаззеров
func New(corpusDirName string) (*fuzzInfoDB, error) {
	pool, err := NewSeedPool(corpusDirName, nil, nil)
	if err != nil {
		return nil, err
	}
	return &fuzzInfoDB{
		mu:           &sync.RWMutex{},
		generalCovMu: &sync.Mutex{},
		seedPool:     pool,
		fuzzerMap:    make(map[entities.ElementID]Fuzzer, 0),
		covData:      NewCovData(),
	}, nil
}

// AddFuzzer - добавляет новый фаззер и начинает собирать по нему информацию
func (db *fuzzInfoDB) AddFuzzer(f Fuzzer) error {
	db.mu.Lock()
	if _, exists := db.fuzzerMap[f.ID]; exists {
		return errors.Errorf("fuzzer with id=%v already exists", f.ID)
	}
	db.covData.AddFuzzer(f)
	db.fuzzerMap[f.ID] = f
	db.seedPool.AddFuzzer(f.ID)
	db.mu.Unlock()
	return nil
}

// AddTestcases - сохраняет список тест-кейсов за раз
func (db *fuzzInfoDB) AddTestcases(tcList []entities.Testcase, evalDataList []entities.EvaluatingData) {
	timer := prometheus.NewTimer(evaluationTime)
	db.mu.Lock()

	defer func() {
		db.mu.Unlock()
		timer.ObserveDuration()
	}()

	for i := 0; i < len(tcList); i++ {
		fuzzer, exists := db.fuzzerMap[tcList[i].FuzzerID]
		if !exists {
			logger.ErrorMessage("unknown fuzzer=%v of testcase, skip it", tcList[i].FuzzerID)
			continue
		}

		db.covData.AddTestcaseCoverage(fuzzer.ID, fuzzer.Configuration, evalDataList[i].Cov)

		if db.seedPool.HasInBloom(tcList[i].ID) {
			logger.Infof("test case %d already exists", tcList[i].ID)
			continue
		}

		evalDataList[i].NewCov = db.NewGeneralCov(evalDataList[i].Cov)
		if !evalDataList[i].HasCrash && evalDataList[i].NewCov == 0 {
			logger.Infof("useless testcase from %v", tcList[i].FuzzerID)
			continue
		}

		var (
			nodeID   = strconv.FormatUint(uint64(tcList[i].FuzzerID.NodeID), 10)
			onNodeID = strconv.FormatUint(uint64(tcList[i].FuzzerID.OnNodeID), 10)
		)

		fuzzer.TotalSaveTestcaseCount++

		if evalDataList[i].HasCrash {
			fuzzer.BugsFound++

			crashCount.WithLabelValues(nodeID, onNodeID).Inc()
		}

		db.fuzzerMap[tcList[i].FuzzerID] = fuzzer

		if err := db.seedPool.AddSeed(tcList[i], evalDataList[i]); err != nil {
			logger.Errorf(err, "failed to add testcase to pool: %v", tcList[i])
			continue
		}
		savedTestcaseCount.WithLabelValues(nodeID, onNodeID).Inc()
	}
}

// DeleteFuzzer - удаляет фаззер
func (db *fuzzInfoDB) DeleteFuzzer(id entities.ElementID) error {
	db.mu.Lock()
	if _, exists := db.fuzzerMap[id]; !exists {
		db.mu.Unlock()
		return errors.Errorf("fuzzer with id=%v doesn't exist", id)
	}
	delete(db.fuzzerMap, id)
	db.covData.DeleteFuzzer(id)
	db.mu.Unlock()
	return nil
}

// NewGeneralCov - добавляет покрытие собранное оценщиком и возвращает кол-во новых бранчей
func (db *fuzzInfoDB) NewGeneralCov(cov entities.Coverage) uint {
	db.generalCovMu.Lock()
	res := uint(0)
	for i := 0; i < entities.CovSize; i++ {
		if cov[i] != 0 {
			if db.generalCoverage[i] {
				continue
			}
			db.generalCoverage[i] = true
			res++
		}
	}
	db.generalCovMu.Unlock()
	return res
}

func (db *fuzzInfoDB) CreateAnalyze() (Analyze, error) {
	return db.covData.CreateAnalyze()
}
