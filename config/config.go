package config

import (
	"fmt"
	"os"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type APIConfig struct {
	LogFormat  string         `envconfig:"LOG_FORMAT"`
	Server     ServerConfig   `envconfig:"SERVER"`
	Database   DatabaseConfig `envconfig:"DATABASE"`
	QueueRedis RedisConfig    `envconfig:"QUEUE_REDIS"`
	ArtifactS3 S3Config       `envconfig:"ARTIFACT_S3"`
}

type WorkerConfig struct {
	LogFormat   string         `envconfig:"LOG_FORMAT"`
	Concurrency int            `envconfig:"CONCURRENCY"`
	KubeConfig  string         `envconfig:"KUBECONFIG"`
	HealthPort  int            `envconfig:"HEALTH_PORT"`
	Database    DatabaseConfig `envconfig:"DATABASE"`
	QueueRedis  RedisConfig    `envconfig:"QUEUE_REDIS"`
	K8sJob      K8sJobConfig   `envconfig:"K8S"`
	ArtifactS3  S3Config       `envconfig:"ARTIFACT_S3"`
	Janitor     JanitorConfig  `envconfig:"JANITOR"`
}

type ServerConfig struct {
	Host string `envconfig:"HOST"`
	Port int    `envconfig:"PORT"`
}

type DatabaseConfig struct {
	DSN string `envconfig:"DSN" required:"true"`
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

type K8sJobConfig struct {
	JobTimeout          time.Duration `envconfig:"JOB_TIMEOUT"`
	PollInterval        time.Duration `envconfig:"POLL_INTERVAL"`
	TTLAfterFinished    int32         `envconfig:"JOB_TTL_SECONDS"`
	VerifierImage       string        `envconfig:"VERIFIER_IMAGE"`
	VerifierWorkerImage string        `envconfig:"VERIFIER_WORKER_IMAGE"`
	TestImage           string        `envconfig:"TEST_IMAGE"`
	ImagePullSecret     string        `envconfig:"IMAGE_PULL_SECRET"`
	PostgresImage       string        `envconfig:"POSTGRES_IMAGE"`
	RedisImage          string        `envconfig:"REDIS_IMAGE"`
	MinioImage          string        `envconfig:"MINIO_IMAGE"`
	EncryptionSecret    string        `envconfig:"ENCRYPTION_SECRET"`
	JWTSecret           string        `envconfig:"JWT_SECRET"`
	PluginEndpoint      string        `envconfig:"PLUGIN_ENDPOINT"`
	HostAliases         string        `envconfig:"HOST_ALIASES"`
}

type JanitorConfig struct {
	Interval       time.Duration `envconfig:"INTERVAL"`
	StaleThreshold time.Duration `envconfig:"STALE_THRESHOLD"`
}

func ReadAPIConfig() (*APIConfig, error) {
	var cfg APIConfig
	err := envconfig.Process("PLUGIN_TESTS_API", &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read API config: %w", err)
	}
	return &cfg, nil
}

func ReadWorkerConfig() (*WorkerConfig, error) {
	var cfg WorkerConfig
	err := envconfig.Process("PLUGIN_TESTS_WORKER", &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read worker config: %w", err)
	}
	if cfg.KubeConfig == "" {
		cfg.KubeConfig = os.Getenv("KUBECONFIG")
	}
	return &cfg, nil
}
