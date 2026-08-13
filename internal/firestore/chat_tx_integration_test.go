//go:build integration

// The M1.3 store gate: chat creation and membership mutation against a live
// Firestore database.
//
// These are gates, not measurements — every outcome here is specified by
// ADR-006 §4 and ADR-016, so each test asserts it. That is the opposite of
// directpair_integration_test.go next door, which measures a quantity nobody
// knew and therefore asserts only that its harness was valid. Both belong in
// the same package and neither should be mistaken for the other.
//
// The concurrency test is the one that carries a debt from RTM-04 C7: it
// records WHICH mechanism refused each loser, because the shipped registration
// gate proved uniqueness for five months while its sentinel never fired once,
// and nothing noticed because the assertion accepted any refusal.

package firestore_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// chatFixture is the pieces a chat test needs: the writer, the stores it
// verifies through, and a clock value that does not drift between assertions.
type chatFixture struct {
	tx          *firestore.ChatTx
	chats       *firestore.Chats
	memberships *firestore.Memberships
	now         time.Time
}

func newChatFixture(t *testing.T) chatFixture {
	t.Helper()

	client := liveClient(t)
	return chatFixture{
		tx:          firestore.NewChatTx(client),
		chats:       firestore.NewChats(client),
		memberships: firestore.NewMemberships(client),
		now:         time.Now().UTC().Truncate(time.Millisecond),
	}
}

// newGroup creates a group with the given number of extra members and returns
// the chat plus its owner.
func (f chatFixture) newGroup(ctx context.Context, t *testing.T, extra int) (domain.ChatID, domain.UserID) {
	t.Helper()

	owner := domain.GenerateUserID()
	members := make([]domain.UserID, extra)
	for i := range members {
		members[i] = domain.GenerateUserID()
	}

	chatID := domain.GenerateChatID()
	_, err := f.tx.CreateGroup(ctx, firestore.GroupChatParams{
		ChatID:    chatID,
		Name:      "gate group",
		OwnerID:   owner,
		MemberIDs: members,
		Now:       f.now,
	})
	require.NoError(t, err)

	return chatID, owner
}

func TestCreateDirectChatWritesFourDocuments(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	userA, userB := domain.GenerateUserID(), domain.GenerateUserID()

	// Act
	result, err := f.tx.CreateDirect(ctx, firestore.DirectChatParams{
		ChatID: domain.GenerateChatID(),
		UserA:  userA,
		UserB:  userB,
		Now:    f.now,
	})

	// Assert
	require.NoError(t, err)
	assert.False(t, result.Existing, "a pair with no chat must report a creation")
	assert.Equal(t, string(domain.ChatTypeDirect), result.Chat.ChatType)
	assert.Equal(t, 2, result.Chat.MemberCount)

	chatID := domain.MustChatID(result.Chat.ID)
	for _, userID := range []domain.UserID{userA, userB} {
		membership, memberErr := f.memberships.Get(ctx, chatID, userID)
		require.NoError(t, memberErr, "both participants must be members")

		// A direct chat has no owner: neither participant can administer the
		// other, which is what makes its membership immutable (ADR-006 §4.1).
		assert.Equal(t, string(domain.RoleMember), membership.Role)
	}
}

func TestCreateDirectChatIsIdempotentInEitherOrder(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	userA, userB := domain.GenerateUserID(), domain.GenerateUserID()

	first, err := f.tx.CreateDirect(ctx, firestore.DirectChatParams{
		ChatID: domain.GenerateChatID(),
		UserA:  userA,
		UserB:  userB,
		Now:    f.now,
	})
	require.NoError(t, err)

	// Act — ask again with the arguments the other way round and a different
	// candidate chat ID, which is what a second client would send.
	second, err := f.tx.CreateDirect(ctx, firestore.DirectChatParams{
		ChatID: domain.GenerateChatID(),
		UserA:  userB,
		UserB:  userA,
		Now:    f.now,
	})

	// Assert — ADR-006 §4.1: an existing direct chat is returned rather than
	// refused, so a client may create one without checking first.
	require.NoError(t, err)
	assert.True(t, second.Existing, "the second call must report a replay, not a creation")
	assert.Equal(t, first.Chat.ID, second.Chat.ID,
		"argument order must not decide which chat a pair gets")
}

