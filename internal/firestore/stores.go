package firestore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// The stores below implement exactly the Firestore rows of ADR-023's
// access-pattern table and nothing beyond them. Every query is a single-field
// equality match, which Firestore's automatic indexes serve — this schema needs
// no composite index and no manual index configuration, which is the main thing
// it bought over the three DynamoDB GSIs it replaced.

// Users reads and writes the `users` collection.
type Users struct{ client *Client }

// NewUsers returns the users store.
func NewUsers(client *Client) *Users { return &Users{client: client} }

// Get returns the user, or domain.ErrNotFound.
func (s *Users) Get(ctx context.Context, userID domain.UserID) (UserDoc, error) {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	snapshot, err := s.client.FS.Collection(CollectionUsers).Doc(userID.String()).Get(ctx)
	if err != nil {
		return UserDoc{}, wrapErr("get user", userID.String(), err)
	}

	var doc UserDoc
	if err := snapshot.DataTo(&doc); err != nil {
		return UserDoc{}, fmt.Errorf("firestore: decode user %s: %w", userID.String(), err)
	}
	doc.ID = snapshot.Ref.ID

	return doc, nil
}

// FindByPhone returns the user registered with an E.164 phone number, or
// domain.ErrNotFound. This replaced DynamoDB's phone_number-index GSI with a
// plain equality query on an automatic index.
func (s *Users) FindByPhone(ctx context.Context, phone domain.PhoneNumber) (UserDoc, error) {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	snapshots, err := s.client.FS.Collection(CollectionUsers).
		Where("phone_number", "==", phone.String()).
		Limit(1).
		Documents(ctx).
		GetAll()
	if err != nil {
		return UserDoc{}, fmt.Errorf("firestore: find user by phone: %w", err)
	}
	if len(snapshots) == 0 {
		return UserDoc{}, fmt.Errorf("firestore: no user with that phone number: %w", domain.ErrNotFound)
	}

	var doc UserDoc
	if err := snapshots[0].DataTo(&doc); err != nil {
		return UserDoc{}, fmt.Errorf("firestore: decode user %s: %w", snapshots[0].Ref.ID, err)
	}
	doc.ID = snapshots[0].Ref.ID

	return doc, nil
}

// Set writes the user document at doc.ID, replacing any existing one.
func (s *Users) Set(ctx context.Context, doc UserDoc) error {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	if err := doc.Validate(); err != nil {
		return err
	}

	if _, err := s.client.FS.Collection(CollectionUsers).Doc(doc.ID).Set(ctx, doc); err != nil {
		return fmt.Errorf("firestore: set user %s: %w", doc.ID, err)
	}
	return nil
}

// Chats reads and writes the `chats` collection.
type Chats struct{ client *Client }

// NewChats returns the chats store.
func NewChats(client *Client) *Chats { return &Chats{client: client} }

// Get returns the chat, or domain.ErrNotFound.
func (s *Chats) Get(ctx context.Context, chatID domain.ChatID) (ChatDoc, error) {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	snapshot, err := s.client.FS.Collection(CollectionChats).Doc(chatID.String()).Get(ctx)
	if err != nil {
		return ChatDoc{}, wrapErr("get chat", chatID.String(), err)
	}

	var doc ChatDoc
	if err := snapshot.DataTo(&doc); err != nil {
		return ChatDoc{}, fmt.Errorf("firestore: decode chat %s: %w", chatID.String(), err)
	}
	doc.ID = snapshot.Ref.ID

	return doc, nil
}

// Set writes the chat document at doc.ID, replacing any existing one.
func (s *Chats) Set(ctx context.Context, doc ChatDoc) error {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	if err := doc.Validate(); err != nil {
		return err
	}

	if _, err := s.client.FS.Collection(CollectionChats).Doc(doc.ID).Set(ctx, doc); err != nil {
		return fmt.Errorf("firestore: set chat %s: %w", doc.ID, err)
	}
	return nil
}

// Memberships reads and writes the `memberships` collection, addressed by the
// deterministic composite ID from MembershipDocID.
type Memberships struct{ client *Client }

// NewMemberships returns the memberships store.
func NewMemberships(client *Client) *Memberships { return &Memberships{client: client} }

// Get answers "is this user in this chat?" as a strongly consistent point
// lookup, returning domain.ErrNotFound when they are not.
func (s *Memberships) Get(ctx context.Context, chatID domain.ChatID, userID domain.UserID) (MembershipDoc, error) {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	docID := MembershipDocID(chatID, userID)

	snapshot, err := s.client.FS.Collection(CollectionMemberships).Doc(docID).Get(ctx)
	if err != nil {
		return MembershipDoc{}, wrapErr("get membership", docID, err)
	}

	doc, err := membershipFrom(snapshot)
	if err != nil {
		return MembershipDoc{}, err
	}

	return doc, nil
}

