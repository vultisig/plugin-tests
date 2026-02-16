package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

type APIConfig struct {
	LogFormat  string       `envconfig:"LOG_FORMAT" default:"text"`
	Server     ServerConfig `envconfig:"SERVER"`
	Database   DatabaseConfig
	QueueRedis RedisConfig `envconfig:"QUEUE_REDIS"`
}

type WorkerConfig struct {
	LogFormat  string `envconfig:"LOG_FORMAT" default:"text"`
	Database   DatabaseConfig
	QueueRedis RedisConfig `envconfig:"QUEUE_REDIS"`
	Kubernetes K8sConfig   `envconfig:"KUBERNETES"`
	ArtifactS3 S3Config    `envconfig:"ARTIFACT_S3"`
	Janitor    JanitorConfig
	HealthPort int `envconfig:"HEALTH_PORT" default:"80"`
}

type ServerConfig struct {
	Host string `envconfig:"HOST" default:"0.0.0.0"`
	Port int    `envconfig:"PORT" default:"8080"`
}

type DatabaseConfig struct {
	DSN string `envconfig:"DATABASE_DSN" required:"true"`
}

type RedisConfig struct {
	URI      string `envconfig:"URI"`
	Host     string `envconfig:"HOST"`
	Port     string `envconfig:"PORT" default:"6379"`
	User     string `envconfig:"USER"`
	Password string `envconfig:"PASSWORD"`
	DB       int    `envconfig:"DB" default:"0"`
}

type S3Config struct {
	Endpoint  string `envconfig:"ENDPOINT"`
	Region    string `envconfig:"REGION" default:"us-east-1"`
	AccessKey string `envconfig:"ACCESS_KEY"`
	SecretKey string `envconfig:"SECRET_KEY"`
	Bucket    string `envconfig:"BUCKET"`
}

type K8sConfig struct {
	VerifierImage       string `envconfig:"VERIFIER_IMAGE" default:"ghcr.io/vultisig/verifier:latest"`
	VerifierWorkerImage string `envconfig:"VERIFIER_WORKER_IMAGE" default:"ghcr.io/vultisig/verifier-worker:latest"`
	TestImage           string `envconfig:"TEST_IMAGE" default:"ghcr.io/vultisig/plugin-ops-test:latest"`
	ImagePullSecret     string `envconfig:"IMAGE_PULL_SECRET"`
	JobTimeoutMinutes   int    `envconfig:"JOB_TIMEOUT_MINUTES" default:"15"`
}

type JanitorConfig struct {
	IntervalMinutes       int `envconfig:"JANITOR_INTERVAL_MINUTES" default:"5"`
	StaleThresholdMinutes int `envconfig:"JANITOR_STALE_THRESHOLD_MINUTES" default:"30"`
}

func ReadAPIConfig() (*APIConfig, error) {
	var cfg APIConfig
	err := envconfig.Process("", &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read API config: %w", err)
	}
	return &cfg, nil
}

func ReadWorkerConfig() (*WorkerConfig, error) {
	var cfg WorkerConfig
	err := envconfig.Process("", &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read worker config: %w", err)
	}
	return &cfg, nil
}
