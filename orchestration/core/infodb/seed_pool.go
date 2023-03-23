package infodb

import (
	"github.com/emirpasic/gods/trees/redblacktree"
	"github.com/pkg/errors"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"os"
	"path"
	"strconv"
	"time"
)

type seed struct {
	Crash     bool
	NewCov    uint
	InputHash uint64
	CreatedAt time.Time
	FuzzerID  entities.FuzzerID
}

func fromTestcase(tc entities.Testcase) seed {
	return seed{
		CreatedAt: tc.CreatedAt,
		InputHash: tc.InputHash,
	}
}

func seedCmp(ai interface{}, bi interface{}) int {
	a := ai.(seed)
	b := bi.(seed)
	if a.InputHash == b.InputHash {
		return 0
	}
	if a.Crash || b.Crash == true {
		if a.Crash && !b.Crash {
			return -1
		} else if !a.Crash && b.Crash {
			return 1
		}
	}
	if a.NewCov != b.NewCov {
		return int(b.NewCov) - int(a.NewCov)
	}
	if before := a.CreatedAt.Before(b.CreatedAt); before {
		return 1
	}
	return -1
}

type seedPool struct {
	inputMap  map[string]os.FileInfo
	seeds     *redblacktree.Tree
	corpusDir string
}

func NewSeedPool(corpusDirName string, initialSeeds []entities.Testcase, evalDataList []entities.EvaluatingData) (*seedPool, error) {
	if err := os.RemoveAll(corpusDirName); err != nil {
		return nil, errors.Errorf("please remove previous %s dir or give me permission on it", corpusDirName)
	}
	if err := os.Mkdir(corpusDirName, os.ModePerm); err != nil {
		return nil, errors.Wrapf(err, "failed to create corpus dir=%s", corpusDirName)
	}
	sp := &seedPool{
		corpusDir: corpusDirName,
		inputMap:  make(map[string]os.FileInfo, 0),
		seeds:     redblacktree.NewWith(seedCmp),
	}

	for i, initialSeed := range initialSeeds {
		if err := sp.AddSeed(entities.MasterFuzzerID, initialSeed, evalDataList[i]); err != nil {
			logger.Errorf(err, "failed to add initial seed=%d", initialSeed.InputHash)
		}
	}
	return sp, nil
}

func (sp *seedPool) AddSeed(fuzzerID entities.FuzzerID, tc entities.Testcase, evalData entities.EvaluatingData) error {
	if err := os.WriteFile(
		path.Join(sp.corpusDir, strconv.FormatUint(tc.InputHash, 10)),
		tc.InputData,
		os.ModePerm,
	); err != nil {
		return errors.Wrap(err, "failed to save seed input data to filepath")
	}
	// тк входные данные уже на диске то нет необходимости в них в оперативной памяти
	tc.InputData = nil
	sp.seeds.Put(seed{
		Crash:     evalData.HasCrash,
		NewCov:    evalData.NewCov,
		CreatedAt: tc.CreatedAt,
		InputHash: tc.InputHash,
	}, tc)
	return nil
}

func (sp *seedPool) GetMostInterestingIDs(count uint) (res []entities.Testcase) {
	it := sp.seeds.Iterator()
	it.End()
	for i := uint(0); i < count; i++ {
		if moved := it.Prev(); moved {
			tc := it.Value().(entities.Testcase)
			input, err := os.ReadFile(path.Join(sp.corpusDir, strconv.FormatUint(tc.InputHash, 10)))
			if err != nil {
				logger.Errorf(err, "failed to get testcase=%v", tc)
				continue
			}
			tc.InputData = input
			res = append(res, tc)
		}
	}
	return res
}

func (sp *seedPool) GetByID(id uint64) (res entities.Testcase, err error) {
	val, found := sp.seeds.Get(seed{
		InputHash: id,
	})
	if !found {
		return entities.Testcase{}, errors.Errorf("not found testcase with id=%d", id)
	}
	tc := val.(entities.Testcase)
	input, err := os.ReadFile(path.Join(sp.corpusDir, strconv.FormatUint(tc.InputHash, 10)))
	if err != nil {
		return entities.Testcase{}, errors.Errorf("failed to get testcase input=%v; err=%v", tc, err)
	}
	tc.InputData = input
	return tc, nil
}
