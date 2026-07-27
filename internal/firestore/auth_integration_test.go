//go:build integration

// The M1.2 gate's Firestore half: the auth semantics that only a real database
// can demonstrate, because every one of them is about a *conditional* write or
// a *concurrent* one, and both are properties of the store rather than of the
// code that calls it. There is no emulator (ADR-021 Axis F), so this runs only
// when pointed at a live database:
//
//	PROJECT_ID=... make auth-test
//
// Every document is written under a freshly generated ID or phone hash, so runs
// never collide and nothing needs cleaning up between them.

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

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// uniquePhoneHash returns a phone hash no other test or run will use.
func uniquePhoneHash(t *testing.T) string {
	t.Helper()
	return auth.HashPhone(fmt.Sprintf("+5255%s", domain.GenerateUserID().String()))
}

// TestOTPCreateRefusesALiveOTPButNotAnExpiredOne is the reason
// OTPRequests.Create is a transaction rather than a Create().
//
// Firestore's Create() fails whenever the document exists, and TTL collects an
// expired OTP only within ~24 hours of its expiry. A bare Create() would
// therefore refuse every new code for almost a full day after one five-minute
// OTP lapsed — locking a phone number out of the product entirely, with no
// error anywhere that would explain it. The second half of this test is the
// case that catches that.
func TestOTPCreateRefusesALiveOTPButNotAnExpiredOne(t *testing.T) {
	// Arrange
	ctx := context.Background()
	requests := firestore.NewOTPRequests(liveClient(t))

	phoneHash := uniquePhoneHash(t)
	now := time.Now().UTC()

	// Act — issue an OTP that is live for five minutes.
	first := firestore.NewOTPRequestDoc(phoneHash, "mac-first", now, now.Add(domain.OTPValidityDuration))
	require.NoError(t, requests.Create(ctx, first, now))

	// Assert — a second request while it is live is refused.
	second := firestore.NewOTPRequestDoc(phoneHash, "mac-second", now, now.Add(domain.OTPValidityDuration))
	err := requests.Create(ctx, second, now)
	assert.ErrorIs(t, err, domain.ErrAlreadyExists)

	stored, err := requests.Get(ctx, phoneHash)
	require.NoError(t, err)
	assert.Equal(t, "mac-first", stored.OTPMAC, "the live OTP must not be overwritten")

	// The document is still present hours later — TTL has not run — but it is
	// expired, so a fresh request must now succeed.
	later := now.Add(6 * time.Hour)
	require.True(t, stored.IsExpired(later))

	third := firestore.NewOTPRequestDoc(phoneHash, "mac-third", later, later.Add(domain.OTPValidityDuration))
	require.NoError(t, requests.Create(ctx, third, later),
		"an expired-but-uncollected OTP must not block a new one for the ~24h until TTL runs")

	stored, err = requests.Get(ctx, phoneHash)
	require.NoError(t, err)
	assert.Equal(t, "mac-third", stored.OTPMAC)
	assert.Zero(t, stored.AttemptCount, "reissuing resets the attempt budget with the code")
}

// TestOTPCreateReplacesAConsumedOTP covers the third disjunct of ADR-015 §1.2's
// condition, `#status = "verified"`.
//
// The live flow gate is what found this missing: a user verifies successfully,
// immediately asks for another code, and gets refused — because the spent
// record was still inside its five-minute window and an expiry-only check
// treats it as live. Nothing is protected by keeping it: the code is consumed
// and cannot be replayed, and throttling re-requests is the rate limiter's job
// (ADR-013 §4.1), not this write's.
func TestOTPCreateReplacesAConsumedOTP(t *testing.T) {
	// Arrange — an OTP consumed by a registration, still well inside its
	// validity window.
	ctx := context.Background()
	client := liveClient(t)
	requests := firestore.NewOTPRequests(client)
	authTx := firestore.NewAuthTx(client)

	phoneHash := uniquePhoneHash(t)
	now := time.Now().UTC()
	phone, err := domain.NewPhoneNumber("+525512340005")
	require.NoError(t, err)

	require.NoError(t, requests.Create(ctx,
		firestore.NewOTPRequestDoc(phoneHash, "mac-consumed", now, now.Add(domain.OTPValidityDuration)), now))
	require.NoError(t, authTx.Register(ctx,
		registration(phoneHash, "mac-consumed", now, domain.GenerateUserID(), domain.GenerateSessionID(), phone.String())))

	consumed, err := requests.Get(ctx, phoneHash)
	require.NoError(t, err)
	require.Equal(t, domain.OTPStatusVerified, consumed.Status)
	require.False(t, consumed.IsExpired(now), "the spent record is still inside its window")

	// Act — request another code one second later.
	soon := now.Add(time.Second)
	next := firestore.NewOTPRequestDoc(phoneHash, "mac-next", soon, soon.Add(domain.OTPValidityDuration))

	// Assert
	require.NoError(t, requests.Create(ctx, next, soon),
		"a consumed OTP must not block a new one for the rest of its window")

	stored, err := requests.Get(ctx, phoneHash)
	require.NoError(t, err)
	assert.Equal(t, "mac-next", stored.OTPMAC)
	assert.Equal(t, domain.OTPStatusPending, stored.Status, "the replacement is issuable, not pre-consumed")
}

