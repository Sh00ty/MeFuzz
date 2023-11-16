package tcp

import (
	"fmt"
)

type TcpMasterMessage struct {
	ClientID uint32 `msgpack:"client_id,as_array"`
	Flags    uint32 `msgpack:"flags,as_array"`
	Payload  []int  `msgpack:"payload,as_array"`
}

func (TcpMasterMessage) Name() string {
	return "TcpMasterMessage"
}

func (t TcpMasterMessage) String() string {
	return fmt.Sprintf("{ClientID=%d Flags=%d len(Payload)=%d", t.ClientID, t.Flags, len(t.Payload))
}

// init messages

type initMsg struct {
	Cores      int64  `msgpack:"cores,as_array"`
	ManualRole string `msgpack:"manual_role,as_array"`
}

func (initMsg) Name() string {
	return "init_msg"
}

type nodeConfiguration struct {
	Elements []element `msgpack:"elements,as_array"`
}

func (nodeConfiguration) Name() string {
	return "node_configuration"
}

type element struct {
	OnNodeID            uint32               `msgpack:"on_node_id,as_array"`
	Kind                int64                `msgpack:"type,as_array"`
	FuzzerConfiguration *fuzzerConfiguration `msgpack:"fuzzer_configuration,omitempty"`
}

func (element) Name() string {
	return "element"
}

type fuzzerConfiguration struct {
	MutatorID   string `msgpack:"mutator_id"`
	SchedulerID string `msgpack:"scheduler_id"`
}

func (fuzzerConfiguration) Name() string {
	return "fuzzer_configuration"
}

// evaler messages

type evaluationOutput struct {
	EvalData []evaluationData `msgpack:"eval_data,as_array"`
}

func (evaluationOutput) Name() string {
	return "evaluation_output"
}

type evaluationData struct {
	ExecInfo uint64 `msgpack:"exec_info,as_array"`
	Coverage []int  `msgpack:"coverage,as_array"`
}

type evalIn struct {
	Testcases [][]int `msgpack:"testcases,as_array"`
}

func (input) Name() string {
	return "input"
}

// fuzzer messages

type newTestcase struct {
	Input        input     `msgpack:"input"`
	ObserversBug *[]int    `msgpack:",omitempty"`
	ExitKind     string    `msgpack:"exit_kind"`
	CorpusSize   uint32    `msgpack:"corpus_size"`
	ClientConfig string    `msgpack:"client_config"`
	Timestamp    timestamp `msgpack:"time"`
	Executions   uint32    `msgpack:"executions"`
}

func (newTestcase) Name() string {
	return "NewTestcase"
}

type input struct {
	Input []int
}

type timestamp struct {
	Secs uint64 `msgpack:"secs"`
	Nsec uint64 `msgpack:"nsec"`
}
