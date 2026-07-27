// Package config provides configuration loading using koanf.
// Follows TBD-PR0-1 decisions: env → defaults precedence.
package config

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// Config holds all service configuration.
// Fields marked with `required:"true"` cause startup failure if missing.
type Config struct {
	// Environment identifier: "local", "dev", "prod"
	Environment string `koanf:"environment"`

	// Logging configuration
	LogLevel  string `koanf:"log_level"`
	LogFormat string `koanf:"log_format"`

	// Service-specific configurations
	Gateway  GatewayConfig  `koanf:"gateway"`
	Ingest   IngestConfig   `koanf:"ingest"`
	Fanout   FanoutConfig   `koanf:"fanout"`
	ChatMgmt ChatMgmtConfig `koanf:"chatmgmt"`

	// Infrastructure configurations
	Kafka     KafkaConfig     `koanf:"kafka"`
	Redis     RedisConfig     `koanf:"redis"`
	Firestore FirestoreConfig `koanf:"firestore"`
	Secrets   SecretsConfig   `koanf:"secrets"`

	// Auth configuration (ADR-015)
	Auth AuthConfig `koanf:"auth"`

	// OpenTelemetry configuration
	OTEL OTELConfig `koanf:"otel"`
}

// GatewayConfig holds Gateway service configuration.
type GatewayConfig struct {
	HTTPPort int `koanf:"http_port"`
	GRPCPort int `koanf:"grpc_port"`
}

// IngestConfig holds Ingest service configuration.
type IngestConfig struct {
	HTTPPort int `koanf:"http_port"`
	GRPCPort int `koanf:"grpc_port"`
}

// FanoutConfig holds Fanout service configuration.
type FanoutConfig struct {
	HTTPPort int `koanf:"http_port"`
}

// ChatMgmtConfig holds Chat Management service configuration.
type ChatMgmtConfig struct {
	HTTPPort int `koanf:"http_port"`
	GRPCPort int `koanf:"grpc_port"`
}

// KafkaConfig holds Kafka configuration.
type KafkaConfig struct {
	Brokers  []string `koanf:"brokers"` // Required in production
	ClientID string   `koanf:"client_id"`
}

// RedisConfig holds Redis configuration.
type RedisConfig struct {
	Addr     string        `koanf:"addr"` // Required
	Password string        `koanf:"password"`
	DB       int           `koanf:"db"`
	Timeout  time.Duration `koanf:"timeout"`
}

// FirestoreConfig holds Firestore configuration (ADR-023 identity tier).
//
// Keys are single words on purpose: Load maps every underscore in an env var to
// a key delimiter, so FIRESTORE_PROJECT reaches `firestore.project` while
// FIRESTORE_PROJECT_ID would become `firestore.project.id` and bind to nothing.
type FirestoreConfig struct {
	ProjectID string `koanf:"project"` // Required outside local — env FIRESTORE_PROJECT
	// Database is the named database Terraform provisions; there is no
	// "(default)" database in this project.
	Database string        `koanf:"database"` // Required outside local — env FIRESTORE_DATABASE
	Timeout  time.Duration `koanf:"timeout"`
}

// SecretsConfig holds GCP Secret Manager configuration — the source of the
// JWT signing key and the OTP pepper (ADR-015 §3.2, Appendix F).
//
// Keys are single words for the same reason FirestoreConfig's are: Load maps
// every underscore in an env var to a key delimiter, so SECRETS_PROJECT
// reaches `secrets.project` while SECRETS_PROJECT_ID would become
// `secrets.project.id` and bind to nothing.
type SecretsConfig struct {
	ProjectID string        `koanf:"project"` // Required outside local — env SECRETS_PROJECT
	Timeout   time.Duration `koanf:"timeout"`
}

// AuthConfig holds JWT issuance and validation parameters (ADR-015 §3).
//
// Issuer and Audience are validated on every token, so both halves of the
// system must agree: a Gateway configured with a different audience will
// reject every token Chat Mgmt mints, with an error that names neither value.
type AuthConfig struct {
	Issuer   string `koanf:"issuer"`   // env AUTH_ISSUER
	Audience string `koanf:"audience"` // env AUTH_AUDIENCE

	// KeyRefresh bounds how stale the in-memory key set may be (ADR-015 §3.2).
	// It is also the upper bound on how long a freshly rotated key stays
	// unrecognised, since the unknown-kid immediate refresh is deferred.
	KeyRefresh time.Duration `koanf:"keyrefresh"` // env AUTH_KEYREFRESH
}

