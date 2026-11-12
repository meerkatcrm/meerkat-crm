package config

import (
	"os"
	"testing"
)

func TestBlockPrivateURLsPrecedence(t *testing.T) {
	flags := []string{"BLOCK_PRIVATE_URLS", "WEBHOOK_BLOCK_PRIVATE_URLS", "CALDAV_BLOCK_PRIVATE_URLS", "CARDDAV_BLOCK_PRIVATE_URLS", "MONICA_BLOCK_PRIVATE_URLS"}
	tests := []struct {
		name string
		env  map[string]string
		// webhook, caldav, carddav, monica
		want [4]bool
	}{
		{"nothing set: all off", nil, [4]bool{false, false, false, false}},
		{"global on", map[string]string{"BLOCK_PRIVATE_URLS": "true"}, [4]bool{true, true, true, true}},
		{"global off", map[string]string{"BLOCK_PRIVATE_URLS": "false"}, [4]bool{false, false, false, false}},
		{"global on overrides explicit off", map[string]string{"BLOCK_PRIVATE_URLS": "true", "WEBHOOK_BLOCK_PRIVATE_URLS": "false", "CALDAV_BLOCK_PRIVATE_URLS": "false", "CARDDAV_BLOCK_PRIVATE_URLS": "false", "MONICA_BLOCK_PRIVATE_URLS": "false"}, [4]bool{true, true, true, true}},
		{"global off overrides explicit on", map[string]string{"BLOCK_PRIVATE_URLS": "false", "WEBHOOK_BLOCK_PRIVATE_URLS": "true", "CALDAV_BLOCK_PRIVATE_URLS": "true", "CARDDAV_BLOCK_PRIVATE_URLS": "true", "MONICA_BLOCK_PRIVATE_URLS": "true"}, [4]bool{false, false, false, false}},
		{"old flags alone", map[string]string{"WEBHOOK_BLOCK_PRIVATE_URLS": "true", "MONICA_BLOCK_PRIVATE_URLS": "true"}, [4]bool{true, false, false, true}},
		{"carddav follows caldav", map[string]string{"CALDAV_BLOCK_PRIVATE_URLS": "true"}, [4]bool{false, true, true, false}},
		{"carddav explicit off", map[string]string{"CALDAV_BLOCK_PRIVATE_URLS": "true", "CARDDAV_BLOCK_PRIVATE_URLS": "false"}, [4]bool{false, true, false, false}},
		{"invalid global fails closed", map[string]string{"BLOCK_PRIVATE_URLS": "maybe"}, [4]bool{true, true, true, true}},
		{"invalid global beats explicit off", map[string]string{"BLOCK_PRIVATE_URLS": "maybe", "WEBHOOK_BLOCK_PRIVATE_URLS": "false"}, [4]bool{true, true, true, true}},
		{"empty global fails closed", map[string]string{"BLOCK_PRIVATE_URLS": ""}, [4]bool{true, true, true, true}},
		{"yes", map[string]string{"BLOCK_PRIVATE_URLS": "Yes"}, [4]bool{true, true, true, true}},
		{"on", map[string]string{"BLOCK_PRIVATE_URLS": "ON"}, [4]bool{true, true, true, true}},
		{"1", map[string]string{"BLOCK_PRIVATE_URLS": "1"}, [4]bool{true, true, true, true}},
		{"no", map[string]string{"BLOCK_PRIVATE_URLS": "no"}, [4]bool{false, false, false, false}},
		{"off", map[string]string{"BLOCK_PRIVATE_URLS": " Off "}, [4]bool{false, false, false, false}},
		{"0", map[string]string{"BLOCK_PRIVATE_URLS": "0"}, [4]bool{false, false, false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, f := range flags {
				t.Setenv(f, "")
				// t.Setenv registers the restore; unset so "not set" is really unset
				unsetEnv(t, f)
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			cfg := LoadConfig()
			got := [4]bool{cfg.WebhookBlockPrivateURLs, cfg.CalDAVBlockPrivateURLs, cfg.CardDAVBlockPrivateURLs, cfg.MonicaBlockPrivateURLs}
			if got != tt.want {
				t.Errorf("webhook/caldav/carddav/monica = %v, want %v", got, tt.want)
			}
			if allowed := cfg.PrivateURLsAllowed(); allowed != (tt.want != [4]bool{true, true, true, true}) {
				t.Errorf("PrivateURLsAllowed = %v", allowed)
			}
		})
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}
