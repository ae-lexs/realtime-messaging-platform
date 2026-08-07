//go:build integration

// The RTM-04 negative control: does a Firestore transaction protect a query
// that matched nothing?
//
// The existing gate (TestConcurrentRegistrationYieldsExactlyOneUser) only ever
// ran *with* the phone_index sentinel in place. It proves the sentinel path
// works. It cannot show that the sentinel is what made it work, and it cannot
// show that the phantom would occur without one — the essay's central claim is
// argued, not measured. Firebase's documentation states that Cloud Firestore
// "guarantees serializable isolation of transactions", and Firestore is
// implemented over Spanner, whose locking is documented to cover key ranges
// beyond the rows a read matched. Either of those could mean the store closes
// the hole on its own.
//
// These are measurements, not gates. The outcomes were registered in advance
// and both are informative, so the tests assert only that the harness was
// valid — that every racer ran and that the contention was real — and report
// the quantity under test to the captured log. A test that failed on one of
// the pre-registered outcomes would delete the finding it exists to record.
//
// Three arms, one provisioning cycle:
//
//	A  TestConcurrentInsertsBehindAnEmptyQuery — the isolated claim. No OTP,
//	   no AuthTx: N transactions each query users by phone_number, see nothing,
//	   and insert. Nothing is shared between them except the absence.
//
//	B  TestConcurrentRegistrationWithoutTheSentinel — the system-level
//	   counterfactual: the real registration path minus tx.Create(phoneRef).
//	   Arm A isolates the store's behaviour; arm B answers whether the sentinel
//	   is what protects *this* path, given that AuthTx.run reads and writes one
//	   OTP document keyed by the same phone hash every racer contends on.
//
//	C  TestConcurrentRegistrationLoserErrors — the same five-racer gate as
//	   today, recording which refusal each loser received. The shipped gate
//	   accepts ErrAlreadyExists *or* ErrInvalidOTP and discards which, so it
//	   does not record whether the sentinel ever fired.

package firestore_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gcfs "cloud.google.com/go/firestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// racers is the concurrency of every arm, held equal to the shipped gate so
// the arms are comparable with it and with each other.
const racers = 5

var phoneCounter atomic.Int64

// uniquePhoneNumber returns a valid E.164 number no other test or run will
// use, so a phone_number query matches only the writes of the test that made
// it.
func uniquePhoneNumber(t *testing.T) domain.PhoneNumber {
	t.Helper()
	n := time.Now().UnixNano()%1_000_000_000 + phoneCounter.Add(1)*1_000_000_000
	return domain.MustPhoneNumber(fmt.Sprintf("+52%010d", n%10_000_000_000))
}

// report writes the measured quantities as one greppable block. The
// infrastructure is destroyed at the end of the session (ADR-021), so this log
// is the only durable record that the run happened.
func report(t *testing.T, arm string, lines ...string) {
	t.Helper()
	t.Logf("\n=== MEASURED: %s ===\n%s\n=== END: %s ===",
		arm, strings.Join(lines, "\n"), arm)
}

