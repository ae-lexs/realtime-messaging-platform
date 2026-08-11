package firestore_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// TestDocumentFieldNamesMatchTheSchema pins every persisted field name against
// ADR-023's Firestore model. Renaming a Go field is invisible on the wire until
// something queries the old name — `where('phone_number','==',x)` against a
// document that now stores `phoneNumber` returns nothing, with no error. The
// schema is the authority; this test is what stops the code drifting from it.
func TestDocumentFieldNamesMatchTheSchema(t *testing.T) {
	tests := []struct {
		name     string
		doc      any
		wantTags []string
	}{
		{
			name:     "users",
			doc:      firestore.UserDoc{},
			wantTags: []string{"phone_number", "phone_verified", "display_name", "created_at", "updated_at"},
		},
		{
			// member_count joined the collection in ADR-023 v1.4: it is the
			// group-size cap's serialization point, read inside the same
			// transaction that writes a membership.
			name:     "chats",
			doc:      firestore.ChatDoc{},
			wantTags: []string{"chat_type", "name", "created_by", "member_count", "created_at", "updated_at"},
		},
		{
			name:     "memberships",
			doc:      firestore.MembershipDoc{},
			wantTags: []string{"chat_id", "user_id", "role", "joined_at", "muted_until"},
		},
		{
			// prev_token_hash and token_generation are additions to ADR-023's
			// list, required by ADR-015 refresh rotation and reuse detection.
			name: "sessions",
			doc:  firestore.SessionDoc{},
			wantTags: []string{
				"user_id", "device_id", "refresh_token_hash", "prev_token_hash",
				"token_generation", "created_at", "expires_at",
			},
		},
		{
			name:     "otp_requests",
			doc:      firestore.OTPRequestDoc{},
			wantTags: []string{"otp_mac", "status", "attempt_count", "created_at", "expires_at"},
		},
		{
			name:     "phone_index",
			doc:      firestore.PhoneIndexDoc{},
			wantTags: []string{"user_id", "created_at"},
		},
		{
			// chat_id is the field that makes this a lookup and not only a
			// sentinel — the race loser reads it to learn which chat won
			// (ADR-023 v1.3, v1.4).
			name:     "direct_chats",
			doc:      firestore.DirectChatDoc{},
			wantTags: []string{"chat_id", "created_at"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			var got []string
			typ := reflect.TypeOf(tt.doc)
			for i := range typ.NumField() {
				tag := typ.Field(i).Tag.Get("firestore")
				if tag == "-" {
					continue // the document ID, carried on the reference
				}
				got = append(got, tag)
			}

			// Assert
			assert.Equal(t, tt.wantTags, got)
		})
	}
}

// TestSessionExpiresAtIsATimestamp guards the reason expires_at may never
// become a formatted string: a Firestore TTL policy only acts on
// timestamp-typed fields, so a string here would disable session garbage
// collection silently.
func TestSessionExpiresAtIsATimestamp(t *testing.T) {
	// Act
	field, ok := reflect.TypeOf(firestore.SessionDoc{}).FieldByName("ExpiresAt")

	// Assert
	require.True(t, ok)
	assert.Equal(t, reflect.TypeOf(time.Time{}), field.Type)
}

// TestOTPRequestExpiresAtIsATimestamp guards the same TTL requirement as the
// session field: a policy only acts on timestamp-typed fields.
func TestOTPRequestExpiresAtIsATimestamp(t *testing.T) {
	// Act
	field, ok := reflect.TypeOf(firestore.OTPRequestDoc{}).FieldByName("ExpiresAt")

	// Assert
	require.True(t, ok)
	assert.Equal(t, reflect.TypeOf(time.Time{}), field.Type)
}

