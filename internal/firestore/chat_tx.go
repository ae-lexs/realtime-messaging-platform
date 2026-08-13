package firestore

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// ChatTx performs the multi-document writes ADR-016 requires to be atomic:
// creating a chat with its memberships, and every mutation that changes
// membership and the count denormalized beside it.
//
// It enforces the chat's own invariants and nothing else. ADR-016 §5 asks for
// these at the data layer rather than only in the handler, because a new code
// path or an internal gRPC caller could bypass an API-level check:
//
//   - a direct chat's membership never changes after creation
//   - a group never exceeds domain.MaxGroupSize
//   - a group always has exactly one owner, who cannot leave or be removed
//   - member_count always equals the memberships written beside it
//
// **Authorization is not here.** Whether a caller may perform an operation —
// ADR-006 §8's role matrix — belongs to the service layer, which knows the
// caller. This type answers only whether the operation is coherent for the
// chat. The division matters: an owner removing a member and a stranger
// removing a member are the same write, and only one of them is allowed.
//
// Firestore has no per-write condition — the conditional-write form ADR-016
// specifies these invariants in — so each is asserted in Go against a
// document read *inside* the transaction. That is not weaker: the transaction
// fails and retries if any document it read changed before commit, which is
// what the condition defended against — and it is the guarantee the
// documentation actually makes, being a lock on a document the transaction
// read (ADR-023 v1.4).
type ChatTx struct{ client *Client }

// NewChatTx returns the chat transaction writer.
func NewChatTx(client *Client) *ChatTx { return &ChatTx{client: client} }

// DirectChatParams is the write set for creating a direct chat.
type DirectChatParams struct {
	// ChatID is the ID to create the chat under if this caller wins. It is
	// generated before the transaction so a loser can be told what it would
	// have created, and discarded if the pair already has a chat.
	ChatID domain.ChatID

	// UserA and UserB are the two participants in either order; the canonical
	// pair key is derived, never supplied.
	UserA domain.UserID
	UserB domain.UserID

	Now time.Time
}

// DirectChatResult reports what CreateDirect did.
type DirectChatResult struct {
	// Chat is the pair's chat, whether this call created it or found it.
	Chat ChatDoc

	// Existing is true when the pair already had a chat, which ADR-006 §4.1
	// answers with 200 OK and `X-Idempotent-Replay: true` rather than an
	// error. A caller may create a direct chat without checking first; that is
	// the point of the contract.
	Existing bool
}

// CreateDirect returns the pair's direct chat, creating it if it does not
// exist (ADR-006 §4.1, ADR-016 §2.2, ADR-023 v1.4).
//
// One transaction reads `direct_chats/{min}__{max}` and, if absent, creates
// the sentinel, the chat and both memberships — four documents, all or
// nothing. The read is the fast path and the common one; the Create is what
// enforces uniqueness where the store's own range protection is absent or
// unspecified.
//
// A caller that loses the race completes normally rather than failing: it
// re-reads the sentinel, finds the winner's chat, and returns it. That is the
// opposite of the registration race, whose loser holds an OTP the winner
// already consumed and has nothing left to spend (ADR-015 §10.2 as corrected
// in v1.4).
func (t *ChatTx) CreateDirect(ctx context.Context, params DirectChatParams) (DirectChatResult, error) {
	if params.UserA == params.UserB {
		// A chat with oneself has no second participant to write, and the
		// canonical pair would name one document twice.
		return DirectChatResult{}, fmt.Errorf(
			"firestore: direct chat needs two distinct users: %w", domain.ErrInvalidInput)
	}

	ctx, cancel := t.client.withTxTimeout(ctx)
	defer cancel()

	pairID := DirectChatDocID(params.UserA, params.UserB)
	sentinelRef := t.client.FS.Collection(CollectionDirectChats).Doc(pairID)

	var result DirectChatResult
	err := t.client.runTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		// Reset per attempt: the SDK re-invokes this callback on every retry,
		// and a result left over from an aborted attempt would be returned as
		// though it had committed.
		result = DirectChatResult{}

		existing, found, readErr := t.readDirectPair(tx, sentinelRef, pairID)
		if readErr != nil {
			return readErr
		}
		if found {
			result = DirectChatResult{Chat: existing, Existing: true}
			return nil
		}

		// Firestore requires every read to precede every write, so all writes
		// happen from here.
		created, writeErr := t.writeDirectPair(tx, sentinelRef, pairID, params)
		if writeErr != nil {
			return writeErr
		}

		result = DirectChatResult{Chat: created, Existing: false}
		return nil
	})
	if status.Code(err) == codes.AlreadyExists {
		// The sentinel was taken between this transaction's read and its
		// commit. Nothing was written, and the caller retries into the fast
		// path above.
		return DirectChatResult{}, fmt.Errorf(
			"firestore: direct pair %s already claimed: %w", pairID, domain.ErrAlreadyExists)
	}
	if err != nil {
		return DirectChatResult{}, err
	}

	return result, nil
}

