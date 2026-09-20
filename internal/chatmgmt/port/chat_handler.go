package port

import (
	"context"
	"fmt"

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
}

// ChatHandler implements the chat half of ChatMgmtServiceServer.
//
// It is not registered directly. AuthenticatedChatServer wraps it, so by the
// time a method here runs the caller has been authenticated and injected into
// the context; every method reads that caller rather than trusting anything in
// the request body.
//
// **The RPCs it does not implement answer Unimplemented, by design.** The proto
// declares all ten of ADR-006 §4's endpoints, and this PR ships three. The
// remainder come with their own service methods and tests rather than as empty
// handlers returning empty responses, which would look implemented from the
// outside (execution-plan Principle 3).
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
	if m.MutedUntil != nil {
		member.MutedUntil = timeToProtoTimestamp(*m.MutedUntil)
	}
	return member
}

func membersToProto(members []app.MemberRecord) []*messagingv1.ChatMember {
	out := make([]*messagingv1.ChatMember, 0, len(members))
	for _, m := range members {
		out = append(out, memberToProto(m))
	}
	return out
}
