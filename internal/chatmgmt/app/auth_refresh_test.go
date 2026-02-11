package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

func TestRefreshTokens(t *testing.T) {
	const deviceID = "device-abc-123"

	t.Run("normal rotation: new tokens returned + session updated", func(t *testing.T) {
		h := newTestHarness(t)

		// Mint a real access token for validation.
		mintResult, err := h.minter.MintAccessToken("user-001", "sess-001")
		require.NoError(t, err)

		refreshToken := "original-refresh-token"
		refreshHash := auth.HashRefreshToken(refreshToken)

		session := sampleSessionRecord("user-001", "sess-001", deviceID, refreshHash, h.clock)
		h.sessionStore.getByIDFn = func(_ context.Context, _ string) (*app.SessionRecord, error) {
			return session, nil
		}

		var updatedSession app.SessionUpdate
		h.sessionStore.updateFn = func(_ context.Context, _ string, update app.SessionUpdate) error {
			updatedSession = update
			return nil
		}

		result, err := h.svc.RefreshTokens(context.Background(), mintResult.Token, refreshToken, deviceID)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.NotEmpty(t, result.AccessToken)
		assert.NotEmpty(t, result.RefreshToken)
		assert.NotEqual(t, refreshToken, result.RefreshToken, "should return new refresh token")

		// Session should be updated with new hash and bumped generation.
		assert.Equal(t, refreshHash, updatedSession.PrevTokenHash, "prev hash should be old hash")
		assert.Equal(t, session.TokenGeneration+1, updatedSession.TokenGeneration)
	})

	t.Run("reuse detection: session deleted + JTI revoked + ErrRefreshTokenReuse", func(t *testing.T) {
		h := newTestHarness(t)

		mintResult, err := h.minter.MintAccessToken("user-001", "sess-001")
		require.NoError(t, err)

		currentRefresh := "current-refresh-token"
		currentHash := auth.HashRefreshToken(currentRefresh)

		// The reused token is the previous one.
		reusedRefresh := "old-refresh-token"
		reusedHash := auth.HashRefreshToken(reusedRefresh)

		session := sampleSessionRecord("user-001", "sess-001", deviceID, currentHash, h.clock)
		session.PrevTokenHash = reusedHash

		h.sessionStore.getByIDFn = func(_ context.Context, _ string) (*app.SessionRecord, error) {
			return session, nil
		}

		sessionDeleted := false
		h.sessionStore.deleteFn = func(_ context.Context, _ string) error {
			sessionDeleted = true
			return nil
		}

		jtiRevoked := false
		h.revocationStore.revokeFn = func(_ context.Context, _ string) error {
			jtiRevoked = true
			return nil
		}

		_, err = h.svc.RefreshTokens(context.Background(), mintResult.Token, reusedRefresh, deviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenReuse)
		assert.True(t, sessionDeleted, "session should be deleted on reuse")
		assert.True(t, jtiRevoked, "JTI should be revoked on reuse")
	})

	t.Run("invalid refresh token: ErrInvalidRefreshToken", func(t *testing.T) {
		h := newTestHarness(t)

		mintResult, err := h.minter.MintAccessToken("user-001", "sess-001")
		require.NoError(t, err)

		refreshHash := auth.HashRefreshToken("real-token")
		session := sampleSessionRecord("user-001", "sess-001", deviceID, refreshHash, h.clock)
		h.sessionStore.getByIDFn = func(_ context.Context, _ string) (*app.SessionRecord, error) {
			return session, nil
		}

		_, err = h.svc.RefreshTokens(context.Background(), mintResult.Token, "wrong-token", deviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidRefreshToken)
	})

	t.Run("device mismatch: ErrDeviceMismatch", func(t *testing.T) {
		h := newTestHarness(t)

		mintResult, err := h.minter.MintAccessToken("user-002", "sess-001")
		require.NoError(t, err)

		refreshHash := auth.HashRefreshToken("some-token")
		session := sampleSessionRecord("user-002", "sess-001", "other-device", refreshHash, h.clock)
		h.sessionStore.getByIDFn = func(_ context.Context, _ string) (*app.SessionRecord, error) {
			return session, nil
		}

		_, err = h.svc.RefreshTokens(context.Background(), mintResult.Token, "some-token", deviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrDeviceMismatch)
	})

	t.Run("session expired: ErrSessionExpired", func(t *testing.T) {
		h := newTestHarness(t)

		mintResult, err := h.minter.MintAccessToken("user-001", "sess-002")
		require.NoError(t, err)

		refreshHash := auth.HashRefreshToken("some-token")
		session := sampleSessionRecord("user-001", "sess-002", deviceID, refreshHash, h.clock)
		// Expire the session.
		session.ExpiresAt = testStart.Add(-time.Hour).Format(time.RFC3339)

		h.sessionStore.getByIDFn = func(_ context.Context, _ string) (*app.SessionRecord, error) {
			return session, nil
		}

		_, err = h.svc.RefreshTokens(context.Background(), mintResult.Token, "some-token", deviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrSessionExpired)
	})

	t.Run("session not found: ErrSessionRevoked", func(t *testing.T) {
		h := newTestHarness(t)

		mintResult, err := h.minter.MintAccessToken("user-001", "sess-001")
		require.NoError(t, err)

		h.sessionStore.getByIDFn = func(_ context.Context, _ string) (*app.SessionRecord, error) {
			return nil, domain.ErrNotFound
		}

		_, err = h.svc.RefreshTokens(context.Background(), mintResult.Token, "some-token", deviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrSessionRevoked)
	})

	t.Run("invalid access token: ErrUnauthorized", func(t *testing.T) {
		h := newTestHarness(t)

		_, err := h.svc.RefreshTokens(context.Background(), "garbage-token", "some-refresh", deviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUnauthorized)
	})

	t.Run("GetByID non-ErrNotFound: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		errDB := errors.New("dynamo throttle")

		mintResult, err := h.minter.MintAccessToken("user-001", "sess-001")
		require.NoError(t, err)

		h.sessionStore.getByIDFn = func(_ context.Context, _ string) (*app.SessionRecord, error) {
			return nil, errDB
		}

		_, err = h.svc.RefreshTokens(context.Background(), mintResult.Token, "some-refresh", deviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errDB)
	})

	t.Run("session update failure: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)

		mintResult, err := h.minter.MintAccessToken("user-001", "sess-001")
		require.NoError(t, err)

		refreshToken := "valid-refresh-token"
		refreshHash := auth.HashRefreshToken(refreshToken)
		errDB := errors.New("provisioned throughput exceeded")

		session := sampleSessionRecord("user-001", "sess-001", deviceID, refreshHash, h.clock)
		h.sessionStore.getByIDFn = func(_ context.Context, _ string) (*app.SessionRecord, error) {
			return session, nil
		}
		h.sessionStore.updateFn = func(_ context.Context, _ string, _ app.SessionUpdate) error {
			return errDB
		}

		_, err = h.svc.RefreshTokens(context.Background(), mintResult.Token, refreshToken, deviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errDB)
	})
}
