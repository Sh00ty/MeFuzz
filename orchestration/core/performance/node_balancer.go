package performance

import (
	"orchestration/entities"
	"orchestration/infra/utils/logger"
)

type nodeBalancer struct {
}

func (nodeBalancer) GiveNode(nodeType entities.NodeType) entities.NodeID {
	logger.ErrorMessage("unimplemented give node")
	return 0
}