func TestCreateDirectChatRefusesAPairWithItself(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	user := domain.GenerateUserID()

	// Act
	_, err := f.tx.CreateDirect(ctx, firestore.DirectChatParams{
		ChatID: domain.GenerateChatID(),
		UserA:  user,
		UserB:  user,
		Now:    f.now,
	})

	// Assert — the canonical pair would name one document twice, and the chat
	// would have one membership while claiming two.
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestCreateGroupIsOneTransaction(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)

	// Act — a full group, which ADR-016 §3.1 could not create atomically under
	// the prior substrate's 100-item transaction limit and which Firestore
	// commits in one go (ADR-023 v1.4).
	chatID, owner := f.newGroup(ctx, t, domain.MaxGroupSize-1)

	// Assert
	chat, err := f.chats.Get(ctx, chatID)
	require.NoError(t, err)
	assert.Equal(t, domain.MaxGroupSize, chat.MemberCount)

	members, err := f.memberships.ListByChat(ctx, chatID)
	require.NoError(t, err)
	assert.Len(t, members, domain.MaxGroupSize,
		"every membership commits with the chat: there is no phase two to lag behind it")

	ownerDoc, err := f.memberships.Get(ctx, chatID, owner)
	require.NoError(t, err)
	assert.Equal(t, string(domain.RoleOwner), ownerDoc.Role)
}

func TestCreateGroupRefusesMoreThanTheMaximum(t *testing.T) {
	// Arrange — the owner counts toward the limit (ADR-006 §4.1: max 100
	// including the creator), so MaxGroupSize others is one too many.
	ctx := context.Background()
	f := newChatFixture(t)

	members := make([]domain.UserID, domain.MaxGroupSize)
	for i := range members {
		members[i] = domain.GenerateUserID()
	}

	// Act
	_, err := f.tx.CreateGroup(ctx, firestore.GroupChatParams{
		ChatID:    domain.GenerateChatID(),
		Name:      "one too many",
		OwnerID:   domain.GenerateUserID(),
		MemberIDs: members,
		Now:       f.now,
	})

	// Assert
	require.ErrorIs(t, err, domain.ErrChatFull)
}

func TestAddMemberMaintainsTheCountAtomically(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	chatID, _ := f.newGroup(ctx, t, 1)
	newcomer := domain.GenerateUserID()

	// Act
	membership, err := f.tx.AddMember(ctx, chatID, newcomer, domain.RoleAdmin, f.now)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, string(domain.RoleAdmin), membership.Role)

	chat, err := f.chats.Get(ctx, chatID)
	require.NoError(t, err)
	assert.Equal(t, 3, chat.MemberCount, "the count and the membership commit together")

	members, err := f.memberships.ListByChat(ctx, chatID)
	require.NoError(t, err)
	assert.Len(t, members, chat.MemberCount,
		"member_count must equal the memberships written beside it")
}

func TestAddMemberRefusesAFullChat(t *testing.T) {
	// Arrange — a chat exactly at the cap.
	ctx := context.Background()
	f := newChatFixture(t)
	chatID, _ := f.newGroup(ctx, t, domain.MaxGroupSize-1)

	// Act
	_, err := f.tx.AddMember(ctx, chatID, domain.GenerateUserID(), domain.RoleMember, f.now)

	// Assert — the cap is read inside the transaction, which is what makes it
	// hold under concurrent adds rather than only sequential ones.
	require.ErrorIs(t, err, domain.ErrChatFull)

	chat, err := f.chats.Get(ctx, chatID)
	require.NoError(t, err)
	assert.Equal(t, domain.MaxGroupSize, chat.MemberCount, "a refused add must not move the count")
}

func TestAddMemberRefusesADuplicate(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	chatID, owner := f.newGroup(ctx, t, 1)

	// Act
	_, err := f.tx.AddMember(ctx, chatID, owner, domain.RoleMember, f.now)

	// Assert — ADR-006 §4.5 answers 409. Without the check the write would
	// succeed at the same deterministic document ID and demote the owner.
	require.ErrorIs(t, err, domain.ErrAlreadyMember)

	ownerDoc, err := f.memberships.Get(ctx, chatID, owner)
	require.NoError(t, err)
	assert.Equal(t, string(domain.RoleOwner), ownerDoc.Role, "the refused add must not have overwritten a role")
}

