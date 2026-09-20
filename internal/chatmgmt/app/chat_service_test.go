package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/domain/domaintest"
)

// fakeChatWriter records what the service asked the store to write. It does not
// enforce the chat's invariants: those live in internal/firestore and are
// covered by its live gate. A fake that re-implemented them would certify a
// copy, which is the defect RTM-04 C4 found in the auth fakes.
type fakeChatWriter struct {
	direct   app.ChatRecord
	existing bool
	group    app.ChatRecord
	member   app.MemberRecord
	chat     app.ChatRecord
	err      error

	gotCaller, gotOther, gotOwner, gotName string
	gotMembers                             []string

	gotChatID, gotUserID                                                                          string
	gotRole                                                                                       domain.Role
	gotMutedUntil                                                                                 *time.Time
	addMemberCalled, removeMemberCalled, leaveCalled, setRoleCalled, setMuteCalled, setNameCalled bool
}

func (f *fakeChatWriter) CreateDirect(
	_ context.Context, callerID, otherID string, _ time.Time,
) (app.ChatRecord, bool, error) {
	f.gotCaller, f.gotOther = callerID, otherID
	return f.direct, f.existing, f.err
}

func (f *fakeChatWriter) CreateGroup(
	_ context.Context, ownerID, name string, memberIDs []string, _ time.Time,
) (app.ChatRecord, error) {
	f.gotOwner, f.gotName, f.gotMembers = ownerID, name, memberIDs
	return f.group, f.err
}

func (f *fakeChatWriter) AddMember(
	_ context.Context, chatID, userID string, role domain.Role, _ time.Time,
) (app.MemberRecord, error) {
	f.addMemberCalled = true
	f.gotChatID, f.gotUserID, f.gotRole = chatID, userID, role
	return f.member, f.err
}

func (f *fakeChatWriter) RemoveMember(_ context.Context, chatID, userID string, _ time.Time) error {
	f.removeMemberCalled = true
	f.gotChatID, f.gotUserID = chatID, userID
	return f.err
}

func (f *fakeChatWriter) Leave(_ context.Context, chatID, userID string, _ time.Time) error {
	f.leaveCalled = true
	f.gotChatID, f.gotUserID = chatID, userID
	return f.err
}

func (f *fakeChatWriter) SetRole(
	_ context.Context, chatID, userID string, role domain.Role, _ time.Time,
) (app.MemberRecord, error) {
	f.setRoleCalled = true
	f.gotChatID, f.gotUserID, f.gotRole = chatID, userID, role
	return f.member, f.err
}

func (f *fakeChatWriter) SetMute(_ context.Context, chatID, userID string, until *time.Time) error {
	f.setMuteCalled = true
	f.gotChatID, f.gotUserID, f.gotMutedUntil = chatID, userID, until
	return f.err
}

func (f *fakeChatWriter) SetName(_ context.Context, chatID, name string, _ time.Time) (app.ChatRecord, error) {
	f.setNameCalled = true
	f.gotChatID, f.gotName = chatID, name
	return f.chat, f.err
}

// fakeChatReader serves chats and memberships from maps.
type fakeChatReader struct {
	chats       map[string]app.ChatRecord
	memberships map[string]app.MemberRecord // keyed chatID+"|"+userID
	byChat      map[string][]app.MemberRecord
	byUser      map[string][]app.MemberRecord
}

func (f *fakeChatReader) GetChat(_ context.Context, chatID string) (app.ChatRecord, error) {
	chat, ok := f.chats[chatID]
	if !ok {
		return app.ChatRecord{}, domain.ErrNotFound
	}
	return chat, nil
}

func (f *fakeChatReader) GetMembership(_ context.Context, chatID, userID string) (app.MemberRecord, error) {
	m, ok := f.memberships[chatID+"|"+userID]
	if !ok {
		return app.MemberRecord{}, domain.ErrNotFound
	}
	return m, nil
}

func (f *fakeChatReader) ListMembers(_ context.Context, chatID string) ([]app.MemberRecord, error) {
	return f.byChat[chatID], nil
}

func (f *fakeChatReader) ListUserChats(_ context.Context, userID string) ([]app.MemberRecord, error) {
	return f.byUser[userID], nil
}

// fakeUserStore resolves display names.
type fakeUserStore struct{ users map[string]app.UserRecord }

