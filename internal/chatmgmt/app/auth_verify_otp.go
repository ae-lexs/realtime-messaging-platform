package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/observability"
)

// VerifyOTP validates an OTP candidate and completes either new-user registration
// or existing-user login (ADR-015 §1.4, §2).
func (s *AuthService) VerifyOTP(ctx context.Context, phone, otpCandidate, deviceID string) (*VerifyOTPResult, error) {
	ctx, span := tracer.Start(ctx, "auth.verify_otp")
	defer span.End()

	logger := observability.WithTraceID(ctx, s.logger)

	if _, err := domain.NewPhoneNumber(phone); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if deviceID == "" {
		err := fmt.Errorf("device ID is required: %w", domain.ErrInvalidInput)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	phoneHash := auth.HashPhone(phone)

	if err := s.checkVerifyRateLimits(ctx, phoneHash); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	record, err := s.validateOTPRecord(ctx, phoneHash, otpCandidate)
	if err != nil {
		logger.InfoContext(ctx, "auth.otp_failed", "phone_hash", phoneHash)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	existingUser, findErr := s.userStore.FindByPhone(ctx, phone)
	if findErr != nil && !errors.Is(findErr, domain.ErrNotFound) {
		span.RecordError(findErr)
		span.SetStatus(codes.Error, findErr.Error())
		return nil, fmt.Errorf("find user by phone: %w", findErr)
	}

	var result *VerifyOTPResult
	if errors.Is(findErr, domain.ErrNotFound) {
		result, err = s.verifyOTPNewUser(ctx, phone, phoneHash, record, deviceID)
	} else {
		result, err = s.verifyOTPExistingUser(ctx, phoneHash, record, existingUser, deviceID)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.Bool("auth.is_new_user", result.IsNewUser))
	logger.InfoContext(ctx, "auth.otp_verified",
		"user_id", result.User.UserID,
		"session_id", result.SessionID,
		"is_new_user", result.IsNewUser,
	)

	return result, nil
}

// checkVerifyRateLimits enforces rate limits and lockout for OTP verification.
func (s *AuthService) checkVerifyRateLimits(ctx context.Context, phoneHash string) error {
	allowed, err := s.rateLimiter.CheckAndIncrement(
		ctx,
		"otp_verify:phone:"+phoneHash,
		domain.MaxOTPVerifyAttempts,
		int(domain.OTPRateLimitWindow.Seconds()),
	)
	if err != nil {
		return fmt.Errorf("check verify rate limit: %w", errors.Join(err, domain.ErrUnavailable))
	}
	if !allowed {
		rateLimitsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", "verify_otp"),
			attribute.String("limit_type", "phone"),
		))
		return domain.ErrRateLimited
	}

	locked, err := s.rateLimiter.CheckLockout(ctx, "otp_lockout:phone:"+phoneHash)
	if err != nil {
		return fmt.Errorf("check lockout: %w", errors.Join(err, domain.ErrUnavailable))
	}
	if locked {
		rateLimitsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", "verify_otp"),
			attribute.String("limit_type", "lockout"),
		))
		return domain.ErrRateLimited
	}

	return nil
}

