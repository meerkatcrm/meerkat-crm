package controllers

import (
	"bytes"
	"meerkat/models"
	"meerkat/services"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedExportSource gives userID one of everything the data export writes.
func seedExportSource(t *testing.T, db *gorm.DB, userID uint) {
	t.Helper()
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", userID).
		Update("custom_field_names", `["Pet name"]`).Error)

	alice := models.Contact{
		UserID: userID, Firstname: "Alice", Lastname: "Johnson", Nickname: "Ally",
		Gender: "female", Birthday: "1990-01-15", Anniversary: "--06-01",
		HowWeMet: "Work conference", Note: "Line one\nLine two, with \"quotes\"",
		Circles:      []string{"Friends", "Work"},
		Emails:       []models.ContactEmail{{Type: "work", Value: "alice@example.com"}, {Value: "alice@home.example"}},
		Phones:       []models.ContactPhone{{Type: "cell", Value: "+49 170 1234567"}},
		Addresses:    []models.ContactAddress{{Type: "home", Street: "1 Main St", City: "Berlin"}},
		URLs:         []models.ContactURL{{Type: "blog", Value: "https://alice.example"}},
		IMPPs:        []models.ContactIMPP{{Type: "signal", Value: "alice.01"}},
		Organization: "Acme", JobTitle: "Engineer",
		Archived:     true,
		CustomFields: map[string]string{"Pet name": "Rex"},
	}
	require.NoError(t, db.Create(&alice).Error)
	// Two contacts with the same name: activities must still link the right one.
	bob1 := models.Contact{UserID: userID, Firstname: "Bob", Lastname: "Smith", Emails: []models.ContactEmail{{Value: "bob1@example.com"}}}
	bob2 := models.Contact{UserID: userID, Firstname: "Bob", Lastname: "Smith", Emails: []models.ContactEmail{{Value: "bob2@example.com"}}}
	require.NoError(t, db.Create(&bob1).Error)
	require.NoError(t, db.Create(&bob2).Error)

	require.NoError(t, db.Create(&models.Relationship{UserID: userID, ContactID: alice.ID, Name: "Charlie", Type: "Child", StartDate: "2001"}).Error)
	require.NoError(t, db.Create(&models.Relationship{UserID: userID, ContactID: alice.ID, Name: "Bob Smith", Type: "Friend", RelatedContactID: &bob2.ID}).Error)

	require.NoError(t, db.Create(&models.Activity{
		UserID: userID, Title: "Coffee", Location: "Cafe",
		Date:     time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC),
		Contacts: []models.Contact{alice, bob2},
	}).Error)
	require.NoError(t, db.Create(&models.Note{
		UserID: userID, ContactID: &alice.ID, Content: "Follow up\nabout the project",
		Date: time.Date(2024, 7, 10, 10, 0, 0, 0, time.UTC),
	}).Error)
	require.NoError(t, db.Create(&models.Note{
		UserID: userID, Content: "Journal: a quiet day",
		Date: time.Date(2024, 7, 11, 10, 0, 0, 0, time.UTC),
	}).Error)
	byMail, reoccur := true, false
	lastSent := time.Date(2024, 8, 1, 9, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&models.Reminder{
		UserID: userID, ContactID: &alice.ID, Message: "Call Alice", Recurrence: "monthly",
		RemindAt: time.Date(2024, 9, 1, 9, 0, 0, 0, time.UTC), ByMail: &byMail,
		ReoccurFromCompletion: &reoccur, LastSent: &lastSent,
	}).Error)
}

