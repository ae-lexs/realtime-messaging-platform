//go:build integration

// The M1.3 negative control: does the direct-chat uniqueness argument survive
// on its own path?
//
// ADR-023 v1.3 justifies the `direct_chats/{min}__{max}` sentinel with
// phone_index's reasoning verbatim — "a Firestore transaction locks what it
// reads, a query matching nothing locks nothing" — and ADR-016's amendment
// banner repeats it independently. RTM-04's negative control measured that
// premise false on 2026-08-07 (C6, C8): a transaction whose only read was a
// query matching zero documents was aborted by a concurrent insert, in both
// concurrency modes.
//
// That result was measured on the registration path, which has a confound:
// AuthTx.run reads and writes `otp_requests/{phone_hash}` on every path, so
// every racer for one phone number contends on a shared document as well as on
// the empty range, and C7 found the OTP document refusing all twenty losers
// while the sentinel refused none. Direct-chat creation has no such document —
// the chat ID and both membership IDs are fresh per racer, so the only thing
// two racers for one pair share is the absence they each observed. This is the
// cleaner instance of the same question, and it is the path the ledger's C3
// covers, which has no measurement of its own.
//
// These are measurements, not gates. Outcomes are pre-registered per arm and
// both directions are informative, so arms A and B assert only that the
// harness was valid and report the quantity under test to the captured log. An
// assertion on the measured direction would delete the finding it exists to
// record.
//
// Three arms, one provisioning cycle:
//
//	A  TestConcurrentDirectChatsBehindAnEmptyQuery — the naive design, which is
//	   ADR-016 §2.1's rejected Option C: an indexed pair field on the chat
//	   document, queried inside the transaction. If the empty range is not
//	   lockable, this produces two chats for one conversation.
//
//	B  TestConcurrentDirectChatsWithNoQueryAtAll — arm A's positive control,
//	   the query deleted and nothing else changed. Without it arm A proves
//	   nothing: a single chat could mean the racers never overlapped.
//
//	C  TestConcurrentDirectChatsBehindTheSentinel — the design M1.3 will ship,
//	   run here to record *which mechanism* refuses each loser. C7 is the
//	   reason this arm exists: the shipped auth gate proved uniqueness for five
//	   months without recording that its sentinel never fired once.
//
// Arm C is written inline rather than against internal/firestore's production
// helpers because those do not exist yet — M1.3's store layer is the PR after
// this one, and the ADR it will cite is not amended until this run says what
// it should say.

package firestore_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	gcfs "cloud.google.com/go/firestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// collectionDirectChats is the sentinel collection ADR-023 v1.3 specifies. It
// is a literal here and a named constant in the store layer from the next PR;
// this file is deliberately not the place that defines the production schema.
const collectionDirectChats = "direct_chats"

// fieldDirectPair is the indexed pair key of arm A's naive chat document —
// ADR-016 §2.1's Option C, materialised only to be measured. It is not part of
// the ADR-023 schema and must never appear in the store layer.
const fieldDirectPair = "direct_pair"

// roleMember is the role both participants of a direct chat hold, since a
// direct chat has no owner (ADR-006 §4.1). It is a literal here because the
// role constants belong to the store layer M1.3 has not written yet.
const roleMember = "member"

// naiveChatDoc is `chats` as the rejected design would have written it: the
// canonical pair key stored on the chat itself, so uniqueness could be checked
// with a query instead of a sentinel document.
//
// Storing it is also why the design was rejected on grounds independent of
// concurrency — the pair key is the membership, and it would travel wherever a
// chat document goes.
type naiveChatDoc struct {
	ChatType   string    `firestore:"chat_type"`
	Name       string    `firestore:"name"`
	CreatedBy  string    `firestore:"created_by"`
	DirectPair string    `firestore:"direct_pair"`
	CreatedAt  time.Time `firestore:"created_at"`
	UpdatedAt  time.Time `firestore:"updated_at"`
}

