package port

import (
	"context"
	"fmt"
	"time"

	messagingv1 "github.com/aelexs/realtime-messaging-platform/gen/messaging/v1"
	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/errmap"
)

// chatService is the narrow, consumer-defined interface of the operations this
// handler needs. *app.ChatService satisfies it.
type chatService interface {
	CreateChat(ctx context.Context, params app.CreateChatParams) (*app.CreateChatResult, error)
	GetChat(ctx context.Context, callerID, chatID string) (*app.ChatRecord, []app.MemberRecord, error)
	ListChats(ctx context.Context, callerID string) ([]app.ChatRecord, error)
	UpdateChat(ctx context.Context, callerID, chatID, name string) (*app.ChatRecord, error)
	AddMember(ctx context.Context, callerID, chatID, userID string, role domain.Role) (*app.MemberRecord, error)
	RemoveMember(ctx context.Context, callerID, chatID, userID string) error
	UpdateMemberRole(ctx context.Context, callerID, chatID, userID string, role domain.Role) (*app.MemberRecord, error)
	LeaveChat(ctx context.Context, callerID, chatID string) error
	MuteChat(ctx context.Context, callerID, chatID string, durationHours *int32) (time.Time, error)
	UnmuteChat(ctx context.Context, callerID, chatID string) error
}

// ChatHandler implements the chat half of ChatMgmtServiceServer.
//
// It is not registered directly. AuthenticatedChatServer wraps it, so by the
// time a method here runs the caller has been authenticated and injected into
// the context; every method reads that caller rather than trusting anything in
// the request body.
//
// **The RPCs it does not implement answer Unimplemented, by design.** The proto
// declares all ten of ADR-006 §4's endpoints. GetMessages is the one still
// missing: it needs message history, which does not exist until M2 builds the
// write path. Every chat-lifecycle and membership RPC ships here with its own
// service method and tests, rather than as an empty handler returning an
// empty response, which would look implemented from the outside
// (execution-plan Principle 3).
type ChatHandler struct {
	messagingv1.UnimplementedChatMgmtServiceServer
	svc chatService
}

// NewChatHandler creates a ChatHandler backed by the given service.
func NewChatHandler(svc *app.ChatService) *ChatHandler {
	return &ChatHandler{svc: svc}
}

// CreateChat creates a direct or group chat (ADR-006 §4.1).
func (h *ChatHandler) CreateChat(
	ctx context.Context, req *messagingv1.CreateChatRequest,
) (*messagingv1.CreateChatResponse, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	result, err := h.svc.CreateChat(ctx, app.CreateChatParams{
		CallerID:  caller.UserID,
		ChatType:  chatTypeFromProto(req.GetChatType()),
		Name:      req.GetName(),
		MemberIDs: req.GetMemberIds(),
	})
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.CreateChatResponse{
		Chat:       chatToProto(result.Chat),
		IsExisting: result.Existing,
		Members:    membersToProto(result.Members),
	}, nil
}

// GetChat returns a chat and its members (ADR-006 §4.3).
func (h *ChatHandler) GetChat(
	ctx context.Context, req *messagingv1.GetChatRequest,
) (*messagingv1.GetChatResponse, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	chat, members, err := h.svc.GetChat(ctx, caller.UserID, req.GetChatId())
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	resp := &messagingv1.GetChatResponse{
		Chat:    chatToProto(*chat),
		Members: membersToProto(members),
	}
	for _, m := range members {
		if m.UserID == caller.UserID {
			resp.MyMembership = memberToProto(m)
			break
		}
	}

	return resp, nil
}

// ListChats returns the caller's chats (ADR-006 §4.2).
func (h *ChatHandler) ListChats(
	ctx context.Context, _ *messagingv1.ListChatsRequest,
) (*messagingv1.ListChatsResponse, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	chats, err := h.svc.ListChats(ctx, caller.UserID)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	out := make([]*messagingv1.Chat, 0, len(chats))
	for _, c := range chats {
		out = append(out, chatToProto(c))
	}

	// Pagination is declared in the proto and not yet honoured: ADR-006 §4.2's
	// cursor is over an ordering this service cannot produce until the write
	// path supplies last_message (M2.2). Returning every chat is correct for
	// the MaxConcurrentChats ceiling and is not a silent truncation — the
	// response carries no next_page_token, so a client cannot mistake a partial
	// answer for a complete one.
	return &messagingv1.ListChatsResponse{Chats: out}, nil
}

