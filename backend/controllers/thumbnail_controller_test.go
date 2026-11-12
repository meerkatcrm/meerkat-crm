package controllers

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"meerkat/config"
	"meerkat/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type thumbEnv struct {
	db    *gorm.DB
	r     *gin.Engine
	me    models.User
	other models.User
	dir   string
}

func newThumbEnv(t *testing.T) *thumbEnv {
	t.Helper()
	db, r := setupRouter()
	e := &thumbEnv{db: db, r: r, dir: t.TempDir()}
	require.NoError(t, db.First(&e.me).Error)
	e.other = models.User{Username: "other", Email: "o@example.com", Password: "x"}
	require.NoError(t, db.Create(&e.other).Error)
	cfg := &config.Config{ProfilePhotoDir: e.dir}
	r.GET("/contacts", GetContacts)
	r.GET("/contacts/:id/picture", func(c *gin.Context) { GetProfilePicture(c, cfg) })
	r.POST("/contacts/:id/picture", func(c *gin.Context) { AddPhotoToContact(c, cfg) })
	r.GET("/graph", GetGraph)
	return e
}

func (e *thumbEnv) get(path string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func TestThumbnailIsServedFromItsFileWithETagAndCache(t *testing.T) {
	e := newThumbEnv(t)
	require.NoError(t, os.WriteFile(filepath.Join(e.dir, "abc_thumb.jpg"), []byte("jpegbytes"), 0o600))
	mine := models.Contact{UserID: e.me.ID, Firstname: "A", PhotoThumbnailFile: "abc_thumb.jpg"}
	theirs := models.Contact{UserID: e.other.ID, Firstname: "B", PhotoThumbnailFile: "abc_thumb.jpg"}
	none := models.Contact{UserID: e.me.ID, Firstname: "C"}
	require.NoError(t, e.db.Create(&[]*models.Contact{&mine, &theirs, &none}).Error)
	path := func(id uint) string { return "/contacts/" + strconv.Itoa(int(id)) + "/picture?thumbnail=true" }

	w := e.get(path(mine.ID))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "jpegbytes", w.Body.String())
	assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Cache-Control"), "max-age=31536000")
	assert.Contains(t, w.Header().Get("Cache-Control"), "private")
	etag := w.Header().Get("ETag")
	require.NotEmpty(t, etag)

	w = e.get(path(mine.ID), "If-None-Match", etag)
	assert.Equal(t, http.StatusNotModified, w.Code)
	assert.Empty(t, w.Body.String())
	w = e.get(path(mine.ID), "If-None-Match", `"other"`)
	assert.Equal(t, http.StatusOK, w.Code)

	assert.Equal(t, http.StatusNotFound, e.get(path(theirs.ID)).Code, "another user's thumbnail")
	assert.Equal(t, http.StatusNotFound, e.get(path(none.ID)).Code)
}

func TestThumbnailFileNameCannotEscapeTheDirectory(t *testing.T) {
	e := newThumbEnv(t)
	secret := filepath.Join(filepath.Dir(e.dir), "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("secret"), 0o600))
	t.Cleanup(func() { os.Remove(secret) })
	for _, name := range []string{"../secret.txt", secret, "sub/x.jpg", `..\secret.txt`} {
		c := models.Contact{UserID: e.me.ID, Firstname: "X", PhotoThumbnailFile: name}
		require.NoError(t, e.db.Create(&c).Error)
		w := e.get("/contacts/" + strconv.Itoa(int(c.ID)) + "/picture?thumbnail=true")
		assert.NotEqual(t, http.StatusOK, w.Code, name)
		assert.NotContains(t, w.Body.String(), "secret", name)
	}
}

func TestThumbnailFallsBackToTheLegacyDatabaseCopy(t *testing.T) {
	e := newThumbEnv(t)
	c := models.Contact{UserID: e.me.ID, Firstname: "Old", LegacyThumbnail: "data:image/jpeg;base64,anBlZ2J5dGVz"}
	require.NoError(t, e.db.Create(&c).Error)
	w := e.get("/contacts/" + strconv.Itoa(int(c.ID)) + "/picture?thumbnail=true")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "jpegbytes", w.Body.String())
}

