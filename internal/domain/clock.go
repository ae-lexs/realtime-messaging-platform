package domain

import "time"

// Clock provides the current time. Implementations may be real (production)
// or deterministic (testing). This aligns with Clean Architecture - the domain
// defines the interface; adapters provide implementations.
type Clock interface {
	// Now returns the current time. The returned time includes both wall clock
	// and monotonic readings when using RealClock.
	Now() time.Time
}

// RealClock implements Clock using the system clock.
// It is a zero-allocation implementation (empty struct).
type RealClock struct{}

// Now returns time.Now().
func (RealClock) Now() time.Time {
	return time.Now()
}

// NowUTCMillis returns the current wall clock as UTC milliseconds since epoch.
// Use this for all persisted timestamps per TBD-PR0-3.
func NowUTCMillis(c Clock) int64 {
	return c.Now().UTC().UnixMilli()
}

// FromMillis converts epoch milliseconds to time.Time.
// The returned time has no monotonic reading (safe for serialization/comparison).
func FromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

// Ensure RealClock implements Clock at compile time.
var _ Clock = RealClock{}
