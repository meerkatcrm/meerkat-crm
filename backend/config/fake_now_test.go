package config

import "testing"

func TestFakeNowMustBeRFC3339(t *testing.T) {
	has := func(c *Config) bool {
		for _, e := range c.Validate() {
			if e.Field == "FAKE_NOW" {
				return true
			}
		}
		return false
	}
	if has(&Config{}) || has(&Config{FakeNow: "2026-12-24T12:00:00Z"}) {
		t.Error("an empty or valid FAKE_NOW must pass")
	}
	if !has(&Config{FakeNow: "tomorrow"}) {
		t.Error("an invalid FAKE_NOW must be reported")
	}
}
