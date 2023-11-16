package master

import (
	"fmt"
	"math"
	"orchestration/core/infodb"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"slices"

	"github.com/influxdata/tdigest"
)

type badFuzzer struct {
	norm    float64
	speed   float64
	id      entities.ElementID
	isNoice bool
}

func badFuzzerSetToString(bad map[entities.ElementID]badFuzzer) string {
	str := "bad fuzzer set:\n"
	for _, bf := range bad {
		str += fmt.Sprintf("id: %v, norm: %0.4f, speed: %0.4f, is noice: %v", bf.id, bf.norm, bf.speed, bf.isNoice)
	}
	return str
}

type advice struct {
	id      entities.ElementID
	why     string
	isNoice bool
}

func (m *Master) startAnalysis(analyze infodb.Analyze) *advice {
	var (
		// TODO: avg cluster to identify bad clusters
		norms  = tdigest.New()
		speeds = tdigest.New()
		bad    = make(map[entities.ElementID]badFuzzer, 0)
	)
	for _, norm := range analyze.Norms {
		norms.Add(norm.Norm(), 1)
		speeds.Add(norm.Speed(), 1)
	}

	for _, candidate := range analyze.ClusteringData.Noice {
		// шум больше всего непохож на всех, поэтому он интересный
		canidateNorm := analyze.Norms[candidate]
		if canidateNorm.Norm() < norms.Quantile(0.5) &&
			canidateNorm.Speed() < speeds.Quantile(0.5) {
			bad[candidate] = badFuzzer{
				norm:    canidateNorm.Norm(),
				speed:   canidateNorm.Speed(),
				id:      candidate,
				isNoice: true,
			}
		}
	}

	maxClusterSize := -1
	for _, cluster := range analyze.ClusteringData.Clusters {
		if len(cluster) > maxClusterSize {
			maxClusterSize = len(cluster)
		}
	}
	for _, cluster := range analyze.ClusteringData.Clusters {
		if len(cluster) != maxClusterSize {
			continue
		}
		var (
			minNorm   = math.MaxFloat64
			minNormID entities.ElementID
			speed     float64
		)
		for _, id := range cluster {
			norm := analyze.Norms[id]
			if norm.Norm() < minNorm && norm.Speed() < speeds.Quantile(0.66) {
				minNorm = norm.Norm()
				speed = norm.Speed()
				minNormID = id
			}
		}
		zeroID := entities.ElementID{}
		if minNormID == zeroID {
			continue
		}
		bad[minNormID] = badFuzzer{
			norm:  minNorm,
			speed: speed,
			id:    minNormID,
		}
	}

	logger.Infof("%v", analyze.ClusteringData)
	logger.Info(badFuzzerSetToString(bad))

	switch len(bad) {
	case 0:
		return nil
	case 1:
		for _, bf := range bad {
			return &advice{
				id:      bf.id,
				why:     "only one bad fuzzer",
				isNoice: bf.isNoice,
			}
		}
	}

	badUnique := detectByUniqueCoverage(bad, analyze)
	if len(badUnique) == 1 {
		bad := badUnique[0]
		return &advice{
			id:      bad.id,
			why:     "min unique edges",
			isNoice: bad.isNoice,
		}
	}
	return detectByNormAndSpeed(badUnique)
}

func detectByUniqueCoverage(bad map[entities.ElementID]badFuzzer, an infodb.Analyze) []badFuzzer {
	type uniqueBlocks struct {
		id    entities.ElementID
		count int
	}

	ubs := make([]uniqueBlocks, 0, len(bad))
	commonCoverage := make([]int, entities.CovSize)
	for id := range bad {
		cov := an.Coverages[id]
		for i, c := range cov {
			if c > 0 {
				commonCoverage[i]++
			}
		}
	}
	for id := range bad {
		ub := 0
		cov := an.Coverages[id]
		for i, c := range cov {
			if c < 1 {
				continue
			}
			if commonCoverage[i]-1 == 0 {
				ub++
			}
		}
		ubs = append(ubs, uniqueBlocks{
			id:    id,
			count: ub,
		})
	}

	logger.Infof("ubs: %+v", ubs)
	slices.SortFunc(ubs, func(a, b uniqueBlocks) int {
		return a.count - b.count
	})
	// если самый лучший не дает выигрыша, то кикаем его
	if ubs[len(ubs)-1].count == 0 {
		return nil
	}

	var (
		first  = true
		result []badFuzzer
	)
	for _, ub := range ubs {
		if first {
			first = false
			result = append(result, bad[ub.id])
			continue
		}
		if ub.count != 0 && !first {
			return result
		}
		result = append(result, bad[ub.id])
	}
	return result
}

func detectByNormAndSpeed(bad []badFuzzer) *advice {
	slices.SortFunc(bad, func(a, b badFuzzer) int {
		if a.id == b.id {
			return 0
		}
		if a.norm < b.norm {
			return -1
		}
		return 1
	})
	b := bad[0]
	return &advice{
		id:      b.id,
		why:     "worst norm",
		isNoice: b.isNoice,
	}
}