// UpdateChat renames a group chat (ADR-006 §4.4).
func (h *ChatHandler) UpdateChat(
	ctx context.Context, req *messagingv1.UpdateChatRequest,
) (*messagingv1.UpdateChatResponse, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	chat, err := h.svc.UpdateChat(ctx, caller.UserID, req.GetChatId(), req.GetName())
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.UpdateChatResponse{Chat: chatToProto(*chat)}, nil
}

// AddMember adds a user to a group chat (ADR-006 §4.5).
func (h *ChatHandler) AddMember(
	ctx context.Context, req *messagingv1.AddMemberRequest,
) (*messagingv1.AddMemberResponse, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	member, err := h.svc.AddMember(ctx, caller.UserID, req.GetChatId(), req.GetUserId(), roleFromProto(req.GetRole()))
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.AddMemberResponse{
		Member:  memberToProto(*member),
		AddedBy: caller.UserID,
	}, nil
}

// RemoveMember removes a user from a group chat (ADR-006 §4.6).
func (h *ChatHandler) RemoveMember(
	ctx context.Context, req *messagingv1.RemoveMemberRequest,
) (*messagingv1.RemoveMemberResponse, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	if err := h.svc.RemoveMember(ctx, caller.UserID, req.GetChatId(), req.GetUserId()); err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.RemoveMemberResponse{}, nil
}

// UpdateMemberRole changes a member's role (ADR-006 §4.7).
func (h *ChatHandler) UpdateMemberRole(
	ctx context.Context, req *messagingv1.UpdateMemberRoleRequest,
) (*messagingv1.UpdateMemberRoleResponse, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	member, err := h.svc.UpdateMemberRole(
		ctx, caller.UserID, req.GetChatId(), req.GetUserId(), roleFromProto(req.GetRole()))
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.UpdateMemberRoleResponse{
		Member:    memberToProto(*member),
		UpdatedBy: caller.UserID,
	}, nil
}

// LeaveChat removes the caller from a group chat (ADR-006 §4.8).
func (h *ChatHandler) LeaveChat(
	ctx context.Context, req *messagingv1.LeaveChatRequest,
) (*messagingv1.LeaveChatResponse, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	if err := h.svc.LeaveChat(ctx, caller.UserID, req.GetChatId()); err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.LeaveChatResponse{}, nil
}

// MuteChat silences notifications for the calling user (ADR-006 §4.9).
func (h *ChatHandler) MuteChat(
	ctx context.Context, req *messagingv1.MuteChatRequest,
) (*messagingv1.MuteChatResponse, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	var duration *int32
	if req.DurationHours != nil {
		duration = req.DurationHours
	}

	until, err := h.svc.MuteChat(ctx, caller.UserID, req.GetChatId(), duration)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.MuteChatResponse{
		ChatId:     req.GetChatId(),
		MutedUntil: mutedUntilToProto(&until),
	}, nil
}

// UnmuteChat clears the mute (ADR-006 §4.10).
func (h *ChatHandler) UnmuteChat(
	ctx context.Context, req *messagingv1.UnmuteChatRequest,
) (*messagingv1.UnmuteChatResponse, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	if err := h.svc.UnmuteChat(ctx, caller.UserID, req.GetChatId()); err != nil {
		return nil, errmap.ToGRPCError(err)
	}

	return &messagingv1.UnmuteChatResponse{ChatId: req.GetChatId()}, nil
}

// requireCaller extracts the authenticated caller, refusing if it is absent.
//
// Absence is a wiring fault — the decorator injects it on every path — and the
// only safe reading of a wiring fault on an authenticated endpoint is denial.
// It is a distinct check rather than an assumption so that registering this
// handler without its wrapper fails closed instead of serving every request as
// an anonymous user with an empty ID.
func requireCaller(ctx context.Context) (Caller, error) {
	caller, ok := CallerFrom(ctx)
	if !ok || caller.UserID == "" {
		return Caller{}, fmt.Errorf("request reached the handler unauthenticated: %w", domain.ErrUnauthorized)
	}
	return caller, nil
}

