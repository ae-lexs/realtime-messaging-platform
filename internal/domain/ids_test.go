package domain_test

import (
	"strings"
	"testing"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatID(t *testing.T) {
	validUUID := "550e8400-e29b-41d4-a716-446655440000"

	t.Run("valid UUID", func(t *testing.T) {
		id, err := domain.NewChatID(validUUID)
		require.NoError(t, err)
		assert.Equal(t, validUUID, id.String())
		assert.False(t, id.IsZero())
	})

	t.Run("empty string returns error", func(t *testing.T) {
		_, err := domain.NewChatID("")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrEmptyID)
	})

	t.Run("invalid format returns error", func(t *testing.T) {
		_, err := domain.NewChatID("not-a-uuid")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidID)
	})

	t.Run("zero value is zero", func(t *testing.T) {
		var id domain.ChatID
		assert.True(t, id.IsZero())
		assert.Empty(t, id.String())
	})

	t.Run("generate creates valid ID", func(t *testing.T) {
		id := domain.GenerateChatID()
		assert.False(t, id.IsZero())
		// Verify it's a valid UUID by parsing it
		_, err := domain.NewChatID(id.String())
		require.NoError(t, err)
	})

	t.Run("MustChatID panics on invalid", func(t *testing.T) {
		assert.Panics(t, func() {
			domain.MustChatID("invalid")
		})
	})

	t.Run("MustChatID succeeds on valid", func(t *testing.T) {
		assert.NotPanics(t, func() {
			id := domain.MustChatID(validUUID)
			assert.Equal(t, validUUID, id.String())
		})
	})
}

func TestUserID(t *testing.T) {
	validUUID := "550e8400-e29b-41d4-a716-446655440000"

	t.Run("valid UUID", func(t *testing.T) {
		id, err := domain.NewUserID(validUUID)
		require.NoError(t, err)
		assert.Equal(t, validUUID, id.String())
		assert.False(t, id.IsZero())
	})

	t.Run("empty string returns error", func(t *testing.T) {
		_, err := domain.NewUserID("")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrEmptyID)
	})

	t.Run("generate creates valid ID", func(t *testing.T) {
		id := domain.GenerateUserID()
		assert.False(t, id.IsZero())
	})
}

func TestMessageID(t *testing.T) {
	validUUID := "550e8400-e29b-41d4-a716-446655440000"

	t.Run("valid UUID", func(t *testing.T) {
		id, err := domain.NewMessageID(validUUID)
		require.NoError(t, err)
		assert.Equal(t, validUUID, id.String())
	})

	t.Run("generate creates valid ID", func(t *testing.T) {
		id := domain.GenerateMessageID()
		assert.False(t, id.IsZero())
	})
}

func TestSessionID(t *testing.T) {
	validUUID := "550e8400-e29b-41d4-a716-446655440000"

	t.Run("valid UUID", func(t *testing.T) {
		id, err := domain.NewSessionID(validUUID)
		require.NoError(t, err)
		assert.Equal(t, validUUID, id.String())
	})

	t.Run("generate creates valid ID", func(t *testing.T) {
		id := domain.GenerateSessionID()
		assert.False(t, id.IsZero())
	})
}

func TestDeviceID(t *testing.T) {
	validUUID := "550e8400-e29b-41d4-a716-446655440000"

	t.Run("valid UUID", func(t *testing.T) {
		id, err := domain.NewDeviceID(validUUID)
		require.NoError(t, err)
		assert.Equal(t, validUUID, id.String())
	})

	t.Run("generate creates valid ID", func(t *testing.T) {
		id := domain.GenerateDeviceID()
		assert.False(t, id.IsZero())
	})
}

func TestClientMessageID(t *testing.T) {
	t.Run("valid client message ID", func(t *testing.T) {
		id, err := domain.NewClientMessageID("client-123-abc")
		require.NoError(t, err)
		assert.Equal(t, "client-123-abc", id.String())
		assert.False(t, id.IsZero())
	})

	t.Run("empty string returns error", func(t *testing.T) {
		_, err := domain.NewClientMessageID("")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrEmptyID)
	})

	t.Run("too long returns error", func(t *testing.T) {
		longID := strings.Repeat("a", domain.MaxClientMessageIDLength+1)
		_, err := domain.NewClientMessageID(longID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidID)
	})

	t.Run("max length accepted", func(t *testing.T) {
		maxID := strings.Repeat("a", domain.MaxClientMessageIDLength)
		id, err := domain.NewClientMessageID(maxID)
		require.NoError(t, err)
		assert.Equal(t, maxID, id.String())
	})
}

func TestSequence(t *testing.T) {
	t.Run("creates from uint64", func(t *testing.T) {
		seq := domain.NewSequence(42)
		assert.Equal(t, uint64(42), seq.Uint64())
	})

	t.Run("zero is zero", func(t *testing.T) {
		seq := domain.NewSequence(0)
		assert.True(t, seq.IsZero())
	})

	t.Run("next increments", func(t *testing.T) {
		seq := domain.NewSequence(42)
		next := seq.Next()
		assert.Equal(t, uint64(43), next.Uint64())
	})
}

func TestConnectionID(t *testing.T) {
	validUUID := "550e8400-e29b-41d4-a716-446655440000"

	t.Run("valid UUID", func(t *testing.T) {
		id, err := domain.NewConnectionID(validUUID)
		require.NoError(t, err)
		assert.Equal(t, validUUID, id.String())
	})

	t.Run("generate creates valid ID", func(t *testing.T) {
		id := domain.GenerateConnectionID()
		assert.False(t, id.IsZero())
	})
}
