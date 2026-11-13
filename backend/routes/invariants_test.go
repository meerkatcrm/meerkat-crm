package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"meerkat/database"
	"meerkat/middleware"
	"meerkat/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Invariant: no route lets one user change another user's data.
//
// routes_test.go already checks that every route but the public ones rejects an anonymous
// request. The walk below logs in as user A and sends every mutating route, with B's ids in the
// path and the body, then compares B's rows with a snapshot taken before. A new route is walked
// without touching this file; a new table with a user_id fails until B gets a row in it.

var mutating = map[string]bool{
	"POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	"PROPPATCH": true, "MKCOL": true, "COPY": true, "MOVE": true,
}

// unseeded lists the tables with a user_id that no route reads or writes per user, so the
// walk has nothing to protect in them. Every entry needs a reason.
var unseeded = map[string]string{
	"email_outbox": "a queue only the mail engine reads; no route returns or changes its rows",
	"carddav_sync": "unused table; no code reads or writes it",
}

// proof names a top-level Go test, in a file relative to backend/, that shows the isolation.
type proof struct{ file, test string }

// sessionKeyed are the mutating routes whose target is an in-memory import session rather
// than a database row, so the walk cannot hand them B's session. Each names the test that
// shows another user's session is refused.
var sessionKeyed = map[string]proof{
	"POST /api/v1/contacts/import/preview":          {"controllers/import_controller_test.go", "TestCSVImport_SessionOfAnotherUserIsOffLimits"},
	"POST /api/v1/contacts/import/confirm":          {"controllers/import_controller_test.go", "TestCSVImport_SessionOfAnotherUserIsOffLimits"},
	"POST /api/v1/contacts/import/vcf/confirm":      {"controllers/import_controller_test.go", "TestVCFImport_SessionOfAnotherUserCannotBeConfirmed"},
	"POST /api/v1/contacts/import/monica/fetch":     {"controllers/monica_import_controller_test.go", "TestMonicaSessionOwnershipEnforced"},
	"POST /api/v1/contacts/import/monica/confirm":   {"controllers/monica_import_controller_test.go", "TestMonicaSessionOwnershipEnforced"},
	"DELETE /api/v1/contacts/import/monica/session": {"controllers/monica_import_controller_test.go", "TestMonicaSessionOwnershipEnforced"},
}

const isolationPassword = "Correct-horse-1!"

type isolationEnv struct {
	db       *gorm.DB
	router   *gin.Engine
	a, b     models.User
	token    string
	ids      map[string]uint // B's rows, by the path segment that names them
	contact2 uint            // B's second contact, for fields that pair two contacts
}

func newIsolationEnv(t *testing.T) *isolationEnv {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	db, err := database.InitDB(filepath.Join(t.TempDir(), "isolation.db"))
	require.NoError(t, err)
	db = db.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	cfg := testConfig()
	cfg.ProfilePhotoDir = t.TempDir()
	e := &isolationEnv{db: db, router: routerOn(db, cfg, nil)}

	hash, err := bcrypt.GenerateFromPassword([]byte(isolationPassword), bcrypt.MinCost)
	require.NoError(t, err)
	e.a = models.User{Username: "a", Email: "a@example.com", Password: string(hash)}
	e.b = models.User{Username: "b", Email: "b@example.com", Password: string(hash)}
	require.NoError(t, db.Create(&e.a).Error)
	require.NoError(t, db.Create(&e.b).Error)
	e.token, err = middleware.GenerateToken(e.a, cfg)
	require.NoError(t, err)
	e.seed(t)
	return e
}

