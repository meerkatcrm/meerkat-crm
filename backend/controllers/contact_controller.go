package controllers

import (
	"cmp"
	"database/sql"
	"errors"
	apperrors "meerkat/errors"
	"meerkat/logger"
	"meerkat/middleware"
	"meerkat/models"
	"meerkat/services"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// contactSelectableFields lists the columns a client may request via ?fields=.
// CONTACT_DETAIL_COLUMNS in frontend/src/ContactDetailPage.tsx must stay in sync;
// frontend/src/contactColumns.test.ts fails when the two drift apart.
var contactSelectableFields = []string{
	"ID", "firstname", "lastname", "nickname", "gender",
	"email", "phone", "birthday", "address", "how_we_met",
	"food_preference", "work_information", "contact_information", "about",
	"circles", "photo", "photo_thumbnail", "custom_fields", "archived",
	"emails", "phones", "addresses", "urls", "impps",
	"prefix", "middle_name", "suffix", "organization", "department",
	"job_title", "role", "anniversary", "is_deceased", "deceased_date",
}

// contactIncludes maps a value of ?includes= to the association it preloads and
// the table whose user_id scopes it.
var contactIncludes = map[string]struct{ association, table string }{
	"notes":              {"Notes", "notes"},
	"activities":         {"Activities", "activities"},
	"relationships":      {"Relationships", "relationships"},
	"reminders":          {"Reminders", "reminders"},
	"employment_history": {"EmploymentHistories", "employment_histories"},
}

// applyContactInput copies every client-writable field except the four employment
// scalars (Organization, Department, JobTitle, Role), which create and update
// treat differently. A new contact field is added here once, for both.
func applyContactInput(contact *models.Contact, in *models.ContactInput) {
	contact.Firstname = in.Firstname
	contact.Lastname = in.Lastname
	contact.Nickname = in.Nickname
	contact.Gender = in.Gender
	contact.Email = in.Email
	contact.Phone = in.Phone
	contact.Birthday = in.Birthday
	contact.Address = in.Address
	contact.HowWeMet = in.HowWeMet
	contact.FoodPreference = in.FoodPreference
	contact.WorkInformation = in.WorkInformation
	contact.ContactInformation = in.ContactInformation
	contact.About = in.About
	contact.Circles = in.Circles
	contact.CustomFields = in.CustomFields
	contact.Emails = in.Emails
	contact.Phones = in.Phones
	contact.Addresses = in.Addresses
	contact.URLs = in.URLs
	contact.IMPPs = in.IMPPs
	contact.Prefix = in.Prefix
	contact.MiddleName = in.MiddleName
	contact.Suffix = in.Suffix
	contact.Anniversary = in.Anniversary
	contact.IsDeceased = in.IsDeceased
	contact.DeceasedDate = in.DeceasedDate
}

func CreateContact(c *gin.Context) {
	// Save to the database
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Get validated input from validation middleware
	contactInput, err := middleware.GetValidated[models.ContactInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	// Create contact from validated input
	contact := models.Contact{
		UserID:       userID,
		Organization: contactInput.Organization,
		Department:   contactInput.Department,
		JobTitle:     contactInput.JobTitle,
		Role:         contactInput.Role,
	}
	applyContactInput(&contact, contactInput)
	if err := db.Create(&contact).Error; err != nil {
		logger.FromContext(c).Error().Err(err).Msg("Error saving contact to database")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to save contact").WithError(err))
		return
	}

	go services.TriggerWebhooks(db, currentConfig(c), userID, "contact.created", contact)
	c.JSON(http.StatusCreated, gin.H{"message": "Contact created successfully", "contact": contact})
}

// filters a contacts query by a free-text term
func applyContactSearch(query *gorm.DB, userID uint, searchTerm string) *gorm.DB {
	const jsonValue = "json_extract(json_each.value, '$.value')"
	return query.Where(
		likeAny("firstname", "lastname", "nickname",
			"(firstname || ' ' || lastname)", "(nickname || ' ' || lastname)",
			"email", "phone")+
			" OR (json_valid(emails) AND EXISTS (SELECT 1 FROM json_each(contacts.emails) WHERE "+likeAny(jsonValue)+"))"+
			" OR (json_valid(phones) AND EXISTS (SELECT 1 FROM json_each(contacts.phones) WHERE "+likeAny(jsonValue)+"))"+
			// People without a contact of their own (e.g. a child stored only as
			// a relationship) are found through the contact they belong to.
			" OR EXISTS (SELECT 1 FROM relationships WHERE relationships.contact_id = contacts.id AND relationships.user_id = @user_id AND relationships.deleted_at IS NULL AND relationships.related_contact_id IS NULL AND "+likeAny("relationships.name")+")",
		searchArg(searchTerm), sql.Named("user_id", userID),
	)
}

func GetContacts(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	pagination := GetPaginationParams(c)

	// Parse requested fields with validation
	var selectedFields []string
	fields := c.Query("fields")
	if fields != "" {
		for _, field := range strings.Split(fields, ",") {
			if slices.Contains(contactSelectableFields, field) { // Validate field
				selectedFields = append(selectedFields, field)
			}
		}
	} else {
		selectedFields = slices.Clone(contactSelectableFields) // Use all allowed fields if none are specified
	}

	// Parse and validate sort parameters
	sortField := c.DefaultQuery("sort", "id")
	sortOrder := c.DefaultQuery("order", "desc")

	allowedSortFields := map[string]bool{"firstname": true, "lastname": true, "id": true, "random": true}
	if !allowedSortFields[sortField] {
		sortField = "id"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	// Parse archive filtering parameters
	includeArchived := c.Query("include_archived") == "true"
	archivedOnly := c.Query("archived") == "true"

	searchTerm := c.Query("search")
	circle := c.Query("circle")

	// The page query and the count query must apply exactly the same filters.
	filtered := func() *gorm.DB {
		query := db.Model(&models.Contact{}).Where("user_id = ?", userID)
		if !includeArchived {
			query = query.Where("archived = ?", archivedOnly)
		}
		if searchTerm != "" {
			query = applyContactSearch(query, userID, searchTerm)
		}
		if circle != "" {
			query = query.Where("EXISTS (SELECT 1 FROM json_each(contacts.circles) WHERE json_each.value = ?)", circle)
		}
		return query
	}

	var contacts []models.Contact
	query := filtered().Limit(pagination.Limit).Offset(pagination.Offset)

	// Apply ordering - random uses RANDOM() function, others use column name
	// For search with include_archived, order non-archived first
	if includeArchived && searchTerm != "" {
		query = query.Order("archived ASC")
	}
	if sortField == "random" {
		query = query.Order("RANDOM()")
	} else {
		query = query.Order(sortField + " " + sortOrder)
	}

	if len(selectedFields) > 0 {
		query = query.Select(selectedFields)
	}

	// Preload requested relationships
	for _, rel := range strings.Split(c.Query("includes"), ",") {
		if preload, ok := contactIncludes[rel]; ok {
			query = query.Preload(preload.association, preload.table+".user_id = ?", userID)
		}
	}

	// Execute query
	if err := query.Find(&contacts).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contacts").WithError(err))
		return
	}

	var total int64
	if err := filtered().Count(&total).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to count contacts").WithError(err))
		return
	}

	// Map contacts to ContactResponse with photo thumbnails
	contactResponses := make([]models.ContactResponse, len(contacts))
	for i, contact := range contacts {
		contactResponses[i] = models.ContactResponse{
			Contact:        contact,
			PhotoThumbnail: contact.PhotoThumbnail,
		}
	}

	// Respond with contacts and pagination metadata
	c.JSON(http.StatusOK, gin.H{
		"contacts": contactResponses,
		"total":    total,
		"page":     pagination.Page,
		"limit":    pagination.Limit,
	})
}

func GetContactsRandom(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var selectedFields = []string{"ID", "firstname", "lastname", "nickname", "circles", "photo_thumbnail"}

	var contacts []models.Contact
	query := db.Model(&models.Contact{}).Where("user_id = ?", userID).Where("archived = ?", false)

	if len(selectedFields) > 0 {
		query = query.Select(selectedFields)
	}

	// Get 5 random contacts
	query = query.Order("RANDOM()").Limit(5)

	// Execute query
	if err := query.Find(&contacts).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contacts").WithError(err))
		return
	}

	// Map to response with photo thumbnail
	contactResponses := make([]models.ContactResponse, len(contacts))
	for i, contact := range contacts {
		contactResponses[i] = models.ContactResponse{
			Contact:        contact,
			PhotoThumbnail: contact.PhotoThumbnail,
		}
	}

	// Respond with random contacts
	c.JSON(http.StatusOK, gin.H{
		"contacts": contactResponses,
	})
}

