package firestore

import (
	"fmt"
	"time"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// UserDoc is a document in `users` (ADR-023). Doc ID is the user ID.
type UserDoc struct {
	ID string `firestore:"-"`

	// PhoneNumber is E.164. Queried by equality on the automatic single-field
	// index — this is what replaced DynamoDB's phone_number-index GSI.
	PhoneNumber   string    `firestore:"phone_number"`
	PhoneVerified bool      `firestore:"phone_verified"`
	DisplayName   string    `firestore:"display_name"`
	CreatedAt     time.Time `firestore:"created_at"`
	UpdatedAt     time.Time `firestore:"updated_at"`
}

// ChatDoc is a document in `chats` (ADR-023). Doc ID is the chat ID.
type ChatDoc struct {
	ID string `firestore:"-"`

	// ChatType is "direct" or "group" (ADR-016).
	ChatType  string    `firestore:"chat_type"`
	Name      string    `firestore:"name"`
	CreatedBy string    `firestore:"created_by"`
	CreatedAt time.Time `firestore:"created_at"`
	UpdatedAt time.Time `firestore:"updated_at"`
}

// MembershipDoc is a document in `memberships` (ADR-023). Doc ID is the
// deterministic composite from MembershipDocID.
//
// ChatID and UserID are stored as fields as well as encoded in the ID: the ID
// serves the point lookup, the fields serve the two list queries.
type MembershipDoc struct {
	ID string `firestore:"-"`

	ChatID string `firestore:"chat_id"`
	UserID string `firestore:"user_id"`

	// Role is "owner", "admin" or "member" (ADR-016).
	Role     string    `firestore:"role"`
	JoinedAt time.Time `firestore:"joined_at"`

	// MutedUntil is nil when the member is not muted.
	MutedUntil *time.Time `firestore:"muted_until"`
}

// SessionDoc is a document in `sessions` (ADR-023, ADR-015). Doc ID is the
// session ID.
//
// ExpiresAt must be a Firestore timestamp, not a string: the TTL policy only
// acts on timestamp-typed fields. The DynamoDB-era `ttl` attribute has no
// counterpart here — the Terraform TTL policy on expires_at replaces it.
//
// PrevTokenHash and TokenGeneration are absent from ADR-023's field list but
// required by ADR-015's refresh rotation and reuse detection, which ADR-021
// carries over unchanged.
type SessionDoc struct {
	ID string `firestore:"-"`

	UserID   string `firestore:"user_id"`
	DeviceID string `firestore:"device_id"`

	// RefreshTokenHash is SHA-256 of the current refresh token; PrevTokenHash
	// is the previous one, kept so a replayed token is detected rather than
	// merely rejected (ADR-015).
	RefreshTokenHash string `firestore:"refresh_token_hash"`
	PrevTokenHash    string `firestore:"prev_token_hash"`
	TokenGeneration  int64  `firestore:"token_generation"`

	CreatedAt time.Time `firestore:"created_at"`
	ExpiresAt time.Time `firestore:"expires_at"`
}

// Validate reports whether the document can be written. Each Validate covers
// what its collection's write actually requires, so the checks are testable
// without a Firestore connection and are enforced in one place.
func (d UserDoc) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("firestore: user document ID is required")
	}
	return nil
}

// Validate reports whether the chat document can be written.
func (d ChatDoc) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("firestore: chat document ID is required")
	}
	return nil
}

// DocID derives the document ID a membership is written at, validating both
// identifiers on the way.
//
// The ID is derived from the fields rather than supplied, so the two can never
// disagree — a membership addressed by one pair while carrying another would
// break the point lookup and both list queries at once.
func (d MembershipDoc) DocID() (string, error) {
	chatID, err := domain.NewChatID(d.ChatID)
	if err != nil {
		return "", fmt.Errorf("firestore: membership chat ID: %w", err)
	}
	userID, err := domain.NewUserID(d.UserID)
	if err != nil {
		return "", fmt.Errorf("firestore: membership user ID: %w", err)
	}
	return MembershipDocID(chatID, userID), nil
}

// Validate reports whether the session can be written.
func (d SessionDoc) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("firestore: session document ID is required")
	}
	if d.ExpiresAt.IsZero() {
		// A zero expires_at is never collected by the TTL policy and never
		// refused by IsExpired — a session that would live forever, silently.
		return fmt.Errorf("firestore: session %s has no expires_at", d.ID)
	}
	return nil
}

// IsExpired reports whether the session is past its expiry at now.
//
// This is the ADR-023 application-enforced invariant, and it is not redundant
// with the TTL policy: Firestore deletes expired documents within ~24 hours of
// expires_at, so an expired session stays readable for up to a day. TTL is
// garbage collection; this function is the correctness gate. Auth must call it
// on every session read.
func (d SessionDoc) IsExpired(now time.Time) bool {
	return !now.Before(d.ExpiresAt)
}
