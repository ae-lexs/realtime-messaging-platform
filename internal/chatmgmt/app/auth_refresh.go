package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/observability"
)

// RefreshTokens rotates tokens using the refresh token rotation protocol
// with reuse detection (ADR-015 §4.2, §4.3).
func (s *AuthService) RefreshTokens(ctx context.Context, accessToken, refreshToken, deviceID string) (*RefreshResult, error) {
	ctx, span := tracer.Start(ctx, "auth.refresh_tokens")
	defer span.End()

	logger := observability.WithTraceID(ctx, s.logger)

	// 1. Validate access token (ignore expiry — refresh flow accepts expired JWTs).
	claims, err := s.validator.ValidateIgnoreExpiry(accessToken)
	if err != nil {
		authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "invalid_token")))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("%w: %w", domain.ErrUnauthorized, err)
	}

	// 2. Look up session.
	session, err := s.sessionStore.GetByID(ctx, claims.SessionID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "session_expired")))
			span.SetStatus(codes.Error, "session not found")
			return nil, domain.ErrSessionRevoked
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("get session: %w", err)
	}

	// 3. Validate device binding.
	if session.DeviceID != deviceID {
		authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "device_mismatch")))
		span.SetStatus(codes.Error, "device mismatch")
		return nil, domain.ErrDeviceMismatch
	}

	// 4. Check session expiry.
	sessionExpiry, err := time.Parse(time.RFC3339, session.ExpiresAt)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("parse session expiry: %w", err)
	}
	if s.clock.Now().UTC().After(sessionExpiry) {
		authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "session_expired")))
		span.SetStatus(codes.Error, "session expired")
		return nil, domain.ErrSessionExpired
	}

	// 5. Check current refresh token hash.
	if auth.ValidateRefreshHash(refreshToken, session.RefreshTokenHash) {
		result, rotateErr := s.rotateRefreshToken(ctx, claims.Subject, claims.SessionID, session)
		if rotateErr != nil {
			span.RecordError(rotateErr)
			span.SetStatus(codes.Error, rotateErr.Error())
			return nil, rotateErr
		}
		logger.InfoContext(ctx, "auth.token_refreshed",
			"user_id", claims.Subject,
			"session_id", claims.SessionID,
		)
		return result, nil
	}

	// 6. Check previous token hash (reuse detection).
	if session.PrevTokenHash != "" && auth.ValidateRefreshHash(refreshToken, session.PrevTokenHash) {
		// REUSE DETECTED — revoke session immediately.
		sessionRevocationsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "reuse_detection")))
		if delErr := s.sessionStore.Delete(ctx, claims.SessionID); delErr != nil {
			logger.ErrorContext(ctx, "failed to delete session on reuse detection", "error", delErr)
		}
		if revErr := s.revocationStore.Revoke(ctx, claims.ID); revErr != nil {
			logger.ErrorContext(ctx, "failed to revoke JTI on reuse detection", "error", revErr)
		}
		authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "refresh_token_reuse")))
		logger.WarnContext(ctx, "auth.refresh_token_reuse",
			"session_id", claims.SessionID,
			"user_id", claims.Subject,
		)
		span.SetStatus(codes.Error, "refresh token reuse detected")
		return nil, domain.ErrRefreshTokenReuse
	}

	authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "invalid_refresh_token")))
	span.SetStatus(codes.Error, "invalid refresh token")
	return nil, domain.ErrInvalidRefreshToken
}

// rotateRefreshToken performs normal refresh token rotation.
func (s *AuthService) rotateRefreshToken(
	ctx context.Context,
	userID, sessionID string,
	session *SessionRecord,
) (*RefreshResult, error) {
	newRefresh, err := auth.GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	newHash := auth.HashRefreshToken(newRefresh)

	newExpiry := s.clock.Now().UTC().Add(domain.RefreshTokenLifetime)

	update := SessionUpdate{
		RefreshTokenHash: newHash,
		PrevTokenHash:    session.RefreshTokenHash,
		TokenGeneration:  session.TokenGeneration + 1,
		ExpiresAt:        newExpiry.Format(time.RFC3339),
		TTL:              newExpiry.Unix(),
	}

	if updateErr := s.sessionStore.Update(ctx, sessionID, update); updateErr != nil {
		return nil, fmt.Errorf("update session: %w", updateErr)
	}

	mintResult, err := s.minter.MintAccessToken(userID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("mint access token: %w", err)
	}

	tokenMintedTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("flow", "refresh")))

	return &RefreshResult{
		AccessToken:       mintResult.Token,
		RefreshToken:      newRefresh,
		AccessTokenExpiry: mintResult.ExpiresAt,
	}, nil
}
