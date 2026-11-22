package port

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	messagingv1 "github.com/aelexs/realtime-messaging-platform/gen/messaging/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// chatFieldsLeftOutOfListItem names every Chat field ChatListItem deliberately
// does not carry, with the reason. ADR-006 §4.2 lists neither.
var chatFieldsLeftOutOfListItem = map[protoreflect.Name]string{
	"last_sequence": "sync state served from Postgres (M2.2); §4.2 carries last_message instead",
	"created_by":    "not listed in ADR-006 §4.2",
}

// TestChatListItemCoversChat is the drift guard: it fails if Chat gains a field
// that ChatListItem neither carries (same name, same kind) nor lists above.
func TestChatListItemCoversChat(t *testing.T) {
	chat := (&messagingv1.Chat{}).ProtoReflect().Descriptor().Fields()
	item := (&messagingv1.ChatListItem{}).ProtoReflect().Descriptor().Fields()

	for i := range chat.Len() {
		f := chat.Get(i)
		if _, leftOut := chatFieldsLeftOutOfListItem[f.Name()]; leftOut {
			assert.Nil(t, item.ByName(f.Name()), "%s is listed as left out but ChatListItem has it", f.Name())
			continue
		}
		got := item.ByName(f.Name())
		if assert.NotNil(t, got, "Chat.%s has no counterpart in ChatListItem: add it, or list it as left out with a reason", f.Name()) {
			assert.Equal(t, f.Kind(), got.Kind(), "Chat.%s and ChatListItem.%s differ in kind", f.Name(), f.Name())
		}
	}
}

func TestChatListItemToProto(t *testing.T) {
	muted := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	joined := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)

	t.Run("direct chat carries membership and other member", func(t *testing.T) {
		item := chatListItemToProto(app.ChatListEntry{
			Chat:        app.ChatRecord{ChatID: "c1", ChatType: domain.ChatTypeDirect, MemberCount: 2},
			Membership:  app.MemberRecord{Role: domain.RoleMember, JoinedAt: joined, MutedUntil: &muted},
			OtherMember: &app.OtherMemberRecord{UserID: "u2", DisplayName: "Carol"},
		})
		assert.Equal(t, "c1", item.GetChatId())
		assert.Equal(t, messagingv1.ChatType_CHAT_TYPE_DIRECT, item.GetChatType())
		assert.Equal(t, messagingv1.MemberRole_MEMBER_ROLE_MEMBER, item.GetMyMembership().GetRole())
		assert.Equal(t, joined.UnixMilli(), item.GetMyMembership().GetJoinedAt().GetMillis())
		assert.Equal(t, muted.UnixMilli(), item.GetMyMembership().GetMutedUntil().GetMillis())
		assert.Equal(t, "u2", item.GetOtherMember().GetUserId())
		assert.Equal(t, "Carol", item.GetOtherMember().GetDisplayName())
	})

	t.Run("group chat has no other member and an unmuted membership", func(t *testing.T) {
		item := chatListItemToProto(app.ChatListEntry{
			Chat:       app.ChatRecord{ChatID: "g1", ChatType: domain.ChatTypeGroup, Name: "Team", MemberCount: 5},
			Membership: app.MemberRecord{Role: domain.RoleOwner, JoinedAt: joined},
		})
		assert.Equal(t, "Team", item.GetName())
		assert.Equal(t, int32(5), item.GetMemberCount())
		assert.Equal(t, messagingv1.MemberRole_MEMBER_ROLE_OWNER, item.GetMyMembership().GetRole())
		assert.Nil(t, item.GetMyMembership().GetMutedUntil())
		assert.Nil(t, item.GetOtherMember())
	})

	t.Run("indefinite mute renders as absent, as everywhere else", func(t *testing.T) {
		forever := domain.IndefiniteMuteUntil
		item := chatListItemToProto(app.ChatListEntry{
			Chat:       app.ChatRecord{ChatID: "g1", ChatType: domain.ChatTypeGroup},
			Membership: app.MemberRecord{Role: domain.RoleMember, MutedUntil: &forever},
		})
		assert.Nil(t, item.GetMyMembership().GetMutedUntil())
	})
}

// TestListChatsItemJSONShape checks the REST rendering of one direct item
// against ADR-006 §4.2's fields, under the API's JSON conventions (camelCase,
// unset messages as null). last_message and pending_ack_count are out of scope.
func TestListChatsItemJSONShape(t *testing.T) {
	resp := &messagingv1.ListChatsResponse{Items: []*messagingv1.ChatListItem{chatListItemToProto(app.ChatListEntry{
		Chat:        app.ChatRecord{ChatID: "c1", ChatType: domain.ChatTypeDirect, MemberCount: 2},
		Membership:  app.MemberRecord{Role: domain.RoleMember},
		OtherMember: &app.OtherMemberRecord{UserID: "u2", DisplayName: "Carol"},
	})}}

	data, err := marshalerFor(t).Marshal(resp)
	require.NoError(t, err)

	var decoded struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Len(t, decoded.Items, 1)

	keys := make([]string, 0, len(decoded.Items[0]))
	for k := range decoded.Items[0] {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{
		"chatId", "chatType", "name", "createdAt", "updatedAt", "memberCount", "myMembership", "otherMember",
	}, keys)

	var mine map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(decoded.Items[0]["myMembership"], &mine))
	assert.Contains(t, mine, "role")
	assert.Contains(t, mine, "joinedAt")
	// muted_until is proto3 optional (a synthetic oneof), which EmitUnpopulated
	// does not emit: an unmuted membership omits the key, where ADR-006 §4.2
	// shows null. Recorded as part of the API-wide JSON divergence.
	assert.NotContains(t, mine, "mutedUntil", "an unset optional is omitted, not rendered as null")
}
