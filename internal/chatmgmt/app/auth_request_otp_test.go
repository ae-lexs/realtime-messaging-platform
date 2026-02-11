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

func TestRequestOTP(t *testing.T) {
	const validPhone = "+15551234567"
	const clientIP = "192.168.1.1"
	validPhoneHash := auth.HashPhone(validPhone)

	t.Run("success: OTP created, SMS goroutine completes, result returned", func(t *testing.T) {
		h := newTestHarness(t)

		smsSent := make(chan struct{})
		h.smsProvider.sendOTPFn = func(_ context.Context, _, _ string) error {
			close(smsSent)
			return nil
		}

		result, err := h.svc.RequestOTP(context.Background(), validPhone, clientIP)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify ownership: Wait drains the background goroutine.
		h.svc.Wait()
		select {
		case <-smsSent:
			// SMS goroutine completed before Wait returned.
		default:
			t.Fatal("Wait returned but SMS goroutine did not complete")
		}

		expectedExpiry := testStart.Add(domain.OTPValidityDuration)
		assert.Equal(t, expectedExpiry, result.ExpiresAt)
		assert.Equal(t, 60, result.RetryAfterSeconds)
	})

	t.Run("SMS goroutine survives request context cancellation", func(t *testing.T) {
		h := newTestHarness(t)

		smsSent := make(chan struct{})
		h.smsProvider.sendOTPFn = func(ctx context.Context, _, _ string) error {
			// The goroutine should receive a non-cancelled context
			// even though the request context is cancelled below.
			assert.NoError(t, ctx.Err(), "SMS context must not be cancelled")
			close(smsSent)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		result, err := h.svc.RequestOTP(ctx, validPhone, clientIP)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Cancel the request context — simulates handler returning.
		cancel()

		// The goroutine must still complete.
		h.svc.Wait()
		select {
		case <-smsSent:
		default:
			t.Fatal("SMS goroutine did not complete after request context cancellation")
		}
	})

	t.Run("invalid phone: returns ErrInvalidPhoneNumber", func(t *testing.T) {
		h := newTestHarness(t)

		_, err := h.svc.RequestOTP(context.Background(), "not-a-phone", clientIP)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidPhoneNumber)
	})

	t.Run("phone rate limited: returns ErrPhoneRateLimited", func(t *testing.T) {
		h := newTestHarness(t)
		h.rateLimiter.checkAndIncrementFn = func(_ context.Context, key string, _, _ int) (bool, error) {
			if key == "otp_req:phone:"+validPhoneHash {
				return false, nil
			}
			return true, nil
		}

		_, err := h.svc.RequestOTP(context.Background(), validPhone, clientIP)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrPhoneRateLimited)
	})

	t.Run("IP rate limited: returns ErrIPRateLimited", func(t *testing.T) {
		h := newTestHarness(t)
		h.rateLimiter.checkAndIncrementFn = func(_ context.Context, key string, _, _ int) (bool, error) {
			if key == "otp_req:ip:"+clientIP {
				return false, nil
			}
			return true, nil
		}

		_, err := h.svc.RequestOTP(context.Background(), validPhone, clientIP)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrIPRateLimited)
	})

	t.Run("IP rate limit Redis failure: proceeds (fail-open)", func(t *testing.T) {
		h := newTestHarness(t)
		h.rateLimiter.checkAndIncrementFn = func(_ context.Context, key string, _, _ int) (bool, error) {
			if key == "otp_req:ip:"+clientIP {
				return false, errors.New("redis connection refused")
			}
			return true, nil
		}

		result, err := h.svc.RequestOTP(context.Background(), validPhone, clientIP)
		require.NoError(t, err)
		require.NotNil(t, result)
		h.svc.Wait()
	})

	t.Run("active OTP exists: returns existing expiry", func(t *testing.T) {
		h := newTestHarness(t)
		existingExpiry := testStart.Add(3 * time.Minute)

		h.otpStore.createOTPFn = func(_ context.Context, _ app.OTPRecord) error {
			return domain.ErrAlreadyExists
		}
		h.otpStore.getOTPFn = func(_ context.Context, _ string) (*app.OTPRecord, error) {
			return &app.OTPRecord{
				ExpiresAt: existingExpiry.Format(time.RFC3339),
			}, nil
		}

		result, err := h.svc.RequestOTP(context.Background(), validPhone, clientIP)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, existingExpiry, result.ExpiresAt)
		assert.Equal(t, 60, result.RetryAfterSeconds)
	})

	t.Run("phone rate limit Redis failure: returns error (fail-closed)", func(t *testing.T) {
		h := newTestHarness(t)
		h.rateLimiter.checkAndIncrementFn = func(_ context.Context, key string, _, _ int) (bool, error) {
			if key == "otp_req:phone:"+validPhoneHash {
				return false, errors.New("redis connection refused")
			}
			return true, nil
		}

		_, err := h.svc.RequestOTP(context.Background(), validPhone, clientIP)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "check phone rate limit")
	})

	t.Run("OTPStore failure: returns wrapped error", func(t *testing.T) {
		h := newTestHarness(t)
		h.otpStore.createOTPFn = func(_ context.Context, _ app.OTPRecord) error {
			return errors.New("dynamodb timeout")
		}

		_, err := h.svc.RequestOTP(context.Background(), validPhone, clientIP)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create OTP")
	})
}