// TestNewOTPRequestDocTruncatesToTheSecond pins the invariant that makes OTP
// verification work at all.
//
// auth.ComputeOTPMAC binds the MAC to the RFC3339 rendering of expires_at, and
// Firestore stores timestamps at microsecond precision. A nanosecond component
// therefore survives the write, is silently dropped somewhere in the
// round-trip, and the MAC recomputed on verification no longer matches the one
// stored — every OTP fails, for a reason nothing in the auth code would
// explain. Truncating at construction makes the stored value already exact.
func TestNewOTPRequestDocTruncatesToTheSecond(t *testing.T) {
	// Arrange — a timestamp with sub-second precision Firestore cannot hold.
	created := time.Date(2026, 7, 26, 12, 0, 0, 123456789, time.UTC)
	expires := created.Add(5 * time.Minute)

	// Act
	doc := firestore.NewOTPRequestDoc("phone-hash", "mac", created, expires)

	// Assert
	assert.Equal(t, doc.CreatedAt, doc.CreatedAt.Truncate(time.Second))
	assert.Equal(t, doc.ExpiresAt, doc.ExpiresAt.Truncate(time.Second))
	assert.Zero(t, doc.ExpiresAt.Nanosecond())

	// And the MAC input is therefore stable across a round-trip that keeps
	// only microseconds.
	roundTripped := doc.ExpiresAt.Truncate(time.Microsecond)
	assert.Equal(t,
		doc.ExpiresAt.UTC().Format(time.RFC3339),
		roundTripped.UTC().Format(time.RFC3339),
	)
}

// TestNewOTPRequestDocNormalizesToUTC guards the other half of the same
// invariant: RFC3339 renders the offset, so a record built from a non-UTC
// clock would produce a different MAC input for the same instant.
func TestNewOTPRequestDocNormalizesToUTC(t *testing.T) {
	// Arrange
	zone := time.FixedZone("UTC-7", -7*60*60)
	created := time.Date(2026, 7, 26, 5, 0, 0, 0, zone)

	// Act
	doc := firestore.NewOTPRequestDoc("phone-hash", "mac", created, created.Add(5*time.Minute))

	// Assert
	assert.Equal(t, time.UTC, doc.ExpiresAt.Location())
	assert.Equal(t, "2026-07-26T12:05:00Z", doc.ExpiresAt.Format(time.RFC3339))
}

func TestNewOTPRequestDocStartsPending(t *testing.T) {
	// Act
	doc := firestore.NewOTPRequestDoc("phone-hash", "mac", time.Now(), time.Now().Add(time.Minute))

	// Assert
	assert.Equal(t, "phone-hash", doc.ID)
	assert.Equal(t, domain.OTPStatusPending, doc.Status)
	assert.Zero(t, doc.AttemptCount)
}

func TestOTPRequestIsExpired(t *testing.T) {
	expiresAt := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	otp := firestore.OTPRequestDoc{ExpiresAt: expiresAt}

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{name: "within the five-minute window", now: expiresAt.Add(-time.Minute), want: false},
		{name: "exactly at expiry", now: expiresAt, want: true},
		{
			// The gap this gate exists for, and it is much wider here than for
			// sessions: the credential is valid for five minutes but TTL
			// collects the document within ~24 hours, so for nearly a day the
			// record is expired and still readable.
			name: "hours after expiry, still readable",
			now:  expiresAt.Add(20 * time.Hour),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, otp.IsExpired(tt.now))
		})
	}
}

func TestSessionIsExpired(t *testing.T) {
	expiresAt := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	session := firestore.SessionDoc{ExpiresAt: expiresAt}

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{name: "well before expiry", now: expiresAt.Add(-time.Hour), want: false},
		{name: "one nanosecond before", now: expiresAt.Add(-time.Nanosecond), want: false},
		{name: "exactly at expiry", now: expiresAt, want: true},
		{name: "after expiry", now: expiresAt.Add(time.Hour), want: true},
		{
			// The window TTL leaves open: Firestore deletes within ~24h, so a
			// long-expired session is still readable and must still be refused.
			name: "a day after expiry, still readable",
			now:  expiresAt.Add(23 * time.Hour),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, session.IsExpired(tt.now))
		})
	}
}
