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

// ToHTTPError converts a domain error to an HTTP error.
// Note: grpc-gateway handles gRPC→HTTP mapping automatically.
// This function is for non-grpc-gateway HTTP handlers.
func ToHTTPError(err error) HTTPError {
	if err == nil {
		return HTTPError{StatusCode: http.StatusOK}
	}

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return HTTPError{
			StatusCode: http.StatusNotFound,
			Code:       "NOT_FOUND",
			Message:    err.Error(),
		}

	case errors.Is(err, domain.ErrAlreadyExists):
		return HTTPError{
			StatusCode: http.StatusConflict,
			Code:       "ALREADY_EXISTS",
			Message:    err.Error(),
		}

	case errors.Is(err, domain.ErrDuplicateMessage):
		// Idempotency hit - return success with original data
		return HTTPError{
			StatusCode: http.StatusOK,
			Code:       "DUPLICATE",
			Message:    err.Error(),
		}

	case errors.Is(err, domain.ErrUnauthorized):
		return HTTPError{
			StatusCode: http.StatusUnauthorized,
			Code:       "UNAUTHENTICATED",
			Message:    err.Error(),
		}

	case errors.Is(err, domain.ErrForbidden):
		return HTTPError{
			StatusCode: http.StatusForbidden,
			Code:       "PERMISSION_DENIED",
			Message:    err.Error(),
		}

	case errors.Is(err, domain.ErrNotMember):
		return HTTPError{
			StatusCode: http.StatusForbidden,
			Code:       "NOT_MEMBER",
			Message:    err.Error(),
		}

	case errors.Is(err, domain.ErrInvalidInput),
		errors.Is(err, domain.ErrMessageTooLarge),
		errors.Is(err, domain.ErrInvalidContentType),
		errors.Is(err, domain.ErrEmptyID),
		errors.Is(err, domain.ErrInvalidID):
		return HTTPError{
			StatusCode: http.StatusBadRequest,
			Code:       "INVALID_ARGUMENT",
			Message:    err.Error(),
		}

	case errors.Is(err, domain.ErrRateLimited):
		return HTTPError{
			StatusCode: http.StatusTooManyRequests,
			Code:       "RATE_LIMITED",
			Message:    err.Error(),
		}

	case errors.Is(err, domain.ErrSlowConsumer):
		return HTTPError{
			StatusCode: http.StatusTooManyRequests,
			Code:       "RESOURCE_EXHAUSTED",
			Message:    err.Error(),
		}

	case errors.Is(err, domain.ErrUnavailable):
		return HTTPError{
			StatusCode: http.StatusServiceUnavailable,
			Code:       "UNAVAILABLE",
			Message:    err.Error(),
		}

	default:
		// Never expose internal error details to clients
		return HTTPError{
			StatusCode: http.StatusInternalServerError,
			Code:       "INTERNAL",
			Message:    "internal error",
		}
	}
}

// ToHTTPStatusCode extracts just the HTTP status code for a domain error.
func ToHTTPStatusCode(err error) int {
	return ToHTTPError(err).StatusCode
}
