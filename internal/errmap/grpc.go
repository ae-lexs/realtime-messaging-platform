// Package errmap provides wire protocol mappers for domain errors.
// Per TBD-PR0-2: every domain error has explicit gRPC, HTTP, and WebSocket mappings.
package errmap

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// grpcMappings maps domain errors to gRPC status codes.
// Order matters: first match wins (via errors.Is).
//
// Mapping follows gRPC status codes reference:
// https://grpc.github.io/grpc/core/md_doc_statuscodes.html
var grpcMappings = []struct {
	err  error
	code codes.Code
}{
	// Resource errors
	{domain.ErrNotFound, codes.NotFound},
	{domain.ErrAlreadyExists, codes.AlreadyExists},
	{domain.ErrDuplicateMessage, codes.AlreadyExists},

	// Auth errors (ADR-015) — Unauthenticated
	{domain.ErrUnauthorized, codes.Unauthenticated},
	{domain.ErrInvalidOTP, codes.Unauthenticated},
	{domain.ErrOTPExpired, codes.Unauthenticated},
	{domain.ErrDeviceMismatch, codes.Unauthenticated},
	{domain.ErrInvalidRefreshToken, codes.Unauthenticated},
	{domain.ErrRefreshTokenReuse, codes.Unauthenticated},
	{domain.ErrSessionExpired, codes.Unauthenticated},
	{domain.ErrSessionRevoked, codes.Unauthenticated},

	// Permission errors
	{domain.ErrForbidden, codes.PermissionDenied},
	{domain.ErrNotMember, codes.PermissionDenied},

	// Validation errors
	{domain.ErrInvalidInput, codes.InvalidArgument},
	{domain.ErrMessageTooLarge, codes.InvalidArgument},
	{domain.ErrInvalidContentType, codes.InvalidArgument},
	{domain.ErrEmptyID, codes.InvalidArgument},
	{domain.ErrInvalidID, codes.InvalidArgument},
	{domain.ErrInvalidPhoneNumber, codes.InvalidArgument},

	// Rate limiting / resource exhaustion
	{domain.ErrRateLimited, codes.ResourceExhausted},
	{domain.ErrPhoneRateLimited, codes.ResourceExhausted},
	{domain.ErrIPRateLimited, codes.ResourceExhausted},
	{domain.ErrMaxSessionsExceeded, codes.ResourceExhausted},
	{domain.ErrSlowConsumer, codes.ResourceExhausted},

	// Availability
	{domain.ErrUnavailable, codes.Unavailable},
}

// ToGRPCStatus converts a domain error to a gRPC status.
// The returned status can be sent directly to gRPC clients.
func ToGRPCStatus(err error) *status.Status {
	if err == nil {
		return status.New(codes.OK, "")
	}
	for _, m := range grpcMappings {
		if errors.Is(err, m.err) {
			return status.New(m.code, err.Error())
		}
	}
	// Never expose internal error details to clients
	return status.New(codes.Internal, "internal error")
}

// ToGRPCError converts a domain error to a gRPC error (implements error interface).
func ToGRPCError(err error) error {
	return ToGRPCStatus(err).Err()
}

// FromGRPCError extracts the gRPC status code from an error.
// Returns codes.Unknown if the error is not a gRPC status error.
func FromGRPCError(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	if st, ok := status.FromError(err); ok {
		return st.Code()
	}
	return codes.Unknown
}
