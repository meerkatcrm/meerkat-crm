package services

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"meerkat/carddav"
	"meerkat/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func thumbDataURL(t *testing.T, c color.Color) (string, []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for x := 0; x < 48; x++ {
		for y := 0; y < 48; y++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), buf.Bytes()
}

func thumbTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Contact{}, &models.EmploymentHistory{}))
	return db
}

func TestExportLegacyThumbnailsMovesBlobsToFilesAndIsIdempotent(t *testing.T) {
	db := thumbTestDB(t)
	dir := filepath.Join(t.TempDir(), "photos")
	url1, raw1 := thumbDataURL(t, color.RGBA{R: 200, A: 255})
	url2, raw2 := thumbDataURL(t, color.RGBA{B: 200, A: 255})
	live := models.Contact{UserID: 1, Firstname: "Live", LegacyThumbnail: url1}
	trashed := models.Contact{UserID: 2, Firstname: "Trashed", LegacyThumbnail: url2}
	none := models.Contact{UserID: 1, Firstname: "None"}
	require.NoError(t, db.Create(&[]*models.Contact{&live, &trashed, &none}).Error)
	require.NoError(t, db.Delete(&trashed).Error)

	n, err := ExportLegacyThumbnails(db, dir)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	var got []models.Contact
	require.NoError(t, db.Unscoped().Order("id").Find(&got).Error)
	require.Len(t, got, 3)
	for i, want := range [][]byte{raw1, raw2, nil} {
		if want == nil {
			assert.Empty(t, got[i].PhotoThumbnailFile)
			continue
		}
		require.NotEmpty(t, got[i].PhotoThumbnailFile)
		assert.Regexp(t, `^[0-9a-f-]{36}_thumb\.jpg$`, got[i].PhotoThumbnailFile)
		data, err := os.ReadFile(filepath.Join(dir, got[i].PhotoThumbnailFile))
		require.NoError(t, err)
		assert.Equal(t, want, data)
	}
	// the blob stays: it is the copy a downgrade needs
	assert.Equal(t, url1, got[0].LegacyThumbnail)

	// second run: nothing to do, files untouched
	before, _ := os.ReadDir(dir)
	n, err = ExportLegacyThumbnails(db, dir)
	require.NoError(t, err)
	assert.Zero(t, n)
	after, _ := os.ReadDir(dir)
	assert.Equal(t, len(before), len(after))
}

func TestExportLegacyThumbnailsSkipsBadRowsAndKeepsGoing(t *testing.T) {
	db := thumbTestDB(t)
	dir := t.TempDir()
	good, _ := thumbDataURL(t, color.RGBA{G: 200, A: 255})
	rows := []models.Contact{
		{UserID: 1, Firstname: "NotBase64", LegacyThumbnail: "data:image/jpeg;base64,!!!"},
		{UserID: 1, Firstname: "NoComma", LegacyThumbnail: "data:image/jpeg"},
		{UserID: 1, Firstname: "Legacy file name", LegacyThumbnail: "old_thumb.jpg"},
		{UserID: 1, Firstname: "Good", LegacyThumbnail: good},
		{UserID: 1, Firstname: "Has file", LegacyThumbnail: good, PhotoThumbnailFile: "existing_thumb.jpg"},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}
	n, err := ExportLegacyThumbnails(db, dir)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var byName = map[string]models.Contact{}
	var all []models.Contact
	require.NoError(t, db.Find(&all).Error)
	for _, c := range all {
		byName[c.Firstname] = c
	}
	assert.Empty(t, byName["NotBase64"].PhotoThumbnailFile)
	assert.Empty(t, byName["NoComma"].PhotoThumbnailFile)
	assert.Empty(t, byName["Legacy file name"].PhotoThumbnailFile)
	assert.NotEmpty(t, byName["Good"].PhotoThumbnailFile)
	assert.Equal(t, "existing_thumb.jpg", byName["Has file"].PhotoThumbnailFile, "an existing file is never replaced")
}

func TestRemoveContactPhotoFilesAlsoRemovesTheThumbnailFile(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a_photo.jpg", "a_thumb.jpg", "keep.jpg"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600))
	}
	carddav.RemoveContactPhotoFiles(dir, models.Contact{Photo: "a_photo.jpg", PhotoThumbnailFile: "a_thumb.jpg", LegacyThumbnail: "data:image/jpeg;base64,AA"})
	entries, _ := os.ReadDir(dir)
	require.Len(t, entries, 1)
	assert.Equal(t, "keep.jpg", entries[0].Name())

	// a name outside the directory is refused
	outside := filepath.Join(filepath.Dir(dir), "outside_thumb.jpg")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))
	t.Cleanup(func() { os.Remove(outside) })
	carddav.RemoveContactPhotoFiles(dir, models.Contact{PhotoThumbnailFile: "../outside_thumb.jpg"})
	_, err := os.Stat(outside)
	assert.NoError(t, err)
}
