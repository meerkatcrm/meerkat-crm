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
	"MKCOL": true, "COPY": true, "MOVE": true,
}

// reads are the methods the read walk sends. Every registered method must be in mutating or
// reads; TestInvariants_EveryRouteMethodIsWalked fails on any other.
var reads = map[string]bool{
	"GET": true, "HEAD": true, "OPTIONS": true, "PROPFIND": true, "REPORT": true, "CONNECT": true, "TRACE": true,
}

// unseeded lists the tables with a user_id that no route reads or writes per user, so the
// walk has nothing to protect in them. Every entry needs a reason.
var unseeded = map[string]string{
	"email_outbox": "a queue only the mail engine reads; no route returns or changes its rows",
	"carddav_sync": "only the admin user-delete flow touches it; no user-facing route reads or writes it",
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

// sessionNotFound is how both import session stores answer the walk's made-up session id; a
// mutating route that answers with it must be in sessionKeyed.
const sessionNotFound = "Import session expired or not found"

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
	e.b = models.User{Username: "b", Email: "bsecret-user@example.com", Password: string(hash)}
	require.NoError(t, db.Create(&e.a).Error)
	require.NoError(t, db.Create(&e.b).Error)
	e.token, err = middleware.GenerateToken(e.a, cfg, time.Now())
	require.NoError(t, err)
	e.seed(t)
	return e
}

// secret is in every text field of B's seeded rows; no response to A may contain it.
const secret = "BSecret"

// showsSecret reports whether s holds the marker, in any case: some fields (emails) come
// back lowercased.
func showsSecret(s string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(secret))
}

// seed gives B a row in every table with a user_id that the routes reach.
func (e *isolationEnv) seed(t *testing.T) {
	t.Helper()
	b := e.b.ID
	now := time.Now().UTC().Truncate(time.Second)
	create := func(row any) { require.NoError(t, e.db.Create(row).Error) }

	contact := models.Contact{UserID: b, Firstname: "BSecretContact", Nickname: "BSecretNick", Email: "bsecret@example.com", Note: "BSecretContactNote", VCardUID: "b-uid", Circles: []string{"BSecretCircle"}}
	create(&contact)
	other := models.Contact{UserID: b, Firstname: "BSecretContactToo"}
	create(&other)
	e.contact2 = other.ID

	activity := models.Activity{UserID: b, Title: "BSecretActivity", Date: now, Contacts: []models.Contact{contact}}
	create(&activity)
	note := models.Note{UserID: b, Content: "BSecretNote", Date: now, ContactID: &contact.ID}
	create(&note)
	relationship := models.Relationship{UserID: b, ContactID: contact.ID, Name: "BSecretRelation", Type: "Sibling", RelatedContactID: &other.ID}
	create(&relationship)
	job := models.EmploymentHistory{UserID: b, ContactID: contact.ID, Organization: "BSecretOrg"}
	create(&job)
	reminder := models.Reminder{UserID: b, ContactID: &contact.ID, Message: "BSecretReminder", RemindAt: now, Recurrence: "once"}
	create(&reminder)
	completion := models.ReminderCompletion{UserID: b, ReminderID: &reminder.ID, ContactID: contact.ID, Message: "BSecretCompletion", CompletedAt: now}
	create(&completion)
	token := models.ApiToken{UserID: b, Name: "BSecretToken", TokenHash: "b-hash"}
	create(&token)
	webhook := models.Webhook{UserID: b, Name: "BSecretHook", URL: "https://example.com/hook", Secret: "s", Events: []string{"contact.created"}, IsActive: true}
	create(&webhook)
	create(&models.WebhookDelivery{WebhookID: webhook.ID, EventType: "contact.created", Payload: "{}"})
	calendar := models.CalendarSubscription{UserID: b, Name: "BSecretCalendar", URL: "https://example.com/cal.ics"}
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
		"carddav-connection": connection.ID, // only sent in the body
	}
}

// pointer is a column that holds the id of a row in another table.
type pointer struct{ col, target string }

