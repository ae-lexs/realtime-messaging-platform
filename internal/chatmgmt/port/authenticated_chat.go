package port

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	messagingv1 "github.com/aelexs/realtime-messaging-platform/gen/messaging/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/errmap"
)

// callerKey types the context value holding the authenticated caller, so
// nothing else in the process can write or read it by accident.
type callerKey struct{}

// Caller is the authenticated identity a chat handler acts on behalf of.
type Caller struct {
	// UserID is the token's subject — the user every authorization decision in
	// ADR-006 §8 is made about.
	UserID string

	// SessionID and JTI are carried for logging and revocation, not for
	// authorization: a chat operation is permitted by who you are and what role
	// you hold, never by which device you are on.
	SessionID string
	JTI       string
}

// CallerFrom returns the authenticated caller, and false if the context never
// passed through AuthenticatedChatServer.
//
// A handler that reads false must refuse the request. It cannot mean "an
// anonymous caller" — every chat endpoint in ADR-006 §4 requires
// authentication, so the only way to reach a handler without a caller is a
// wiring mistake, and the safe reading of a wiring mistake is denial.
func CallerFrom(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerKey{}).(Caller)
	return caller, ok
}

// revocationChecker reports whether an access token's JTI has been revoked.
// Narrow by design: this decorator needs one question answered.
type revocationChecker interface {
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

// AuthenticatedChatServer wraps a ChatMgmtServiceServer so that every method
// authenticates before it runs.
//
// **Why a decorator and not a gRPC interceptor.** The composition root serves
// REST by registering the handler with grpc-gateway *in process*
// (RegisterChatMgmtServiceHandlerServer), which calls the handler's methods
// directly rather than dialling this service's own gRPC port. A
// UnaryServerInterceptor never runs on that path. An interceptor would
// therefore have protected the gRPC port while leaving every REST route —
// which is every route ADR-006 §4 specifies — wide open, and nothing about
// the code would have looked wrong. A decorator sits where both transports
// converge: on the server implementation itself.
//
// It embeds the generated Unimplemented struct so that adding an RPC to the
// proto cannot silently produce an unauthenticated endpoint. A method that
// exists on the service but is not overridden here answers Unimplemented,
// which is a refusal; the failure mode of forgetting to wrap a new method is a
// broken endpoint rather than an open one.
type AuthenticatedChatServer struct {
	messagingv1.UnimplementedChatMgmtServiceServer

	inner     messagingv1.ChatMgmtServiceServer
	validator *auth.Validator
	revoked   revocationChecker
}

// NewAuthenticatedChatServer wraps inner so every call is authenticated.
func NewAuthenticatedChatServer(
	inner messagingv1.ChatMgmtServiceServer,
	validator *auth.Validator,
	revoked revocationChecker,
) *AuthenticatedChatServer {
	return &AuthenticatedChatServer{inner: inner, validator: validator, revoked: revoked}
}

// authenticate validates the bearer token and returns a context carrying the
// caller.
//
// Three refusals, all of them domain.ErrUnauthorized so the transport maps
// them identically — a caller learns that it may not proceed, never why, since
// distinguishing "expired" from "revoked" from "malformed" tells an attacker
// which of their guesses was closest.
//
// The revocation check is required on **every** request (ADR-013) and **fails
// closed** (ADR-013 §3.4): if Redis cannot answer, the request is denied. That
// is the whole point of revocation — a token whose session was destroyed stays
// cryptographically valid until it expires, so the only thing standing between
// a stolen token and the API is this lookup. Answering "allow" when the store
// is unreachable would make revocation advisory.
func (s *AuthenticatedChatServer) authenticate(ctx context.Context) (context.Context, error) {
	token := extractBearerToken(ctx)
	if token == "" {
		return nil, fmt.Errorf("no bearer token: %w", domain.ErrUnauthorized)
	}

	claims, err := s.validator.ValidateAccessToken(token)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrUnauthorized, err)
	}

	revoked, err := s.revoked.IsRevoked(ctx, claims.ID)
	if err != nil {
		// Fail closed. Denying on an infrastructure fault costs an authorized
		// user one retry; allowing would honour tokens the system has already
		// been told to stop honouring.
		return nil, fmt.Errorf("revocation check unavailable: %w", domain.ErrUnauthorized)
	}
	if revoked {
		return nil, fmt.Errorf("token revoked: %w", domain.ErrUnauthorized)
	}

	caller := Caller{UserID: claims.Subject, SessionID: claims.SessionID, JTI: claims.ID}

	// The caller identifies the subject of every downstream authorization
	// decision, so it belongs on the span that covers them.
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("enduser.id", caller.UserID))

	return context.WithValue(ctx, callerKey{}, caller), nil
}

// The methods below are mechanical: authenticate, then delegate. They are
// written out rather than generated because Go has no way to intercept an
// interface's methods, and because an explicit list is auditable — a reviewer
// can see that every RPC on the service is covered.

// CreateChat authenticates, then delegates.
func (s *AuthenticatedChatServer) CreateChat(
	ctx context.Context, req *messagingv1.CreateChatRequest,
) (*messagingv1.CreateChatResponse, error) {
	ctx, err := s.authenticate(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}
	return s.inner.CreateChat(ctx, req)
}

// GetChat authenticates, then delegates.
func (s *AuthenticatedChatServer) GetChat(
	ctx context.Context, req *messagingv1.GetChatRequest,
) (*messagingv1.GetChatResponse, error) {
	ctx, err := s.authenticate(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}
	return s.inner.GetChat(ctx, req)
}

// ListChats authenticates, then delegates.
func (s *AuthenticatedChatServer) ListChats(
	ctx context.Context, req *messagingv1.ListChatsRequest,
) (*messagingv1.ListChatsResponse, error) {
	ctx, err := s.authenticate(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}
	return s.inner.ListChats(ctx, req)
}