// validateOTPRecord retrieves and validates the OTP record, including
// status, attempt count, expiry, and MAC verification.
func (s *AuthService) validateOTPRecord(ctx context.Context, phoneHash, otpCandidate string) (*OTPRecord, error) {
	record, err := s.otpStore.GetOTP(ctx, phoneHash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "invalid_otp")))
			return nil, domain.ErrInvalidOTP
		}
		return nil, fmt.Errorf("get OTP: %w", err)
	}

	if record.Status == "verified" {
		authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "invalid_otp")))
		return nil, domain.ErrInvalidOTP
	}

	if record.AttemptCount >= domain.MaxOTPVerifyAttempts {
		if lockErr := s.rateLimiter.SetLockout(ctx, "otp_lockout:phone:"+phoneHash,
			int(domain.OTPLockoutDuration.Seconds())); lockErr != nil {
			s.logger.ErrorContext(ctx, "failed to set lockout", "error", lockErr)
		}
		return nil, domain.ErrRateLimited
	}

	expiresAt, err := time.Parse(time.RFC3339, record.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("parse OTP expiry: %w", err)
	}
	if s.clock.Now().UTC().After(expiresAt) {
		authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "otp_expired")))
		return nil, domain.ErrInvalidOTP
	}

	if !auth.VerifyOTPMAC(s.pepper, otpCandidate, phoneHash, record.ExpiresAt, record.OTPMAC) {
		if incErr := s.otpStore.IncrementAttempts(ctx, phoneHash); incErr != nil {
			s.logger.ErrorContext(ctx, "failed to increment OTP attempts", "error", incErr)
		}
		authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "invalid_otp")))
		return nil, domain.ErrInvalidOTP
	}

	return record, nil
}

// verifyOTPNewUser handles registration: creates user + session in a single transaction.
func (s *AuthService) verifyOTPNewUser(
	ctx context.Context,
	phone, phoneHash string,
	record *OTPRecord,
	deviceID string,
) (*VerifyOTPResult, error) {
	userID := uuid.NewString()
	sessionID := uuid.NewString()
	now := s.clock.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	refreshToken, err := auth.GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshHash := auth.HashRefreshToken(refreshToken)

	sessionExpiry := now.Add(domain.RefreshTokenLifetime)

	params := RegistrationParams{
		PhoneHash:        phoneHash,
		OTPExpiresAt:     record.ExpiresAt,
		OTPMAC:           record.OTPMAC,
		UserID:           userID,
		PhoneNumber:      phone,
		Now:              nowStr,
		SessionID:        sessionID,
		DeviceID:         deviceID,
		RefreshTokenHash: refreshHash,
		SessionExpiresAt: sessionExpiry.Format(time.RFC3339),
		SessionTTL:       sessionExpiry.Unix(),
	}

	if txErr := s.transactor.VerifyOTPAndCreateUser(ctx, params); txErr != nil {
		if errors.Is(txErr, domain.ErrAlreadyExists) {
			user, findErr := s.userStore.FindByPhone(ctx, phone)
			if findErr != nil {
				return nil, fmt.Errorf("find user after race: %w", findErr)
			}
			return s.verifyOTPExistingUser(ctx, phoneHash, record, user, deviceID)
		}
		return nil, fmt.Errorf("register user: %w", txErr)
	}

	mintResult, err := s.minter.MintAccessToken(userID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("mint access token: %w", err)
	}

	sessionCreatedTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("flow", "registration")))
	tokenMintedTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("flow", "registration")))
	observability.WithTraceID(ctx, s.logger).InfoContext(ctx, "session.created",
		"user_id", userID,
		"session_id", sessionID,
		"flow", "registration",
	)

	return &VerifyOTPResult{
		User: UserRecord{
			UserID:      userID,
			PhoneNumber: phone,
			CreatedAt:   nowStr,
			UpdatedAt:   nowStr,
		},
		SessionID:         sessionID,
		AccessToken:       mintResult.Token,
		RefreshToken:      refreshToken,
		IsNewUser:         true,
		AccessTokenExpiry: mintResult.ExpiresAt,
	}, nil
}