// OTELConfig holds OpenTelemetry configuration.
type OTELConfig struct {
	Endpoint    string `koanf:"endpoint"` // Empty disables OTLP export
	ServiceName string `koanf:"service_name"`
}

// defaults returns a Config with compiled default values.
// These match normative limits from ADR-009.
func defaults() *Config {
	return &Config{
		Environment: "local",
		LogLevel:    "info",
		LogFormat:   "json",

		Gateway: GatewayConfig{
			HTTPPort: 8080,
			GRPCPort: 9090,
		},
		Ingest: IngestConfig{
			HTTPPort: 8081,
			GRPCPort: 9091,
		},
		Fanout: FanoutConfig{
			HTTPPort: 8082,
		},
		ChatMgmt: ChatMgmtConfig{
			HTTPPort: 8083,
			GRPCPort: 9093,
		},

		Kafka: KafkaConfig{
			ClientID: "messaging-platform",
		},
		Redis: RedisConfig{
			Addr:    "localhost:6379",
			DB:      0,
			Timeout: domain.RedisTimeout,
		},
		Firestore: FirestoreConfig{
			Timeout: domain.FirestoreTimeout,
		},
		Secrets: SecretsConfig{
			Timeout: domain.SecretManagerTimeout,
		},
		Auth: AuthConfig{
			Issuer:     "messaging-platform",
			Audience:   "messaging-api",
			KeyRefresh: domain.JWTKeyRefreshInterval,
		},
	}
}

// Load loads configuration following the precedence:
// 1. Environment variables (highest)
// 2. Compiled defaults (lowest)
//
// Per TBD-PR0-1 normative rule:
// - Required keys missing → startup failure
// - Optional keys missing → fallback to defaults with warning
func Load(ctx context.Context) (*Config, error) {
	k := koanf.New(".")

	// Start with compiled defaults
	cfg := defaults()

	// Load environment variables
	// Prefix: none (we use full names like GATEWAY_HTTP_PORT)
	// Delimiter: _ maps to . for nested config
	err := k.Load(env.Provider("", ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(s), "_", ".")
	}), nil)
	if err != nil {
		return nil, fmt.Errorf("load env vars: %w", err)
	}

	// Unmarshal into config struct
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Validate required fields
	if err := validateRequired(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validateRequired checks that required configuration is present.
// Per TBD-PR0-1: required key failure → startup failure.
func validateRequired(cfg *Config) error {
	// In local environment, most fields have sensible defaults
	if cfg.Environment == "local" {
		return nil
	}

	// In production, certain fields are required
	if cfg.Environment == "prod" {
		if len(cfg.Kafka.Brokers) == 0 {
			return fmt.Errorf("%w: kafka.brokers", domain.ErrConfigRequired)
		}
		if cfg.Redis.Addr == "" {
			return fmt.Errorf("%w: redis.addr", domain.ErrConfigRequired)
		}
		if cfg.Firestore.ProjectID == "" {
			return fmt.Errorf("%w: firestore.project", domain.ErrConfigRequired)
		}
		// There is no "(default)" database in this project; a missing ID would
		// silently target one that was never provisioned.
		if cfg.Firestore.Database == "" {
			return fmt.Errorf("%w: firestore.database", domain.ErrConfigRequired)
		}
		// Without this the signing key cannot be located, and ADR-015 requires
		// Chat Mgmt to refuse to start rather than serve auth it cannot sign.
		if cfg.Secrets.ProjectID == "" {
			return fmt.Errorf("%w: secrets.project", domain.ErrConfigRequired)
		}
	}

	return nil
}

// IsLocal returns true if running in local development environment.
func (c *Config) IsLocal() bool {
	return c.Environment == "local"
}

// IsProd returns true if running in production environment.
func (c *Config) IsProd() bool {
	return c.Environment == "prod"
}
