package errmap_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/errmap"
)

func TestToWebSocketClose(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   int
		wantReason string
	}{
		// Nil error
		{"nil error", nil, errmap.CloseNormalClosure, "normal_closure"},

		// Authorization errors
		{"ErrUnauthorized", domain.ErrUnauthorized, errmap.CloseUnauthorized, "unauthorized"},
		{"ErrForbidden", domain.ErrForbidden, errmap.CloseForbidden, "forbidden"},
		{"ErrNotMember", domain.ErrNotMember, errmap.CloseForbidden, "not_a_member"},

		// Auth errors (ADR-015)
		{"ErrInvalidOTP", domain.ErrInvalidOTP, errmap.CloseUnauthorized, "invalid_otp"},
		{"ErrOTPExpired", domain.ErrOTPExpired, errmap.CloseUnauthorized, "otp_expired"},
		{"ErrDeviceMismatch", domain.ErrDeviceMismatch, errmap.CloseUnauthorized, "device_mismatch"},
		{"ErrInvalidRefreshToken", domain.ErrInvalidRefreshToken, errmap.CloseUnauthorized, "invalid_refresh_token"},
		{"ErrRefreshTokenReuse", domain.ErrRefreshTokenReuse, errmap.CloseUnauthorized, "refresh_token_reuse"},
		{"ErrSessionExpired", domain.ErrSessionExpired, errmap.CloseUnauthorized, "session_expired"},
		{"ErrSessionRevoked", domain.ErrSessionRevoked, errmap.CloseUnauthorized, "session_revoked"},
		{"ErrInvalidPhoneNumber", domain.ErrInvalidPhoneNumber, errmap.CloseInvalidMessage, "invalid_phone_number"},
		{"ErrPhoneRateLimited", domain.ErrPhoneRateLimited, errmap.CloseRateLimited, "phone_rate_limited"},
		{"ErrIPRateLimited", domain.ErrIPRateLimited, errmap.CloseRateLimited, "ip_rate_limited"},
		{"ErrMaxSessionsExceeded", domain.ErrMaxSessionsExceeded, errmap.CloseRateLimited, "max_sessions_exceeded"},

		// Resource errors
		{"ErrNotFound", domain.ErrNotFound, errmap.CloseNotFound, "not_found"},
		{"ErrAlreadyExists", domain.ErrAlreadyExists, errmap.CloseAlreadyExists, "already_exists"},

		// Validation errors
		{"ErrInvalidInput", domain.ErrInvalidInput, errmap.CloseInvalidMessage, "invalid_message"},
		{"ErrEmptyID", domain.ErrEmptyID, errmap.CloseInvalidMessage, "invalid_message"},
		{"ErrInvalidID", domain.ErrInvalidID, errmap.CloseInvalidMessage, "invalid_message"},
		{"ErrMessageTooLarge", domain.ErrMessageTooLarge, errmap.CloseMessageTooLarge, "message_too_large"},
		{"ErrInvalidContentType", domain.ErrInvalidContentType, errmap.CloseInvalidMessage, "invalid_content_type"},

		// Operational errors
		{"ErrRateLimited", domain.ErrRateLimited, errmap.CloseRateLimited, "rate_limited"},
		{"ErrSlowConsumer", domain.ErrSlowConsumer, errmap.CloseRateLimited, "slow_consumer"},
		{"ErrUnavailable", domain.ErrUnavailable, errmap.CloseTryAgainLater, "service_unavailable"},

		// Idempotency
		{"ErrDuplicateMessage", domain.ErrDuplicateMessage, errmap.CloseAlreadyExists, "duplicate_message"},

		// Wrapped errors
		{"wrapped ErrNotFound", fmt.Errorf("chat: %w", domain.ErrNotFound), errmap.CloseNotFound, "not_found"},
		{"wrapped ErrNotMember", fmt.Errorf("user: %w", domain.ErrNotMember), errmap.CloseForbidden, "not_a_member"},

		// Unknown errors map to Internal
		{"unknown error", fmt.Errorf("unexpected"), errmap.CloseInternalError, "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errmap.ToWebSocketClose(tt.err)
			assert.Equal(t, tt.wantCode, got.Code, "expected code %d, got %d", tt.wantCode, got.Code)
			assert.Equal(t, tt.wantReason, got.Reason, "expected reason %q, got %q", tt.wantReason, got.Reason)
		})
	}
}

