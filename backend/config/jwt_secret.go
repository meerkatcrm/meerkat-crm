package config

// Placeholders from the docs and old dev defaults: they are public secrets.
var placeholderJWTSecrets = map[string]bool{
	"your-very-long-very-secret-jwt-key-change-this-in-production": true,
	"<paste a secret here, changing it will invalidate logins>":    true,
	"test-secret-key-for-e2e-testing-minimum-32-chars":             true,
}

const minDistinctJWTSecretChars = 10

// WeakJWTSecret describes why JWT_SECRET_KEY looks weak, or returns "". It is only a warning:
// existing instances must keep starting.
func (c Config) WeakJWTSecret() string {
	s := c.JWTSecretKey
	switch {
	case placeholderJWTSecrets[s]:
		return "JWT_SECRET_KEY is a published example or old default value. Anyone can sign logins with it."
	case len(distinctChars(s)) < minDistinctJWTSecretChars:
		return "JWT_SECRET_KEY has very few different characters."
	case repeats(s):
		return "JWT_SECRET_KEY is a short pattern that repeats."
	}
	return ""
}

func distinctChars(s string) map[rune]bool {
	seen := map[rune]bool{}
	for _, r := range s {
		seen[r] = true
	}
	return seen
}

func repeats(s string) bool {
	for period := 1; period <= len(s)/2; period++ {
		same := true
		for i := period; i < len(s) && same; i++ {
			same = s[i] == s[i-period]
		}
		if same {
			return true
		}
	}
	return false
}
