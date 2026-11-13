package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"meerkat/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFTSMatch(t *testing.T) {
	for in, want := range map[string]string{
		"zoe":              `"zoe"*`,
		"  Zoë   Müller ":  `"Zoë"* "Müller"*`,
		"john-smith":       `"john"* "smith"*`,
		"zoe@work.example": `"zoe"* "work"* "example"*`,
		`o"brien`:          `"o"* "brien"*`,
		"AND OR NOT NEAR":  `"AND"* "OR"* "NOT"* "NEAR"*`,
		"col:val*":         `"col"* "val"*`,
		"room 12":          `"room"* "12"*`,
		"+41 anna":         `"41"* "anna"*`,
	} {
		got, ok := ftsMatch(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	// LIKE handles these: no words, phone-like digits, scripts without spaces
	for _, in := range []string{"", "   ", "+", "-*", `""`, "%_",
		"795550", "+41 79", "(030) 1234", "555-0101", "1.2.3",
		"山田", "やまだ", "ヤマダ", "김민수", "Yamada 山田"} {
		_, ok := ftsMatch(in)
		assert.False(t, ok, in)
	}
}

func TestFTSMatchCapsLongInput(t *testing.T) {
	got, _ := ftsMatch(strings.Repeat("ab ", 100))
	assert.Equal(t, maxSearchWords, strings.Count(got, `"*`))
	got, _ = ftsMatch(strings.Repeat("é", 300))
	assert.Equal(t, `"`+strings.Repeat("é", maxSearchRunes)+`"*`, got, "cut by character, not byte")
}

// searchRouter is setupRouter with the three searchable lists mounted.
func searchRouter(t *testing.T) (*gorm.DB, *gin.Engine, models.User) {
	t.Helper()
	db, router := setupRouter()
	var user models.User
	require.NoError(t, db.First(&user).Error)
	router.GET("/contacts", GetContacts)
	router.GET("/activities", GetActivities)
	mountNotes(router)
	return db, router, user
}

func getJSON(t *testing.T, router *gin.Engine, path string, params url.Values) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", path+"?"+params.Encode(), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func contactField(out map[string]any, field string) []string {
	var values []string
	for _, c := range out["contacts"].([]any) {
		values = append(values, c.(map[string]any)[field].(string))
	}
	return values
}

func TestContactSearchFoldsAccentsAndMatchesWordPrefixes(t *testing.T) {
	db, r, u := searchRouter(t)
	require.NoError(t, db.Create(&[]models.Contact{
		{UserID: u.ID, Firstname: "Zoë", Lastname: "Müller", Emails: []models.ContactEmail{{Type: "home", Value: "zoe@work.example"}}},
		{UserID: u.ID, Firstname: "Bob", Lastname: "Smith", Phones: []models.ContactPhone{{Type: "mobile", Value: "+41 79 555 0101"}}},
		{UserID: u.ID, Firstname: "José", Lastname: "García", Nickname: "Pepe"},
	}).Error)

	for term, want := range map[string]string{
		"zoe": "Zoë", "ZOË": "Zoë", "mull": "Zoë", "zoe muller": "Zoë", "muller zoe": "Zoë",
		"work.exa": "Zoë", "555": "Bob", "0101": "Bob", "jose": "José", "garcia": "José", "pepe": "José",
	} {
		out := getJSON(t, r, "/contacts", url.Values{"search": {term}})
		assert.Equal(t, []string{want}, contactField(out, "firstname"), term)
		assert.EqualValues(t, 1, out["total"], term)
	}
	assert.Empty(t, contactField(getJSON(t, r, "/contacts", url.Values{"search": {"zoe smith"}}), "firstname"), "every word must match")
	assert.Empty(t, contactField(getJSON(t, r, "/contacts", url.Values{"search": {"ller"}}), "firstname"), "no match in the middle of a word")
}

func TestContactSearchKeepsSubstringMatchingForPhonesAndCJK(t *testing.T) {
	db, r, u := searchRouter(t)
	require.NoError(t, db.Create(&[]models.Contact{
		{UserID: u.ID, Firstname: "Plain", Phones: []models.ContactPhone{{Value: "+41795550101"}}},
		{UserID: u.ID, Firstname: "Spaced", Phones: []models.ContactPhone{{Value: "030 1234567"}}},
		{UserID: u.ID, Firstname: "太郎", Lastname: "山田"},
		{UserID: u.ID, Firstname: "민수", Lastname: "김"},
	}).Error)
	for term, want := range map[string]string{
		"795550": "Plain", "+4179": "Plain", "1234567": "Spaced", "030 123": "Spaced",
		"山田": "太郎", "太": "太郎", "민수": "민수",
	} {
		assert.Equal(t, []string{want}, contactField(getJSON(t, r, "/contacts", url.Values{"search": {term}}), "firstname"), term)
	}
}

func TestContactSearchSurvivesFTSSyntax(t *testing.T) {
	db, r, u := searchRouter(t)
	require.NoError(t, db.Create(&models.Contact{UserID: u.ID, Firstname: "Pat", Lastname: "O'Brien"}).Error)
	for _, term := range []string{`"`, `a"b`, "AND", "OR NOT", "pat*", "(pat)", "col:pat", "-", "'", "pat OR", "NEAR(pat", strings.Repeat("pat ", 500)} {
		getJSON(t, r, "/contacts", url.Values{"search": {term}}) // must not be a 500
	}
	assert.Equal(t, []string{"Pat"}, contactField(getJSON(t, r, "/contacts", url.Values{"search": {"o'brien"}}), "firstname"))
}