// columns returns every table with a user_id column, and per table the columns that point at
// a row of a table with a user_id: the declared foreign keys, plus the columns named
// *contact*_id, since some contact references (carddav_contact_links.contact_id, the
// duplicate-dismissal pair) declare no foreign key.
func (e *isolationEnv) columns(t *testing.T) (withUser []string, pointers map[string][]pointer) {
	t.Helper()
	var tables []string
	require.NoError(t, e.db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'schema_migrations' ORDER BY name").Scan(&tables).Error)
	owned := map[string]bool{}
	named := map[string][]string{}
	for _, table := range tables {
		cols, err := e.db.Migrator().ColumnTypes(table)
		require.NoError(t, err)
		for _, c := range cols {
			if name := c.Name(); name == "user_id" {
				withUser = append(withUser, table)
				owned[table] = true
			} else if strings.Contains(name, "contact") && strings.HasSuffix(name, "_id") {
				named[table] = append(named[table], name)
			}
		}
	}
	pointers = map[string][]pointer{}
	for _, table := range tables {
		seen := map[string]bool{}
		var fks []struct {
			Table string
			From  string
		}
		require.NoError(t, e.db.Raw(fmt.Sprintf("PRAGMA foreign_key_list(%q)", table)).Scan(&fks).Error)
		for _, fk := range fks {
			if owned[fk.Table] && !seen[fk.From] {
				seen[fk.From] = true
				pointers[table] = append(pointers[table], pointer{fk.From, fk.Table})
			}
		}
		for _, col := range named[table] {
			if !seen[col] {
				pointers[table] = append(pointers[table], pointer{col, "contacts"})
			}
		}
	}
	return withUser, pointers
}

// snapshotConds returns, per table, the conditions that select the rows the snapshot holds.
func (e *isolationEnv) snapshotConds(t *testing.T) map[string][]string {
	t.Helper()
	withUser, pointers := e.columns(t)
	conds := map[string][]string{"users": {"id = @b"}}
	for _, table := range withUser {
		conds[table] = append(conds[table], "user_id = @b")
	}
	for table, ps := range pointers {
		for _, p := range ps {
			conds[table] = append(conds[table], fmt.Sprintf("%q IN (SELECT id FROM %q WHERE user_id = @b)", p.col, p.target))
		}
	}
	return conds
}

// snapshot renders B's user row, every row B owns, and every row, of anyone, that points at
// one of B's rows through a column columns() reports.
func (e *isolationEnv) snapshot(t *testing.T) string {
	t.Helper()
	conds := e.snapshotConds(t)
	tables := make([]string, 0, len(conds))
	for table := range conds {
		tables = append(tables, table)
	}
	sort.Strings(tables)

	var out strings.Builder
	for _, table := range tables {
		data, err := json.Marshal(e.snapshotRows(t, table, conds[table]))
		require.NoError(t, err)
		fmt.Fprintf(&out, "%s %s\n", table, data)
	}
	return out.String()
}

func (e *isolationEnv) snapshotRows(t *testing.T, table string, conds []string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	query := fmt.Sprintf("SELECT * FROM %q WHERE %s ORDER BY rowid", table, strings.Join(conds, " OR "))
	require.NoError(t, e.db.Raw(query, map[string]any{"b": e.b.ID}).Scan(&rows).Error, table)
	return rows
}

// target is one request of the walk. aimed is set when a path parameter names one of B's rows.
type target struct {
	path  string
	aimed bool
}

// targets returns the requests that aim a route at B's rows. ok is false when a path
// parameter follows a segment the walk does not know. Every parameter is substituted with a
// numeric row id: NumericIDParamsMiddleware answers 400 to any other value under /api/v1, so
// a username or token parameter cannot exist there.
func (e *isolationEnv) targets(route string) (out []target, ok bool) {
	if strings.HasPrefix(route, "/carddav/") {
		// One wildcard route serves every CardDAV path, so the walk picks them: A's own address
		// book (where an unscoped listing would show B's contacts), B's contact reached through
		// it by UID and by id, and B's address book itself, which the backend must refuse.
		id := strconv.Itoa(int(e.ids["contacts"]))
		return []target{
			{"/carddav/addressbooks/a/contacts/", false},
			{"/carddav/addressbooks/a/contacts/b-uid.vcf", false},
			{"/carddav/addressbooks/a/contacts/" + id + ".vcf", false},
			{"/carddav/addressbooks/b/contacts/b-uid.vcf", true},
			{"/carddav/addressbooks/b/contacts/", true},
		}, true
	}
	segments := strings.Split(route, "/")
	aimed := false
	for i, s := range segments {
		if !strings.HasPrefix(s, ":") {
			continue
		}
		id, known := e.ids[segments[i-1]]
		if !known {
			return nil, false
		}
		segments[i] = strconv.Itoa(int(id))
		aimed = true
	}
	return []target{{strings.Join(segments, "/"), aimed}}, true
}

// refuses checks the answer to a request aimed at B's data. The JSON API answers 403 or 404;
// go-webdav answers CardDAV with several codes (404, 405, 501, ...), so there any non-2xx will do.
func refuses(path string, code int) bool {
	if strings.HasPrefix(path, "/carddav/") {
		return code < 200 || code >= 300
	}
	return code == http.StatusForbidden || code == http.StatusNotFound
}

// walk sends every route of the given methods as A and calls check with each answer. It fails
// a request that A's credentials did not get through, so a lockout cannot hollow out the walk.
func (e *isolationEnv) walk(t *testing.T, methods func(string) bool, check func(method string, tg target, w *httptest.ResponseRecorder)) int {
	t.Helper()
	walked := 0
	for _, route := range e.router.Routes() {
		key := routeKey(route)
		if !methods(route.Method) || publicRoutes[key] {
			continue
		}
		targets, ok := e.targets(route.Path)
		if !assert.True(t, ok, "%s: seed a row of B's for its path parameter and add it to isolationEnv.ids", key) {
			continue
		}
		for _, tg := range targets {
			walked++
			w := e.send(route.Method, tg.path)
			assert.NotContains(t, []int{http.StatusUnauthorized, http.StatusTooManyRequests}, w.Code,
				"%s %s: A's credentials did not get through, so the request proves nothing", route.Method, tg.path)
			// OPTIONS only lists the allowed methods; it reads no data, whatever the path.
			if tg.aimed && route.Method != http.MethodOptions {
				assert.True(t, refuses(tg.path, w.Code), "%s %s aims at B's data and answered %d: %.300s", route.Method, tg.path, w.Code, w.Body.String())
			}
			check(route.Method, tg, w)
		}
	}
	return walked
}

// body carries every field the JSON routes require, with B's ids wherever a field names one
// of B's rows or B's user id, so a route that trusts the body over the owner shows up in the
// snapshot.
func (e *isolationEnv) body() []byte {
	c1, c2 := e.ids["contacts"], e.contact2
	data, _ := json.Marshal(map[string]any{
		"firstname": "Mine", "title": "Mine", "name": "Mine", "message": "Mine", "content": "Mine",
		"type": "Friend", "organization": "Mine", "date": time.Now().UTC().Format(time.RFC3339),
		"remind_at": time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339), "recurrence": "once",
		"contact_id": c1, "contact_ids": []uint{c1, c2}, "related_contact_id": c2, "source_id": c2,
		"user_id": e.b.ID, "reminder_id": e.ids["reminders"], "webhook_id": e.ids["webhooks"],
		"subscription_id": e.ids["calendars"], "connection_id": e.ids["carddav-connection"], "activity_id": e.ids["activities"],
		"contact_a_id": c1, "contact_b_id": c2, "url": "https://127.0.0.1:1/x", "base_url": "https://127.0.0.1:1/",
		"address_book_path": "/ab/", "events": []string{"contact.created"}, "language": "de",
		"date_format": "iso", "mode": "all", "circles": []string{"x"}, "hide_empty_contact_fields": true,
		"names": []string{"x"}, "fields": []string{"email"}, "session_id": "b-session", "api_token": "t", "mappings": []any{}, "actions": []any{},
	}) // No password fields, and the circle filter mode "all": either would hide CardDAV from A
	// for the rest of the walk.

	return data
}