func TestDirectChatMembershipIsImmutable(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	userA, userB := domain.GenerateUserID(), domain.GenerateUserID()

	created, err := f.tx.CreateDirect(ctx, firestore.DirectChatParams{
		ChatID: domain.GenerateChatID(),
		UserA:  userA,
		UserB:  userB,
		Now:    f.now,
	})
	require.NoError(t, err)
	chatID := domain.MustChatID(created.Chat.ID)

	// Act + Assert — ADR-016 §5's table, enforced at the data layer rather
	// than only in the handler, so a new code path or an internal caller
	// cannot bypass it.
	t.Run("a member cannot be added", func(t *testing.T) {
		_, addErr := f.tx.AddMember(ctx, chatID, domain.GenerateUserID(), domain.RoleMember, f.now)
		require.ErrorIs(t, addErr, domain.ErrInvalidOperation)
	})

	t.Run("a participant cannot be removed", func(t *testing.T) {
		require.ErrorIs(t, f.tx.RemoveMember(ctx, chatID, userA, f.now), domain.ErrInvalidOperation)
	})

	t.Run("a participant cannot leave", func(t *testing.T) {
		require.ErrorIs(t, f.tx.Leave(ctx, chatID, userB, f.now), domain.ErrInvalidOperation)
	})

	t.Run("it cannot be renamed", func(t *testing.T) {
		_, nameErr := f.tx.SetName(ctx, chatID, "not allowed", f.now)
		require.ErrorIs(t, nameErr, domain.ErrInvalidOperation)
	})

	t.Run("but it can be muted", func(t *testing.T) {
		// The one allowed row in §5's table: muting changes the member's own
		// notification state, not the chat's membership.
		until := f.now.Add(24 * time.Hour)
		require.NoError(t, f.tx.SetMute(ctx, chatID, userA, &until))

		membership, getErr := f.memberships.Get(ctx, chatID, userA)
		require.NoError(t, getErr)
		require.NotNil(t, membership.MutedUntil)
	})
}

func TestTheOwnerCannotLeaveOrBeRemoved(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	chatID, owner := f.newGroup(ctx, t, 1)

	// Act + Assert — ADR-016's owner_always_exists invariant is procedural:
	// no store can express "at least one member holds this role", so it is
	// upheld by refusing the operations that would break it.
	t.Run("the owner cannot leave", func(t *testing.T) {
		require.ErrorIs(t, f.tx.Leave(ctx, chatID, owner, f.now), domain.ErrInvalidOperation)
	})

	t.Run("the owner cannot be removed", func(t *testing.T) {
		require.ErrorIs(t, f.tx.RemoveMember(ctx, chatID, owner, f.now), domain.ErrInvalidOperation)
	})

	t.Run("the owner's role cannot be changed", func(t *testing.T) {
		_, err := f.tx.SetRole(ctx, chatID, owner, domain.RoleMember, f.now)
		require.ErrorIs(t, err, domain.ErrInvalidOperation)
	})

	t.Run("and the owner is still there", func(t *testing.T) {
		membership, err := f.memberships.Get(ctx, chatID, owner)
		require.NoError(t, err)
		assert.Equal(t, string(domain.RoleOwner), membership.Role)
	})
}

func TestLeaveRemovesTheMemberAndDecrementsTheCount(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	chatID, _ := f.newGroup(ctx, t, 1)

	members, err := f.memberships.ListByChat(ctx, chatID)
	require.NoError(t, err)

	var leaver domain.UserID
	for _, m := range members {
		if m.Role != string(domain.RoleOwner) {
			leaver = domain.MustUserID(m.UserID)
			break
		}
	}
	require.NotEmpty(t, leaver.String())

	// Act
	require.NoError(t, f.tx.Leave(ctx, chatID, leaver, f.now))

	// Assert
	_, err = f.memberships.Get(ctx, chatID, leaver)
	require.ErrorIs(t, err, domain.ErrNotFound)

	chat, err := f.chats.Get(ctx, chatID)
	require.NoError(t, err)
	assert.Equal(t, 1, chat.MemberCount)
}

