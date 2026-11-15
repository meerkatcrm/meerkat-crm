package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"meerkat/middleware"
	"meerkat/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelfContactRoundTrip covers marking a contact as "me": it is optional (null until set),
// read back through /users/me, moved rather than duplicated when another contact is marked,
// limited to the caller's own contacts, and cleared by DELETE or by deleting the contact.
func TestSelfContactRoundTrip(t *testing.T) {
	db, router := setupRouter()
	router.PUT("/self", middleware.ValidateJSONMiddleware(&models.SelfContactInput{}), SetSelfContact)
	router.DELETE("/self", ClearSelfContact)
	router.DELETE("/contacts/:id", DeleteContact)
	router.GET("/me", GetCurrentUser)

	var user models.User
	require.NoError(t, db.First(&user).Error)
	other := models.User{Username: "other", Password: "password123", Email: "other@example.com"}
	require.NoError(t, db.Create(&other).Error)

	alice := models.Contact{UserID: user.ID, Firstname: "Alice"}
	bob := models.Contact{UserID: user.ID, Firstname: "Bob"}
	foreign := models.Contact{UserID: other.ID, Firstname: "Foreign"}
	for _, c := range []*models.Contact{&alice, &bob, &foreign} {
		require.NoError(t, db.Create(c).Error)
	}

	send := func(method, body string) int {
		req, _ := http.NewRequest(method, "/self", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}
	put := func(id uint) int { return send(http.MethodPut, fmt.Sprintf(`{"contact_id":%d}`, id)) }
	// Read back through /users/me, which is how the frontend gets the mark.
	self := func() any {
		req, _ := http.NewRequest(http.MethodGet, "/me", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		v, ok := body["self_contact_id"]
		require.True(t, ok, "self_contact_id missing from /users/me")
		return v
	}

	assert.Nil(t, self(), "no contact is marked until the user chooses one")

	assert.Equal(t, http.StatusOK, put(alice.ID))
	assert.EqualValues(t, alice.ID, self())
	// Marking the same contact again (a double click) is not an error.
	assert.Equal(t, http.StatusOK, put(alice.ID))
	assert.EqualValues(t, alice.ID, self())

	// Marking another contact moves the mark: exactly one contact is "me".
	assert.Equal(t, http.StatusOK, put(bob.ID))
	assert.EqualValues(t, bob.ID, self())

	// Another user's contact, an unknown id, or a body without an id are refused and change nothing.
	assert.Equal(t, http.StatusNotFound, put(foreign.ID))
	assert.Equal(t, http.StatusNotFound, put(99999))
	assert.Equal(t, http.StatusBadRequest, send(http.MethodPut, `{}`))
	assert.Equal(t, http.StatusBadRequest, send(http.MethodPut, `{"contact_id":null}`))
	assert.EqualValues(t, bob.ID, self())

	assert.Equal(t, http.StatusOK, send(http.MethodDelete, ""))
	assert.Nil(t, self())
	var kept int64
	require.NoError(t, db.Model(&models.Contact{}).Where("id = ?", bob.ID).Count(&kept).Error)
	assert.EqualValues(t, 1, kept, "clearing the mark keeps the contact")

	// Deleting the marked contact clears the mark instead of leaving it on a deleted row.
	assert.Equal(t, http.StatusOK, put(alice.ID))
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("/contacts/%d", alice.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, self())
}

// TestContactsRandomExcludesSelfContact verifies the dashboard's "stay in touch" suggestions
// never suggest the contact marked as the user themself.
func TestContactsRandomExcludesSelfContact(t *testing.T) {
	db, router := setupRouter()
	router.GET("/contacts/random", GetContactsRandom)

	var user models.User
	require.NoError(t, db.First(&user).Error)
	me := models.Contact{UserID: user.ID, Firstname: "Me"}
	friend := models.Contact{UserID: user.ID, Firstname: "Friend"}
	require.NoError(t, db.Create(&me).Error)
	require.NoError(t, db.Create(&friend).Error)
	require.NoError(t, db.Model(&user).Update("self_contact_id", me.ID).Error)

	// Fewer contacts than the limit, so every eligible one comes back.
	req, _ := http.NewRequest(http.MethodGet, "/contacts/random", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Contacts []map[string]any `json:"contacts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	names := []any{}
	for _, c := range body.Contacts {
		names = append(names, c["firstname"])
	}
	assert.Equal(t, []any{"Friend"}, names)
}
