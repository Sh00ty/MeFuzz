package tcp

import (
	"fmt"
	"orchestration/entities"
	"time"
)

type tcpMasterMessage struct {
	ClientID uint32 `msgpack:"client_id"`
	Flags    uint32 `msgpack:"flags"`
	Payload  []int  `msgpack:"payload"`
}

func (tcpMasterMessage) Name() string {
	return "TcpMasterMessage"
}

func (t tcpMasterMessage) String() string {
	return fmt.Sprintf("{ClientID=%d Flags=%d len(Payload)=%d", t.ClientID, t.Flags, len(t.Payload))
}

type masterNodeHello struct {
	MasterHostname string `msgpack:"master_hostname"`
	BrokerID       uint32 `msgpack:"broker_id"`
	SendLimit      uint32 `msgpack:"send_limit"`
	RecvLimit      uint32 `msgpack:"recv_limit"`
}

func (masterNodeHello) Name() string {
	return "MasterNodeHello"
}

type shmemID struct {
	ID [20]int `msgpack:"id"`
}

type brokerShMemDescription struct {
	Size uint32  `msgpack:"size"`
	ID   shmemID `msgpack:"id"`
}

type brokerConnectHello struct {
	BrokerShmemDescription brokerShMemDescription `msgpack:"broker_shmem_description"`
	Hostname               string                 `msgpack:"hostname"`
}

func (brokerConnectHello) Name() string {
	return "BrokerConnectHello"
}

type remoteBrokerAccepted struct {
	BrokerID uint32 `msgpack:"broker_id"`
}

func (remoteBrokerAccepted) Name() string {
	return "RemoteBrokerAccepted"
}

// BrokerAcceptedByMaster Notify broker has been accepted by master
type brokerAcceptedByMaster struct {
	// harnes we would like to fuzz
	HarnesHash string `msgpack:"harnes_hash"`
}

func (brokerAcceptedByMaster) Name() string {
	return "BrokerAcceptedByMaster"
}

// MasterAccepted Notify the master has been accepted.
type masterAccepted struct {
	// The vector of fuzzer configurations broker has
	FuzzerConfigurations []FuzzerConfiguration `msgpack:"fuzzer_configurations"`
	// The broker id of this element
	// master choose it
	BrokerID uint32 `msgpack:"broker_id"`
}

type FuzzerConfiguration struct {
	MutatorID   string `msgpack:"mutator_id"`
	SchedulerID string `msgpack:"scheduler_id"`
}

func (masterAccepted) Name() string {
	return "MasterAccepted"
}

type newTestcase struct {
	Input        input     `msgpack:"input"`
	ObserversBug *[]int    `msgpack:",omitempty"`
	ExitKind     string    `msgpack:"exit_kind"`
	CorpusSize   uint32    `msgpack:"corpus_size"`
	ClientConfig string    `msgpack:"client_config"`
	Timestamp    timestamp `msgpack:"time"`
	Executions   uint32    `msgpack:"executions"`
}

type input struct {
	Input []int
}

type timestamp struct {
	Secs uint64 `msgpack:"secs"`
	Nsec uint64 `msgpack:"nsec"`
}

func (newTestcase) Name() string {
	return "NewTestcase"
}

type updateExecsStats struct {
	Timestamp  time.Duration `msgpack:"time"`
	Executions uint64        `msgpack:"executions"`
	Phantom    interface{}   `msgpack:"phantom"`
}

type infraMessageHello struct {
	Kind int32 `msgpack:"kind"`
	// todo: fuzzer registrations or empty for evalera
}

type evaluationOutput struct {
	EvalData []evaluationOutput `msgpack:"eval_data"`
}

type evaluationData struct {
	ExecInfo uint64                 `msgpack:"exec_info"`
	Coverage [entities.CovSize]byte `msgpack:"coverage"`
}