// cardDAVBody returns a well-formed body for method, so a CardDAV request is refused for the
// data it aims at and not as malformed.
func cardDAVBody(method string) (body, contentType string) {
	const xml = `<?xml version="1.0" encoding="utf-8"?>`
	switch method {
	case "PROPFIND":
		return xml + `<D:propfind xmlns:D="DAV:"><D:allprop/></D:propfind>`, "application/xml"
	case "REPORT":
		return xml + `<C:addressbook-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav"><D:prop><D:getetag/><C:address-data/></D:prop></C:addressbook-query>`, "application/xml"
	case "MKCOL":
		return "", ""
	}
	return "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:b-uid\r\nFN:Hacked\r\nN:Hacked;;;;\r\nEND:VCARD\r\n", "text/vcard"
}

func (e *isolationEnv) send(method, path string) *httptest.ResponseRecorder {
	if !strings.HasPrefix(path, "/carddav/") {
		// query fields, with B's ids as in body()
		path += "?source_id=" + strconv.Itoa(int(e.contact2)) + "&session_id=b-session"
	}
	return request(e.router, method, path, func(r *http.Request) {
		if strings.HasPrefix(path, "/carddav/") {
			body, contentType := cardDAVBody(method)
			r.Body = io.NopCloser(strings.NewReader(body))
			r.Header.Set("Content-Type", contentType)
			r.Header.Set("Depth", "1")
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

	walked := e.walk(t, func(m string) bool { return mutating[m] }, func(method string, tg target, w *httptest.ResponseRecorder) {
		assert.False(t, showsSecret(w.Body.String()), "%s %s shows B's data to A", method, tg.path)
		if _, listed := sessionKeyed[method+" "+tg.path]; !listed && strings.Contains(w.Body.String(), sessionNotFound) {
			t.Errorf("%s %s looks up an import session: add it to sessionKeyed with the test that proves its isolation", method, tg.path)
		}
		if after := e.snapshot(t); !assert.Equal(t, before, after, "%s %s changed B's rows", method, tg.path) {
			before = after // report each route once
		}
	})
	assert.Greater(t, walked, 60, "the walk must see the real route table")

	// The snapshot sees B's rows; a copy of B's data into anyone else's row is caught here.
	assert.Empty(t, e.rowsOutsideBWithSecret(t), "rows outside B's carry B's data")
}

// rowsOutsideBWithSecret returns "table: row" for every row, outside the ones the snapshot
// holds, that has the marker in any column.
func (e *isolationEnv) rowsOutsideBWithSecret(t *testing.T) []string {
	t.Helper()
	var tables []string
	require.NoError(t, e.db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'schema_migrations' ORDER BY name").Scan(&tables).Error)
	conds := e.snapshotConds(t)
	var found []string
	for _, table := range tables {
		query, args := fmt.Sprintf("SELECT * FROM %q", table), []any{}
		if c, ok := conds[table]; ok {
			// COALESCE: with a NULL pointer column the OR is NULL, and NOT NULL would drop the row.
			query += " WHERE NOT COALESCE(" + strings.Join(c, " OR ") + ", 0)"
			args = append(args, map[string]any{"b": e.b.ID})
		}
		var rows []map[string]any
		require.NoError(t, e.db.Raw(query, args...).Scan(&rows).Error, table)
		for _, row := range rows {
			data, err := json.Marshal(row)
			require.NoError(t, err)
			if showsSecret(string(data)) {
				found = append(found, table+": "+string(data))
			}
		}
	}
	return found
}

func TestInvariants_NoReadRouteShowsAnotherUsersRows(t *testing.T) {
	e := newIsolationEnv(t)

	walked := e.walk(t, func(m string) bool { return reads[m] }, func(method string, tg target, w *httptest.ResponseRecorder) {
		assert.False(t, showsSecret(w.Body.String()), "%s %s shows B's data to A", method, tg.path)
	})
	assert.Greater(t, walked, 30, "the walk must see the real route table")
}

func TestInvariants_EveryRouteMethodIsWalked(t *testing.T) {
	router, _ := newRouter(t, testConfig(), nil)
	registered := map[string]bool{}
	for _, route := range router.Routes() {
		registered[route.Method] = true
		assert.True(t, mutating[route.Method] || reads[route.Method],
			"%s is in neither walk: add %s to mutating or reads", routeKey(route), route.Method)
	}
	for _, set := range []map[string]bool{mutating, reads} {
		for method := range set {
			assert.True(t, registered[method], "no route uses %s: drop it from the walk's methods", method)
		}
	}
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

	// After seeding, every row but A's user row is B's or hangs off B's, so the snapshot must
	// hold all of them. A table it misses (no user_id, no pointer columns() recognizes) is a
	// table the walk would not protect.
	var tables []string
	require.NoError(t, e.db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'schema_migrations'").Scan(&tables).Error)
	conds := e.snapshotConds(t)
	for _, table := range tables {
		var total int64
		require.NoError(t, e.db.Table(table).Count(&total).Error)
		if table == "users" {
			total-- // A
		}
		var held int
		if c, ok := conds[table]; ok {
			held = len(e.snapshotRows(t, table, c))
		}
		assert.EqualValues(t, total, held, "the snapshot holds %d of the %d seeded rows in %s: give it a user_id, a foreign key, or a *contact*_id column", held, total, table)
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
		var fn *ast.FuncDecl
		for _, decl := range parsed.Decls {
			if f, ok := decl.(*ast.FuncDecl); ok && f.Recv == nil && f.Name.Name == p.test {
				fn = f
			}
		}
		if !assert.NotNil(t, fn, "%s: %s has no top-level test %s", key, p.file, p.test) {
			continue
		}
		assert.NotEmpty(t, fn.Body.List, "%s: %s is empty", key, p.test)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "Skip") {
				t.Errorf("%s: %s calls %s, so nothing proves the route's isolation", key, p.test, sel.Sel.Name)
			}
			return true
		})
	}
}
