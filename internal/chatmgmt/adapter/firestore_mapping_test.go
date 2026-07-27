package adapter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aelexs/realtime-messaging-platform/internal/chatmgmt/app"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/firestore"
)

// The Firestore adapters hold no logic beyond mapping, so the mapping is what
// there is to test hermetically. Everything conditional or transactional lives
// in internal/firestore and is covered by the live gate (scripts/auth.sh).
//
// These are not ceremonial. A field dropped in a mapper is invisible: the code
// compiles, the document writes, and the value is simply absent — a missing
// prev_token_hash silently disables reuse detection, a missing device_id
// silently disables device binding.

func TestToOTPRecordCarriesEveryField(t *testing.T) {
	// Arrange
	created := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	doc := firestore.OTPRequestDoc{
		ID:           "phone-hash",
		OTPMAC:       "mac-value",
		Status:       domain.OTPStatusPending,
		AttemptCount: 3,
		CreatedAt:    created,
		ExpiresAt:    created.Add(5 * time.Minute),
	}

	// Act
	record := toOTPRecord(doc)

	// Assert — the document ID is the phone hash; it is not a document field.
	assert.Equal(t, "phone-hash", record.PhoneHash)
	assert.Equal(t, "mac-value", record.OTPMAC)
	assert.Equal(t, domain.OTPStatusPending, record.Status)
	assert.Equal(t, 3, record.AttemptCount)
	assert.Equal(t, created, record.CreatedAt)
	assert.Equal(t, created.Add(5*time.Minute), record.ExpiresAt)
}

func TestToUserRecordCarriesEveryField(t *testing.T) {
	// Arrange
	created := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	userID := domain.GenerateUserID().String()
	doc := firestore.UserDoc{
		ID:            userID,
		PhoneNumber:   "+15551234567",
		PhoneVerified: true,
		DisplayName:   "Alexis",
		CreatedAt:     created,
		UpdatedAt:     created.Add(time.Hour),
	}

	// Act
	record := toUserRecord(doc)

	// Assert
	assert.Equal(t, userID, record.UserID)
	assert.Equal(t, "+15551234567", record.PhoneNumber)
	assert.Equal(t, "Alexis", record.DisplayName)
	assert.Equal(t, created, record.CreatedAt)
	assert.Equal(t, created.Add(time.Hour), record.UpdatedAt)
}

// TestSessionMappingRoundTrips is the important one. Both directions are used —
// documents come back on every refresh, records go out on every session write —
// so a field carried in only one direction would be written and never read, or
// read and never persisted.
func TestSessionMappingRoundTrips(t *testing.T) {
	// Arrange
	created := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	record := app.SessionRecord{
		SessionID:        domain.GenerateSessionID().String(),
		UserID:           domain.GenerateUserID().String(),
		DeviceID:         "device-001",
		RefreshTokenHash: "current-hash",
		PrevTokenHash:    "previous-hash",
		TokenGeneration:  7,
		CreatedAt:        created,
		ExpiresAt:        created.Add(30 * 24 * time.Hour),
	}

	// Act
	roundTripped := toSessionRecord(toSessionDoc(record))

	// Assert
	assert.Equal(t, record, roundTripped)
}

// TestToSessionDocCarriesTheRotationFields names the two fields ADR-023's
// original column list omitted and ADR-015's refresh protocol requires. Losing
// prev_token_hash would not fail anything loudly — it would quietly turn reuse
// detection into a no-op, so a stolen refresh token would be rejected as
// "invalid" instead of revoking the session it was stolen from.
func TestToSessionDocCarriesTheRotationFields(t *testing.T) {
	// Arrange
	record := app.SessionRecord{
		SessionID:       domain.GenerateSessionID().String(),
		PrevTokenHash:   "previous-hash",
		TokenGeneration: 7,
		ExpiresAt:       time.Now().Add(time.Hour),
	}

	// Act
	doc := toSessionDoc(record)

	// Assert
	assert.Equal(t, "previous-hash", doc.PrevTokenHash)
	assert.Equal(t, int64(7), doc.TokenGeneration)
}

// TestOTPConditionUsesTheConfiguredAttemptCap keeps the transaction's
// re-assertion aligned with the service's own check: a cap of zero here would
// refuse every OTP, and a cap higher than the service's would let the
// transaction wave through attempts the service had already exhausted.
func TestOTPConditionUsesTheConfiguredAttemptCap(t *testing.T) {
	// Arrange
	zone := time.FixedZone("UTC-7", -7*60*60)
	now := time.Date(2026, 7, 26, 5, 0, 0, 0, zone)

	// Act
	cond := otpCondition("phone-hash", "mac-value", now)

	// Assert
	assert.Equal(t, "phone-hash", cond.PhoneHash)
	assert.Equal(t, "mac-value", cond.OTPMAC)
	assert.Equal(t, domain.MaxOTPVerifyAttempts, cond.MaxAttempts)
	assert.Equal(t, time.UTC, cond.Now.Location(), "expiry comparisons are made in UTC")
	assert.True(t, cond.Now.Equal(now))
}
