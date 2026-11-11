package controllers

import (
	"bytes"
	"encoding/json"
	"meerkat/middleware"
	"meerkat/models"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetRelationships(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)
	router.GET("/contacts/:id/relationships", GetRelationships)

	// Create a contact and relationships
	contact := models.Contact{
		UserID:    user.ID,
		Firstname: "Jane",
		Lastname:  "Doe",
	}
	db.Create(&contact)

	relationship1 := models.Relationship{
		UserID:           user.ID,
		Name:             "Brother",
		Type:             "Sibling",
		Gender:           "Male",
		ContactID:        contact.ID,
		RelatedContactID: nil, // Using no linked contact for this test
	}
	relationship2 := models.Relationship{
		UserID:           user.ID,
		Name:             "Sister",
		Type:             "Sibling",
		Gender:           "Female",
		ContactID:        contact.ID,
		RelatedContactID: nil,
	}
	db.Create(&relationship1)
	db.Create(&relationship2)

	req, _ := http.NewRequest("GET", "/contacts/"+strconv.Itoa(int(contact.ID))+"/relationships", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody map[string]any
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Len(t, responseBody["relationships"], 2) // Should return both relationships for the contact
}

func TestCreateRelationship(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)
	router.POST("/contacts/:id/relationships", withValidated(func() any { return &models.RelationshipInput{} }), CreateRelationship)

	// Create a contact to associate with the relationship
	contact := models.Contact{
		UserID:    user.ID,
		Firstname: "Alice",
		Lastname:  "Wonderland",
	}
	db.Create(&contact)

	// Create a new relationship
	newRelationship := models.Relationship{
		UserID: user.ID,
		Name:   "Best Friend",
		Type:   "Friendship",
		Gender: "Female",
	}

	jsonValue, _ := json.Marshal(newRelationship)
	req, _ := http.NewRequest("POST", "/contacts/"+strconv.Itoa(int(contact.ID))+"/relationships", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var responseBody map[string]any
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, newRelationship.Name, responseBody["relationship"].(map[string]any)["name"]) // Checking if the created relationship name matches
}

func TestUpdateRelationship(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)
	router.PUT("/relationships/:rid", withValidated(func() any { return &models.RelationshipInput{} }), UpdateRelationship)

	// Create a relationship to update
	existingRelationship := models.Relationship{
		UserID: user.ID,
		Name:   "Colleague",
		Type:   "Work",
		Gender: "Male",
	}
	db.Create(&existingRelationship)

	// Update the relationship
	updatedRelationship := models.Relationship{
		UserID: user.ID,
		Name:   "Close Colleague",
		Type:   "Work",
		Gender: "Male",
	}
	jsonValue, _ := json.Marshal(updatedRelationship)

	req, _ := http.NewRequest("PUT", "/relationships/"+strconv.Itoa(int(existingRelationship.ID)), bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody models.Relationship
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, updatedRelationship.Name, responseBody.Name) // Checking if the updated relationship name matches
}

func TestDeleteRelationship(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)
	router.DELETE("/relationships/:rid", DeleteRelationship)

	// Create a relationship to delete
	relationshipToDelete := models.Relationship{
		UserID: user.ID,
		Name:   "Cousin",
		Type:   "Family",
		Gender: "Female",
	}
	db.Create(&relationshipToDelete)

	// Make the request to delete the relationship
	req, _ := http.NewRequest("DELETE", "/relationships/"+strconv.Itoa(int(relationshipToDelete.ID)), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var responseBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &responseBody)
	assert.Equal(t, "Relationship deleted", responseBody["message"])

	// Verify relationship has been deleted
	var deletedRelationship models.Relationship
	result := db.First(&deletedRelationship, relationshipToDelete.ID)
	assert.Error(t, result.Error) // This should return an error as it has been deleted
}

func TestRelationshipStartAndEndDates(t *testing.T) {
	db, router := setupRouter()

	var user models.User
	db.First(&user)
	router.POST("/contacts/:id/relationships", middleware.ValidateJSONMiddleware(&models.RelationshipInput{}), CreateRelationship)
	router.PUT("/contacts/:id/relationships/:rid", middleware.ValidateJSONMiddleware(&models.RelationshipInput{}), UpdateRelationship)

	contact := models.Contact{UserID: user.ID, Firstname: "Dana"}
	db.Create(&contact)
	base := "/contacts/" + strconv.Itoa(int(contact.ID)) + "/relationships"

	send := func(method, url string, body map[string]any) *httptest.ResponseRecorder {
		jsonValue, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, url, bytes.NewBuffer(jsonValue))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// Create with both dates: persisted as sent.
	w := send("POST", base, map[string]any{"name": "Sam", "type": "Spouse", "start_date": "2010-05", "end_date": "2018"})
	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created struct {
		Relationship models.Relationship `json:"relationship"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)
	var stored models.Relationship
	db.First(&stored, created.Relationship.ID)
	assert.Equal(t, "2010-05", stored.StartDate)
	assert.Equal(t, "2018", stored.EndDate)

	// Only an end date (start unknown) is fine.
	w = send("POST", base, map[string]any{"name": "Kim", "type": "Partner", "end_date": "2019-02-03"})
	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	// A start date in the future is allowed (e.g. an upcoming wedding).
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	w = send("POST", base, map[string]any{"name": "Lee", "type": "Fiance", "start_date": future})
	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	// Rejected: end before start, end in the future, malformed date.
	for name, body := range map[string]map[string]any{
		"end before start": {"name": "X", "type": "Spouse", "start_date": "2015", "end_date": "2014-12"},
		"end in future":    {"name": "X", "type": "Spouse", "end_date": future},
		"malformed":        {"name": "X", "type": "Spouse", "end_date": "2014/12"},
	} {
		w = send("POST", base, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, name)
	}

	// Update: an unrelated edit that resends the dates keeps them; clearing works too.
	rid := strconv.Itoa(int(created.Relationship.ID))
	w = send("PUT", base+"/"+rid, map[string]any{"name": "Samuel", "type": "Spouse", "start_date": "2010-05", "end_date": "2018"})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	db.First(&stored, created.Relationship.ID)
	assert.Equal(t, "Samuel", stored.Name)
	assert.Equal(t, "2010-05", stored.StartDate)
	assert.Equal(t, "2018", stored.EndDate)

	w = send("PUT", base+"/"+rid, map[string]any{"name": "Samuel", "type": "Spouse", "start_date": "2016", "end_date": "2015"})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = send("PUT", base+"/"+rid, map[string]any{"name": "Samuel", "type": "Spouse", "start_date": "2010-05", "end_date": ""})
	assert.Equal(t, http.StatusOK, w.Code)
	db.First(&stored, created.Relationship.ID)
	assert.Equal(t, "", stored.EndDate)
}
