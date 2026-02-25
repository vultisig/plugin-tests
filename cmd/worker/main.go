package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/vultisig/plugin-tests/config"
	"github.com/vultisig/plugin-tests/internal/health"
	"github.com/vultisig/plugin-tests/internal/logging"
	"github.com/vultisig/plugin-tests/internal/queue"
	"github.com/vultisig/plugin-tests/internal/storage/postgres"
	"github.com/vultisig/plugin-tests/internal/worker"
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

	cfg, err := config.ReadWorkerConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config: %v\n", err)
		os.Exit(1)
	}

	logger := logging.NewLogger(cfg.LogFormat)

	db, err := postgres.NewPostgresBackend(cfg.Database.DSN)
	if err != nil {
		logger.WithError(err).Fatal("failed to connect to database")
	}
	defer db.Close()

	redisOpt, err := queue.NewRedisConnOpt(cfg.QueueRedis)
	if err != nil {
		logger.WithError(err).Fatal("failed to create redis connection")
	}

	k8sClient, err := buildK8sClient(cfg.KubeConfig)
	if err != nil {
		logger.WithError(err).Fatal("failed to create k8s client")
	}

	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: concurrency,
		Queues: map[string]int{
			queue.QueueName: 1,
		},
		Logger: logger,
	})

	go func() {
		<-ctx.Done()
		logger.Info("shutting down worker")
		srv.Shutdown()
	}()

	consumer := worker.NewConsumer(db, k8sClient, logger, cfg.K8sJob, cfg.ArtifactS3)

	janitor := worker.NewJanitor(db, k8sClient, logger, cfg.Janitor)
	go janitor.Run(ctx)

	if cfg.HealthPort > 0 {
		healthServer := health.New(cfg.HealthPort)
		go func() {
			err := healthServer.Start(ctx, logger)
			if err != nil {
				logger.WithError(err).Error("health server failed")
			}
		}()
	}

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeRunIntegrationTest, consumer.Handle)

	logger.WithField("concurrency", concurrency).Info("starting worker")

	err = srv.Run(mux)
	if err != nil {
		logger.WithError(err).Fatal("worker failed")
	}
}

func buildK8sClient(kubeconfig string) (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		if kubeconfig == "" {
			return nil, fmt.Errorf("not in cluster and KUBECONFIG not set: %w", err)
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build kubeconfig from %s: %w", kubeconfig, err)
		}
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	return client, nil
}
