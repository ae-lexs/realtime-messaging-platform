//go:build integration

// The M1.3 service-layer gate: app.ChatService wired to the real Firestore
// adapters, not fakes — against a live database.
//
// chat_service_test.go proves the authorization logic in isolation; the store
// invariants it delegates to (AddMember, RemoveMember, Leave, SetRole,
// SetMute, SetName) have their own live gate in
// internal/firestore/chat_tx_integration_test.go. Neither proves the seam
// between them: that adapter.ChatWriter's ID parsing, error wrapping, and
// domain-to-Firestore translation actually hold when the service calls it for
// real. That is what this file is for — the same gap PR #27 identified for
// CreateChat/GetChat/ListChats, closed here for the six RPCs #29 added.
//
// One thing only this level can catch: MuteChat's indefinite-mute sentinel
// round-tripping through a real Firestore write and read. A fake echoes
// whatever value it is given; only a live database can show whether Firestore
// alters a time.Time's precision on the way through, which would break the
// exact equality mutedUntilToProto depends on to detect it.
//
// There is no emulator — dev targets a real GCP project (ADR-021 Axis F) — so
// this runs only when pointed at a live database:
//
//	PROJECT_ID=... make chat-test
package app_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/adapter"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/domain/domaintest"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// liveFirestoreClient mirrors internal/firestore's own liveClient: skip
// rather than fail when nothing points this at a live database, since that is
// the ordinary state of a laptop, not a gate failure.
func liveFirestoreClient(t *testing.T) *firestore.Client {
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

// liveChatService wires the real adapters — the composition root's own
// wiring in cmd/chatmgmt/main.go, not a parallel construction this test
// invented — over a live Firestore client.
func liveChatService(t *testing.T) *app.ChatService {
	t.Helper()

	client := liveFirestoreClient(t)
	return app.NewChatService(app.ChatServiceConfig{
		Writer: adapter.NewChatWriter(firestore.NewChatTx(client)),
		Reader: adapter.NewChatReader(firestore.NewChats(client), firestore.NewMemberships(client)),
		Users:  adapter.NewUserStore(firestore.NewUsers(client)),
		Clock:  domaintest.NewFakeClock(time.Now().UTC().Truncate(time.Millisecond)),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
}

func TestServiceUpdateChatPersistsTheNewName(t *testing.T) {
	// Arrange
	ctx := context.Background()
	svc := liveChatService(t)
	owner := domain.GenerateUserID().String()
	result, err := svc.CreateChat(ctx, app.CreateChatParams{
		CallerID: owner, ChatType: domain.ChatTypeGroup, Name: "rename gate",
		MemberIDs: []string{domain.GenerateUserID().String()},
	})
	require.NoError(t, err)
	chatID := result.Chat.ChatID

	// Act
	updated, err := svc.UpdateChat(ctx, owner, chatID, "renamed live")
	require.NoError(t, err)

	// Assert — read back through the reader, independent of what UpdateChat
	// itself returned, so this proves the write landed rather than that the
	// response was constructed correctly.
	assert.Equal(t, "renamed live", updated.Name)
	reread, _, err := svc.GetChat(ctx, owner, chatID)
	require.NoError(t, err)
	assert.Equal(t, "renamed live", reread.Name)
}

func TestServiceAddMemberAndRemoveMember(t *testing.T) {
	// Arrange
	ctx := context.Background()
	svc := liveChatService(t)
	owner := domain.GenerateUserID().String()
	target := domain.GenerateUserID().String()

	result, err := svc.CreateChat(ctx, app.CreateChatParams{
		CallerID: owner, ChatType: domain.ChatTypeGroup, Name: "add-remove gate",
		MemberIDs: []string{domain.GenerateUserID().String()},
	})
	require.NoError(t, err)
	chatID := result.Chat.ChatID

	// Act — add
	member, err := svc.AddMember(ctx, owner, chatID, target, domain.RoleMember)
	require.NoError(t, err)
	assert.Equal(t, domain.RoleMember, member.Role)

	// Assert — the membership is real, read back independently of AddMember's
	// own response.
	_, members, err := svc.GetChat(ctx, owner, chatID)
	require.NoError(t, err)
	assert.Contains(t, memberIDs(members), target)

	// Act — remove
	require.NoError(t, svc.RemoveMember(ctx, owner, chatID, target))

	// Assert
	_, members, err = svc.GetChat(ctx, owner, chatID)
	require.NoError(t, err)
	assert.NotContains(t, memberIDs(members), target)
}

func TestServiceUpdateMemberRolePersists(t *testing.T) {
	// Arrange
	ctx := context.Background()
	svc := liveChatService(t)
	owner := domain.GenerateUserID().String()
	target := domain.GenerateUserID().String()
	result, err := svc.CreateChat(ctx, app.CreateChatParams{
		CallerID: owner, ChatType: domain.ChatTypeGroup, Name: "role gate", MemberIDs: []string{target},
	})
	require.NoError(t, err)
	chatID := result.Chat.ChatID

	// Act
	member, err := svc.UpdateMemberRole(ctx, owner, chatID, target, domain.RoleAdmin)
	require.NoError(t, err)
	assert.Equal(t, domain.RoleAdmin, member.Role)

	// Assert — read back independently.
	_, members, err := svc.GetChat(ctx, owner, chatID)
	require.NoError(t, err)
	assert.Equal(t, domain.RoleAdmin, roleOf(members, target))
}

func TestServiceRemoveMemberChecksTheTargetsLiveRole(t *testing.T) {
	// Arrange — an admin trying to remove another admin. Unlike
	// chat_service_test.go's version of this case, the target's role here is
	// read back from a real Firestore write (via UpdateMemberRole), not
	// planted directly in a fake's map — proving the live lookup
	// RemoveMember's authorization depends on actually sees it.
	ctx := context.Background()
	svc := liveChatService(t)
	owner := domain.GenerateUserID().String()
	adminCaller := domain.GenerateUserID().String()
	adminTarget := domain.GenerateUserID().String()
	result, err := svc.CreateChat(ctx, app.CreateChatParams{
		CallerID: owner, ChatType: domain.ChatTypeGroup, Name: "remove-authz gate",
		MemberIDs: []string{adminCaller, adminTarget},
	})
	require.NoError(t, err)
	chatID := result.Chat.ChatID
	_, err = svc.UpdateMemberRole(ctx, owner, chatID, adminCaller, domain.RoleAdmin)
	require.NoError(t, err)
	_, err = svc.UpdateMemberRole(ctx, owner, chatID, adminTarget, domain.RoleAdmin)
	require.NoError(t, err)

	// Act
	err = svc.RemoveMember(ctx, adminCaller, chatID, adminTarget)

	// Assert
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func TestServiceLeaveChatRemovesTheCaller(t *testing.T) {
	// Arrange
	ctx := context.Background()
	svc := liveChatService(t)
	owner := domain.GenerateUserID().String()
	leaver := domain.GenerateUserID().String()
	result, err := svc.CreateChat(ctx, app.CreateChatParams{
		CallerID: owner, ChatType: domain.ChatTypeGroup, Name: "leave gate", MemberIDs: []string{leaver},
	})
	require.NoError(t, err)
	chatID := result.Chat.ChatID

	// Act
	require.NoError(t, svc.LeaveChat(ctx, leaver, chatID))

	// Assert
	_, members, err := svc.GetChat(ctx, owner, chatID)
	require.NoError(t, err)
	assert.NotContains(t, memberIDs(members), leaver)
}

func TestServiceLeaveChatRefusesTheOwner(t *testing.T) {
	// Arrange — the store's invariant (ADR-016 §4.3), exercised through the
	// service against a live database rather than a fake.
	ctx := context.Background()
	svc := liveChatService(t)
	owner := domain.GenerateUserID().String()
	result, err := svc.CreateChat(ctx, app.CreateChatParams{
		CallerID: owner, ChatType: domain.ChatTypeGroup, Name: "owner-cannot-leave gate",
		MemberIDs: []string{domain.GenerateUserID().String()},
	})
	require.NoError(t, err)

	// Act
	err = svc.LeaveChat(ctx, owner, result.Chat.ChatID)

	// Assert
	require.ErrorIs(t, err, domain.ErrInvalidOperation)
}

// TestServiceMuteChatIndefinitelyRoundTripsTheSentinel is the test this file
// exists for. domain.IndefiniteMuteUntil only works if a value written to
// Firestore reads back byte-for-byte equal — Firestore's timestamp precision,
// or a client-library conversion, could silently break the sentinel
// comparison mutedUntilToProto depends on. No fake can catch that; only a
// live write-then-read can.
func TestServiceMuteChatIndefinitelyRoundTripsTheSentinel(t *testing.T) {
	// Arrange
	ctx := context.Background()
	svc := liveChatService(t)
	owner := domain.GenerateUserID().String()
	member := domain.GenerateUserID().String()
	result, err := svc.CreateChat(ctx, app.CreateChatParams{
		CallerID: owner, ChatType: domain.ChatTypeGroup, Name: "indefinite-mute gate", MemberIDs: []string{member},
	})
	require.NoError(t, err)
	chatID := result.Chat.ChatID

	// Act
	until, err := svc.MuteChat(ctx, member, chatID, nil)
	require.NoError(t, err)
	assert.True(t, until.Equal(domain.IndefiniteMuteUntil))

	// Assert — read back independently, through GetChat's own Firestore
	// query, not the value MuteChat happened to return.
	_, members, err := svc.GetChat(ctx, owner, chatID)
	require.NoError(t, err)
	m := findMember(members, member)
	require.NotNil(t, m.MutedUntil)
	assert.True(t, m.MutedUntil.Equal(domain.IndefiniteMuteUntil),
		"the sentinel must survive a real Firestore round trip unchanged, or an indefinite mute would misrender as a real (if absurd) expiry")
}

func TestServiceMuteChatWithADurationThenUnmute(t *testing.T) {
	// Arrange
	ctx := context.Background()
	svc := liveChatService(t)
	owner := domain.GenerateUserID().String()
	member := domain.GenerateUserID().String()
	result, err := svc.CreateChat(ctx, app.CreateChatParams{
		CallerID: owner, ChatType: domain.ChatTypeGroup, Name: "timed-mute gate", MemberIDs: []string{member},
	})
	require.NoError(t, err)
	chatID := result.Chat.ChatID
	hours := int32(1)

	// Act
	until, err := svc.MuteChat(ctx, member, chatID, &hours)
	require.NoError(t, err)
	require.False(t, until.Equal(domain.IndefiniteMuteUntil), "a timed mute must not collide with the indefinite sentinel")

	// Assert — muted
	_, members, err := svc.GetChat(ctx, owner, chatID)
	require.NoError(t, err)
	require.NotNil(t, findMember(members, member).MutedUntil)

	// Act — unmute
	require.NoError(t, svc.UnmuteChat(ctx, member, chatID))

	// Assert — cleared
	_, members, err = svc.GetChat(ctx, owner, chatID)
	require.NoError(t, err)
	assert.Nil(t, findMember(members, member).MutedUntil)
}

func memberIDs(members []app.MemberRecord) []string {
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.UserID
	}
	return ids
}

func roleOf(members []app.MemberRecord, userID string) domain.Role {
	if m := findMember(members, userID); m != nil {
		return m.Role
	}
	return ""
}

func findMember(members []app.MemberRecord, userID string) *app.MemberRecord {
	for i := range members {
		if members[i].UserID == userID {
			return &members[i]
		}
	}
	return nil
}
