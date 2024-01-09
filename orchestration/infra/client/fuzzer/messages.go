package fuzzer

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

func (newTestcase) Name() string {
	return "NewTestcase"
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

// MasterAccepted Notify the master has been accepted.
type masterAccepted struct {
	// The vector of fuzzer configurations broker has
	FuzzerConfigurations []fuzzerConfiguration `msgpack:"fuzzer_configurations"`
	// The broker id of this element
	// master choose it
	BrokerID uint32 `msgpack:"broker_id"`
}

type fuzzerConfiguration struct {
	MutatorID   string `msgpack:"mutator_id"`
	SchedulerID string `msgpack:"scheduler_id"`
}

func (masterAccepted) Name() string {
	return "MasterAccepted"
}