// TestOTPExpiresAtSurvivesTheRoundTripExactly guards the MAC-stability
// invariant end to end. auth.ComputeOTPMAC binds the MAC to expires_at's
// RFC3339 rendering, and Firestore stores microseconds — so if the value that
// comes back were not byte-identical to the one that went in, every OTP would
// verify in memory and fail after a write.
func TestOTPExpiresAtSurvivesTheRoundTripExactly(t *testing.T) {
	// Arrange — a timestamp carrying nanoseconds Firestore cannot hold.
	ctx := context.Background()
	requests := firestore.NewOTPRequests(liveClient(t))

	phoneHash := uniquePhoneHash(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 123456789, time.UTC)
	expiresAt := now.Add(domain.OTPValidityDuration)

	pepper := []byte("integration-pepper-32-bytes-long")
	mac := auth.ComputeOTPMAC(pepper, "123456", phoneHash, expiresAt)

	// Act
	doc := firestore.NewOTPRequestDoc(phoneHash, mac, now, expiresAt)
	require.NoError(t, requests.Create(ctx, doc, now))

	stored, err := requests.Get(ctx, phoneHash)
	require.NoError(t, err)

	// Assert — the MAC recomputed from the stored timestamp still verifies.
	assert.True(t,
		auth.VerifyOTPMAC(pepper, "123456", phoneHash, stored.ExpiresAt, stored.OTPMAC),
		"the MAC must still verify against the expires_at that came back from Firestore",
	)
	assert.Equal(t, auth.OTPMACTime(expiresAt), auth.OTPMACTime(stored.ExpiresAt))
}

// TestOTPIncrementAttemptsIsAtomic checks that concurrent wrong guesses all
// count. A read-modify-write would lose increments under contention, and the
// symptom would be an attacker quietly getting more than MaxOTPVerifyAttempts
// tries at a six-digit code.
func TestOTPIncrementAttemptsIsAtomic(t *testing.T) {
	// Arrange
	ctx := context.Background()
	requests := firestore.NewOTPRequests(liveClient(t))

	phoneHash := uniquePhoneHash(t)
	now := time.Now().UTC()
	require.NoError(t, requests.Create(ctx,
		firestore.NewOTPRequestDoc(phoneHash, "mac", now, now.Add(domain.OTPValidityDuration)), now))

	const guesses = 5

	// Act
	var wg sync.WaitGroup
	errs := make([]error, guesses)
	for i := range guesses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = requests.IncrementAttempts(ctx, phoneHash)
		}()
	}
	wg.Wait()

	// Assert
	for _, err := range errs {
		require.NoError(t, err)
	}
	stored, err := requests.Get(ctx, phoneHash)
	require.NoError(t, err)
	assert.Equal(t, guesses, stored.AttemptCount, "every concurrent attempt must count")
}

