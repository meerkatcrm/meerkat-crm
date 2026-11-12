package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"meerkat/config"
)

// credentialPrefix marks a value encrypted with ENCRYPTION_KEY; values without it use the key
// derived from JWT_SECRET_KEY (docs/deployment.md#encryption-key).
const credentialPrefix = "v2:"

// ErrNoEncryptionKey means a value needs ENCRYPTION_KEY and the server has none.
var ErrNoEncryptionKey = errors.New("ENCRYPTION_KEY is not set")

// CredentialKeys are the keys for secrets stored in the database.
type CredentialKeys struct {
	cipher []byte // nil without ENCRYPTION_KEY
	legacy []byte
}

func NewCredentialKeys(encryptionKey []byte, jwtSecret string) CredentialKeys {
	k := CredentialKeys{legacy: legacyCredentialKey(jwtSecret)}
	if len(encryptionKey) == 0 {
		return k
	}
	k.cipher = subkey(encryptionKey, "meerkat-credential-encryption")
	return k
}

func CredentialKeysOf(cfg config.Config) CredentialKeys {
	return NewCredentialKeys(cfg.EncryptionKeyBytes(), cfg.JWTSecretKey)
}

// Ready reports whether ENCRYPTION_KEY is set.
func (k CredentialKeys) Ready() bool { return k.cipher != nil }

func subkey(key []byte, info string) []byte {
	out, err := hkdf.Key(sha256.New, key, nil, info, 32)
	if err != nil {
		panic(err) // only for a length HKDF-SHA256 cannot produce
	}
	return out
}

// legacyCredentialKey is the key from before ENCRYPTION_KEY. Stored values that predate it
// stay readable with it until it is removed.
func legacyCredentialKey(jwtSecret string) []byte {
	sum := sha256.Sum256([]byte("meerkat-credential-encryption:" + jwtSecret))
	return sum[:]
}

// EncryptCredential encrypts a secret with AES-256-GCM for storage at rest, with
// ENCRYPTION_KEY when it is set. An empty plaintext encrypts to an empty string.
func EncryptCredential(k CredentialKeys, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if !k.Ready() {
		return seal(k.legacy, plaintext)
	}
	enc, err := seal(k.cipher, plaintext)
	if err != nil {
		return "", err
	}
	return credentialPrefix + enc, nil
}

// DecryptCredential reverses EncryptCredential.
func DecryptCredential(k CredentialKeys, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	rest, isNew := strings.CutPrefix(encoded, credentialPrefix)
	if !isNew {
		return open(k.legacy, encoded)
	}
	if !k.Ready() {
		return "", ErrNoEncryptionKey
	}
	return open(k.cipher, rest)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	return gcm, nil
}

func seal(key []byte, plaintext string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func open(key []byte, encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("stored credential is corrupted")
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("stored credential is corrupted")
	}
	plaintext, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("failed to decrypt stored credential (was ENCRYPTION_KEY or JWT_SECRET_KEY changed?)")
	}
	return string(plaintext), nil
}
