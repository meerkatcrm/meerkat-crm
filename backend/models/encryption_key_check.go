package models

// EncryptionKeyCheck is a fixed text encrypted with ENCRYPTION_KEY. If it does not decrypt at
// startup, the key is not the one the stored secrets were encrypted with. One row only.
type EncryptionKeyCheck struct {
	ID    uint   `gorm:"primaryKey"`
	Value string `gorm:"not null"`
}

func (EncryptionKeyCheck) TableName() string { return "encryption_key_check" }
