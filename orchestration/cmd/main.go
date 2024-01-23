package main

import (
	"context"
	"orchestration/core/infodb"
	"orchestration/core/master"
	"orchestration/infra/conn/tcp"
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

	<-ctx.Done()
	srv.Close()
	logger.Infof("server closed")
}
