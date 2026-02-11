package port

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcpeer "google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	messagingv1 "github.com/aelexs/realtime-messaging-platform/gen/messaging/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// ---------------------------------------------------------------------------
// Stub — implements authService for unit tests.
// ---------------------------------------------------------------------------

type stubAuthService struct {
	requestOTPFn    func(ctx context.Context, phone, clientIP string) (*app.RequestOTPResult, error)
	verifyOTPFn     func(ctx context.Context, phone, otpCandidate, deviceID string) (*app.VerifyOTPResult, error)
	refreshTokensFn func(ctx context.Context, accessToken, refreshToken, deviceID string) (*app.RefreshResult, error)
	logoutFn        func(ctx context.Context, accessToken string) error
}

func (s *stubAuthService) RequestOTP(ctx context.Context, phone, clientIP string) (*app.RequestOTPResult, error) {
	return s.requestOTPFn(ctx, phone, clientIP)
}

func (s *stubAuthService) VerifyOTP(ctx context.Context, phone, otpCandidate, deviceID string) (*app.VerifyOTPResult, error) {
	return s.verifyOTPFn(ctx, phone, otpCandidate, deviceID)
}

func (s *stubAuthService) RefreshTokens(ctx context.Context, accessToken, refreshToken, deviceID string) (*app.RefreshResult, error) {
	return s.refreshTokensFn(ctx, accessToken, refreshToken, deviceID)
}

func (s *stubAuthService) Logout(ctx context.Context, accessToken string) error {
	return s.logoutFn(ctx, accessToken)
}

var _ authService = (*stubAuthService)(nil)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var fixedTime = time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)

func ctxWithMetadata(md metadata.MD) context.Context {
	return metadata.NewIncomingContext(context.Background(), md)
}

// ---------------------------------------------------------------------------
// Tests — RequestOTP
// ---------------------------------------------------------------------------

