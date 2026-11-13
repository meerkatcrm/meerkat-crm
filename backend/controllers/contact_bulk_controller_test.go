package controllers

import (
	"bytes"
	"encoding/json"
	"meerkat/middleware"
	"meerkat/models"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupBulkRouter(t *testing.T) (*gorm.DB, *gin.Engine, models.User) {
	t.Helper()
	db, router := setupRouter()
	var user models.User
	require.NoError(t, db.First(&user).Error)
	router.POST("/contacts/bulk-update", middleware.ValidateJSONMiddleware(&models.ContactBulkUpdateInput{}), BulkUpdateContacts)
	return db, router, user
}

func postBulk(router *gin.Engine, body any) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/contacts/bulk-update", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestBulkUpdateContacts_CirclesAndFields(t *testing.T) {
	db, router, user := setupBulkRouter(t)

	alice := models.Contact{UserID: user.ID, Firstname: "Alice", Circles: []string{"friends", "old"}, FoodPreference: "vegan"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob", Note: "keep me",
		Addresses: []models.ContactAddress{{Type: "work", Street: "1 Main St", City: "Berlin"}}}
	carol := models.Contact{UserID: user.ID, Firstname: "Carol", Address: "Legacy street 5"}
	untouched := models.Contact{UserID: user.ID, Firstname: "Dave", Circles: []string{"old"}}
	for _, ct := range []*models.Contact{&alice, &bob, &carol, &untouched} {
		require.NoError(t, db.Create(ct).Error)
	}

	w := postBulk(router, gin.H{
		"contact_ids":     []uint{alice.ID, bob.ID, carol.ID, alice.ID},
		"add_circles":     []string{" work ", "friends"},
		"remove_circles":  []string{"old"},
		"food_preference": "  no nuts ",
		"organization":    "Acme",
		"city":            "Hamburg",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(3), resp["updated"])

	got := loadContact(t, db, alice.ID)
	assert.Equal(t, []string{"friends", "work"}, got.Circles)
	assert.Equal(t, "no nuts", got.FoodPreference)
	assert.Equal(t, "Acme", got.Organization)
	assert.Equal(t, []models.ContactAddress{{Type: "home", City: "Hamburg"}}, got.Addresses)

	got = loadContact(t, db, bob.ID)
	assert.Equal(t, []string{"work", "friends"}, got.Circles)
	assert.Equal(t, "keep me", got.Note, "fields not in the request are kept")
	assert.Equal(t, []models.ContactAddress{{Type: "work", Street: "1 Main St", City: "Hamburg"}}, got.Addresses)
	assert.Equal(t, "1 Main St, Hamburg", got.Address)

	got = loadContact(t, db, carol.ID)
	assert.Equal(t, []models.ContactAddress{{Type: "home", Street: "Legacy street 5", City: "Hamburg"}}, got.Addresses)

	// The organization reaches the employment history like a single save does
	var jobs []models.EmploymentHistory
	require.NoError(t, db.Where("contact_id = ?", alice.ID).Find(&jobs).Error)
	require.Len(t, jobs, 1)
	assert.Equal(t, "Acme", jobs[0].Organization)

	got = loadContact(t, db, untouched.ID)
	assert.Equal(t, []string{"old"}, got.Circles)
	assert.Empty(t, got.FoodPreference)
}

func TestBulkUpdateContacts_ArchiveDropsReminders(t *testing.T) {
	db, router, user := setupBulkRouter(t)

	a := models.Contact{UserID: user.ID, Firstname: "A"}
	b := models.Contact{UserID: user.ID, Firstname: "B"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	contactID := a.ID
	require.NoError(t, db.Create(&models.Reminder{UserID: user.ID, ContactID: &contactID, Message: "call", RemindAt: time.Now().Add(time.Hour), Recurrence: "once"}).Error)

	w := postBulk(router, gin.H{"contact_ids": []uint{a.ID, b.ID}, "archived": true})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var archived int64
	db.Model(&models.Contact{}).Where("archived = ?", true).Count(&archived)
	assert.Equal(t, int64(2), archived)
	var reminders int64
	db.Model(&models.Reminder{}).Where("contact_id = ?", a.ID).Count(&reminders)
	assert.Zero(t, reminders)

	w = postBulk(router, gin.H{"contact_ids": []uint{a.ID}, "archived": false})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := loadContact(t, db, a.ID)
	assert.False(t, got.Archived)
}

func TestBulkUpdateContacts_OtherUsersContactChangesNothing(t *testing.T) {
	db, router, user := setupBulkRouter(t)

	other := models.User{Username: "other", Password: "password123", Email: "other@example.com"}
	require.NoError(t, db.Create(&other).Error)
	mine := models.Contact{UserID: user.ID, Firstname: "Mine"}
	theirs := models.Contact{UserID: other.ID, Firstname: "Theirs"}
	require.NoError(t, db.Create(&mine).Error)
	require.NoError(t, db.Create(&theirs).Error)

	w := postBulk(router, gin.H{"contact_ids": []uint{mine.ID, theirs.ID}, "food_preference": "pizza"})
	assert.Equal(t, http.StatusNotFound, w.Code)

	got := loadContact(t, db, mine.ID)
	assert.Empty(t, got.FoodPreference, "all or nothing")
	got = loadContact(t, db, theirs.ID)
	assert.Empty(t, got.FoodPreference)
}

func TestBulkUpdateContacts_RejectsBadInput(t *testing.T) {
	db, router, user := setupBulkRouter(t)
	ct := models.Contact{UserID: user.ID, Firstname: "A"}
	require.NoError(t, db.Create(&ct).Error)

	cases := map[string]gin.H{
		"no contacts":          {"contact_ids": []uint{}, "food_preference": "x"},
		"no change":            {"contact_ids": []uint{ct.ID}},
		"blank field":          {"contact_ids": []uint{ct.ID}, "city": "   "},
		"add and remove":       {"contact_ids": []uint{ct.ID}, "add_circles": []string{"a"}, "remove_circles": []string{"a"}},
		"blank circles only":   {"contact_ids": []uint{ct.ID}, "add_circles": []string{" "}},
		"field over max":       {"contact_ids": []uint{ct.ID}, "role": string(make([]byte, 201))},
		"too many contacts":    {"contact_ids": slices.Repeat([]uint{ct.ID}, models.MaxBulkContacts+1), "city": "x"},
		"circle name too long": {"contact_ids": []uint{ct.ID}, "add_circles": []string{string(bytes.Repeat([]byte("a"), 101))}},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			w := postBulk(router, body)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}

func loadContact(t *testing.T, db *gorm.DB, id uint) models.Contact {
	t.Helper()
	var ct models.Contact
	require.NoError(t, db.First(&ct, id).Error)
	return ct
}

func TestBulkUpdateContacts_UnchangedContactsAreNotSaved(t *testing.T) {
	db, router, user := setupBulkRouter(t)

	active := models.Contact{UserID: user.ID, Firstname: "Active", Circles: []string{"friends"}}
	archived := models.Contact{UserID: user.ID, Firstname: "Archived", Archived: true}
	require.NoError(t, db.Create(&active).Error)
	require.NoError(t, db.Create(&archived).Error)
	etag := loadContact(t, db, active.ID).ETag

	w := postBulk(router, gin.H{"contact_ids": []uint{active.ID, archived.ID}, "archived": false, "add_circles": []string{"friends"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(1), resp["updated"], "only the archived contact changes")

	assert.Equal(t, etag, loadContact(t, db, active.ID).ETag, "an unchanged contact keeps its ETag")
	got := loadContact(t, db, archived.ID)
	assert.False(t, got.Archived)
	assert.Equal(t, []string{"friends"}, got.Circles)
}