// ListByChat returns the chat's members.
func (s *Memberships) ListByChat(ctx context.Context, chatID domain.ChatID) ([]MembershipDoc, error) {
	return s.list(ctx, "chat_id", chatID.String())
}

// ListByUser returns the chats a user belongs to — the direction DynamoDB
// needed the user_chats-index GSI for.
func (s *Memberships) ListByUser(ctx context.Context, userID domain.UserID) ([]MembershipDoc, error) {
	return s.list(ctx, "user_id", userID.String())
}

func (s *Memberships) list(ctx context.Context, field, value string) ([]MembershipDoc, error) {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	iter := s.client.FS.Collection(CollectionMemberships).Where(field, "==", value).Documents(ctx)
	defer iter.Stop()

	var docs []MembershipDoc
	for {
		snapshot, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("firestore: list memberships by %s: %w", field, err)
		}

		doc, err := membershipFrom(snapshot)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
}

// Set writes the membership at its deterministic ID. Because the ID is derived
// rather than generated, this overwrites joined_at on a re-add: the collection
// holds current membership, and its history lives in the lake (ADR-023).
func (s *Memberships) Set(ctx context.Context, doc MembershipDoc) error {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	docID, err := doc.DocID()
	if err != nil {
		return err
	}

	if _, err := s.client.FS.Collection(CollectionMemberships).Doc(docID).Set(ctx, doc); err != nil {
		return fmt.Errorf("firestore: set membership %s: %w", docID, err)
	}
	return nil
}

// Delete removes the membership. Deleting a document that does not exist is not
// an error in Firestore, which suits "leave a chat you are not in".
func (s *Memberships) Delete(ctx context.Context, chatID domain.ChatID, userID domain.UserID) error {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	docID := MembershipDocID(chatID, userID)

	if _, err := s.client.FS.Collection(CollectionMemberships).Doc(docID).Delete(ctx); err != nil {
		return fmt.Errorf("firestore: delete membership %s: %w", docID, err)
	}
	return nil
}

// Sessions reads and writes the `sessions` collection. Callers must treat
// SessionDoc.IsExpired as the expiry gate; the TTL policy only collects
// garbage (ADR-023).
type Sessions struct{ client *Client }

// NewSessions returns the sessions store.
func NewSessions(client *Client) *Sessions { return &Sessions{client: client} }

// Get returns the session, or domain.ErrNotFound. An expired session is
// returned normally — expiry is the caller's check, not a read failure.
func (s *Sessions) Get(ctx context.Context, sessionID domain.SessionID) (SessionDoc, error) {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	snapshot, err := s.client.FS.Collection(CollectionSessions).Doc(sessionID.String()).Get(ctx)
	if err != nil {
		return SessionDoc{}, wrapErr("get session", sessionID.String(), err)
	}

	var doc SessionDoc
	if err := snapshot.DataTo(&doc); err != nil {
		return SessionDoc{}, fmt.Errorf("firestore: decode session %s: %w", sessionID.String(), err)
	}
	doc.ID = snapshot.Ref.ID

	return doc, nil
}

// ListByUser returns every session document for a user, expired ones included.
func (s *Sessions) ListByUser(ctx context.Context, userID domain.UserID) ([]SessionDoc, error) {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	iter := s.client.FS.Collection(CollectionSessions).Where("user_id", "==", userID.String()).Documents(ctx)
	defer iter.Stop()

	var docs []SessionDoc
	for {
		snapshot, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("firestore: list sessions by user: %w", err)
		}

		var doc SessionDoc
		if err := snapshot.DataTo(&doc); err != nil {
			return nil, fmt.Errorf("firestore: decode session %s: %w", snapshot.Ref.ID, err)
		}
		doc.ID = snapshot.Ref.ID
		docs = append(docs, doc)
	}
}

// Set writes the session document at doc.ID, replacing any existing one.
func (s *Sessions) Set(ctx context.Context, doc SessionDoc) error {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	if err := doc.Validate(); err != nil {
		return err
	}

	if _, err := s.client.FS.Collection(CollectionSessions).Doc(doc.ID).Set(ctx, doc); err != nil {
		return fmt.Errorf("firestore: set session %s: %w", doc.ID, err)
	}
	return nil
}

// Delete revokes a session immediately, rather than waiting for TTL.
func (s *Sessions) Delete(ctx context.Context, sessionID domain.SessionID) error {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	if _, err := s.client.FS.Collection(CollectionSessions).Doc(sessionID.String()).Delete(ctx); err != nil {
		return fmt.Errorf("firestore: delete session %s: %w", sessionID.String(), err)
	}
	return nil
}

