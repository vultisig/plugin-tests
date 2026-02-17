package queue

import (
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/vultisig/plugin-tests/config"
)

func NewRedisConnOpt(cfg config.RedisConfig) (asynq.RedisConnOpt, error) {
	if cfg.URI != "" {
		opt, err := asynq.ParseRedisURI(cfg.URI)
		if err != nil {
			return nil, fmt.Errorf("failed to parse redis URI: %w", err)
		}
		return opt, nil
	}

	if cfg.Host == "" {
		return nil, fmt.Errorf("redis configuration requires either URI or Host to be set")
	}

	return asynq.RedisClientOpt{
		Addr:     cfg.Host + ":" + cfg.Port,
		Username: cfg.User,
		Password: cfg.Password,
		DB:       cfg.DB,
	}, nil
}