func GetUpcomingBirthdays(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	birthdays, err := services.GetUpcomingBirthdays(db, userID, time.Now())
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve upcoming birthdays").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"birthdays": birthdays,
	})
}

func GetContact(c *gin.Context) {
	id := c.Param("id")

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	// Check for fields query parameter to enable partial fetching
	var selectedFields []string
	fields := c.Query("fields")
	if fields != "" {
		for _, field := range strings.Split(fields, ",") {
			if slices.Contains(contactSelectableFields, field) {
				selectedFields = append(selectedFields, field)
			}
		}
	}

	var contact models.Contact
	query := db.Where("user_id = ?", userID)

	if len(selectedFields) > 0 {
		// Partial fetch: only select requested fields, skip preloading associations
		query = query.Select(selectedFields)
	} else {
		// Full fetch: preload all associations
		for _, preload := range contactIncludes {
			query = query.Preload(preload.association, preload.table+".user_id = ?", userID)
		}
	}

	if err := query.First(&contact, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact").WithError(err))
		}
		return
	}
	c.JSON(http.StatusOK, contact)
}

func UpdateContact(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	contact, ok := findOwned[models.Contact](c, db, userID, id, "Contact")
	if !ok {
		return
	}

	// Get validated input from validation middleware
	contactInput, err := middleware.GetValidated[models.ContactInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	applyContactInput(&contact, contactInput)
	// Organization, Department, JobTitle and Role mirror the current employment
	// history entry. Saving the contact applies a change to that entry, or creates
	// one if there is none (Contact.syncCurrentEmploymentHistory). The input can't
	// tell a missing field from a blank one, so blank keeps the stored value: a
	// partial PUT must not wipe the rest of the job. Clearing a field goes through
	// the employment history endpoints.
	organization := strings.TrimSpace(contactInput.Organization)
	department := strings.TrimSpace(contactInput.Department)
	jobTitle := strings.TrimSpace(contactInput.JobTitle)
	role := strings.TrimSpace(contactInput.Role)
	// A client holding an old copy of the contact must not bring back a job the
	// user has since ended, or overwrite a newer one. This compares what was sent:
	// merged with the current job's values, it would no longer match the ended job.
	stale, staleErr := contact.EndedEmploymentMatches(db, organization, department, jobTitle, role)
	if staleErr != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to update contact").WithError(staleErr))
		return
	}
	if !stale {
		contact.Organization = cmp.Or(organization, contact.Organization)
		contact.Department = cmp.Or(department, contact.Department)
		contact.JobTitle = cmp.Or(jobTitle, contact.JobTitle)
		contact.Role = cmp.Or(role, contact.Role)
	}
	// Also covers a partial PUT whose merged values equal the ended job.
	contact.RevertStaleEmployment = true

	if err := db.Save(&contact).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to update contact").WithError(err))
		return
	}

	go services.TriggerWebhooks(db, currentConfig(c), userID, "contact.updated", contact)
	c.JSON(http.StatusOK, contact)
}

