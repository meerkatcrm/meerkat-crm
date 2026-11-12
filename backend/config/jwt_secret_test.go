package config

import "testing"

func TestWeakJWTSecretsAreReported(t *testing.T) {
	for name, secret := range map[string]string{
		".env.example":  "your-very-long-very-secret-jwt-key-change-this-in-production",
		"README":        "<paste a secret here, changing it will invalidate logins>",
		"one character": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"few different": "abcabcabcabcabcabcabcabcabcabcab12",
		"old test key":  "test-secret-key-for-e2e-testing-minimum-32-chars",
		"pattern":       "Correct-Horse7Correct-Horse7Correct-Horse7",
	} {
		if (&Config{JWTSecretKey: secret}).WeakJWTSecret() == "" {
			t.Errorf("%s: %q must give a warning", name, secret)
		}
	}
}

func TestRandomJWTSecretsAreNotReported(t *testing.T) {
	for _, secret := range []string{
		"q8Zr1VtW2nYx3K0pLm9cB5dE7fGhJ4sA6uIoPkQwErT=",
		"TESThmCqW032YZThAr7NlwUVzgSOSanJdBiaVT0PTfJX7/Rh8OAy",
	} {
		if msg := (&Config{JWTSecretKey: secret}).WeakJWTSecret(); msg != "" {
			t.Errorf("%q: unexpected warning %q", secret, msg)
		}
	}
}
