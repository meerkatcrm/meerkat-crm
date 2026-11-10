package models

import "gorm.io/gorm"

// DeleteContactCascade soft-deletes a contact together with everything that
// hangs off it. Soft deletes do not trigger the database's ON DELETE CASCADE, so
// every path that removes a contact (API, CardDAV, remote sync) must go through
// here; otherwise its reminders keep being emailed and its relationships'
// birthdays keep showing up. Call it inside a transaction.
func DeleteContactCascade(tx *gorm.DB, userID, contactID uint) error {
	for _, child := range []any{&Reminder{}, &Note{}, &Relationship{}, &EmploymentHistory{}} {
		if err := tx.Where("contact_id = ? AND user_id = ?", contactID, userID).Delete(child).Error; err != nil {
			return err
		}
	}

	// Relationships of other contacts that point at this one keep their own
	// name and type, but must not link to a contact that no longer exists.
	if err := tx.Model(&Relationship{}).
		Where("related_contact_id = ? AND user_id = ?", contactID, userID).
		Update("related_contact_id", nil).Error; err != nil {
		return err
	}

	// Activities are shared with other contacts: only this contact's link goes.
	if err := tx.Exec("DELETE FROM activity_contacts WHERE contact_id = ? AND activity_id IN (SELECT id FROM activities WHERE user_id = ?)", contactID, userID).Error; err != nil {
		return err
	}

	return tx.Where("user_id = ?", userID).Delete(&Contact{}, contactID).Error
}