// seed gives B a row in every table with a user_id that the routes reach.
func (e *isolationEnv) seed(t *testing.T) {
	t.Helper()
	b := e.b.ID
	now := time.Now().UTC().Truncate(time.Second)
	create := func(row any) { require.NoError(t, e.db.Create(row).Error) }

	contact := models.Contact{UserID: b, Firstname: "Theirs", VCardUID: "b-uid", Circles: []string{"friends"}}
	create(&contact)
	other := models.Contact{UserID: b, Firstname: "Theirs too"}
	create(&other)
	e.contact2 = other.ID

	activity := models.Activity{UserID: b, Title: "Lunch", Date: now, Contacts: []models.Contact{contact}}
	create(&activity)
	note := models.Note{UserID: b, Content: "private", Date: now, ContactID: &contact.ID}
	create(&note)
	relationship := models.Relationship{UserID: b, ContactID: contact.ID, Name: "Sib", Type: "Sibling", RelatedContactID: &other.ID}
	create(&relationship)
	job := models.EmploymentHistory{UserID: b, ContactID: contact.ID, Organization: "Acme"}
	create(&job)
	reminder := models.Reminder{UserID: b, ContactID: &contact.ID, Message: "call", RemindAt: now, Recurrence: "once"}
	create(&reminder)
	completion := models.ReminderCompletion{UserID: b, ReminderID: &reminder.ID, ContactID: contact.ID, Message: "called", CompletedAt: now}
	create(&completion)
	token := models.ApiToken{UserID: b, Name: "cli", TokenHash: "b-hash"}
	create(&token)
	webhook := models.Webhook{UserID: b, Name: "hook", URL: "https://example.com/hook", Secret: "s", Events: []string{"contact.created"}, IsActive: true}
	create(&webhook)
	create(&models.WebhookDelivery{WebhookID: webhook.ID, EventType: "contact.created", Payload: "{}"})
	calendar := models.CalendarSubscription{UserID: b, Name: "cal", URL: "https://example.com/cal.ics"}
	create(&calendar)
	create(&models.CalendarEventLink{SubscriptionID: calendar.ID, UserID: b, UID: "ev", ActivityID: activity.ID, ContentHash: "h"})
	connection := models.CardDAVConnection{UserID: b, BaseURL: "https://dav.example.com", AddressBookPath: "/ab/"}
	create(&connection)
	create(&models.CardDAVContactLink{ConnectionID: connection.ID, UserID: b, ContactID: contact.ID, RemoteUID: "r", RemotePath: "/ab/r.vcf"})
	create(&models.ContactDuplicateDismissal{UserID: b, ContactAID: contact.ID, ContactBID: other.ID})

	e.ids = map[string]uint{
		"contacts": contact.ID, "activities": activity.ID, "notes": note.ID,
		"relationships": relationship.ID, "employment-history": job.ID,
		"reminders": reminder.ID, "reminder-completions": completion.ID,
		"api-tokens": token.ID, "webhooks": webhook.ID, "calendars": calendar.ID, "users": b,
	}
}

// columns returns every table with a user_id column, and per table the columns that point at
// a contact.
func (e *isolationEnv) columns(t *testing.T) (withUser []string, contactCols map[string][]string) {
	t.Helper()
	var tables []string
	require.NoError(t, e.db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'schema_migrations' ORDER BY name").Scan(&tables).Error)
	contactCols = map[string][]string{}
	for _, table := range tables {
		cols, err := e.db.Migrator().ColumnTypes(table)
		require.NoError(t, err)
		for _, c := range cols {
			if name := c.Name(); name == "user_id" {
				withUser = append(withUser, table)
			} else if strings.Contains(name, "contact") && strings.HasSuffix(name, "_id") {
				contactCols[table] = append(contactCols[table], name)
			}
		}
	}
	return withUser, contactCols
}

// snapshot renders B's user row, every row B owns, and every row, of anyone, that points at
// one of B's contacts.
func (e *isolationEnv) snapshot(t *testing.T) string {
	t.Helper()
	withUser, contactCols := e.columns(t)
	conds := map[string][]string{"users": {"id = @b"}}
	for _, table := range withUser {
		conds[table] = append(conds[table], "user_id = @b")
	}
	for table, cols := range contactCols {
		for _, col := range cols {
			conds[table] = append(conds[table], col+" IN (SELECT id FROM contacts WHERE user_id = @b)")
		}
	}
	tables := make([]string, 0, len(conds))
	for table := range conds {
		tables = append(tables, table)
	}
	sort.Strings(tables)

	var out strings.Builder
	for _, table := range tables {
		var rows []map[string]any
		query := fmt.Sprintf("SELECT * FROM %q WHERE %s ORDER BY rowid", table, strings.Join(conds[table], " OR "))
		require.NoError(t, e.db.Raw(query, map[string]any{"b": e.b.ID}).Scan(&rows).Error, table)
		data, err := json.Marshal(rows)
		require.NoError(t, err)
		fmt.Fprintf(&out, "%s %s\n", table, data)
	}
	return out.String()
}

// targets returns the concrete paths that aim a route at B's rows, and whether a path
// parameter picked one of them. ok is false when a parameter follows a segment the walk
// does not know.
func (e *isolationEnv) targets(route string) (paths []string, aimed, ok bool) {
	if strings.HasPrefix(route, "/carddav/") {
		id := strconv.Itoa(int(e.ids["contacts"]))
		return []string{
			"/carddav/addressbooks/a/contacts/b-uid.vcf",
			"/carddav/addressbooks/a/contacts/" + id + ".vcf",
			"/carddav/addressbooks/b/contacts/b-uid.vcf",
			"/carddav/addressbooks/b/contacts/",
		}, false, true
	}
	segments := strings.Split(route, "/")
	for i, s := range segments {
		if !strings.HasPrefix(s, ":") {
			continue
		}
		id, known := e.ids[segments[i-1]]
		if !known {
			return nil, false, false
		}
		segments[i] = strconv.Itoa(int(id))
		aimed = true
	}
	return []string{strings.Join(segments, "/")}, aimed, true
}

// body carries every field the JSON routes require, with B's ids wherever a field names a
// contact, so a route that trusts the body over the owner shows up in the snapshot.
func (e *isolationEnv) body() []byte {
	c1, c2 := e.ids["contacts"], e.contact2
	data, _ := json.Marshal(map[string]any{
		"firstname": "Mine", "title": "Mine", "name": "Mine", "message": "Mine", "content": "Mine",
		"type": "Friend", "organization": "Mine", "date": time.Now().UTC().Format(time.RFC3339),
		"remind_at": time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339), "recurrence": "once",
		"contact_id": c1, "contact_ids": []uint{c1, c2}, "related_contact_id": c2, "source_id": c2,
		"contact_a_id": c1, "contact_b_id": c2, "url": "https://127.0.0.1:1/x", "base_url": "https://127.0.0.1:1/",
		"address_book_path": "/ab/", "events": []string{"contact.created"}, "language": "de",
		"date_format": "iso", "mode": "include", "circles": []string{"x"}, "hide_empty_contact_fields": true,
		"names": []string{"x"}, "fields": []string{"email"}, "session_id": "b-session", "api_token": "t",
	}) // no password fields: changing A's password would lock the walk out of CardDAV

	return data
}