// directChatSentinelDoc is `direct_chats/{min}__{max}` (ADR-023 v1.3): the
// document whose only job is to be contended for, carrying the winner's chat
// ID so the loser can return it.
type directChatSentinelDoc struct {
	ChatID    string    `firestore:"chat_id"`
	CreatedAt time.Time `firestore:"created_at"`
}

// directPair is one racing pair: two freshly generated user IDs and the
// canonical key derived from them.
type directPair struct {
	a, b domain.UserID
	key  string
}

// newDirectPair returns a pair no other test or repetition will use, so a
// query on its key matches only the writes of the run that made it.
//
// The key is the two IDs sorted lexicographically as strings and joined by
// `__` — ADR-023 v1.3's canonicalisation, which is what makes {A,B} and {B,A}
// address one document. UUIDs exclude both `_` and `#`, so the separator is
// unambiguous either way and consistency with `memberships` decides it.
func newDirectPair() directPair {
	a, b := domain.GenerateUserID(), domain.GenerateUserID()
	lo, hi := a.String(), b.String()
	if hi < lo {
		lo, hi = hi, lo
	}
	return directPair{a: a, b: b, key: lo + "__" + hi}
}

// racerOutcome is what one racer did, as observed from outside the store.
//
// Every arm records the same fields so the three are read side by side in one
// log, and so the counts below are computed once rather than per arm.
type racerOutcome struct {
	// attempts is how many times the SDK invoked the transaction body. More
	// than one means the store aborted this racer and made it start over,
	// which is the instrument: it is how a defended range announces itself.
	attempts int

	// fastPath records that the racer found the chat already there and
	// returned it instead of creating one — ADR-006 §4.1's idempotent create,
	// not a failure.
	fastPath bool

	// refused records an AlreadyExists from the sentinel's tx.Create, the
	// guarantee ADR-016 §2.1's conditional put was chosen for.
	refused bool

	// rejected records an InvalidArgument on a retried transaction:
	//
	//	Transaction options should be the same as specified previous transaction
	//
	// It is a property of how the SDK retries, not of this test. The first
	// BeginTransaction sends no options at all; every retry sends an explicit
	// ReadWrite carrying the previous transaction's ID (firestore@v1.24.0
	// transaction.go, the runTransaction loop). The backend intermittently
	// refuses that second shape — and because the SDK's retry loop is gated on
	// isAborted, an InvalidArgument is returned to the caller immediately
	// rather than attempted again.
	//
	// It occurs in BOTH concurrency modes and its incidence varies between
	// runs of identical code, so nothing here attributes it to a mode: the
	// first pessimistic run of this arm produced none, a later one produced
	// three. What is not established by these runs is the trigger — only that
	// it happens, that it is not confined to a mode, and that the SDK does not
	// retry it.
	//
	// Recorded rather than asserted away, because the consequence for M1.3 is
	// real: a create failing this way is application-retryable and nothing
	// beneath the service will retry it.
	rejected bool

	// chatID is what this racer would return to its caller. Every racer that
	// completed must name the same one, or two clients believe they are in
	// different conversations.
	chatID string

	err error
}

// classify records which of the two expected error shapes a racer ended with.
// Anything else stays an error the arm did not predict, and fails it.
func (o *racerOutcome) classify() {
	o.refused = status.Code(o.err) == codes.AlreadyExists
	o.rejected = status.Code(o.err) == codes.InvalidArgument
}

// tallies are the per-arm counts the log reports.
type tallies struct {
	retried  int
	fastPath int
	refused  int
	rejected int
}

func tally(outcomes []racerOutcome) tallies {
	var t tallies
	for _, o := range outcomes {
		if o.attempts > 1 {
			t.retried++
		}
		if o.fastPath {
			t.fastPath++
		}
		if o.refused {
			t.refused++
		}
		if o.rejected {
			t.rejected++
		}
	}
	return t
}

func outcomeLines(outcomes []racerOutcome) []string {
	lines := make([]string, 0, len(outcomes))
	for i, o := range outcomes {
		lines = append(lines, fmt.Sprintf(
			"racer %d: attempts=%d fast-path=%v refused=%v rejected=%v chat=%s err=%v",
			i, o.attempts, o.fastPath, o.refused, o.rejected, o.chatID, o.err))
	}
	return lines
}

