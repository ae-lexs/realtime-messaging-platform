//go:build integration

// Experimental apparatus for the RTM-04 negative control. Compiled only into
// the integration test binary, never into the service.
//
// The claim under test is that `phone_index` is what makes concurrent
// registration safe. Testing that requires running the registration path with
// the sentinel removed and nothing else changed — in particular with the same
// AuthTx.run wrapper, because that wrapper reads and writes the OTP document
// and is itself a candidate explanation for the observed uniqueness.
// Reimplementing the wrapper in the test would have measured the copy.

package firestore

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

// RegisterWithoutSentinel is Register with exactly one statement removed: the
// tx.Create on phone_index. Everything else — the OTP read, the assertion, the
// user and session creates, the OTP consumption, the transaction boundary — is
// the production path, called through the same unexported run.
func (t *AuthTx) RegisterWithoutSentinel(ctx context.Context, params Registration) error {
	if err := params.User.Validate(); err != nil {
		return err
	}
	if err := params.Session.Validate(); err != nil {
		return err
	}

	return t.run(ctx, params.OTP, func(tx *firestore.Transaction) error {
		userRef := t.client.FS.Collection(CollectionUsers).Doc(params.User.ID)
		if err := tx.Create(userRef, params.User); err != nil {
			return fmt.Errorf("firestore: create user %s: %w", params.User.ID, err)
		}

		sessionRef := t.client.FS.Collection(CollectionSessions).Doc(params.Session.ID)
		if err := tx.Create(sessionRef, params.Session); err != nil {
			return fmt.Errorf("firestore: create session %s: %w", params.Session.ID, err)
		}

		return nil
	})
}
