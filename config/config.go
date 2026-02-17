package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

type APIConfig struct {
	LogFormat  string       `envconfig:"LOG_FORMAT"`
	Server     ServerConfig `envconfig:"SERVER"`
	Database   DatabaseConfig
	QueueRedis RedisConfig `envconfig:"QUEUE_REDIS"`
}

type WorkerConfig struct {
	LogFormat  string `envconfig:"LOG_FORMAT"`
	Database   DatabaseConfig
	QueueRedis RedisConfig `envconfig:"QUEUE_REDIS"`
	Kubernetes K8sConfig   `envconfig:"KUBERNETES"`
	ArtifactS3 S3Config    `envconfig:"ARTIFACT_S3"`
	Janitor    JanitorConfig
	HealthPort int `envconfig:"HEALTH_PORT"`
}

type ServerConfig struct {
	Host string `envconfig:"HOST"`
	Port int    `envconfig:"PORT"`
}

type DatabaseConfig struct {
	DSN string `envconfig:"DATABASE_DSN" required:"true"`
}

type RedisConfig struct {
	URI      string `envconfig:"URI"`
	Host     string `envconfig:"HOST"`
	Port     string `envconfig:"PORT"`
	User     string `envconfig:"USER"`
	Password string `envconfig:"PASSWORD"`
	DB       int    `envconfig:"DB"`
}

type S3Config struct {
	Endpoint  string `envconfig:"ENDPOINT"`
	Region    string `envconfig:"REGION"`
	AccessKey string `envconfig:"ACCESS_KEY"`
	SecretKey string `envconfig:"SECRET_KEY"`
	Bucket    string `envconfig:"BUCKET"`
}

type K8sConfig struct {
	VerifierImage       string `envconfig:"VERIFIER_IMAGE"`
	VerifierWorkerImage string `envconfig:"VERIFIER_WORKER_IMAGE"`
	TestImage           string `envconfig:"TEST_IMAGE"`
	ImagePullSecret     string `envconfig:"IMAGE_PULL_SECRET"`
	JobTimeoutMinutes   int    `envconfig:"JOB_TIMEOUT_MINUTES"`
}

type JanitorConfig struct {
	IntervalMinutes       int `envconfig:"JANITOR_INTERVAL_MINUTES"`
	StaleThresholdMinutes int `envconfig:"JANITOR_STALE_THRESHOLD_MINUTES"`
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
