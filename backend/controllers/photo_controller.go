package controllers

import (
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"meerkat/carddav"
	"meerkat/config"
	apperrors "meerkat/errors"
	"meerkat/httputil"
	"meerkat/logger"
	"meerkat/models"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gen2brain/heic"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nfnt/resize"
	"gorm.io/gorm"
)

func GetProfilePicture(c *gin.Context, cfg *config.Config) {
	idParam := c.Param("id")
	contactID, err := strconv.ParseUint(idParam, 10, 64)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrValidation("Invalid contact ID"))
		return
	}
	var contact models.Contact
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Find the contact in the database
	if err := db.Where("user_id = ?", userID).First(&contact, "id = ?", contactID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact"))
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrDatabase("find").WithError(err))
		return
	}

	// Check if thumbnail is requested via query parameter
	wantsThumbnail := c.Query("thumbnail") == "true"

	if wantsThumbnail {
		serveThumbnail(c, cfg, contact)
		return
	}

	// Serve full photo from file using validated config path
	if contact.Photo == "" {
		apperrors.AbortWithError(c, apperrors.ErrNotFound("Photo"))
		return
	}

	// Prevent path traversal attacks
	if strings.Contains(contact.Photo, "..") || filepath.IsAbs(contact.Photo) {
		apperrors.AbortWithError(c, apperrors.ErrValidation("Invalid photo path"))
		return
	}

	filePath := filepath.Join(cfg.ProfilePhotoDir, contact.Photo)
	logger.FromContext(c).Debug().Str("file_path", filePath).Msg("Serving profile picture")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		apperrors.AbortWithError(c, apperrors.ErrNotFound("Image"))
		return
	}

	// The URL names the contact, not the picture, so replacing the photo leaves the URL alone:
	// nothing here may be reused without revalidating, and ServeFile's Last-Modified makes the
	// revalidation a 304.
	c.Header("Cache-Control", "private, no-cache")
	c.File(filePath)
}

func AddPhotoToContact(c *gin.Context, cfg *config.Config) {
	// Check if demo mode is enabled - photo uploads are disabled in demo
	if cfg.DemoMode {
		apperrors.AbortWithError(c, apperrors.ErrForbidden("Photo uploads are disabled in demo mode"))
		return
	}

	idParam := c.Param("id")
	contactID, err := strconv.ParseUint(idParam, 10, 64)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrValidation("Invalid contact ID"))
		return
	}
	var contact models.Contact
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Find the contact in the database
	if err := db.Where("user_id = ?", userID).First(&contact, "id = ?", contactID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact"))
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrDatabase("find").WithError(err))
		return
	}

	// Check if there's an uploaded file
	file, err := c.FormFile("photo")
	if err != nil && !errors.Is(err, http.ErrMissingFile) && !errors.Is(err, http.ErrNotMultipart) {
		// The form could not be read, so a photo was sent but cannot be stored.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apperrors.AbortWithError(c, apperrors.ErrValidation("File too large. Maximum size is 10MB"))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrValidation("The uploaded file could not be read"))
		}
		return
	}
	if err == nil {
		// Validate file size (10MB limit to prevent DoS)
		const maxFileSize = 10 * 1024 * 1024 // 10MB
		if file.Size > maxFileSize {
			apperrors.AbortWithError(c, apperrors.ErrValidation("File too large. Maximum size is 10MB"))
			return
		}

		// Use validated upload directory from config
		if err := os.MkdirAll(cfg.ProfilePhotoDir, os.ModePerm); err != nil {
			apperrors.AbortWithError(c, apperrors.ErrInternal("Failed to create upload directory").WithError(err))
			return
		}
		photoPath, thumbnailPath, err := processAndSavePhoto(file, cfg.ProfilePhotoDir)
		if err != nil {
			if errors.Is(err, errInvalidPhoto) {
				apperrors.AbortWithError(c, apperrors.ErrValidation("Unsupported or damaged image. Use a JPEG, PNG or HEIC file"))
				return
			}
			apperrors.AbortWithError(c, apperrors.ErrInternal("Failed to process photo").WithError(err))
			return
		}
		swap := carddav.NewPhotoSwap(&contact)
		swap.Apply(&contact, photoPath, thumbnailPath)

		// Only the photo columns: a full Save would write back every other field as read
		// above and undo an edit made in the meantime. UpdatedAt still moves, so AfterSave
		// gives the contact a new CardDAV ETag.
		if err := db.Model(&contact).Select("photo", "photo_thumbnail_file", "photo_thumbnail").Updates(map[string]any{
			"photo": contact.Photo, "photo_thumbnail_file": contact.PhotoThumbnailFile, "photo_thumbnail": contact.LegacyThumbnail,
		}).Error; err != nil {
			// Clean up the newly saved files since the DB update failed
			swap.Rollback(cfg.ProfilePhotoDir, &contact)
			apperrors.AbortWithError(c, apperrors.ErrDatabase("update").WithError(err))
			return
		}

		// Delete the old photo files after the db update
		swap.Commit(cfg.ProfilePhotoDir)

		c.JSON(http.StatusOK, contact)
		return
	}

	// No photo uploaded: nothing to change, and writing the contact back as read could
	// undo an edit made in the meantime.
	c.JSON(http.StatusOK, contact)
}

