package errors_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/errors"
)

func TestToWebSocketClose(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   int
		wantReason string
	}{
		// Nil error
		{"nil error", nil, errors.CloseNormalClosure, "normal_closure"},

		// Authorization errors
		{"ErrUnauthorized", domain.ErrUnauthorized, errors.CloseUnauthorized, "unauthorized"},
		{"ErrForbidden", domain.ErrForbidden, errors.CloseForbidden, "forbidden"},
		{"ErrNotMember", domain.ErrNotMember, errors.CloseForbidden, "not_a_member"},

		// Resource errors
		{"ErrNotFound", domain.ErrNotFound, errors.CloseNotFound, "not_found"},
		{"ErrAlreadyExists", domain.ErrAlreadyExists, errors.CloseAlreadyExists, "already_exists"},

		// Validation errors
		{"ErrInvalidInput", domain.ErrInvalidInput, errors.CloseInvalidMessage, "invalid_message"},
		{"ErrEmptyID", domain.ErrEmptyID, errors.CloseInvalidMessage, "invalid_message"},
		{"ErrInvalidID", domain.ErrInvalidID, errors.CloseInvalidMessage, "invalid_message"},
		{"ErrMessageTooLarge", domain.ErrMessageTooLarge, errors.CloseMessageTooLarge, "message_too_large"},
		{"ErrInvalidContentType", domain.ErrInvalidContentType, errors.CloseInvalidMessage, "invalid_content_type"},

		// Operational errors
		{"ErrRateLimited", domain.ErrRateLimited, errors.CloseRateLimited, "rate_limited"},
		{"ErrSlowConsumer", domain.ErrSlowConsumer, errors.CloseRateLimited, "slow_consumer"},
		{"ErrUnavailable", domain.ErrUnavailable, errors.CloseTryAgainLater, "service_unavailable"},

		// Idempotency
		{"ErrDuplicateMessage", domain.ErrDuplicateMessage, errors.CloseAlreadyExists, "duplicate_message"},

		// Wrapped errors
		{"wrapped ErrNotFound", fmt.Errorf("chat: %w", domain.ErrNotFound), errors.CloseNotFound, "not_found"},
		{"wrapped ErrNotMember", fmt.Errorf("user: %w", domain.ErrNotMember), errors.CloseForbidden, "not_a_member"},

		// Unknown errors map to Internal
		{"unknown error", fmt.Errorf("unexpected"), errors.CloseInternalError, "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errors.ToWebSocketClose(tt.err)
			assert.Equal(t, tt.wantCode, got.Code, "expected code %d, got %d", tt.wantCode, got.Code)
			assert.Equal(t, tt.wantReason, got.Reason, "expected reason %q, got %q", tt.wantReason, got.Reason)
		})
	}
}

func TestWebSocketCloseCodes(t *testing.T) {
	t.Run("standard codes are in valid range", func(t *testing.T) {
		standardCodes := []int{
			errors.CloseNormalClosure,
			errors.CloseGoingAway,
			errors.CloseProtocolError,
			errors.ClosePolicyViolation,
			errors.CloseInternalError,
			errors.CloseServiceRestart,
			errors.CloseTryAgainLater,
		}

		for _, code := range standardCodes {
			assert.True(t, code >= 1000 && code <= 1015, "standard code %d should be in range 1000-1015", code)
		}
	})

	t.Run("application codes are in valid range", func(t *testing.T) {
		appCodes := []int{
			errors.CloseInvalidMessage,
			errors.CloseUnauthorized,
			errors.CloseForbidden,
			errors.CloseNotFound,
			errors.CloseAlreadyExists,
			errors.CloseMessageTooLarge,
			errors.CloseRateLimited,
		}

		for _, code := range appCodes {
			assert.True(t, code >= 4000 && code <= 4999, "app code %d should be in range 4000-4999", code)
		}
	})
}

func TestCommonCloseReasons(t *testing.T) {
	t.Run("CloseTokenExpired", func(t *testing.T) {
		assert.Equal(t, errors.CloseUnauthorized, errors.CloseTokenExpired.Code)
		assert.Equal(t, "token_expired", errors.CloseTokenExpired.Reason)
	})

	t.Run("CloseServerShutdown", func(t *testing.T) {
		assert.Equal(t, errors.CloseGoingAway, errors.CloseServerShutdown.Code)
		assert.Equal(t, "server_shutdown", errors.CloseServerShutdown.Reason)
	})

	t.Run("CloseProtocolViolation", func(t *testing.T) {
		assert.Equal(t, errors.CloseProtocolError, errors.CloseProtocolViolation.Code)
		assert.Equal(t, "protocol_error", errors.CloseProtocolViolation.Reason)
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
	}

	for _, err := range domainErrors {
		t.Run(err.Error(), func(t *testing.T) {
			wsClose := errors.ToWebSocketClose(err)
			// All domain errors should have explicit mappings (not internal_error)
			assert.NotEqual(t, "internal_error", wsClose.Reason,
				"domain error %q should have explicit WebSocket mapping", err.Error())
		})
	}
}