// OTPRequests reads and writes the `otp_requests` collection. Callers must
// treat OTPRequestDoc.IsExpired as the expiry gate; the TTL policy only
// collects garbage, and for a five-minute credential it lags by up to a day
// (ADR-023 v1.2).
type OTPRequests struct{ client *Client }

// NewOTPRequests returns the OTP request store.
func NewOTPRequests(client *Client) *OTPRequests { return &OTPRequests{client: client} }

// Get returns the OTP request for a phone hash, or domain.ErrNotFound. An
// expired record is returned normally — expiry is the caller's check.
func (s *OTPRequests) Get(ctx context.Context, phoneHash string) (OTPRequestDoc, error) {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	snapshot, err := s.client.FS.Collection(CollectionOTPRequests).Doc(phoneHash).Get(ctx)
	if err != nil {
		return OTPRequestDoc{}, wrapErr("get OTP request", phoneHash, err)
	}

	return otpRequestFrom(snapshot)
}

// Create issues an OTP, refusing to overwrite one that is still live: it
// returns domain.ErrAlreadyExists when a record exists and has not yet passed
// expires_at, which is ADR-015 §1.2's ConditionalCheckFailed branch.
//
// This is a transaction rather than a plain Create() for one reason, and it is
// not a stylistic one. Firestore's Create() fails with AlreadyExists whenever
// the document is present, and TTL removes an expired OTP only within ~24
// hours of its expiry — so a bare Create() would refuse every new OTP for
// almost a full day after one five-minute code lapsed, locking the phone
// number out. The transaction asks the question that actually matters, "is
// there a *live* OTP?", and Firestore's optimistic-concurrency retry makes the
// read-then-write atomic against a second request for the same number.
func (s *OTPRequests) Create(ctx context.Context, doc OTPRequestDoc, now time.Time) error {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	if err := doc.Validate(); err != nil {
		return err
	}

	ref := s.client.FS.Collection(CollectionOTPRequests).Doc(doc.ID)

	return s.client.FS.RunTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		snapshot, getErr := tx.Get(ref)
		switch {
		case status.Code(getErr) == codes.NotFound:
			// No record at all — the common first-request path.
		case getErr != nil:
			return fmt.Errorf("firestore: read OTP request %s: %w", doc.ID, getErr)
		default:
			existing, decodeErr := otpRequestFrom(snapshot)
			if decodeErr != nil {
				return decodeErr
			}
			if !existing.IsExpired(now) {
				return fmt.Errorf("firestore: OTP request %s is still live: %w", doc.ID, domain.ErrAlreadyExists)
			}
			// Expired but not yet collected: overwriting is correct, and
			// resets attempt_count along with the code.
		}

		if setErr := tx.Set(ref, doc); setErr != nil {
			return fmt.Errorf("firestore: set OTP request %s: %w", doc.ID, setErr)
		}
		return nil
	})
}

// IncrementAttempts records a failed verification (ADR-015 §1.4).
//
// firestore.Increment is a server-side atomic operation, not a read-modify-
// write: two wrong guesses arriving together both count, where a
// read-then-write would let one overwrite the other and silently extend the
// attacker's budget past MaxOTPVerifyAttempts.
func (s *OTPRequests) IncrementAttempts(ctx context.Context, phoneHash string) error {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	_, err := s.client.FS.Collection(CollectionOTPRequests).Doc(phoneHash).Update(ctx, []firestore.Update{
		{Path: "attempt_count", Value: firestore.Increment(1)},
	})
	if err != nil {
		return wrapErr("increment OTP attempts", phoneHash, err)
	}
	return nil
}

func otpRequestFrom(snapshot *firestore.DocumentSnapshot) (OTPRequestDoc, error) {
	var doc OTPRequestDoc
	if err := snapshot.DataTo(&doc); err != nil {
		return OTPRequestDoc{}, fmt.Errorf("firestore: decode OTP request %s: %w", snapshot.Ref.ID, err)
	}
	doc.ID = snapshot.Ref.ID
	return doc, nil
}

func membershipFrom(snapshot *firestore.DocumentSnapshot) (MembershipDoc, error) {
	var doc MembershipDoc
	if err := snapshot.DataTo(&doc); err != nil {
		return MembershipDoc{}, fmt.Errorf("firestore: decode membership %s: %w", snapshot.Ref.ID, err)
	}
	doc.ID = snapshot.Ref.ID
	return doc, nil
}

// wrapErr maps Firestore's NotFound status to domain.ErrNotFound so callers
// match on the domain sentinel instead of importing gRPC status codes.
func wrapErr(op, id string, err error) error {
	if status.Code(err) == codes.NotFound {
		return fmt.Errorf("firestore: %s %s: %w", op, id, domain.ErrNotFound)
	}
	return fmt.Errorf("firestore: %s %s: %w", op, id, err)
}
