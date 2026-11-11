package controllers

import (
	"fmt"
	"image"
	"image/jpeg"
	"meerkat/config"
	"meerkat/models"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type itemImageFixture struct {
	db       *gorm.DB
	router   *gin.Engine
	dir      string
	user     models.User
	contact  models.Contact
	note     models.Note
	activity models.Activity
	theirs   models.Note
}

func setupItemImageFixture(t *testing.T) itemImageFixture {
	t.Helper()
	db := newTestDB(t)
	user := newTestUser(t, db, "owner", false)
	stranger := newTestUser(t, db, "stranger", false)
	dir := t.TempDir()
	cfg := &config.Config{ProfilePhotoDir: dir}

	router := routerAs(db, user.ID, *cfg)
	for path, item := range map[string]ImageItem{"notes": NoteImages, "activities": ActivityImages} {
		router.GET("/"+path+"/:id/image", func(c *gin.Context) { GetItemImage(c, cfg, item) })
		router.POST("/"+path+"/:id/image", func(c *gin.Context) { UploadItemImage(c, cfg, item) })
		router.DELETE("/"+path+"/:id/image", func(c *gin.Context) { DeleteItemImage(c, cfg, item) })
	}
	router.DELETE("/notes/:id", DeleteNote)
	router.DELETE("/activities/:id", DeleteActivity)
	router.DELETE("/contacts/:id", DeleteContact)

	contact := newTestContact(t, db, user.ID, "Mine")
	note := models.Note{UserID: user.ID, Content: "card", Date: time.Now(), ContactID: &contact.ID}
	require.NoError(t, db.Create(&note).Error)
	activity := models.Activity{UserID: user.ID, Title: "Dinner", Date: time.Now()}
	require.NoError(t, db.Create(&activity).Error)
	theirs := models.Note{UserID: stranger.ID, Content: "secret", Date: time.Now()}
	require.NoError(t, db.Create(&theirs).Error)

	return itemImageFixture{db: db, router: router, dir: dir, user: user, contact: contact, note: note, activity: activity, theirs: theirs}
}

func (f itemImageFixture) files(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(f.dir)
	require.NoError(t, err)
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func (f itemImageFixture) noteImage(t *testing.T) string {
	t.Helper()
	var note models.Note
	require.NoError(t, f.db.Unscoped().First(&note, f.note.ID).Error)
	return note.Image
}

func TestItemImage_UploadKeepsAspectRatioAndCapsSize(t *testing.T) {
	f := setupItemImageFixture(t)

	w := doUpload(t, f.router, fmt.Sprintf("/activities/%d/image", f.activity.ID), "image", "card.png", encodePNG(t, sidesAndCenterImage(3200, 1000)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var activity models.Activity
	require.NoError(t, f.db.First(&activity, f.activity.ID).Error)
	assert.Equal(t, decodeJSON[map[string]string](t, w)["image"], activity.Image)
	assert.Equal(t, []string{activity.Image}, f.files(t))

	file, err := os.Open(filepath.Join(f.dir, activity.Image))
	require.NoError(t, err)
	defer file.Close()
	saved, err := jpeg.Decode(file)
	require.NoError(t, err)
	assert.Equal(t, image.Pt(1600, 500), saved.Bounds().Size(), "not cropped, longest side capped")

	get := doRequest(f.router, http.MethodGet, fmt.Sprintf("/activities/%d/image", f.activity.ID), nil, "")
	assert.Equal(t, http.StatusOK, get.Code)
	assert.Equal(t, "image/jpeg", get.Header().Get("Content-Type"))
}

func TestItemImage_SmallImageIsNotUpscaled(t *testing.T) {
	f := setupItemImageFixture(t)

	w := doUpload(t, f.router, fmt.Sprintf("/notes/%d/image", f.note.ID), "image", "a.jpg", encodeJPEG(t, sidesAndCenterImage(120, 80)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	file, err := os.Open(filepath.Join(f.dir, f.noteImage(t)))
	require.NoError(t, err)
	defer file.Close()
	saved, err := jpeg.Decode(file)
	require.NoError(t, err)
	assert.Equal(t, image.Pt(120, 80), saved.Bounds().Size())
}

func TestItemImage_ReplacingAndDeletingRemoveFiles(t *testing.T) {
	f := setupItemImageFixture(t)
	path := fmt.Sprintf("/notes/%d/image", f.note.ID)

	require.Equal(t, http.StatusOK, doUpload(t, f.router, path, "image", "a.png", encodePNG(t, sidesAndCenterImage(10, 10))).Code)
	first := f.noteImage(t)
	require.Equal(t, http.StatusOK, doUpload(t, f.router, path, "image", "b.png", encodePNG(t, sidesAndCenterImage(10, 10))).Code)
	second := f.noteImage(t)
	assert.NotEqual(t, first, second)
	assert.Equal(t, []string{second}, f.files(t), "the replaced image is removed")

	require.Equal(t, http.StatusOK, doRequest(f.router, http.MethodDelete, path, nil, "").Code)
	assert.Empty(t, f.noteImage(t))
	assert.Empty(t, f.files(t))
	assert.Equal(t, http.StatusNotFound, doRequest(f.router, http.MethodGet, path, nil, "").Code)
}

func TestItemImage_DeletingTheItemRemovesItsImage(t *testing.T) {
	cases := map[string]func(f itemImageFixture) (upload, remove string){
		"note": func(f itemImageFixture) (string, string) {
			return fmt.Sprintf("/notes/%d/image", f.note.ID), fmt.Sprintf("/notes/%d", f.note.ID)
		},
		"activity": func(f itemImageFixture) (string, string) {
			return fmt.Sprintf("/activities/%d/image", f.activity.ID), fmt.Sprintf("/activities/%d", f.activity.ID)
		},
		"contact of the note": func(f itemImageFixture) (string, string) {
			return fmt.Sprintf("/notes/%d/image", f.note.ID), fmt.Sprintf("/contacts/%d", f.contact.ID)
		},
	}
	for name, paths := range cases {
		t.Run(name, func(t *testing.T) {
			f := setupItemImageFixture(t)
			upload, remove := paths(f)
			require.Equal(t, http.StatusOK, doUpload(t, f.router, upload, "image", "a.png", encodePNG(t, sidesAndCenterImage(10, 10))).Code)
			require.Len(t, f.files(t), 1)

			w := doRequest(f.router, http.MethodDelete, remove, nil, "")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Empty(t, f.files(t))
		})
	}
}

func TestItemImage_RejectsBadUploads(t *testing.T) {
	f := setupItemImageFixture(t)
	path := fmt.Sprintf("/notes/%d/image", f.note.ID)

	assert.Equal(t, http.StatusBadRequest, doUpload(t, f.router, path, "image", "a.txt", []byte("not an image")).Code)
	assert.Equal(t, http.StatusBadRequest, doUpload(t, f.router, path, "photo", "a.png", encodePNG(t, sidesAndCenterImage(10, 10))).Code, "wrong field name")
	assert.Equal(t, http.StatusBadRequest, doUpload(t, f.router, path, "image", "huge.jpg", make([]byte, 10*1024*1024+1)).Code)
	assert.Empty(t, f.files(t))
	assert.Empty(t, f.noteImage(t))
}

func TestItemImage_AnotherUsersItemIsNotFound(t *testing.T) {
	f := setupItemImageFixture(t)
	require.NoError(t, f.db.Model(&f.theirs).Update("image", "x_image.jpg").Error)
	require.NoError(t, os.WriteFile(filepath.Join(f.dir, "x_image.jpg"), []byte("x"), 0o600))
	path := fmt.Sprintf("/notes/%d/image", f.theirs.ID)

	assert.Equal(t, http.StatusNotFound, doRequest(f.router, http.MethodGet, path, nil, "").Code)
	assert.Equal(t, http.StatusNotFound, doUpload(t, f.router, path, "image", "a.png", encodePNG(t, sidesAndCenterImage(10, 10))).Code)
	assert.Equal(t, http.StatusNotFound, doRequest(f.router, http.MethodDelete, path, nil, "").Code)
	assert.Equal(t, []string{"x_image.jpg"}, f.files(t))
}

func TestItemImage_RefusesPathsOutsideTheDir(t *testing.T) {
	f := setupItemImageFixture(t)
	require.NoError(t, f.db.Model(&f.note).Update("image", "../secret.jpg").Error)

	assert.Equal(t, http.StatusNotFound, doRequest(f.router, http.MethodGet, fmt.Sprintf("/notes/%d/image", f.note.ID), nil, "").Code)
}

func TestItemImage_UpdatingTheNoteKeepsTheImage(t *testing.T) {
	f := setupItemImageFixture(t)
	require.NoError(t, f.db.Model(&f.note).Update("image", "kept_image.jpg").Error)
	router := routerAs(f.db, f.user.ID, config.Config{})
	router.PUT("/notes/:id", func(c *gin.Context) {
		c.Set("validated", &models.NoteInput{Content: "edited", Date: time.Now(), ContactID: &f.contact.ID})
		UpdateNote(c)
	})

	w := doRequest(router, http.MethodPut, fmt.Sprintf("/notes/%d", f.note.ID), nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "kept_image.jpg", f.noteImage(t))
}
