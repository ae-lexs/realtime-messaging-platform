package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/observability"
)

var (
	chatCreatedTotal        metric.Int64Counter
	chatDedupTotal          metric.Int64Counter
	chatAuthzDenialsTotal   metric.Int64Counter
	membershipMutationTotal metric.Int64Counter
)

func init() {
	m := otel.Meter("chatmgmt/app")

	chatCreatedTotal, _ = m.Int64Counter("chat_created_total",
		metric.WithDescription("Chats created, by type (ADR-016 §10)"))
	chatDedupTotal, _ = m.Int64Counter("direct_chat_dedup_total",
		metric.WithDescription("Direct-chat creation outcomes: created or existing (ADR-016 §10)"))
	chatAuthzDenialsTotal, _ = m.Int64Counter("chat_authz_denials_total",
		metric.WithDescription("Chat operations refused by the authorization matrix (ADR-016 §8)"))
	membershipMutationTotal, _ = m.Int64Counter("membership_mutation_total",
		metric.WithDescription("Membership changes, by change_type (ADR-016 §10)"))
}

// ChatRecord is the service's view of a chat. Adapters map the store's shape
// onto it, so nothing above this layer knows how a chat is persisted.
type ChatRecord struct {
	ChatID      string
	ChatType    domain.ChatType
	Name        string
	CreatedBy   string
	MemberCount int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// MemberRecord is the service's view of a membership.
type MemberRecord struct {
	ChatID   string
	UserID   string
	Role     domain.Role
	JoinedAt time.Time

	// DisplayName is denormalized for rendering (ADR-006 §4.3). It is filled by
	// the service from the user store, not held on the membership.
	DisplayName string

	// MutedUntil is nil when the member is not muted.
	MutedUntil *time.Time
}

// CreateChatParams is a request to create a chat.
type CreateChatParams struct {
	// CallerID is the authenticated user. It is never taken from the request
	// body: ADR-013's authority invariant puts the identity on the token, and a
	// caller-supplied creator field would let anyone create a chat as someone
	// else.
	CallerID string

	ChatType domain.ChatType
	Name     string

	// MemberIDs excludes the caller (ADR-006 §4.1).
	MemberIDs []string
}

// CreateChatResult is what CreateChat produced.
type CreateChatResult struct {
	Chat    ChatRecord
	Members []MemberRecord

	// Existing reports that a direct chat for this pair already existed and is
	// being returned rather than created — ADR-006 §4.1's 200-with-replay
	// contract, not an error.
	Existing bool
}

// ChatWriter is the transactional store the service writes chats through.
// Implemented by the adapter over internal/firestore's ChatTx.
type ChatWriter interface {
	CreateDirect(ctx context.Context, callerID, otherID string, now time.Time) (ChatRecord, bool, error)
	CreateGroup(ctx context.Context, ownerID, name string, memberIDs []string, now time.Time) (ChatRecord, error)
	AddMember(ctx context.Context, chatID, userID string, role domain.Role, now time.Time) (MemberRecord, error)
	RemoveMember(ctx context.Context, chatID, userID string, now time.Time) error
	Leave(ctx context.Context, chatID, userID string, now time.Time) error
	SetRole(ctx context.Context, chatID, userID string, role domain.Role, now time.Time) (MemberRecord, error)
	SetMute(ctx context.Context, chatID, userID string, until *time.Time) error
	SetName(ctx context.Context, chatID, name string, now time.Time) (ChatRecord, error)
}

// ChatReader reads chats and memberships.
type ChatReader interface {
	GetChat(ctx context.Context, chatID string) (ChatRecord, error)
	GetMembership(ctx context.Context, chatID, userID string) (MemberRecord, error)
	ListMembers(ctx context.Context, chatID string) ([]MemberRecord, error)
	ListUserChats(ctx context.Context, userID string) ([]MemberRecord, error)
}

// ChatServiceConfig is the dependency set for NewChatService.
type ChatServiceConfig struct {
	Writer ChatWriter
	Reader ChatReader

	// Users resolves display names. The same interface the auth service uses,
	// so there is one way to read a user in this process.
	Users UserStore

	Clock  domain.Clock
	Logger *slog.Logger
}

// ChatService orchestrates chat lifecycle operations (ADR-016).
//
// It owns exactly one thing the store below it cannot: **who may act**.
// internal/firestore enforces the chat's own invariants — a direct chat's
// membership never changes, a group never exceeds its cap, an owner cannot
// leave — because those hold no matter who asks. Authorization is the opposite
// kind of rule: an owner removing a member and a stranger removing a member
// are the same write, and only the caller's identity distinguishes them
// (ADR-016 §8).
//
// The division is not stylistic. Putting authorization in the store would mean
// threading a caller through every transaction and trusting each call site to
// pass the real one; putting invariants in the service would leave them
// bypassable by any future code path that writes directly.
type ChatService struct {
	writer ChatWriter
	reader ChatReader
	users  UserStore
	clock  domain.Clock
	logger *slog.Logger
}

// NewChatService builds the chat service.
func NewChatService(cfg ChatServiceConfig) *ChatService {
	return &ChatService{
		writer: cfg.Writer,
		reader: cfg.Reader,
		users:  cfg.Users,
		clock:  cfg.Clock,
		logger: cfg.Logger,
	}
}

// CreateChat creates a direct or group chat (ADR-006 §4.1).
//
// Any authenticated user may create either kind — ADR-016 §8's first two rows
// — so there is no role check here. What there is instead is validation the
// store cannot do, because it concerns the request rather than the data: the
// member list must be the right size and must not contain the caller.
func (s *ChatService) CreateChat(ctx context.Context, params CreateChatParams) (*CreateChatResult, error) {
	ctx, span := tracer.Start(ctx, "chat.create")
	defer span.End()
	span.SetAttributes(attribute.String("chat.type", string(params.ChatType)))

	logger := observability.WithTraceID(ctx, s.logger)

	if err := validateCreate(params); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	var (
		result CreateChatResult
		err    error
	)
	switch params.ChatType {
	case domain.ChatTypeDirect:
		result, err = s.createDirect(ctx, params)
	case domain.ChatTypeGroup:
		result, err = s.createGroup(ctx, params)
	default:
		// Unreachable: validateCreate rejects anything else. Present so a new
		// chat type cannot be added to the domain and silently fall through.
		err = fmt.Errorf("unsupported chat type %q: %w", params.ChatType, domain.ErrInvalidInput)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	members, err := s.membersWithNames(ctx, result.Chat.ChatID)
	if err != nil {
		return nil, err
	}
	result.Members = members

	logger.InfoContext(ctx, "chat.create",
		"chat_id", result.Chat.ChatID,
		"chat_type", string(result.Chat.ChatType),
		"caller_id", params.CallerID,
		"existing", result.Existing,
	)

	return &result, nil
}

func (s *ChatService) createDirect(ctx context.Context, params CreateChatParams) (CreateChatResult, error) {
	chat, existing, err := s.writer.CreateDirect(ctx, params.CallerID, params.MemberIDs[0], s.clock.Now())
	if err != nil {
		return CreateChatResult{}, err
	}

	outcome := "created"
	if existing {
		outcome = "existing"
	}
	chatDedupTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("result", outcome)))
	if !existing {
		chatCreatedTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("chat_type", "direct")))
	}

	return CreateChatResult{Chat: chat, Existing: existing}, nil
}

