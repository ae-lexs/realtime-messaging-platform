package auth_test

import (
	"testing"

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

func TestComputeOTPMAC(t *testing.T) {
	pepper := []byte("test-pepper-32-bytes-long-secret")

	t.Run("deterministic with same inputs", func(t *testing.T) {
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", "2026-01-01T00:05:00Z")
		mac2 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", "2026-01-01T00:05:00Z")
		assert.Equal(t, mac1, mac2)
	})

	t.Run("different OTP changes MAC", func(t *testing.T) {
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", "2026-01-01T00:05:00Z")
		mac2 := auth.ComputeOTPMAC(pepper, "654321", "phonehash", "2026-01-01T00:05:00Z")
		assert.NotEqual(t, mac1, mac2)
	})

	t.Run("different phone hash changes MAC", func(t *testing.T) {
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash1", "2026-01-01T00:05:00Z")
		mac2 := auth.ComputeOTPMAC(pepper, "123456", "phonehash2", "2026-01-01T00:05:00Z")
		assert.NotEqual(t, mac1, mac2)
	})

	t.Run("different expiry changes MAC", func(t *testing.T) {
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", "2026-01-01T00:05:00Z")
		mac2 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", "2026-01-01T00:10:00Z")
		assert.NotEqual(t, mac1, mac2)
	})

	t.Run("different pepper changes MAC", func(t *testing.T) {
		pepper2 := []byte("another-pepper-32-bytes-long-sec")
		mac1 := auth.ComputeOTPMAC(pepper, "123456", "phonehash", "2026-01-01T00:05:00Z")
		mac2 := auth.ComputeOTPMAC(pepper2, "123456", "phonehash", "2026-01-01T00:05:00Z")
		assert.NotEqual(t, mac1, mac2)
	})

	t.Run("produces 64-char hex HMAC-SHA256", func(t *testing.T) {
		mac := auth.ComputeOTPMAC(pepper, "123456", "phonehash", "2026-01-01T00:05:00Z")
		assert.Len(t, mac, 64)
	})
}

func TestVerifyOTPMAC(t *testing.T) {
	pepper := []byte("test-pepper-32-bytes-long-secret")
	storedMAC := auth.ComputeOTPMAC(pepper, "123456", "phonehash", "2026-01-01T00:05:00Z")

	t.Run("correct OTP verifies", func(t *testing.T) {
		assert.True(t, auth.VerifyOTPMAC(pepper, "123456", "phonehash", "2026-01-01T00:05:00Z", storedMAC))
	})

	t.Run("wrong OTP rejects", func(t *testing.T) {
		assert.False(t, auth.VerifyOTPMAC(pepper, "654321", "phonehash", "2026-01-01T00:05:00Z", storedMAC))
	})

	t.Run("wrong phone rejects", func(t *testing.T) {
		assert.False(t, auth.VerifyOTPMAC(pepper, "123456", "wronghash", "2026-01-01T00:05:00Z", storedMAC))
	})

	t.Run("wrong expiry rejects", func(t *testing.T) {
		assert.False(t, auth.VerifyOTPMAC(pepper, "123456", "phonehash", "2026-12-31T00:00:00Z", storedMAC))
	})

	t.Run("wrong pepper rejects", func(t *testing.T) {
		wrongPepper := []byte("wrong-pepper-32-bytes-long-secr!")
		assert.False(t, auth.VerifyOTPMAC(wrongPepper, "123456", "phonehash", "2026-01-01T00:05:00Z", storedMAC))
	})
}
