package errmap

import (
	"errors"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// WebSocket close codes per RFC 6455.
// Standard codes: https://datatracker.ietf.org/doc/html/rfc6455#section-7.4
// Application-specific codes use the 4000-4999 range.
const (
	// Standard codes (RFC 6455)
	CloseNormalClosure   = 1000
	CloseGoingAway       = 1001
	CloseProtocolError   = 1002
	ClosePolicyViolation = 1008
	CloseInternalError   = 1011
	CloseServiceRestart  = 1012
	CloseTryAgainLater   = 1013

	// Application-specific codes (4000-4999)
	CloseInvalidMessage  = 4000
	CloseUnauthorized    = 4001
	CloseForbidden       = 4003
	CloseNotFound        = 4004
	CloseAlreadyExists   = 4009
	CloseMessageTooLarge = 4013
	CloseRateLimited     = 4029
)

// WebSocketClose represents a close code and reason for WebSocket termination.
type WebSocketClose struct {
	Code   int
	Reason string
}

// ToWebSocketClose converts a domain error to a WebSocket close code and reason.
func ToWebSocketClose(err error) WebSocketClose {
	if err == nil {
		return WebSocketClose{Code: CloseNormalClosure, Reason: "normal_closure"}
	}

	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		return WebSocketClose{Code: CloseUnauthorized, Reason: "unauthorized"}

	case errors.Is(err, domain.ErrForbidden):
		return WebSocketClose{Code: CloseForbidden, Reason: "forbidden"}

	case errors.Is(err, domain.ErrNotMember):
		return WebSocketClose{Code: CloseForbidden, Reason: "not_a_member"}

	case errors.Is(err, domain.ErrNotFound):
		return WebSocketClose{Code: CloseNotFound, Reason: "not_found"}

	case errors.Is(err, domain.ErrAlreadyExists):
		return WebSocketClose{Code: CloseAlreadyExists, Reason: "already_exists"}

	case errors.Is(err, domain.ErrDuplicateMessage):
		// Idempotency hit - not an error, but we include it for completeness
		return WebSocketClose{Code: CloseAlreadyExists, Reason: "duplicate_message"}

	case errors.Is(err, domain.ErrInvalidInput):
		return WebSocketClose{Code: CloseInvalidMessage, Reason: "invalid_message"}

	case errors.Is(err, domain.ErrEmptyID):
		return WebSocketClose{Code: CloseInvalidMessage, Reason: "invalid_message"}

	case errors.Is(err, domain.ErrInvalidID):
		return WebSocketClose{Code: CloseInvalidMessage, Reason: "invalid_message"}

	case errors.Is(err, domain.ErrMessageTooLarge):
		return WebSocketClose{Code: CloseMessageTooLarge, Reason: "message_too_large"}

	case errors.Is(err, domain.ErrInvalidContentType):
		return WebSocketClose{Code: CloseInvalidMessage, Reason: "invalid_content_type"}

	case errors.Is(err, domain.ErrRateLimited):
		return WebSocketClose{Code: CloseRateLimited, Reason: "rate_limited"}

	case errors.Is(err, domain.ErrSlowConsumer):
		return WebSocketClose{Code: CloseRateLimited, Reason: "slow_consumer"}

	case errors.Is(err, domain.ErrUnavailable):
		return WebSocketClose{Code: CloseTryAgainLater, Reason: "service_unavailable"}

	default:
		return WebSocketClose{Code: CloseInternalError, Reason: "internal_error"}
	}
}

// Common close reasons for special cases not directly mapped to domain errors.
var (
	CloseTokenExpired      = WebSocketClose{Code: CloseUnauthorized, Reason: "token_expired"}
	CloseServerShutdown    = WebSocketClose{Code: CloseGoingAway, Reason: "server_shutdown"}
	CloseProtocolViolation = WebSocketClose{Code: CloseProtocolError, Reason: "protocol_error"}
)
