package models

import (
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestDeleteContactCascade(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&Reminder{}, &Note{}, &Relationship{}, &Activity{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	gone := Contact{UserID: 1, Firstname: "Gone"}
	kept := Contact{UserID: 1, Firstname: "Kept"}
	foreign := Contact{UserID: 2, Firstname: "Foreign"}
	for _, c := range []*Contact{&gone, &kept, &foreign} {
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("create contact: %v", err)
		}
	}
	activity := Activity{UserID: 1, Title: "Dinner", Date: time.Now(), Contacts: []Contact{gone, kept}}
	seed := []any{
		&Reminder{UserID: 1, ContactID: &gone.ID, Message: "call", RemindAt: time.Now(), Recurrence: "once"},
		&Reminder{UserID: 1, ContactID: &kept.ID, Message: "write", RemindAt: time.Now(), Recurrence: "once"},
		&Note{UserID: 1, ContactID: &gone.ID, Content: "note", Date: time.Now()},
		&EmploymentHistory{UserID: 1, ContactID: gone.ID, Organization: "Acme"},
		&Relationship{UserID: 1, ContactID: gone.ID, Name: "Kid", Type: "child"},
		&Relationship{UserID: 1, ContactID: kept.ID, Name: "Gone", Type: "friend", RelatedContactID: &gone.ID},
		&activity,
	}
	for _, row := range seed {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	// The wrong owner deletes nothing.
	if err := db.Transaction(func(tx *gorm.DB) error { return DeleteContactCascade(tx, 2, gone.ID) }); err != nil {
		t.Fatalf("cascade as other user: %v", err)
	}
	if n := count(t, db, &Contact{}, "id = ?", gone.ID); n != 1 {
		t.Fatalf("another user's cascade deleted the contact")
	}

	if err := db.Transaction(func(tx *gorm.DB) error { return DeleteContactCascade(tx, 1, gone.ID) }); err != nil {
		t.Fatalf("cascade: %v", err)
	}

	for name, got := range map[string]int64{
		"contact":             count(t, db, &Contact{}, "id = ?", gone.ID),
		"reminders":           count(t, db, &Reminder{}, "contact_id = ?", gone.ID),
		"notes":               count(t, db, &Note{}, "contact_id = ?", gone.ID),
		"employment history":  count(t, db, &EmploymentHistory{}, "contact_id = ?", gone.ID),
		"own relationships":   count(t, db, &Relationship{}, "contact_id = ?", gone.ID),
		"incoming link":       count(t, db, &Relationship{}, "related_contact_id = ?", gone.ID),
		"activity membership": countRaw(t, db, "SELECT COUNT(*) FROM activity_contacts WHERE contact_id = ?", gone.ID),
	} {
		if got != 0 {
			t.Errorf("%s: %d rows left", name, got)
		}
	}
	for name, got := range map[string]int64{
		"other contact":                count(t, db, &Contact{}, "id = ?", kept.ID),
		"other contact's reminder":     count(t, db, &Reminder{}, "contact_id = ?", kept.ID),
		"incoming relationship itself": count(t, db, &Relationship{}, "contact_id = ? AND name = ?", kept.ID, "Gone"),
		"activity":                     count(t, db, &Activity{}, "id = ?", activity.ID),
		"other activity membership":    countRaw(t, db, "SELECT COUNT(*) FROM activity_contacts WHERE contact_id = ?", kept.ID),
	} {
		if got != 1 {
			t.Errorf("%s: want 1 row, got %d", name, got)
		}
	}
}

func count(t *testing.T, db *gorm.DB, model any, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(model).Where(query, args...).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func countRaw(t *testing.T, db *gorm.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestReleaseDeletedVCardUID(t *testing.T) {
	db := setupTestDB(t)
	if err := db.Exec("CREATE UNIQUE INDEX idx_contacts_vcard_uid_user ON contacts(user_id, vcard_uid)").Error; err != nil {
		t.Fatal(err)
	}
	deleted := Contact{UserID: 1, Firstname: "Old", VCardUID: "uid-1"}
	live := Contact{UserID: 1, Firstname: "Live", VCardUID: "uid-2"}
	for _, c := range []*Contact{&deleted, &live} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&deleted).Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Create(&Contact{UserID: 1, Firstname: "New", VCardUID: "uid-1"}).Error; err == nil {
		t.Fatal("expected the deleted contact to still hold its UID")
	}
	for _, uid := range []string{"uid-1", "uid-2"} {
		if err := ReleaseDeletedVCardUID(db, 1, uid); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&Contact{UserID: 1, Firstname: "New", VCardUID: "uid-1"}).Error; err != nil {
		t.Fatalf("UID of a deleted contact was not released: %v", err)
	}
	if n := count(t, db, &Contact{}, "vcard_uid = ?", "uid-2"); n != 1 {
		t.Errorf("a live contact's UID was touched")
	}
}
