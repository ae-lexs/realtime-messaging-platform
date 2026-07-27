package firestore_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			name:     "chats",
			doc:      firestore.ChatDoc{},
			wantTags: []string{"chat_type", "name", "created_by", "created_at", "updated_at"},
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

// TestSessionExpiresAtIsATimestamp guards the reason expires_at cannot go back
// to the RFC3339 strings the DynamoDB-era records used: a Firestore TTL policy
// only acts on timestamp-typed fields, so a string here would disable session
// garbage collection silently.
func TestSessionExpiresAtIsATimestamp(t *testing.T) {
	// Act
	field, ok := reflect.TypeOf(firestore.SessionDoc{}).FieldByName("ExpiresAt")

	// Assert
	require.True(t, ok)
	assert.Equal(t, reflect.TypeOf(time.Time{}), field.Type)
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