func TestRemoveMemberRefusesANonMember(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	chatID, _ := f.newGroup(ctx, t, 1)

	// Act
	err := f.tx.RemoveMember(ctx, chatID, domain.GenerateUserID(), f.now)

	// Assert — a delete of a document that does not exist is not an error in
	// Firestore, so without the read this would silently decrement the count
	// for a membership that was never there.
	require.ErrorIs(t, err, domain.ErrNotMember)

	chat, err := f.chats.Get(ctx, chatID)
	require.NoError(t, err)
	assert.Equal(t, 2, chat.MemberCount, "a refused removal must not move the count")
}

func TestSetRoleGrantsAdminButNeverOwner(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	chatID, owner := f.newGroup(ctx, t, 1)

	members, err := f.memberships.ListByChat(ctx, chatID)
	require.NoError(t, err)

	var target domain.UserID
	for _, m := range members {
		if m.UserID != owner.String() {
			target = domain.MustUserID(m.UserID)
			break
		}
	}
	require.NotEmpty(t, target.String())

	// Act + Assert
	t.Run("a member can be promoted to admin", func(t *testing.T) {
		updated, roleErr := f.tx.SetRole(ctx, chatID, target, domain.RoleAdmin, f.now)
		require.NoError(t, roleErr)
		assert.Equal(t, string(domain.RoleAdmin), updated.Role)
	})

	t.Run("but nobody can be made owner", func(t *testing.T) {
		// ADR-006 §4.7: ownership transfer has no implementation, and two
		// independent writes would leave a window with zero owners or two.
		_, roleErr := f.tx.SetRole(ctx, chatID, target, domain.RoleOwner, f.now)
		require.ErrorIs(t, roleErr, domain.ErrInvalidOperation)
	})

	t.Run("and a non-member has no role to change", func(t *testing.T) {
		_, roleErr := f.tx.SetRole(ctx, chatID, domain.GenerateUserID(), domain.RoleAdmin, f.now)
		require.ErrorIs(t, roleErr, domain.ErrNotMember)
	})
}

func TestSetMuteClearsWithNil(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	chatID, owner := f.newGroup(ctx, t, 1)

	until := f.now.Add(24 * time.Hour)
	require.NoError(t, f.tx.SetMute(ctx, chatID, owner, &until))

	// Act
	require.NoError(t, f.tx.SetMute(ctx, chatID, owner, nil))

	// Assert
	membership, err := f.memberships.Get(ctx, chatID, owner)
	require.NoError(t, err)
	assert.Nil(t, membership.MutedUntil, "unmuting must clear the field, not zero it")
}

func TestSetMuteRefusesANonMember(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	chatID, _ := f.newGroup(ctx, t, 1)
	until := f.now.Add(time.Hour)

	// Act
	err := f.tx.SetMute(ctx, chatID, domain.GenerateUserID(), &until)

	// Assert — Update on a missing document fails, which is what turns
	// "mute a chat you are not in" into a refusal rather than a stray write.
	require.ErrorIs(t, err, domain.ErrNotMember)
}

func TestSetNameRenamesAGroup(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	chatID, _ := f.newGroup(ctx, t, 1)
	renamedAt := f.now.Add(time.Minute)

	// Act
	updated, err := f.tx.SetName(ctx, chatID, "renamed", renamedAt)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "renamed", updated.Name)

	stored, err := f.chats.Get(ctx, chatID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", stored.Name)
	assert.True(t, stored.UpdatedAt.After(f.now), "a rename must move updated_at")
}

func TestMutationsOnAMissingChatAreNotFound(t *testing.T) {
	// Arrange — every mutation reads the chat first, so a chat that does not
	// exist must be reported as such rather than as a membership problem.
	ctx := context.Background()
	f := newChatFixture(t)
	missing := domain.GenerateChatID()

	// Act + Assert
	_, addErr := f.tx.AddMember(ctx, missing, domain.GenerateUserID(), domain.RoleMember, f.now)
	require.ErrorIs(t, addErr, domain.ErrNotFound)

	require.ErrorIs(t, f.tx.RemoveMember(ctx, missing, domain.GenerateUserID(), f.now), domain.ErrNotFound)

	_, nameErr := f.tx.SetName(ctx, missing, "nothing to rename", f.now)
	require.ErrorIs(t, nameErr, domain.ErrNotFound)
}