func chatTypeFromProto(t messagingv1.ChatType) domain.ChatType {
	switch t {
	case messagingv1.ChatType_CHAT_TYPE_DIRECT:
		return domain.ChatTypeDirect
	case messagingv1.ChatType_CHAT_TYPE_GROUP:
		return domain.ChatTypeGroup
	case messagingv1.ChatType_CHAT_TYPE_UNSPECIFIED:
		// Mapped to the empty type, which the service rejects as invalid input.
		// Defaulting an omitted field to a real chat type would create
		// something the caller never asked for.
		return domain.ChatType("")
	default:
		return domain.ChatType("")
	}
}

func chatTypeToProto(t domain.ChatType) messagingv1.ChatType {
	switch t {
	case domain.ChatTypeDirect:
		return messagingv1.ChatType_CHAT_TYPE_DIRECT
	case domain.ChatTypeGroup:
		return messagingv1.ChatType_CHAT_TYPE_GROUP
	default:
		return messagingv1.ChatType_CHAT_TYPE_UNSPECIFIED
	}
}

// roleFromProto defaults an unspecified role to MEMBER (ADR-006 §4.5's "Role
// to grant. Defaults to MEMBER when unspecified"), unlike chatTypeFromProto's
// refusal — a chat type has no sensible default, but a granted role does.
func roleFromProto(r messagingv1.MemberRole) domain.Role {
	switch r {
	case messagingv1.MemberRole_MEMBER_ROLE_OWNER:
		return domain.RoleOwner
	case messagingv1.MemberRole_MEMBER_ROLE_ADMIN:
		return domain.RoleAdmin
	case messagingv1.MemberRole_MEMBER_ROLE_MEMBER, messagingv1.MemberRole_MEMBER_ROLE_UNSPECIFIED:
		return domain.RoleMember
	default:
		return domain.RoleMember
	}
}

// mutedUntilToProto renders a mute for the wire, omitting the timestamp for an
// indefinite mute (ADR-006 §4.9/§4.10: "absent when muted indefinitely").
// domain.IndefiniteMuteUntil is the sentinel the store round-trips for that
// case; rendering it as a literal timestamp would show a real, if absurd,
// expiry a year-9999 away instead of the absence the API contract specifies.
func mutedUntilToProto(until *time.Time) *messagingv1.Timestamp {
	if until == nil || until.Equal(domain.IndefiniteMuteUntil) {
		return nil
	}
	return timeToProtoTimestamp(*until)
}

func roleToProto(r domain.Role) messagingv1.MemberRole {
	switch r {
	case domain.RoleOwner:
		return messagingv1.MemberRole_MEMBER_ROLE_OWNER
	case domain.RoleAdmin:
		return messagingv1.MemberRole_MEMBER_ROLE_ADMIN
	case domain.RoleMember:
		return messagingv1.MemberRole_MEMBER_ROLE_MEMBER
	default:
		return messagingv1.MemberRole_MEMBER_ROLE_UNSPECIFIED
	}
}

func chatToProto(c app.ChatRecord) *messagingv1.Chat {
	return &messagingv1.Chat{
		ChatId:      c.ChatID,
		ChatType:    chatTypeToProto(c.ChatType),
		Name:        c.Name,
		MemberCount: clampInt32(c.MemberCount),
		CreatedAt:   timeToProtoTimestamp(c.CreatedAt),
		CreatedBy:   c.CreatedBy,
		UpdatedAt:   timeToProtoTimestamp(c.UpdatedAt),

		// LastSequence is served from the Postgres write path and stays zero
		// until M2.2 (documented on the proto field).
	}
}

func memberToProto(m app.MemberRecord) *messagingv1.ChatMember {
	member := &messagingv1.ChatMember{
		UserId:      m.UserID,
		ChatId:      m.ChatID,
		Role:        roleToProto(m.Role),
		JoinedAt:    timeToProtoTimestamp(m.JoinedAt),
		DisplayName: m.DisplayName,
	}
	member.MutedUntil = mutedUntilToProto(m.MutedUntil)
	return member
}

func membersToProto(members []app.MemberRecord) []*messagingv1.ChatMember {
	out := make([]*messagingv1.ChatMember, 0, len(members))
	for _, m := range members {
		out = append(out, memberToProto(m))
	}
	return out
}
