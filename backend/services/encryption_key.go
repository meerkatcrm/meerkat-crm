package services

import (
	"errors"
	"fmt"

	"meerkat/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const keyCheckText = "meerkat-encryption-key-check"

var (
	// ErrEncryptionKeyNeeded means the database holds values encrypted with ENCRYPTION_KEY and
	// the server has none.
	ErrEncryptionKeyNeeded = errors.New("ENCRYPTION_KEY is not set, but the database needs it")
	// ErrEncryptionKeyWrong means ENCRYPTION_KEY is not the key the database was encrypted with.
	ErrEncryptionKeyWrong = errors.New("ENCRYPTION_KEY is not the key the stored secrets were encrypted with")
)

// KeyMigration counts the stored passwords moved to ENCRYPTION_KEY, and those that could not
// be decrypted with the old key and stay as they are. Reset tells that a reset deleted the
// values of a lost key.
type KeyMigration struct {
	Moved, Failed int
	Reset         bool
}

// PrepareCredentialKeys runs at startup, before requests are served. With ENCRYPTION_KEY it
// checks that the key is the right one and encrypts the passwords from before it again. With
// reset, a key that does not fit replaces the lost one: what the lost key encrypted is deleted.
func PrepareCredentialKeys(db *gorm.DB, k CredentialKeys, reset bool) (KeyMigration, error) {
	if !k.Ready() {
		used, err := encryptionKeyUsed(db)
		if err != nil {
			return KeyMigration{}, err
		}
		if used {
			return KeyMigration{}, ErrEncryptionKeyNeeded
		}
		return KeyMigration{}, nil
	}
	var result KeyMigration
	err := checkEncryptionKey(db, k)
	if errors.Is(err, ErrEncryptionKeyWrong) && reset {
		if err = deleteLostKeyValues(db); err == nil {
			result.Reset = true
			err = checkEncryptionKey(db, k)
		}
	}
	if err != nil {
		return KeyMigration{}, err
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		for _, table := range []string{"carddav_connections", "calendar_subscriptions"} {
			if err := moveToEncryptionKey(tx, k, table, &result); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func encryptionKeyUsed(db *gorm.DB) (bool, error) {
	var n int64
	err := db.Raw(`SELECT
		(SELECT COUNT(*) FROM encryption_key_check) +
		(SELECT COUNT(*) FROM carddav_connections WHERE password_encrypted LIKE ?) +
		(SELECT COUNT(*) FROM calendar_subscriptions WHERE password_encrypted LIKE ?)`,
		credentialPrefix+"%", credentialPrefix+"%").Scan(&n).Error
	return n > 0, err
}

func checkEncryptionKey(db *gorm.DB, k CredentialKeys) error {
	value, err := EncryptCredential(k, keyCheckText)
	if err != nil {
		return err
	}
	// Of two instances that start at the same time, the first row wins and both check it.
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.EncryptionKeyCheck{ID: 1, Value: value}).Error; err != nil {
		return err
	}
	var check models.EncryptionKeyCheck
	if err := db.First(&check, 1).Error; err != nil {
		return err
	}
	if plain, err := DecryptCredential(k, check.Value); err != nil || plain != keyCheckText {
		return ErrEncryptionKeyWrong
	}
	return nil
}

func deleteLostKeyValues(db *gorm.DB) error {
	newFormat := credentialPrefix + "%"
	return db.Transaction(func(tx *gorm.DB) error {
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"UPDATE carddav_connections SET password_encrypted = '' WHERE password_encrypted LIKE ?", []any{newFormat}},
			{"UPDATE calendar_subscriptions SET password_encrypted = '' WHERE password_encrypted LIKE ?", []any{newFormat}},
			{"DELETE FROM encryption_key_check", nil},
		} {
			if err := tx.Exec(q.sql, q.args...).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func moveToEncryptionKey(tx *gorm.DB, k CredentialKeys, table string, result *KeyMigration) error {
	var rows []struct {
		ID                uint
		PasswordEncrypted string
	}
	if err := tx.Table(table).Select("id, password_encrypted").
		Where("password_encrypted <> '' AND password_encrypted NOT LIKE ?", credentialPrefix+"%").
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		plain, err := DecryptCredential(k, row.PasswordEncrypted)
		if err != nil {
			result.Failed++
			continue
		}
		enc, err := EncryptCredential(k, plain)
		if err != nil {
			return err
		}
		if err := tx.Table(table).Where("id = ?", row.ID).Update("password_encrypted", enc).Error; err != nil {
			return fmt.Errorf("%s %d: %w", table, row.ID, err)
		}
		result.Moved++
	}
	return nil
}
