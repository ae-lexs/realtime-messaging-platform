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

// SessionRotation is the mutable field set of a refresh-token rotation
// (ADR-015 §4.2).
type SessionRotation struct {
	// RefreshTokenHash is the new token's hash.
	RefreshTokenHash string

	// PrevTokenHash is the hash being replaced. It is both stored — reuse
	// detection compares against it — and used as the write's precondition.
	PrevTokenHash string

	TokenGeneration int64
	ExpiresAt       time.Time
}

// Rotate advances a session's refresh token, refusing the write unless the
// stored hash is still the one being replaced. That precondition is ADR-015
// §4.2's `ConditionExpression: refresh_token_hash = :old_hash`, and it is what
// makes rotation single-use: if two requests arrive holding the same refresh
// token, the first commits and the second finds a hash it does not recognise
// and gets domain.ErrInvalidRefreshToken. Without it both would succeed, the
// second overwriting prev_token_hash with a value that erases the evidence
// reuse detection depends on.
func (s *Sessions) Rotate(ctx context.Context, sessionID domain.SessionID, rotation SessionRotation) error {
	ctx, cancel := s.client.withTimeout(ctx)
	defer cancel()

	ref := s.client.FS.Collection(CollectionSessions).Doc(sessionID.String())

	return s.client.FS.RunTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		snapshot, getErr := tx.Get(ref)
		if status.Code(getErr) == codes.NotFound {
			return fmt.Errorf("firestore: rotate session %s: %w", sessionID.String(), domain.ErrNotFound)
		}
		if getErr != nil {
			return fmt.Errorf("firestore: read session %s: %w", sessionID.String(), getErr)
		}

		var current SessionDoc
		if decodeErr := snapshot.DataTo(&current); decodeErr != nil {
			return fmt.Errorf("firestore: decode session %s: %w", sessionID.String(), decodeErr)
		}

		if current.RefreshTokenHash != rotation.PrevTokenHash {
			return fmt.Errorf("firestore: session %s already rotated: %w",
				sessionID.String(), domain.ErrInvalidRefreshToken)
		}

		if updateErr := tx.Update(ref, []firestore.Update{
			{Path: "refresh_token_hash", Value: rotation.RefreshTokenHash},
			{Path: "prev_token_hash", Value: rotation.PrevTokenHash},
			{Path: "token_generation", Value: rotation.TokenGeneration},
			{Path: "expires_at", Value: rotation.ExpiresAt},
		}); updateErr != nil {
			return fmt.Errorf("firestore: rotate session %s: %w", sessionID.String(), updateErr)
		}

		return nil
	})
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

// Create issues an OTP, refusing only to overwrite one that is still usable —
// present, unconsumed, and unexpired. That is ADR-015 §1.2's condition, whose
// three disjuncts (absent, verified, or past expires_at) all mean "no active,
// unverified OTP is in flight"; anything else returns domain.ErrAlreadyExists.
//
// Both of the permissive cases matter, and each is a bug if dropped:
//
//   - Expired. TTL removes a lapsed OTP only within ~24 hours of its expiry,
//     so the document outlives its five-minute validity by nearly a day. Were
//     expiry not checked, the phone number would be locked out for that whole
//     window with nothing to explain it.
//   - Verified. Once consumed, the code is spent and cannot be replayed, so
//     nothing is protected by keeping it. Were consumption not checked, a user
//     who just logged in could not request another OTP until their previous one
//     timed out — which the live gate caught. Re-request abuse is the rate
//     limiter's job (ADR-013 §4.1), not this write's.
//
// It is a transaction rather than a plain Create() because Create() can only
// express "the document is absent", which is the strictest of the three
// disjuncts and not the one the ADR asks for. Firestore's optimistic-
// concurrency retry is what makes the read-then-write atomic against a second
// request for the same number.
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
			if existing.Status == domain.OTPStatusPending && !existing.IsExpired(now) {
				return fmt.Errorf("firestore: OTP request %s is still live: %w", doc.ID, domain.ErrAlreadyExists)
			}
			// Consumed, or expired and not yet collected. Either way no
			// unverified code is in flight, so overwriting is correct — and it
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
