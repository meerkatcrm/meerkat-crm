package services

import (
	"meerkat/models"

	"gorm.io/gorm"
)

// PrivateURLWarning reports whether private addresses are reachable from outbound
// features (webhooks, calendar and contact sync, Monica import) on an instance with more
// than one user, where one user could point them at the server's internal network.
func PrivateURLWarning(db *gorm.DB, privateURLsAllowed bool) (warn bool, users int64, err error) {
	if err := db.Model(&models.User{}).Count(&users).Error; err != nil {
		return false, 0, err
	}
	return privateURLsAllowed && users > 1, users, nil
}