func (f *fakeUserStore) GetByID(_ context.Context, userID string) (*app.UserRecord, error) {
	u, ok := f.users[userID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &u, nil
}

func (f *fakeUserStore) FindByPhone(context.Context, string) (*app.UserRecord, error) {
	return nil, domain.ErrNotFound
}

func newChatService(w *fakeChatWriter, r *fakeChatReader, u *fakeUserStore) *app.ChatService {
	return app.NewChatService(app.ChatServiceConfig{
		Writer: w,
		Reader: r,
		Users:  u,
		Clock:  domaintest.NewFakeClock(time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func emptyReader() *fakeChatReader {
	return &fakeChatReader{
		chats:       map[string]app.ChatRecord{},
		memberships: map[string]app.MemberRecord{},
		byChat:      map[string][]app.MemberRecord{},
		byUser:      map[string][]app.MemberRecord{},
	}
}

func TestCreateChatValidation(t *testing.T) {
	caller := domain.GenerateUserID().String()
	other := domain.GenerateUserID().String()

	tooMany := make([]string, domain.MaxGroupSize)
	for i := range tooMany {
		tooMany[i] = domain.GenerateUserID().String()
	}

	tests := []struct {
		name    string
		params  app.CreateChatParams
		wantErr error
	}{
		{
			name: "a direct chat needs exactly one other member",
			params: app.CreateChatParams{
				CallerID: caller,
				ChatType: domain.ChatTypeDirect,
				// ADR-006 §4.1: member_ids excludes the caller, so two entries
				// means a three-person direct chat, which does not exist.
				MemberIDs: []string{other, domain.GenerateUserID().String()},
			},
			wantErr: domain.ErrInvalidInput,
		},
		{
			name: "a direct chat with nobody is refused",
			params: app.CreateChatParams{
				CallerID: caller, ChatType: domain.ChatTypeDirect, MemberIDs: nil,
			},
			wantErr: domain.ErrInvalidInput,
		},
		{
			// The trap the proto comment used to set: a client that includes
			// itself would otherwise create a chat where it is listed twice.
			name: "the caller may not list itself",
			params: app.CreateChatParams{
				CallerID: caller, ChatType: domain.ChatTypeDirect, MemberIDs: []string{caller},
			},
			wantErr: domain.ErrInvalidInput,
		},
		{
			name: "a group needs a name",
			params: app.CreateChatParams{
				CallerID: caller, ChatType: domain.ChatTypeGroup, MemberIDs: []string{other},
			},
			wantErr: domain.ErrInvalidInput,
		},
		{
			name: "a group needs at least one other member",
			params: app.CreateChatParams{
				CallerID: caller, ChatType: domain.ChatTypeGroup, Name: "empty", MemberIDs: nil,
			},
			wantErr: domain.ErrInvalidInput,
		},
		{
			// The owner counts toward the maximum, so MaxGroupSize others is
			// one too many (ADR-006 §4.1).
			name: "a group may not exceed the maximum once the owner is counted",
			params: app.CreateChatParams{
				CallerID: caller, ChatType: domain.ChatTypeGroup, Name: "too big", MemberIDs: tooMany,
			},
			wantErr: domain.ErrInvalidInput,
		},
		{
			name: "an unspecified chat type is refused rather than defaulted",
			params: app.CreateChatParams{
				CallerID: caller, ChatType: domain.ChatType(""), MemberIDs: []string{other},
			},
			wantErr: domain.ErrInvalidInput,
		},
		{
			name: "a request with no caller is unauthorized",
			params: app.CreateChatParams{
				ChatType: domain.ChatTypeDirect, MemberIDs: []string{other},
			},
			wantErr: domain.ErrUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			writer := &fakeChatWriter{}
			svc := newChatService(writer, emptyReader(), &fakeUserStore{})

			// Act
			_, err := svc.CreateChat(context.Background(), tt.params)

			// Assert
			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, writer.gotCaller, "an invalid request must not reach the store")
			assert.Empty(t, writer.gotOwner, "an invalid request must not reach the store")
		})
	}
}

func TestCreateDirectChatUsesTheCallerAsAParticipant(t *testing.T) {
	// Arrange
	caller := domain.GenerateUserID().String()
	other := domain.GenerateUserID().String()
	chatID := domain.GenerateChatID().String()

	writer := &fakeChatWriter{direct: app.ChatRecord{ChatID: chatID, ChatType: domain.ChatTypeDirect}}
	reader := emptyReader()
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act
	result, err := svc.CreateChat(context.Background(), app.CreateChatParams{
		CallerID: caller, ChatType: domain.ChatTypeDirect, MemberIDs: []string{other},
	})

	// Assert — the caller comes from the authenticated identity, never the
	// body, so a client cannot open a conversation on someone else's behalf.
	require.NoError(t, err)
	assert.Equal(t, caller, writer.gotCaller)
	assert.Equal(t, other, writer.gotOther)
	assert.False(t, result.Existing)
}

func TestCreateDirectChatReportsAReplay(t *testing.T) {
	// Arrange — the pair already has a chat.
	writer := &fakeChatWriter{
		direct:   app.ChatRecord{ChatID: domain.GenerateChatID().String(), ChatType: domain.ChatTypeDirect},
		existing: true,
	}
	svc := newChatService(writer, emptyReader(), &fakeUserStore{})

	// Act
	result, err := svc.CreateChat(context.Background(), app.CreateChatParams{
		CallerID:  domain.GenerateUserID().String(),
		ChatType:  domain.ChatTypeDirect,
		MemberIDs: []string{domain.GenerateUserID().String()},
	})

	// Assert — ADR-006 §4.1 answers an existing direct chat with 200 and a
	// replay flag, not an error: a client may create one without checking.
	require.NoError(t, err)
	assert.True(t, result.Existing)
}

func TestGetChatRefusesANonMember(t *testing.T) {
	// Arrange — the chat exists; the caller is not in it.
	chatID := domain.GenerateChatID().String()
	reader := emptyReader()
	reader.chats[chatID] = app.ChatRecord{ChatID: chatID, ChatType: domain.ChatTypeGroup}

	svc := newChatService(&fakeChatWriter{}, reader, &fakeUserStore{})

	// Act
	_, _, err := svc.GetChat(context.Background(), domain.GenerateUserID().String(), chatID)

	// Assert — 403 NOT_A_MEMBER, not 404. The chat's existence is not secret
	// from someone holding its ID; its contents are.
	require.ErrorIs(t, err, domain.ErrNotMember)
}

func TestGetChatOnAMissingChatIsNotFound(t *testing.T) {
	// Arrange
	svc := newChatService(&fakeChatWriter{}, emptyReader(), &fakeUserStore{})

	// Act
	_, _, err := svc.GetChat(context.Background(),
		domain.GenerateUserID().String(), domain.GenerateChatID().String())

	// Assert — and NOT ErrNotMember, or the API becomes an oracle for which
	// chat IDs are real.
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestGetChatFillsDisplayNames(t *testing.T) {
	// Arrange
	chatID := domain.GenerateChatID().String()
	caller := domain.GenerateUserID().String()

	reader := emptyReader()
	reader.chats[chatID] = app.ChatRecord{ChatID: chatID, ChatType: domain.ChatTypeGroup}
	reader.memberships[chatID+"|"+caller] = app.MemberRecord{ChatID: chatID, UserID: caller, Role: domain.RoleOwner}
	reader.byChat[chatID] = []app.MemberRecord{{ChatID: chatID, UserID: caller, Role: domain.RoleOwner}}

	users := &fakeUserStore{users: map[string]app.UserRecord{caller: {UserID: caller, DisplayName: "Alexis"}}}
	svc := newChatService(&fakeChatWriter{}, reader, users)

	// Act
	_, members, err := svc.GetChat(context.Background(), caller, chatID)

	// Assert
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "Alexis", members[0].DisplayName)
}

func TestGetChatToleratesAMemberWithNoUser(t *testing.T) {
	// Arrange — cross-store drift: a membership naming a user that is gone.
	// ADR-023 makes these references application-enforced, so this is possible
	// and must not fail the read.
	chatID := domain.GenerateChatID().String()
	caller := domain.GenerateUserID().String()
	ghost := domain.GenerateUserID().String()

	reader := emptyReader()
	reader.chats[chatID] = app.ChatRecord{ChatID: chatID, ChatType: domain.ChatTypeGroup}
	reader.memberships[chatID+"|"+caller] = app.MemberRecord{ChatID: chatID, UserID: caller}
	reader.byChat[chatID] = []app.MemberRecord{
		{ChatID: chatID, UserID: caller},
		{ChatID: chatID, UserID: ghost},
	}

	users := &fakeUserStore{users: map[string]app.UserRecord{caller: {UserID: caller, DisplayName: "Alexis"}}}
	svc := newChatService(&fakeChatWriter{}, reader, users)

	// Act
	_, members, err := svc.GetChat(context.Background(), caller, chatID)

	// Assert — the membership is real, so it is returned without a name rather
	// than dropped, which would understate the chat's membership.
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.Empty(t, members[1].DisplayName)
}

// membership seeds a reader with one caller's role in one chat, group by
// default — every membership-mutation test needs at least this much.
func membership(reader *fakeChatReader, chatID, userID string, role domain.Role) {
	reader.chats[chatID] = app.ChatRecord{ChatID: chatID, ChatType: domain.ChatTypeGroup}
	reader.memberships[chatID+"|"+userID] = app.MemberRecord{ChatID: chatID, UserID: userID, Role: role}
}

func TestUpdateChatAuthorization(t *testing.T) {
	tests := []struct {
		name    string
		role    domain.Role
		wantErr error
	}{
		{"owner may rename", domain.RoleOwner, nil},
		{"admin may rename", domain.RoleAdmin, nil},
		{"a member may not rename", domain.RoleMember, domain.ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			chatID, caller := domain.GenerateChatID().String(), domain.GenerateUserID().String()
			reader := emptyReader()
			membership(reader, chatID, caller, tt.role)
			writer := &fakeChatWriter{chat: app.ChatRecord{ChatID: chatID, Name: "new name"}}
			svc := newChatService(writer, reader, &fakeUserStore{})

			// Act
			_, err := svc.UpdateChat(context.Background(), caller, chatID, "new name")

			// Assert
			if tt.wantErr == nil {
				require.NoError(t, err)
				assert.True(t, writer.setNameCalled)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
				assert.False(t, writer.setNameCalled, "a refused rename must not reach the store")
			}
		})
	}
}

func TestUpdateChatRefusesAnInvalidName(t *testing.T) {
	// Arrange
	chatID, caller := domain.GenerateChatID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleOwner)
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act
	_, err := svc.UpdateChat(context.Background(), caller, chatID, "")

	// Assert — validated before the store is asked who may rename, so an empty
	// name fails the same way whether or not the caller could have renamed it.
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	assert.False(t, writer.setNameCalled)
}

func TestAddMemberAuthorization(t *testing.T) {
	tests := []struct {
		name       string
		callerRole domain.Role
		grantRole  domain.Role
		wantErr    error
	}{
		{"owner may add a plain member", domain.RoleOwner, domain.RoleMember, nil},
		{"owner may add an admin", domain.RoleOwner, domain.RoleAdmin, nil},
		{"admin may add a plain member", domain.RoleAdmin, domain.RoleMember, nil},
		{
			"an admin may not grant admin — a second, weaker path to a privilege " +
				"UpdateMemberRole restricts to the owner",
			domain.RoleAdmin, domain.RoleAdmin, domain.ErrForbidden,
		},
		{"a member may not add anyone", domain.RoleMember, domain.RoleMember, domain.ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			chatID, caller, target := domain.GenerateChatID().String(), domain.GenerateUserID().String(), domain.GenerateUserID().String()
			reader := emptyReader()
			membership(reader, chatID, caller, tt.callerRole)
			writer := &fakeChatWriter{member: app.MemberRecord{ChatID: chatID, UserID: target, Role: tt.grantRole}}
			svc := newChatService(writer, reader, &fakeUserStore{})

			// Act
			_, err := svc.AddMember(context.Background(), caller, chatID, target, tt.grantRole)

			// Assert
			if tt.wantErr == nil {
				require.NoError(t, err)
				assert.True(t, writer.addMemberCalled)
				assert.Equal(t, tt.grantRole, writer.gotRole)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
				assert.False(t, writer.addMemberCalled, "a refused add must not reach the store")
			}
		})
	}
}

