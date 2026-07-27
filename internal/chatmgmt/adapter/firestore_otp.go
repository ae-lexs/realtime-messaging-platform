package adapter

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// Compile-time check: OTPStore satisfies app.OTPStore.
var _ app.OTPStore = (*OTPStore)(nil)

// OTPStore persists OTP requests in Firestore (ADR-023 v1.2).
type OTPStore struct {
	requests *firestore.OTPRequests
	clock    domain.Clock
}

// NewOTPStore creates an OTPStore backed by the given collection.
//
// It takes a clock because the conditional write needs to know whether the
// stored OTP is still live, and "live" is a question about now — the store
// cannot answer it from the document alone.
func NewOTPStore(requests *firestore.OTPRequests, clock domain.Clock) *OTPStore {
	return &OTPStore{requests: requests, clock: clock}
}

// CreateOTP issues an OTP, returning domain.ErrAlreadyExists if a live one is
// already outstanding for the phone number (ADR-015 §1.2).
func (s *OTPStore) CreateOTP(ctx context.Context, record app.OTPRecord) error {
	ctx, span := tracer.Start(ctx, "firestore.otp.create")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionOTPRequests),
	)

	doc := firestore.NewOTPRequestDoc(record.PhoneHash, record.OTPMAC, record.CreatedAt, record.ExpiresAt)

	if err := s.requests.Create(ctx, doc, s.clock.Now().UTC()); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("create OTP: %w", err)
	}
	return nil
}

// GetOTP returns the stored OTP request, or domain.ErrNotFound.
//
// An expired record is returned like any other: the caller decides, because
// the TTL policy collects it only within ~24 hours and existence therefore
// says nothing about validity (ADR-023 v1.2).
func (s *OTPStore) GetOTP(ctx context.Context, phoneHash string) (*app.OTPRecord, error) {
	ctx, span := tracer.Start(ctx, "firestore.otp.get")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionOTPRequests),
	)

	doc, err := s.requests.Get(ctx, phoneHash)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("get OTP: %w", err)
	}

	record := toOTPRecord(doc)
	return &record, nil
}

// IncrementAttempts records a failed verification against the OTP.
func (s *OTPStore) IncrementAttempts(ctx context.Context, phoneHash string) error {
	ctx, span := tracer.Start(ctx, "firestore.otp.increment_attempts")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionOTPRequests),
	)

	if err := s.requests.IncrementAttempts(ctx, phoneHash); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("increment OTP attempts: %w", err)
	}
	return nil
}

// toOTPRecord maps the stored document onto the service's record.
func toOTPRecord(doc firestore.OTPRequestDoc) app.OTPRecord {
	return app.OTPRecord{
		PhoneHash:    doc.ID,
		OTPMAC:       doc.OTPMAC,
		Status:       doc.Status,
		AttemptCount: doc.AttemptCount,
		CreatedAt:    doc.CreatedAt,
		ExpiresAt:    doc.ExpiresAt,
	}
}
