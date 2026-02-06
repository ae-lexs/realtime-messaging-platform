package config_test

import (
	"context"
	"testing"

	"github.com/aelexs/realtime-messaging-platform/internal/config"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	cfg, err := config.Load(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "local", cfg.Environment)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)

	// Service ports
	assert.Equal(t, 8080, cfg.Gateway.HTTPPort)
	assert.Equal(t, 9090, cfg.Gateway.GRPCPort)
	assert.Equal(t, 8081, cfg.Ingest.HTTPPort)
	assert.Equal(t, 9091, cfg.Ingest.GRPCPort)
	assert.Equal(t, 8082, cfg.Fanout.HTTPPort)
	assert.Equal(t, 8083, cfg.ChatMgmt.HTTPPort)
	assert.Equal(t, 9093, cfg.ChatMgmt.GRPCPort)

	// Infrastructure defaults
	assert.Equal(t, domain.DynamoDBTimeout, cfg.DynamoDB.Timeout)
	assert.Equal(t, "localhost:6379", cfg.Redis.Addr)
	assert.Equal(t, 0, cfg.Redis.DB)
	assert.Equal(t, domain.RedisTimeout, cfg.Redis.Timeout)
	assert.Equal(t, "messaging-platform", cfg.Kafka.ClientID)
	assert.Equal(t, "us-east-1", cfg.AWS.Region)
}

func TestIsLocal(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"local returns true", "local", true},
		{"prod returns false", "prod", false},
		{"dev returns false", "dev", false},
		{"empty returns false", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Environment: tt.env}

			assert.Equal(t, tt.want, cfg.IsLocal())
		})
	}
}

func TestIsProd(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"prod returns true", "prod", true},
		{"local returns false", "local", false},
		{"dev returns false", "dev", false},
		{"empty returns false", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Environment: tt.env}

			assert.Equal(t, tt.want, cfg.IsProd())
		})
	}
}

func TestValidateRequired_LocalAllowsMissingFields(t *testing.T) {
	t.Setenv("ENVIRONMENT", "local")

	cfg, err := config.Load(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "local", cfg.Environment)
}

func TestValidateRequired_ProdRequiresKafkaBrokers(t *testing.T) {
	t.Setenv("ENVIRONMENT", "prod")
	t.Setenv("REDIS_ADDR", "redis:6379")

	_, err := config.Load(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrConfigRequired)
	assert.Contains(t, err.Error(), "kafka.brokers")
}

func TestValidateRequired_ProdRequiresRedisAddr(t *testing.T) {
	t.Setenv("ENVIRONMENT", "prod")
	t.Setenv("KAFKA_BROKERS", "broker1:9092")
	t.Setenv("REDIS_ADDR", "")

	_, err := config.Load(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrConfigRequired)
	assert.Contains(t, err.Error(), "redis.addr")
}

func TestLoadWithEnvOverride(t *testing.T) {
	t.Setenv("ENVIRONMENT", "prod")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("KAFKA_BROKERS", "broker1:9092")

	cfg, err := config.Load(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "prod", cfg.Environment)
	assert.Equal(t, "redis:6379", cfg.Redis.Addr)
}