// readDirectPair looks for an existing chat for the pair, returning it and
// true when the sentinel names one.
//
// The chat is read inside the same transaction as the sentinel. Reading it
// afterwards would be a second, unsynchronized round trip that could observe a
// chat the sentinel had not yet been checked against.
func (t *ChatTx) readDirectPair(
	tx *firestore.Transaction,
	sentinelRef *firestore.DocumentRef,
	pairID string,
) (ChatDoc, bool, error) {
	snapshot, err := tx.Get(sentinelRef)
	if status.Code(err) == codes.NotFound {
		return ChatDoc{}, false, nil
	}
	if err != nil {
		return ChatDoc{}, false, fmt.Errorf("firestore: read direct chat %s: %w", pairID, err)
	}

	var sentinel DirectChatDoc
	if decodeErr := snapshot.DataTo(&sentinel); decodeErr != nil {
		return ChatDoc{}, false, fmt.Errorf("firestore: decode direct chat %s: %w", pairID, decodeErr)
	}

	chatID, idErr := domain.NewChatID(sentinel.ChatID)
	if idErr != nil {
		return ChatDoc{}, false, fmt.Errorf(
			"firestore: direct chat %s names an invalid chat: %w", pairID, idErr)
	}

	chat, chatErr := t.getChatIn(tx, chatID)
	if chatErr != nil {
		return ChatDoc{}, false, chatErr
	}

	return chat, true, nil
}

// writeDirectPair creates the sentinel, the chat and both memberships.
func (t *ChatTx) writeDirectPair(
	tx *firestore.Transaction,
	sentinelRef *firestore.DocumentRef,
	pairID string,
	params DirectChatParams,
) (ChatDoc, error) {
	chat := ChatDoc{
		ID:       params.ChatID.String(),
		ChatType: string(domain.ChatTypeDirect),

		// CreatedBy records who opened the conversation and confers nothing:
		// a direct chat has no owner, and both participants are members
		// (ADR-006 §4.1).
		CreatedBy:   params.UserA.String(),
		MemberCount: 2,
		CreatedAt:   params.Now,
		UpdatedAt:   params.Now,
	}
	if err := chat.Validate(); err != nil {
		return ChatDoc{}, err
	}

	sentinel := DirectChatDoc{ID: pairID, ChatID: chat.ID, CreatedAt: params.Now}
	if err := sentinel.Validate(); err != nil {
		return ChatDoc{}, err
	}

	if err := tx.Create(sentinelRef, sentinel); err != nil {
		return ChatDoc{}, fmt.Errorf("firestore: claim direct pair %s: %w", pairID, err)
	}
	if err := tx.Create(t.chatRef(params.ChatID), chat); err != nil {
		return ChatDoc{}, fmt.Errorf("firestore: create chat %s: %w", chat.ID, err)
	}
	for _, userID := range []domain.UserID{params.UserA, params.UserB} {
		if err := t.createMembership(tx, params.ChatID, userID, domain.RoleMember, params.Now); err != nil {
			return ChatDoc{}, err
		}
	}

	return chat, nil
}

// GroupChatParams is the write set for creating a group chat.
type GroupChatParams struct {
	ChatID  domain.ChatID
	Name    string
	OwnerID domain.UserID

	// MemberIDs excludes the owner, who is added as RoleOwner (ADR-006 §4.1).
	MemberIDs []domain.UserID

	Now time.Time
}

