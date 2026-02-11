package app

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/observability"
)

// Logout invalidates the session and revokes the access token's JTI (ADR-015 §5).
func (s *AuthService) Logout(ctx context.Context, accessToken string) error {
	ctx, span := tracer.Start(ctx, "auth.logout")
	defer span.End()

	logger := observability.WithTraceID(ctx, s.logger)

	// 1. Validate access token (full validation including expiry).
	claims, err := s.validator.ValidateAccessToken(accessToken)
	if err != nil {
		authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "invalid_token")))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("%w: %w", domain.ErrUnauthorized, err)
	}

	// 2. Delete session (idempotent — ignore not-found).
	if err := s.sessionStore.Delete(ctx, claims.SessionID); err != nil {
		logger.ErrorContext(ctx, "failed to delete session on logout",
			"error", err, "session_id", claims.SessionID)
	}

	// 3. Revoke JTI.
	if err := s.revocationStore.Revoke(ctx, claims.ID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("revoke JTI: %w", err)
	}

	sessionRevocationsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "logout")))
	logger.InfoContext(ctx, "auth.logout",
		"user_id", claims.Subject,
		"session_id", claims.SessionID,
	)

	return nil
}
