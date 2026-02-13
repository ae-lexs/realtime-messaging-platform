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

const (
	testPhone    = "+15551234567"
	testDeviceID = "device-abc-123"
	testOTP      = "123456"
)

func TestVerifyOTP(t *testing.T) {
	testPhoneHash := auth.HashPhone(testPhone)

	t.Run("new user success: registration transaction + tokens minted", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		// FindByPhone returns not found → new user path.
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return nil, domain.ErrNotFound
		}

		result, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.IsNewUser)
		assert.NotEmpty(t, result.AccessToken)
		assert.NotEmpty(t, result.RefreshToken)
		assert.NotEmpty(t, result.SessionID)
		assert.Equal(t, testPhone, result.User.PhoneNumber)
	})

	t.Run("existing user success: login session created + tokens minted", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		user := sampleUserRecord()

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return user, nil
		}
		h.sessionStore.listByUserFn = func(_ context.Context, _ string) ([]app.SessionRecord, error) {
			return nil, nil
		}

		result, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsNewUser)
		assert.NotEmpty(t, result.AccessToken)
		assert.NotEmpty(t, result.RefreshToken)
		assert.Equal(t, user.UserID, result.User.UserID)
	})

	t.Run("invalid OTP code: attempt incremented + ErrInvalidOTP", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}

		incrementCalled := false
		h.otpStore.incrementAttemptsFn = func(_ context.Context, _ string) error {
			incrementCalled = true
			return nil
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, "000000", testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidOTP)
		assert.True(t, incrementCalled, "IncrementAttempts should be called on bad OTP")
	})

	t.Run("expired OTP: returns ErrInvalidOTP", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}

		// Advance clock past OTP expiry.
		h.clock.Advance(domain.OTPValidityDuration + time.Minute)

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidOTP)
	})

	t.Run("max attempts reached: lockout set + ErrRateLimited", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		record.AttemptCount = domain.MaxOTPVerifyAttempts

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}

		lockoutSet := false
		h.rateLimiter.setLockoutFn = func(_ context.Context, _ string, _ int) error {
			lockoutSet = true
			return nil
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrRateLimited)
		assert.True(t, lockoutSet, "lockout should be set on max attempts")
	})

	t.Run("phone sentinel race: falls back to existing user flow", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		user := sampleUserRecord()

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}

		// First call: not found (new user path), second call: found (after race).
		findCalls := 0
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			findCalls++
			if findCalls == 1 {
				return nil, domain.ErrNotFound
			}
			return user, nil
		}

		// Registration transaction fails with ErrAlreadyExists (phone sentinel conflict).
		h.transactor.verifyOTPAndCreateUserFn = func(_ context.Context, _ app.RegistrationParams) error {
			return domain.ErrAlreadyExists
		}
		h.sessionStore.listByUserFn = func(_ context.Context, _ string) ([]app.SessionRecord, error) {
			return nil, nil
		}

		result, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsNewUser, "should fall back to existing user flow")
		assert.Equal(t, user.UserID, result.User.UserID)
	})

	t.Run("session limit enforcement: oldest session evicted", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		user := sampleUserRecord()

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return user, nil
		}

		// Create max sessions (5) with different devices.
		sessions := make([]app.SessionRecord, domain.MaxSessionsPerUser)
		for i := range sessions {
			sessions[i] = app.SessionRecord{
				SessionID: "sess-" + string(rune('A'+i)),
				UserID:    user.UserID,
				DeviceID:  "other-device-" + string(rune('A'+i)),
				CreatedAt: testStart.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
				ExpiresAt: testStart.Add(30 * 24 * time.Hour).Format(time.RFC3339),
			}
		}
		h.sessionStore.listByUserFn = func(_ context.Context, _ string) ([]app.SessionRecord, error) {
			return sessions, nil
		}

		deletedSessions := make([]string, 0)
		h.sessionStore.deleteFn = func(_ context.Context, sessionID string) error {
			deletedSessions = append(deletedSessions, sessionID)
			return nil
		}

		result, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.NoError(t, err)
		require.NotNil(t, result)
		// Oldest session should be evicted.
		assert.Contains(t, deletedSessions, "sess-A", "oldest session should be evicted")
	})

	t.Run("device replacement: existing session with same device deleted", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		user := sampleUserRecord()

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return user, nil
		}

		// Existing session with same device_id.
		h.sessionStore.listByUserFn = func(_ context.Context, _ string) ([]app.SessionRecord, error) {
			return []app.SessionRecord{
				{
					SessionID: "old-session",
					UserID:    user.UserID,
					DeviceID:  testDeviceID,
					CreatedAt: testStart.Add(-time.Hour).Format(time.RFC3339),
				},
			}, nil
		}

		deletedSessions := make([]string, 0)
		h.sessionStore.deleteFn = func(_ context.Context, sessionID string) error {
			deletedSessions = append(deletedSessions, sessionID)
			return nil
		}

		result, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Contains(t, deletedSessions, "old-session", "old session with same device should be deleted")
	})

	t.Run("verify rate limit exceeded: returns ErrRateLimited", func(t *testing.T) {
		h := newTestHarness(t)
		h.rateLimiter.checkAndIncrementFn = func(_ context.Context, key string, _, _ int) (bool, error) {
			if key == "otp_verify:phone:"+testPhoneHash {
				return false, nil
			}
			return true, nil
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrRateLimited)
	})

	t.Run("lockout active: returns ErrRateLimited", func(t *testing.T) {
		h := newTestHarness(t)
		h.rateLimiter.checkLockoutFn = func(_ context.Context, _ string) (bool, error) {
			return true, nil
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrRateLimited)
	})

	t.Run("empty device ID: returns ErrInvalidInput", func(t *testing.T) {
		h := newTestHarness(t)

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, "")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidInput)
	})

	t.Run("OTP not found: returns ErrInvalidOTP", func(t *testing.T) {
		h := newTestHarness(t)
		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return nil, domain.ErrNotFound
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidOTP)
	})

	t.Run("already verified OTP: returns ErrInvalidOTP", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		record.Status = "verified"

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidOTP)
	})

	t.Run("FindByPhone internal error: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		errDB := errors.New("db connection lost")

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return nil, errDB
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errDB)
	})

	t.Run("ListByUser failure: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		user := sampleUserRecord()
		errDB := errors.New("dynamo throttle")

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return user, nil
		}
		h.sessionStore.listByUserFn = func(_ context.Context, _ string) ([]app.SessionRecord, error) {
			return nil, errDB
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errDB)
	})

	t.Run("evict session delete failure: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		user := sampleUserRecord()
		errDB := errors.New("conditional check failed")

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return user, nil
		}
		h.sessionStore.listByUserFn = func(_ context.Context, _ string) ([]app.SessionRecord, error) {
			return []app.SessionRecord{
				{SessionID: "old-session", UserID: user.UserID, DeviceID: testDeviceID},
			}, nil
		}
		h.sessionStore.deleteFn = func(_ context.Context, _ string) error {
			return errDB
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errDB)
	})

	t.Run("evict session revoke failure: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		user := sampleUserRecord()
		errRedis := errors.New("redis timeout")

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return user, nil
		}
		h.sessionStore.listByUserFn = func(_ context.Context, _ string) ([]app.SessionRecord, error) {
			return []app.SessionRecord{
				{SessionID: "old-session", UserID: user.UserID, DeviceID: testDeviceID},
			}, nil
		}
		h.revocationStore.revokeFn = func(_ context.Context, _ string) error {
			return errRedis
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errRedis)
	})

	t.Run("login transaction failure: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		user := sampleUserRecord()
		errTx := errors.New("transaction conflict")

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return user, nil
		}
		h.sessionStore.listByUserFn = func(_ context.Context, _ string) ([]app.SessionRecord, error) {
			return nil, nil
		}
		h.transactor.verifyOTPAndCreateSessionFn = func(_ context.Context, _ app.LoginParams) error {
			return errTx
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errTx)
	})

	t.Run("registration transaction failure (non-race): returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		errTx := errors.New("provisioned throughput exceeded")

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return nil, domain.ErrNotFound
		}
		h.transactor.verifyOTPAndCreateUserFn = func(_ context.Context, _ app.RegistrationParams) error {
			return errTx
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errTx)
	})

	t.Run("find user after registration race: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		errDB := errors.New("db unavailable")

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		findCalls := 0
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			findCalls++
			if findCalls == 1 {
				return nil, domain.ErrNotFound
			}
			return nil, errDB
		}
		h.transactor.verifyOTPAndCreateUserFn = func(_ context.Context, _ app.RegistrationParams) error {
			return domain.ErrAlreadyExists
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errDB)
	})

	t.Run("invalid phone number: returns error", func(t *testing.T) {
		h := newTestHarness(t)

		_, err := h.svc.VerifyOTP(context.Background(), "not-a-phone", testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidPhoneNumber)
	})

	t.Run("CheckAndIncrement Redis error: returns ErrUnavailable (fail-closed → 503)", func(t *testing.T) {
		h := newTestHarness(t)
		errRedis := errors.New("redis connection refused")

		h.rateLimiter.checkAndIncrementFn = func(_ context.Context, key string, _, _ int) (bool, error) {
			if key == "otp_verify:phone:"+testPhoneHash {
				return false, errRedis
			}
			return true, nil
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUnavailable)
		assert.ErrorIs(t, err, errRedis)
	})

	t.Run("CheckLockout Redis error: returns ErrUnavailable (fail-closed → 503)", func(t *testing.T) {
		h := newTestHarness(t)
		errRedis := errors.New("redis connection refused")

		h.rateLimiter.checkLockoutFn = func(_ context.Context, _ string) (bool, error) {
			return false, errRedis
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUnavailable)
		assert.ErrorIs(t, err, errRedis)
	})

	t.Run("GetOTP non-ErrNotFound: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		errDB := errors.New("dynamo throttle")

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return nil, errDB
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errDB)
	})

	t.Run("enforceSessionLimit delete failure: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		user := sampleUserRecord()
		errDB := errors.New("conditional check failed")

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return user, nil
		}

		// MaxSessionsPerUser sessions with different devices triggers eviction.
		sessions := make([]app.SessionRecord, domain.MaxSessionsPerUser)
		for i := range sessions {
			sessions[i] = app.SessionRecord{
				SessionID: "sess-" + string(rune('A'+i)),
				UserID:    user.UserID,
				DeviceID:  "other-device-" + string(rune('A'+i)),
				CreatedAt: testStart.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
				ExpiresAt: testStart.Add(30 * 24 * time.Hour).Format(time.RFC3339),
			}
		}
		h.sessionStore.listByUserFn = func(_ context.Context, _ string) ([]app.SessionRecord, error) {
			return sessions, nil
		}
		h.sessionStore.deleteFn = func(_ context.Context, _ string) error {
			return errDB
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errDB)
	})

	t.Run("enforceSessionLimit revoke failure: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		record := sampleOTPRecord(testPhoneHash, h.clock)
		user := sampleUserRecord()
		errRedis := errors.New("redis timeout")

		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return record, nil
		}
		h.userStore.findByPhoneFn = func(_ context.Context, _ string) (*app.UserRecord, error) {
			return user, nil
		}

		sessions := make([]app.SessionRecord, domain.MaxSessionsPerUser)
		for i := range sessions {
			sessions[i] = app.SessionRecord{
				SessionID: "sess-" + string(rune('A'+i)),
				UserID:    user.UserID,
				DeviceID:  "other-device-" + string(rune('A'+i)),
				CreatedAt: testStart.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
				ExpiresAt: testStart.Add(30 * 24 * time.Hour).Format(time.RFC3339),
			}
		}
		h.sessionStore.listByUserFn = func(_ context.Context, _ string) ([]app.SessionRecord, error) {
			return sessions, nil
		}
		// Delete succeeds, but revoke fails — partial failure.
		h.revocationStore.revokeFn = func(_ context.Context, _ string) error {
			return errRedis
		}

		_, err := h.svc.VerifyOTP(context.Background(), testPhone, testOTP, testDeviceID)
		require.Error(t, err)
		assert.ErrorIs(t, err, errRedis)
	})
}
