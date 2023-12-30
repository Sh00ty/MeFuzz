package infodb

import (
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"sync"

	"github.com/pkg/errors"
)

type fuzzInfoDB struct {
	// сохраняем тест-кейсы для общей картины
	seedPool *seedPool
	// общее покрытие собранное тест-кейсами пришедшими в мастер
	generalCoverage [entities.CovSize]bool
	generalCovMu    *sync.Mutex

	mu *sync.RWMutex
	// все зарегистрированные фаззеры
	fuzzerMap map[entities.FuzzerID]entities.Fuzzer
	// фаззеры плохо проявиших себя в предыдущих раундах
	toChangeConf map[entities.FuzzerID]struct{}
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
		fuzzerMap:    make(map[entities.FuzzerID]entities.Fuzzer, 0),
		toChangeConf: make(map[entities.FuzzerID]struct{}),
		covData:      NewCovData(),
	}, nil
}

// ChangeConfig - сохраняет новый конфиг для фаззера
func (db *fuzzInfoDB) ChangeConfig(fuzzerID entities.FuzzerID, conf entities.FuzzerConf) error {
	db.mu.Lock()
	fuzzer := db.fuzzerMap[fuzzerID]
	fuzzer.Configuration = conf
	db.fuzzerMap[fuzzerID] = fuzzer
	db.covData.ChangeFuzzerConfig(fuzzerID, conf)
	db.mu.Unlock()
	logger.Debugf("changed fuzzer %v config to %v", fuzzerID, conf)
	return nil
}

// AddFuzzer - добавляет новый фаззер и начинает собирать по нему информацию
func (db *fuzzInfoDB) AddFuzzer(f entities.Fuzzer) error {
	db.mu.Lock()
	if _, exists := db.fuzzerMap[f.ID]; exists {
		return errors.Errorf("fuzzer with id=%v already exists", f.ID)
	}
	db.covData.AddFuzzer(f)
	db.fuzzerMap[f.ID] = f
	db.mu.Unlock()
	return nil
}

// AddTestcases - сохраняет список тест-кейсов за раз
func (db *fuzzInfoDB) AddTestcases(tcList []entities.Testcase, evalDataList []entities.EvaluatingData) {
	db.mu.Lock()
	defer db.mu.Unlock()

	for i := 0; i < len(tcList); i++ {
		fuzzer, exists := db.fuzzerMap[tcList[i].FuzzerID]
		if !exists {
			logger.ErrorMessage("unknown fuzzer=%v of testcase, skip it", tcList[i].FuzzerID)
			continue
		}

		if _, exists := fuzzer.Testcases[tcList[i].ID]; exists {
			logger.Debugf("test case with hash %d already exists", tcList[i].ID)
			continue
		}

		db.covData.AddTestcaseCoverage(fuzzer.ID, fuzzer.Configuration, evalDataList[i].Cov)

		if !evalDataList[i].HasCrash && evalDataList[i].NewCov == 0 {
			logger.Debug("useless testcase")
			continue
		}
		if evalDataList[i].HasCrash {
			fuzzer.BugsFound++
		}
		fuzzer.Testcases[tcList[i].ID] = struct{}{}

		db.fuzzerMap[tcList[i].FuzzerID] = fuzzer

		if err := db.seedPool.AddSeed(tcList[i], evalDataList[i]); err != nil {
			logger.Errorf(err, "failed to add testcase to pool: %v", tcList[i])
		}
	}
}

// DeleteFuzzer - удаляет фаззер
func (db *fuzzInfoDB) DeleteFuzzer(id entities.FuzzerID) error {
	db.mu.Lock()
	if _, exists := db.fuzzerMap[id]; !exists {
		db.mu.Unlock()
		return errors.Errorf("fuzzer with id=%v doesn't exist", id)
	}
	delete(db.fuzzerMap, id)
	delete(db.toChangeConf, id)
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

// GetCovData - возращает данные необходимые для проведения PCA (НЕ ПОТОКОБЕЗОПАСНО для fuzzer map)
func (db *fuzzInfoDB) GetCovData() (map[entities.FuzzerID]entities.Fuzzer, *CovData) {
	return db.fuzzerMap, db.covData
}