// TestRegisterCommitsEverythingOrNothing covers the ADR-015 §5.1 invariant that
// made the transaction necessary in the first place: OTP consumption and
// session creation must not be separable, or a code can be consumed twice.
func TestRegisterCommitsEverythingOrNothing(t *testing.T) {
	ctx := context.Background()
	client := liveClient(t)
	requests := firestore.NewOTPRequests(client)
	users := firestore.NewUsers(client)
	sessions := firestore.NewSessions(client)
	authTx := firestore.NewAuthTx(client)

	t.Run("a valid OTP commits the whole write set", func(t *testing.T) {
		// Arrange
		phoneHash := uniquePhoneHash(t)
		now := time.Now().UTC()
		phone, err := domain.NewPhoneNumber("+525512340001")
		require.NoError(t, err)

		require.NoError(t, requests.Create(ctx,
			firestore.NewOTPRequestDoc(phoneHash, "mac-valid", now, now.Add(domain.OTPValidityDuration)), now))

		userID, sessionID := domain.GenerateUserID(), domain.GenerateSessionID()

		// Act
		require.NoError(t, authTx.Register(ctx, registration(phoneHash, "mac-valid", now, userID, sessionID, phone.String())))

		// Assert — user, session and consumed OTP all landed.
		user, err := users.Get(ctx, userID)
		require.NoError(t, err)
		assert.True(t, user.PhoneVerified)

		session, err := sessions.Get(ctx, sessionID)
		require.NoError(t, err)
		assert.Equal(t, userID.String(), session.UserID)

		otp, err := requests.Get(ctx, phoneHash)
		require.NoError(t, err)
		assert.Equal(t, domain.OTPStatusVerified, otp.Status)
	})

	t.Run("a consumed OTP cannot be replayed, and nothing is written", func(t *testing.T) {
		// Arrange — an OTP already marked verified.
		phoneHash := uniquePhoneHash(t)
		now := time.Now().UTC()
		phone, err := domain.NewPhoneNumber("+525512340002")
		require.NoError(t, err)

		require.NoError(t, requests.Create(ctx,
			firestore.NewOTPRequestDoc(phoneHash, "mac-once", now, now.Add(domain.OTPValidityDuration)), now))

		firstUser, firstSession := domain.GenerateUserID(), domain.GenerateSessionID()
		require.NoError(t, authTx.Register(ctx, registration(phoneHash, "mac-once", now, firstUser, firstSession, phone.String())))

		// Act — replay the same code with fresh IDs.
		replayUser, replaySession := domain.GenerateUserID(), domain.GenerateSessionID()
		err = authTx.Register(ctx, registration(phoneHash, "mac-once", now, replayUser, replaySession, phone.String()))

		// Assert
		assert.ErrorIs(t, err, domain.ErrInvalidOTP)

		_, err = users.Get(ctx, replayUser)
		assert.ErrorIs(t, err, domain.ErrNotFound, "a rejected OTP must leave no user behind")
		_, err = sessions.Get(ctx, replaySession)
		assert.ErrorIs(t, err, domain.ErrNotFound, "a rejected OTP must leave no session behind")
	})

	t.Run("an expired OTP is refused even though the document still exists", func(t *testing.T) {
		// Arrange
		phoneHash := uniquePhoneHash(t)
		now := time.Now().UTC()
		phone, err := domain.NewPhoneNumber("+525512340003")
		require.NoError(t, err)

		require.NoError(t, requests.Create(ctx,
			firestore.NewOTPRequestDoc(phoneHash, "mac-stale", now, now.Add(domain.OTPValidityDuration)), now))

		// Act — verify well after expiry, while TTL has certainly not run.
		later := now.Add(6 * time.Hour)
		userID, sessionID := domain.GenerateUserID(), domain.GenerateSessionID()
		err = authTx.Register(ctx, registration(phoneHash, "mac-stale", later, userID, sessionID, phone.String()))

		// Assert
		assert.ErrorIs(t, err, domain.ErrInvalidOTP)
		_, err = users.Get(ctx, userID)
		assert.ErrorIs(t, err, domain.ErrNotFound)
	})
}

// TestConcurrentRegistrationYieldsExactlyOneUser is why phone_index exists.
//
// Firestore's queries are strongly consistent, which is easy to mistake for
// enough. It is not: a transaction locks the documents it *reads*, and a query
// matching nothing locks nothing, so two simultaneous first-time verifications
// of one number would both see "no user" and both commit — two accounts for one
// phone, with nothing reporting a problem. tx.Create on a deterministic
// document path is the only thing in the store that can make one of them lose.
func TestConcurrentRegistrationYieldsExactlyOneUser(t *testing.T) {
	// Arrange
	ctx := context.Background()
	client := liveClient(t)
	requests := firestore.NewOTPRequests(client)
	users := firestore.NewUsers(client)
	authTx := firestore.NewAuthTx(client)

	phoneHash := uniquePhoneHash(t)
	now := time.Now().UTC()
	phone, err := domain.NewPhoneNumber("+525512340004")
	require.NoError(t, err)

	require.NoError(t, requests.Create(ctx,
		firestore.NewOTPRequestDoc(phoneHash, "mac-race", now, now.Add(domain.OTPValidityDuration)), now))

	const racers = 5
	userIDs := make([]domain.UserID, racers)
	results := make([]error, racers)

	// Act — every racer holds the same valid OTP and a distinct new user ID.
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := range racers {
		userIDs[i] = domain.GenerateUserID()
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			results[i] = authTx.Register(ctx,
				registration(phoneHash, "mac-race", now, userIDs[i], domain.GenerateSessionID(), phone.String()))
		}()
	}
	start.Done()
	done.Wait()

	// Assert — exactly one racer committed.
	var winners int
	for i, err := range results {
		if err == nil {
			winners++
			_, getErr := users.Get(ctx, userIDs[i])
			assert.NoError(t, getErr, "the winner's user must exist")
			continue
		}
		// Losers see either the sentinel already claimed or the OTP already
		// consumed, depending on which the transaction reached first. Both are
		// correct refusals; what matters is that they did not commit.
		assert.True(t,
			errors.Is(err, domain.ErrAlreadyExists) || errors.Is(err, domain.ErrInvalidOTP),
			"unexpected loser error: %v", err,
		)
		_, getErr := users.Get(ctx, userIDs[i])
		assert.ErrorIs(t, getErr, domain.ErrNotFound, "a loser must leave no user behind")
	}
	assert.Equal(t, 1, winners, "exactly one concurrent registration may claim a phone number")
}