func TestAddMemberDefaultsAnUnspecifiedRoleToMember(t *testing.T) {
	// Arrange
	chatID, caller, target := domain.GenerateChatID().String(), domain.GenerateUserID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleOwner)
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act — the zero value of domain.Role, what an unspecified proto enum maps
	// to (ADR-006 §4.5: "Defaults to MEMBER when unspecified").
	_, err := svc.AddMember(context.Background(), caller, chatID, target, domain.Role(""))

	// Assert
	require.NoError(t, err)
	assert.Equal(t, domain.RoleMember, writer.gotRole)
}

func TestAddMemberRefusesGrantingOwner(t *testing.T) {
	// Arrange
	chatID, caller, target := domain.GenerateChatID().String(), domain.GenerateUserID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleOwner)
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act — ownership is conferred once, by creating the group; there is no
	// grant path (ADR-016 §4.3).
	_, err := svc.AddMember(context.Background(), caller, chatID, target, domain.RoleOwner)

	// Assert
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	assert.False(t, writer.addMemberCalled)
}

func TestRemoveMemberAuthorization(t *testing.T) {
	tests := []struct {
		name       string
		callerRole domain.Role
		targetRole domain.Role
		wantErr    error
	}{
		{"owner may remove a plain member", domain.RoleOwner, domain.RoleMember, nil},
		{"owner may remove an admin", domain.RoleOwner, domain.RoleAdmin, nil},
		{"admin may remove a plain member", domain.RoleAdmin, domain.RoleMember, nil},
		{"an admin may not remove another admin", domain.RoleAdmin, domain.RoleAdmin, domain.ErrForbidden},
		{"an admin may not remove the owner", domain.RoleAdmin, domain.RoleOwner, domain.ErrForbidden},
		{"a member may not remove anyone", domain.RoleMember, domain.RoleMember, domain.ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			chatID, caller, target := domain.GenerateChatID().String(), domain.GenerateUserID().String(), domain.GenerateUserID().String()
			reader := emptyReader()
			membership(reader, chatID, caller, tt.callerRole)
			reader.memberships[chatID+"|"+target] = app.MemberRecord{ChatID: chatID, UserID: target, Role: tt.targetRole}
			writer := &fakeChatWriter{}
			svc := newChatService(writer, reader, &fakeUserStore{})

			// Act
			err := svc.RemoveMember(context.Background(), caller, chatID, target)

			// Assert
			if tt.wantErr == nil {
				require.NoError(t, err)
				assert.True(t, writer.removeMemberCalled)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
				assert.False(t, writer.removeMemberCalled, "a refused removal must not reach the store")
			}
		})
	}
}