// TestConcurrentCreateDirectYieldsExactlyOneChat is M1.3's headline invariant,
// asserted through the shipped path — and it records which mechanism refused
// each loser rather than only that one chat survived.
//
// That second half is the debt RTM-04 C7 left. The registration gate asserted
// "exactly one user" for five months and accepted either refusal without
// recording which, so nobody noticed that the sentinel it was named for never
// fired once. A gate that cannot tell you which mechanism did the work will
// keep passing after that mechanism is removed.
//
// Three outcomes are legitimate here and all are recorded:
//
//	created   this racer wrote the sentinel, the chat and both memberships
//	replay    it read the sentinel and returned the winner's chat, which is
//	          ADR-006 §4.1's idempotent create and not an error
//	refused   its tx.Create on the sentinel lost, mapped to ErrAlreadyExists
//
// What is asserted is the invariant: one chat, and every caller that completed
// was told about the same one. If two clients each believed they had opened a
// conversation with the other, they would be writing into different chats with
// separate sequences and separate history — the failure this construction
// exists to prevent.
func TestConcurrentCreateDirectYieldsExactlyOneChat(t *testing.T) {
	// Arrange
	ctx := context.Background()
	f := newChatFixture(t)
	userA, userB := domain.GenerateUserID(), domain.GenerateUserID()

	type outcome struct {
		chatID  string
		replay  bool
		refused bool
		err     error
	}
	outcomes := make([]outcome, racers)

	// Act — one barrier, so the racers contend instead of queueing.
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := range racers {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()

			result, err := f.tx.CreateDirect(ctx, firestore.DirectChatParams{
				ChatID: domain.GenerateChatID(),
				UserA:  userA,
				UserB:  userB,
				Now:    f.now,
			})
			outcomes[i] = outcome{
				chatID:  result.Chat.ID,
				replay:  result.Existing,
				refused: errors.Is(err, domain.ErrAlreadyExists),
				err:     err,
			}
		}()
	}
	start.Done()
	done.Wait()

	// Measure — the attribution, reported whatever the distribution.
	lines := make([]string, 0, racers+3)
	var created, replayed, refused int
	for i, o := range outcomes {
		switch {
		case o.refused:
			refused++
		case o.err == nil && o.replay:
			replayed++
		case o.err == nil:
			created++
		}
		lines = append(lines, fmt.Sprintf(
			"racer %d: replay=%v refused=%v chat=%s err=%v", i, o.replay, o.refused, o.chatID, o.err))
	}
	lines = append(lines,
		fmt.Sprintf("created:  %d  <-- must be exactly 1", created),
		fmt.Sprintf("replayed: %d  <-- read the sentinel, returned the winner", replayed),
		fmt.Sprintf("refused:  %d  <-- tx.Create on the sentinel lost", refused))
	report(t, "M1.3 GATE — concurrent direct-chat creation", lines...)

	// Assert
	for i, o := range outcomes {
		if o.refused {
			continue
		}
		require.NoError(t, o.err, "racer %d failed for an unexpected reason", i)
		assert.NotEmpty(t, o.chatID, "racer %d returned no chat to its caller", i)
	}
	assert.Equal(t, 1, created, "exactly one racer may create the pair's chat")

	winner := ""
	for _, o := range outcomes {
		if o.err == nil && !o.replay {
			winner = o.chatID
		}
	}
	require.NotEmpty(t, winner)

	for i, o := range outcomes {
		if o.err != nil {
			continue
		}
		assert.Equal(t, winner, o.chatID,
			"racer %d was told about a different chat: two clients would write into separate histories", i)
	}

	// And the sentinel names the chat that exists, which is what a later
	// lookup for this pair will return.
	sentinel, err := f.chats.Get(ctx, domain.MustChatID(winner))
	require.NoError(t, err)
	assert.Equal(t, 2, sentinel.MemberCount)
}
