// Package clock is the only place that reads the wall clock. Business code gets "now"
// as a value (taken once per request by middleware, once per job by the scheduler)
// instead of calling time.Now(); see docs/development/backend.md#time.
package clock

import (
	"fmt"
	"sync"
	"time"
)

// ContextKey is where the request middleware stores the time of the request in the
// gin context.
const ContextKey = "now"

// ClockKey is where the middleware stores the Clock itself.
const ClockKey = "clock"

// Getter is the part of *gin.Context the request helpers need (kept as an interface so
// this package does not depend on gin).
type Getter interface {
	Get(key any) (any, bool)
}

// RequestTime returns the time the request middleware stored under ContextKey. Without
// it (a bare test context) it reads the wall clock.
func RequestTime(c Getter) time.Time {
	if v, ok := c.Get(ContextKey); ok {
		if t, ok := v.(time.Time); ok {
			return t
		}
	}
	return time.Now()
}

// Clock tells the current time.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Real returns the wall clock.
func Real() Clock { return realClock{} }

// offsetClock starts at a chosen instant and then runs at normal speed.
type offsetClock struct{ offset time.Duration }

func (c offsetClock) Now() time.Time { return time.Now().Add(c.offset) }

// FromEnv builds the clock for the FAKE_NOW setting: empty means the wall clock, an
// RFC 3339 instant means a clock that starts there and keeps running. The bool reports
// whether the clock is fake, so the caller can log a warning.
func FromEnv(fakeNow string) (Clock, bool, error) {
	if fakeNow == "" {
		return Real(), false, nil
	}
	t, err := time.Parse(time.RFC3339, fakeNow)
	if err != nil {
		return nil, false, fmt.Errorf("FAKE_NOW must be an RFC 3339 time such as 2026-12-24T12:00:00Z: %w", err)
	}
	return offsetClock{offset: time.Until(t)}, true, nil
}

// Fake is a clock for tests that only moves when told to.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake set to t.
func NewFake(t time.Time) *Fake { return &Fake{now: t} }

// Now returns the current fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Set moves the clock to t.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

// Advance moves the clock forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