// TestSessionRotationIsSingleUse covers ADR-015 §4.2's conditional update.
// Without the precondition both holders of a refresh token would succeed, and
// the later write would overwrite prev_token_hash — erasing the evidence reuse
// detection reads, so a stolen token would come back "invalid" instead of
// revoking the session it was stolen from.
func TestSessionRotationIsSingleUse(t *testing.T) {
	// Arrange
	ctx := context.Background()
	sessions := firestore.NewSessions(liveClient(t))

	sessionID := domain.GenerateSessionID()
	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, sessions.Set(ctx, firestore.SessionDoc{
		ID:               sessionID.String(),
		UserID:           domain.GenerateUserID().String(),
		DeviceID:         domain.GenerateDeviceID().String(),
		RefreshTokenHash: "hash-gen-1",
		TokenGeneration:  1,
		CreatedAt:        now,
		ExpiresAt:        now.Add(domain.RefreshTokenLifetime),
	}))

	// Act — rotate once, legitimately.
	require.NoError(t, sessions.Rotate(ctx, sessionID, firestore.SessionRotation{
		RefreshTokenHash: "hash-gen-2",
		PrevTokenHash:    "hash-gen-1",
		TokenGeneration:  2,
		ExpiresAt:        now.Add(domain.RefreshTokenLifetime),
	}))

	// Assert
	rotated, err := sessions.Get(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, "hash-gen-2", rotated.RefreshTokenHash)
	assert.Equal(t, "hash-gen-1", rotated.PrevTokenHash)
	assert.Equal(t, int64(2), rotated.TokenGeneration)

	// A second rotation from the same starting hash is a replay of a spent
	// token and must be refused.
	err = sessions.Rotate(ctx, sessionID, firestore.SessionRotation{
		RefreshTokenHash: "hash-attacker",
		PrevTokenHash:    "hash-gen-1",
		TokenGeneration:  2,
		ExpiresAt:        now.Add(domain.RefreshTokenLifetime),
	})
	assert.ErrorIs(t, err, domain.ErrInvalidRefreshToken)

	unchanged, err := sessions.Get(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, "hash-gen-2", unchanged.RefreshTokenHash, "a refused rotation must change nothing")
	assert.Equal(t, "hash-gen-1", unchanged.PrevTokenHash, "reuse detection's evidence must survive")

	require.NoError(t, sessions.Delete(ctx, sessionID))
}

// registration builds the write set for a first-time verify-otp.
func registration(
	phoneHash, otpMAC string,
	now time.Time,
	userID domain.UserID,
	sessionID domain.SessionID,
	phone string,
) firestore.Registration {
	return firestore.Registration{
		OTP: firestore.OTPCondition{
			PhoneHash:   phoneHash,
			OTPMAC:      otpMAC,
			Now:         now,
			MaxAttempts: domain.MaxOTPVerifyAttempts,
		},
		PhoneIndex: firestore.PhoneIndexDoc{
			ID:        phoneHash,
			UserID:    userID.String(),
			CreatedAt: now,
		},
		User: firestore.UserDoc{
			ID:            userID.String(),
			PhoneNumber:   phone,
			PhoneVerified: true,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		Session: firestore.SessionDoc{
			ID:               sessionID.String(),
			UserID:           userID.String(),
			DeviceID:         domain.GenerateDeviceID().String(),
			RefreshTokenHash: "hash-initial",
			TokenGeneration:  1,
			CreatedAt:        now,
			ExpiresAt:        now.Add(domain.RefreshTokenLifetime),
		},
	}
}
