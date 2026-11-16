package controllers

import (
	"fmt"
	apperrors "meerkat/errors"
	"meerkat/logger"
	"meerkat/services"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ImportData re-imports a file produced by GET /export into the caller's account.
// Multipart form field "file" holds the export CSV; ?dry_run=true reports what the
// import would do without saving anything.
func ImportData(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	log := logger.FromContext(c)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("file", "No file uploaded"))
		return
	}
	if file.Size > services.MaxDataImportSize {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("file", fmt.Sprintf("File too large. Maximum size is %d MB", services.MaxDataImportSize/(1024*1024))))
		return
	}
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".csv") {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("file", "File must be a CSV file"))
		return
	}
	// An unreadable value must not fall back to a real import.
	dryRun := false
	if raw := c.Query("dry_run"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			apperrors.AbortWithError(c, apperrors.ErrInvalidInput("dry_run", "dry_run must be true or false"))
			return
		}
		dryRun = parsed
	}

	f, err := file.Open()
	if err != nil {
		log.Error().Err(err).Msg("Failed to open uploaded data export")
		apperrors.AbortWithError(c, apperrors.ErrInternal("Failed to process file"))
		return
	}
	defer f.Close()

	parsed, err := services.ParseDataExport(f)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("file", err.Error()))
		return
	}

	result, err := services.ImportDataExport(db, userID, parsed, dryRun)
	if err != nil {
		log.Error().Err(err).Msg("Data import failed")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Import failed").WithError(err))
		return
	}

	log.Info().
		Bool("dry_run", dryRun).
		Int("contacts_created", result.Contacts.Created).
		Int("contacts_existing", result.Contacts.Existing).
		Int("activities_created", result.Activities.Created).
		Int("notes_created", result.Notes.Created).
		Int("reminders_created", result.Reminders.Created).
		Int("relationships_created", result.Relationships.Created).
		Msg("Data import completed")

	c.JSON(http.StatusOK, result)
}