func TestRemoveMemberRefusesSelfRemoval(t *testing.T) {
	// Arrange
	chatID, caller := domain.GenerateChatID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleOwner)
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act — "remove yourself" and "leave" are the same write; ADR-006 keeps
	// them as two endpoints with two authorization stories.
	err := svc.RemoveMember(context.Background(), caller, chatID, caller)

	// Assert
	require.ErrorIs(t, err, domain.ErrInvalidOperation)
	assert.False(t, writer.removeMemberCalled)
}

func TestUpdateMemberRoleAuthorization(t *testing.T) {
	// Arrange
	chatID, caller, target := domain.GenerateChatID().String(), domain.GenerateUserID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleAdmin) // admin, not owner
	reader.memberships[chatID+"|"+target] = app.MemberRecord{ChatID: chatID, UserID: target, Role: domain.RoleMember}
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act — only the owner may change roles (ADR-006 §4.7).
	_, err := svc.UpdateMemberRole(context.Background(), caller, chatID, target, domain.RoleAdmin)

	// Assert
	require.ErrorIs(t, err, domain.ErrForbidden)
	assert.False(t, writer.setRoleCalled)
}

func TestUpdateMemberRoleAllowsTheOwner(t *testing.T) {
	// Arrange
	chatID, caller, target := domain.GenerateChatID().String(), domain.GenerateUserID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleOwner)
	reader.memberships[chatID+"|"+target] = app.MemberRecord{ChatID: chatID, UserID: target, Role: domain.RoleMember}
	writer := &fakeChatWriter{member: app.MemberRecord{ChatID: chatID, UserID: target, Role: domain.RoleAdmin}}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act
	_, err := svc.UpdateMemberRole(context.Background(), caller, chatID, target, domain.RoleAdmin)

	// Assert
	require.NoError(t, err)
	assert.True(t, writer.setRoleCalled)
	assert.Equal(t, domain.RoleAdmin, writer.gotRole)
}