func (s *ChatService) createGroup(ctx context.Context, params CreateChatParams) (CreateChatResult, error) {
	chat, err := s.writer.CreateGroup(ctx, params.CallerID, params.Name, params.MemberIDs, s.clock.Now())
	if err != nil {
		return CreateChatResult{}, err
	}

	chatCreatedTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("chat_type", "group")))

	return CreateChatResult{Chat: chat}, nil
}

// GetChat returns a chat with its members (ADR-006 §4.3).
//
// Membership is the authorization: a non-member gets 403 NOT_A_MEMBER, not
// 404, because the chat's existence is not a secret from someone who was told
// its ID — but its contents are. The distinction matters the other way too: a
// chat that does not exist must not answer 403, or the API becomes an oracle
// for which chat IDs are real.
func (s *ChatService) GetChat(ctx context.Context, callerID, chatID string) (*ChatRecord, []MemberRecord, error) {
	ctx, span := tracer.Start(ctx, "chat.get")
	defer span.End()
	span.SetAttributes(attribute.String("chat.id", chatID))

	chat, err := s.reader.GetChat(ctx, chatID)
	if err != nil {
		span.RecordError(err)
		return nil, nil, err
	}

	if _, memberErr := s.requireMembership(ctx, chatID, callerID, "get_chat"); memberErr != nil {
		span.RecordError(memberErr)
		return nil, nil, memberErr
	}

	members, err := s.membersWithNames(ctx, chatID)
	if err != nil {
		return nil, nil, err
	}

	return &chat, members, nil
}

