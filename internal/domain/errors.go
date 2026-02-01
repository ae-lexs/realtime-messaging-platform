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

	// Configuration errors
	ErrConfigRequired = errors.New("required configuration key missing")
)

// IsRetryable returns true if the error represents a transient condition
// that may succeed on retry (ADR-009 Tier classification).
func IsRetryable(err error) bool {
	return errors.Is(err, ErrUnavailable) || errors.Is(err, ErrRateLimited)
}

// IsClientError returns true if the error represents a client-side issue
// that will not succeed on retry without client-side changes.
func IsClientError(err error) bool {
	return errors.Is(err, ErrInvalidInput) ||
		errors.Is(err, ErrMessageTooLarge) ||
		errors.Is(err, ErrInvalidContentType) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrNotMember) ||
		errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrUnauthorized) ||
		errors.Is(err, ErrEmptyID) ||
		errors.Is(err, ErrInvalidID)
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
