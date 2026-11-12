package services

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testJWTSecret = "test-jwt-secret-key-with-32-chars!!"

func testEncryptionKey(fill byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = fill + byte(i)
	}
	return key
}

func testKeys() CredentialKeys { return NewCredentialKeys(testEncryptionKey(1), testJWTSecret) }

func legacyKeys() CredentialKeys { return NewCredentialKeys(nil, testJWTSecret) }

func TestCredentialRoundTrip(t *testing.T) {
	enc, err := EncryptCredential(testKeys(), "p@ss wörd")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(enc, "v2:"), enc)
	assert.NotContains(t, enc, "p@ss")
	dec, err := DecryptCredential(testKeys(), enc)
	require.NoError(t, err)
	assert.Equal(t, "p@ss wörd", dec)
}

func TestCredentialWithoutEncryptionKeyUsesTheOldFormat(t *testing.T) {
	enc, err := EncryptCredential(legacyKeys(), "pw")
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(enc, "v2:"), enc)
	dec, err := DecryptCredential(legacyKeys(), enc)
	require.NoError(t, err)
	assert.Equal(t, "pw", dec)

	dec, err = DecryptCredential(testKeys(), enc)
	require.NoError(t, err, "an old value stays readable once ENCRYPTION_KEY is set")
	assert.Equal(t, "pw", dec)
}

func TestCredentialNewFormatNeedsTheEncryptionKey(t *testing.T) {
	enc, err := EncryptCredential(testKeys(), "pw")
	require.NoError(t, err)
	_, err = DecryptCredential(legacyKeys(), enc)
	assert.ErrorIs(t, err, ErrNoEncryptionKey)
}

func TestCredentialEmptyStaysEmpty(t *testing.T) {
	enc, err := EncryptCredential(testKeys(), "")
	require.NoError(t, err)
	assert.Empty(t, enc)
	dec, err := DecryptCredential(testKeys(), "")
	require.NoError(t, err)
	assert.Empty(t, dec)
}

func TestCredentialEncryptionIsRandomised(t *testing.T) {
	a, err := EncryptCredential(testKeys(), "same")
	require.NoError(t, err)
	b, err := EncryptCredential(testKeys(), "same")
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "a fresh nonce per encryption")
}

func TestCredentialWrongKeyFails(t *testing.T) {
	enc, err := EncryptCredential(testKeys(), "pw")
	require.NoError(t, err)
	_, err = DecryptCredential(NewCredentialKeys(testEncryptionKey(2), testJWTSecret), enc)
	assert.Error(t, err)

	old, err := EncryptCredential(legacyKeys(), "pw")
	require.NoError(t, err)
	_, err = DecryptCredential(NewCredentialKeys(testEncryptionKey(1), "another-jwt-secret-with-32-chars!!"), old)
	assert.Error(t, err)
}

func TestCredentialDoesNotDependOnTheJWTSecret(t *testing.T) {
	enc, err := EncryptCredential(testKeys(), "pw")
	require.NoError(t, err)
	dec, err := DecryptCredential(NewCredentialKeys(testEncryptionKey(1), "a-new-jwt-secret-after-rotation!!!"), enc)
	require.NoError(t, err)
	assert.Equal(t, "pw", dec)
}

func TestCredentialTamperedOrCorruptedFails(t *testing.T) {
	enc, err := EncryptCredential(testKeys(), "pw")
	require.NoError(t, err)
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, "v2:"))
	require.NoError(t, err)

	flipped := append([]byte{}, raw...)
	flipped[len(flipped)-1] ^= 1
	_, err = DecryptCredential(testKeys(), "v2:"+base64.StdEncoding.EncodeToString(flipped))
	assert.Error(t, err, "a changed ciphertext must fail authentication")

	_, err = DecryptCredential(testKeys(), "v2:"+base64.StdEncoding.EncodeToString(raw[:5]))
	assert.Error(t, err, "shorter than a nonce")
	_, err = DecryptCredential(testKeys(), "v2:not base64 !!")
	assert.Error(t, err)
}

func TestCredentialSubkeyIsDerived(t *testing.T) {
	k := testKeys()
	assert.NotEqual(t, testEncryptionKey(1), k.cipher, "the raw key is not used directly")
	assert.Len(t, k.cipher, 32)
	assert.True(t, k.Ready())
	assert.False(t, legacyKeys().Ready())
}
