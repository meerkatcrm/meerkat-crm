package controllers

import (
	"errors"
	apperrors "meerkat/errors"
	"meerkat/middleware"
	"meerkat/models"
	"meerkat/services"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const legacyThumbnailColumn = "photo_thumbnail"

// bulkColumns are the columns a bulk update writes: what it can change, the address
// scalar derived from the addresses, and the update time the ETag is made from.
var bulkColumns = []string{
	"circles", "how_we_met", "food_preference", "work_information",
	"organization", "department", "job_title", "role",
	"addresses", "address", "archived", "updated_at",
}

// bulkTextField pairs a bulk-update input value with the contact field it sets.
type bulkTextField struct {
	name  string
	value *string
	set   func(*models.Contact, string)
}

// BulkUpdateContacts applies one change (circles added or removed, text fields set,
// archived or restored) to every listed contact of the caller, all or nothing.
func BulkUpdateContacts(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	input, appErr := middleware.GetValidated[models.ContactBulkUpdateInput](c)
	if appErr != nil {
		apperrors.AbortWithError(c, appErr)
		return
	}

	fields := []bulkTextField{
		{"how_we_met", input.HowWeMet, func(ct *models.Contact, v string) { ct.HowWeMet = v }},
		{"food_preference", input.FoodPreference, func(ct *models.Contact, v string) { ct.FoodPreference = v }},
		{"work_information", input.WorkInformation, func(ct *models.Contact, v string) { ct.WorkInformation = v }},
		{"organization", input.Organization, func(ct *models.Contact, v string) { ct.Organization = v }},
		{"department", input.Department, func(ct *models.Contact, v string) { ct.Department = v }},
		{"job_title", input.JobTitle, func(ct *models.Contact, v string) { ct.JobTitle = v }},
		{"role", input.Role, func(ct *models.Contact, v string) { ct.Role = v }},
		{"city", input.City, setPrimaryCity},
	}

	addCircles := cleanCircles(input.AddCircles)
	removeCircles := cleanCircles(input.RemoveCircles)
	changes := len(addCircles) + len(removeCircles)
	if input.Archived != nil {
		changes++
	}
	for _, f := range fields {
		if f.value == nil {
			continue
		}
		if strings.TrimSpace(*f.value) == "" {
			apperrors.AbortWithError(c, apperrors.ErrInvalidInput(f.name, "must not be blank"))
			return
		}
		changes++
	}
	if changes == 0 {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("request", "no change given"))
		return
	}
	for _, circle := range addCircles {
		if containsCircle(removeCircles, circle) {
			apperrors.AbortWithError(c, apperrors.ErrInvalidInput("circles", "a circle can't be both added and removed"))
			return
		}
	}

	ids := slices.Clone(input.ContactIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)

	var contacts, changed []models.Contact
	err := db.Transaction(func(tx *gorm.DB) error {
		// The legacy base64 thumbnail column is not needed here.
		if err := tx.Omit(legacyThumbnailColumn).Where("user_id = ? AND id IN ?", userID, ids).Find(&contacts).Error; err != nil {
			return apperrors.ErrDatabase("Failed to load contacts").WithError(err)
		}
		if len(contacts) != len(ids) {
			// A contact the user deleted since selecting it can't be edited and is
			// skipped. Any other id is unknown or someone else's: change none rather than some.
			var deleted int64
			if err := tx.Unscoped().Model(&models.Contact{}).
				Where("user_id = ? AND id IN ? AND deleted_at IS NOT NULL", userID, ids).
				Count(&deleted).Error; err != nil {
				return apperrors.ErrDatabase("Failed to load contacts").WithError(err)
			}
			if len(contacts)+int(deleted) != len(ids) {
				return apperrors.ErrNotFound("Contact")
			}
		}
		for i := range contacts {
			contact := &contacts[i]
			before := bulkSnapshotOf(contact)
			contact.Circles = applyCircleChanges(contact.Circles, addCircles, removeCircles)
			for _, f := range fields {
				if f.value != nil {
					f.set(contact, strings.TrimSpace(*f.value))
				}
			}
			if input.Archived != nil {
				if *input.Archived && !contact.Archived {
					// Archiving a single contact also drops its reminders.
					if err := tx.Where("contact_id = ? AND user_id = ?", contact.ID, userID).Delete(&models.Reminder{}).Error; err != nil {
						return apperrors.ErrDatabase("Failed to archive contacts").WithError(err)
					}
				}
				contact.Archived = *input.Archived
			}
			// A contact that already holds every value is left alone: saving it would
			// still bump its ETag and send a contact.updated webhook.
			if reflect.DeepEqual(before, bulkSnapshotOf(contact)) {
				continue
			}
			// Only the columns a bulk update can change are written, so an edit made
			// elsewhere to any other field in the meantime is never reverted. The model's
			// hooks still run: the address scalar, the ETag and the employment history.
			if err := tx.Model(contact).Select(bulkColumns).Updates(contact).Error; err != nil {
				return apperrors.ErrDatabase("Failed to update contacts").WithError(err)
			}
			changed = append(changed, *contact)
		}
		return nil
	})
	if err != nil {
		var ae *apperrors.AppError
		if errors.As(err, &ae) {
			apperrors.AbortWithError(c, ae)
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to update contacts").WithError(err))
		}
		return
	}

	cfg, clk := currentConfig(c), middleware.RequestClock(c)
	if len(changed) > 0 {
		go func() {
			for _, contact := range changed {
				services.TriggerWebhooks(db, cfg, userID, "contact.updated", contact, clk)
			}
		}()
	}

	c.JSON(http.StatusOK, gin.H{"updated": len(changed)})
}

