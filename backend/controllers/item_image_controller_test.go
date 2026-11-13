package controllers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"meerkat/config"
	"meerkat/models"
	"net/http"
	"net/http/httptest"
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
	mountNotes(router)
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

func TestItemImage_DemoModeBlocksUploadAndDelete(t *testing.T) {
	f := setupItemImageFixture(t)
	require.NoError(t, f.db.Model(&f.note).Update("image", "seeded_image.jpg").Error)
	cfg := &config.Config{ProfilePhotoDir: f.dir, DemoMode: true}
	router := routerAs(f.db, f.user.ID, *cfg)
	router.POST("/notes/:id/image", func(c *gin.Context) { UploadItemImage(c, cfg, NoteImages) })
	router.DELETE("/notes/:id/image", func(c *gin.Context) { DeleteItemImage(c, cfg, NoteImages) })
	path := fmt.Sprintf("/notes/%d/image", f.note.ID)

	assert.Equal(t, http.StatusForbidden, doUpload(t, router, path, "image", "a.png", encodePNG(t, sidesAndCenterImage(10, 10))).Code)
	assert.Equal(t, http.StatusForbidden, doRequest(router, http.MethodDelete, path, nil, "").Code)
	assert.Equal(t, "seeded_image.jpg", f.noteImage(t))
}

func TestItemImage_DeletingAMissingImageIsNotFound(t *testing.T) {
	f := setupItemImageFixture(t)
	var before models.Note
	require.NoError(t, f.db.First(&before, f.note.ID).Error)

	w := doRequest(f.router, http.MethodDelete, fmt.Sprintf("/notes/%d/image", f.note.ID), nil, "")

	assert.Equal(t, http.StatusNotFound, w.Code)
	var after models.Note
	require.NoError(t, f.db.First(&after, f.note.ID).Error)
	assert.True(t, before.UpdatedAt.Equal(after.UpdatedAt), "a no-op delete must not touch the note")
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
	mountNotes(router)

	w := doJSON(t, router, http.MethodPut, fmt.Sprintf("/notes/%d", f.note.ID), map[string]any{"content": "edited", "date": time.Now(), "contact_id": f.contact.ID})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "kept_image.jpg", f.noteImage(t))
}

// Image changes fire the item's update webhook with the current filename, so
// consumers are not left with the one a preceding PUT carried.
func TestItemImage_ChangesFireTheUpdateWebhook(t *testing.T) {
	f := setupItemImageFixture(t)
	received := make(chan map[string]any, 8)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload
	}))
	defer hook.Close()
	require.NoError(t, f.db.Create(&models.Webhook{
		UserID: f.user.ID, Name: "test", URL: hook.URL, Secret: "s", IsActive: true,
		Events: []string{"note.updated", "activity.updated"},
	}).Error)

	next := func() map[string]any {
		t.Helper()
		select {
		case payload := <-received:
			return payload
		case <-time.After(5 * time.Second):
			t.Fatal("no webhook delivered")
			return nil
		}
	}

	for _, tc := range []struct {
		path  string
		event string
	}{
		{fmt.Sprintf("/notes/%d/image", f.note.ID), "note.updated"},
		{fmt.Sprintf("/activities/%d/image", f.activity.ID), "activity.updated"},
	} {
		w := doUpload(t, f.router, tc.path, "image", "a.png", encodePNG(t, sidesAndCenterImage(10, 10)))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		image := decodeJSON[map[string]string](t, w)["image"]

		payload := next()
		assert.Equal(t, tc.event, payload["event"])
		assert.Equal(t, image, payload["data"].(map[string]any)["image"], "upload sends the new filename")

		require.Equal(t, http.StatusOK, doRequest(f.router, http.MethodDelete, tc.path, nil, "").Code)
		payload = next()
		assert.Equal(t, tc.event, payload["event"])
		assert.Equal(t, "", payload["data"].(map[string]any)["image"], "removal sends no filename")
	}
}

