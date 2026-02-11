package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

func TestLogout(t *testing.T) {
	t.Run("success: session deleted + JTI revoked", func(t *testing.T) {
		h := newTestHarness(t)

		mintResult, err := h.minter.MintAccessToken("user-001", "sess-001")
		require.NoError(t, err)

		sessionDeleted := false
		h.sessionStore.deleteFn = func(_ context.Context, sessionID string) error {
			assert.Equal(t, "sess-001", sessionID)
			sessionDeleted = true
			return nil
		}

		jtiRevoked := ""
		h.revocationStore.revokeFn = func(_ context.Context, jti string) error {
			jtiRevoked = jti
			return nil
		}

		err = h.svc.Logout(context.Background(), mintResult.Token)
		require.NoError(t, err)
		assert.True(t, sessionDeleted, "session should be deleted")
		assert.Equal(t, mintResult.JTI, jtiRevoked, "JTI should be revoked")
	})

	t.Run("invalid access token: ErrUnauthorized", func(t *testing.T) {
		h := newTestHarness(t)

		err := h.svc.Logout(context.Background(), "garbage-token")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUnauthorized)
	})

	t.Run("session not found on delete: no error (idempotent)", func(t *testing.T) {
		h := newTestHarness(t)

		mintResult, err := h.minter.MintAccessToken("user-001", "sess-001")
		require.NoError(t, err)

		h.sessionStore.deleteFn = func(_ context.Context, _ string) error {
			return domain.ErrNotFound
		}

		err = h.svc.Logout(context.Background(), mintResult.Token)
		require.NoError(t, err)
	})

	t.Run("revoke failure: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		errRedis := errors.New("redis timeout")

		mintResult, err := h.minter.MintAccessToken("user-001", "sess-001")
		require.NoError(t, err)

		h.revocationStore.revokeFn = func(_ context.Context, _ string) error {
			return errRedis
		}

		err = h.svc.Logout(context.Background(), mintResult.Token)
		require.Error(t, err)
		assert.ErrorIs(t, err, errRedis)
	})
}
