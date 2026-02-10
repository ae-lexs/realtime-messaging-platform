package errmap_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/errmap"
)

func TestToGRPCStatus(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		// Nil error
		{"nil error", nil, codes.OK},

		// Resource errors
		{"ErrNotFound", domain.ErrNotFound, codes.NotFound},
		{"ErrAlreadyExists", domain.ErrAlreadyExists, codes.AlreadyExists},

		// Authorization errors
		{"ErrUnauthorized", domain.ErrUnauthorized, codes.Unauthenticated},
		{"ErrForbidden", domain.ErrForbidden, codes.PermissionDenied},
		{"ErrNotMember", domain.ErrNotMember, codes.PermissionDenied},

		// Validation errors
		{"ErrInvalidInput", domain.ErrInvalidInput, codes.InvalidArgument},
		{"ErrMessageTooLarge", domain.ErrMessageTooLarge, codes.InvalidArgument},
		{"ErrInvalidContentType", domain.ErrInvalidContentType, codes.InvalidArgument},
		{"ErrEmptyID", domain.ErrEmptyID, codes.InvalidArgument},
		{"ErrInvalidID", domain.ErrInvalidID, codes.InvalidArgument},

		// Auth errors (ADR-015)
		{"ErrInvalidOTP", domain.ErrInvalidOTP, codes.Unauthenticated},
		{"ErrOTPExpired", domain.ErrOTPExpired, codes.Unauthenticated},
		{"ErrDeviceMismatch", domain.ErrDeviceMismatch, codes.Unauthenticated},
		{"ErrInvalidRefreshToken", domain.ErrInvalidRefreshToken, codes.Unauthenticated},
		{"ErrRefreshTokenReuse", domain.ErrRefreshTokenReuse, codes.Unauthenticated},
		{"ErrSessionExpired", domain.ErrSessionExpired, codes.Unauthenticated},
		{"ErrSessionRevoked", domain.ErrSessionRevoked, codes.Unauthenticated},
		{"ErrInvalidPhoneNumber", domain.ErrInvalidPhoneNumber, codes.InvalidArgument},
		{"ErrPhoneRateLimited", domain.ErrPhoneRateLimited, codes.ResourceExhausted},
		{"ErrIPRateLimited", domain.ErrIPRateLimited, codes.ResourceExhausted},
		{"ErrMaxSessionsExceeded", domain.ErrMaxSessionsExceeded, codes.ResourceExhausted},

		// Operational errors
		{"ErrRateLimited", domain.ErrRateLimited, codes.ResourceExhausted},
		{"ErrSlowConsumer", domain.ErrSlowConsumer, codes.ResourceExhausted},
		{"ErrUnavailable", domain.ErrUnavailable, codes.Unavailable},

		// Idempotency
		{"ErrDuplicateMessage", domain.ErrDuplicateMessage, codes.AlreadyExists},

		// Wrapped errors (via %w) must map to correct codes
		{"wrapped ErrNotFound", fmt.Errorf("chat %s: %w", "123", domain.ErrNotFound), codes.NotFound},
		{"wrapped ErrNotMember", fmt.Errorf("user not in chat: %w", domain.ErrNotMember), codes.PermissionDenied},
		{"wrapped ErrUnauthorized", fmt.Errorf("token expired: %w", domain.ErrUnauthorized), codes.Unauthenticated},

		// Unknown errors map to Internal
		{"unknown error", fmt.Errorf("something unexpected"), codes.Internal},
		{"stdlib error", fmt.Errorf("connection refused"), codes.Internal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errmap.ToGRPCStatus(tt.err)
			assert.Equal(t, tt.wantCode, got.Code(), "expected code %v, got %v", tt.wantCode, got.Code())
		})
	}
}

func TestToGRPCError(t *testing.T) {
	t.Run("returns nil for nil error", func(t *testing.T) {
		got := errmap.ToGRPCError(nil)
		assert.Nil(t, got)
	})

	t.Run("returns error for non-nil", func(t *testing.T) {
		got := errmap.ToGRPCError(domain.ErrNotFound)
		assert.NotNil(t, got)
		assert.Equal(t, codes.NotFound, errmap.FromGRPCError(got))
	})
}

func TestFromGRPCError(t *testing.T) {
	t.Run("returns OK for nil", func(t *testing.T) {
		got := errmap.FromGRPCError(nil)
		assert.Equal(t, codes.OK, got)
	})

	t.Run("extracts code from gRPC error", func(t *testing.T) {
		grpcErr := errmap.ToGRPCError(domain.ErrNotFound)
		got := errmap.FromGRPCError(grpcErr)
		assert.Equal(t, codes.NotFound, got)
	})

	t.Run("returns Unknown for non-gRPC error", func(t *testing.T) {
		got := errmap.FromGRPCError(fmt.Errorf("regular error"))
		assert.Equal(t, codes.Unknown, got)
	})
}

// TestGRPCMappingCompleteness ensures every domain error has an explicit mapping.
// This test will fail if a new domain error is added without updating the mapper.
func TestGRPCMappingCompleteness(t *testing.T) {
	// All sentinel errors from domain/errors.go
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
		domain.ErrConfigRequired,
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
			status := errmap.ToGRPCStatus(err)
			// ErrConfigRequired is internal, so it maps to Internal
			// All others should NOT map to Internal (they should have explicit mappings)
			if !errors.Is(err, domain.ErrConfigRequired) {
				assert.NotEqual(t, codes.Internal, status.Code(),
					"domain error %q should have explicit gRPC mapping, not Internal", err.Error())
			}
		})
	}
}