// ListChats returns the chats the caller belongs to (ADR-006 §4.2).
//
// Driven from the caller's own memberships, so it needs no authorization check
// of its own — a user can only ever reach chats they are a member of, and the
// query is keyed by their ID.
func (s *ChatService) ListChats(ctx context.Context, callerID string) ([]ChatRecord, error) {
	ctx, span := tracer.Start(ctx, "chat.list")
	defer span.End()

	memberships, err := s.reader.ListUserChats(ctx, callerID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list chats: %w", err)
	}

	chats := make([]ChatRecord, 0, len(memberships))
	for _, m := range memberships {
		chat, err := s.reader.GetChat(ctx, m.ChatID)
		if err != nil {
			if domain.IsNotFound(err) {
				// A membership pointing at a chat that no longer exists is
				// cross-store drift, not a reason to fail the whole listing
				// (ADR-023: these references are application-enforced). Skip it
				// and record it, because it should not happen.
				s.logger.WarnContext(ctx, "membership references a missing chat",
					"chat_id", m.ChatID, "user_id", callerID)
				continue
			}
			return nil, err
		}
		chats = append(chats, chat)
	}

	span.SetAttributes(attribute.Int("chat.count", len(chats)))
	return chats, nil
}

// requireMembership fetches the caller's membership or refuses.
//
// Returns domain.ErrNotMember, which ADR-006 maps to 403 NOT_A_MEMBER — never
// ErrNotFound, which would tell the caller the chat is absent when it is
// merely none of their business.
func (s *ChatService) requireMembership(
	ctx context.Context, chatID, userID, operation string,
) (MemberRecord, error) {
	membership, err := s.reader.GetMembership(ctx, chatID, userID)
	if err != nil {
		if domain.IsNotFound(err) {
			chatAuthzDenialsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", operation),
				attribute.String("reason", "not_a_member"),
			))
			return MemberRecord{}, fmt.Errorf(
				"user %s in chat %s: %w", userID, chatID, domain.ErrNotMember)
		}
		return MemberRecord{}, err
	}
	return membership, nil
}

// requireRole confirms the caller is a member holding one of the allowed
// roles (ADR-016 §8). Membership is checked first, via requireMembership, so
// a non-member sees ErrNotMember rather than ErrForbidden — the two answer
// different questions ("are you in this chat" vs "are you allowed to do
// this"), and collapsing them would tell a non-member which was closer.
func (s *ChatService) requireRole(
	ctx context.Context, chatID, userID, operation string, allowed ...domain.Role,
) error {
	membership, err := s.requireMembership(ctx, chatID, userID, operation)
	if err != nil {
		return err
	}
	for _, r := range allowed {
		if membership.Role == r {
			return nil
		}
	}
	chatAuthzDenialsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.String("reason", "insufficient_role"),
	))
	return fmt.Errorf(
		"user %s holds role %q in chat %s, insufficient for %s: %w",
		userID, membership.Role, chatID, operation, domain.ErrForbidden)
}