// bulkSnapshot holds the values a bulk update can change.
type bulkSnapshot struct {
	Circles                                   []string
	HowWeMet, FoodPreference, WorkInformation string
	Organization, Department, JobTitle, Role  string
	Addresses                                 []models.ContactAddress
	Archived                                  bool
}

func bulkSnapshotOf(c *models.Contact) bulkSnapshot {
	circles := c.Circles
	if len(circles) == 0 {
		circles = nil // a stored [] and a nil list are the same circles
	}
	return bulkSnapshot{
		Circles: circles, HowWeMet: c.HowWeMet, FoodPreference: c.FoodPreference,
		WorkInformation: c.WorkInformation, Organization: c.Organization, Department: c.Department,
		JobTitle: c.JobTitle, Role: c.Role, Addresses: slices.Clone(c.Addresses), Archived: c.Archived,
	}
}

// containsCircle reports whether the list has the circle. Circle names that differ
// only in case are the same circle, as the unique_circles validator of a single
// contact save treats them: a bulk edit must not write a list that one rejects.
func containsCircle(circles []string, circle string) bool {
	return slices.ContainsFunc(circles, func(c string) bool { return strings.EqualFold(c, circle) })
}

// cleanCircles trims the names and drops blanks and repeats.
func cleanCircles(circles []string) []string {
	out := make([]string, 0, len(circles))
	for _, circle := range circles {
		circle = strings.TrimSpace(circle)
		if circle != "" && !containsCircle(out, circle) {
			out = append(out, circle)
		}
	}
	return out
}

// applyCircleChanges keeps the contact's circles in their order, drops the removed
// ones and appends the added ones it doesn't have yet, keeping the casing a
// circle already has on the contact.
func applyCircleChanges(circles, add, remove []string) []string {
	out := make([]string, 0, len(circles)+len(add))
	for _, circle := range circles {
		if !containsCircle(remove, circle) && !containsCircle(out, circle) {
			out = append(out, circle)
		}
	}
	for _, circle := range add {
		if !containsCircle(out, circle) {
			out = append(out, circle)
		}
	}
	return out
}

// setPrimaryCity sets the city of the contact's first address, adding a home
// address with the city when it has none. A legacy one-line address with no
// structured entry is kept as that address's street rather than overwritten.
func setPrimaryCity(contact *models.Contact, city string) {
	if len(contact.Addresses) == 0 {
		contact.Addresses = []models.ContactAddress{{Type: "home", Street: contact.Address, City: city}}
		return
	}
	addresses := slices.Clone(contact.Addresses)
	addresses[0].City = city
	contact.Addresses = addresses
}
