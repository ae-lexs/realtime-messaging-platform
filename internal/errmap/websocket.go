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

// wsMappings maps domain errors to WebSocket close codes and reasons.
// Order matters: first match wins (via errors.Is).
var wsMappings = []struct {
	err    error
	code   int
	reason string
}{
	// Auth errors — unauthorized (ADR-015)
	{domain.ErrUnauthorized, CloseUnauthorized, "unauthorized"},
	{domain.ErrInvalidOTP, CloseUnauthorized, "invalid_otp"},
	{domain.ErrOTPExpired, CloseUnauthorized, "otp_expired"},
	{domain.ErrDeviceMismatch, CloseUnauthorized, "device_mismatch"},
	{domain.ErrInvalidRefreshToken, CloseUnauthorized, "invalid_refresh_token"},
	{domain.ErrRefreshTokenReuse, CloseUnauthorized, "refresh_token_reuse"},
	{domain.ErrSessionExpired, CloseUnauthorized, "session_expired"},
	{domain.ErrSessionRevoked, CloseUnauthorized, "session_revoked"},

	// Permission errors
	{domain.ErrForbidden, CloseForbidden, "forbidden"},
	{domain.ErrNotMember, CloseForbidden, "not_a_member"},

	// Resource errors
	{domain.ErrNotFound, CloseNotFound, "not_found"},
	{domain.ErrAlreadyExists, CloseAlreadyExists, "already_exists"},
	{domain.ErrDuplicateMessage, CloseAlreadyExists, "duplicate_message"},

	// Validation errors
	{domain.ErrInvalidInput, CloseInvalidMessage, "invalid_message"},
	{domain.ErrEmptyID, CloseInvalidMessage, "invalid_message"},
	{domain.ErrInvalidID, CloseInvalidMessage, "invalid_message"},
	{domain.ErrInvalidPhoneNumber, CloseInvalidMessage, "invalid_phone_number"},
	{domain.ErrMessageTooLarge, CloseMessageTooLarge, "message_too_large"},
	{domain.ErrInvalidContentType, CloseInvalidMessage, "invalid_content_type"},

	// Rate limiting
	{domain.ErrRateLimited, CloseRateLimited, "rate_limited"},
	{domain.ErrPhoneRateLimited, CloseRateLimited, "phone_rate_limited"},
	{domain.ErrIPRateLimited, CloseRateLimited, "ip_rate_limited"},
	{domain.ErrMaxSessionsExceeded, CloseRateLimited, "max_sessions_exceeded"},
	{domain.ErrSlowConsumer, CloseRateLimited, "slow_consumer"},

	// Availability
	{domain.ErrUnavailable, CloseTryAgainLater, "service_unavailable"},
}

// ToWebSocketClose converts a domain error to a WebSocket close code and reason.
func ToWebSocketClose(err error) WebSocketClose {
	if err == nil {
		return WebSocketClose{Code: CloseNormalClosure, Reason: "normal_closure"}
	}
	for _, m := range wsMappings {
		if errors.Is(err, m.err) {
			return WebSocketClose{Code: m.code, Reason: m.reason}
		}
	}
	return WebSocketClose{Code: CloseInternalError, Reason: "internal_error"}
}

// Common close reasons for special cases not directly mapped to domain errors.
var (
	CloseTokenExpired      = WebSocketClose{Code: CloseUnauthorized, Reason: "token_expired"}
	CloseServerShutdown    = WebSocketClose{Code: CloseGoingAway, Reason: "server_shutdown"}
	CloseProtocolViolation = WebSocketClose{Code: CloseProtocolError, Reason: "protocol_error"}
)
