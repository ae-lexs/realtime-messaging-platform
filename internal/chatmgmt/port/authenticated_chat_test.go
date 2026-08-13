package port_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	messagingv1 "github.com/aelexs/realtime-messaging-platform/gen/messaging/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/port"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/domain/domaintest"
)

// spyChatServer records whether it was reached and what caller it saw. A fake
// rather than a mock: the assertion is on what arrived, not on how it was
// called.
type spyChatServer struct {
	messagingv1.UnimplementedChatMgmtServiceServer

	called bool
	caller port.Caller
	hadOne bool
}

func (s *spyChatServer) CreateChat(
	ctx context.Context, _ *messagingv1.CreateChatRequest,
) (*messagingv1.CreateChatResponse, error) {
	s.called = true
	s.caller, s.hadOne = port.CallerFrom(ctx)
	return &messagingv1.CreateChatResponse{}, nil
}

// fakeRevocations answers the one question the decorator asks. `err` models the
// store being unreachable, which is the case the fail-closed rule exists for.
type fakeRevocations struct {
	revoked bool
	err     error
}

func (f fakeRevocations) IsRevoked(context.Context, string) (bool, error) {
	return f.revoked, f.err
}

type authFixture struct {
	server *port.AuthenticatedChatServer
	inner  *spyChatServer
	token  string
	userID string
}

func newAuthFixture(t *testing.T, revocations fakeRevocations) authFixture {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	clock := domaintest.NewFakeClock(time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC))
	keyStore := auth.NewStaticKeyStore(key, "test-key-001")

	minter := auth.NewMinter(auth.MinterConfig{
		KeyStore:  keyStore,
		AccessTTL: time.Hour,
		Issuer:    "messaging-platform",
		Audience:  "messaging-api",
		Clock:     clock,
	})
	validator := auth.NewValidator(auth.ValidatorConfig{
		KeyStore: keyStore,
		Issuer:   "messaging-platform",
		Audience: "messaging-api",
		Clock:    clock,
	})

	userID := domain.GenerateUserID().String()
	minted, err := minter.MintAccessToken(userID, domain.GenerateSessionID().String())
	require.NoError(t, err)

	inner := &spyChatServer{}
	return authFixture{
		server: port.NewAuthenticatedChatServer(inner, validator, revocations),
		inner:  inner,
		token:  minted.Token,
		userID: userID,
	}
}

// ctxWithToken puts a bearer token where both transports deliver it: gRPC
// metadata. The REST bridge lands here too, because IncomingHeaderMatcher
// forwards the Authorization header into this key.
func ctxWithToken(token string) context.Context {
	return metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("authorization", "Bearer "+token),
	)
}

func TestAuthenticatedChatServerAcceptsAValidToken(t *testing.T) {
	// Arrange
	f := newAuthFixture(t, fakeRevocations{})

	// Act
	_, err := f.server.CreateChat(ctxWithToken(f.token), &messagingv1.CreateChatRequest{})

	// Assert
	require.NoError(t, err)
	assert.True(t, f.inner.called, "the wrapped server must be reached")
	require.True(t, f.inner.hadOne, "the caller must be injected into the context")
	assert.Equal(t, f.userID, f.inner.caller.UserID,
		"the caller must come from the token's subject, never the request body")
}

func TestAuthenticatedChatServerRefusals(t *testing.T) {
	tests := []struct {
		name        string
		ctx         func(f authFixture) context.Context
		revocations fakeRevocations
		reason      string
	}{
		{
			name:   "no metadata at all",
			ctx:    func(authFixture) context.Context { return context.Background() },
			reason: "a call with no bearer token is not anonymous, it is unauthorized",
		},
		{
			name:   "a token that is not a token",
			ctx:    func(authFixture) context.Context { return ctxWithToken("not-a-jwt") },
			reason: "garbage must not reach the service",
		},
		{
			name:        "a revoked token",
			ctx:         func(f authFixture) context.Context { return ctxWithToken(f.token) },
			revocations: fakeRevocations{revoked: true},
			reason:      "a token whose session was destroyed stays cryptographically valid; this is what stops it",
		},
		{
			// The case the fail-closed rule exists for (ADR-013 §3.4). If Redis
			// is unreachable and the decorator answered "allow", revocation
			// would become advisory: every revoked token in the wild would work
			// again for the rest of its lifetime, precisely during an incident.
			name:        "the revocation store is unreachable",
			ctx:         func(f authFixture) context.Context { return ctxWithToken(f.token) },
			revocations: fakeRevocations{err: errors.New("redis unreachable")},
			reason:      "an unavailable revocation store must deny, not allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			f := newAuthFixture(t, tt.revocations)

			// Act
			_, err := f.server.CreateChat(tt.ctx(f), &messagingv1.CreateChatRequest{})

			// Assert
			require.Error(t, err, tt.reason)
			assert.Equal(t, codes.Unauthenticated, status.Code(err),
				"every refusal maps to the same code, so a caller learns nothing about which guess was closest")
			assert.False(t, f.inner.called, "the wrapped server must never be reached: %s", tt.reason)
		})
	}
}

// TestAuthenticatedChatServerCoversEveryImplementedRPC guards the decorator's
// whole premise.
//
// The wrapper is only protection if it wraps everything. A method added to the
// handler but not to the decorator would fall through to the embedded
// Unimplemented — a refusal, which is the safe direction — but a method added
// to the decorator that forgets to authenticate would not. This asserts the
// three shipped RPCs all refuse an unauthenticated call, so a future edit that
// drops the authenticate() line from one of them fails here.
func TestAuthenticatedChatServerCoversEveryImplementedRPC(t *testing.T) {
	f := newAuthFixture(t, fakeRevocations{})
	ctx := context.Background() // no token

	calls := map[string]func() error{
		"CreateChat": func() error {
			_, err := f.server.CreateChat(ctx, &messagingv1.CreateChatRequest{})
			return err
		},
		"GetChat": func() error {
			_, err := f.server.GetChat(ctx, &messagingv1.GetChatRequest{})
			return err
		},
		"ListChats": func() error {
			_, err := f.server.ListChats(ctx, &messagingv1.ListChatsRequest{})
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			require.Error(t, err)
			assert.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}
}

func TestCallerFromReportsAbsence(t *testing.T) {
	// Arrange — a context that never passed through the decorator.
	_, ok := port.CallerFrom(context.Background())

	// Assert — the handler relies on this being false to fail closed rather
	// than acting for a user with an empty ID.
	assert.False(t, ok)
}