// assertNoUnexpectedFailures checks the failure every arm shares: a racer that
// entered no transaction, or failed for a reason the arm did not predict.
//
// Two error shapes are predicted and therefore exempt — an AlreadyExists from
// the sentinel, which is the mechanism under study, and the InvalidArgument
// the SDK's retry produces under OPTIMISTIC, which is measured behaviour
// reported in the log (see racerOutcome.rejected). Every other error fails the
// arm.
func assertNoUnexpectedFailures(t *testing.T, outcomes []racerOutcome) {
	t.Helper()

	for i, o := range outcomes {
		assert.GreaterOrEqual(t, o.attempts, 1, "racer %d never entered a transaction", i)
		if !o.refused && !o.rejected {
			assert.NoError(t, o.err, "racer %d failed for an unexpected reason", i)
		}
	}
}

// runRacers releases n goroutines from one barrier and collects what each did.
//
// The barrier is what makes this a race: without it the first racer would
// commit before the second began, and the second's query would legitimately
// find the chat, measuring nothing.
func runRacers(body func(i int, out *racerOutcome)) []racerOutcome {
	outcomes := make([]racerOutcome, racers)

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := range racers {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			body(i, &outcomes[i])
			outcomes[i].classify()
		}()
	}
	start.Done()
	done.Wait()

	return outcomes
}

// memberWrites writes the two membership documents a direct chat creates.
// Both IDs are derived from the chat ID, which is fresh per racer, so no two
// racers can collide on a membership path either — the absence stays the only
// thing they share.
func memberWrites(
	tx *gcfs.Transaction,
	client *firestore.Client,
	chatID domain.ChatID,
	pair directPair,
	now time.Time,
) error {
	for _, userID := range []domain.UserID{pair.a, pair.b} {
		ref := client.FS.
			Collection(firestore.CollectionMemberships).
			Doc(firestore.MembershipDocID(chatID, userID))

		// Direct chats have no owner: both participants are members
		// (ADR-006 §4.1).
		if err := tx.Create(ref, firestore.MembershipDoc{
			ChatID:   chatID.String(),
			UserID:   userID.String(),
			Role:     roleMember,
			JoinedAt: now,
		}); err != nil {
			return fmt.Errorf("create membership for %s: %w", userID.String(), err)
		}
	}
	return nil
}

