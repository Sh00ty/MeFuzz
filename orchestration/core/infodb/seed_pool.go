package infodb

import (
	"encoding/binary"
	"fmt"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/emirpasic/gods/trees/redblacktree"
	"github.com/pkg/errors"
)

const (
	// bloom ~ 2mb
	seedCountExpected      = 10e6
	bloomFalsePositiveRate = 10e-4 // 0.1%

	maxBestTcs = 500
)

type seed struct {
	ID        uint64
	NewCov    uint
	CreatedAt time.Time
	FuzzerID  entities.ElementID
	Crash     bool
}

func seedCmp(ai interface{}, bi interface{}) int {
	a := ai.(seed)
	b := bi.(seed)
	if a.ID == b.ID {
		return 0
	}

	if a.Crash && !b.Crash {
		return -1
	}
	if !a.Crash && b.Crash {
		return 1
	}

	if a.NewCov != b.NewCov {
		return int(b.NewCov) - int(a.NewCov)
	}
	if a.CreatedAt.Before(b.CreatedAt) {
		return 1
	}
	return -1
}

type seedPool struct {
	seeds     *redblacktree.Tree
	bloom     *bloom.BloomFilter
	bloomBuf  []byte
	corpusDir string
}

func NewSeedPool(corpusDirName string, initialSeeds []entities.Testcase, evalDataList []entities.EvaluatingData) (*seedPool, error) {
	sp := &seedPool{
		corpusDir: corpusDirName,
		seeds:     redblacktree.NewWith(seedCmp),
		bloom:     bloom.NewWithEstimates(seedCountExpected, bloomFalsePositiveRate),
		bloomBuf:  make([]byte, 8),
	}

	for i, initialSeed := range initialSeeds {
		if err := sp.AddSeed(initialSeed, evalDataList[i]); err != nil {
			logger.Errorf(err, "failed to add initial seed %d", initialSeed.ID)
		}
	}
	return sp, nil
}

func (sp *seedPool) AddFuzzer(id entities.ElementID) error {
	return os.Mkdir(path.Join(sp.corpusDir, fmt.Sprintf("%d_%d", id.NodeID, id.OnNodeID)), 0777)
}

func (sp *seedPool) addToBloom(id uint64) {
	binary.BigEndian.PutUint64(sp.bloomBuf, id)
	sp.bloom = sp.bloom.Add(sp.bloomBuf)
}

func (sp *seedPool) HasInBloom(id uint64) bool {
	binary.BigEndian.PutUint64(sp.bloomBuf, id)
	return sp.bloom.Test(sp.bloomBuf)
}

func (sp *seedPool) AddSeed(tc entities.Testcase, evalData entities.EvaluatingData) error {
	if err := os.WriteFile(
		path.Join(
			sp.corpusDir,
			fmt.Sprintf("%d_%d", tc.FuzzerID.NodeID, tc.FuzzerID.OnNodeID),
			fmt.Sprintf("%d_%d", time.Now().Unix(), tc.ID),
		),
		tc.InputData,
		os.ModePerm,
	); err != nil {
		return errors.Wrap(err, "failed to save seed input data to filepath")
	}
	// тк входные данные уже на диске то нет необходимости в них в оперативной памяти
	tc.InputData = nil
	sp.addToBloom(tc.ID)

	if sp.seeds.Size() > maxBestTcs {
		logger.ErrorMessage("too many seeds in red black tree: %d", sp.seeds.Size())
	}

	sp.seeds.Put(seed{
		ID:        tc.ID,
		Crash:     evalData.HasCrash,
		NewCov:    evalData.NewCov,
		CreatedAt: tc.CreatedAt,
	}, tc)
	return nil
}

func (sp *seedPool) GetMostInterestingIDs(count uint) (res []entities.Testcase) {
	it := sp.seeds.Iterator()
	it.End()
	for i := uint(0); i < count; i++ {
		if moved := it.Prev(); moved {
			tc := it.Value().(entities.Testcase)
			input, err := os.ReadFile(path.Join(sp.corpusDir, strconv.FormatUint(tc.ID, 10)))
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
	it := sp.seeds.Iterator()
	it.Begin()

	found := false
	for it.Next() {
		tc := it.Value().(entities.Testcase)
		if tc.ID == id {
			res = tc
			found = true
		}
	}
	if !found {
		return entities.Testcase{}, errors.Errorf("not found seed %d", id)
	}

	input, err := os.ReadFile(path.Join(sp.corpusDir, strconv.FormatUint(res.ID, 10)))
	if err != nil {
		return entities.Testcase{}, errors.Errorf("failed to get testcase input=%v; err=%v", res, err)
	}
	res.InputData = input
	return res, nil
}
