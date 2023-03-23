package evaluating

import (
	"math/rand"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"time"
)

type runtimeEvaler interface {
	Evaluate(testCases entities.Testcase) (entities.EvaluatingData, error)
}

type nodeBalancer interface {
	GiveNode(nodeType entities.NodeType) entities.NodeID
}

type infoDB interface {
	NewGeneralCov(cov entities.Coverage) uint
}

type evaluator struct {
	re       runtimeEvaler
	balancer nodeBalancer
	db       infoDB
}

func New(db infoDB) *evaluator {
	return &evaluator{
		db: db,
	}
}

func (e *evaluator) Evaluate(testCases []entities.Testcase, len uint) ([]entities.EvaluatingData, error) {
	logger.ErrorMessage("unimplemented!!!")
	res := make([]entities.EvaluatingData, len)
	r := rand.New(rand.NewSource(time.Now().Unix()))
	for i := uint(0); i < len; i++ {
		for j := 0; j < entities.CovSize; j++ {
			res[i].Cov[j] = byte(r.Int31n(2) * r.Int31n(2) * r.Int31n(2) * r.Int31n(2) * r.Int31n(2) * r.Int31n(2))
		}
		newCov := e.db.NewGeneralCov(res[i].Cov)
		res[i].NewCov = newCov
	}
	return res, nil
}
