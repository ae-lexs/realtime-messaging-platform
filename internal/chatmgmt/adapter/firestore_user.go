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

// Compile-time check: UserStore satisfies app.UserStore.
var _ app.UserStore = (*UserStore)(nil)

// UserStore reads users from Firestore (ADR-023).
//
// It is read-only by design: users are created inside the registration
// transaction (AuthTransactor), never on their own, because a user that exists
// without its phone-number sentinel and first session would be a half-finished
// registration nothing can complete.
type UserStore struct {
	users *firestore.Users
}

// NewUserStore creates a UserStore backed by the given collection.
func NewUserStore(users *firestore.Users) *UserStore {
	return &UserStore{users: users}
}

// GetByID returns the user, or domain.ErrNotFound.
func (s *UserStore) GetByID(ctx context.Context, userID string) (*app.UserRecord, error) {
	ctx, span := tracer.Start(ctx, "firestore.user.get")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionUsers),
	)

	id, err := domain.NewUserID(userID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("get user: %w", err)
	}

	doc, err := s.users.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("get user: %w", err)
	}

	record := toUserRecord(doc)
	return &record, nil
}

// FindByPhone returns the user registered with an E.164 phone number, or
// domain.ErrNotFound.
//
// This queries users.phone_number, as ADR-023's access-pattern table specifies.
// It is not the uniqueness check: a query that matches nothing locks nothing
// inside a transaction, so phone_index is what actually enforces one user per
// number (see AuthTransactor).
func (s *UserStore) FindByPhone(ctx context.Context, phone string) (*app.UserRecord, error) {
	ctx, span := tracer.Start(ctx, "firestore.user.find_by_phone")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionUsers),
	)

	number, err := domain.NewPhoneNumber(phone)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("find user by phone: %w", err)
	}

	doc, err := s.users.FindByPhone(ctx, number)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("find user by phone: %w", err)
	}

	record := toUserRecord(doc)
	return &record, nil
}

// toUserRecord maps the stored document onto the service's record.
func toUserRecord(doc firestore.UserDoc) app.UserRecord {
	return app.UserRecord{
		UserID:      doc.ID,
		PhoneNumber: doc.PhoneNumber,
		DisplayName: doc.DisplayName,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
}