// CreateGroup creates a group chat and all of its memberships in one
// transaction (ADR-006 §4.1, ADR-023 v1.4).
//
// ADR-016 §3.1 split this into two phases to fit a prior substrate's 100-item
// transaction limit, which a full group exceeded. Firestore publishes no
// per-transaction document count — the binding limits are the request size and
// the transaction's time budget, and a maximum group is 101 small documents —
// so the chat and every membership commit together. The partial-membership
// window §3.2 documented, and the reconciler §3.3 specified to repair it, do
// not exist here (ADR-023 v1.4).
//
// No sentinel: two groups with identical membership are legitimately different
// chats (ADR-006 §4.2), so nothing deduplicates them.
func (t *ChatTx) CreateGroup(ctx context.Context, params GroupChatParams) (ChatDoc, error) {
	members, err := dedupeExcluding(params.MemberIDs, params.OwnerID)
	if err != nil {
		return ChatDoc{}, err
	}
	if len(members) == 0 {
		return ChatDoc{}, fmt.Errorf(
			"firestore: a group needs at least one member besides its owner: %w", domain.ErrInvalidInput)
	}

	// The owner counts toward the maximum (ADR-006 §4.1: max 100 including
	// the creator), so the cap is checked against the total.
	total := len(members) + 1
	if total > domain.MaxGroupSize {
		return ChatDoc{}, fmt.Errorf(
			"firestore: %d members exceeds the %d limit: %w", total, domain.MaxGroupSize, domain.ErrChatFull)
	}

	ctx, cancel := t.client.withTxTimeout(ctx)
	defer cancel()

	chat := ChatDoc{
		ID:          params.ChatID.String(),
		ChatType:    string(domain.ChatTypeGroup),
		Name:        params.Name,
		CreatedBy:   params.OwnerID.String(),
		MemberCount: total,
		CreatedAt:   params.Now,
		UpdatedAt:   params.Now,
	}
	if validateErr := chat.Validate(); validateErr != nil {
		return ChatDoc{}, validateErr
	}

	err = t.client.runTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		if createErr := tx.Create(t.chatRef(params.ChatID), chat); createErr != nil {
			return fmt.Errorf("firestore: create chat %s: %w", chat.ID, createErr)
		}
		if ownerErr := t.createMembership(
			tx, params.ChatID, params.OwnerID, domain.RoleOwner, params.Now,
		); ownerErr != nil {
			return ownerErr
		}
		for _, userID := range members {
			if memberErr := t.createMembership(
				tx, params.ChatID, userID, domain.RoleMember, params.Now,
			); memberErr != nil {
				return memberErr
			}
		}
		return nil
	})
	if err != nil {
		return ChatDoc{}, err
	}

	return chat, nil
}

// AddMember adds a user to a group chat, maintaining member_count in the same
// transaction (ADR-006 §4.5, ADR-016 §4.1).
//
// The cap is the reason this is a transaction rather than a write. Two
// concurrent adds against a chat with 99 members would both read 99 and both
// commit, producing 101. Reading `chats/{id}` inside the transaction makes
// that document the serialization point — a lock on a document the transaction
// read, which is the guarantee Firestore documents (ADR-023 v1.4).
func (t *ChatTx) AddMember(
	ctx context.Context,
	chatID domain.ChatID,
	userID domain.UserID,
	role domain.Role,
	now time.Time,
) (MembershipDoc, error) {
	if !domain.IsAssignableRole(role) {
		return MembershipDoc{}, fmt.Errorf(
			"firestore: role %q cannot be granted: %w", role, domain.ErrInvalidOperation)
	}

	ctx, cancel := t.client.withTxTimeout(ctx)
	defer cancel()

	membership := MembershipDoc{
		ChatID:   chatID.String(),
		UserID:   userID.String(),
		Role:     string(role),
		JoinedAt: now,
	}
	membershipRef, err := t.membershipRef(membership)
	if err != nil {
		return MembershipDoc{}, err
	}

	err = t.client.runTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		chat, chatErr := t.getChatIn(tx, chatID)
		if chatErr != nil {
			return chatErr
		}
		if chat.IsDirect() {
			return fmt.Errorf(
				"firestore: chat %s is direct and its membership is fixed: %w",
				chatID.String(), domain.ErrInvalidOperation)
		}
		if chat.MemberCount >= domain.MaxGroupSize {
			return fmt.Errorf(
				"firestore: chat %s has %d of %d members: %w",
				chatID.String(), chat.MemberCount, domain.MaxGroupSize, domain.ErrChatFull)
		}

		// Read before write, and it doubles as the already-a-member check that
		// ADR-006 §4.5 answers with 409.
		if _, getErr := tx.Get(membershipRef); getErr == nil {
			return fmt.Errorf(
				"firestore: user %s is already in chat %s: %w",
				userID.String(), chatID.String(), domain.ErrAlreadyMember)
		} else if status.Code(getErr) != codes.NotFound {
			return fmt.Errorf("firestore: read membership: %w", getErr)
		}

		if createErr := tx.Create(membershipRef, membership); createErr != nil {
			return fmt.Errorf("firestore: create membership: %w", createErr)
		}
		return t.setCount(tx, chatID, chat.MemberCount+1, now)
	})
	if err != nil {
		return MembershipDoc{}, err
	}

	return membership, nil
}

