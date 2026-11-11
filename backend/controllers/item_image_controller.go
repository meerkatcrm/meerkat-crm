package controllers

import (
	"errors"
	"meerkat/config"
	apperrors "meerkat/errors"
	"meerkat/logger"
	"meerkat/models"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nfnt/resize"
	"gorm.io/gorm"
)

// Notes and activities can carry one optional image each. The image is a JPEG
// in the profile photo directory; the row's image column holds its filename.

// maxItemImageSize caps the longest side of a stored note or activity image.
const maxItemImageSize = 1600

// ImageItem names a model with an image column, such as &models.Note{}.
type ImageItem struct {
	Model any
	Name  string
}

var (
	NoteImages     = ImageItem{Model: &models.Note{}, Name: "Note"}
	ActivityImages = ImageItem{Model: &models.Activity{}, Name: "Activity"}
)

// ownedImage returns the image filename of the caller's item named by :id.
func (it ImageItem) ownedImage(c *gin.Context, db *gorm.DB) (string, bool) {
	userID, ok := currentUserID(c)
	if !ok {
		return "", false
	}
	var images []string
	if err := db.Model(it.Model).Where("id = ? AND user_id = ?", c.Param("id"), userID).Pluck("image", &images).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("find").WithError(err))
		return "", false
	}
	if len(images) == 0 {
		apperrors.AbortWithError(c, apperrors.ErrNotFound(it.Name))
		return "", false
	}
	return images[0], true
}

func (it ImageItem) setImage(c *gin.Context, db *gorm.DB, image string) error {
	userID, _ := currentUserID(c)
	return db.Model(it.Model).Where("id = ? AND user_id = ?", c.Param("id"), userID).Update("image", image).Error
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

	if err := it.setImage(c, db, image); err != nil {
		removeItemImageFile(c, cfg.ProfilePhotoDir, image)
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update").WithError(err))
		return
	}
	removeItemImageFile(c, cfg.ProfilePhotoDir, oldImage)
	c.JSON(http.StatusOK, gin.H{"image": image})
}

// DeleteItemImage removes the image of a note or activity.
func DeleteItemImage(c *gin.Context, cfg *config.Config, it ImageItem) {
	db := c.MustGet("db").(*gorm.DB)
	image, ok := it.ownedImage(c, db)
	if !ok {
		return
	}
	if err := it.setImage(c, db, ""); err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update").WithError(err))
		return
	}
	removeItemImageFile(c, cfg.ProfilePhotoDir, image)
	c.JSON(http.StatusOK, gin.H{"image": ""})
}

// removeItemImageFile deletes a note or activity image from disk. Call it only
// after the database no longer refers to the file.
func removeItemImageFile(c *gin.Context, dir, image string) {
	if dir == "" || image == "" || strings.Contains(image, "..") || filepath.IsAbs(image) {
		return
	}
	if err := os.Remove(filepath.Join(dir, image)); err != nil && !os.IsNotExist(err) {
		logger.FromContext(c).Warn().Err(err).Str("image", image).Msg("Failed to delete image")
	}
}
