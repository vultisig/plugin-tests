package queue

import (
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vultisig/plugin-tests/config"
)

func TestNewRedisConnOpt(t *testing.T) {
	t.Run("valid uri", func(t *testing.T) {
		cfg := config.RedisConfig{URI: "redis://localhost:6379/0"}

		opt, err := NewRedisConnOpt(cfg)
		require.NoError(t, err)
		assert.NotNil(t, opt)
	})

	t.Run("invalid uri", func(t *testing.T) {
		cfg := config.RedisConfig{URI: "not-a-valid-uri"}

		_, err := NewRedisConnOpt(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse redis URI")
	})

	t.Run("host port fallback", func(t *testing.T) {
		cfg := config.RedisConfig{
			Host:     "redis.local",
			Port:     "6380",
			User:     "admin",
			Password: "secret",
			DB:       2,
		}

		opt, err := NewRedisConnOpt(cfg)
		require.NoError(t, err)

		clientOpt, ok := opt.(asynq.RedisClientOpt)
		require.True(t, ok)
		assert.Equal(t, "redis.local:6380", clientOpt.Addr)
		assert.Equal(t, "admin", clientOpt.Username)
		assert.Equal(t, "secret", clientOpt.Password)
		assert.Equal(t, 2, clientOpt.DB)
	})

	t.Run("neither uri nor host", func(t *testing.T) {
		cfg := config.RedisConfig{}

		_, err := NewRedisConnOpt(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires either URI or Host")
	})

	t.Run("uri takes precedence over host", func(t *testing.T) {
		cfg := config.RedisConfig{
			URI:  "redis://localhost:6379/0",
			Host: "other-host",
			Port: "9999",
		}

		opt, err := NewRedisConnOpt(cfg)
		require.NoError(t, err)
		assert.NotNil(t, opt)
	})
}
