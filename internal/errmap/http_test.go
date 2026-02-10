package errmap_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/errmap"
)

func TestToHTTPError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantStatusCode int
		wantCode       string
	}{
		// Nil error
		{"nil error", nil, http.StatusOK, ""},

		// Resource errors
		{"ErrNotFound", domain.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"ErrAlreadyExists", domain.ErrAlreadyExists, http.StatusConflict, "ALREADY_EXISTS"},

		// Authorization errors
		{"ErrUnauthorized", domain.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHENTICATED"},
		{"ErrForbidden", domain.ErrForbidden, http.StatusForbidden, "PERMISSION_DENIED"},
		{"ErrNotMember", domain.ErrNotMember, http.StatusForbidden, "NOT_MEMBER"},

		// Validation errors
		{"ErrInvalidInput", domain.ErrInvalidInput, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"ErrMessageTooLarge", domain.ErrMessageTooLarge, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"ErrInvalidContentType", domain.ErrInvalidContentType, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"ErrEmptyID", domain.ErrEmptyID, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"ErrInvalidID", domain.ErrInvalidID, http.StatusBadRequest, "INVALID_ARGUMENT"},

		// Auth errors (ADR-015)
		{"ErrInvalidOTP", domain.ErrInvalidOTP, http.StatusUnauthorized, "INVALID_OTP"},
		{"ErrOTPExpired", domain.ErrOTPExpired, http.StatusUnauthorized, "OTP_EXPIRED"},
		{"ErrDeviceMismatch", domain.ErrDeviceMismatch, http.StatusUnauthorized, "DEVICE_MISMATCH"},
		{"ErrInvalidRefreshToken", domain.ErrInvalidRefreshToken, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN"},
		{"ErrRefreshTokenReuse", domain.ErrRefreshTokenReuse, http.StatusUnauthorized, "REFRESH_TOKEN_REUSE"},
		{"ErrSessionExpired", domain.ErrSessionExpired, http.StatusUnauthorized, "SESSION_EXPIRED"},
		{"ErrSessionRevoked", domain.ErrSessionRevoked, http.StatusUnauthorized, "SESSION_REVOKED"},
		{"ErrInvalidPhoneNumber", domain.ErrInvalidPhoneNumber, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"ErrPhoneRateLimited", domain.ErrPhoneRateLimited, http.StatusTooManyRequests, "PHONE_RATE_LIMITED"},
		{"ErrIPRateLimited", domain.ErrIPRateLimited, http.StatusTooManyRequests, "IP_RATE_LIMITED"},
		{"ErrMaxSessionsExceeded", domain.ErrMaxSessionsExceeded, http.StatusTooManyRequests, "MAX_SESSIONS_EXCEEDED"},

		// Operational errors
		{"ErrRateLimited", domain.ErrRateLimited, http.StatusTooManyRequests, "RATE_LIMITED"},
		{"ErrSlowConsumer", domain.ErrSlowConsumer, http.StatusTooManyRequests, "RESOURCE_EXHAUSTED"},
		{"ErrUnavailable", domain.ErrUnavailable, http.StatusServiceUnavailable, "UNAVAILABLE"},

		// Idempotency - returns OK (not error)
		{"ErrDuplicateMessage", domain.ErrDuplicateMessage, http.StatusOK, "DUPLICATE"},

		// Wrapped errors
		{"wrapped ErrNotFound", fmt.Errorf("chat: %w", domain.ErrNotFound), http.StatusNotFound, "NOT_FOUND"},

		// Unknown errors map to Internal
		{"unknown error", fmt.Errorf("unexpected"), http.StatusInternalServerError, "INTERNAL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errmap.ToHTTPError(tt.err)
			assert.Equal(t, tt.wantStatusCode, got.StatusCode, "expected status %d, got %d", tt.wantStatusCode, got.StatusCode)
			assert.Equal(t, tt.wantCode, got.Code, "expected code %q, got %q", tt.wantCode, got.Code)
		})
	}
}

func TestToHTTPStatusCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, http.StatusOK},
		{"not found", domain.ErrNotFound, http.StatusNotFound},
		{"unauthorized", domain.ErrUnauthorized, http.StatusUnauthorized},
		{"rate limited", domain.ErrRateLimited, http.StatusTooManyRequests},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errmap.ToHTTPStatusCode(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHTTPErrorImplementsError(t *testing.T) {
	httpErr := errmap.ToHTTPError(domain.ErrNotFound)
	var err error = httpErr
	assert.NotEmpty(t, err.Error())
}

// TestHTTPMappingMatchesGRPCGateway verifies our HTTP mappings align with grpc-gateway defaults.
// Reference: https://github.com/grpc-ecosystem/grpc-gateway/blob/main/runtime/errors.go
func TestHTTPMappingMatchesGRPCGateway(t *testing.T) {
	// grpc-gateway default mappings
	grpcGatewayDefaults := map[int]int{
		// gRPC code -> HTTP status
		// codes.OK: 200
		// codes.InvalidArgument: 400
		// codes.Unauthenticated: 401
		// codes.PermissionDenied: 403
		// codes.NotFound: 404
		// codes.AlreadyExists: 409
		// codes.ResourceExhausted: 429
		// codes.Unavailable: 503
		// codes.Internal: 500
	}

	testCases := []struct {
		err                error
		expectedHTTPStatus int
	}{
		{domain.ErrInvalidInput, http.StatusBadRequest},        // InvalidArgument -> 400
		{domain.ErrUnauthorized, http.StatusUnauthorized},      // Unauthenticated -> 401
		{domain.ErrForbidden, http.StatusForbidden},            // PermissionDenied -> 403
		{domain.ErrNotFound, http.StatusNotFound},              // NotFound -> 404
		{domain.ErrAlreadyExists, http.StatusConflict},         // AlreadyExists -> 409
		{domain.ErrRateLimited, http.StatusTooManyRequests},    // ResourceExhausted -> 429
		{domain.ErrUnavailable, http.StatusServiceUnavailable}, // Unavailable -> 503
	}

	_ = grpcGatewayDefaults // silence unused warning

	for _, tc := range testCases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			got := errmap.ToHTTPStatusCode(tc.err)
			assert.Equal(t, tc.expectedHTTPStatus, got,
				"HTTP mapping should match grpc-gateway defaults for %v", tc.err)
		})
	}
}
