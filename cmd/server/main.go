package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"

	"github.com/vultisig/plugin-tests/config"
	"github.com/vultisig/plugin-tests/internal/api"
	"github.com/vultisig/plugin-tests/internal/logging"
	"github.com/vultisig/plugin-tests/internal/queue"
	"github.com/vultisig/plugin-tests/internal/storage/postgres"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	cfg, err := config.ReadAPIConfig()
	if err != nil {
		panic(err)
	}

	logger := logging.NewLogger(cfg.LogFormat)

	db, err := postgres.NewPostgresBackend(cfg.Database.DSN)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	redisOpt, err := queue.NewRedisConnOpt(cfg.QueueRedis)
	if err != nil {
		panic(err)
	}

	client := asynq.NewClient(redisOpt)
	producer := queue.NewProducer(client)
	defer producer.Close()

	server := api.NewServer(cfg, db, producer, logger)

	err = server.Start(ctx)
	if err != nil {
		logger.WithError(err).Fatal("API server failed")
	}
}