// createNaiveChat writes the rejected design's chat document: the pair key on
// the chat itself.
func createNaiveChat(
	tx *gcfs.Transaction,
	client *firestore.Client,
	chatID domain.ChatID,
	pair directPair,
	now time.Time,
) error {
	ref := client.FS.Collection(firestore.CollectionChats).Doc(chatID.String())

	if err := tx.Create(ref, naiveChatDoc{
		ChatType:   string(domain.ChatTypeDirect),
		CreatedBy:  pair.a.String(),
		DirectPair: pair.key,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		return fmt.Errorf("create chat: %w", err)
	}

	return memberWrites(tx, client, chatID, pair, now)
}

// countCommittedChats reports how many of the racers' chat documents exist.
//
// Counting the generated IDs rather than querying works identically across all
// three arms, including arm C, whose chat documents carry no pair field to
// query by.
func countCommittedChats(
	ctx context.Context,
	t *testing.T,
	client *firestore.Client,
	chatIDs []domain.ChatID,
) int {
	t.Helper()

	committed := 0
	for _, chatID := range chatIDs {
		_, err := client.FS.Collection(firestore.CollectionChats).Doc(chatID.String()).Get(ctx)
		switch {
		case err == nil:
			committed++
		case status.Code(err) == codes.NotFound:
		default:
			require.NoError(t, err, "reading chat %s back", chatID.String())
		}
	}
	return committed
}

// generateChatIDs gives every racer the chat ID it will try to create, decided
// before the race so a losing racer's document can still be looked for.
func generateChatIDs() []domain.ChatID {
	chatIDs := make([]domain.ChatID, racers)
	for i := range chatIDs {
		chatIDs[i] = domain.GenerateChatID()
	}
	return chatIDs
}

func chatIDStrings(chatIDs []domain.ChatID) []string {
	out := make([]string, len(chatIDs))
	for i, id := range chatIDs {
		out[i] = id.String()
	}
	return out
}

// TestConcurrentDirectChatsBehindAnEmptyQuery is the negative control for the
// claim that a Firestore transaction takes no lock on a query that matched
// nothing — asked on the path where nothing else can answer it.
//
// Every racer runs the uniqueness check ADR-016's reasoning would have
// licensed without a sentinel: inside a transaction, query `chats` for the
// canonical pair key; if the result is empty, create the chat and both
// memberships. Every document path written is fresh, so the racers share the
// empty range and nothing else.
//
// Pre-registered outcomes:
//
//	2+ chats → the absence is not lockable on this path either. The sentinel
//	           is load-bearing, ADR-023 v1.3's stated reason is restored, and
//	           C6 does not generalise beyond the OTP-confounded path.
//	1 chat   → the store defended the range, as C6/C8 predict. Attempt counts
//	           say how: a racer that ran its transaction twice was aborted and
//	           retried. The sentinel is then justified by portability and
//	           citability, not by a race it is the only thing preventing.
func TestConcurrentDirectChatsBehindAnEmptyQuery(t *testing.T) {
	// Arrange
	ctx := context.Background()
	client := liveClient(t)
	pair := newDirectPair()
	now := time.Now().UTC()
	chatIDs := generateChatIDs()

	// Act
	outcomes := runRacers(func(i int, out *racerOutcome) {
		out.err = client.FS.RunTransaction(ctx, func(_ context.Context, tx *gcfs.Transaction) error {
			// Counted inside the callback: the SDK re-invokes it on every
			// retry, so this is how many times the store made this racer
			// start over.
			out.attempts++

			query := client.FS.
				Collection(firestore.CollectionChats).
				Where(fieldDirectPair, "==", pair.key).
				Limit(1)

			existing, queryErr := tx.Documents(query).GetAll()
			if queryErr != nil {
				return fmt.Errorf("query chats by pair: %w", queryErr)
			}
			if len(existing) > 0 {
				// The naive check doing its job: a chat for this pair was
				// already visible, so this racer declines to create one. Not
				// an error — the correct outcome of the check under test.
				out.fastPath = true
				out.chatID = existing[0].Ref.ID
				return nil
			}

			out.chatID = chatIDs[i].String()
			return createNaiveChat(tx, client, chatIDs[i], pair, now)
		})
	})

	// Measure — count the chats that exist for this pair, both ways. The pair
	// query is the check under test and the ID scan is independent of it; if
	// they disagree the index is stale and neither number is evidence.
	committed := countCommittedChats(ctx, t, client, chatIDs)

	byQuery, err := client.FS.
		Collection(firestore.CollectionChats).
		Where(fieldDirectPair, "==", pair.key).
		Documents(ctx).
		GetAll()
	require.NoError(t, err, "counting the chats this arm created")

	counts := tally(outcomes)
	lines := []string{
		fmt.Sprintf("pair key:            %s", pair.key),
		fmt.Sprintf("racers:              %d", racers),
		fmt.Sprintf("chats created:       %d   <-- the quantity under test", committed),
		fmt.Sprintf("chats by pair query: %d   (cross-check, must match)", len(byQuery)),
	}
	lines = append(lines, outcomeLines(outcomes)...)
	lines = append(lines,
		fmt.Sprintf("racers retried:       %d  <-- store-detected conflicts", counts.retried),
		fmt.Sprintf("racers that declined: %d  <-- query found a chat in time", counts.fastPath),
		fmt.Sprintf("retry rejected:       %d  <-- non-retryable InvalidArgument on retry", counts.rejected))
	report(t, "ARM A — concurrent direct chats behind an empty query", lines...)

	// Assert — harness validity only. The measured direction is the finding.
	assertNoUnexpectedFailures(t, outcomes)
	assert.Equal(t, committed, len(byQuery), "the pair query disagrees with the document scan")
	require.GreaterOrEqual(t, committed, 1, "no racer committed; the harness proved nothing")
	if committed == 1 && counts.retried == 0 && counts.fastPath == 0 {
		t.Errorf("invalid harness: one chat, no retries and no racer saw an existing chat — " +
			"the racers did not overlap, so this run says nothing about locking")
	}
}

// TestConcurrentDirectChatsWithNoQueryAtAll is arm A's positive control, and
// without it arm A proves nothing.
//
// It is arm A with the query deleted and nothing else changed: the same
// barrier, the same racer count, the same fresh document IDs, the same
// collections, the same pair key written to the same indexed field. If arm A's
// aborts are caused by the query's range, this arm must show none — every
// racer commits on its first attempt and every chat exists. If this arm also
// aborts, arm A's retries are ambient contention and its result cannot be
// attributed to the query.
func TestConcurrentDirectChatsWithNoQueryAtAll(t *testing.T) {
	// Arrange
	ctx := context.Background()
	client := liveClient(t)
	pair := newDirectPair()
	now := time.Now().UTC()
	chatIDs := generateChatIDs()

	// Act
	outcomes := runRacers(func(i int, out *racerOutcome) {
		out.err = client.FS.RunTransaction(ctx, func(_ context.Context, tx *gcfs.Transaction) error {
			out.attempts++
			out.chatID = chatIDs[i].String()
			return createNaiveChat(tx, client, chatIDs[i], pair, now)
		})
	})

	// Measure
	committed := countCommittedChats(ctx, t, client, chatIDs)

	counts := tally(outcomes)
	lines := []string{
		fmt.Sprintf("pair key:            %s", pair.key),
		fmt.Sprintf("racers:              %d", racers),
		fmt.Sprintf("chats created:       %d   <-- must equal racers", committed),
	}
	lines = append(lines, outcomeLines(outcomes)...)
	lines = append(lines, fmt.Sprintf("racers retried:       %d  <-- must be 0", counts.retried))
	report(t, "ARM B — the same writes with no query at all (positive control)", lines...)

	// Assert — this arm has a required outcome, because it is the control. Its
	// failure invalidates arm A rather than reporting a finding of its own.
	assertNoUnexpectedFailures(t, outcomes)
	assert.Equal(t, racers, committed,
		"the racers did not all commit, so arm A's contention cannot be attributed to its query")
	assert.Zero(t, counts.retried,
		"racers were aborted with no query in the transaction — arm A's retries are ambient contention")
}

// TestConcurrentDirectChatsBehindTheSentinel measures the design M1.3 ships:
// read `direct_chats/{min}__{max}` first, then tx.Create it alongside the chat
// and both memberships.
//
// Uniqueness is the pre-registered expectation here, not the question — both
// the sentinel and (per C6/C8) the range protection predict exactly one chat,
// so this arm asserts it. What is not known, and what this arm exists to
// record, is which mechanism refuses each loser:
//
//	fast path      the sentinel was already visible on the first attempt, so
//	               the loser read the winner's chat ID and returned it —
//	               ADR-006 §4.1's idempotent create, no error anywhere.
//	AlreadyExists  tx.Create on the sentinel refused the write. This is the
//	               guarantee ADR-016 §2.1's conditional put was chosen for.
//	retry          the transaction was aborted before commit and re-ran, on
//	               the second attempt taking the fast path.
//
// C7 is why this is measured rather than assumed: the shipped registration
// gate proved uniqueness for five months while its sentinel refused nobody,
// and nothing recorded that because the assertion accepted either error.
func TestConcurrentDirectChatsBehindTheSentinel(t *testing.T) {
	// Arrange
	ctx := context.Background()
	client := liveClient(t)
	pair := newDirectPair()
	now := time.Now().UTC()
	chatIDs := generateChatIDs()

	sentinelRef := client.FS.Collection(collectionDirectChats).Doc(pair.key)

	// Act
	outcomes := runRacers(func(i int, out *racerOutcome) {
		out.err = client.FS.RunTransaction(ctx, func(_ context.Context, tx *gcfs.Transaction) error {
			out.attempts++

			// Firestore requires every read in a transaction to precede every
			// write, which is also the order the flow wants: the read is the
			// fast path, the Create is what enforces uniqueness (ADR-023
			// v1.3).
			snapshot, getErr := tx.Get(sentinelRef)
			switch {
			case getErr == nil:
				var existing directChatSentinelDoc
				if decodeErr := snapshot.DataTo(&existing); decodeErr != nil {
					return fmt.Errorf("decode sentinel: %w", decodeErr)
				}
				out.fastPath = true
				out.chatID = existing.ChatID
				return nil
			case status.Code(getErr) != codes.NotFound:
				return fmt.Errorf("read sentinel: %w", getErr)
			}

			if createErr := tx.Create(sentinelRef, directChatSentinelDoc{
				ChatID:    chatIDs[i].String(),
				CreatedAt: now,
			}); createErr != nil {
				return fmt.Errorf("create sentinel: %w", createErr)
			}

			chatRef := client.FS.Collection(firestore.CollectionChats).Doc(chatIDs[i].String())
			if createErr := tx.Create(chatRef, firestore.ChatDoc{
				ID:        chatIDs[i].String(),
				ChatType:  string(domain.ChatTypeDirect),
				CreatedBy: pair.a.String(),
				CreatedAt: now,
				UpdatedAt: now,
			}); createErr != nil {
				return fmt.Errorf("create chat: %w", createErr)
			}

			out.chatID = chatIDs[i].String()
			return memberWrites(tx, client, chatIDs[i], pair, now)
		})
	})

	// Measure
	committed := countCommittedChats(ctx, t, client, chatIDs)

	sentinel, err := sentinelRef.Get(ctx)
	require.NoError(t, err, "the sentinel must exist after a successful create")
	var held directChatSentinelDoc
	require.NoError(t, sentinel.DataTo(&held))

	counts := tally(outcomes)
	lines := []string{
		fmt.Sprintf("pair key:            %s", pair.key),
		fmt.Sprintf("racers:              %d", racers),
		fmt.Sprintf("chats created:       %d   <-- the invariant: exactly 1", committed),
		fmt.Sprintf("sentinel holds:      %s", held.ChatID),
	}
	lines = append(lines, outcomeLines(outcomes)...)
	lines = append(lines,
		fmt.Sprintf("refused by sentinel:  %d  <-- tx.Create AlreadyExists", counts.refused),
		fmt.Sprintf("took the fast path:   %d  <-- read the winner, returned it", counts.fastPath),
		fmt.Sprintf("racers retried:       %d  <-- aborted before commit", counts.retried),
		fmt.Sprintf("retry rejected:       %d  <-- non-retryable InvalidArgument on retry", counts.rejected))
	report(t, "ARM C — the sentinel, with the refusing mechanism recorded", lines...)

	// Assert — uniqueness is the invariant M1.3 ships, so it is asserted. The
	// attribution above is measured, not asserted: any distribution across the
	// three mechanisms is a valid outcome and all three are informative.
	assertNoUnexpectedFailures(t, outcomes)
	assert.Equal(t, 1, committed, "the pair must have exactly one chat")
	assert.Contains(t, chatIDStrings(chatIDs), held.ChatID,
		"the sentinel must name a chat a racer created")

	// Every racer that completed must agree on which chat the pair has, or the
	// clients believe they are in different conversations — the failure this
	// whole construction exists to prevent.
	//
	// A racer that ended in an error returned no chat to anyone, so it is
	// exempt: its chatID field holds the ID it would have created, and
	// asserting on that would report a divergence that no caller ever saw.
	for i, o := range outcomes {
		if o.err != nil {
			continue
		}
		assert.Equal(t, held.ChatID, o.chatID, "racer %d returned a different chat to its caller", i)
	}
}
