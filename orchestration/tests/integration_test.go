package tests

import (
	"orchestration/core/evaluating"
	"orchestration/core/infodb"
	"orchestration/core/monitoring"
	"orchestration/entities"
	"orchestration/infra/fuzzer_conn/tcp"
	"orchestration/infra/utils/logger"
	"testing"
	"time"
)

func TestGettingInfoFormFuzzers(t *testing.T) {
	db, err := infodb.New("./corpus")
	if err != nil {
		panic(err)
	}
	tcpSrv := tcp.NewTcpClient(
		100,
		100,
		300*time.Millisecond,
		100*time.Millisecond,
	)

	monitor := monitoring.New(tcpSrv.GetRecvMessageChan(), evaluating.New(db), db, 300, 5*time.Second)
	if err := tcpSrv.Connect(entities.Broker, entities.NodeID(1), "127.0.0.1", 1337); err != nil {
		logger.Fataf("failed to make connection: %v", err)
	}
	go func() {
		time.Sleep(15 * time.Second)
		monitor.Close()
	}()
	monitor.Run()
	_, covData := db.GetCovData()
	logger.Info("started PCA")
	if err := covData.UpdateDistances(); err != nil {
		t.Fatal(err)
	}
	covData.GetNearestDistances()
}
