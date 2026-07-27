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

const otpRetryAfterSeconds = 60

// RequestOTP validates the phone number, enforces rate limits, generates an OTP,
// stores it, and fires SMS delivery (ADR-015 §1).
func (s *AuthService) RequestOTP(ctx context.Context, phone, clientIP string) (*RequestOTPResult, error) {
	ctx, span := tracer.Start(ctx, "auth.request_otp")
	defer span.End()

	logger := observability.WithTraceID(ctx, s.logger)

	// 1. Validate E.164 phone number.
	if _, err := domain.NewPhoneNumber(phone); err != nil {
		authFailuresTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "invalid_phone")))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	phoneHash := auth.HashPhone(phone)

	// 2. Rate limit: phone (fail-closed per ADR-013).
	allowed, err := s.rateLimiter.CheckAndIncrement(
		ctx,
		"otp_req:phone:"+phoneHash,
		domain.OTPRequestRateLimitPerPhone,
		int(domain.OTPRateLimitWindow.Seconds()),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("check phone rate limit: %w", errors.Join(err, domain.ErrUnavailable))
	}
	if !allowed {
		rateLimitsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", "request_otp"),
			attribute.String("limit_type", "phone"),
		))
		span.SetStatus(codes.Error, "phone rate limited")
		return nil, domain.ErrPhoneRateLimited
	}

	// 3. Rate limit: IP (fail-open — log and continue if Redis fails).
	ipAllowed, ipErr := s.rateLimiter.CheckAndIncrement(
		ctx,
		"otp_req:ip:"+clientIP,
		domain.OTPRequestRateLimitPerIP,
		int(domain.OTPRateLimitWindow.Seconds()),
	)
	if ipErr != nil {
		logger.WarnContext(ctx, "ip rate limit check failed, proceeding (fail-open)",
			"error", ipErr, "client_ip", clientIP)
	} else if !ipAllowed {
		rateLimitsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", "request_otp"),
			attribute.String("limit_type", "ip"),
		))
		span.SetStatus(codes.Error, "IP rate limited")
		return nil, domain.ErrIPRateLimited
	}

	// 4. Generate OTP and compute MAC.
	otp, err := auth.GenerateOTP()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("generate OTP: %w", err)
	}

	now := s.clock.Now().UTC()

	// Truncated to the second because that is what the store holds and what
	// the MAC is computed over (auth.OTPMACTime): keeping sub-second precision
	// here would only make the expiry reported to the client disagree with the
	// one the OTP is actually bound to.
	expiresAt := now.Add(domain.OTPValidityDuration).Truncate(time.Second)

	mac := auth.ComputeOTPMAC(s.pepper, otp, phoneHash, expiresAt)

	// 5. Store OTP record (conditional write — fails if a live OTP exists).
	record := OTPRecord{
		PhoneHash: phoneHash,
		OTPMAC:    mac,
		Status:    domain.OTPStatusPending,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}

	if err := s.otpStore.CreateOTP(ctx, record); err != nil {
		// 6. A live OTP already exists. Report when it expires and send
		//    nothing: the code already in flight stays the valid one, which is
		//    the property ADR-015 §1.2 wanted from re-sending, without the
		//    recoverable-ciphertext machinery it dropped in v1.1.
		if errors.Is(err, domain.ErrAlreadyExists) {
			existing, getErr := s.otpStore.GetOTP(ctx, phoneHash)
			if getErr != nil {
				span.RecordError(getErr)
				span.SetStatus(codes.Error, getErr.Error())
				return nil, fmt.Errorf("get existing OTP: %w", getErr)
			}
			otpRequestsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "existing")))
			return &RequestOTPResult{
				ExpiresAt:         existing.ExpiresAt,
				RetryAfterSeconds: otpRetryAfterSeconds,
			}, nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("create OTP: %w", err)
	}

	// 7. Background SMS delivery — owned by AuthService via bgWG.
	// Detach from request context so cancellation of the HTTP request
	// does not kill the in-flight send. WithoutCancel preserves trace
	// values for structured logging.
	smsCtx := context.WithoutCancel(ctx)
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		if sendErr := s.smsProvider.SendOTP(smsCtx, phone, otp); sendErr != nil {
			s.logger.ErrorContext(smsCtx, "failed to send OTP SMS",
				"error", sendErr, "phone_hash", phoneHash)
		}
	}()

	otpRequestsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "success")))
	logger.InfoContext(ctx, "auth.otp_requested", "phone_hash", phoneHash)

	return &RequestOTPResult{
		ExpiresAt:         expiresAt,
		RetryAfterSeconds: otpRetryAfterSeconds,
	}, nil
}