func TestAuthHandler_RequestOTP(t *testing.T) {
	t.Run("success - maps result to proto response", func(t *testing.T) {
		expiresAt := fixedTime.Add(5 * time.Minute)
		stub := &stubAuthService{
			requestOTPFn: func(_ context.Context, phone, clientIP string) (*app.RequestOTPResult, error) {
				assert.Equal(t, "+14155552671", phone)
				assert.Equal(t, "10.0.0.1", clientIP)
				return &app.RequestOTPResult{
					ExpiresAt:         expiresAt,
					RetryAfterSeconds: 30,
				}, nil
			},
		}
		handler := &AuthHandler{svc: stub}

		ctx := ctxWithMetadata(metadata.Pairs("x-forwarded-for", "10.0.0.1"))
		resp, err := handler.RequestOTP(ctx, &messagingv1.RequestOTPRequest{
			PhoneNumber: "+14155552671",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, expiresAt.UnixMilli(), resp.ExpiresAt.Millis)
		assert.Equal(t, int32(30), resp.RetryAfterSeconds)
	})

	t.Run("rate limited - returns ResourceExhausted", func(t *testing.T) {
		stub := &stubAuthService{
			requestOTPFn: func(_ context.Context, _, _ string) (*app.RequestOTPResult, error) {
				return nil, domain.ErrPhoneRateLimited
			},
		}
		handler := &AuthHandler{svc: stub}

		_, err := handler.RequestOTP(context.Background(), &messagingv1.RequestOTPRequest{
			PhoneNumber: "+14155552671",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.ResourceExhausted, st.Code())
	})

	t.Run("invalid phone - returns InvalidArgument", func(t *testing.T) {
		stub := &stubAuthService{
			requestOTPFn: func(_ context.Context, _, _ string) (*app.RequestOTPResult, error) {
				return nil, domain.ErrInvalidPhoneNumber
			},
		}
		handler := &AuthHandler{svc: stub}

		_, err := handler.RequestOTP(context.Background(), &messagingv1.RequestOTPRequest{
			PhoneNumber: "bad",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

// ---------------------------------------------------------------------------
// Tests — VerifyOTP
// ---------------------------------------------------------------------------

func TestAuthHandler_VerifyOTP(t *testing.T) {
	t.Run("success - maps all fields to proto response", func(t *testing.T) {
		accessExpiry := fixedTime.Add(15 * time.Minute)
		stub := &stubAuthService{
			verifyOTPFn: func(_ context.Context, phone, otp, deviceID string) (*app.VerifyOTPResult, error) {
				assert.Equal(t, "+14155552671", phone)
				assert.Equal(t, "123456", otp)
				assert.Equal(t, "device-abc", deviceID)
				return &app.VerifyOTPResult{
					User: app.UserRecord{
						UserID:      "user-001",
						PhoneNumber: "+14155552671",
						DisplayName: "Alice",
						CreatedAt:   "2026-02-10T12:00:00Z",
					},
					SessionID:         "session-001",
					AccessToken:       "access-jwt",
					RefreshToken:      "refresh-opaque",
					IsNewUser:         true,
					AccessTokenExpiry: accessExpiry,
				}, nil
			},
		}
		handler := &AuthHandler{svc: stub}

		resp, err := handler.VerifyOTP(context.Background(), &messagingv1.VerifyOTPRequest{
			PhoneNumber: "+14155552671",
			Otp:         "123456",
			DeviceId:    "device-abc",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.User)
		assert.Equal(t, "user-001", resp.User.UserId)
		assert.Equal(t, "+14155552671", resp.User.PhoneNumber)
		assert.Equal(t, "Alice", resp.User.DisplayName)
		assert.True(t, resp.User.PhoneVerified)
		assert.Equal(t, fixedTime.UnixMilli(), resp.User.CreatedAt.Millis)
		assert.Equal(t, "session-001", resp.SessionId)
		assert.Equal(t, "access-jwt", resp.AccessToken)
		assert.Equal(t, "refresh-opaque", resp.RefreshToken)
		assert.True(t, resp.IsNewUser)
		assert.Equal(t, accessExpiry.UnixMilli(), resp.AccessTokenExpiresAt.Millis)
	})

	t.Run("invalid OTP - returns Unauthenticated", func(t *testing.T) {
		stub := &stubAuthService{
			verifyOTPFn: func(_ context.Context, _, _, _ string) (*app.VerifyOTPResult, error) {
				return nil, domain.ErrInvalidOTP
			},
		}
		handler := &AuthHandler{svc: stub}

		_, err := handler.VerifyOTP(context.Background(), &messagingv1.VerifyOTPRequest{
			PhoneNumber: "+14155552671",
			Otp:         "000000",
			DeviceId:    "device-abc",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})
}

// ---------------------------------------------------------------------------
// Tests — RefreshTokens
// ---------------------------------------------------------------------------

func TestAuthHandler_RefreshTokens(t *testing.T) {
	t.Run("success - extracts bearer and device-id from metadata", func(t *testing.T) {
		accessExpiry := fixedTime.Add(15 * time.Minute)
		stub := &stubAuthService{
			refreshTokensFn: func(_ context.Context, accessToken, refreshToken, deviceID string) (*app.RefreshResult, error) {
				assert.Equal(t, "my-access-jwt", accessToken)
				assert.Equal(t, "my-refresh-token", refreshToken)
				assert.Equal(t, "device-xyz", deviceID)
				return &app.RefreshResult{
					AccessToken:       "new-access-jwt",
					RefreshToken:      "new-refresh-token",
					AccessTokenExpiry: accessExpiry,
				}, nil
			},
		}
		handler := &AuthHandler{svc: stub}

		ctx := ctxWithMetadata(metadata.Pairs(
			"authorization", "Bearer my-access-jwt",
			"x-device-id", "device-xyz",
		))
		resp, err := handler.RefreshTokens(ctx, &messagingv1.RefreshTokensRequest{
			RefreshToken: "my-refresh-token",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "new-access-jwt", resp.AccessToken)
		assert.Equal(t, "new-refresh-token", resp.RefreshToken)
		assert.Equal(t, accessExpiry.UnixMilli(), resp.AccessTokenExpiresAt.Millis)
	})

	t.Run("token reuse - returns Unauthenticated", func(t *testing.T) {
		stub := &stubAuthService{
			refreshTokensFn: func(_ context.Context, _, _, _ string) (*app.RefreshResult, error) {
				return nil, domain.ErrRefreshTokenReuse
			},
		}
		handler := &AuthHandler{svc: stub}

		_, err := handler.RefreshTokens(context.Background(), &messagingv1.RefreshTokensRequest{
			RefreshToken: "reused-token",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})
}

// ---------------------------------------------------------------------------
// Tests — Logout
// ---------------------------------------------------------------------------

func TestAuthHandler_Logout(t *testing.T) {
	t.Run("success - extracts bearer token and returns empty response", func(t *testing.T) {
		stub := &stubAuthService{
			logoutFn: func(_ context.Context, accessToken string) error {
				assert.Equal(t, "my-access-jwt", accessToken)
				return nil
			},
		}
		handler := &AuthHandler{svc: stub}

		ctx := ctxWithMetadata(metadata.Pairs("authorization", "Bearer my-access-jwt"))
		resp, err := handler.Logout(ctx, &messagingv1.LogoutRequest{})

		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("unauthorized - returns Unauthenticated", func(t *testing.T) {
		stub := &stubAuthService{
			logoutFn: func(_ context.Context, _ string) error {
				return domain.ErrUnauthorized
			},
		}
		handler := &AuthHandler{svc: stub}

		_, err := handler.Logout(context.Background(), &messagingv1.LogoutRequest{})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})
}

// ---------------------------------------------------------------------------
// Tests — Metadata extraction helpers
// ---------------------------------------------------------------------------

func TestExtractBearerToken(t *testing.T) {
	t.Run("strips Bearer prefix", func(t *testing.T) {
		ctx := ctxWithMetadata(metadata.Pairs("authorization", "Bearer abc123"))
		assert.Equal(t, "abc123", extractBearerToken(ctx))
	})

	t.Run("returns raw value without prefix", func(t *testing.T) {
		ctx := ctxWithMetadata(metadata.Pairs("authorization", "raw-token"))
		assert.Equal(t, "raw-token", extractBearerToken(ctx))
	})

	t.Run("returns empty for missing metadata", func(t *testing.T) {
		assert.Equal(t, "", extractBearerToken(context.Background()))
	})

	t.Run("returns empty for missing authorization header", func(t *testing.T) {
		ctx := ctxWithMetadata(metadata.Pairs("other-key", "value"))
		assert.Equal(t, "", extractBearerToken(ctx))
	})
}

func TestExtractDeviceID(t *testing.T) {
	t.Run("returns device ID from metadata", func(t *testing.T) {
		ctx := ctxWithMetadata(metadata.Pairs("x-device-id", "device-abc"))
		assert.Equal(t, "device-abc", extractDeviceID(ctx))
	})

	t.Run("returns empty for missing metadata", func(t *testing.T) {
		assert.Equal(t, "", extractDeviceID(context.Background()))
	})
}

func TestExtractClientIP(t *testing.T) {
	t.Run("uses x-forwarded-for when present", func(t *testing.T) {
		ctx := ctxWithMetadata(metadata.Pairs("x-forwarded-for", "10.0.0.1"))
		assert.Equal(t, "10.0.0.1", extractClientIP(ctx))
	})

	t.Run("takes first IP from comma-separated list", func(t *testing.T) {
		ctx := ctxWithMetadata(metadata.Pairs("x-forwarded-for", "10.0.0.1, 192.168.1.1"))
		assert.Equal(t, "10.0.0.1", extractClientIP(ctx))
	})

	t.Run("falls back to peer address", func(t *testing.T) {
		ctx := grpcpeer.NewContext(context.Background(), &grpcpeer.Peer{
			Addr: &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 54321},
		})
		assert.Equal(t, "192.168.1.100", extractClientIP(ctx))
	})

	t.Run("returns empty when no metadata or peer", func(t *testing.T) {
		assert.Equal(t, "", extractClientIP(context.Background()))
	})
}

// ---------------------------------------------------------------------------
// Tests — Timestamp conversion helpers
// ---------------------------------------------------------------------------

func TestTimeToProtoTimestamp(t *testing.T) {
	ts := timeToProtoTimestamp(fixedTime)
	assert.Equal(t, fixedTime.UnixMilli(), ts.Millis)
}

func TestTimeStringToProtoTimestamp(t *testing.T) {
	t.Run("valid RFC3339", func(t *testing.T) {
		ts := timeStringToProtoTimestamp("2026-02-10T12:00:00Z")
		assert.Equal(t, fixedTime.UnixMilli(), ts.Millis)
	})

	t.Run("invalid string returns zero millis", func(t *testing.T) {
		ts := timeStringToProtoTimestamp("not-a-date")
		assert.Equal(t, int64(0), ts.Millis)
	})
}
