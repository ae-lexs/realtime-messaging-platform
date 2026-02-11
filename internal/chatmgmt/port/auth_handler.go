package port

import (
	"context"
	"math"
	"strings"
	"time"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	messagingv1 "github.com/aelexs/realtime-messaging-platform/gen/messaging/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/errmap"
)

// authService is a narrow, consumer-defined interface for the auth service
// operations the handler requires. The *app.AuthService satisfies this.
type authService interface {
	RequestOTP(ctx context.Context, phone, clientIP string) (*app.RequestOTPResult, error)
	VerifyOTP(ctx context.Context, phone, otpCandidate, deviceID string) (*app.VerifyOTPResult, error)
	RefreshTokens(ctx context.Context, accessToken, refreshToken, deviceID string) (*app.RefreshResult, error)
	Logout(ctx context.Context, accessToken string) error
}

// AuthHandler implements the gRPC AuthServiceServer interface.
// It translates proto requests into app-layer calls and maps results back.
type AuthHandler struct {
	messagingv1.UnimplementedAuthServiceServer
	svc authService
}

// NewAuthHandler creates an AuthHandler backed by the given AuthService.
func NewAuthHandler(svc *app.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// RequestOTP sends a one-time password to the given phone number.
func (h *AuthHandler) RequestOTP(ctx context.Context, req *messagingv1.RequestOTPRequest) (*messagingv1.RequestOTPResponse, error) {
	clientIP := extractClientIP(ctx)

	result, err := h.svc.RequestOTP(ctx, req.GetPhoneNumber(), clientIP)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.RequestOTPResponse{
		ExpiresAt:         timeToProtoTimestamp(result.ExpiresAt),
		RetryAfterSeconds: clampInt32(result.RetryAfterSeconds),
	}, nil
}

// VerifyOTP verifies an OTP and returns authentication tokens.
func (h *AuthHandler) VerifyOTP(ctx context.Context, req *messagingv1.VerifyOTPRequest) (*messagingv1.VerifyOTPResponse, error) {
	result, err := h.svc.VerifyOTP(ctx, req.GetPhoneNumber(), req.GetOtp(), req.GetDeviceId())
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.VerifyOTPResponse{
		User: &messagingv1.AuthUser{
			UserId:        result.User.UserID,
			PhoneNumber:   result.User.PhoneNumber,
			DisplayName:   result.User.DisplayName,
			PhoneVerified: true,
			CreatedAt:     timeStringToProtoTimestamp(result.User.CreatedAt),
		},
		SessionId:            result.SessionID,
		AccessToken:          result.AccessToken,
		RefreshToken:         result.RefreshToken,
		IsNewUser:            result.IsNewUser,
		AccessTokenExpiresAt: timeToProtoTimestamp(result.AccessTokenExpiry),
	}, nil
}

// RefreshTokens exchanges a refresh token for new access and refresh tokens.
func (h *AuthHandler) RefreshTokens(ctx context.Context, req *messagingv1.RefreshTokensRequest) (*messagingv1.RefreshTokensResponse, error) {
	accessToken := extractBearerToken(ctx)
	deviceID := extractDeviceID(ctx)

	result, err := h.svc.RefreshTokens(ctx, accessToken, req.GetRefreshToken(), deviceID)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.RefreshTokensResponse{
		AccessToken:          result.AccessToken,
		RefreshToken:         result.RefreshToken,
		AccessTokenExpiresAt: timeToProtoTimestamp(result.AccessTokenExpiry),
	}, nil
}

// Logout revokes the current session.
func (h *AuthHandler) Logout(ctx context.Context, _ *messagingv1.LogoutRequest) (*messagingv1.LogoutResponse, error) {
	accessToken := extractBearerToken(ctx)

	if err := h.svc.Logout(ctx, accessToken); err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.LogoutResponse{}, nil
}

// extractBearerToken extracts the bearer token from the gRPC "authorization" metadata.
func extractBearerToken(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return ""
	}
	const prefix = "Bearer "
	if strings.HasPrefix(vals[0], prefix) {
		return vals[0][len(prefix):]
	}
	return vals[0]
}

// extractDeviceID extracts the device ID from the gRPC "x-device-id" metadata.
func extractDeviceID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get("x-device-id")
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// extractClientIP extracts the client IP from "x-forwarded-for" metadata
// or falls back to the gRPC peer address.
func extractClientIP(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		vals := md.Get("x-forwarded-for")
		if len(vals) > 0 && vals[0] != "" {
			// x-forwarded-for may contain a comma-separated list; take the first.
			if idx := strings.IndexByte(vals[0], ','); idx >= 0 {
				return strings.TrimSpace(vals[0][:idx])
			}
			return strings.TrimSpace(vals[0])
		}
	}

	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		addr := p.Addr.String()
		// Strip port from addr (e.g. "127.0.0.1:54321" → "127.0.0.1").
		if idx := strings.LastIndexByte(addr, ':'); idx >= 0 {
			return addr[:idx]
		}
		return addr
	}

	return ""
}

// clampInt32 safely converts an int to int32, clamping to math.MaxInt32 on overflow.
func clampInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v) //nolint:gosec // overflow guarded by clamp above
}

// timeToProtoTimestamp converts a time.Time to a proto Timestamp (millis since epoch).
func timeToProtoTimestamp(t time.Time) *messagingv1.Timestamp {
	return &messagingv1.Timestamp{Millis: t.UnixMilli()}
}

// timeStringToProtoTimestamp parses an RFC3339 string and converts to proto Timestamp.
// Returns a zero-millis timestamp if the string is not valid RFC3339.
func timeStringToProtoTimestamp(s string) *messagingv1.Timestamp {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return &messagingv1.Timestamp{Millis: 0}
	}
	return &messagingv1.Timestamp{Millis: t.UnixMilli()}
}
