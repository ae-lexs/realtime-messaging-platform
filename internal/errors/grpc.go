// Package errors provides wire protocol mappers for domain errors.
// Per TBD-PR0-2: every domain error has explicit gRPC, HTTP, and WebSocket mappings.
package errors

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// ToGRPCStatus converts a domain error to a gRPC status.
// The returned status can be sent directly to gRPC clients.
//
// Mapping follows gRPC status codes reference:
// https://grpc.github.io/grpc/core/md_doc_statuscodes.html
func ToGRPCStatus(err error) *status.Status {
	if err == nil {
		return status.New(codes.OK, "")
	}

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.New(codes.NotFound, err.Error())

	case errors.Is(err, domain.ErrAlreadyExists):
		return status.New(codes.AlreadyExists, err.Error())

	case errors.Is(err, domain.ErrDuplicateMessage):
		// Idempotency hit - not an error, returns existing resource
		return status.New(codes.AlreadyExists, err.Error())

	case errors.Is(err, domain.ErrUnauthorized):
		return status.New(codes.Unauthenticated, err.Error())

	case errors.Is(err, domain.ErrForbidden):
		return status.New(codes.PermissionDenied, err.Error())

	case errors.Is(err, domain.ErrNotMember):
		// Membership is a permission concept
		return status.New(codes.PermissionDenied, err.Error())

	case errors.Is(err, domain.ErrInvalidInput):
		return status.New(codes.InvalidArgument, err.Error())

	case errors.Is(err, domain.ErrMessageTooLarge):
		return status.New(codes.InvalidArgument, err.Error())

	case errors.Is(err, domain.ErrInvalidContentType):
		return status.New(codes.InvalidArgument, err.Error())

	case errors.Is(err, domain.ErrEmptyID):
		return status.New(codes.InvalidArgument, err.Error())

	case errors.Is(err, domain.ErrInvalidID):
		return status.New(codes.InvalidArgument, err.Error())

	case errors.Is(err, domain.ErrRateLimited):
		return status.New(codes.ResourceExhausted, err.Error())

	case errors.Is(err, domain.ErrSlowConsumer):
		// Client-side resource issue
		return status.New(codes.ResourceExhausted, err.Error())

	case errors.Is(err, domain.ErrUnavailable):
		return status.New(codes.Unavailable, err.Error())

	default:
		// Never expose internal error details to clients
		return status.New(codes.Internal, "internal error")
	}
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