const isolationVCard = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:b-uid\r\nFN:Hacked\r\nN:Hacked;;;;\r\nEND:VCARD\r\n"

func (e *isolationEnv) send(method, path string) *httptest.ResponseRecorder {
	return request(e.router, method, path, func(r *http.Request) {
		if strings.HasPrefix(path, "/carddav/") {
			r.Body = io.NopCloser(strings.NewReader(isolationVCard))
			r.Header.Set("Content-Type", "text/vcard")
			r.Header.Set("Destination", "/carddav/addressbooks/a/contacts/moved.vcf")
			r.SetBasicAuth("a", isolationPassword)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(e.body()))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+e.token)
	})
}

func TestInvariants_NoMutatingRouteChangesAnotherUsersRows(t *testing.T) {
	e := newIsolationEnv(t)
	before := e.snapshot(t)

	walked := 0
	for _, route := range e.router.Routes() {
		key := routeKey(route)
		if !mutating[route.Method] || publicRoutes[key] {
			continue
		}
		paths, aimed, ok := e.targets(route.Path)
		if !assert.True(t, ok, "%s: seed a row of B's for its path parameter and add it to isolationEnv.ids", key) {
			continue
		}
		for _, path := range paths {
			walked++
			w := e.send(route.Method, path)
			if aimed {
				assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, w.Code,
					"%s %s aims at B's row and answered %d: %s", route.Method, path, w.Code, w.Body.String())
			}
			if after := e.snapshot(t); !assert.Equal(t, before, after, "%s %s changed B's rows", route.Method, path) {
				before = after // report each route once
			}
		}
	}
	assert.Greater(t, walked, 60, "the walk must see the real route table")
}

func TestInvariants_EveryUserTableIsSeededForTheWalk(t *testing.T) {
	e := newIsolationEnv(t)
	withUser, _ := e.columns(t)
	have := map[string]bool{}
	for _, table := range withUser {
		have[table] = true
		var n int64
		require.NoError(t, e.db.Table(table).Where("user_id = ?", e.b.ID).Count(&n).Error)
		_, exempt := unseeded[table]
		assert.False(t, n > 0 && exempt, "%s is seeded and in unseeded; drop it from unseeded", table)
		assert.True(t, n > 0 || exempt, "%s has a user_id but B has no row in it: seed one in isolationEnv.seed, or add it to unseeded with the reason", table)
	}
	for table := range unseeded {
		assert.True(t, have[table], "unseeded lists %s, which has no user_id column", table)
	}
}

func TestInvariants_SessionKeyedRoutesNameTheirIsolationTest(t *testing.T) {
	router, _ := newRouter(t, testConfig(), nil)
	registered := map[string]bool{}
	for _, route := range router.Routes() {
		registered[routeKey(route)] = true
	}
	for key, p := range sessionKeyed {
		assert.True(t, registered[key], "sessionKeyed lists a route that does not exist: %s", key)
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", p.file), nil, parser.SkipObjectResolution)
		if !assert.NoError(t, err, key) {
			continue
		}
		found := false
		for _, decl := range parsed.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == p.test {
				found = true
			}
		}
		assert.True(t, found, "%s: %s has no top-level test %s", key, p.file, p.test)
	}
}