// UpdateChat renames a group chat (ADR-006 §4.4). Owner and admin only; a
// direct chat is refused by the store, not here — that invariant holds no
// matter who asks, which is exactly the line ChatService's own doc comment
// draws between it and authorization.
func (s *ChatService) UpdateChat(ctx context.Context, callerID, chatID, name string) (*ChatRecord, error) {
	ctx, span := tracer.Start(ctx, "chat.update")
	defer span.End()
	span.SetAttributes(attribute.String("chat.id", chatID))

	if name == "" || len(name) > maxChatNameLength {
		err := fmt.Errorf("a chat name must be 1-%d characters: %w", maxChatNameLength, domain.ErrInvalidInput)
		span.RecordError(err)
		return nil, err
	}

	if err := s.requireRole(ctx, chatID, callerID, "update_chat", domain.RoleOwner, domain.RoleAdmin); err != nil {
		span.RecordError(err)
		return nil, err
	}

	chat, err := s.writer.SetName(ctx, chatID, name, s.clock.Now())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	return &chat, nil
}

// AddMember adds a user to a group chat (ADR-006 §4.5).
//
// The role being granted is itself an authorization input, which is why the
// check depends on it: an owner may add a member as ADMIN, but an admin may
// only add them as MEMBER (ADR-016 §8) — granting a role the caller could not
// assign directly through UpdateMemberRole would be a second, weaker path to
// the same privilege.
func (s *ChatService) AddMember(
	ctx context.Context, callerID, chatID, userID string, role domain.Role,
) (*MemberRecord, error) {
	ctx, span := tracer.Start(ctx, "chat.add_member")
	defer span.End()
	span.SetAttributes(attribute.String("chat.id", chatID), attribute.String("member.role", string(role)))

	if role == domain.Role("") {
		role = domain.RoleMember
	}
	if !domain.IsAssignableRole(role) {
		err := fmt.Errorf("role %q cannot be granted: %w", role, domain.ErrInvalidInput)
		span.RecordError(err)
		return nil, err
	}

	caller, err := s.requireMembership(ctx, chatID, callerID, "add_member")
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	allowed := caller.Role == domain.RoleOwner || (caller.Role == domain.RoleAdmin && role == domain.RoleMember)
	if !allowed {
		chatAuthzDenialsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "add_member"), attribute.String("reason", "insufficient_role")))
		denyErr := fmt.Errorf("user %s holds role %q, cannot add a member as %q: %w",
			callerID, caller.Role, role, domain.ErrForbidden)
		span.RecordError(denyErr)
		return nil, denyErr
	}

	member, err := s.writer.AddMember(ctx, chatID, userID, role, s.clock.Now())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	membershipMutationTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("change_type", "added")))
	return &member, nil
}

// RemoveMember removes a user from a group chat (ADR-006 §4.6).
//
// Self-removal is refused here, not left to the store: "remove yourself" and
// "leave" are the same write with a different name, and ADR-006 keeps them as
// two endpoints with two authorization stories rather than one endpoint with
// a branch — LeaveChat is where the owner-cannot-leave refusal belongs.
func (s *ChatService) RemoveMember(ctx context.Context, callerID, chatID, userID string) error {
	ctx, span := tracer.Start(ctx, "chat.remove_member")
	defer span.End()
	span.SetAttributes(attribute.String("chat.id", chatID), attribute.String("member.user_id", userID))

	if userID == callerID {
		err := fmt.Errorf("cannot remove yourself, use leave instead: %w", domain.ErrInvalidOperation)
		span.RecordError(err)
		return err
	}

	caller, err := s.requireMembership(ctx, chatID, callerID, "remove_member")
	if err != nil {
		span.RecordError(err)
		return err
	}

	allowed := caller.Role == domain.RoleOwner
	if caller.Role == domain.RoleAdmin {
		// An admin may remove a plain member only, never another admin or the
		// owner — checked against the target's actual role, since "can I
		// remove this person" depends on who they are, not just who is asking.
		// A target that is not a member at all is left for the store's own
		// removal to refuse identically; this lookup exists only to see a
		// role, never to pre-empt that error.
		target, tErr := s.reader.GetMembership(ctx, chatID, userID)
		allowed = tErr == nil && target.Role == domain.RoleMember
	}
	if !allowed {
		chatAuthzDenialsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "remove_member"), attribute.String("reason", "insufficient_role")))
		err := fmt.Errorf("user %s holds role %q, cannot remove this member: %w",
			callerID, caller.Role, domain.ErrForbidden)
		span.RecordError(err)
		return err
	}

	if err := s.writer.RemoveMember(ctx, chatID, userID, s.clock.Now()); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	membershipMutationTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("change_type", "removed")))
	return nil
}

