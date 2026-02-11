package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/domain/domaintest"
)

func TestRealClock(t *testing.T) {
	t.Run("returns current time", func(t *testing.T) {
		clock := domain.RealClock{}
		before := time.Now()
		got := clock.Now()
		after := time.Now()

		assert.False(t, got.Before(before), "clock.Now() should not be before reference time")
		assert.False(t, got.After(after), "clock.Now() should not be after reference time")
	})
}

func TestFakeClock(t *testing.T) {
	fixedTime := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)

	t.Run("returns fixed time", func(t *testing.T) {
		clock := domaintest.NewFakeClock(fixedTime)
		assert.True(t, clock.Now().Equal(fixedTime))
	})

	t.Run("advance moves time forward", func(t *testing.T) {
		clock := domaintest.NewFakeClock(fixedTime)
		clock.Advance(1 * time.Hour)

		expected := fixedTime.Add(1 * time.Hour)
		assert.True(t, clock.Now().Equal(expected))
	})

	t.Run("set changes time", func(t *testing.T) {
		clock := domaintest.NewFakeClock(fixedTime)
		newTime := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
		clock.Set(newTime)

		assert.True(t, clock.Now().Equal(newTime))
	})
}

func TestNowUTCMillis(t *testing.T) {
	fixedTime := time.Date(2026, 2, 1, 10, 30, 45, 123456789, time.UTC)
	clock := domaintest.NewFakeClock(fixedTime)

	millis := domain.NowUTCMillis(clock)

	// Expected: 2026-02-01 10:30:45.123 UTC in milliseconds
	expected := fixedTime.UTC().UnixMilli()
	assert.Equal(t, expected, millis)
}

func TestFromMillis(t *testing.T) {
	t.Run("converts milliseconds to time", func(t *testing.T) {
		// 2026-02-01 10:30:45.123 UTC
		millis := int64(1769853045123)
		got := domain.FromMillis(millis)

		assert.Equal(t, millis, got.UnixMilli())
		assert.Equal(t, time.UTC, got.Location())
	})

	t.Run("round trip preserves value", func(t *testing.T) {
		fixedTime := time.Date(2026, 2, 1, 10, 30, 45, 0, time.UTC)
		clock := domaintest.NewFakeClock(fixedTime)

		millis := domain.NowUTCMillis(clock)
		restored := domain.FromMillis(millis)

		// Truncate to milliseconds for comparison
		expected := fixedTime.Truncate(time.Millisecond)
		assert.True(t, restored.Equal(expected))
	})
}
