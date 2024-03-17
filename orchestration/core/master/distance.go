package master

import (
	"orchestration/core/infodb"
	"orchestration/entities"
)

/*
храним для каждого анализа его скор
его нужно каждый рас пересчитывать, но не полностью
а с коэфициентом по времени (чем старше, тем более на него пофиг)\

а как считать коэфициенты???

*/

type cycle[T any] struct {
	buf    []T
	maxLen int
	prt    int
}

func (c *cycle[T]) put(a T) {
	if c.maxLen == len(c.buf) {
		c.buf[c.prt] = a
	} else {
		c.buf = append(c.buf, a)
	}
	c.prt++
}

func (c *cycle[T]) getByInd(i int) T {
	return c.buf[(c.prt+i)%len(c.buf)]
}

func (c *cycle[T]) len() int { return len(c.buf) }

type scores map[entities.ElementID]float64

func (s scores) agregate(new scores) {
	for el, sc := range new {
		s[el] += sc
	}
}

func (s scores) delete(old scores) {
	for el, sc := range old {
		s[el] -= sc
	}
}

type AnalyzeReuslts struct {
	prevScores cycle[scores]
	score      scores
}

func (m *Master) startAnalysis(analyze infodb.Analyze) {
	var (
		curMap = make(map[entities.ElementID]float64, len(analyze.Norms))
		score  scores
	)

	for _, norm := range analyze.Norms {
		curMap[norm.ID] = norm.Norm
	}
	for _, dist := range analyze.Distances {

	}
}

// func (m *Master) processDist(
// 	dist infodb.Distance,
// 	norms [2]float64,
// 	analyze infodb.Analyze,
// ) []entities.ElementID {
// 	if norms[0] == 0 {

// 	}
// }

// func (m *Master) processZeroNorm(id entities.ElementID) bool {

// 	for i := 0; i < m.AnalRes.analCycle.len(); i++ {

// 	}
// }
