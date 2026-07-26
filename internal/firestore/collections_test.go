package firestore_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

func TestMembershipDocIDIsDeterministic(t *testing.T) {
	// Arrange
	chatID := domain.GenerateChatID()
	userID := domain.GenerateUserID()

	// Act
	first := firestore.MembershipDocID(chatID, userID)
	second := firestore.MembershipDocID(chatID, userID)

	// Assert — the same pair must always address the same document, or
	// "check membership" stops being a point lookup (ADR-023).
	assert.Equal(t, first, second)
	assert.Equal(t, chatID.String()+"__"+userID.String(), first)
}

func TestMembershipDocIDIsOrderSensitive(t *testing.T) {
	// Arrange — a chat and a user whose raw IDs are swapped into the other slot.
	chatID := domain.GenerateChatID()
	userID := domain.GenerateUserID()
	swappedChat := domain.MustChatID(userID.String())
	swappedUser := domain.MustUserID(chatID.String())

	// Act + Assert — {chat}__{user} is a coordinate, not a set.
	assert.NotEqual(t,
		firestore.MembershipDocID(chatID, userID),
		firestore.MembershipDocID(swappedChat, swappedUser),
	)
}

// TestMembershipDocIDFitsBound pins the ADR-023 document-ID invariant. The ADR
// states 54 bytes on the assumption of ULIDs; the implemented identifiers
// (internal/domain/ids.go) are UUIDs, so the real composite is 74 bytes. Either
// way the margin against Firestore's limit is enormous — but the number is
// asserted here so a change to the ID format has to come past this test.
func TestMembershipDocIDFitsBound(t *testing.T) {
	// Arrange
	chatID := domain.GenerateChatID()
	userID := domain.GenerateUserID()

	// Act
	id := firestore.MembershipDocID(chatID, userID)

	// Assert
	assert.Len(t, id, 74, "36-byte UUID + 2-byte separator + 36-byte UUID")
	assert.Less(t, len(id), firestore.MaxDocIDBytes)
}
