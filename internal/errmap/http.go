package errmap

import (
	"errors"
	"net/http"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// HTTPError represents an HTTP error response.
type HTTPError struct {
	StatusCode int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e HTTPError) Error() string {
	return e.Message
}

// httpMapping defines a domain error to HTTP status/code mapping.
type httpMapping struct {
	err        error
	statusCode int
	code       string
}

// httpMappings maps domain errors to HTTP status codes and error codes.
// Order matters: first match wins (via errors.Is).
//
// Note: grpc-gateway handles gRPC→HTTP mapping automatically.
// These mappings are for non-grpc-gateway HTTP handlers.
var httpMappings = []httpMapping{
	// Resource errors
	{domain.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
	{domain.ErrAlreadyExists, http.StatusConflict, "ALREADY_EXISTS"},
	{domain.ErrDuplicateMessage, http.StatusOK, "DUPLICATE"},

	// Auth errors — 401 (ADR-015)
	{domain.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHENTICATED"},
	{domain.ErrInvalidOTP, http.StatusUnauthorized, "INVALID_OTP"},
	{domain.ErrOTPExpired, http.StatusUnauthorized, "OTP_EXPIRED"},
	{domain.ErrDeviceMismatch, http.StatusUnauthorized, "DEVICE_MISMATCH"},
	{domain.ErrInvalidRefreshToken, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN"},
	{domain.ErrRefreshTokenReuse, http.StatusUnauthorized, "REFRESH_TOKEN_REUSE"},
	{domain.ErrSessionExpired, http.StatusUnauthorized, "SESSION_EXPIRED"},
	{domain.ErrSessionRevoked, http.StatusUnauthorized, "SESSION_REVOKED"},

	// Permission errors
	{domain.ErrForbidden, http.StatusForbidden, "PERMISSION_DENIED"},
	{domain.ErrNotMember, http.StatusForbidden, "NOT_MEMBER"},

	// Validation errors — 400
	{domain.ErrInvalidInput, http.StatusBadRequest, "INVALID_ARGUMENT"},
	{domain.ErrMessageTooLarge, http.StatusBadRequest, "INVALID_ARGUMENT"},
	{domain.ErrInvalidContentType, http.StatusBadRequest, "INVALID_ARGUMENT"},
	{domain.ErrEmptyID, http.StatusBadRequest, "INVALID_ARGUMENT"},
	{domain.ErrInvalidID, http.StatusBadRequest, "INVALID_ARGUMENT"},
	{domain.ErrInvalidPhoneNumber, http.StatusBadRequest, "INVALID_ARGUMENT"},

	// Rate limiting — 429
	{domain.ErrRateLimited, http.StatusTooManyRequests, "RATE_LIMITED"},
	{domain.ErrPhoneRateLimited, http.StatusTooManyRequests, "PHONE_RATE_LIMITED"},
	{domain.ErrIPRateLimited, http.StatusTooManyRequests, "IP_RATE_LIMITED"},
	{domain.ErrMaxSessionsExceeded, http.StatusTooManyRequests, "MAX_SESSIONS_EXCEEDED"},
	{domain.ErrSlowConsumer, http.StatusTooManyRequests, "RESOURCE_EXHAUSTED"},

	// Availability
	{domain.ErrUnavailable, http.StatusServiceUnavailable, "UNAVAILABLE"},
}

// ToHTTPError converts a domain error to an HTTP error.
func ToHTTPError(err error) HTTPError {
	if err == nil {
		return HTTPError{StatusCode: http.StatusOK}
	}
	for _, m := range httpMappings {
		if errors.Is(err, m.err) {
			return HTTPError{StatusCode: m.statusCode, Code: m.code, Message: err.Error()}
		}
	}
	// Never expose internal error details to clients
	return HTTPError{StatusCode: http.StatusInternalServerError, Code: "INTERNAL", Message: "internal error"}
}

// ToHTTPStatusCode extracts just the HTTP status code for a domain error.
func ToHTTPStatusCode(err error) int {
	return ToHTTPError(err).StatusCode
}
