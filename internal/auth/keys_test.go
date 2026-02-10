package auth_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
)

func TestStaticKeyStore(t *testing.T) {
	key := generateTestKey(t)
	keyID := "test-key-001"
	store := auth.NewStaticKeyStore(key, keyID)

	t.Run("SigningKey returns configured key and ID", func(t *testing.T) {
		pk, kid, err := store.SigningKey()
		require.NoError(t, err)
		assert.Equal(t, key, pk)
		assert.Equal(t, keyID, kid)
	})

	t.Run("PublicKey returns key for known kid", func(t *testing.T) {
		pk, err := store.PublicKey(keyID)
		require.NoError(t, err)
		assert.Equal(t, &key.PublicKey, pk)
	})

	t.Run("PublicKey returns error for unknown kid", func(t *testing.T) {
		_, err := store.PublicKey("unknown-key")
		assert.Error(t, err)
	})

	t.Run("AddPublicKey adds additional keys", func(t *testing.T) {
		key2 := generateTestKey(t)
		store.AddPublicKey("key-002", &key2.PublicKey)

		pk, err := store.PublicKey("key-002")
		require.NoError(t, err)
		assert.Equal(t, &key2.PublicKey, pk)
	})
}

func TestStaticKeyStore_NilKey(t *testing.T) {
	store := &auth.StaticKeyStore{}

	_, _, err := store.SigningKey()
	assert.Error(t, err)
}
