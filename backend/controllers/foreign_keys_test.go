package controllers

import (
	"fmt"
	"meerkat/config"
	"meerkat/database"
	"meerkat/models"
	"meerkat/services"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The other controller tests use AutoMigrate on a connection without foreign
// keys. These run the delete paths against the real migrated schema with
// foreign_keys=ON, as in production, so a delete that a foreign key refuses fails here.
func TestDeletePathsWithForeignKeysEnforced(t *testing.T) {
	db, err := database.InitDB(filepath.Join(t.TempDir(), "fk.db"))
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	admin := newTestUser(t, db, "admin", true)
	victim := newTestUser(t, db, "victim", false)
	a := newTestContact(t, db, victim.ID, "A")
	b := newTestContact(t, db, victim.ID, "B")
	c := newTestContact(t, db, victim.ID, "C")
	d := newTestContact(t, db, victim.ID, "D")
	for _, contact := range []models.Contact{a, b, c, d} {
		seedContactData(t, db, victim.ID, contact, a)
	}
	require.NoError(t, db.Create(&models.ContactDuplicateDismissal{UserID: victim.ID, ContactAID: c.ID, ContactBID: d.ID}).Error)
	seedUserReferencingRows(t, db, victim.ID, b)
	cfg := config.Config{ProfilePhotoDir: t.TempDir()}

	// Contact delete, while other contacts' relationships still point at it.
	userRouter := routerAs(db, victim.ID, cfg)
	userRouter.DELETE("/contacts/:id", DeleteContact)
	w := doRequest(userRouter, http.MethodDelete, fmt.Sprintf("/contacts/%d", a.ID), nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Merge, while another contact's relationship points at the merged-away source.
	require.NoError(t, db.Create(&models.Relationship{UserID: victim.ID, ContactID: b.ID, RelatedContactID: &d.ID, Name: "Sibling", Type: "family"}).Error)
	_, err = services.MergeContacts(db, victim.ID, c.ID, d.ID, nil)
	require.NoError(t, err)
	var source models.Contact
	require.NoError(t, db.Unscoped().First(&source, d.ID).Error, "the merge source is soft-deleted, which the relationships -> contacts exemption relies on")
	require.True(t, source.DeletedAt.Valid)

	// User delete hard-deletes the user row, which every user_id foreign key references.
	adminRouter := routerAs(db, admin.ID, cfg)
	adminRouter.DELETE("/users/:id", DeleteUser)
	w = doRequest(adminRouter, http.MethodDelete, fmt.Sprintf("/users/%d", victim.ID), nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for _, model := range []any{
		&models.User{}, &models.ApiToken{}, &models.Webhook{}, &models.CalendarSubscription{},
		&models.CalendarEventLink{}, &models.CardDAVConnection{}, &models.CardDAVContactLink{},
		&models.CardDAVSync{}, &models.ReminderCompletion{}, &models.EmploymentHistory{},
		&models.ContactDuplicateDismissal{},
	} {
		column := "user_id"
		if _, ok := model.(*models.User); ok {
			column = "id"
		}
		var remaining int64
		require.NoError(t, db.Unscoped().Model(model).Where(column+" = ?", victim.ID).Count(&remaining).Error)
		require.Zero(t, remaining, "%T rows of the deleted user remain", model)
	}
	var deliveries int64
	require.NoError(t, db.Model(&models.WebhookDelivery{}).Count(&deliveries).Error)
	require.Zero(t, deliveries)
}

// seedUserReferencingRows gives the user a row in every table whose foreign key references users
// (and in the tables hanging off those), so a missing ON DELETE CASCADE fails DeleteUser.
func seedUserReferencingRows(t *testing.T, db *gorm.DB, userID uint, contact models.Contact) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.Create(&models.ApiToken{UserID: userID, Name: "t", TokenHash: "hash"}).Error)
	webhook := models.Webhook{UserID: userID, Name: "w", URL: "https://example.com/hook", Events: []string{"contact.created"}, Secret: "s"}
	require.NoError(t, db.Create(&webhook).Error)
	require.NoError(t, db.Create(&models.WebhookDelivery{WebhookID: webhook.ID, EventType: "contact.created", Payload: "{}"}).Error)
	subscription := models.CalendarSubscription{UserID: userID, Name: "cal", URL: "https://example.com/cal.ics"}
	require.NoError(t, db.Create(&subscription).Error)
	activity := models.Activity{UserID: userID, Title: "event", Date: now}
	require.NoError(t, db.Create(&activity).Error)
	require.NoError(t, db.Create(&models.CalendarEventLink{SubscriptionID: subscription.ID, UserID: userID, UID: "uid", ActivityID: activity.ID, ContentHash: "h"}).Error)
	connection := models.CardDAVConnection{UserID: userID, BaseURL: "https://example.com/dav", AddressBookPath: "/ab"}
	require.NoError(t, db.Create(&connection).Error)
	require.NoError(t, db.Create(&models.CardDAVContactLink{ConnectionID: connection.ID, UserID: userID, ContactID: contact.ID, RemoteUID: "r", RemotePath: "/ab/r.vcf"}).Error)
	require.NoError(t, db.Create(&models.CardDAVSync{UserID: userID, SyncToken: "s", LastModified: now}).Error)
}

// seedContactData gives contact a row in every table that references contacts.
func seedContactData(t *testing.T, db *gorm.DB, userID uint, contact, other models.Contact) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.Create(&models.Note{UserID: userID, ContactID: &contact.ID, Content: "n", Date: now}).Error)
	reminder := models.Reminder{UserID: userID, ContactID: &contact.ID, Message: "r", RemindAt: now, Recurrence: "once"}
	require.NoError(t, db.Create(&reminder).Error)
	require.NoError(t, db.Create(&models.ReminderCompletion{UserID: userID, ReminderID: &reminder.ID, ContactID: contact.ID, Message: "r", CompletedAt: now}).Error)
	require.NoError(t, db.Create(&models.EmploymentHistory{UserID: userID, ContactID: contact.ID, Organization: "o"}).Error)
	require.NoError(t, db.Create(&models.Relationship{UserID: userID, ContactID: contact.ID, RelatedContactID: &other.ID, Name: "Friend", Type: "friend"}).Error)
	require.NoError(t, db.Create(&models.Activity{UserID: userID, Title: "t", Date: now, Contacts: []models.Contact{contact, other}}).Error)
}
