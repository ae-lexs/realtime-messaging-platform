// Package domaintest provides test doubles for the domain package.
package domaintest

import (
	"sync"
	"time"

	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// FakeClock is a deterministic, advanceable clock for tests.
// Per DD_TIME_AND_CLOCKS: "Time is a dependency; inject it like any other."
// Use Advance/Set to control time progression instead of creating new
// clock instances.
type FakeClock struct {
	mu      sync.Mutex
	current time.Time
}

// NewFakeClock creates a FakeClock set to the given time.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{current: t}
}

// Now returns the fake clock's current time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// Advance moves the fake clock forward by the given duration.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// Set changes the fake clock to a specific time.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = t
}

// Ensure FakeClock implements domain.Clock at compile time.
var _ domain.Clock = (*FakeClock)(nil)