func TestContactSearchIsScopedAndSkipsDeletedAndArchived(t *testing.T) {
	db, r, u := searchRouter(t)
	other := models.User{Username: "other", Password: "password123", Email: "other@example.com"}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&[]models.Contact{
		{UserID: u.ID, Firstname: "Mine"},
		{UserID: other.ID, Firstname: "Mine"},
		{UserID: u.ID, Firstname: "MineGone"},
		{UserID: u.ID, Firstname: "MineOld", Archived: true},
	}).Error)
	require.NoError(t, db.Where("firstname = ?", "MineGone").Delete(&models.Contact{}).Error)

	out := getJSON(t, r, "/contacts", url.Values{"search": {"mine"}})
	assert.Equal(t, []string{"Mine"}, contactField(out, "firstname"))
	out = getJSON(t, r, "/contacts", url.Values{"search": {"mine"}, "include_archived": {"true"}})
	assert.Equal(t, []string{"Mine", "MineOld"}, contactField(out, "firstname"), "archived last")
	assert.EqualValues(t, 2, out["total"])
}

func TestContactSearchRanksUnlessASortIsRequested(t *testing.T) {
	db, r, u := searchRouter(t)
	// created oldest first, so the default newest-first order would be the reverse of the ranking
	parent := models.Contact{UserID: u.ID, Firstname: "Zed", Lastname: "Parent"}
	require.NoError(t, db.Create(&parent).Error)
	require.NoError(t, db.Create(&models.Relationship{UserID: u.ID, ContactID: parent.ID, Name: "Ann Junior", Type: "Child"}).Error)
	require.NoError(t, db.Create(&models.Contact{UserID: u.ID, Firstname: "Ann", Lastname: "Lee"}).Error)
	require.NoError(t, db.Create(&models.Contact{UserID: u.ID, Firstname: "Ann", Lastname: "Marie Louise Van Der Berg Of Somewhere"}).Error)

	ranked := getJSON(t, r, "/contacts", url.Values{"search": {"ann"}})
	assert.Equal(t, []string{"Lee", "Marie Louise Van Der Berg Of Somewhere", "Parent"}, contactField(ranked, "lastname"),
		"shorter, more specific match first; a match through a relationship only comes last")

	sorted := getJSON(t, r, "/contacts", url.Values{"search": {"ann"}, "sort": {"lastname"}, "order": {"asc"}})
	assert.Equal(t, []string{"Lee", "Marie Louise Van Der Berg Of Somewhere", "Parent"}, contactField(sorted, "lastname"))
	sorted = getJSON(t, r, "/contacts", url.Values{"search": {"ann"}, "sort": {"lastname"}, "order": {"desc"}})
	assert.Equal(t, "Parent", contactField(sorted, "lastname")[0])

	paged := getJSON(t, r, "/contacts", url.Values{"search": {"ann"}, "limit": {"2"}, "page": {"2"}})
	assert.Equal(t, []string{"Parent"}, contactField(paged, "lastname"))
	assert.EqualValues(t, 3, paged["total"])
}

func TestNoteAndActivitySearch(t *testing.T) {
	db, r, u := searchRouter(t)
	other := models.User{Username: "other", Password: "password123", Email: "other@example.com"}
	require.NoError(t, db.Create(&other).Error)
	now := time.Now()
	require.NoError(t, db.Create(&[]models.Note{
		{UserID: u.ID, Content: "Trip to Zürich next week", Date: now},
		{UserID: u.ID, Content: "Groceries", Date: now},
		{UserID: u.ID, Content: "来週東京で会議", Date: now},
		{UserID: other.ID, Content: "Zürich is not yours", Date: now},
	}).Error)
	for _, term := range []string{"zurich", "zür", "東京"} {
		assert.EqualValues(t, 1, getJSON(t, r, "/notes", url.Values{"search": {term}})["total"], term)
	}

	contact := models.Contact{UserID: u.ID, Firstname: "Renée", Lastname: "Dupont", Phones: []models.ContactPhone{{Value: "555 0100"}}}
	require.NoError(t, db.Create(&contact).Error)
	require.NoError(t, db.Create(&[]models.Activity{
		{UserID: u.ID, Title: "Café catch-up", Description: "Planning", Location: "Münster", Date: now},
		{UserID: u.ID, Title: "Dinner", Date: now, Contacts: []models.Contact{contact}},
		{UserID: other.ID, Title: "Café of someone else", Date: now},
	}).Error)

	titles := func(term string) []string {
		var out []string
		for _, a := range getJSON(t, r, "/activities", url.Values{"search": {term}})["activities"].([]any) {
			out = append(out, a.(map[string]any)["title"].(string))
		}
		return out
	}
	assert.Equal(t, []string{"Café catch-up"}, titles("cafe"))
	assert.Equal(t, []string{"Café catch-up"}, titles("munster plan"))
	assert.Equal(t, []string{"Dinner"}, titles("renee"))
	assert.Equal(t, []string{"Dinner"}, titles("dupo"))
	assert.Empty(t, titles("dinner renee"), "every word must match in the activity or in one contact's name")
	assert.Empty(t, titles("0100"), "activities match contact names, not their phone numbers")
	assert.Empty(t, titles("someone"), "other users' activities are not searched")

	require.NoError(t, db.Delete(&contact).Error)
	assert.Empty(t, titles("renee"), "a deleted contact's name no longer matches")
	assert.Equal(t, []string{"Café catch-up"}, titles("caf"))
}

func TestLikeSearchTermsAreCapped(t *testing.T) {
	long := strings.Repeat("山", 300)
	assert.Equal(t, "%"+strings.Repeat("山", maxSearchRunes)+"%", searchArg(long).Value)
}