// verifyOTPExistingUser handles login: enforces session limits, creates session.
func (s *AuthService) verifyOTPExistingUser(
	ctx context.Context,
	phoneHash string,
	record *OTPRecord,
	user *UserRecord,
	deviceID string,
) (*VerifyOTPResult, error) {
	sessions, err := s.sessionStore.ListByUser(ctx, user.UserID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	if err := s.evictConflictingSessions(ctx, sessions, deviceID); err != nil {
		return nil, err
	}

	if err := s.enforceSessionLimit(ctx, sessions, deviceID); err != nil {
		return nil, err
	}

	return s.createLoginSession(ctx, phoneHash, record, user, deviceID)
}

// evictConflictingSessions removes sessions bound to the same device ID.
func (s *AuthService) evictConflictingSessions(ctx context.Context, sessions []SessionRecord, deviceID string) error {
	for _, sess := range sessions {
		if sess.DeviceID == deviceID {
			if delErr := s.sessionStore.Delete(ctx, sess.SessionID); delErr != nil {
				return fmt.Errorf("delete replaced session: %w", delErr)
			}
			if revErr := s.revocationStore.Revoke(ctx, sess.SessionID); revErr != nil {
				return fmt.Errorf("revoke replaced session: %w", revErr)
			}
			sessionRevocationsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "device_conflict")))
		}
	}
	return nil
}

// enforceSessionLimit evicts the oldest sessions when the user is at the max.
func (s *AuthService) enforceSessionLimit(ctx context.Context, sessions []SessionRecord, deviceID string) error {
	activeSessions := make([]SessionRecord, 0, len(sessions))
	for _, sess := range sessions {
		if sess.DeviceID != deviceID {
			activeSessions = append(activeSessions, sess)
		}
	}

	if len(activeSessions) < domain.MaxSessionsPerUser {
		return nil
	}

	sort.Slice(activeSessions, func(i, j int) bool {
		return activeSessions[i].CreatedAt < activeSessions[j].CreatedAt
	})
	evictCount := len(activeSessions) - domain.MaxSessionsPerUser + 1
	for i := 0; i < evictCount; i++ {
		sess := activeSessions[i]
		if delErr := s.sessionStore.Delete(ctx, sess.SessionID); delErr != nil {
			return fmt.Errorf("delete evicted session: %w", delErr)
		}
		if revErr := s.revocationStore.Revoke(ctx, sess.SessionID); revErr != nil {
			return fmt.Errorf("revoke evicted session: %w", revErr)
		}
		sessionRevocationsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "session_limit")))
	}
	return nil
}

// createLoginSession generates credentials and executes the login transaction.
func (s *AuthService) createLoginSession(
	ctx context.Context,
	phoneHash string,
	record *OTPRecord,
	user *UserRecord,
	deviceID string,
) (*VerifyOTPResult, error) {
	sessionID := uuid.NewString()
	now := s.clock.Now().UTC()

	refreshToken, err := auth.GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshHash := auth.HashRefreshToken(refreshToken)

	sessionExpiry := now.Add(domain.RefreshTokenLifetime)

	params := LoginParams{
		PhoneHash:        phoneHash,
		OTPExpiresAt:     record.ExpiresAt,
		OTPMAC:           record.OTPMAC,
		SessionID:        sessionID,
		UserID:           user.UserID,
		DeviceID:         deviceID,
		RefreshTokenHash: refreshHash,
		CreatedAt:        now.Format(time.RFC3339),
		SessionExpiresAt: sessionExpiry.Format(time.RFC3339),
		SessionTTL:       sessionExpiry.Unix(),
	}

	if txErr := s.transactor.VerifyOTPAndCreateSession(ctx, params); txErr != nil {
		return nil, fmt.Errorf("create login session: %w", txErr)
	}

	mintResult, err := s.minter.MintAccessToken(user.UserID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("mint access token: %w", err)
	}

	sessionCreatedTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("flow", "login")))
	tokenMintedTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("flow", "login")))
	observability.WithTraceID(ctx, s.logger).InfoContext(ctx, "session.created",
		"user_id", user.UserID,
		"session_id", sessionID,
		"flow", "login",
	)

	return &VerifyOTPResult{
		User:              *user,
		SessionID:         sessionID,
		AccessToken:       mintResult.Token,
		RefreshToken:      refreshToken,
		IsNewUser:         false,
		AccessTokenExpiry: mintResult.ExpiresAt,
	}, nil
}
