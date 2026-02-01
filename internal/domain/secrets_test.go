package domain_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestSecretString(t *testing.T) {
	secret := domain.SecretString("my-super-secret-key")

	t.Run("String returns REDACTED", func(t *testing.T) {
		assert.Equal(t, "[REDACTED]", secret.String())
	})

	t.Run("Expose returns actual value", func(t *testing.T) {
		assert.Equal(t, "my-super-secret-key", secret.Expose())
	})

	t.Run("IsEmpty returns false for non-empty", func(t *testing.T) {
		assert.False(t, secret.IsEmpty())
	})

	t.Run("IsEmpty returns true for empty", func(t *testing.T) {
		empty := domain.SecretString("")
		assert.True(t, empty.IsEmpty())
	})

	t.Run("LogValue returns REDACTED slog value", func(t *testing.T) {
		logValue := secret.LogValue()
		assert.Equal(t, slog.KindString, logValue.Kind())
		assert.Equal(t, "[REDACTED]", logValue.String())
	})

	t.Run("slog output contains REDACTED", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		logger.Info("test", "api_key", secret)

		output := buf.String()
		assert.Contains(t, output, "[REDACTED]")
		assert.NotContains(t, output, "my-super-secret-key")
	})
}

func TestSecretBytes(t *testing.T) {
	secret := domain.SecretBytes([]byte("secret-bytes-data"))

	t.Run("String returns REDACTED", func(t *testing.T) {
		assert.Equal(t, "[REDACTED]", secret.String())
	})

	t.Run("Expose returns actual value", func(t *testing.T) {
		assert.Equal(t, []byte("secret-bytes-data"), secret.Expose())
	})

	t.Run("IsEmpty returns false for non-empty", func(t *testing.T) {
		assert.False(t, secret.IsEmpty())
	})

	t.Run("IsEmpty returns true for empty", func(t *testing.T) {
		empty := domain.SecretBytes{}
		assert.True(t, empty.IsEmpty())
	})

	t.Run("LogValue returns REDACTED slog value", func(t *testing.T) {
		logValue := secret.LogValue()
		assert.Equal(t, slog.KindString, logValue.Kind())
		assert.Equal(t, "[REDACTED]", logValue.String())
	})
}
