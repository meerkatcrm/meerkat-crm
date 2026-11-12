package carddav

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"meerkat/middleware"
	"meerkat/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type server struct {
	db     *gorm.DB
	router *gin.Engine
	alice  models.User
}

const alicePassword = "Correct-horse-1!"

func newServer(t *testing.T) *server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Contact{}, &models.ApiToken{}, &models.EmploymentHistory{}, &models.Note{}, &models.Relationship{}, &models.Reminder{}, &models.Activity{}))
	hash, err := bcrypt.GenerateFromPassword([]byte(alicePassword), bcrypt.MinCost)
	require.NoError(t, err)
	alice := models.User{Username: "alice", Email: "alice@example.com", Password: string(hash)}
	bob := models.User{Username: "bob", Email: "bob@example.com", Password: string(hash)}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	require.NoError(t, db.Create(&models.Contact{UserID: bob.ID, Firstname: "Bobs", Lastname: "Friend", VCardUID: "bobs-friend"}).Error)

	t.Cleanup(func() {
		for _, id := range []string{"alice", "alice@example.com", "bob", "nobody"} {
			middleware.GetAccountRateLimiter().RecordSuccessfulLogin(id)
		}
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/.well-known/carddav", WellKnownRedirect)
	g := r.Group("/carddav")
	g.Use(func(c *gin.Context) { c.Set("db", db); c.Next() })
	g.Use(BasicAuthMiddleware())
	handle := NewHandler(db, t.TempDir()).GinHandler()
	g.Any("/*path", handle)
	for _, m := range []string{"PROPFIND", "REPORT", "MKCOL", "COPY", "MOVE"} {
		g.Handle(m, "/*path", handle)
	}
	return &server{db: db, router: r, alice: alice}
}

const allProps = `<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>`

func (s *server) do(method, path, user, password string, headers ...string) *httptest.ResponseRecorder {
	body := ""
	if method == "PROPFIND" {
		body = allProps
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

func (s *server) propfind(path, user, depth string) *httptest.ResponseRecorder {
	return s.do("PROPFIND", path, user, alicePassword, "Depth", depth, "Content-Type", "application/xml")
}

func vcardBody(uid, given, family string, extra ...string) string {
	lines := []string{"BEGIN:VCARD", "VERSION:3.0", "UID:" + uid, "FN:" + given + " " + family, "N:" + family + ";" + given + ";;;"}
	lines = append(lines, extra...)
	return strings.Join(append(lines, "END:VCARD", ""), "\r\n")
}

func (s *server) put(uid, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PUT", "/carddav/addressbooks/alice/contacts/"+uid+".vcf", strings.NewReader(body))
	req.SetBasicAuth("alice", alicePassword)
	req.Header.Set("Content-Type", "text/vcard")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

// go-webdav answers 201 for an update too, where RFC 4918 expects 204; any success will do.
func assertUpdated(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.True(t, w.Code >= 200 && w.Code < 300, "update answered %d", w.Code)
}

func (s *server) get(uid string) *httptest.ResponseRecorder {
	return s.do("GET", "/carddav/addressbooks/alice/contacts/"+uid+".vcf", "alice", alicePassword)
}

func (s *server) addressBook() string {
	return s.propfind("/carddav/addressbooks/alice/contacts/", "alice", "1").Body.String()
}

func TestAClientCreatesAndUpdatesAContactWithPut(t *testing.T) {
	s := newServer(t)

	created := s.put("new-uid", vcardBody("new-uid", "Carla", "Cole", "TEL:+4930123"))
	assert.Equal(t, http.StatusCreated, created.Code)
	etag := created.Header().Get("ETag")
	assert.NotEmpty(t, etag)

	got := s.get("new-uid")
	assert.Equal(t, http.StatusOK, got.Code)
	assert.Contains(t, got.Body.String(), "Carla")
	assert.Contains(t, got.Body.String(), "Cole")

	updated := s.put("new-uid", vcardBody("new-uid", "Carla", "Colt"), "If-Match", etag)
	assertUpdated(t, updated)
	assert.Contains(t, s.get("new-uid").Body.String(), "Colt")
	assert.NotEqual(t, etag, updated.Header().Get("ETag"), "a change gives a new ETag")
	assert.Equal(t, 1, strings.Count(s.addressBook(), "new-uid.vcf"), "the update did not create a second contact")
}

func TestAStalePutIsRefusedAndKeepsTheServerVersion(t *testing.T) {
	s := newServer(t)
	first := s.put("c-1", vcardBody("c-1", "Dana", "Dunn"))
	require.Equal(t, http.StatusCreated, first.Code)
	stale := first.Header().Get("ETag")
	assertUpdated(t, s.put("c-1", vcardBody("c-1", "Dana", "Dunne"), "If-Match", stale))

	conflict := s.put("c-1", vcardBody("c-1", "Dana", "Overwritten"), "If-Match", stale)
	assert.Equal(t, http.StatusPreconditionFailed, conflict.Code)
	body := s.get("c-1").Body.String()
	assert.Contains(t, body, "Dunne")
	assert.NotContains(t, body, "Overwritten")
}

func TestPutThatMustCreateRefusesAnExistingContact(t *testing.T) {
	s := newServer(t)
	require.Equal(t, http.StatusCreated, s.put("c-2", vcardBody("c-2", "Eli", "East")).Code)

	w := s.put("c-2", vcardBody("c-2", "Eli", "Overwritten"), "If-None-Match", "*")
	assert.Equal(t, http.StatusPreconditionFailed, w.Code)
	assert.NotContains(t, s.get("c-2").Body.String(), "Overwritten")

	fresh := s.put("c-3", vcardBody("c-3", "Fay", "Fox"), "If-None-Match", "*")
	assert.Equal(t, http.StatusCreated, fresh.Code)
}

func TestPutThatMustCreateRefusesAnExistingContactWithoutAStoredETag(t *testing.T) {
	s := newServer(t)
	require.Equal(t, http.StatusCreated, s.put("c-6", vcardBody("c-6", "Gil", "Gold")).Code)
	require.NoError(t, s.db.Model(&models.Contact{}).Where("vcard_uid = ?", "c-6").UpdateColumn("etag", "").Error)

	w := s.put("c-6", vcardBody("c-6", "Gil", "Overwritten"), "If-None-Match", "*")
	assert.Equal(t, http.StatusPreconditionFailed, w.Code)
	assert.NotContains(t, s.get("c-6").Body.String(), "Overwritten")
}

func TestPutThatMustUpdateRefusesAMissingContact(t *testing.T) {
	s := newServer(t)
	w := s.put("ghost", vcardBody("ghost", "Gus", "Gray"), "If-Match", `"anything"`)
	assert.Equal(t, http.StatusPreconditionFailed, w.Code)
	assert.Equal(t, http.StatusNotFound, s.get("ghost").Code)
}

func TestADeletedContactLeavesTheAddressBookAndGoesToTheTrash(t *testing.T) {
	s := newServer(t)
	require.Equal(t, http.StatusCreated, s.put("c-4", vcardBody("c-4", "Hana", "Hill")).Code)
	require.Contains(t, s.addressBook(), "c-4")

	w := s.do("DELETE", "/carddav/addressbooks/alice/contacts/c-4.vcf", "alice", alicePassword)
	assert.Equal(t, http.StatusNoContent, w.Code)

	assert.NotContains(t, s.addressBook(), "c-4")
	assert.Equal(t, http.StatusNotFound, s.get("c-4").Code)
	var trashed models.Contact
	require.NoError(t, s.db.Unscoped().Where("user_id = ? AND vcard_uid = ?", s.alice.ID, "c-4").First(&trashed).Error)
	assert.True(t, trashed.DeletedAt.Valid, "the contact is in the trash, not gone")
}

func TestMissingContactsAnswerNotFound(t *testing.T) {
	s := newServer(t)
	assert.Equal(t, http.StatusNotFound, s.get("nobody-here").Code)
	assert.Equal(t, http.StatusNotFound, s.do("DELETE", "/carddav/addressbooks/alice/contacts/nobody-here.vcf", "alice", alicePassword).Code)
}

func TestAVCardWithAPhotoGivesTheContactAPhoto(t *testing.T) {
	s := newServer(t)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 60, 60))))
	photo := "PHOTO;ENCODING=b;TYPE=PNG:" + base64.StdEncoding.EncodeToString(buf.Bytes())

	assert.Equal(t, http.StatusCreated, s.put("c-5", vcardBody("c-5", "Ivo", "Ives", photo)).Code)

	assert.Contains(t, s.get("c-5").Body.String(), "PHOTO", "the client gets the profile picture back")
}