func exportCSV(t *testing.T, db *gorm.DB, userID uint) []byte {
	t.Helper()
	r := routerForUser(db, userID)
	r.GET("/export", ExportData)
	w := doRequest(r, http.MethodGet, "/export", nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return w.Body.Bytes()
}

func importCSV(t *testing.T, db *gorm.DB, userID uint, path string, content []byte) services.DataImportResult {
	t.Helper()
	r := routerForUser(db, userID)
	r.POST("/import", ImportData)
	w := doUpload(t, r, path, "file", "meerkat-export.csv", content)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return decodeJSON[services.DataImportResult](t, w)
}

func TestImportData_RoundTripIntoAnotherUser(t *testing.T) {
	db := newTestDB(t)
	source := newTestUser(t, db, "source", false)
	target := newTestUser(t, db, "target", false)
	seedExportSource(t, db, source.ID)
	file := exportCSV(t, db, source.ID)

	// A dry run reports the import and saves nothing.
	preview := importCSV(t, db, target.ID, "/import?dry_run=true", file)
	assert.True(t, preview.DryRun)
	assert.Equal(t, models.DataImportCount{Created: 3}, preview.Contacts)
	assert.Equal(t, models.DataImportCount{Created: 2}, preview.Relationships)
	assert.Equal(t, models.DataImportCount{Created: 1}, preview.Activities)
	assert.Equal(t, models.DataImportCount{Created: 2}, preview.Notes)
	assert.Equal(t, models.DataImportCount{Created: 1}, preview.Reminders)
	assert.Equal(t, []string{"Pet name"}, preview.CustomFieldsAdded)
	assert.Empty(t, preview.Warnings)
	var count int64
	db.Model(&models.Contact{}).Where("user_id = ?", target.ID).Count(&count)
	assert.Zero(t, count, "dry run must not save contacts")
	var reloaded models.User
	require.NoError(t, db.First(&reloaded, target.ID).Error)
	assert.Empty(t, reloaded.CustomFieldNames, "dry run must not save custom field names")

	result := importCSV(t, db, target.ID, "/import", file)
	assert.False(t, result.DryRun)
	assert.Equal(t, preview.Contacts, result.Contacts)
	assert.Equal(t, preview.Activities, result.Activities)

	require.NoError(t, db.First(&reloaded, target.ID).Error)
	assert.Equal(t, []string{"Pet name"}, reloaded.CustomFieldNames)

	var alice models.Contact
	require.NoError(t, db.Where("user_id = ? AND firstname = ?", target.ID, "Alice").First(&alice).Error)
	assert.Equal(t, "Johnson", alice.Lastname)
	assert.Equal(t, "Ally", alice.Nickname)
	assert.Equal(t, "female", alice.Gender)
	assert.Equal(t, "1990-01-15", alice.Birthday)
	assert.Equal(t, "--06-01", alice.Anniversary)
	assert.Equal(t, "Line one\nLine two, with \"quotes\"", alice.Note)
	assert.Equal(t, []string{"Friends", "Work"}, alice.Circles)
	assert.Equal(t, []models.ContactEmail{{Type: "work", Value: "alice@example.com"}, {Value: "alice@home.example"}}, alice.Emails)
	assert.Equal(t, "alice@example.com", alice.Email)
	assert.Equal(t, []models.ContactPhone{{Type: "cell", Value: "+49 170 1234567"}}, alice.Phones)
	assert.Equal(t, []models.ContactAddress{{Type: "home", Street: "1 Main St, Berlin"}}, alice.Addresses)
	assert.Equal(t, "1 Main St, Berlin", alice.Address)
	assert.Equal(t, []models.ContactURL{{Type: "blog", Value: "https://alice.example"}}, alice.URLs)
	assert.Equal(t, []models.ContactIMPP{{Type: "signal", Value: "alice.01"}}, alice.IMPPs)
	assert.Equal(t, "Acme", alice.Organization)
	assert.Equal(t, "Engineer", alice.JobTitle)
	assert.True(t, alice.Archived)
	assert.Equal(t, map[string]string{"Pet name": "Rex"}, alice.CustomFields)

	var bob2 models.Contact
	require.NoError(t, db.Where("user_id = ? AND email = ?", target.ID, "bob2@example.com").First(&bob2).Error)

	var rels []models.Relationship
	require.NoError(t, db.Where("user_id = ?", target.ID).Order("name").Find(&rels).Error)
	require.Len(t, rels, 2)
	assert.Equal(t, alice.ID, rels[0].ContactID)
	require.NotNil(t, rels[0].RelatedContactID)
	assert.Equal(t, bob2.ID, *rels[0].RelatedContactID, "related contact is remapped to the imported copy")
	assert.Equal(t, "Charlie", rels[1].Name)
	assert.Equal(t, "2001", rels[1].StartDate)
	assert.Nil(t, rels[1].RelatedContactID)

	var activity models.Activity
	require.NoError(t, db.Where("user_id = ?", target.ID).Preload("Contacts").First(&activity).Error)
	assert.Equal(t, "Coffee", activity.Title)
	assert.True(t, activity.Date.Equal(time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)))
	linked := []uint{}
	for _, c := range activity.Contacts {
		linked = append(linked, c.ID)
		assert.NotEmpty(t, c.Firstname, "linking must not overwrite the contact")
	}
	assert.ElementsMatch(t, []uint{alice.ID, bob2.ID}, linked)

	var notes []models.Note
	require.NoError(t, db.Where("user_id = ?", target.ID).Order("date").Find(&notes).Error)
	require.Len(t, notes, 2)
	assert.Equal(t, "Follow up\nabout the project", notes[0].Content)
	assert.Equal(t, alice.ID, *notes[0].ContactID)
	assert.Equal(t, "Journal: a quiet day", notes[1].Content)
	assert.Nil(t, notes[1].ContactID, "a journal note stays without a contact")

	var reminder models.Reminder
	require.NoError(t, db.Where("user_id = ?", target.ID).First(&reminder).Error)
	assert.Equal(t, "Call Alice", reminder.Message)
	assert.Equal(t, "monthly", reminder.Recurrence)
	assert.Equal(t, alice.ID, *reminder.ContactID)
	assert.True(t, *reminder.ByMail)
	assert.False(t, *reminder.ReoccurFromCompletion)
	assert.True(t, reminder.EmailSent, "an already-sent reminder must not be emailed again")
	require.NotNil(t, reminder.LastSent)

	// The source account is untouched.
	db.Model(&models.Contact{}).Where("user_id = ?", source.ID).Count(&count)
	assert.EqualValues(t, 3, count)
}

