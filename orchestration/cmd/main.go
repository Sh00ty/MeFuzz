package main

import (
	"context"
	"fmt"
	"orchestration/core/infodb"
	"orchestration/core/master"
	"orchestration/entities"
	"orchestration/infra/conn/tcp"
	"orchestration/infra/utils/hashing"
	"orchestration/infra/utils/logger"
	"os"
	"os/signal"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Kill, os.Interrupt)
	defer cancel()

	infoDb, err := infodb.New("corpus")
	if err != nil {
		logger.Fatalf("failed to initialize database: %v", err)
	}
	m := master.NewMaster(context.Background(), infoDb)
	srv := tcp.NewSrv(m)

	for {
		tc := ""
		_, err = fmt.Scanf("%s", &tc)
		fmt.Println(tc)
		if err != nil {
			fmt.Println("failed to scan")
			break
		}
		if ctx.Err() != nil {
			fmt.Println("ctx err")
			break
		}
		m.GetTestcaseChan() <- entities.Testcase{
			ID:        hashing.MakeHash([]byte(tc)),
			InputData: []byte(tc),
			FuzzerID: entities.ElementID{
				OnNodeID: 15,
				NodeID:   2130706433,
			},
		}
		fmt.Println("tc saved")
	}

	<-ctx.Done()
	srv.Close()
	logger.Infof("server closed")
}