func TestUpdateMemberRoleRefusesChangingOwnRole(t *testing.T) {
	// Arrange
	chatID, caller := domain.GenerateChatID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleOwner)
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act
	_, err := svc.UpdateMemberRole(context.Background(), caller, chatID, caller, domain.RoleAdmin)

	// Assert
	require.ErrorIs(t, err, domain.ErrInvalidOperation)
	assert.False(t, writer.setRoleCalled)
}

func TestUpdateMemberRoleRefusesAssigningOwner(t *testing.T) {
	// Arrange
	chatID, caller, target := domain.GenerateChatID().String(), domain.GenerateUserID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleOwner)
	reader.memberships[chatID+"|"+target] = app.MemberRecord{ChatID: chatID, UserID: target, Role: domain.RoleMember}
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act — ownership transfer is out of scope for the MVP (ADR-016 §4.3).
	_, err := svc.UpdateMemberRole(context.Background(), caller, chatID, target, domain.RoleOwner)

	// Assert
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	assert.False(t, writer.setRoleCalled)
}

func TestLeaveChatRequiresMembership(t *testing.T) {
	// Arrange
	chatID := domain.GenerateChatID().String()
	reader := emptyReader()
	reader.chats[chatID] = app.ChatRecord{ChatID: chatID, ChatType: domain.ChatTypeGroup}
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act
	err := svc.LeaveChat(context.Background(), domain.GenerateUserID().String(), chatID)

	// Assert
	require.ErrorIs(t, err, domain.ErrNotMember)
	assert.False(t, writer.leaveCalled)
}