// RemoveMember removes a user from a group chat (ADR-006 §4.6, ADR-016 §4.2).
//
// The owner cannot be removed, which is half of what keeps ADR-016's
// owner_always_exists invariant true; Leave is the other half. Neither store
// can express "at least one member holds this role" as a constraint, so the
// invariant is upheld by refusing the operations that would break it.
func (t *ChatTx) RemoveMember(
	ctx context.Context,
	chatID domain.ChatID,
	userID domain.UserID,
	now time.Time,
) error {
	return t.removeMembership(ctx, chatID, userID, now, "remove")
}

// Leave removes the calling user from a group chat (ADR-006 §4.8).
//
// The owner cannot leave, because ownership transfer is out of scope for the
// MVP and a group with no owner has no one who can administer it. A direct
// chat cannot be left at all — muting is the equivalent, and it is the one
// membership-adjacent operation direct chats allow, because it changes the
// member's own state rather than the chat's.
func (t *ChatTx) Leave(
	ctx context.Context,
	chatID domain.ChatID,
	userID domain.UserID,
	now time.Time,
) error {
	return t.removeMembership(ctx, chatID, userID, now, "leave")
}

func (t *ChatTx) removeMembership(
	ctx context.Context,
	chatID domain.ChatID,
	userID domain.UserID,
	now time.Time,
	op string,
) error {
	ctx, cancel := t.client.withTxTimeout(ctx)
	defer cancel()

	membershipRef := t.client.FS.
		Collection(CollectionMemberships).
		Doc(MembershipDocID(chatID, userID))

	return t.client.runTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		chat, chatErr := t.getChatIn(tx, chatID)
		if chatErr != nil {
			return chatErr
		}
		if chat.IsDirect() {
			return fmt.Errorf(
				"firestore: cannot %s a direct chat %s: %w", op, chatID.String(), domain.ErrInvalidOperation)
		}

		snapshot, getErr := tx.Get(membershipRef)
		if status.Code(getErr) == codes.NotFound {
			return fmt.Errorf(
				"firestore: user %s is not in chat %s: %w",
				userID.String(), chatID.String(), domain.ErrNotMember)
		}
		if getErr != nil {
			return fmt.Errorf("firestore: read membership: %w", getErr)
		}

		membership, decodeErr := membershipFrom(snapshot)
		if decodeErr != nil {
			return decodeErr
		}
		if membership.Role == string(domain.RoleOwner) {
			return fmt.Errorf(
				"firestore: the owner cannot %s chat %s: %w", op, chatID.String(), domain.ErrInvalidOperation)
		}

		if deleteErr := tx.Delete(membershipRef); deleteErr != nil {
			return fmt.Errorf("firestore: delete membership: %w", deleteErr)
		}
		return t.setCount(tx, chatID, chat.MemberCount-1, now)
	})
}

