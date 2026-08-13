package firestore

import (
	"context"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// retryOptionsMismatch is how many times runTransaction will start a wholly new
// transaction after the store rejected a retried one (see runTransaction).
//
// Two is enough for a condition measured at a few percent of transactions: a
// third attempt buys a vanishing improvement and lengthens the tail of a
// request that is holding a client connection open.
const retryOptionsMismatch = 2

// optionsMismatchFragment identifies the rejection by its message, because
// nothing else distinguishes it.
//
// Matching on message text is ordinarily a mistake — errors are matched by
// code, never by string (docs/standards/GO.md). It is done here because the
// status code alone cannot be used: InvalidArgument is also what a genuinely
// malformed write returns, and retrying those would turn a permanent client
// error into three of them. The narrower signal is the message, so the match
// is deliberately narrow and its failure mode is safe — if the wording ever
// changes, this stops matching and the error surfaces to the caller exactly as
// it would have without this code.
const optionsMismatchFragment = "Transaction options should be the same"

// runTransaction runs fn in a Firestore transaction, restarting it from
// scratch if the store rejects the client library's own retry.
//
// Why this exists. A read-write transaction that the store aborts is retried
// by the SDK, and the retry is not identical to the first attempt: the first
// BeginTransaction sends no options at all, while every retry sends an explicit
// ReadWrite carrying the previous transaction's ID (firestore@v1.24.0,
// transaction.go). The backend intermittently refuses that second shape with
//
//	InvalidArgument: Transaction options should be the same as specified
//	                 previous transaction
//
// and because the SDK's retry loop is gated on isAborted, an InvalidArgument
// returns to the caller immediately rather than being attempted again. The
// caller therefore sees a failure on a transaction that committed nothing.
//
// This was measured, not inferred, while validating M1.3's direct-pair
// invariant: it occurs under both concurrency modes, with incidence varying
// between runs of identical code, and the trigger is not established
// (docs/artifacts/evidence/m1.3-direct-pair-*.log, ADR-023 v1.4). The
// consequence does not depend on the cause — a transaction that failed this
// way applied no writes, so starting a fresh one is safe and is the only thing
// that turns an avoidable error into the success the caller asked for.
//
// It is a whole new transaction rather than an inner loop on purpose: the
// rejection is *about* the retry's options, so retrying within the same
// transaction would reproduce the condition. A new RunTransaction call begins
// again with no options, which is the shape that was accepted the first time.
//
// fn must therefore be safe to run more than once. Every transaction body in
// this package already is, because Firestore's own retry imposes the same
// requirement — the SDK re-invokes the callback on every abort.
func (c *Client) runTransaction(
	ctx context.Context,
	fn func(ctx context.Context, tx *firestore.Transaction) error,
) error {
	var err error
	for attempt := 0; attempt <= retryOptionsMismatch; attempt++ {
		err = c.FS.RunTransaction(ctx, fn)
		if !isOptionsMismatch(err) {
			return err
		}
	}
	return err
}

// isOptionsMismatch reports whether err is the rejected-retry condition
// runTransaction restarts on.
func isOptionsMismatch(err error) bool {
	if status.Code(err) != codes.InvalidArgument {
		return false
	}
	return strings.Contains(status.Convert(err).Message(), optionsMismatchFragment)
}
