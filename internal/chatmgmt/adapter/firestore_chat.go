package adapter

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// Compile-time checks: the adapter satisfies the ports the service defines.
var (
	_ app.ChatWriter = (*ChatWriter)(nil)
	_ app.ChatReader = (*ChatReader)(nil)
)

// ChatWriter performs chat writes through the store's transactions
// (ADR-016, ADR-023).
//
// Every method here is a thin translation: parse the service's string IDs into
// domain value objects, call one transaction, map the result back. The
// invariants live in internal/firestore, deliberately — this layer must not
// grow a second, weaker copy of them.
type ChatWriter struct {
	tx *firestore.ChatTx
}

// NewChatWriter creates a ChatWriter over the store's transaction writer.
func NewChatWriter(tx *firestore.ChatTx) *ChatWriter {
	return &ChatWriter{tx: tx}
}

// CreateDirect creates or returns the pair's direct chat, reporting whether it
// already existed (ADR-006 §4.1).
func (w *ChatWriter) CreateDirect(
	ctx context.Context, callerID, otherID string, now time.Time,
) (app.ChatRecord, bool, error) {
	ctx, span := tracer.Start(ctx, "firestore.chat.create_direct")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.String("db.collection", firestore.CollectionChats),
	)

	caller, err := domain.NewUserID(callerID)
	if err != nil {
		return app.ChatRecord{}, false, recordErr(span, fmt.Errorf("caller ID: %w", err))
	}
	other, err := domain.NewUserID(otherID)
	if err != nil {
		return app.ChatRecord{}, false, recordErr(span, fmt.Errorf("member ID: %w", err))
	}

	result, err := w.tx.CreateDirect(ctx, firestore.DirectChatParams{
		// A fresh ID per attempt; it is discarded if the pair already has a
		// chat, which is why generating it here costs nothing.
		ChatID: domain.GenerateChatID(),
		UserA:  caller,
		UserB:  other,
		Now:    now,
	})
	if err != nil {
		return app.ChatRecord{}, false, recordErr(span, err)
	}

	span.SetAttributes(attribute.Bool("chat.existing", result.Existing))
	return toChatRecord(result.Chat), result.Existing, nil
}

// CreateGroup creates a group chat with all of its memberships (ADR-006 §4.1).
func (w *ChatWriter) CreateGroup(
	ctx context.Context, ownerID, name string, memberIDs []string, now time.Time,
) (app.ChatRecord, error) {
	ctx, span := tracer.Start(ctx, "firestore.chat.create_group")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "firestore"),
		attribute.Int("chat.member_count", len(memberIDs)+1),
	)

	owner, err := domain.NewUserID(ownerID)
	if err != nil {
		return app.ChatRecord{}, recordErr(span, fmt.Errorf("owner ID: %w", err))
	}

	members := make([]domain.UserID, 0, len(memberIDs))
	for _, raw := range memberIDs {
		id, idErr := domain.NewUserID(raw)
		if idErr != nil {
			return app.ChatRecord{}, recordErr(span, fmt.Errorf("member ID: %w", idErr))
		}
		members = append(members, id)
	}

	chat, err := w.tx.CreateGroup(ctx, firestore.GroupChatParams{
		ChatID:    domain.GenerateChatID(),
		Name:      name,
		OwnerID:   owner,
		MemberIDs: members,
		Now:       now,
	})
	if err != nil {
		return app.ChatRecord{}, recordErr(span, err)
	}

	return toChatRecord(chat), nil
}

// ChatReader reads chats and memberships from Firestore (ADR-023).
type ChatReader struct {
	chats       *firestore.Chats
	memberships *firestore.Memberships
}

// NewChatReader creates a ChatReader over the two collections.
func NewChatReader(chats *firestore.Chats, memberships *firestore.Memberships) *ChatReader {
	return &ChatReader{chats: chats, memberships: memberships}
}

