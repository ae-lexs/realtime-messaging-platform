package domain_test

import (
	"testing"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPhoneNumber(t *testing.T) {
	t.Run("valid E.164 numbers", func(t *testing.T) {
		valid := []string{
			"+14155552671",     // US
			"+447911123456",    // UK
			"+8613800138000",   // China
			"+1234567",         // Minimum 7 digits
			"+123456789012345", // Maximum 15 digits
		}
		for _, raw := range valid {
			p, err := domain.NewPhoneNumber(raw)
			require.NoError(t, err, "expected %q to be valid", raw)
			assert.Equal(t, raw, p.String())
			assert.False(t, p.IsZero())
		}
	})

	t.Run("empty string returns error", func(t *testing.T) {
		_, err := domain.NewPhoneNumber("")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidPhoneNumber)
	})

	t.Run("missing plus prefix", func(t *testing.T) {
		_, err := domain.NewPhoneNumber("14155552671")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidPhoneNumber)
	})

	t.Run("leading zero after country code", func(t *testing.T) {
		_, err := domain.NewPhoneNumber("+0123456789")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidPhoneNumber)
	})

	t.Run("too short", func(t *testing.T) {
		_, err := domain.NewPhoneNumber("+123456") // 6 digits, need 7
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidPhoneNumber)
	})

	t.Run("too long", func(t *testing.T) {
		_, err := domain.NewPhoneNumber("+1234567890123456") // 16 digits, max 15
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidPhoneNumber)
	})

	t.Run("contains letters", func(t *testing.T) {
		_, err := domain.NewPhoneNumber("+1415555ABCD")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidPhoneNumber)
	})

	t.Run("contains spaces", func(t *testing.T) {
		_, err := domain.NewPhoneNumber("+1 415 555 2671")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrInvalidPhoneNumber)
	})

	t.Run("zero value is zero", func(t *testing.T) {
		var p domain.PhoneNumber
		assert.True(t, p.IsZero())
		assert.Empty(t, p.String())
	})

	t.Run("MustPhoneNumber panics on invalid", func(t *testing.T) {
		assert.Panics(t, func() {
			domain.MustPhoneNumber("invalid")
		})
	})

	t.Run("MustPhoneNumber succeeds on valid", func(t *testing.T) {
		assert.NotPanics(t, func() {
			p := domain.MustPhoneNumber("+14155552671")
			assert.Equal(t, "+14155552671", p.String())
		})
	})
}