// TestConcurrentInsertsBehindAnEmptyQuery is the negative control for the
// claim that a Firestore transaction takes no lock on a query that matched
// nothing.
//
// Every racer runs the naive uniqueness check that ADR-015 §5.1's reasoning
// would have licensed on a strongly consistent store: inside a transaction,
// query users by phone_number; if the result is empty, create a user. The
// document IDs are freshly generated UUIDs, so no two racers can collide on a
// document path — the *only* thing they share is the absence they each
// observed. Nothing else is in the transaction. There is no OTP document, no
// sentinel, and no shared write target.
//
// Pre-registered outcomes:
//
//	2+ users → the absence is not lockable; the phantom is real on Firestore
//	           and the documented serializable guarantee has a boundary.
//	1 user   → the store detected the conflict. Attempt counts tell you how:
//	           a racer that ran its transaction twice was aborted and retried,
//	           which is the store refusing a range it had no matched row for.
//	           A single attempt everywhere with one user would mean the racers
//	           never actually overlapped, and the harness is invalid.
func TestConcurrentInsertsBehindAnEmptyQuery(t *testing.T) {
	// Arrange
	ctx := context.Background()
	client := liveClient(t)
	phone := uniquePhoneNumber(t)
	now := time.Now().UTC()

	userIDs := make([]domain.UserID, racers)
	results := make([]error, racers)
	attempts := make([]int, racers)
	sawExisting := make([]bool, racers)

	// Act — release all racers from one barrier so they contend rather than
	// queue. Without this the first would commit before the second began, and
	// the second's query would legitimately find a user.
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := range racers {
		userIDs[i] = domain.GenerateUserID()
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()

			results[i] = client.FS.RunTransaction(ctx,
				func(_ context.Context, tx *gcfs.Transaction) error {
					// Counted inside the callback: the SDK re-invokes it on
					// every retry, so this is how many times the store made
					// this racer start over.
					attempts[i]++

					query := client.FS.
						Collection(firestore.CollectionUsers).
						Where("phone_number", "==", phone.String()).
						Limit(1)

					existing, queryErr := tx.Documents(query).GetAll()
					if queryErr != nil {
						return fmt.Errorf("query users by phone: %w", queryErr)
					}
					if len(existing) > 0 {
						// The naive check doing its job: a user was already
						// visible, so this racer declines to create one. Not
						// an error — a correct outcome of the check under test.
						sawExisting[i] = true
						return nil
					}

					ref := client.FS.
						Collection(firestore.CollectionUsers).
						Doc(userIDs[i].String())

					return tx.Create(ref, firestore.UserDoc{
						ID:            userIDs[i].String(),
						PhoneNumber:   phone.String(),
						PhoneVerified: true,
						CreatedAt:     now,
						UpdatedAt:     now,
					})
				})
		}()
	}
	start.Done()
	done.Wait()

	// Measure — count the users that actually exist for this number.
	committed, err := client.FS.
		Collection(firestore.CollectionUsers).
		Where("phone_number", "==", phone.String()).
		Documents(ctx).
		GetAll()
	require.NoError(t, err, "counting the users this arm created")

	lines := []string{
		fmt.Sprintf("phone_number:        %s", phone.String()),
		fmt.Sprintf("racers:              %d", racers),
		fmt.Sprintf("users created:       %d   <-- the quantity under test", len(committed)),
	}
	var retried, declined int
	for i := range racers {
		if attempts[i] > 1 {
			retried++
		}
		if sawExisting[i] {
			declined++
		}
		lines = append(lines, fmt.Sprintf(
			"racer %d: attempts=%d saw-existing=%v err=%v",
			i, attempts[i], sawExisting[i], results[i]))
	}
	lines = append(lines,
		fmt.Sprintf("racers retried:      %d   <-- store-detected conflicts", retried),
		fmt.Sprintf("racers that declined: %d  <-- query found a user in time", declined))
	report(t, "ARM A — concurrent inserts behind an empty query", lines...)

	// Assert — harness validity only. The measured direction is the finding.
	for i := range racers {
		assert.GreaterOrEqual(t, attempts[i], 1, "racer %d never entered a transaction", i)
		assert.NoError(t, results[i], "racer %d failed for an unexpected reason", i)
	}
	require.GreaterOrEqual(t, len(committed), 1, "no racer committed; the harness proved nothing")
	if len(committed) == 1 && retried == 0 && declined == 0 {
		t.Errorf("invalid harness: one user, no retries and no racer saw an existing user — " +
			"the racers did not overlap, so this run says nothing about locking")
	}
}

