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

// Compile-time check: SessionStore satisfies app.SessionStore.
var _ app.SessionStore = (*SessionStore)(nil)

// SessionStore persists sessions in Firestore (ADR-023, ADR-015 §4).
type SessionStore struct {
	sessions *firestore.Sessions
}

// NewSessionStore creates a SessionStore backed by the given collection.
func NewSessionStore(sessions *firestore.Sessions) *SessionStore {
	return &SessionStore{sessions: sessions}
}

// Create writes a session.
//
// Sessions created as part of a verify-otp go through AuthTransactor instead,
// so that the OTP consumption and the session share a transaction (ADR-015
// §5). This method exists for session creation outside that flow.
func (s *SessionStore) Create(ctx context.Context, session app.SessionRecord) error {
	ctx, span := tracer.Start(ctx, "firestore.session.create")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionSessions),
	)

	if err := s.sessions.Set(ctx, toSessionDoc(session)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetByID returns the session, or domain.ErrNotFound.
//
// An expired session is returned like any other. The caller must reject it —
// TTL removes it only within ~24 hours of expires_at, so it stays readable for
// up to a day (ADR-023 application-enforced invariant, checked in
// AuthService.RefreshTokens).
func (s *SessionStore) GetByID(ctx context.Context, sessionID string) (*app.SessionRecord, error) {
	ctx, span := tracer.Start(ctx, "firestore.session.get")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionSessions),
	)

	id, err := domain.NewSessionID(sessionID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("get session: %w", err)
	}

	doc, err := s.sessions.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("get session: %w", err)
	}

	record := toSessionRecord(doc)
	return &record, nil
}

// ListByUser returns every session document for a user, expired ones included
// — the input to the concurrent-session limit (ADR-015 §5.2).
func (s *SessionStore) ListByUser(ctx context.Context, userID string) ([]app.SessionRecord, error) {
	ctx, span := tracer.Start(ctx, "firestore.session.list_by_user")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionSessions),
	)

	id, err := domain.NewUserID(userID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	docs, err := s.sessions.ListByUser(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	records := make([]app.SessionRecord, 0, len(docs))
	for _, doc := range docs {
		records = append(records, toSessionRecord(doc))
	}
	return records, nil
}

// Update rotates the session's refresh token.
//
// The write is conditional on the stored hash still being the one being
// replaced (ADR-015 §4.2). That is what makes a refresh token single-use under
// concurrency: the second of two requests holding the same token finds a hash
// it does not recognise and gets domain.ErrInvalidRefreshToken, rather than
// both succeeding and the later one overwriting prev_token_hash — which is the
// evidence reuse detection reads.
func (s *SessionStore) Update(ctx context.Context, sessionID string, update app.SessionUpdate) error {
	ctx, span := tracer.Start(ctx, "firestore.session.rotate")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionSessions),
	)

	id, err := domain.NewSessionID(sessionID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("rotate session: %w", err)
	}

	rotation := firestore.SessionRotation{
		RefreshTokenHash: update.RefreshTokenHash,
		PrevTokenHash:    update.PrevTokenHash,
		TokenGeneration:  update.TokenGeneration,
		ExpiresAt:        update.ExpiresAt,
	}

	if rotateErr := s.sessions.Rotate(ctx, id, rotation); rotateErr != nil {
		span.RecordError(rotateErr)
		span.SetStatus(codes.Error, rotateErr.Error())
		return fmt.Errorf("rotate session: %w", rotateErr)
	}
	return nil
}

// Delete revokes a session immediately rather than waiting for TTL.
//
// Deleting a document that does not exist is not an error in Firestore, which
// suits logout: it is idempotent, so a client that retries after a dropped
// response gets the same answer.
func (s *SessionStore) Delete(ctx context.Context, sessionID string) error {
	ctx, span := tracer.Start(ctx, "firestore.session.delete")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionSessions),
	)

	id, err := domain.NewSessionID(sessionID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("delete session: %w", err)
	}

	if delErr := s.sessions.Delete(ctx, id); delErr != nil {
		span.RecordError(delErr)
		span.SetStatus(codes.Error, delErr.Error())
		return fmt.Errorf("delete session: %w", delErr)
	}
	return nil
}

// toSessionRecord maps the stored document onto the service's record.
func toSessionRecord(doc firestore.SessionDoc) app.SessionRecord {
	return app.SessionRecord{
		SessionID:        doc.ID,
		UserID:           doc.UserID,
		DeviceID:         doc.DeviceID,
		RefreshTokenHash: doc.RefreshTokenHash,
		PrevTokenHash:    doc.PrevTokenHash,
		TokenGeneration:  doc.TokenGeneration,
		CreatedAt:        doc.CreatedAt,
		ExpiresAt:        doc.ExpiresAt,
	}
}

// toSessionDoc maps the service's record onto the stored document.
func toSessionDoc(record app.SessionRecord) firestore.SessionDoc {
	return firestore.SessionDoc{
		ID:               record.SessionID,
		UserID:           record.UserID,
		DeviceID:         record.DeviceID,
		RefreshTokenHash: record.RefreshTokenHash,
		PrevTokenHash:    record.PrevTokenHash,
		TokenGeneration:  record.TokenGeneration,
		CreatedAt:        record.CreatedAt,
		ExpiresAt:        record.ExpiresAt,
	}
}
