package tcp

import (
	"orchestration/core/master"
	"orchestration/entities"
	"orchestration/infra/utils/logger"
	"orchestration/infra/utils/msgpack"

	"github.com/pkg/errors"
)

type Evaler struct {
	conn                 MultiplexedConnection
	onNodeID             entities.OnNodeID
	performanceEventChan chan<- master.Event
}

func NewEvaler(
	conn MultiplexedConnection,
	onNodeID entities.OnNodeID,
	performanceEventChan chan<- master.Event,
) *Evaler {
	return &Evaler{
		conn:                 conn,
		onNodeID:             onNodeID,
		performanceEventChan: performanceEventChan,
	}
}

func (e *Evaler) Evaluate(testCases []entities.Testcase) ([]entities.EvaluatingData, error) {
	evalTestcases := make([][]int, len(testCases))
	for i := range testCases {
		evalTestcases[i] = msgpack.CovertTo[byte, int](testCases[i].InputData)
	}

	in := evalIn{Testcases: evalTestcases}
	if err := e.conn.Send(e.onNodeID, Evaluation, in); err != nil {
		if errors.Is(err, ErrConnectionClosed) {
			return nil, master.ErrStopElement
		}
		return nil, errors.Wrap(err, "failed to send eval input message")
	}

	out := evaluationOutput{}
	if err := e.conn.Recv(&out); err != nil {
		if errors.Is(err, ErrConnectionClosed) {
			logger.Infof("evaler connection on conn %v closed", e.conn)
			return nil, master.ErrStopElement
		}
		return nil, errors.Wrapf(err, "failed to recv output message")
	}

	res := make([]entities.EvaluatingData, len(out.EvalData))
	for i := range out.EvalData {
		for j := 0; j < entities.CovSize; j++ {
			res[i].Cov[j] = byte(out.EvalData[i].Coverage[j])
		}
		res[i].HasCrash = out.EvalData[i].ExecInfo != 1
	}
	return res, nil
}

func (e *Evaler) GetElementID() entities.ElementID {
	return entities.ElementID{
		NodeID:   e.conn.conn.NodeID,
		OnNodeID: e.onNodeID,
	}
}

func (e *Evaler) Stop() {
	e.conn.Close()
}
