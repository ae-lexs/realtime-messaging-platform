package auth_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
)

func TestGenerateRefreshToken(t *testing.T) {
	t.Run("produces 43-char base64url string", func(t *testing.T) {
		token, err := auth.GenerateRefreshToken()
		require.NoError(t, err)
		assert.Len(t, token, 43) // 32 bytes base64url (no padding) = 43 chars
	})

	t.Run("produces different tokens", func(t *testing.T) {
		t1, err := auth.GenerateRefreshToken()
		require.NoError(t, err)
		t2, err := auth.GenerateRefreshToken()
		require.NoError(t, err)
		assert.NotEqual(t, t1, t2)
	})
}

func TestHashRefreshToken(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		h1 := auth.HashRefreshToken("some-token")
		h2 := auth.HashRefreshToken("some-token")
		assert.Equal(t, h1, h2)
	})

	t.Run("different tokens produce different hashes", func(t *testing.T) {
		h1 := auth.HashRefreshToken("token-a")
		h2 := auth.HashRefreshToken("token-b")
		assert.NotEqual(t, h1, h2)
	})

	t.Run("produces 64-char hex SHA-256", func(t *testing.T) {
		h := auth.HashRefreshToken("some-token")
		assert.Len(t, h, 64)
	})
}

func TestValidateRefreshHash(t *testing.T) {
	token := "dGhpcyBpcyBhIHJlZnJlc2ggdG9rZW4AAAA"
	hash := auth.HashRefreshToken(token)

	t.Run("matching token validates", func(t *testing.T) {
		assert.True(t, auth.ValidateRefreshHash(token, hash))
	})

	t.Run("different token rejects", func(t *testing.T) {
		assert.False(t, auth.ValidateRefreshHash("wrong-token", hash))
	})

	t.Run("empty token rejects", func(t *testing.T) {
		assert.False(t, auth.ValidateRefreshHash("", hash))
	})
}
