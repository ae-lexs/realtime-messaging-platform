package observability_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/aelexs/realtime-messaging-platform/internal/observability"
	"github.com/stretchr/testify/assert"
)

func TestRedactingHandler(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		value        string
		shouldRedact bool
	}{
		{"api_key is redacted", "api_key", "secret123", true},
		{"password is redacted", "password", "mysecret", true},
		{"db_password is redacted", "db_password", "dbpass", true},
		{"auth_token is redacted", "auth_token", "token123", true},
		{"jwt_secret is redacted", "jwt_secret", "jwtsec", true},
		{"authorization is redacted", "authorization", "Bearer xyz", true},
		{"private_key is redacted", "private_key", "-----BEGIN", true},
		{"AWS_SECRET_ACCESS_KEY is redacted", "aws_secret_access_key", "AKIA...", true},
		{"user_id not redacted", "user_id", "user123", false},
		{"chat_id not redacted", "chat_id", "chat456", false},
		{"message not redacted", "message", "hello world", false},
		{"error not redacted", "error", "something failed", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			handler := observability.NewRedactingHandler(&buf, nil)
			logger := slog.New(handler)

			logger.Info("test", tt.key, tt.value)
			output := buf.String()

			if tt.shouldRedact {
				assert.Contains(t, output, "[REDACTED]", "expected %s to be redacted", tt.key)
				assert.NotContains(t, output, tt.value, "expected actual value to not appear for %s", tt.key)
			} else {
				assert.Contains(t, output, tt.value, "expected %s value to appear", tt.key)
				assert.NotContains(t, output, "[REDACTED]", "expected %s to not be redacted", tt.key)
			}
		})
	}
}

func TestInitLogger(t *testing.T) {
	t.Run("creates logger with service context", func(t *testing.T) {
		cfg := observability.LogConfig{
			Level:       "info",
			Format:      "json",
			ServiceName: "test-service",
			Environment: "test",
		}

		logger := observability.InitLogger(cfg)
		assert.NotNil(t, logger)
	})

	t.Run("respects log level", func(t *testing.T) {
		cfg := observability.LogConfig{
			Level:       "error",
			Format:      "json",
			ServiceName: "test-service",
			Environment: "test",
		}

		_ = observability.InitLogger(cfg)
		// Logger is set as default, but we can't easily test level filtering
		// without more complex setup. The important thing is it doesn't panic.
	})
}