// RemovePhotoFromContact deletes a contact's profile picture without touching the
// rest of the contact. The files are removed only after the database no longer points
// at them, so a failed update leaves the photo intact.
func RemovePhotoFromContact(c *gin.Context, cfg *config.Config) {
	// Photo changes are disabled in demo mode, like uploads.
	if cfg.DemoMode {
		apperrors.AbortWithError(c, apperrors.ErrForbidden("Photo changes are disabled in demo mode"))
		return
	}

	contactID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrValidation("Invalid contact ID"))
		return
	}
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var contact models.Contact
	if err := db.Where("user_id = ?", userID).First(&contact, "id = ?", contactID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact"))
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrDatabase("find").WithError(err))
		return
	}

	if contact.Photo == "" && contact.PhotoThumbnailFile == "" && contact.LegacyThumbnail == "" {
		c.JSON(http.StatusOK, contact)
		return
	}

	old := contact
	// Only the photo columns: a full Save would write back every other field as read
	// above and undo an edit made in the meantime. UpdatedAt still moves, so AfterSave
	// gives the contact a new CardDAV ETag.
	if err := db.Model(&contact).Select("photo", "photo_thumbnail_file", "photo_thumbnail").
		Updates(map[string]any{"photo": "", "photo_thumbnail_file": "", "photo_thumbnail": ""}).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update").WithError(err))
		return
	}

	carddav.RemoveContactPhotoFiles(cfg.ProfilePhotoDir, old)

	c.JSON(http.StatusOK, contact)
}

// serveThumbnail sends the thumbnail file with an ETag and a long private cache -- for the
// URLs that carry the file's unique name (see models.ThumbnailURL), where a new picture is a
// new URL. A contact whose thumbnail was not exported yet still gets the legacy database copy.
func serveThumbnail(c *gin.Context, cfg *config.Config, contact models.Contact) {
	name := contact.PhotoThumbnailFile
	if name == "" {
		serveLegacyThumbnail(c, contact.LegacyThumbnail)
		return
	}
	if strings.Contains(name, "..") || filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		apperrors.AbortWithError(c, apperrors.ErrValidation("Invalid thumbnail path"))
		return
	}
	file, err := os.Open(filepath.Join(cfg.ProfilePhotoDir, name)) //nolint:gosec // name checked above, directory from config
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrNotFound("Thumbnail"))
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		apperrors.AbortWithError(c, apperrors.ErrNotFound("Thumbnail"))
		return
	}
	// The name is unique per picture; size and mtime guard against a file replaced in place.
	c.Header("ETag", fmt.Sprintf(`"%s-%x-%x"`, strings.TrimSuffix(name, ".jpg"), info.Size(), info.ModTime().UnixNano()))
	// The pin is earned by the version in the URL, not by the handler: a request that does not
	// carry it asks at a URL which outlives the picture behind it, so it revalidates against
	// the ETag instead of holding the old face for a year.
	if c.Query("v") == models.ThumbnailVersion(name) {
		c.Header("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		c.Header("Cache-Control", "private, no-cache")
	}
	c.Header("Content-Type", "image/jpeg")
	http.ServeContent(c.Writer, c.Request, "", info.ModTime(), file)
}

func serveLegacyThumbnail(c *gin.Context, legacy string) {
	// Format: data:image/jpeg;base64,<data>
	if !strings.HasPrefix(legacy, "data:") {
		apperrors.AbortWithError(c, apperrors.ErrNotFound("Thumbnail"))
		return
	}
	parts := strings.SplitN(legacy, ",", 2)
	if len(parts) != 2 {
		apperrors.AbortWithError(c, apperrors.ErrInternal("Invalid thumbnail format"))
		return
	}
	imageData, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrInternal("Failed to decode thumbnail").WithError(err))
		return
	}
	c.Header("Cache-Control", "private, no-cache")
	c.Data(http.StatusOK, "image/jpeg", imageData)
}

// processAndSavePhoto processes an uploaded photo and returns:
// - photoPath: filename of the saved full-size photo
// - thumbnailPath: filename of the saved 48px thumbnail
// errInvalidPhoto marks a processing failure caused by the uploaded file rather than the server.
var errInvalidPhoto = errors.New("invalid photo")