func TestLeaveChatAllowsAnyMember(t *testing.T) {
	// Arrange — the store, not the service, refuses the owner (fakeChatWriter
	// does not re-implement that invariant; internal/firestore's live gate
	// covers it).
	chatID, caller := domain.GenerateChatID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleMember)
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act
	err := svc.LeaveChat(context.Background(), caller, chatID)

	// Assert
	require.NoError(t, err)
	assert.True(t, writer.leaveCalled)
}

func TestMuteChatWithADuration(t *testing.T) {
	// Arrange
	chatID, caller := domain.GenerateChatID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleMember)
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})
	hours := int32(24)

	// Act
	until, err := svc.MuteChat(context.Background(), caller, chatID, &hours)

	// Assert — the fixed clock in newChatService is 2026-08-12T12:00:00Z.
	require.NoError(t, err)
	assert.True(t, writer.setMuteCalled)
	require.NotNil(t, writer.gotMutedUntil)
	assert.Equal(t, time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC), *writer.gotMutedUntil)
	assert.Equal(t, *writer.gotMutedUntil, until)
}

func TestMuteChatIndefinitely(t *testing.T) {
	// Arrange
	chatID, caller := domain.GenerateChatID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleMember)
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act — an absent duration means indefinite, not unmuted.
	until, err := svc.MuteChat(context.Background(), caller, chatID, nil)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, writer.gotMutedUntil)
	assert.Equal(t, domain.IndefiniteMuteUntil, *writer.gotMutedUntil)
	assert.Equal(t, domain.IndefiniteMuteUntil, until)
}

func TestMuteChatRejectsADurationBeyondTheCeiling(t *testing.T) {
	// Arrange
	chatID, caller := domain.GenerateChatID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleMember)
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})
	tooLong := int32(domain.MaxMuteDurationHours + 1)

	// Act
	_, err := svc.MuteChat(context.Background(), caller, chatID, &tooLong)

	// Assert
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	assert.False(t, writer.setMuteCalled, "validated before membership is even checked")
}

func TestUnmuteChatClearsTheMute(t *testing.T) {
	// Arrange
	chatID, caller := domain.GenerateChatID().String(), domain.GenerateUserID().String()
	reader := emptyReader()
	membership(reader, chatID, caller, domain.RoleMember)
	writer := &fakeChatWriter{}
	svc := newChatService(writer, reader, &fakeUserStore{})

	// Act
	err := svc.UnmuteChat(context.Background(), caller, chatID)

	// Assert
	require.NoError(t, err)
	assert.True(t, writer.setMuteCalled)
	assert.Nil(t, writer.gotMutedUntil)
}

func TestListChatsSkipsMembershipsWhoseChatIsGone(t *testing.T) {
	// Arrange — one live chat, one dangling membership.
	caller := domain.GenerateUserID().String()
	live := domain.GenerateChatID().String()
	dangling := domain.GenerateChatID().String()

	reader := emptyReader()
	reader.chats[live] = app.ChatRecord{ChatID: live, ChatType: domain.ChatTypeGroup}
	reader.byUser[caller] = []app.MemberRecord{{ChatID: live, UserID: caller}, {ChatID: dangling, UserID: caller}}

	svc := newChatService(&fakeChatWriter{}, reader, &fakeUserStore{})

	// Act
	chats, err := svc.ListChats(context.Background(), caller)

	// Assert — one bad reference must not fail the whole listing.
	require.NoError(t, err)
	require.Len(t, chats, 1)
	assert.Equal(t, live, chats[0].ChatID)
}
