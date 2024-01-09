package tcp

import (
	"fmt"
)

type TcpMasterMessage struct {
	Payload  []int  `msgpack:"payload"`
	ClientID uint32 `msgpack:"client_id"`
	Flags    uint32 `msgpack:"flags"`
}

func (TcpMasterMessage) Name() string {
	return "TcpMasterMessage"
}

func (t TcpMasterMessage) String() string {
	return fmt.Sprintf("{ClientID=%d Flags=%d len(Payload)=%d", t.ClientID, t.Flags, len(t.Payload))
}

type cores struct {
	Cores int64 `msgpack:"cores,as_array"`
}

func (cores) Name() string {
	return "cores"
}

// evaler messages

type evaluationOutput struct {
	EvalData []evaluationData `msgpack:"eval_data,as_array"`
}

type evaluationData struct {
	ExecInfo uint64 `msgpack:"exec_info,as_array"`
	Coverage []int  `msgpack:"coverage,as_array"`
}

type evalIn struct {
	Testcases [][]int `msgpack:"testcases,as_array"`
}

func (input) Name() string {
	return "testcases"
}

// fuzzer messages

type newTestcase struct {
	Input        input     `msgpack:"input"`
	ObserversBug *[]int    `msgpack:",omitempty"`
	ExitKind     string    `msgpack:"exit_kind"`
	ClientConfig string    `msgpack:"client_config"`
	Timestamp    timestamp `msgpack:"time"`
	Executions   uint32    `msgpack:"executions"`
	CorpusSize   uint32    `msgpack:"corpus_size"`
}

type input struct {
	Input []int
}

type timestamp struct {
	Secs uint64 `msgpack:"secs"`
	Nsec uint64 `msgpack:"nsec"`
}