func TestImportData_ReimportAddsNothing(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	seedExportSource(t, db, user.ID)
	file := exportCSV(t, db, user.ID)

	result := importCSV(t, db, user.ID, "/import", file)
	assert.Equal(t, models.DataImportCount{Existing: 3}, result.Contacts)
	assert.Equal(t, models.DataImportCount{Existing: 2}, result.Relationships)
	assert.Equal(t, models.DataImportCount{Existing: 1}, result.Activities)
	assert.Equal(t, models.DataImportCount{Existing: 2}, result.Notes)
	assert.Equal(t, models.DataImportCount{Existing: 1}, result.Reminders)
	assert.Empty(t, result.CustomFieldsAdded)

	var contacts int64
	db.Model(&models.Contact{}).Where("user_id = ?", user.ID).Count(&contacts)
	assert.EqualValues(t, 3, contacts)
}

func TestImportData_OlderExportLinksActivitiesByName(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	file := strings.Join([]string{
		"=== CONTACTS ===",
		"ID,Firstname,Lastname,Email",
		"7,Dana,Lee,dana@example.com",
		"",
		"=== ACTIVITIES ===",
		"ID,Title,Description,Location,Date,Contact Names,Created At,Updated At",
		"1,Lunch,,,2024-01-02T12:00:00Z,Dana Lee; Ghost Person,,",
		"",
		"=== NOTES ===",
		"ID,Contact ID,Contact Name,Content,Date,Created At,Updated At",
		"1,99,Ghost Person,Orphan,2024-01-02T12:00:00Z,,",
		"",
	}, "\n")

	result := importCSV(t, db, user.ID, "/import", []byte(file))
	assert.Equal(t, 1, result.Contacts.Created)
	assert.Equal(t, 1, result.Activities.Created)
	assert.Equal(t, models.DataImportCount{Skipped: 1}, result.Notes)
	assert.Len(t, result.Warnings, 2, "one unlinked activity contact, one skipped note")

	var activity models.Activity
	require.NoError(t, db.Where("user_id = ?", user.ID).Preload("Contacts").First(&activity).Error)
	require.Len(t, activity.Contacts, 1)
	assert.Equal(t, "Dana", activity.Contacts[0].Firstname)
}

