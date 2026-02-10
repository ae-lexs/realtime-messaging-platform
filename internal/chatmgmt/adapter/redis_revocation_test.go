package adapter_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/adapter"
	redisclient "github.com/aelexs/realtime-messaging-platform/internal/redis"
)

func newTestRevocationStore(t *testing.T) (*adapter.RevocationStore, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redisclient.NewClient(redisclient.Config{
		Addr:         mr.Addr(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})

	return adapter.NewRevocationStore(client.RDB), mr
}

func TestRevocationStore_Revoke(t *testing.T) {
	t.Run("creates revocation key", func(t *testing.T) {
		store, mr := newTestRevocationStore(t)
		ctx := context.Background()

		err := store.Revoke(ctx, "abc-123-jti")

		require.NoError(t, err)
		assert.True(t, mr.Exists("revoked_jti:abc-123-jti"), "revocation key should exist")
		val, getErr := mr.Get("revoked_jti:abc-123-jti")
		require.NoError(t, getErr)
		assert.Equal(t, "1", val, "value should be '1'")
	})

	t.Run("sets fixed 3600s TTL", func(t *testing.T) {
		store, mr := newTestRevocationStore(t)
		ctx := context.Background()

		err := store.Revoke(ctx, "def-456-jti")

		require.NoError(t, err)
		ttl := mr.TTL("revoked_jti:def-456-jti")
		assert.Equal(t, 3600*time.Second, ttl, "TTL should be exactly 3600 seconds")
	})

	t.Run("revoking same JTI twice succeeds", func(t *testing.T) {
		store, mr := newTestRevocationStore(t)
		ctx := context.Background()

		require.NoError(t, store.Revoke(ctx, "ghi-789-jti"))
		require.NoError(t, store.Revoke(ctx, "ghi-789-jti"))

		assert.True(t, mr.Exists("revoked_jti:ghi-789-jti"), "key should still exist")
	})
}

func TestRevocationStore_IsRevoked(t *testing.T) {
	t.Run("returns false for non-revoked JTI", func(t *testing.T) {
		store, _ := newTestRevocationStore(t)
		ctx := context.Background()

		revoked, err := store.IsRevoked(ctx, "unknown-jti")

		require.NoError(t, err)
		assert.False(t, revoked, "non-revoked JTI should return false")
	})

	t.Run("returns true after Revoke", func(t *testing.T) {
		store, _ := newTestRevocationStore(t)
		ctx := context.Background()

		require.NoError(t, store.Revoke(ctx, "revoked-jti"))

		revoked, err := store.IsRevoked(ctx, "revoked-jti")

		require.NoError(t, err)
		assert.True(t, revoked, "revoked JTI should return true")
	})

	t.Run("returns false after TTL expires", func(t *testing.T) {
		store, mr := newTestRevocationStore(t)
		ctx := context.Background()

		require.NoError(t, store.Revoke(ctx, "expiring-jti"))

		// Fast-forward past the 3600s TTL.
		mr.FastForward(3601 * time.Second)

		revoked, err := store.IsRevoked(ctx, "expiring-jti")

		require.NoError(t, err)
		assert.False(t, revoked, "revocation should expire after TTL")
	})

	t.Run("different JTIs are independent", func(t *testing.T) {
		store, _ := newTestRevocationStore(t)
		ctx := context.Background()

		require.NoError(t, store.Revoke(ctx, "jti-a"))

		revoked, err := store.IsRevoked(ctx, "jti-b")

		require.NoError(t, err)
		assert.False(t, revoked, "unrevoked JTI should not be affected by other revocations")
	})
}

func TestRevocationStore_RevokeAndCheck_Integration(t *testing.T) {
	t.Run("full lifecycle: revoke then check then expire", func(t *testing.T) {
		store, mr := newTestRevocationStore(t)
		ctx := context.Background()
		jti := "lifecycle-jti"

		// Before revocation.
		revoked, err := store.IsRevoked(ctx, jti)
		require.NoError(t, err)
		assert.False(t, revoked, "should not be revoked initially")

		// After revocation.
		require.NoError(t, store.Revoke(ctx, jti))

		revoked, err = store.IsRevoked(ctx, jti)
		require.NoError(t, err)
		assert.True(t, revoked, "should be revoked after Revoke call")

		// After TTL expires.
		mr.FastForward(3601 * time.Second)

		revoked, err = store.IsRevoked(ctx, jti)
		require.NoError(t, err)
		assert.False(t, revoked, "should no longer be revoked after TTL expires")
	})
}
