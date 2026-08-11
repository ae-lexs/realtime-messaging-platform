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
	// index, so the lookup needs no index configuration (ADR-023).
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
	ChatType  string `firestore:"chat_type"`
	Name      string `firestore:"name"`
	CreatedBy string `firestore:"created_by"`

	// MemberCount is the number of memberships, maintained in the same
	// transaction as the membership writes (ADR-023 v1.4).
	//
	// Two things it is not. It is **never an authorization input** — the
	// membership check reads `memberships` directly (ADR-016 §3.2), and a
	// future reader must not repurpose this as a truth source. And it is not
	// the "intended count" ADR-016 §3.2 hedged it as: that hedge answered a
	// prior substrate's transaction limit (ADR-023 v1.4 retires it), while a
	// Firestore transaction creates a whole group at once, so this value is
	// exact from the moment the chat exists.
	//
	// What it is for: display (ADR-006 §4.2, §4.3) and the group-size cap,
	// where reading this document inside the transaction is what serializes
	// concurrent adds. That lock is the one Firestore actually documents — on
	// a document the transaction read — which is why the cap is enforced this
	// way rather than by counting memberships with a query.
	MemberCount int `firestore:"member_count"`

	CreatedAt time.Time `firestore:"created_at"`
	UpdatedAt time.Time `firestore:"updated_at"`
}

// IsDirect reports whether the chat is a direct chat, whose membership is
// fixed at creation (ADR-016 §5).
func (d ChatDoc) IsDirect() bool {
	return d.ChatType == string(domain.ChatTypeDirect)
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
// ExpiresAt must be a Firestore timestamp, not a string: a TTL policy only
// acts on timestamp-typed fields, so a string here would disable session
// garbage collection silently. The policy itself is declared in Terraform
// against this field; there is no companion attribute to keep in step with it.
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

// OTPRequestDoc is a document in `otp_requests` (ADR-023 v1.2, ADR-015 §1.1).
// Doc ID is the SHA-256 hash of the E.164 number: this collection is
// high-churn and short-lived, and holding raw phone numbers here would widen
// the blast radius of a leak for no access-pattern gain — the hash is an exact
// match key, which is the only pattern the collection serves.
//
// The OTP itself is stored in no form at all. OTPMAC verifies a candidate;
// ADR-015 v1.1 dropped the KMS-encrypted copy that existed only so a repeat
// request could re-send the same code.
type OTPRequestDoc struct {
	ID string `firestore:"-"`

	// OTPMAC is HMAC-SHA256(pepper, otp || phone_hash || expires_at).
	OTPMAC string `firestore:"otp_mac"`

	// Status is domain.OTPStatusPending or domain.OTPStatusVerified.
	Status string `firestore:"status"`

	// AttemptCount is verification attempts against this OTP. Incremented
	// server-side so concurrent wrong guesses cannot both read the same value
	// and lose an increment between them.
	AttemptCount int `firestore:"attempt_count"`

	CreatedAt time.Time `firestore:"created_at"`

	// ExpiresAt carries the collection's TTL policy and MUST be stored
	// truncated to the second. auth.ComputeOTPMAC binds the MAC to this
	// value's RFC3339 rendering, and Firestore stores timestamps at
	// microsecond precision: a sub-second component would survive the write
	// and not the read, changing the MAC input and failing every subsequent
	// verification. Truncating makes the round-trip exact. NewOTPRequestDoc
	// applies it; do not construct this struct by hand.
	ExpiresAt time.Time `firestore:"expires_at"`
}

// NewOTPRequestDoc builds a pending OTP record, applying the second-truncation
// the MAC depends on (see OTPRequestDoc.ExpiresAt).
func NewOTPRequestDoc(phoneHash, otpMAC string, createdAt, expiresAt time.Time) OTPRequestDoc {
	return OTPRequestDoc{
		ID:           phoneHash,
		OTPMAC:       otpMAC,
		Status:       domain.OTPStatusPending,
		AttemptCount: 0,
		CreatedAt:    createdAt.UTC().Truncate(time.Second),
		ExpiresAt:    expiresAt.UTC().Truncate(time.Second),
	}
}

// PhoneIndexDoc is a document in `phone_index` (ADR-023 v1.2) — the sentinel
// that makes "one user per phone number" true under concurrency. Doc ID is the
// SHA-256 phone hash, and `tx.Create` on that deterministic path is what
// enforces uniqueness: it fails atomically if the document already exists
// (ADR-015 §5.1).
//
// It is deliberately not a read path: FindByPhone queries users.phone_number,
// exactly as ADR-023's access-pattern table specifies. This document exists to
// be contended for, not to be looked up.
type PhoneIndexDoc struct {
	ID string `firestore:"-"`

	UserID    string    `firestore:"user_id"`
	CreatedAt time.Time `firestore:"created_at"`
}

// DirectChatDoc is a document in `direct_chats` (ADR-023 v1.3). Doc ID is the
// canonical pair from DirectChatDocID.
//
// It is both the uniqueness sentinel for a user pair and the lookup that
// answers which chat that pair has — the second is what justifies it, since
// the concurrency argument v1.3 gave for the first did not survive measurement
// (ADR-023 v1.4). Permanent, no TTL: if direct-chat deletion is ever built,
// this document must be deleted in the same transaction as the chat, or the
// pair can never open a second conversation.
type DirectChatDoc struct {
	ID string `firestore:"-"`

	ChatID    string    `firestore:"chat_id"`
	CreatedAt time.Time `firestore:"created_at"`
}

// Validate reports whether the direct-pair sentinel can be written.
func (d DirectChatDoc) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("firestore: direct chat document ID is required")
	}
	if d.ChatID == "" {
		// A sentinel with no chat_id claims the pair for nothing: the pair
		// could never open a chat, and the loser of a race would read a
		// document that names no winner.
		return fmt.Errorf("firestore: direct chat %s has no chat ID", d.ID)
	}
	return nil
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

// Validate reports whether the OTP request can be written.
func (d OTPRequestDoc) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("firestore: OTP request document ID is required")
	}
	if d.OTPMAC == "" {
		// Without a MAC nothing can ever verify against this record, but the
		// conditional write would still refuse a fresh OTP for the phone
		// number until it expired.
		return fmt.Errorf("firestore: OTP request %s has no MAC", d.ID)
	}
	if d.Status != domain.OTPStatusPending && d.Status != domain.OTPStatusVerified {
		return fmt.Errorf("firestore: OTP request %s has invalid status %q", d.ID, d.Status)
	}
	if d.ExpiresAt.IsZero() {
		return fmt.Errorf("firestore: OTP request %s has no expires_at", d.ID)
	}
	return nil
}

// IsExpired reports whether the OTP is past its expiry at now.
//
// This is the correctness gate, and it matters more here than it does for
// sessions: an OTP is valid for five minutes, while Firestore's TTL collects
// the document within ~24 hours of expires_at. For nearly a full day the
// record is expired but still readable, so nothing may infer validity from the
// document's existence — neither verification nor the conditional write in
// OTPRequests.Create.
func (d OTPRequestDoc) IsExpired(now time.Time) bool {
	return !now.Before(d.ExpiresAt)
}

// Validate reports whether the phone-index sentinel can be written.
func (d PhoneIndexDoc) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("firestore: phone index document ID is required")
	}
	if d.UserID == "" {
		// A sentinel with no user_id would claim a phone number for nobody and
		// permanently block its registration.
		return fmt.Errorf("firestore: phone index %s has no user ID", d.ID)
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