func TestImportData_InvalidValuesAreDroppedNotFatal(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	file := "=== CONTACTS ===\n" +
		"ID,Firstname,Lastname,Gender,Birthday,Emails\n" +
		"1,Eve,Adams,robot,not-a-date,work: not-an-email; eve@example.com\n" +
		"2,,Nameless,,,\n"

	result := importCSV(t, db, user.ID, "/import", []byte(file))
	assert.Equal(t, models.DataImportCount{Created: 1, Skipped: 1}, result.Contacts)
	assert.Len(t, result.Warnings, 2)

	var eve models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&eve).Error)
	assert.Empty(t, eve.Gender)
	assert.Empty(t, eve.Birthday)
	assert.Equal(t, []models.ContactEmail{{Value: "eve@example.com"}}, eve.Emails)
}

func TestImportData_LongMultibyteNoteIsKept(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	content := strings.Repeat("笔", 3000) // 3000 characters, 9000 bytes: within the 5000-character limit
	file := "=== CONTACTS ===\nID,Firstname\n1,Li\n\n=== NOTES ===\n" +
		"ID,Contact ID,Contact Name,Content,Date,Created At,Updated At\n" +
		"1,1,Li ," + content + ",2024-01-02T12:00:00Z,,\n"

	result := importCSV(t, db, user.ID, "/import", []byte(file))
	assert.Equal(t, models.DataImportCount{Created: 1}, result.Notes, result.Warnings)
}

func TestImportData_RejectsOtherFiles(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	r := routerForUser(db, user.ID)
	r.POST("/import", ImportData)

	w := doUpload(t, r, "/import", "file", "contacts.csv", []byte("Firstname,Lastname\nA,B\n"))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	w = doUpload(t, r, "/import", "file", "contacts.vcf", []byte("BEGIN:VCARD\n"))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	// A dry_run value that can't be read must not turn into a real import.
	w = doUpload(t, r, "/import?dry_run=yes-please", "file", "meerkat-export.csv",
		[]byte("=== CONTACTS ===\nID,Firstname\n1,Ann\n"))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var count int64
	db.Model(&models.Contact{}).Where("user_id = ?", user.ID).Count(&count)
	assert.Zero(t, count)
}

// The export writes contact values by position, so each row must have exactly one
// value per header; a header added to models.ContactExportHeaders without its value
// (or the reverse) would shift every column after it.
func TestExportData_ContactRowsMatchHeaders(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	seedExportSource(t, db, user.ID)

	parsed, err := services.ParseDataExport(bytes.NewReader(exportCSV(t, db, user.ID)))
	require.NoError(t, err)
	headers, rows := parsed.Section("CONTACTS")
	assert.Equal(t, append(slices.Clone(models.ContactExportHeaders), "Pet name"), headers)
	require.NotEmpty(t, rows)
	for _, row := range rows {
		assert.Len(t, row, len(headers))
	}
}

// A contact deleted after the export comes back as a new contact on import: soft-deleted
// contacts are not duplicate candidates, so nothing gets attached to the deleted row.
func TestImportData_DeletedContactIsRecreated(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	alice := models.Contact{UserID: user.ID, Firstname: "Alice", Lastname: "Gone", Emails: []models.ContactEmail{{Value: "alice@example.com"}}}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&models.Note{
		UserID: user.ID, ContactID: &alice.ID, Content: "Met at the fair",
		Date: time.Date(2024, 5, 1, 9, 0, 0, 0, time.UTC),
	}).Error)
	file := exportCSV(t, db, user.ID)
	require.NoError(t, db.Delete(&alice).Error)

	result := importCSV(t, db, user.ID, "/import", file)
	assert.Equal(t, models.DataImportCount{Created: 1}, result.Contacts)
	assert.Equal(t, models.DataImportCount{Created: 1}, result.Notes)

	var live []models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).Find(&live).Error)
	require.Len(t, live, 1)
	assert.NotEqual(t, alice.ID, live[0].ID)
	var note models.Note
	require.NoError(t, db.Where("user_id = ? AND content = ?", user.ID, "Met at the fair").Order("id DESC").First(&note).Error)
	assert.Equal(t, live[0].ID, *note.ContactID, "the note belongs to the visible contact")
}

