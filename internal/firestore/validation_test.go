package firestore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// The write guards live on the documents rather than inside the stores, so they
// are testable without a Firestore connection — no credentials, no emulator.

func TestUserDocValidate(t *testing.T) {
	assert.Error(t, firestore.UserDoc{}.Validate(), "a document with no ID has nowhere to be written")
	assert.NoError(t, firestore.UserDoc{ID: domain.GenerateUserID().String()}.Validate())
}

func TestChatDocValidate(t *testing.T) {
	assert.Error(t, firestore.ChatDoc{}.Validate())
	assert.NoError(t, firestore.ChatDoc{ID: domain.GenerateChatID().String()}.Validate())
}

func TestSessionDocValidate(t *testing.T) {
	sessionID := domain.GenerateSessionID().String()

	tests := []struct {
		name    string
		doc     firestore.SessionDoc
		wantErr string
	}{
		{
			name:    "no ID",
			doc:     firestore.SessionDoc{ExpiresAt: time.Now()},
			wantErr: "document ID is required",
		},
		{
			// The failure this guard exists for: a zero expires_at is never
			// collected by the TTL policy and never refused by IsExpired, so the
			// session would live forever without anything reporting a problem.
			name:    "no expiry",
			doc:     firestore.SessionDoc{ID: sessionID},
			wantErr: "no expires_at",
		},
		{
			name: "valid",
			doc:  firestore.SessionDoc{ID: sessionID, ExpiresAt: time.Now().Add(time.Hour)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.doc.Validate()

			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestMembershipDocID(t *testing.T) {
	chatID := domain.GenerateChatID()
	userID := domain.GenerateUserID()

	t.Run("derives the ID from the fields", func(t *testing.T) {
		// Act
		docID, err := firestore.MembershipDoc{
			ChatID: chatID.String(),
			UserID: userID.String(),
		}.DocID()

		// Assert
		require.NoError(t, err)
		assert.Equal(t, firestore.MembershipDocID(chatID, userID), docID)
	})

	t.Run("rejects identifiers that do not parse", func(t *testing.T) {
		tests := []struct {
			name       string
			doc        firestore.MembershipDoc
			wantErr    string
			wantTarget error
		}{
			{
				name:       "chat ID not a UUID",
				doc:        firestore.MembershipDoc{ChatID: "chat-1", UserID: userID.String()},
				wantErr:    "membership chat ID",
				wantTarget: domain.ErrInvalidID,
			},
			{
				name:       "user ID empty",
				doc:        firestore.MembershipDoc{ChatID: chatID.String()},
				wantErr:    "membership user ID",
				wantTarget: domain.ErrEmptyID,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := tt.doc.DocID()

				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.ErrorIs(t, err, tt.wantTarget)
			})
		}
	})
}