func DeleteContact(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Check if contact exists first
	contact, ok := findOwned[models.Contact](c, db, userID, id, "Contact")
	if !ok {
		return
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		return models.DeleteContactCascade(tx, userID, contact.ID)
	})

	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to delete contact and associated data").WithError(err))
		return
	}

	// Cleanup profile photos after successful database transaction
	// This is done outside the transaction since file deletion cannot be rolled back
	deleteContactPhotos(c, contact)

	go services.TriggerWebhooks(db, currentConfig(c), userID, "contact.deleted", gin.H{"id": contact.ID})
	c.JSON(http.StatusOK, gin.H{"message": "Contact deleted"})
}

// deleteContactPhotos removes the profile photo file for a contact
// Note: thumbnails are stored as base64 in the database, not as files
func deleteContactPhotos(c *gin.Context, contact models.Contact) {
	uploadDir := os.Getenv("PROFILE_PHOTO_DIR")
	if uploadDir == "" {
		return
	}

	log := logger.FromContext(c)

	// Delete main photo if it exists
	if contact.Photo != "" {
		photoPath := filepath.Join(uploadDir, contact.Photo)
		if err := os.Remove(photoPath); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", photoPath).Msg("Failed to delete contact photo")
		} else if err == nil {
			log.Debug().Str("path", photoPath).Msg("Deleted contact photo")
		}
	}

	// Delete legacy file-based thumbnail if it exists (not base64 data URL)
	if contact.PhotoThumbnail != "" && !strings.HasPrefix(contact.PhotoThumbnail, "data:") {
		thumbnailPath := filepath.Join(uploadDir, contact.PhotoThumbnail)
		if err := os.Remove(thumbnailPath); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", thumbnailPath).Msg("Failed to delete contact thumbnail")
		} else if err == nil {
			log.Debug().Str("path", thumbnailPath).Msg("Deleted contact thumbnail")
		}
	}
}

// GetCircles returns all unique circles associated with contacts.
func GetCircles(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var circleNames []string

	// Raw SQL query to extract unique circle names
	err := db.Raw(`SELECT DISTINCT json_each.value AS circle
	               FROM contacts, json_each(contacts.circles)
	               WHERE contacts.user_id = ?`, userID).Scan(&circleNames).Error
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve circles").WithError(err))
		return
	}

	// Return the list of unique circle names
	c.JSON(http.StatusOK, circleNames)
}

// ArchiveContact archives a contact and deletes all its reminders
func ArchiveContact(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	contact, ok := findOwned[models.Contact](c, db, userID, id, "Contact")
	if !ok {
		return
	}

	// Archive contact and delete reminders in a transaction
	err := db.Transaction(func(tx *gorm.DB) error {
		// Delete all reminders for this contact
		if err := tx.Where("contact_id = ? AND user_id = ?", id, userID).Delete(&models.Reminder{}).Error; err != nil {
			return err
		}

		// Set archived to true
		if err := tx.Model(&contact).Update("archived", true).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to archive contact").WithError(err))
		return
	}

	contact.Archived = true
	c.JSON(http.StatusOK, contact)
}

// UnarchiveContact restores an archived contact
func UnarchiveContact(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	contact, ok := findOwned[models.Contact](c, db, userID, id, "Contact")
	if !ok {
		return
	}

	if err := db.Model(&contact).Update("archived", false).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to unarchive contact").WithError(err))
		return
	}

	contact.Archived = false
	c.JSON(http.StatusOK, contact)
}