// TestConcurrentInsertsWithNoQueryAtAll is arm A's positive control, and
// without it arm A proves nothing.
//
// It is arm A with the query deleted and nothing else changed: the same
// barrier, the same racer count, the same fresh document IDs, the same
// collection, the same phone_number value. If arm A's aborts are caused by the
// query's range, this arm must show none — every racer commits on its first
// attempt and every user exists. If this arm *also* aborts, then arm A's
// retries are ambient contention on the collection and its result cannot be
// attributed to the query.
func TestConcurrentInsertsWithNoQueryAtAll(t *testing.T) {
	// Arrange
	ctx := context.Background()
	client := liveClient(t)
	phone := uniquePhoneNumber(t)
	now := time.Now().UTC()

	userIDs := make([]domain.UserID, racers)
	results := make([]error, racers)
	attempts := make([]int, racers)

	// Act
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := range racers {
		userIDs[i] = domain.GenerateUserID()
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()

			results[i] = client.FS.RunTransaction(ctx,
				func(_ context.Context, tx *gcfs.Transaction) error {
					attempts[i]++

					ref := client.FS.
						Collection(firestore.CollectionUsers).
						Doc(userIDs[i].String())

					return tx.Create(ref, firestore.UserDoc{
						ID:            userIDs[i].String(),
						PhoneNumber:   phone.String(),
						PhoneVerified: true,
						CreatedAt:     now,
						UpdatedAt:     now,
					})
				})
		}()
	}
	start.Done()
	done.Wait()

	// Measure
	committed, err := client.FS.
		Collection(firestore.CollectionUsers).
		Where("phone_number", "==", phone.String()).
		Documents(ctx).
		GetAll()
	require.NoError(t, err, "counting the users this arm created")

	lines := []string{
		fmt.Sprintf("phone_number:  %s", phone.String()),
		fmt.Sprintf("racers:        %d", racers),
		fmt.Sprintf("users created: %d   <-- expected %d: nothing here can conflict", len(committed), racers),
	}
	var retried int
	for i := range racers {
		if attempts[i] > 1 {
			retried++
		}
		lines = append(lines, fmt.Sprintf("racer %d: attempts=%d err=%v", i, attempts[i], results[i]))
	}
	lines = append(lines, fmt.Sprintf(
		"racers retried: %d   <-- must be 0, or arm A's aborts are not the query's doing", retried))
	report(t, "ARM D — positive control: the same inserts with no query", lines...)

	// Assert — this arm does gate, because it is the control that licenses
	// arm A's interpretation.
	assert.Equal(t, racers, len(committed),
		"writes to distinct fresh document IDs must not conflict with one another")
	assert.Zero(t, retried, "no racer should have been aborted when no query was read")
}

// TestConcurrentRegistrationWithoutTheSentinel is arm A's question asked of
// the real registration path.
//
// It matters separately because AuthTx.run reads otp_requests/{phoneHash} and
// then writes it, and every racer for one phone number addresses the *same*
// OTP document. Under the server SDK's pessimistic locking that document is a
// shared read-and-write target keyed by exactly the contended value, which
// makes it a second candidate explanation for "exactly one user" — one that
// has nothing to do with predicate locking. If this arm yields one user while
// arm A yields several, the OTP document is serialising registration and the
// sentinel is redundant *on this path*, though not on any path that lacks a
// one-time code.
func TestConcurrentRegistrationWithoutTheSentinel(t *testing.T) {
	// Arrange
	ctx := context.Background()
	client := liveClient(t)
	requests := firestore.NewOTPRequests(client)
	authTx := firestore.NewAuthTx(client)

	phoneHash := uniquePhoneHash(t)
	phone := uniquePhoneNumber(t)
	now := time.Now().UTC()

	require.NoError(t, requests.Create(ctx,
		firestore.NewOTPRequestDoc(phoneHash, "mac-negative-control", now,
			now.Add(domain.OTPValidityDuration)), now))

	userIDs := make([]domain.UserID, racers)
	results := make([]error, racers)

	// Act
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := range racers {
		userIDs[i] = domain.GenerateUserID()
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			results[i] = authTx.RegisterWithoutSentinel(ctx, registration(
				phoneHash, "mac-negative-control", now,
				userIDs[i], domain.GenerateSessionID(), phone.String()))
		}()
	}
	start.Done()
	done.Wait()

	// Measure
	committed, err := client.FS.
		Collection(firestore.CollectionUsers).
		Where("phone_number", "==", phone.String()).
		Documents(ctx).
		GetAll()
	require.NoError(t, err, "counting the users this arm created")

	lines := []string{
		fmt.Sprintf("phone_number:  %s", phone.String()),
		fmt.Sprintf("racers:        %d", racers),
		fmt.Sprintf("users created: %d   <-- the quantity under test", len(committed)),
	}
	var winners int
	for i := range racers {
		if results[i] == nil {
			winners++
		}
		lines = append(lines, fmt.Sprintf("racer %d: err=%v", i, results[i]))
	}
	lines = append(lines, fmt.Sprintf("racers returning nil: %d", winners))
	report(t, "ARM B — registration with the sentinel removed", lines...)

	// Assert — harness validity only.
	require.GreaterOrEqual(t, len(committed), 1, "no racer committed; the harness proved nothing")
}