func TestListsCarryAThumbnailURLNotTheImage(t *testing.T) {
	e := newThumbEnv(t)
	big := "data:image/jpeg;base64," + strings.Repeat("A", 4000)
	with := models.Contact{UserID: e.me.ID, Firstname: "With", PhotoThumbnailFile: "11111111-2222-3333-4444-555555555555_thumb.jpg", LegacyThumbnail: big}
	without := models.Contact{UserID: e.me.ID, Firstname: "Without", LegacyThumbnail: big}
	require.NoError(t, e.db.Create(&[]*models.Contact{&with, &without}).Error)

	for _, q := range []string{"", "?fields=ID,firstname,photo_thumbnail", "?search=with"} {
		w := e.get("/contacts" + q)
		require.Equal(t, http.StatusOK, w.Code, q)
		assert.NotContains(t, w.Body.String(), "data:image", q)
		assert.Less(t, w.Body.Len(), 3000, "response size %s", q)
		var out struct {
			Contacts []map[string]any `json:"contacts"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		for _, c := range out.Contacts {
			url, _ := c["photo_thumbnail"].(string)
			if c["firstname"] == "With" {
				want := "/api/v1/contacts/" + strconv.Itoa(int(with.ID)) + "/profile_picture?thumbnail=true&v=11111111-2222-3333-4444-555555555555"
				assert.Equal(t, want, url, q)
			} else {
				assert.Empty(t, url, q)
			}
		}
	}

	w := e.get("/graph")
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "data:image")
	assert.Contains(t, w.Body.String(), `profile_picture?thumbnail=true\u0026v=11111111`)
}

func TestUploadWritesAThumbnailFileAndReplacingRemovesTheOldOnes(t *testing.T) {
	e := newThumbEnv(t)
	c := models.Contact{UserID: e.me.ID, Firstname: "A", LegacyThumbnail: legacyForUpload}
	require.NoError(t, e.db.Create(&c).Error)
	var img bytes.Buffer
	require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 60, 60))))
	upload := func() *httptest.ResponseRecorder {
		var body bytes.Buffer
		contentType := newMultipart(&body, img.Bytes())
		req := httptest.NewRequest("POST", "/contacts/"+strconv.Itoa(int(c.ID))+"/picture", &body)
		req.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		e.r.ServeHTTP(w, req)
		return w
	}
	require.Equal(t, http.StatusOK, upload().Code)
	var first models.Contact
	require.NoError(t, e.db.First(&first, c.ID).Error)
	assert.True(t, strings.HasSuffix(first.PhotoThumbnailFile, "_thumb.jpg"))
	assert.Empty(t, first.LegacyThumbnail, "the old picture's database copy is dropped")
	entries, _ := os.ReadDir(e.dir)
	assert.Len(t, entries, 2)

	require.Equal(t, http.StatusOK, upload().Code)
	var second models.Contact
	require.NoError(t, e.db.First(&second, c.ID).Error)
	assert.NotEqual(t, first.PhotoThumbnailFile, second.PhotoThumbnailFile)
	entries, _ = os.ReadDir(e.dir)
	assert.Len(t, entries, 2, "old photo and thumbnail are gone")
	_, err := os.Stat(filepath.Join(e.dir, first.PhotoThumbnailFile))
	assert.True(t, os.IsNotExist(err))
}

const legacyForUpload = "data:image/jpeg;base64,anBlZw=="

// newMultipart writes a one-file "photo" form into body and returns its content type.
func newMultipart(body *bytes.Buffer, data []byte) string {
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("photo", "p.png")
	part.Write(data)
	mw.Close()
	return mw.FormDataContentType()
}

func TestListWithThumbnailFieldButNoIDStillGetsWorkingURLs(t *testing.T) {
	e := newThumbEnv(t)
	c := models.Contact{UserID: e.me.ID, Firstname: "With", PhotoThumbnailFile: "11111111-2222-3333-4444-555555555555_thumb.jpg"}
	require.NoError(t, e.db.Create(&c).Error)
	w := e.get("/contacts?fields=firstname,photo_thumbnail")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "/contacts/"+strconv.Itoa(int(c.ID))+"/profile_picture?thumbnail=true")
}
