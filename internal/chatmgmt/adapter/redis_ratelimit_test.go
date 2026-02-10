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

func newTestRateLimiter(t *testing.T) (*adapter.RateLimiter, *miniredis.Miniredis) {
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

	return adapter.NewRateLimiter(client.RDB), mr
}

func TestRateLimiter_CheckAndIncrement(t *testing.T) {
	t.Run("allows requests under the limit", func(t *testing.T) {
		rl, _ := newTestRateLimiter(t)
		ctx := context.Background()

		allowed, err := rl.CheckAndIncrement(ctx, "otp_req:phone:abc", 3, 60)

		require.NoError(t, err)
		assert.True(t, allowed, "first request should be allowed")
	})

	t.Run("allows exactly up to the limit", func(t *testing.T) {
		rl, _ := newTestRateLimiter(t)
		ctx := context.Background()
		key := "otp_req:phone:def"
		limit := 3

		for i := 0; i < limit; i++ {
			allowed, err := rl.CheckAndIncrement(ctx, key, limit, 60)
			require.NoError(t, err)
			assert.True(t, allowed, "request %d should be allowed", i+1)
		}
	})

	t.Run("rejects requests exceeding the limit", func(t *testing.T) {
		rl, _ := newTestRateLimiter(t)
		ctx := context.Background()
		key := "otp_req:phone:ghi"
		limit := 3

		for i := 0; i < limit; i++ {
			_, err := rl.CheckAndIncrement(ctx, key, limit, 60)
			require.NoError(t, err)
		}

		allowed, err := rl.CheckAndIncrement(ctx, key, limit, 60)

		require.NoError(t, err)
		assert.False(t, allowed, "request beyond limit should be rejected")
	})

	t.Run("sets TTL on the key", func(t *testing.T) {
		rl, mr := newTestRateLimiter(t)
		ctx := context.Background()
		key := "otp_req:phone:jkl"

		_, err := rl.CheckAndIncrement(ctx, key, 10, 900)

		require.NoError(t, err)
		assert.True(t, mr.Exists(key), "key should exist after increment")
		ttl := mr.TTL(key)
		assert.Equal(t, 900*time.Second, ttl, "TTL should match windowSeconds")
	})

	t.Run("does not reset TTL on subsequent increments", func(t *testing.T) {
		rl, mr := newTestRateLimiter(t)
		ctx := context.Background()
		key := "otp_req:phone:mno"

		_, err := rl.CheckAndIncrement(ctx, key, 10, 900)
		require.NoError(t, err)

		// Fast-forward 100s so TTL decreases.
		mr.FastForward(100 * time.Second)

		_, err = rl.CheckAndIncrement(ctx, key, 10, 900)
		require.NoError(t, err)

		ttl := mr.TTL(key)
		assert.Equal(t, 800*time.Second, ttl, "TTL should not reset on subsequent increments")
	})

	t.Run("different keys are independent", func(t *testing.T) {
		rl, _ := newTestRateLimiter(t)
		ctx := context.Background()
		limit := 1

		_, err := rl.CheckAndIncrement(ctx, "key:a", limit, 60)
		require.NoError(t, err)

		allowed, err := rl.CheckAndIncrement(ctx, "key:b", limit, 60)
		require.NoError(t, err)
		assert.True(t, allowed, "different key should be independent")
	})

	t.Run("counter resets after window expires", func(t *testing.T) {
		rl, mr := newTestRateLimiter(t)
		ctx := context.Background()
		key := "otp_req:phone:pqr"
		limit := 1

		_, err := rl.CheckAndIncrement(ctx, key, limit, 60)
		require.NoError(t, err)

		allowed, err := rl.CheckAndIncrement(ctx, key, limit, 60)
		require.NoError(t, err)
		assert.False(t, allowed, "second request in same window should be rejected")

		// Fast-forward past the window.
		mr.FastForward(61 * time.Second)

		allowed, err = rl.CheckAndIncrement(ctx, key, limit, 60)
		require.NoError(t, err)
		assert.True(t, allowed, "first request in new window should be allowed")
	})
}

func TestRateLimiter_CheckLockout(t *testing.T) {
	t.Run("returns false when no lockout exists", func(t *testing.T) {
		rl, _ := newTestRateLimiter(t)
		ctx := context.Background()

		locked, err := rl.CheckLockout(ctx, "otp_lockout:phone:abc")

		require.NoError(t, err)
		assert.False(t, locked, "should not be locked when key does not exist")
	})

	t.Run("returns true when lockout is active", func(t *testing.T) {
		rl, mr := newTestRateLimiter(t)
		ctx := context.Background()
		key := "otp_lockout:phone:def"

		require.NoError(t, mr.Set(key, "1"))

		locked, err := rl.CheckLockout(ctx, key)

		require.NoError(t, err)
		assert.True(t, locked, "should be locked when key exists")
	})

	t.Run("returns false after lockout expires", func(t *testing.T) {
		rl, mr := newTestRateLimiter(t)
		ctx := context.Background()
		key := "otp_lockout:phone:ghi"

		require.NoError(t, mr.Set(key, "1"))
		mr.SetTTL(key, 60*time.Second)

		// Fast-forward past the TTL.
		mr.FastForward(61 * time.Second)

		locked, err := rl.CheckLockout(ctx, key)

		require.NoError(t, err)
		assert.False(t, locked, "lockout should expire after TTL")
	})
}

func TestRateLimiter_SetLockout(t *testing.T) {
	t.Run("creates lockout key with TTL", func(t *testing.T) {
		rl, mr := newTestRateLimiter(t)
		ctx := context.Background()
		key := "otp_lockout:phone:abc"

		err := rl.SetLockout(ctx, key, 900)

		require.NoError(t, err)
		assert.True(t, mr.Exists(key), "lockout key should exist")
		val, getErr := mr.Get(key)
		require.NoError(t, getErr)
		assert.Equal(t, "1", val, "lockout value should be '1'")
		assert.Equal(t, 900*time.Second, mr.TTL(key), "TTL should match ttlSeconds")
	})

	t.Run("lockout expires after TTL", func(t *testing.T) {
		rl, mr := newTestRateLimiter(t)
		ctx := context.Background()
		key := "otp_lockout:phone:def"

		err := rl.SetLockout(ctx, key, 60)
		require.NoError(t, err)

		mr.FastForward(61 * time.Second)

		assert.False(t, mr.Exists(key), "lockout key should expire after TTL")
	})
}