func TestWebSocketCloseCodes(t *testing.T) {
	t.Run("standard codes are in valid range", func(t *testing.T) {
		standardCodes := []int{
			errmap.CloseNormalClosure,
			errmap.CloseGoingAway,
			errmap.CloseProtocolError,
			errmap.ClosePolicyViolation,
			errmap.CloseInternalError,
			errmap.CloseServiceRestart,
			errmap.CloseTryAgainLater,
		}

		for _, code := range standardCodes {
			assert.True(t, code >= 1000 && code <= 1015, "standard code %d should be in range 1000-1015", code)
		}
	})

	t.Run("application codes are in valid range", func(t *testing.T) {
		appCodes := []int{
			errmap.CloseInvalidMessage,
			errmap.CloseUnauthorized,
			errmap.CloseForbidden,
			errmap.CloseNotFound,
			errmap.CloseAlreadyExists,
			errmap.CloseMessageTooLarge,
			errmap.CloseRateLimited,
		}

		for _, code := range appCodes {
			assert.True(t, code >= 4000 && code <= 4999, "app code %d should be in range 4000-4999", code)
		}
	})
}

func TestCommonCloseReasons(t *testing.T) {
	t.Run("CloseTokenExpired", func(t *testing.T) {
		assert.Equal(t, errmap.CloseUnauthorized, errmap.CloseTokenExpired.Code)
		assert.Equal(t, "token_expired", errmap.CloseTokenExpired.Reason)
	})

	t.Run("CloseServerShutdown", func(t *testing.T) {
		assert.Equal(t, errmap.CloseGoingAway, errmap.CloseServerShutdown.Code)
		assert.Equal(t, "server_shutdown", errmap.CloseServerShutdown.Reason)
	})

	t.Run("CloseProtocolViolation", func(t *testing.T) {
		assert.Equal(t, errmap.CloseProtocolError, errmap.CloseProtocolViolation.Code)
		assert.Equal(t, "protocol_error", errmap.CloseProtocolViolation.Reason)
	})
}

// TestWebSocketMappingCompleteness ensures every domain error has an explicit mapping.
func TestWebSocketMappingCompleteness(t *testing.T) {
	domainErrors := []error{
		domain.ErrEmptyID,
		domain.ErrInvalidID,
		domain.ErrNotFound,
		domain.ErrAlreadyExists,
		domain.ErrUnauthorized,
		domain.ErrForbidden,
		domain.ErrNotMember,
		domain.ErrInvalidInput,
		domain.ErrMessageTooLarge,
		domain.ErrInvalidContentType,
		domain.ErrRateLimited,
		domain.ErrUnavailable,
		domain.ErrSlowConsumer,
		domain.ErrDuplicateMessage,
		// Auth errors (ADR-015)
		domain.ErrInvalidOTP,
		domain.ErrOTPExpired,
		domain.ErrDeviceMismatch,
		domain.ErrInvalidRefreshToken,
		domain.ErrRefreshTokenReuse,
		domain.ErrSessionExpired,
		domain.ErrSessionRevoked,
		domain.ErrMaxSessionsExceeded,
		domain.ErrPhoneRateLimited,
		domain.ErrIPRateLimited,
		domain.ErrInvalidPhoneNumber,
	}

	for _, err := range domainErrors {
		t.Run(err.Error(), func(t *testing.T) {
			wsClose := errmap.ToWebSocketClose(err)
			// All domain errors should have explicit mappings (not internal_error)
			assert.NotEqual(t, "internal_error", wsClose.Reason,
				"domain error %q should have explicit WebSocket mapping", err.Error())
		})
	}
}
