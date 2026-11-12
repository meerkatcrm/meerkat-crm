package config

import (
	"encoding/base64"
	"strings"
)

const (
	encryptionKeyBytes = 32
	// Random 32 bytes have about 30 different values; a typed or repeated pattern has few.
	minDistinctKeyBytes = 16
)

// EncryptionKeySteps tells the admin how to make and set ENCRYPTION_KEY.
const EncryptionKeySteps = `To fix this:
    1. Run: openssl rand -base64 32
    2. Set the output as ENCRYPTION_KEY in your .env file (or docker-compose.yml).
       Do not change JWT_SECRET_KEY. It must stay the same.
    3. Restart Meerkat. It moves the stored passwords to the new key.
    4. Keep ENCRYPTION_KEY safe with your .env and your backups. Without it, the stored
       CardDAV and calendar passwords are lost.
    See docs/deployment.md#encryption-key.`

// EncryptionKeyBytes returns the decoded ENCRYPTION_KEY, or nil when it is unset or invalid.
func (c Config) EncryptionKeyBytes() []byte {
	encoded := strings.TrimSpace(c.EncryptionKey)
	if encoded == "" || c.encryptionKeyProblem() != "" {
		return nil
	}
	key, _ := base64.StdEncoding.DecodeString(encoded)
	return key
}

func (c Config) encryptionKeyProblem() string {
	encoded := strings.TrimSpace(c.EncryptionKey)
	if encoded == "" {
		return ""
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	switch {
	case err != nil || len(key) != encryptionKeyBytes:
		return "ENCRYPTION_KEY must be 32 random bytes in base64 (44 characters).\n" + EncryptionKeySteps
	case distinctBytes(key) < minDistinctKeyBytes:
		return "ENCRYPTION_KEY is not random. Make it with openssl, not by hand.\n" + EncryptionKeySteps
	case encoded == strings.TrimSpace(c.JWTSecretKey):
		return "ENCRYPTION_KEY must not be the same as JWT_SECRET_KEY. Make a new value for ENCRYPTION_KEY.\n" + EncryptionKeySteps
	}
	return ""
}

func distinctBytes(b []byte) int {
	seen := map[byte]bool{}
	for _, v := range b {
		seen[v] = true
	}
	return len(seen)
}