func TestImportData_OverlongCustomColumnIsLeftOut(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	long := strings.Repeat("x", 101)
	file := "=== CONTACTS ===\nID,Firstname,Hobby," + long + "\n1,Ann,Chess,hidden\n"

	result := importCSV(t, db, user.ID, "/import", []byte(file))
	assert.Equal(t, []string{"Hobby"}, result.CustomFieldsAdded)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "left out")

	var ann models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&ann).Error)
	assert.Equal(t, map[string]string{"Hobby": "Chess"}, ann.CustomFields)
}

func TestImportData_ShortenedContactNoteIsReported(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	file := "=== CONTACTS ===\nID,Firstname,Note\n1,Ann," + strings.Repeat("n", models.MaxNoteLength+10) + "\n"

	result := importCSV(t, db, user.ID, "/import", []byte(file))
	assert.Equal(t, models.DataImportCount{Created: 1}, result.Contacts)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "note shortened")

	var ann models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&ann).Error)
	assert.Len(t, ann.Note, models.MaxNoteLength)
}

func TestImportData_UnresolvedRelatedContactIsReported(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	file := "=== CONTACTS ===\nID,Firstname\n1,Ann\n\n=== RELATIONSHIPS ===\n" +
		"ID,Contact ID,Contact Name,Name,Type,Gender,Birthday,Related Contact ID,Related Contact Name,Created At,Updated At\n" +
		"1,1,Ann ,Bo,Friend,,,42,Bo Gone,,\n" +
		"2,1,Ann ,Cy,Sibling,,,,,,\n"

	result := importCSV(t, db, user.ID, "/import", []byte(file))
	assert.Equal(t, models.DataImportCount{Created: 2}, result.Relationships)
	require.Len(t, result.Warnings, 1, "only the relationship whose related contact is missing is reported")
	assert.Contains(t, result.Warnings[0], `Relationship "Bo"`)
	assert.Contains(t, result.Warnings[0], "not linked")

	var bo models.Relationship
	require.NoError(t, db.Where("user_id = ? AND name = ?", user.ID, "Bo").First(&bo).Error)
	assert.Nil(t, bo.RelatedContactID)
}

func TestImportData_SelfLinkedRelationshipKeepsItsLink(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	file := "=== CONTACTS ===\nID,Firstname\n1,Ann\n\n=== RELATIONSHIPS ===\n" +
		"ID,Contact ID,Contact Name,Name,Type,Gender,Birthday,Related Contact ID,Related Contact Name,Created At,Updated At\n" +
		"1,1,Ann ,Ann,Self,,,1,Ann ,,\n"

	result := importCSV(t, db, user.ID, "/import", []byte(file))
	assert.Equal(t, models.DataImportCount{Created: 1}, result.Relationships)
	assert.Empty(t, result.Warnings)

	var rel models.Relationship
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&rel).Error)
	require.NotNil(t, rel.RelatedContactID)
	assert.Equal(t, rel.ContactID, *rel.RelatedContactID)
}

// Two contacts in the file that both match one existing contact: a relationship between
// them would point back at its own contact, so the link is left out and reported.
func TestImportData_RelationshipBetweenMergedContactsIsReported(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	require.NoError(t, db.Create(&models.Contact{UserID: user.ID, Firstname: "Sam", Lastname: "Lee",
		Emails: []models.ContactEmail{{Value: "sam@example.com"}}}).Error)
	file := "=== CONTACTS ===\nID,Firstname,Lastname,Email\n1,Sam,Lee,\n2,Samuel,L,sam@example.com\n\n=== RELATIONSHIPS ===\n" +
		"ID,Contact ID,Contact Name,Name,Type,Gender,Birthday,Related Contact ID,Related Contact Name,Created At,Updated At\n" +
		"1,1,Sam Lee,Samuel L,Friend,,,2,Samuel L,,\n"

	result := importCSV(t, db, user.ID, "/import", []byte(file))
	assert.Equal(t, models.DataImportCount{Existing: 2}, result.Contacts)
	assert.Equal(t, models.DataImportCount{Created: 1}, result.Relationships)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "matched the same existing contact")

	var rel models.Relationship
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&rel).Error)
	assert.Nil(t, rel.RelatedContactID)
}

