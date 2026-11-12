package config

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func encryptionKeyError(c *Config) string {
	for _, e := range c.Validate() {
		if e.Field == "ENCRYPTION_KEY" {
			return e.Message
		}
	}
	return ""
}

func randomLookingKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i*37 + 11)
	}
	return key
}

func TestEncryptionKeyIsOptional(t *testing.T) {
	c := &Config{}
	if msg := encryptionKeyError(c); msg != "" {
		t.Errorf("an unset ENCRYPTION_KEY must not stop the server yet: %s", msg)
	}
	if c.EncryptionKeyBytes() != nil {
		t.Error("no key bytes without ENCRYPTION_KEY")
	}
	if (&Config{EncryptionKey: " \n"}).EncryptionKeyBytes() != nil {
		t.Error("a blank ENCRYPTION_KEY is unset")
	}
}

func TestEncryptionKeyAccepts32RandomBytes(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(randomLookingKey())
	c := &Config{EncryptionKey: " " + encoded + "\n"}
	if msg := encryptionKeyError(c); msg != "" {
		t.Fatalf("a valid key was refused: %s", msg)
	}
	if !bytes.Equal(c.EncryptionKeyBytes(), randomLookingKey()) {
		t.Error("the decoded key differs")
	}
}

func TestEncryptionKeyRefusesBadValues(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(randomLookingKey()[:16])
	repeated := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7, 8}, 16))
	valid := base64.StdEncoding.EncodeToString(randomLookingKey())
	for name, c := range map[string]*Config{
		"not base64":     {EncryptionKey: "this is not base64 at all, it is a sentence!"},
		"16 bytes":       {EncryptionKey: short},
		"repeated bytes": {EncryptionKey: repeated},
		"same as JWT":    {EncryptionKey: valid, JWTSecretKey: valid},
	} {
		msg := encryptionKeyError(c)
		if msg == "" {
			t.Errorf("%s: must stop the server", name)
			continue
		}
		if !strings.Contains(msg, "openssl rand -base64 32") {
			t.Errorf("%s: the message must say how to make a key: %s", name, msg)
		}
		if c.EncryptionKeyBytes() != nil {
			t.Errorf("%s: an invalid key must not be used", name)
		}
	}
}