// UpdateMemberRole changes a member's role (ADR-006 §4.7). Owner only; cannot
// change the caller's own role. The store separately refuses assigning or
// demoting the owner role (ADR-016 §4.4 — ownership transfer is out of scope
// for the MVP), which is why that case is not re-checked here.
func (s *ChatService) UpdateMemberRole(
	ctx context.Context, callerID, chatID, userID string, role domain.Role,
) (*MemberRecord, error) {
	ctx, span := tracer.Start(ctx, "chat.update_member_role")
	defer span.End()
	span.SetAttributes(attribute.String("chat.id", chatID), attribute.String("member.user_id", userID))

	if userID == callerID {
		err := fmt.Errorf("cannot change your own role: %w", domain.ErrInvalidOperation)
		span.RecordError(err)
		return nil, err
	}
	if !domain.IsAssignableRole(role) {
		err := fmt.Errorf("role %q cannot be assigned: %w", role, domain.ErrInvalidInput)
		span.RecordError(err)
		return nil, err
	}

	if err := s.requireRole(ctx, chatID, callerID, "update_member_role", domain.RoleOwner); err != nil {
		span.RecordError(err)
		return nil, err
	}

	member, err := s.writer.SetRole(ctx, chatID, userID, role, s.clock.Now())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	membershipMutationTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("change_type", "role_changed")))
	return &member, nil
}

// LeaveChat removes the caller from a group chat (ADR-006 §4.8).
//
// No role check beyond membership: the store refuses the owner's departure
// and any attempt on a direct chat (ADR-016 §4.3), which is every
// restriction ADR-016 §8's Leave row states — owner excepted, everyone who is
// a member may leave.
func (s *ChatService) LeaveChat(ctx context.Context, callerID, chatID string) error {
	ctx, span := tracer.Start(ctx, "chat.leave")
	defer span.End()
	span.SetAttributes(attribute.String("chat.id", chatID))

	if _, err := s.requireMembership(ctx, chatID, callerID, "leave"); err != nil {
		span.RecordError(err)
		return err
	}

	if err := s.writer.Leave(ctx, chatID, callerID, s.clock.Now()); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	membershipMutationTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("change_type", "left")))
	return nil
}

// MuteChat silences the caller's notifications for a chat (ADR-006 §4.9).
//
// The one membership write direct chats allow (ADR-016 §5): it changes the
// caller's own notification state, not the chat itself, so it needs no role
// beyond membership. A nil durationHours means indefinite — represented by
// domain.IndefiniteMuteUntil, since the store's nil already means "not
// muted" and cannot also stand for "muted forever".
func (s *ChatService) MuteChat(ctx context.Context, callerID, chatID string, durationHours *int32) (time.Time, error) {
	ctx, span := tracer.Start(ctx, "chat.mute")
	defer span.End()
	span.SetAttributes(attribute.String("chat.id", chatID))

	if durationHours != nil && (*durationHours <= 0 || *durationHours > domain.MaxMuteDurationHours) {
		err := fmt.Errorf("duration_hours must be 1-%d, got %d: %w",
			domain.MaxMuteDurationHours, *durationHours, domain.ErrInvalidInput)
		span.RecordError(err)
		return time.Time{}, err
	}

	if _, err := s.requireMembership(ctx, chatID, callerID, "mute"); err != nil {
		span.RecordError(err)
		return time.Time{}, err
	}

	until := domain.IndefiniteMuteUntil
	if durationHours != nil {
		until = s.clock.Now().Add(time.Duration(*durationHours) * time.Hour)
	}

	if err := s.writer.SetMute(ctx, chatID, callerID, &until); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return time.Time{}, err
	}

	membershipMutationTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("change_type", "muted")))
	return until, nil
}

