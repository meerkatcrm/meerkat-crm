package services

import (
	"strings"
	"testing"

	"meerkat/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func encryptionKeyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.CardDAVConnection{}, &models.CalendarSubscription{}, &models.EncryptionKeyCheck{}))
	return db
}

func mustEncryptCredential(t *testing.T, k CredentialKeys, plain string) string {
	t.Helper()
	enc, err := EncryptCredential(k, plain)
	require.NoError(t, err)
	return enc
}

func TestPrepareCredentialKeysWithoutAKeyOnAFreshDatabase(t *testing.T) {
	db := encryptionKeyTestDB(t)
	got, err := PrepareCredentialKeys(db, legacyKeys(), false)
	require.NoError(t, err)
	assert.Zero(t, got)
}

func TestPrepareCredentialKeysMovesOldPasswordsToTheNewKey(t *testing.T) {
	db := encryptionKeyTestDB(t)
	conn := models.CardDAVConnection{UserID: 1, BaseURL: "https://dav.example.com", AddressBookPath: "/a/", PasswordEncrypted: mustEncryptCredential(t, legacyKeys(), "carddav-pw")}
	require.NoError(t, db.Create(&conn).Error)
	sub := models.CalendarSubscription{UserID: 1, Name: "c", URL: "https://cal.example.com", PasswordEncrypted: mustEncryptCredential(t, legacyKeys(), "caldav-pw")}
	require.NoError(t, db.Create(&sub).Error)
	deleted := models.CalendarSubscription{UserID: 1, Name: "d", URL: "https://cal.example.com", PasswordEncrypted: mustEncryptCredential(t, legacyKeys(), "deleted-pw")}
	require.NoError(t, db.Create(&deleted).Error)
	require.NoError(t, db.Delete(&deleted).Error)
	broken := models.CalendarSubscription{UserID: 2, Name: "b", URL: "https://cal.example.com", PasswordEncrypted: "bm90IGEgdmFsaWQgY2lwaGVydGV4dA=="}
	require.NoError(t, db.Create(&broken).Error)

	got, err := PrepareCredentialKeys(db, testKeys(), false)
	require.NoError(t, err)
	assert.Equal(t, KeyMigration{Moved: 3, Failed: 1}, got)

	read := func(enc string) string {
		require.True(t, strings.HasPrefix(enc, "v2:"), enc)
		plain, err := DecryptCredential(NewCredentialKeys(testEncryptionKey(1), "a-new-jwt-secret-after-rotation!!!"), enc)
		require.NoError(t, err)
		return plain
	}
	require.NoError(t, db.First(&conn, conn.ID).Error)
	assert.Equal(t, "carddav-pw", read(conn.PasswordEncrypted))
	require.NoError(t, db.First(&sub, sub.ID).Error)
	assert.Equal(t, "caldav-pw", read(sub.PasswordEncrypted))
	require.NoError(t, db.Unscoped().First(&deleted, deleted.ID).Error)
	assert.Equal(t, "deleted-pw", read(deleted.PasswordEncrypted))
	require.NoError(t, db.First(&broken, broken.ID).Error)
	assert.Equal(t, "bm90IGEgdmFsaWQgY2lwaGVydGV4dA==", broken.PasswordEncrypted, "a value that does not decrypt stays as it was")

	got, err = PrepareCredentialKeys(db, testKeys(), false)
	require.NoError(t, err)
	assert.Equal(t, KeyMigration{Failed: 1}, got, "the next start has nothing left to move")
}

func TestPrepareCredentialKeysRefusesAnotherKey(t *testing.T) {
	db := encryptionKeyTestDB(t)
	_, err := PrepareCredentialKeys(db, testKeys(), false)
	require.NoError(t, err)
	_, err = PrepareCredentialKeys(db, NewCredentialKeys(testEncryptionKey(2), testJWTSecret), false)
	assert.ErrorIs(t, err, ErrEncryptionKeyWrong)
}

func TestPrepareCredentialKeysRefusesToStartWithoutTheKeyOnceUsed(t *testing.T) {
	db := encryptionKeyTestDB(t)
	_, err := PrepareCredentialKeys(db, testKeys(), false)
	require.NoError(t, err)
	_, err = PrepareCredentialKeys(db, legacyKeys(), false)
	assert.ErrorIs(t, err, ErrEncryptionKeyNeeded)
}

func TestPrepareCredentialKeysRefusesToStartWithoutTheKeyWhenValuesNeedIt(t *testing.T) {
	db := encryptionKeyTestDB(t)
	conn := models.CardDAVConnection{UserID: 1, BaseURL: "https://dav.example.com", AddressBookPath: "/a/", PasswordEncrypted: mustEncryptCredential(t, testKeys(), "pw")}
	require.NoError(t, db.Create(&conn).Error)
	_, err := PrepareCredentialKeys(db, legacyKeys(), false)
	assert.ErrorIs(t, err, ErrEncryptionKeyNeeded)
}

func TestResetDeletesWhatTheLostKeyEncrypted(t *testing.T) {
	db := encryptionKeyTestDB(t)
	_, err := PrepareCredentialKeys(db, testKeys(), false)
	require.NoError(t, err)
	user := models.User{Username: "u", Email: "u@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)
	lost := models.CardDAVConnection{UserID: user.ID, BaseURL: "https://dav.example.com", AddressBookPath: "/a/", PasswordEncrypted: mustEncryptCredential(t, testKeys(), "lost-pw")}
	require.NoError(t, db.Create(&lost).Error)
	old := models.CalendarSubscription{UserID: user.ID, Name: "c", URL: "https://cal.example.com", PasswordEncrypted: mustEncryptCredential(t, legacyKeys(), "old-pw")}
	require.NoError(t, db.Create(&old).Error)

	newKeys := NewCredentialKeys(testEncryptionKey(2), testJWTSecret)
	_, err = PrepareCredentialKeys(db, newKeys, false)
	require.ErrorIs(t, err, ErrEncryptionKeyWrong, "without the reset a new key is refused")

	got, err := PrepareCredentialKeys(db, newKeys, true)
	require.NoError(t, err)
	assert.Equal(t, KeyMigration{Moved: 1, Reset: true}, got)

	require.NoError(t, db.First(&lost, lost.ID).Error)
	assert.Empty(t, lost.PasswordEncrypted)
	require.NoError(t, db.First(&old, old.ID).Error)
	plain, err := DecryptCredential(newKeys, old.PasswordEncrypted)
	require.NoError(t, err, "a password from before ENCRYPTION_KEY is still moved")
	assert.Equal(t, "old-pw", plain)

	got, err = PrepareCredentialKeys(db, newKeys, true)
	require.NoError(t, err)
	assert.Equal(t, KeyMigration{}, got, "with the right key the reset does nothing")
}

func TestResetNeedsANewKey(t *testing.T) {
	db := encryptionKeyTestDB(t)
	_, err := PrepareCredentialKeys(db, testKeys(), false)
	require.NoError(t, err)
	_, err = PrepareCredentialKeys(db, legacyKeys(), true)
	assert.ErrorIs(t, err, ErrEncryptionKeyNeeded)
}
