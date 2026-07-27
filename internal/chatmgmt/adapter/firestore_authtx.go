package adapter

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// Compile-time check: AuthTransactor satisfies app.AuthTransactor.
var _ app.AuthTransactor = (*AuthTransactor)(nil)

// AuthTransactor commits the verify-otp write sets atomically (ADR-015 §5).
type AuthTransactor struct {
	tx *firestore.AuthTx
}

// NewAuthTransactor creates an AuthTransactor backed by the given writer.
func NewAuthTransactor(tx *firestore.AuthTx) *AuthTransactor {
	return &AuthTransactor{tx: tx}
}

// VerifyOTPAndCreateUser commits a first-time registration: the phone-number
// sentinel, the user, their first session and the OTP consumption, all or none.
//
// Returns domain.ErrAlreadyExists when the number was claimed by a concurrent
// registration. The caller treats that as "someone else won" and continues as
// a login (ADR-015 §5.1).
func (t *AuthTransactor) VerifyOTPAndCreateUser(ctx context.Context, params app.RegistrationParams) error {
	ctx, span := tracer.Start(ctx, "firestore.auth.register")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.operation", "transaction"),
	)

	registration := firestore.Registration{
		OTP: otpCondition(params.PhoneHash, params.OTPMAC, params.Now),
		PhoneIndex: firestore.PhoneIndexDoc{
			ID:        params.PhoneHash,
			UserID:    params.UserID,
			CreatedAt: params.Now,
		},
		User: firestore.UserDoc{
			ID:          params.UserID,
			PhoneNumber: params.PhoneNumber,
			// Reaching this transaction means the OTP verified, which is the
			// only way a number is ever proven in this system.
			PhoneVerified: true,
			CreatedAt:     params.Now,
			UpdatedAt:     params.Now,
		},
		Session: firestore.SessionDoc{
			ID:               params.SessionID,
			UserID:           params.UserID,
			DeviceID:         params.DeviceID,
			RefreshTokenHash: params.RefreshTokenHash,
			TokenGeneration:  1,
			CreatedAt:        params.Now,
			ExpiresAt:        params.SessionExpiresAt,
		},
	}

	if err := t.tx.Register(ctx, registration); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("register user: %w", err)
	}
	return nil
}

// VerifyOTPAndCreateSession commits a returning user's login: the session and
// the OTP consumption, all or none.
func (t *AuthTransactor) VerifyOTPAndCreateSession(ctx context.Context, params app.LoginParams) error {
	ctx, span := tracer.Start(ctx, "firestore.auth.login")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.operation", "transaction"),
	)

	login := firestore.Login{
		OTP: otpCondition(params.PhoneHash, params.OTPMAC, params.CreatedAt),
		Session: firestore.SessionDoc{
			ID:               params.SessionID,
			UserID:           params.UserID,
			DeviceID:         params.DeviceID,
			RefreshTokenHash: params.RefreshTokenHash,
			TokenGeneration:  1,
			CreatedAt:        params.CreatedAt,
			ExpiresAt:        params.SessionExpiresAt,
		},
	}

	if err := t.tx.Login(ctx, login); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("create login session: %w", err)
	}
	return nil
}

// otpCondition builds the assertion the transaction re-checks under isolation.
//
// The service has already validated the OTP against its own read; this guards
// the window between that read and the commit, where a concurrent verification
// of the same code could otherwise consume it twice.
func otpCondition(phoneHash, otpMAC string, now time.Time) firestore.OTPCondition {
	return firestore.OTPCondition{
		PhoneHash:   phoneHash,
		OTPMAC:      otpMAC,
		Now:         now.UTC(),
		MaxAttempts: domain.MaxOTPVerifyAttempts,
	}
}
