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

// AuthTx performs the two multi-document writes ADR-015 §5 requires to be
// atomic: consuming an OTP together with creating a session, and — on first
// sight of a phone number — a user and its uniqueness sentinel as well.
//
// The atomicity is not decorative. ADR-015 §5.1 records why the earlier
// non-transactional design was wrong: validate the OTP, then fail to create
// the session, and the OTP is left "pending" and re-verifiable, so a code can
// be consumed twice. Either everything commits or nothing does.
//
// This is the Firestore translation of a DynamoDB TransactWriteItems, and the
// two stores express the same guarantee differently. DynamoDB attached a
// ConditionExpression to each item; Firestore has no per-write condition, so
// the OTP assertions are made in Go against a document read *inside* the
// transaction. That is equivalent, not weaker: the transaction fails and
// retries if any document it read changed before commit, which is exactly what
// the conditions defended against.
type AuthTx struct{ client *Client }

// NewAuthTx returns the auth transaction writer.
func NewAuthTx(client *Client) *AuthTx { return &AuthTx{client: client} }

// OTPCondition is the assertion made about the pending OTP before it is
// consumed — the Firestore form of ADR-015 §5.1's ConditionExpression
// (status = pending AND now < expires_at AND attempt_count < 5 AND otp_mac
// matches).
//
// The service layer has already checked all of this against its own read; that
// check is the user-facing one, producing the right error for the right
// reason. This one is the race guard, re-asserted under the transaction's
// isolation so a concurrent verification of the same code cannot slip between
// the two.
type OTPCondition struct {
	PhoneHash   string
	OTPMAC      string
	Now         time.Time
	MaxAttempts int
}

// Registration is the write set for a first-time verify-otp: claim the phone
// number, create the user and the session, consume the OTP.
type Registration struct {
	OTP        OTPCondition
	PhoneIndex PhoneIndexDoc
	User       UserDoc
	Session    SessionDoc
}

// Login is the write set for a returning user's verify-otp: create the
// session, consume the OTP.
type Login struct {
	OTP     OTPCondition
	Session SessionDoc
}

// Register commits a new user, their first session, the phone-number sentinel
// and the OTP consumption as one unit.
//
// It returns domain.ErrAlreadyExists when the sentinel is already held. That
// is the concurrent-registration case, and it is the whole reason the sentinel
// exists: two simultaneous first-time verifications of one number both find no
// user via users.phone_number, because a Firestore query that matches nothing
// locks nothing and so offers no phantom protection. tx.Create on a
// deterministic document path is the only thing in the store that can make one
// of them lose. The caller resolves the loss by re-reading the winner's user
// and continuing as a login (ADR-015 §5.1).
func (t *AuthTx) Register(ctx context.Context, params Registration) error {
	if err := params.PhoneIndex.Validate(); err != nil {
		return err
	}
	if err := params.User.Validate(); err != nil {
		return err
	}
	if err := params.Session.Validate(); err != nil {
		return err
	}

	return t.run(ctx, params.OTP, func(tx *firestore.Transaction) error {
		phoneRef := t.client.FS.Collection(CollectionPhoneIndex).Doc(params.PhoneIndex.ID)
		if err := tx.Create(phoneRef, params.PhoneIndex); err != nil {
			return fmt.Errorf("firestore: claim phone %s: %w", params.PhoneIndex.ID, err)
		}

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

// Login commits a returning user's session and the OTP consumption as one unit.
func (t *AuthTx) Login(ctx context.Context, params Login) error {
	if err := params.Session.Validate(); err != nil {
		return err
	}

	return t.run(ctx, params.OTP, func(tx *firestore.Transaction) error {
		sessionRef := t.client.FS.Collection(CollectionSessions).Doc(params.Session.ID)
		if err := tx.Create(sessionRef, params.Session); err != nil {
			return fmt.Errorf("firestore: create session %s: %w", params.Session.ID, err)
		}
		return nil
	})
}

// run holds the shape both flows share: assert the OTP, apply the flow's
// writes, then mark the OTP verified.
//
// The ordering is forced by Firestore, which requires every read in a
// transaction to precede every write. That happens to be the ordering the
// invariant wants anyway — nothing is written until the OTP has been proven
// good.
func (t *AuthTx) run(
	ctx context.Context,
	cond OTPCondition,
	writes func(tx *firestore.Transaction) error,
) error {
	ctx, cancel := t.client.withTimeout(ctx)
	defer cancel()

	otpRef := t.client.FS.Collection(CollectionOTPRequests).Doc(cond.PhoneHash)

	err := t.client.FS.RunTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		snapshot, getErr := tx.Get(otpRef)
		if status.Code(getErr) == codes.NotFound {
			return fmt.Errorf("firestore: no OTP for %s: %w", cond.PhoneHash, domain.ErrInvalidOTP)
		}
		if getErr != nil {
			return fmt.Errorf("firestore: read OTP request %s: %w", cond.PhoneHash, getErr)
		}

		otp, decodeErr := otpRequestFrom(snapshot)
		if decodeErr != nil {
			return decodeErr
		}
		if assertErr := cond.assert(otp); assertErr != nil {
			return assertErr
		}

		if writeErr := writes(tx); writeErr != nil {
			return writeErr
		}

		// Consume last. The OTP stays in the collection as "verified" until
		// TTL collects it, so a replay is refused as already-used rather than
		// read as never-issued.
		if updateErr := tx.Update(otpRef, []firestore.Update{
			{Path: "status", Value: domain.OTPStatusVerified},
		}); updateErr != nil {
			return fmt.Errorf("firestore: consume OTP %s: %w", cond.PhoneHash, updateErr)
		}

		return nil
	})
	if status.Code(err) == codes.AlreadyExists {
		// In practice this is the phone-number sentinel: user and session IDs
		// are freshly generated UUIDs, so nothing can already hold them.
		return fmt.Errorf("firestore: phone %s already claimed: %w", cond.PhoneHash, domain.ErrAlreadyExists)
	}
	return err
}

// assert re-checks ADR-015 §5.1's conditions against the record read inside
// the transaction.
func (c OTPCondition) assert(otp OTPRequestDoc) error {
	if otp.Status != domain.OTPStatusPending {
		return fmt.Errorf("firestore: OTP %s already consumed: %w", c.PhoneHash, domain.ErrInvalidOTP)
	}
	if otp.IsExpired(c.Now) {
		return fmt.Errorf("firestore: OTP %s expired: %w", c.PhoneHash, domain.ErrInvalidOTP)
	}
	if otp.AttemptCount >= c.MaxAttempts {
		return fmt.Errorf("firestore: OTP %s out of attempts: %w", c.PhoneHash, domain.ErrRateLimited)
	}
	if otp.OTPMAC != c.OTPMAC {
		// The record was replaced by a newer OTP between the service's read
		// and this transaction, so the code the caller verified is no longer
		// the live one.
		return fmt.Errorf("firestore: OTP %s superseded: %w", c.PhoneHash, domain.ErrInvalidOTP)
	}
	return nil
}
