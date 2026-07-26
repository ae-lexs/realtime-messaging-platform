//go:build integration

// The M1.1 gate: a CRUD round-trip against the dev Firestore, covering every
// Firestore row of ADR-023's access-pattern table. There is no emulator — dev
// targets a real GCP project (ADR-021 Axis F) — so this runs only when pointed
// at a live database:
//
//	PROJECT_ID=... make firestore-test
//
// Every document is written under a freshly generated ID, so runs never collide.
// Memberships and sessions are deleted at the end because deletion is part of
// their contract; users and chats are not, since ADR-023's access-pattern table
// gives them no delete — those documents are cleaned up when the database is
// destroyed with the environment.

package firestore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

func liveClient(t *testing.T) *firestore.Client {
	t.Helper()

	project, database := os.Getenv("FIRESTORE_PROJECT"), os.Getenv("FIRESTORE_DATABASE")
	if project == "" || database == "" {
		t.Skip("set FIRESTORE_PROJECT and FIRESTORE_DATABASE to run against a live database")
	}

	client, err := firestore.NewClient(context.Background(), firestore.Config{
		ProjectID:  project,
		DatabaseID: database,
		Timeout:    domain.FirestoreTimeout,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })

	return client
}

func TestUsersRoundTrip(t *testing.T) {
	// Arrange
	ctx := context.Background()
	users := firestore.NewUsers(liveClient(t))

	userID := domain.GenerateUserID()
	phone, err := domain.NewPhoneNumber("+525512345678")
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Millisecond)
	doc := firestore.UserDoc{
		ID:            userID.String(),
		PhoneNumber:   phone.String(),
		PhoneVerified: true,
		DisplayName:   "Integration Test",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Act
	require.NoError(t, users.Set(ctx, doc))

	// Assert — the point read...
	got, err := users.Get(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, doc.PhoneNumber, got.PhoneNumber)
	assert.True(t, got.PhoneVerified)
	assert.Equal(t, doc.DisplayName, got.DisplayName)
	assert.Equal(t, userID.String(), got.ID, "the document ID must survive the round-trip")
	assert.WithinDuration(t, now, got.CreatedAt, time.Second)

	// ...and the phone lookup that replaced the phone_number-index GSI.
	found, err := users.FindByPhone(ctx, phone)
	require.NoError(t, err)
	assert.Equal(t, userID.String(), found.ID)

	// A phone nobody registered is not found, not an empty document.
	unknown, err := domain.NewPhoneNumber("+525599999999")
	require.NoError(t, err)
	_, err = users.FindByPhone(ctx, unknown)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	// An unknown user ID likewise.
	_, err = users.Get(ctx, domain.GenerateUserID())
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestChatsRoundTrip(t *testing.T) {
	// Arrange
	ctx := context.Background()
	chats := firestore.NewChats(liveClient(t))

	chatID := domain.GenerateChatID()
	now := time.Now().UTC().Truncate(time.Millisecond)
	doc := firestore.ChatDoc{
		ID:        chatID.String(),
		ChatType:  "group",
		Name:      "integration",
		CreatedBy: domain.GenerateUserID().String(),
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Act
	require.NoError(t, chats.Set(ctx, doc))
	got, err := chats.Get(ctx, chatID)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, doc.ChatType, got.ChatType)
	assert.Equal(t, doc.Name, got.Name)
	assert.Equal(t, doc.CreatedBy, got.CreatedBy)

	_, err = chats.Get(ctx, domain.GenerateChatID())
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

// TestMembershipsRoundTrip exercises the reason the composite document ID
// exists: one document serves the point lookup and both list directions.
func TestMembershipsRoundTrip(t *testing.T) {
	// Arrange
	ctx := context.Background()
	memberships := firestore.NewMemberships(liveClient(t))

	chatID, userID := domain.GenerateChatID(), domain.GenerateUserID()
	doc := firestore.MembershipDoc{
		ChatID:   chatID.String(),
		UserID:   userID.String(),
		Role:     "owner",
		JoinedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	// Act
	require.NoError(t, memberships.Set(ctx, doc))

	// Assert — "is this user in this chat?" is a point read, and its ID is the
	// 74-byte composite the unit tests pin.
	got, err := memberships.Get(ctx, chatID, userID)
	require.NoError(t, err)
	assert.Equal(t, "owner", got.Role)
	assert.Equal(t, firestore.MembershipDocID(chatID, userID), got.ID)
	assert.Len(t, got.ID, 74)
	assert.Nil(t, got.MutedUntil, "an unmuted member stores no muted_until")

	byChat, err := memberships.ListByChat(ctx, chatID)
	require.NoError(t, err)
	require.Len(t, byChat, 1)
	assert.Equal(t, userID.String(), byChat[0].UserID)

	byUser, err := memberships.ListByUser(ctx, userID)
	require.NoError(t, err)
	require.Len(t, byUser, 1)
	assert.Equal(t, chatID.String(), byUser[0].ChatID)

	// Leaving removes the document; both directions go empty.
	require.NoError(t, memberships.Delete(ctx, chatID, userID))

	_, err = memberships.Get(ctx, chatID, userID)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	byChat, err = memberships.ListByChat(ctx, chatID)
	require.NoError(t, err)
	assert.Empty(t, byChat)

	// Deleting a membership that is not there is not an error — "leave a chat
	// you are not in" needs no special case.
	assert.NoError(t, memberships.Delete(ctx, chatID, userID))
}

// TestSessionsRoundTrip covers the ADR-023 invariant that TTL is garbage
// collection and not the correctness gate: an expired session is still
// readable, and refusing it is the caller's job.
func TestSessionsRoundTrip(t *testing.T) {
	// Arrange
	ctx := context.Background()
	sessions := firestore.NewSessions(liveClient(t))

	userID := domain.GenerateUserID()
	active := domain.GenerateSessionID()
	expired := domain.GenerateSessionID()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Act
	require.NoError(t, sessions.Set(ctx, firestore.SessionDoc{
		ID:               active.String(),
		UserID:           userID.String(),
		DeviceID:         domain.GenerateDeviceID().String(),
		RefreshTokenHash: "hash-current",
		TokenGeneration:  1,
		CreatedAt:        now,
		ExpiresAt:        now.Add(time.Hour),
	}))
	require.NoError(t, sessions.Set(ctx, firestore.SessionDoc{
		ID:               expired.String(),
		UserID:           userID.String(),
		DeviceID:         domain.GenerateDeviceID().String(),
		RefreshTokenHash: "hash-rotated",
		PrevTokenHash:    "hash-previous",
		TokenGeneration:  2,
		CreatedAt:        now.Add(-2 * time.Hour),
		ExpiresAt:        now.Add(-time.Hour),
	}))

	// Assert
	got, err := sessions.Get(ctx, active)
	require.NoError(t, err)
	assert.False(t, got.IsExpired(now))
	assert.Equal(t, int64(1), got.TokenGeneration)

	// The expired session reads back fine — Firestore removes it within ~24h of
	// expires_at, so the application check is what refuses it.
	stale, err := sessions.Get(ctx, expired)
	require.NoError(t, err, "an expired session is readable until TTL collects it")
	assert.True(t, stale.IsExpired(now))
	assert.Equal(t, "hash-previous", stale.PrevTokenHash, "rotation fields survive the round-trip")

	// The per-user list replaced the user_sessions-index GSI and returns both.
	byUser, err := sessions.ListByUser(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, byUser, 2)

	// Revocation is an immediate delete, not a wait for TTL.
	require.NoError(t, sessions.Delete(ctx, active))
	require.NoError(t, sessions.Delete(ctx, expired))

	_, err = sessions.Get(ctx, active)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}