// SetRole changes a member's role (ADR-006 §4.7, ADR-016 §4.4).
//
// A single document changes, but it is still a transaction: the assertions are
// about the document being written, and reading it outside would leave a
// window in which the member was removed, or became the owner, between the
// check and the write.
func (t *ChatTx) SetRole(
	ctx context.Context,
	chatID domain.ChatID,
	userID domain.UserID,
	role domain.Role,
	now time.Time,
) (MembershipDoc, error) {
	if !domain.IsAssignableRole(role) {
		return MembershipDoc{}, fmt.Errorf(
			"firestore: role %q cannot be assigned: %w", role, domain.ErrInvalidOperation)
	}

	ctx, cancel := t.client.withTxTimeout(ctx)
	defer cancel()

	membershipRef := t.client.FS.
		Collection(CollectionMemberships).
		Doc(MembershipDocID(chatID, userID))

	var updated MembershipDoc
	err := t.client.runTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		updated = MembershipDoc{}

		chat, chatErr := t.getChatIn(tx, chatID)
		if chatErr != nil {
			return chatErr
		}
		if chat.IsDirect() {
			return fmt.Errorf(
				"firestore: direct chat %s has no roles to change: %w",
				chatID.String(), domain.ErrInvalidOperation)
		}

		snapshot, getErr := tx.Get(membershipRef)
		if status.Code(getErr) == codes.NotFound {
			return fmt.Errorf(
				"firestore: user %s is not in chat %s: %w",
				userID.String(), chatID.String(), domain.ErrNotMember)
		}
		if getErr != nil {
			return fmt.Errorf("firestore: read membership: %w", getErr)
		}

		current, decodeErr := membershipFrom(snapshot)
		if decodeErr != nil {
			return decodeErr
		}
		if current.Role == string(domain.RoleOwner) {
			// Demoting the owner would leave the group without one, which
			// nothing can repair while ownership transfer does not exist.
			return fmt.Errorf(
				"firestore: the owner's role cannot change in chat %s: %w",
				chatID.String(), domain.ErrInvalidOperation)
		}

		if updateErr := tx.Update(membershipRef, []firestore.Update{
			{Path: "role", Value: string(role)},
		}); updateErr != nil {
			return fmt.Errorf("firestore: update role: %w", updateErr)
		}

		current.Role = string(role)
		updated = current
		return nil
	})
	if err != nil {
		return MembershipDoc{}, err
	}

	return updated, nil
}

// SetMute sets or clears a member's mute (ADR-006 §4.9, §4.10).
//
// A nil until clears it. This is the one membership write allowed on a direct
// chat, because it changes the member's own notification state rather than the
// chat's membership — ADR-016 §5's table has it as the sole "allowed" row for
// direct chats.
//
// A plain update rather than a transaction: it touches one document, one
// field, on behalf of the one user it belongs to, and there is no invariant
// spanning it and anything else.
func (t *ChatTx) SetMute(
	ctx context.Context,
	chatID domain.ChatID,
	userID domain.UserID,
	until *time.Time,
) error {
	ctx, cancel := t.client.withTimeout(ctx)
	defer cancel()

	docID := MembershipDocID(chatID, userID)

	_, err := t.client.FS.Collection(CollectionMemberships).Doc(docID).Update(ctx, []firestore.Update{
		{Path: "muted_until", Value: until},
	})
	if status.Code(err) == codes.NotFound {
		return fmt.Errorf(
			"firestore: user %s is not in chat %s: %w",
			userID.String(), chatID.String(), domain.ErrNotMember)
	}
	if err != nil {
		return fmt.Errorf("firestore: set mute on %s: %w", docID, err)
	}
	return nil
}

// SetName renames a group chat (ADR-006 §4.4).
//
// Direct chats cannot be renamed: they have no name of their own, being
// displayed as the other participant.
func (t *ChatTx) SetName(ctx context.Context, chatID domain.ChatID, name string, now time.Time) (ChatDoc, error) {
	ctx, cancel := t.client.withTxTimeout(ctx)
	defer cancel()

	var updated ChatDoc
	err := t.client.runTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		updated = ChatDoc{}

		chat, chatErr := t.getChatIn(tx, chatID)
		if chatErr != nil {
			return chatErr
		}
		if chat.IsDirect() {
			return fmt.Errorf(
				"firestore: direct chat %s cannot be renamed: %w", chatID.String(), domain.ErrInvalidOperation)
		}

		if updateErr := tx.Update(t.chatRef(chatID), []firestore.Update{
			{Path: "name", Value: name},
			{Path: "updated_at", Value: now},
		}); updateErr != nil {
			return fmt.Errorf("firestore: rename chat %s: %w", chatID.String(), updateErr)
		}

		chat.Name = name
		chat.UpdatedAt = now
		updated = chat
		return nil
	})
	if err != nil {
		return ChatDoc{}, err
	}

	return updated, nil
}

