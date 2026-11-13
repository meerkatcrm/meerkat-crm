package clock

import (
	"testing"
	"time"
)

func TestFakeIsFixedUntilMoved(t *testing.T) {
	start := time.Date(2026, 12, 24, 12, 0, 0, 0, time.UTC)
	f := NewFake(start)
	if got := f.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}
	f.Advance(36 * time.Hour)
	if got, want := f.Now(), start.Add(36*time.Hour); !got.Equal(want) {
		t.Fatalf("after Advance: %v, want %v", got, want)
	}
	f.Set(start)
	if !f.Now().Equal(start) {
		t.Fatal("Set did not move the clock")
	}
}

func TestRealFollowsWallClock(t *testing.T) {
	before := time.Now()
	got := Real().Now()
	if got.Before(before) || time.Since(got) > time.Minute {
		t.Fatalf("Real().Now() = %v, not near the wall clock", got)
	}
}

func TestFromEnv(t *testing.T) {
	c, fake, err := FromEnv("")
	if err != nil || fake || c == nil {
		t.Fatalf("empty value: fake=%v err=%v", fake, err)
	}

	start := time.Date(2026, 12, 24, 12, 0, 0, 0, time.UTC)
	c, fake, err = FromEnv("2026-12-24T12:00:00Z")
	if err != nil || !fake {
		t.Fatalf("fake=%v err=%v", fake, err)
	}
	// The clock starts at the given instant and keeps running.
	if d := c.Now().Sub(start); d < 0 || d > time.Minute {
		t.Fatalf("fake clock reads %v, want just after %v", c.Now(), start)
	}

	if _, _, err = FromEnv("tomorrow"); err == nil {
		t.Fatal("an invalid FAKE_NOW must be an error")
	}
}