// TestConcurrentRegistrationLoserErrors closes a measurement gap in the
// shipped gate rather than opening a new question.
//
// TestConcurrentRegistrationYieldsExactlyOneUser accepts ErrAlreadyExists or
// ErrInvalidOTP from a loser and records neither, so its captured PASS does
// not show whether the sentinel ever refused anybody. If every loser lost on
// the OTP assertion, the sentinel did no work in the run the ledger cites for
// RTM-04-C2.
func TestConcurrentRegistrationLoserErrors(t *testing.T) {
	// Arrange
	ctx := context.Background()
	client := liveClient(t)
	requests := firestore.NewOTPRequests(client)
	authTx := firestore.NewAuthTx(client)

	phoneHash := uniquePhoneHash(t)
	phone := uniquePhoneNumber(t)
	now := time.Now().UTC()

	require.NoError(t, requests.Create(ctx,
		firestore.NewOTPRequestDoc(phoneHash, "mac-attribution", now,
			now.Add(domain.OTPValidityDuration)), now))

	userIDs := make([]domain.UserID, racers)
	results := make([]error, racers)

	// Act
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := range racers {
		userIDs[i] = domain.GenerateUserID()
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			results[i] = authTx.Register(ctx, registration(
				phoneHash, "mac-attribution", now,
				userIDs[i], domain.GenerateSessionID(), phone.String()))
		}()
	}
	start.Done()
	done.Wait()

	// Measure — attribute every refusal.
	var winners, sentinelRefusals, otpRefusals, other int
	lines := []string{fmt.Sprintf("racers: %d", racers)}
	for i := range racers {
		switch {
		case results[i] == nil:
			winners++
			lines = append(lines, fmt.Sprintf("racer %d: WON", i))
		case errors.Is(results[i], domain.ErrAlreadyExists):
			sentinelRefusals++
			lines = append(lines, fmt.Sprintf("racer %d: refused by the SENTINEL (%v)", i, results[i]))
		case errors.Is(results[i], domain.ErrInvalidOTP):
			otpRefusals++
			lines = append(lines, fmt.Sprintf("racer %d: refused by the OTP assertion (%v)", i, results[i]))
		default:
			other++
			lines = append(lines, fmt.Sprintf("racer %d: UNEXPECTED (%v)", i, results[i]))
		}
	}
	lines = append(lines,
		fmt.Sprintf("winners:            %d", winners),
		fmt.Sprintf("sentinel refusals:  %d   <-- how often phone_index did the work", sentinelRefusals),
		fmt.Sprintf("OTP refusals:       %d   <-- how often the OTP document did it instead", otpRefusals),
		fmt.Sprintf("unexpected:         %d", other))
	report(t, "ARM C — which document refuses the losers", lines...)

	// Assert — this arm re-runs a shipped invariant, so it does gate.
	assert.Equal(t, 1, winners, "exactly one concurrent registration may claim a phone number")
	assert.Zero(t, other, "a loser failed for a reason neither document explains")
}