// GetChat returns the chat, or domain.ErrNotFound.
func (r *ChatReader) GetChat(ctx context.Context, chatID string) (app.ChatRecord, error) {
	ctx, span := tracer.Start(ctx, "firestore.chat.get")
	defer span.End()
	span.SetAttributes(attribute.String("db.collection", firestore.CollectionChats))

	id, err := domain.NewChatID(chatID)
	if err != nil {
		return app.ChatRecord{}, recordErr(span, fmt.Errorf("chat ID: %w", err))
	}

	doc, err := r.chats.Get(ctx, id)
	if err != nil {
		return app.ChatRecord{}, recordErr(span, err)
	}

	return toChatRecord(doc), nil
}

// GetMembership answers "is this user in this chat?" as a point lookup, or
// domain.ErrNotFound (ADR-023's strongly consistent membership check).
func (r *ChatReader) GetMembership(ctx context.Context, chatID, userID string) (app.MemberRecord, error) {
	ctx, span := tracer.Start(ctx, "firestore.membership.get")
	defer span.End()
	span.SetAttributes(attribute.String("db.collection", firestore.CollectionMemberships))

	chat, user, err := parseIDs(chatID, userID)
	if err != nil {
		return app.MemberRecord{}, recordErr(span, err)
	}

	doc, err := r.memberships.Get(ctx, chat, user)
	if err != nil {
		return app.MemberRecord{}, recordErr(span, err)
	}

	return toMemberRecord(doc), nil
}

// ListMembers returns the chat's members.
func (r *ChatReader) ListMembers(ctx context.Context, chatID string) ([]app.MemberRecord, error) {
	ctx, span := tracer.Start(ctx, "firestore.membership.list_by_chat")
	defer span.End()

	id, err := domain.NewChatID(chatID)
	if err != nil {
		return nil, recordErr(span, fmt.Errorf("chat ID: %w", err))
	}

	docs, err := r.memberships.ListByChat(ctx, id)
	if err != nil {
		return nil, recordErr(span, err)
	}

	return toMemberRecords(docs), nil
}

// ListUserChats returns the memberships a user holds — the reverse direction,
// served by the same collection off an automatic index (ADR-023).
func (r *ChatReader) ListUserChats(ctx context.Context, userID string) ([]app.MemberRecord, error) {
	ctx, span := tracer.Start(ctx, "firestore.membership.list_by_user")
	defer span.End()

	id, err := domain.NewUserID(userID)
	if err != nil {
		return nil, recordErr(span, fmt.Errorf("user ID: %w", err))
	}

	docs, err := r.memberships.ListByUser(ctx, id)
	if err != nil {
		return nil, recordErr(span, err)
	}

	return toMemberRecords(docs), nil
}

func parseIDs(chatID, userID string) (domain.ChatID, domain.UserID, error) {
	chat, err := domain.NewChatID(chatID)
	if err != nil {
		return domain.ChatID{}, domain.UserID{}, fmt.Errorf("chat ID: %w", err)
	}
	user, err := domain.NewUserID(userID)
	if err != nil {
		return domain.ChatID{}, domain.UserID{}, fmt.Errorf("user ID: %w", err)
	}
	return chat, user, nil
}

func toChatRecord(doc firestore.ChatDoc) app.ChatRecord {
	return app.ChatRecord{
		ChatID:      doc.ID,
		ChatType:    domain.ChatType(doc.ChatType),
		Name:        doc.Name,
		CreatedBy:   doc.CreatedBy,
		MemberCount: doc.MemberCount,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
}

func toMemberRecord(doc firestore.MembershipDoc) app.MemberRecord {
	return app.MemberRecord{
		ChatID:     doc.ChatID,
		UserID:     doc.UserID,
		Role:       domain.Role(doc.Role),
		JoinedAt:   doc.JoinedAt,
		MutedUntil: doc.MutedUntil,
	}
}

func toMemberRecords(docs []firestore.MembershipDoc) []app.MemberRecord {
	records := make([]app.MemberRecord, 0, len(docs))
	for _, doc := range docs {
		records = append(records, toMemberRecord(doc))
	}
	return records
}

// recordErr marks the span and returns the error unchanged, so every method
// above reports failures identically without four lines of ceremony each.
func recordErr(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}
