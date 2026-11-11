package controllers

import (
	"errors"
	"meerkat/carddav"
	"meerkat/config"
	apperrors "meerkat/errors"
	"meerkat/logger"
	"meerkat/models"
	"meerkat/services"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nfnt/resize"
	"gorm.io/gorm"
)

// Notes and activities can carry one optional image each. The image is a JPEG
// in the profile photo directory; the row's image column holds its filename.

// maxItemImageSize caps the longest side of a stored note or activity image.
const maxItemImageSize = 1600

// ImageItem names a soft-deletable table with an image column. A table name
// rather than a shared model value: GORM writes updates back into a model.
type ImageItem struct {
	Table string
	Name  string
	// Event is the webhook fired when the image changes, with load's result as
	// payload: the same item an update through the regular endpoint would send.
	Event string
	load  func(db *gorm.DB, id string, userID uint) (any, error)
}

var (
	NoteImages = ImageItem{Table: "notes", Name: "Note", Event: "note.updated",
		load: func(db *gorm.DB, id string, userID uint) (any, error) {
			var note models.Note
			err := db.Where("user_id = ?", userID).First(&note, "id = ?", id).Error
			return note, err
		}}
	ActivityImages = ImageItem{Table: "activities", Name: "Activity", Event: "activity.updated",
		load: func(db *gorm.DB, id string, userID uint) (any, error) {
			var activity models.Activity
			err := db.Preload("Contacts", "contacts.user_id = ?", userID).Where("user_id = ?", userID).First(&activity, "id = ?", id).Error
			return activity, err
		}}
)

// notifyImageChanged fires the item's update webhook, so consumers learn the
// new filename (a PUT that preceded the image change carried the old one).
func (it ImageItem) notifyImageChanged(c *gin.Context, db *gorm.DB) {
	userID, _ := currentUserID(c)
	item, err := it.load(db, c.Param("id"), userID)
	if err != nil {
		logger.FromContext(c).Warn().Err(err).Str("item", it.Name).Msg("Failed to load item for webhook after image change")
		return
	}
	go services.TriggerWebhooks(db, currentConfig(c), userID, it.Event, item)
}

func (it ImageItem) owned(db *gorm.DB, c *gin.Context, userID uint) *gorm.DB {
	return db.Table(it.Table).Where("id = ? AND user_id = ? AND deleted_at IS NULL", c.Param("id"), userID)
}

// ownedImage returns the image filename of the caller's item named by :id.
func (it ImageItem) ownedImage(c *gin.Context, db *gorm.DB) (string, bool) {
	userID, ok := currentUserID(c)
	if !ok {
		return "", false
	}
	var images []string
	if err := it.owned(db, c, userID).Pluck("image", &images).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("find").WithError(err))
		return "", false
	}
	if len(images) == 0 {
		apperrors.AbortWithError(c, apperrors.ErrNotFound(it.Name))
		return "", false
	}
	return images[0], true
}

// errImageChanged means another request changed the image in the meantime.
var errImageChanged = errors.New("image changed concurrently")

// setImage replaces the image filename only if it is still oldImage, so of two
// concurrent changes one fails instead of leaving the other's file orphaned.
// gorm.ErrRecordNotFound means the item was deleted since it was looked up.
func (it ImageItem) setImage(c *gin.Context, db *gorm.DB, oldImage, image string) error {
	userID, _ := currentUserID(c)
	result := it.owned(db, c, userID).Where("image = ?", oldImage).
		Updates(map[string]any{"image": image, "updated_at": time.Now()})
	if result.Error != nil || result.RowsAffected > 0 {
		return result.Error
	}
	var count int64
	if err := it.owned(db, c, userID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return errImageChanged
}

func abortSetImage(c *gin.Context, it ImageItem, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		apperrors.AbortWithError(c, apperrors.ErrNotFound(it.Name))
		return
	}
	if errors.Is(err, errImageChanged) {
		apperrors.AbortWithError(c, apperrors.ErrConflict("The image was changed by another request. Reload and try again"))
		return
	}
	apperrors.AbortWithError(c, apperrors.ErrDatabase("update").WithError(err))
}

// GetItemImage serves the image of a note or activity.
func GetItemImage(c *gin.Context, cfg *config.Config, it ImageItem) {
	db := c.MustGet("db").(*gorm.DB)
	image, ok := it.ownedImage(c, db)
	if !ok {
		return
	}
	if image == "" || strings.Contains(image, "..") || filepath.IsAbs(image) {
		apperrors.AbortWithError(c, apperrors.ErrNotFound("Image"))
		return
	}
	filePath := filepath.Join(cfg.ProfilePhotoDir, image)
	if _, err := os.Stat(filePath); err != nil {
		apperrors.AbortWithError(c, apperrors.ErrNotFound("Image"))
		return
	}
	c.File(filePath)
}

// UploadItemImage sets or replaces the image of a note or activity from the
// multipart field "image".
func UploadItemImage(c *gin.Context, cfg *config.Config, it ImageItem) {
	if cfg.DemoMode {
		apperrors.AbortWithError(c, apperrors.ErrForbidden("Photo uploads are disabled in demo mode"))
		return
	}
	db := c.MustGet("db").(*gorm.DB)
	oldImage, ok := it.ownedImage(c, db)
	if !ok {
		return
	}

	file, err := c.FormFile("image")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apperrors.AbortWithError(c, apperrors.ErrValidation("File too large. Maximum size is 10MB"))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrMissingField("image"))
		}
		return
	}
	const maxFileSize = 10 * 1024 * 1024
	if file.Size > maxFileSize {
		apperrors.AbortWithError(c, apperrors.ErrValidation("File too large. Maximum size is 10MB"))
		return
	}

	img, err := decodeUploadedImage(file)
	if err != nil {
		if errors.Is(err, errInvalidPhoto) {
			apperrors.AbortWithError(c, apperrors.ErrValidation("Unsupported or damaged image. Use a JPEG, PNG or HEIC file"))
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrInternal("Failed to process image").WithError(err))
		return
	}

	if err := os.MkdirAll(cfg.ProfilePhotoDir, os.ModePerm); err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInternal("Failed to create upload directory").WithError(err))
		return
	}
	// Thumbnail keeps the aspect ratio and never upscales.
	img = resize.Thumbnail(maxItemImageSize, maxItemImageSize, img, resize.Lanczos3)
	image := uuid.New().String() + "_image.jpg"
	if err := saveImage(filepath.Join(cfg.ProfilePhotoDir, image), img); err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInternal("Failed to save image").WithError(err))
		return
	}

	if err := it.setImage(c, db, oldImage, image); err != nil {
		carddav.RemoveContactPhotoFile(cfg.ProfilePhotoDir, image)
		abortSetImage(c, it, err)
		return
	}
	carddav.RemoveContactPhotoFile(cfg.ProfilePhotoDir, oldImage)
	it.notifyImageChanged(c, db)
	c.JSON(http.StatusOK, gin.H{"image": image})
}

// DeleteItemImage removes the image of a note or activity.
func DeleteItemImage(c *gin.Context, cfg *config.Config, it ImageItem) {
	db := c.MustGet("db").(*gorm.DB)
	image, ok := it.ownedImage(c, db)
	if !ok {
		return
	}
	if err := it.setImage(c, db, image, ""); err != nil {
		abortSetImage(c, it, err)
		return
	}
	carddav.RemoveContactPhotoFile(cfg.ProfilePhotoDir, image)
	it.notifyImageChanged(c, db)
	c.JSON(http.StatusOK, gin.H{"image": ""})
}
