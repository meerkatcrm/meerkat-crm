package services

import (
	"meerkat/carddav"
	"meerkat/models"

	"gorm.io/gorm"
)

// replaceContactPhoto points a contact at freshly written photo files (an import's photo
// task). The old picture's files and its legacy database thumbnail go once the update
// succeeded; when it fails, the new files are deleted so nothing is left behind.
func replaceContactPhoto(db *gorm.DB, photoDir string, contactID uint, photo, thumbnail string) error {
	var old models.Contact
	// Unscoped: an import restores soft-deleted contacts, whose files still count.
	if err := db.Unscoped().Select("id", "photo", "photo_thumbnail_file").First(&old, contactID).Error; err != nil {
		carddav.RemoveContactPhotoFile(photoDir, photo, thumbnail)
		return err
	}
	// The primary key must be set on the model (not only in Where) so the save hooks can
	// stamp the ETag of the row being updated.
	contact := models.Contact{Model: gorm.Model{ID: contactID}}
	err := db.Model(&contact).Updates(map[string]interface{}{
		"photo":                photo,
		"photo_thumbnail_file": thumbnail,
		"photo_thumbnail":      "",
	}).Error
	if err != nil {
		carddav.RemoveContactPhotoFile(photoDir, photo, thumbnail)
		return err
	}
	if old.Photo != photo {
		carddav.RemoveContactPhotoFiles(photoDir, old)
	}
	return nil
}