func TestImportData_RejectsRepeatedSection(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	r := routerForUser(db, user.ID)
	r.POST("/import", ImportData)
	file := "=== CONTACTS ===\nID,Firstname\n1,Ann\n\n=== CONTACTS ===\nID,Firstname\n1,Bea\n"

	w := doUpload(t, r, "/import", "file", "meerkat-export.csv", []byte(file))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "CONTACTS section appears more than once")

	var count int64
	db.Model(&models.Contact{}).Where("user_id = ?", user.ID).Count(&count)
	assert.Zero(t, count)
}

// Existing activities are matched by title and time to the second, whatever time zone
// or sub-second precision either side carries.
func TestImportData_ActivityMatchIgnoresZoneAndSubSecond(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	berlin := time.FixedZone("CET", 3600)
	require.NoError(t, db.Create(&models.Activity{UserID: user.ID, Title: "Lunch",
		Date: time.Date(2024, 1, 2, 13, 0, 0, 400_000_000, berlin)}).Error)
	file := "=== CONTACTS ===\nID,Firstname\n1,Ann\n\n=== ACTIVITIES ===\n" +
		"ID,Title,Description,Location,Date,Contact Names,Created At,Updated At,Contact IDs\n" +
		"1,Lunch,,,2024-01-02T12:00:00Z,,,,\n" +
		"2,Lunch,,,2024-01-02T12:00:01Z,,,,\n"

	result := importCSV(t, db, user.ID, "/import", []byte(file))
	assert.Equal(t, models.DataImportCount{Created: 1, Existing: 1}, result.Activities)
}

// A custom field named like a fixed column is exported twice under that name; the
// second column is the custom field and must come back as one.
func TestImportData_CustomFieldNamedLikeFixedColumn(t *testing.T) {
	db := newTestDB(t)
	source := newTestUser(t, db, "source", false)
	target := newTestUser(t, db, "target", false)
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", source.ID).
		Update("custom_field_names", `["Note"]`).Error)
	require.NoError(t, db.Create(&models.Contact{UserID: source.ID, Firstname: "Ann",
		Note: "the contact note", CustomFields: map[string]string{"Note": "the custom value"}}).Error)

	result := importCSV(t, db, target.ID, "/import", exportCSV(t, db, source.ID))
	assert.Equal(t, []string{"Note"}, result.CustomFieldsAdded)

	var ann models.Contact
	require.NoError(t, db.Where("user_id = ?", target.ID).First(&ann).Error)
	assert.Equal(t, "the contact note", ann.Note)
	assert.Equal(t, map[string]string{"Note": "the custom value"}, ann.CustomFields)
}

func TestImportData_UnreadableFlagsAreReported(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	file := "=== CONTACTS ===\nID,Firstname,Deceased,Archived\n1,Ann,yes please,true\n\n=== REMINDERS ===\n" +
		"ID,Contact ID,Contact Name,Message,Remind At,Recurrence,By Mail,Reoccur From Completion,Completed,Last Sent,Created At,Updated At\n" +
		"1,1,Ann ,Call Ann,2024-09-01T09:00:00Z,once,maybe,,false,,,\n"

	result := importCSV(t, db, user.ID, "/import", []byte(file))
	assert.Equal(t, models.DataImportCount{Created: 1}, result.Contacts)
	assert.Equal(t, models.DataImportCount{Created: 1}, result.Reminders)
	require.Len(t, result.Warnings, 2)
	assert.Contains(t, result.Warnings[0], "deceased (not true or false)")
	assert.Contains(t, result.Warnings[1], "By Mail not true or false")

	var ann models.Contact
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&ann).Error)
	assert.False(t, ann.IsDeceased)
	assert.True(t, ann.Archived)
	var reminder models.Reminder
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&reminder).Error)
	assert.False(t, *reminder.ByMail)
	assert.True(t, *reminder.ReoccurFromCompletion, "an empty cell takes the column default")
}
