package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"meerkat/clock"
	"meerkat/models"

	"github.com/stretchr/testify/assert"
)

// A server that answers is not "unreachable": server errors and unparsable answers are
// reported as such, with the start of the server's error text.
func TestContactSyncTellsTheUserWhatWentWrong(t *testing.T) {
	respond := func(status int, body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	cases := []struct {
		name    string
		address string
		want    error
		detail  string
	}{
		{"a response that is not valid XML", respond(http.StatusMultiStatus, `<d:multistatus xmlns:d="DAV:"><d:response>`), ErrContactSyncInvalidData, "XML syntax error"},
		{"a server error", respond(http.StatusInternalServerError, "<error>database is down</error>"), ErrContactSyncServerError, "500 Internal Server Error: <error>database is down</error>"},
		{"a long server error", respond(http.StatusBadGateway, strings.Repeat("x", 5000)), ErrContactSyncServerError, "xxx […]"},
		{"a server that does not answer", closed.URL, ErrContactSyncUnreachable, ""},
	}
	for _, tc := range cases {
		env := setupContactSyncEnv(t, models.CardDAVDirectionTwoWay, &davRemote{proxyURL: tc.address, path: "/book/"})

		_, err := env.service.SyncConnection(context.Background(), env.db, env.cfg, env.conn, clock.Real())

		assert.ErrorIs(t, err, tc.want, tc.name)
		assert.Contains(t, env.conn.LastSyncError, tc.want.Error(), tc.name)
		assert.Contains(t, env.conn.LastSyncError, tc.detail, tc.name)
		assert.NotContains(t, env.conn.LastSyncError, strings.Repeat("x", 301), tc.name)
	}
}

// Discovery tells the same cases apart: a server error is not "could not be reached".
func TestContactSyncDiscoveryTellsTheUserWhatWentWrong(t *testing.T) {
	respond := func(status int, body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	for _, tc := range []struct {
		name    string
		address string
		want    error
	}{
		{"a server error", respond(http.StatusInternalServerError, "<error>database is down</error>"), ErrContactSyncServerError},
		{"a web server without WebDAV", respond(http.StatusMethodNotAllowed, ""), ErrContactSyncInvalidData},
		{"a server that does not answer", closed.URL, ErrContactSyncUnreachable},
	} {
		_, err := NewContactSyncService(false).Discover(context.Background(), tc.address+"/dav/", "u", "p")
		assert.ErrorIs(t, err, tc.want, tc.name)
	}
}