// getChatIn reads a chat inside a transaction, mapping absence to
// domain.ErrNotFound.
func (t *ChatTx) getChatIn(tx *firestore.Transaction, chatID domain.ChatID) (ChatDoc, error) {
	snapshot, err := tx.Get(t.chatRef(chatID))
	if status.Code(err) == codes.NotFound {
		return ChatDoc{}, fmt.Errorf("firestore: chat %s: %w", chatID.String(), domain.ErrNotFound)
	}
	if err != nil {
		return ChatDoc{}, fmt.Errorf("firestore: read chat %s: %w", chatID.String(), err)
	}

	var doc ChatDoc
	if decodeErr := snapshot.DataTo(&doc); decodeErr != nil {
		return ChatDoc{}, fmt.Errorf("firestore: decode chat %s: %w", chatID.String(), decodeErr)
	}
	doc.ID = snapshot.Ref.ID

	return doc, nil
}

// setCount writes member_count as an absolute value read in this transaction,
// not as an increment.
//
// firestore.Increment would be wrong here even though it is atomic: the count
// has to agree with a membership write that is only valid because of what this
// transaction read, and an increment applied on top of someone else's
// concurrent change would preserve the arithmetic while breaking the cap the
// read was checked against.
func (t *ChatTx) setCount(tx *firestore.Transaction, chatID domain.ChatID, count int, now time.Time) error {
	if err := tx.Update(t.chatRef(chatID), []firestore.Update{
		{Path: "member_count", Value: count},
		{Path: "updated_at", Value: now},
	}); err != nil {
		return fmt.Errorf("firestore: update member count on %s: %w", chatID.String(), err)
	}
	return nil
}

func (t *ChatTx) createMembership(
	tx *firestore.Transaction,
	chatID domain.ChatID,
	userID domain.UserID,
	role domain.Role,
	now time.Time,
) error {
	doc := MembershipDoc{
		ChatID:   chatID.String(),
		UserID:   userID.String(),
		Role:     string(role),
		JoinedAt: now,
	}

	ref, err := t.membershipRef(doc)
	if err != nil {
		return err
	}
	if createErr := tx.Create(ref, doc); createErr != nil {
		return fmt.Errorf("firestore: create membership for %s: %w", userID.String(), createErr)
	}
	return nil
}

func (t *ChatTx) membershipRef(doc MembershipDoc) (*firestore.DocumentRef, error) {
	docID, err := doc.DocID()
	if err != nil {
		return nil, err
	}
	return t.client.FS.Collection(CollectionMemberships).Doc(docID), nil
}

func (t *ChatTx) chatRef(chatID domain.ChatID) *firestore.DocumentRef {
	return t.client.FS.Collection(CollectionChats).Doc(chatID.String())
}

// dedupeExcluding returns ids without duplicates and without owner, preserving
// order.
//
// Both removals are ADR-006 §4.1's contract rather than tidiness: member_ids
// excludes the caller, who is added as owner, so an owner listed again would
// have their role silently overwritten by a second membership write at the
// same deterministic document ID. A repeated member would do the same and
// inflate member_count past the memberships that exist.
func dedupeExcluding(ids []domain.UserID, owner domain.UserID) ([]domain.UserID, error) {
	seen := make(map[domain.UserID]struct{}, len(ids))
	out := make([]domain.UserID, 0, len(ids))

	for _, id := range ids {
		if id == owner {
			return nil, fmt.Errorf(
				"firestore: member_ids must exclude the creator %s: %w", owner.String(), domain.ErrInvalidInput)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf(
				"firestore: member %s listed twice: %w", id.String(), domain.ErrInvalidInput)
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	return out, nil
}
