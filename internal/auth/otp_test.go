package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aelexs/realtime-messaging-platform/internal/auth"
)

func TestGenerateOTP(t *testing.T) {
	t.Run("produces 6-digit string", func(t *testing.T) {
		otp, err := auth.GenerateOTP()
		require.NoError(t, err)
		assert.Len(t, otp, 6)
		for _, ch := range otp {
			assert.True(t, ch >= '0' && ch <= '9', "expected digit, got %c", ch)
		}
	})

	t.Run("produces different values", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			otp, err := auth.GenerateOTP()
			require.NoError(t, err)
			seen[otp] = true
		}
		assert.Greater(t, len(seen), 90, "expected at least 90 unique OTPs from 100 draws")
	})

	t.Run("matches 6-digit pattern", func(t *testing.T) {
		otp, err := auth.GenerateOTP()
		require.NoError(t, err)
		assert.Regexp(t, `^\d{6}$`, otp)
	})
}

func TestHashPhone(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		h1 := auth.HashPhone("+14155552671")
		h2 := auth.HashPhone("+14155552671")
		assert.Equal(t, h1, h2)
	})

	t.Run("different phones produce different hashes", func(t *testing.T) {
		h1 := auth.HashPhone("+14155552671")
		h2 := auth.HashPhone("+447911123456")
		assert.NotEqual(t, h1, h2)
	})

	t.Run("produces 64-char hex SHA-256", func(t *testing.T) {
		h := auth.HashPhone("+14155552671")
		assert.Len(t, h, 64)
	})
}

var (
	macExpiry      = time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)
	macOtherExpiry = time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC)
)

func TestComputeOTPMAC(t *testing.T) {
	pepper := []byte("test-pepper-32-bytes-long-secret")

	t.Run("deterministic with same inputs", func(t *testing.T) {
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macExpiry)
		mac2 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macExpiry)
		assert.Equal(t, mac1, mac2)
	})

	t.Run("different OTP changes MAC", func(t *testing.T) {
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macExpiry)
		mac2 := auth.ComputeOTPMAC(pepper, "654321", "phonehash", macExpiry)
		assert.NotEqual(t, mac1, mac2)
	})

	t.Run("different phone hash changes MAC", func(t *testing.T) {
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash1", macExpiry)
		mac2 := auth.ComputeOTPMAC(pepper, "123456", "phonehash2", macExpiry)
		assert.NotEqual(t, mac1, mac2)
	})

	t.Run("different expiry changes MAC", func(t *testing.T) {
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macExpiry)
		mac2 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macOtherExpiry)
		assert.NotEqual(t, mac1, mac2)
	})

	t.Run("different pepper changes MAC", func(t *testing.T) {
		pepper2 := []byte("another-pepper-32-bytes-long-sec")
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macExpiry)
		mac2 := auth.ComputeOTPMAC(pepper2, "123456", "phonehash", macExpiry)
		assert.NotEqual(t, mac1, mac2)
	})

	t.Run("produces 64-char hex HMAC-SHA256", func(t *testing.T) {
		mac := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macExpiry)
		assert.Len(t, mac, 64)
	})

	// The renderings below all denote the same instant. If any of them
	// produced a different MAC, an OTP would verify before it was stored and
	// fail afterwards — Firestore keeps microseconds, so nanoseconds do not
	// survive the round-trip, and a clock in another zone renders a different
	// RFC3339 offset. auth.OTPMACTime normalises all three away.
	t.Run("sub-second precision does not change the MAC", func(t *testing.T) {
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macExpiry)
		mac2 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macExpiry.Add(123456789*time.Nanosecond))
		assert.Equal(t, mac1, mac2)
	})

	t.Run("microsecond truncation does not change the MAC", func(t *testing.T) {
		withNanos := macExpiry.Add(999999999 * time.Nanosecond)

		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", withNanos)
		mac2 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", withNanos.Truncate(time.Microsecond))
		assert.Equal(t, mac1, mac2)
	})

	t.Run("location does not change the MAC", func(t *testing.T) {
		elsewhere := macExpiry.In(time.FixedZone("UTC-7", -7*60*60))

		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macExpiry)
		mac2 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", elsewhere)
		assert.Equal(t, mac1, mac2)
	})
}

func TestOTPMACTime(t *testing.T) {
	// Arrange — sub-second precision, in a zone seven hours behind.
	zone := time.FixedZone("UTC-7", -7*60*60)
	stamp := time.Date(2026, 1, 1, 5, 4, 59, 999999999, zone)

	// Act + Assert
	assert.Equal(t, "2026-01-01T12:04:59Z", auth.OTPMACTime(stamp))
}

func TestVerifyOTPMAC(t *testing.T) {
	pepper := []byte("test-pepper-32-bytes-long-secret")
	storedMAC := auth.ComputeOTPMAC(pepper, "123456", "phonehash", macExpiry)

	t.Run("correct OTP verifies", func(t *testing.T) {
		assert.True(t, auth.VerifyOTPMAC(pepper, "123456", "phonehash", macExpiry, storedMAC))
	})

	t.Run("wrong OTP rejects", func(t *testing.T) {
		assert.False(t, auth.VerifyOTPMAC(pepper, "654321", "phonehash", macExpiry, storedMAC))
	})

	t.Run("wrong phone rejects", func(t *testing.T) {
		assert.False(t, auth.VerifyOTPMAC(pepper, "123456", "wronghash", macExpiry, storedMAC))
	})

	t.Run("wrong expiry rejects", func(t *testing.T) {
		assert.False(t, auth.VerifyOTPMAC(pepper, "123456", "phonehash", macOtherExpiry, storedMAC))
	})

	t.Run("wrong pepper rejects", func(t *testing.T) {
		wrongPepper := []byte("wrong-pepper-32-bytes-long-secr!")
		assert.False(t, auth.VerifyOTPMAC(wrongPepper, "123456", "phonehash", macExpiry, storedMAC))
	})
}