func processAndSavePhoto(file *multipart.FileHeader, uploadDir string) (string, string, error) {
	img, err := decodeUploadedImage(file)
	if err != nil {
		return "", "", err
	}

	// Crop to centered square if rectangular
	img = cropToSquare(img)

	// Generate unique filename for full photo
	baseFilename := uuid.New().String()
	photoPath := baseFilename + "_photo.jpg" // Always save as JPG

	// Create the output directory
	if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
		return "", "", err
	}

	// Save the photo as JPG (max 400x400, only downscale)
	const maxPhotoSize = 400
	bounds := img.Bounds()
	photoImg := img
	if bounds.Dx() > maxPhotoSize || bounds.Dy() > maxPhotoSize {
		photoImg = resize.Resize(maxPhotoSize, maxPhotoSize, img, resize.Lanczos3)
	}
	fullPhotoPath := filepath.Join(uploadDir, photoPath)
	if err := saveImage(fullPhotoPath, photoImg); err != nil {
		return "", "", err
	}

	// Thumbnail (48x48) as a file next to the photo
	thumbnailPath := baseFilename + "_thumb.jpg"
	if err := saveImage(filepath.Join(uploadDir, thumbnailPath), resize.Resize(48, 48, img, resize.Lanczos3)); err != nil {
		os.Remove(fullPhotoPath)
		return "", "", err
	}

	return photoPath, thumbnailPath, nil
}

// decodeUploadedImage decodes an uploaded JPEG, PNG or HEIC file. Failures caused
// by the file itself wrap errInvalidPhoto.
func decodeUploadedImage(file *multipart.FileHeader) (image.Image, error) {
	src, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	// Detect the image format
	buf := make([]byte, 512)
	_, err = src.Read(buf)
	if errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: empty file", errInvalidPhoto)
	}
	if err != nil {
		return nil, err
	}
	contentType := http.DetectContentType(buf)

	// Check for HEIC/HEIF format (http.DetectContentType doesn't recognize it)
	// HEIC files have "ftyp" followed by "heic", "heix", "hevc", "hevx", or "mif1" at byte 4
	if contentType == "application/octet-stream" && len(buf) >= 12 {
		if string(buf[4:8]) == "ftyp" {
			brand := string(buf[8:12])
			if brand == "heic" || brand == "heix" || brand == "hevc" || brand == "hevx" || brand == "mif1" {
				contentType = "image/heic"
			}
		}
	}

	// Validate supported content types
	if contentType != "image/jpeg" && contentType != "image/png" &&
		contentType != "image/heic" && contentType != "image/heif" {
		return nil, fmt.Errorf("%w: unsupported file format", errInvalidPhoto)
	}

	// Check the declared size before decoding allocates memory for it.
	src.Seek(0, 0)
	if err := httputil.CheckImageDimensions(src); err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidPhoto, err)
	}

	// Rewind the file reader
	src.Seek(0, 0)

	// Decode the image (handle JPEG, PNG, and HEIC)
	var img image.Image
	switch contentType {
	case "image/jpeg":
		img, err = jpeg.Decode(src)
	case "image/png":
		img, err = png.Decode(src)
	case "image/heic", "image/heif":
		img, err = heic.Decode(src)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidPhoto, err)
	}
	return img, nil
}

// cropToSquare crops an image to a centered square
// If the image is already square, it returns the original image unchanged
func cropToSquare(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Already square, return as-is
	if width == height {
		return img
	}

	// Calculate the size of the square (use the smaller dimension)
	size := width
	if height < width {
		size = height
	}

	// Calculate crop offset to center the square
	offsetX := (width - size) / 2
	offsetY := (height - size) / 2

	// Create a new RGBA image for the cropped result
	cropped := image.NewRGBA(image.Rect(0, 0, size, size))

	// Copy the centered square region
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			cropped.Set(x, y, img.At(bounds.Min.X+offsetX+x, bounds.Min.Y+offsetY+y))
		}
	}

	return cropped
}

func saveImage(path string, img image.Image) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	// Always encode as JPEG
	return jpeg.Encode(out, img, &jpeg.Options{Quality: 85})
}

// ProxyImage fetches an image from a URL and returns it to the client.
// This is used to work around CORS restrictions when fetching images from external URLs.
func ProxyImage(c *gin.Context) {
	imageURL := c.Query("url")
	if imageURL == "" {
		apperrors.AbortWithError(c, apperrors.ErrMissingField("url"))
		return
	}

	// Use the shared SSRF-protected fetch function
	body, contentType, err := httputil.FetchImageFromURL(c.Request.Context(), imageURL)
	if err != nil {
		logger.FromContext(c).Warn().Err(err).Str("url", imageURL).Msg("Failed to fetch image from URL")
		apperrors.AbortWithError(c, apperrors.ErrExternal("image proxy", "Failed to fetch image").WithError(err))
		return
	}

	// Never pass the remote bytes on: decode the image and encode it again as JPEG.
	photo, err := carddav.ReencodePhoto(body, contentType)
	if err != nil {
		logger.FromContext(c).Warn().Err(err).Str("url", imageURL).Msg("Fetched image could not be decoded")
		apperrors.AbortWithError(c, apperrors.ErrExternal("image proxy", "Failed to fetch image").WithError(err))
		return
	}

	c.Data(http.StatusOK, "image/jpeg", photo)
}