// UnmuteChat clears the caller's mute (ADR-006 §4.10).
func (s *ChatService) UnmuteChat(ctx context.Context, callerID, chatID string) error {
	ctx, span := tracer.Start(ctx, "chat.unmute")
	defer span.End()
	span.SetAttributes(attribute.String("chat.id", chatID))

	if _, err := s.requireMembership(ctx, chatID, callerID, "unmute"); err != nil {
		span.RecordError(err)
		return err
	}

	if err := s.writer.SetMute(ctx, chatID, callerID, nil); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	membershipMutationTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("change_type", "unmuted")))
	return nil
}

// membersWithNames lists a chat's members with their display names filled in.
//
// One read per member, which is the cost of denormalizing a name that lives on
// the user rather than the membership. Bounded by the group cap, so the worst
// case is 100 point reads on a single chat detail request; if that ever
// matters, the answer is to denormalize display_name onto the membership at
// write time, not to drop the field.
func (s *ChatService) membersWithNames(ctx context.Context, chatID string) ([]MemberRecord, error) {
	members, err := s.reader.ListMembers(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	for i := range members {
		user, err := s.users.GetByID(ctx, members[i].UserID)
		if err != nil {
			if domain.IsNotFound(err) {
				// Same cross-store drift as above: a membership naming a user
				// that is gone. The membership is still real, so it is
				// returned without a name rather than dropped.
				continue
			}
			return nil, fmt.Errorf("resolve member name: %w", err)
		}
		members[i].DisplayName = user.DisplayName
	}

	return members, nil
}

// validateCreate applies ADR-006 §4.1's request rules.
//
// These are about the request, not the stored data, which is why they live
// here and not in the store: the store cannot know that member_ids was meant
// to exclude its caller.
func validateCreate(params CreateChatParams) error {
	if params.CallerID == "" {
		return fmt.Errorf("caller is required: %w", domain.ErrUnauthorized)
	}
	if !domain.IsValidChatType(params.ChatType) {
		return fmt.Errorf("chat type %q: %w", params.ChatType, domain.ErrInvalidInput)
	}

	for _, id := range params.MemberIDs {
		if id == params.CallerID {
			return fmt.Errorf(
				"member_ids must exclude the caller, who is added automatically: %w", domain.ErrInvalidInput)
		}
	}

	switch params.ChatType {
	case domain.ChatTypeDirect:
		if len(params.MemberIDs) != 1 {
			return fmt.Errorf(
				"a direct chat needs exactly one other member, got %d: %w",
				len(params.MemberIDs), domain.ErrInvalidInput)
		}
	case domain.ChatTypeGroup:
		if params.Name == "" || len(params.Name) > maxChatNameLength {
			return fmt.Errorf(
				"a group needs a name of 1-%d characters: %w", maxChatNameLength, domain.ErrInvalidInput)
		}
		if len(params.MemberIDs) == 0 || len(params.MemberIDs) > domain.MaxGroupSize-1 {
			return fmt.Errorf(
				"a group needs 1-%d members besides its creator, got %d: %w",
				domain.MaxGroupSize-1, len(params.MemberIDs), domain.ErrInvalidInput)
		}
	}

	return nil
}

// maxChatNameLength is ADR-006 §4.1's validation rule for a group name.
const maxChatNameLength = 128
