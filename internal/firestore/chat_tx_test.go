package firestore_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

func TestDirectChatDocIDIsCanonical(t *testing.T) {
	// Arrange
	a, b := domain.GenerateUserID(), domain.GenerateUserID()

	// Act
	forward := firestore.DirectChatDocID(a, b)
	reversed := firestore.DirectChatDocID(b, a)

	// Assert — the whole point of the canonical pair: a caller never has to
	// know which order it asked in, so {A,B} and {B,A} address one document
	// (ADR-006 §4.1).
	assert.Equal(t, forward, reversed)
	assert.Contains(t, forward, "__")
	assert.LessOrEqual(t, len(forward), firestore.MaxDocIDBytes)
}

func TestDirectChatDocIDIsSortedLexicographically(t *testing.T) {
	// Arrange — fixed IDs, so the assertion is about ordering rather than
	// about whichever pair the generator produced.
	lo := domain.MustUserID("00000000-0000-4000-8000-000000000001")
	hi := domain.MustUserID("ffffffff-0000-4000-8000-00000000000f")

	// Act
	id := firestore.DirectChatDocID(hi, lo)

	// Assert
	assert.Equal(t, lo.String()+"__"+hi.String(), id)
}

func TestDirectChatDocValidate(t *testing.T) {
	tests := []struct {
		name    string
		doc     firestore.DirectChatDoc
		wantErr bool
	}{
		{
			name: "a sentinel naming a chat is writable",
			doc:  firestore.DirectChatDoc{ID: "a__b", ChatID: "chat-1"},
		},
		{
			name:    "without an ID it addresses no pair",
			doc:     firestore.DirectChatDoc{ChatID: "chat-1"},
			wantErr: true,
		},
		{
			// The pair would be claimed for nothing: no chat could ever be
			// created for them, and a race loser would read a document naming
			// no winner.
			name:    "without a chat ID it claims the pair for nobody",
			doc:     firestore.DirectChatDoc{ID: "a__b"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			err := tt.doc.Validate()

			// Assert
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestChatDocIsDirect(t *testing.T) {
	// Arrange
	direct := firestore.ChatDoc{ChatType: string(domain.ChatTypeDirect)}
	group := firestore.ChatDoc{ChatType: string(domain.ChatTypeGroup)}

	// Assert
	assert.True(t, direct.IsDirect())
	assert.False(t, group.IsDirect())

	// A chat whose type never got written must not read as a group, because
	// every membership mutation is gated on "not direct" and would then be
	// permitted on a document nothing can vouch for.
	assert.False(t, firestore.ChatDoc{}.IsDirect())
}

func TestDedupeExcluding(t *testing.T) {
	owner := domain.GenerateUserID()
	first, second := domain.GenerateUserID(), domain.GenerateUserID()

	tests := []struct {
		name    string
		ids     []domain.UserID
		wantLen int
		wantErr error
	}{
		{
			name:    "distinct members pass through in order",
			ids:     []domain.UserID{first, second},
			wantLen: 2,
		},
		{
			// ADR-006 §4.1: member_ids excludes the creator, who is added as
			// owner. Listed again, the second write would land on the same
			// deterministic membership ID and silently demote them.
			name:    "the owner listed again is refused",
			ids:     []domain.UserID{first, owner},
			wantErr: domain.ErrInvalidInput,
		},
		{
			// A repeat would inflate member_count past the memberships that
			// actually exist, and the count is the group-size cap's input.
			name:    "a member listed twice is refused",
			ids:     []domain.UserID{first, first},
			wantErr: domain.ErrInvalidInput,
		},
		{
			name:    "an empty list is not itself an error here",
			ids:     nil,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got, err := firestore.DedupeExcluding(tt.ids, owner)

			// Assert
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

func TestIsOptionsMismatch(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "the rejected retry is matched",
			err: status.Error(codes.InvalidArgument,
				"Transaction options should be the same as specified previous transaction"),
			want: true,
		},
		{
			// The status code alone cannot be the signal: a malformed write
			// returns InvalidArgument too, and retrying it would turn one
			// permanent client error into three.
			name: "another InvalidArgument is not",
			err:  status.Error(codes.InvalidArgument, "invalid document reference"),
		},
		{
			name: "an aborted transaction is the SDK's to retry, not ours",
			err:  status.Error(codes.Aborted, "too much contention"),
		},
		{
			name: "a wrapped domain error is not a transport rejection",
			err:  fmt.Errorf("firestore: read chat: %w", domain.ErrNotFound),
		},
		{
			name: "success is not a mismatch",
			err:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act + Assert
			assert.Equal(t, tt.want, firestore.IsOptionsMismatch(tt.err))
		})
	}
}

func TestIsOptionsMismatchSeesThroughWrapping(t *testing.T) {
	// Arrange — the transaction body wraps what it returns, and the SDK
	// returns the callback's error unchanged, so the condition has to survive
	// %w wrapping or the retry never fires where it matters.
	wrapped := fmt.Errorf("firestore: create chat: %w",
		status.Error(codes.InvalidArgument,
			"Transaction options should be the same as specified previous transaction"))

	// Assert
	assert.True(t, firestore.IsOptionsMismatch(wrapped))
	assert.True(t, errors.Is(wrapped, wrapped))
}

func TestMuteWindowIsRepresentable(t *testing.T) {
	// Arrange — ADR-006 §4.9 caps a mute at 8760 hours (one year) and allows
	// an absent duration to mean indefinite. The document field is a pointer
	// for exactly that reason, so both states round-trip.
	until := time.Now().UTC().Add(8760 * time.Hour)

	// Act
	muted := firestore.MembershipDoc{MutedUntil: &until}
	cleared := firestore.MembershipDoc{}

	// Assert
	require.NotNil(t, muted.MutedUntil)
	assert.Equal(t, until, *muted.MutedUntil)
	assert.Nil(t, cleared.MutedUntil, "an unmuted member must be nil, not the zero time")
}