func TestItemImage_StaleChangeIsRejectedAndKeepsTheOtherImage(t *testing.T) {
	f := setupItemImageFixture(t)
	// Another request stored its image after this one read the old (empty) filename.
	require.NoError(t, f.db.Model(&f.note).Update("image", "other_image.jpg").Error)
	var err error
	router := routerAs(f.db, f.user.ID, config.Config{})
	router.POST("/notes/:id/stale", func(c *gin.Context) { err = NoteImages.setImage(c, f.db, "", "mine_image.jpg") })

	doRequest(router, http.MethodPost, fmt.Sprintf("/notes/%d/stale", f.note.ID), nil, "")

	assert.ErrorIs(t, err, errImageChanged)
	assert.Equal(t, "other_image.jpg", f.noteImage(t), "the other request's image stays referenced")
}

// rotatedHEICBase64 is a 40x60 upright picture with a red band at the top,
// stored sideways (60x40 pixels) with a "rotate 90" transform, as iPhones do.
// Made with sips from a JPEG with EXIF orientation 6.
const rotatedHEICBase64 = "AAAAJGZ0eXBoZWljAAAAAG1pZjFNaVBybWlhZk1pSEJoZWljAAABw21ldGEAAAAAAAAAIWhkbHIAAAAAAAAAAHBpY3QAAAAAAAAAAAAAAAAAAAAAJGRpbmYAAAAcZHJlZgAAAAAAAAABAAAADHVybCAAAAABAAAADnBpdG0AAAAAAAEAAAA4aWluZgAAAAAAAgAAABVpbmZlAgAAAAABAABodmMxAAAAABVpbmZlAgAAAQACAABFeGlmAAAAABppcmVmAAAAAAAAAA5jZHNjAAIAAQABAAAA5mlwcnAAAADFaXBjbwAAABNjb2xybmNseAACAAIABoAAAAAMY2xsaQDLAEAAAAAUaXNwZQAAAAAAAAA8AAAAKAAAAAlpcm90AwAAABBwaXhpAAAAAAMICAgAAABxaHZjQwEDcAAAALAAAAAAAB7wAPz9+PgAAAsDoAABABdAAQwB//8DcAAAAwCwAAADAAADAB5wJKEAAQAjQgEBA3AAAAMAsAAAAwAAAwAeoBQgQcGcbiHuRZVNwICBgCCiAAEACUQBwGFyyEBTJAAAABlpcG1hAAAAAAAAAAEAAQaBAgMFhoQAAAAsaWxvYwAAAABEAAACAAEAAAABAAACTwAAAPUAAgAAAAEAAAH3AAAAWAAAAAFtZGF0AAAAAAAAAV0AAAAGRXhpZgAATU0AKgAAAAgABAESAAMAAAABAAYAAAEaAAUAAAABAAAAPgEbAAUAAAABAAAARgEoAAMAAAABAAEAAAAAAAAAAAABAAAAAQAAAAEAAAABAAAA8SgBr6PUgBpiI/7L1N/N/kK3HkSUZ1tSa/0LZMPq1jPfEDWqBqqFQvy3DG/o8sT9+k08+F/TR/hKo5kaNAgZLjopcWex8SYjQ2L32ZpZWzmauRI2nEsBbbmBQfgfI7OMpV6mmFcMhFtvbNQ+l2dSSIaxRCuuLrMZpNHjKWaMjNX/RMG8AJ8vW5Uzvl1bKvsFAeOIh7fU8/atq4535WPegI5Oy83q8SHOCtECPiT/M+9ifV5IW9ZiqbGzCS37OXZZwKWTIMBJdvxlhjJpLKm0MX/2UcRhypuJxqtYaxNbsLYts73Gdk3KXimNrapAHp94CU0="

// Browsers other than Safari cannot decode HEIC, so the original file is sent
// as is: the server must apply the HEIC rotation itself.
func TestItemImage_HEICRotationIsApplied(t *testing.T) {
	f := setupItemImageFixture(t)
	data, err := base64.StdEncoding.DecodeString(rotatedHEICBase64)
	require.NoError(t, err)

	w := doUpload(t, f.router, fmt.Sprintf("/notes/%d/image", f.note.ID), "image", "phone.heic", data)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	file, err := os.Open(filepath.Join(f.dir, f.noteImage(t)))
	require.NoError(t, err)
	defer file.Close()
	saved, err := jpeg.Decode(file)
	require.NoError(t, err)
	assert.Equal(t, image.Pt(40, 60), saved.Bounds().Size(), "stored upright, not sideways")
	r, g, _, _ := saved.At(20, 3).RGBA()
	assert.Greater(t, r, g, "the red band is at the top")
}
