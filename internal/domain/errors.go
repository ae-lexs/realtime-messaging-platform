package domain

import "errors"

// Sentinel errors for domain error conditions.
// Use errors.Is() for matching - never compare error strings.
var (
	// ID validation errors
	ErrEmptyID   = errors.New("ID cannot be empty")
	ErrInvalidID = errors.New("invalid ID format")

	// Resource errors
	ErrNotFound      = errors.New("resource not found")
	ErrAlreadyExists = errors.New("resource already exists")

	// Authorization errors
	ErrUnauthorized = errors.New("authentication required")
	ErrForbidden    = errors.New("permission denied")
	ErrNotMember    = errors.New("user is not a member of this chat")

	// Validation errors
	ErrInvalidInput       = errors.New("invalid input")
	ErrMessageTooLarge    = errors.New("message exceeds size limit")
	ErrInvalidContentType = errors.New("unsupported content type")

	// Operational errors
	ErrRateLimited  = errors.New("rate limit exceeded")
	ErrUnavailable  = errors.New("service temporarily unavailable")
	ErrSlowConsumer = errors.New("client not consuming messages fast enough")

	// Idempotency signal (not semantically a failure - indicates successful deduplication)
	// Returns HTTP 200/gRPC OK with the original message; included here for mapper completeness
	ErrDuplicateMessage = errors.New("duplicate client_message_id")

	// Auth errors (ADR-015)
	ErrInvalidOTP          = errors.New("invalid OTP")
	ErrOTPExpired          = errors.New("OTP has expired")
	ErrDeviceMismatch      = errors.New("device ID does not match session")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrRefreshTokenReuse   = errors.New("refresh token reuse detected")
	ErrSessionExpired      = errors.New("session has expired")
	ErrSessionRevoked      = errors.New("session has been revoked")
	ErrMaxSessionsExceeded = errors.New("maximum concurrent sessions exceeded")
	ErrPhoneRateLimited    = errors.New("phone number rate limit exceeded")
	ErrIPRateLimited       = errors.New("IP address rate limit exceeded")
	ErrInvalidPhoneNumber  = errors.New("invalid phone number format")

	// Configuration errors
	ErrConfigRequired = errors.New("required configuration key missing")
)

// IsRetryable returns true if the error represents a transient condition
// that may succeed on retry (ADR-009 Tier classification).
func IsRetryable(err error) bool {
	return errors.Is(err, ErrUnavailable) ||
		errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrPhoneRateLimited) ||
		errors.Is(err, ErrIPRateLimited) ||
		errors.Is(err, ErrMaxSessionsExceeded)
}

// clientErrors enumerates all domain errors that represent client-side issues.
var clientErrors = []error{
	ErrInvalidInput,
	ErrMessageTooLarge,
	ErrInvalidContentType,
	ErrNotFound,
	ErrNotMember,
	ErrForbidden,
	ErrUnauthorized,
	ErrEmptyID,
	ErrInvalidID,
	ErrInvalidOTP,
	ErrOTPExpired,
	ErrDeviceMismatch,
	ErrInvalidRefreshToken,
	ErrRefreshTokenReuse,
	ErrSessionExpired,
	ErrSessionRevoked,
	ErrInvalidPhoneNumber,
}

// IsClientError returns true if the error represents a client-side issue
// that will not succeed on retry without client-side changes.
func IsClientError(err error) bool {
	for _, target := range clientErrors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// IsPermissionDenied returns true if the error represents a permission issue.
func IsPermissionDenied(err error) bool {
	return errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrNotMember) ||
		errors.Is(err, ErrUnauthorized)
}

// IsNotFound returns true if the error represents a missing resource.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
