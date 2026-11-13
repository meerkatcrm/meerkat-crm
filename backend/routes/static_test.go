package routes

import (
	"bytes"
	"compress/gzip"
	"io"
	"meerkat/middleware"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bundle the tests serve: index.html and unhashed files at the root, the content-hashed
// output under static/.
const (
	indexBody  = "<!doctype html><title>Meerkat CRM</title>"
	favicon    = "icon-bytes"
	bundleBody = "console.log('the bundle');"
	cssBody    = "body{margin:0}"
)

// writeBundle lays out a frontend build in a temporary directory and returns its path.
// The hashed file is over the gzip minimum so compression has something to prove.
func writeBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"index.html":               indexBody,
		"favicon.ico":              favicon,
		"robots.txt":               "User-agent: *\n",
		"static/js/main.a1b2.js":   strings.Repeat(bundleBody, 200),
		"static/css/main.a1b2.css": cssBody,
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return dir
}

// staticRouter is main.go's chain for the parts that matter here: the API policy on
// everything, and a few API routes to keep out of the bundle's way. /webhooks/incoming/:id
// stands for any group that exists today and was never named in this file.
func staticRouter(t *testing.T, frontendDir string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	cfg := testConfig()
	cfg.FrontendDir = frontendDir

	r := gin.New()
	r.Use(middleware.SecurityHeadersMiddleware(cfg.CookieSecure))
	r.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/api/v1/contacts", func(c *gin.Context) {
		c.String(http.StatusOK, strings.Repeat("contact,", 300))
	})
	r.POST("/api/v1/login", func(c *gin.Context) { c.String(http.StatusOK, "login") })
	r.POST("/webhooks/incoming/:id", func(c *gin.Context) { c.String(http.StatusOK, "hook") })
	require.NoError(t, RegisterStaticRoutes(r, cfg))
	return r
}

// The point of reading the route table instead of listing the API's prefixes: a group nobody
// thought to write down here still keeps its own namespace, so a mistyped call under it is a
// 404 rather than the app with a 200.
func TestGuardFollowsTheRouteTable(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	require.Equal(t, http.StatusOK, request(r, http.MethodPost, "/webhooks/incoming/7", nil).Code)
	for _, path := range []string{"/webhooks/incoming/typo", "/webhooks/incoming/", "/webhooks/nothing"} {
		w := request(r, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusNotFound, w.Code, path)
		assert.NotEqual(t, indexBody, w.Body.String(), path)
	}
	// A namespace nothing registered is still the app's, /webhooks aside.
	assert.Equal(t, indexBody, request(r, http.MethodGet, "/webhooks-ui", nil).Body.String())
}

// Namespaces are claimed by whole segment, so a near miss on an operational path is an
// unknown path and gets the app, the way any other unknown path does. /health itself is a
// route, so the healthcheck is unaffected.
func TestNearMissOnAClaimedNamespaceIsTheApp(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	assert.Equal(t, http.StatusOK, request(r, http.MethodGet, "/health", nil).Code)
	assert.Equal(t, indexBody, request(r, http.MethodGet, "/healthz", nil).Body.String())
}

func TestRootServesTheWebApp(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	for _, path := range []string{"/", "/contacts", "/contacts/17", "/login", "/settings/profile"} {
		w := request(r, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, w.Code, path)
		assert.Equal(t, indexBody, w.Body.String(), path)
	}
}

// index.html names the hashed bundles, so a client that reused it without revalidating
// would keep loading the previous release's assets.
func TestRootIsNeverServedFromCache(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	for _, path := range []string{"/", "/contacts/17", "/favicon.ico", "/robots.txt"} {
		w := request(r, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, w.Code, path)
		assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"), path)
	}
}

// http.ServeFile canonicalizes /index.html to the directory, which is the behaviour worth
// having: the file that must never be reused without revalidating has one URL and one cache
// key, and both answers say so.
func TestIndexHtmlCanonicalizesToTheRoot(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	w := request(r, http.MethodGet, "/index.html", nil)
	require.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "./", w.Header().Get("Location"), "relative, so the port and host survive it")
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.NotContains(t, w.Header().Get("Content-Security-Policy"), "default-src 'none'")

	final := request(r, http.MethodGet, "/", nil)
	assert.Equal(t, indexBody, final.Body.String())
	assert.Equal(t, "no-cache", final.Header().Get("Cache-Control"))
}

func TestRootServesItsOwnFiles(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	w := request(r, http.MethodGet, "/favicon.ico", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, favicon, w.Body.String())
	assert.Equal(t, "image/vnd.microsoft.icon", w.Header().Get("Content-Type"))
}

func TestStaticServesTheHashedBundle(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	for path, body := range map[string]string{
		"/static/js/main.a1b2.js":   strings.Repeat(bundleBody, 200),
		"/static/css/main.a1b2.css": cssBody,
	} {
		w := request(r, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, w.Code, path)
		assert.Equal(t, body, w.Body.String(), path)
		// The hash in the filename changes with the bytes, so the browser may pin it.
		assert.Equal(t, "public, max-age=31536000, immutable", w.Header().Get("Cache-Control"), path)
	}
}

// A missing bundle file is a 404 and not index.html: a page under a .js URL is how a broken
// release turns into a confusing one. router.Static would have handed these to NoRoute.
func TestMissingAssetIsNotFoundAndNotTheApp(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	for _, path := range []string{"/static/js/main.missing.js", "/static/absent.txt", "/static/"} {
		w := request(r, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusNotFound, w.Code, path)
		assert.NotEqual(t, indexBody, w.Body.String(), path)
	}
}

// Under the root, a request with no file behind it is an app route: try_files sent every
// path it could not find to index.html, missing images included.
func TestMissingRootFileFallsBackToTheApp(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	for _, path := range []string{"/absent.png", "/no-such-page", "/contacts/999999"} {
		w := request(r, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusOK, w.Code, path)
		assert.Equal(t, indexBody, w.Body.String(), path)
	}
}

// A typo in an API path is a broken API call: answering it with the app would report a
// successful call whose body is HTML. The paths here are the ones this router registered;
// the ones the real table adds are covered against that table below.
func TestAPIPathsKeepTheirOwnNotFound(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/typo"},
		{http.MethodGet, "/api/v1/contacts/1/typo"},
		{http.MethodPost, "/api/v1/typo"},
		// GET /api/v1/login is a wrong method, still an API answer and not the app.
		{http.MethodGet, "/api/v1/login"},
		{http.MethodGet, "/api"},
		{http.MethodDelete, "/contacts/17"},
	}
	for _, c := range cases {
		w := request(r, c.method, c.path, nil)
		assert.Equal(t, http.StatusNotFound, w.Code, c.method+" "+c.path)
		assert.NotEqual(t, indexBody, w.Body.String(), c.method+" "+c.path)
		// The API policy survives: the bundle's would let the response run scripts.
		assert.Equal(t, "default-src 'none'; frame-ancestors 'none'", w.Header().Get("Content-Security-Policy"), c.path)
	}

	// The routes themselves are untouched.
	assert.Equal(t, http.StatusOK, request(r, http.MethodGet, "/api/v1/contacts", nil).Code)
	assert.Equal(t, http.StatusOK, request(r, http.MethodGet, "/health", nil).Code)
}

// The web app needs a policy of its own: the API's `default-src 'none'` refuses the bundle
// its scripts.
func TestBundleGetsTheWebAppPolicy(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	for _, path := range []string{"/", "/contacts/17", "/static/js/main.a1b2.js"} {
		w := request(r, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, w.Code, path)
		csp := w.Header().Get("Content-Security-Policy")
		for _, directive := range []string{"default-src 'self'", "script-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
			assert.Contains(t, csp, directive, path)
		}
		assert.NotContains(t, csp, "default-src 'none'", path)
		assert.Equal(t, "same-origin", w.Header().Get("Cross-Origin-Resource-Policy"), path)
	}
}

func TestBundleHSTSOnlyOverHTTPS(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	assert.Empty(t, request(r, http.MethodGet, "/", nil).Header().Get("Strict-Transport-Security"))
	forwarded := request(r, http.MethodGet, "/", func(req *http.Request) {
		req.Header.Set("X-Forwarded-Proto", "https")
	})
	assert.Equal(t, "max-age=31536000", forwarded.Header().Get("Strict-Transport-Security"))
}

// The bundler's assets are ~1MB of JS, so they are compressed as nginx did; the API keeps
// the Content-Length its responses set.
func TestOnlyTheBundleIsGzipped(t *testing.T) {
	r := staticRouter(t, writeBundle(t))
	gzipClient := func(req *http.Request) { req.Header.Set("Accept-Encoding", "gzip") }

	w := request(r, http.MethodGet, "/static/js/main.a1b2.js", gzipClient)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", w.Header().Get("Vary"))
	assert.Less(t, w.Body.Len(), len(bundleBody)*200, "the response is smaller than the file")
	zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)
	decoded, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat(bundleBody, 200), string(decoded))

	// nginx' gzip_min_length: compressing a small file only makes it bigger.
	small := request(r, http.MethodGet, "/favicon.ico", gzipClient)
	assert.Empty(t, small.Header().Get("Content-Encoding"))

	// The API is excluded even when it is large enough to be worth compressing.
	api := request(r, http.MethodGet, "/api/v1/contacts", gzipClient)
	assert.Empty(t, api.Header().Get("Content-Encoding"))
	assert.Equal(t, strings.Repeat("contact,", 300), api.Body.String(), "the body is the bytes it set")
}

func TestBundleAnswersHead(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	for _, path := range []string{"/", "/static/js/main.a1b2.js"} {
		w := request(r, http.MethodHead, path, nil)
		require.Equal(t, http.StatusOK, w.Code, path)
		assert.Empty(t, w.Body.String(), path)
	}
}

// Nothing may read past the bundle: the router cleans the path, and the handler cleans it
// again because it builds a filesystem path out of it.
func TestBundleCannotBeClimbedOutOf(t *testing.T) {
	dir := writeBundle(t)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("the host"), 0o644))
	r := staticRouter(t, dir)

	for _, path := range []string{"/../secret.txt", "/static/../../secret.txt", "/%2e%2e/%2e%2e/secret.txt"} {
		w := request(r, http.MethodGet, path, nil)
		assert.NotEqual(t, "the host", w.Body.String(), path)
	}
}

func TestNoRoutesWithoutFrontendDir(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	cfg := testConfig() // FRONTEND_DIR unset
	r := gin.New()
	require.NoError(t, RegisterStaticRoutes(r, cfg))

	assert.Equal(t, http.StatusNotFound, request(r, http.MethodGet, "/", nil).Code)
}

// A FRONTEND_DIR the operator set but that holds no bundle is a broken image, and saying so
// beats serving an API with no page in front of it.
func TestFrontendDirWithoutABundleIsAnError(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	cfg := testConfig()
	cfg.FrontendDir = filepath.Join(t.TempDir(), "nope")
	err := RegisterStaticRoutes(gin.New(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index.html")

	cfg.FrontendDir = t.TempDir() // exists, but empty
	assert.Error(t, RegisterStaticRoutes(gin.New(), cfg))
}

// The full route table has to accept the two static paths: gin rejects a catch-all that
// collides with a registered prefix, and only registering both says so.
func TestStaticRoutesCoexistWithEveryRoute(t *testing.T) {
	cfg := testConfig()
	cfg.FrontendDir = writeBundle(t)
	router, _ := newRouter(t, cfg, nil)
	require.NoError(t, RegisterStaticRoutes(router, cfg))

	assert.Equal(t, http.StatusOK, request(router, http.MethodGet, "/", nil).Code)
	assert.Equal(t, indexBody, request(router, http.MethodGet, "/contacts/17", nil).Body.String())

	// A path the API owns still reaches the API: 401 from its auth middleware, not the app.
	w := request(router, http.MethodGet, "/api/v1/contacts", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NotEqual(t, indexBody, w.Body.String())

	// Every namespace the real table claims keeps its own answer for a path below it that
	// matched nothing: /carddav/ and /.well-known/ come from CardDAV being on in testConfig,
	// and no list in this file named them.
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/typo"},
		{http.MethodGet, "/carddav/typo"},
		{http.MethodGet, "/carddav/"},
		{http.MethodGet, "/.well-known/carddav"},
	} {
		got := request(router, c.method, c.path, nil)
		assert.NotEqual(t, indexBody, got.Body.String(), c.path)
		assert.NotContains(t, got.Header().Get("Content-Security-Policy"), "default-src 'self'", c.path)
	}
}

// A path no route claimed is the app's, whatever it is called: this is what makes the guard
// need no maintenance, and the price is that an operational path that is not configured is
// indistinguishable from any other unknown path.
func TestUnclaimedPathsAreTheApp(t *testing.T) {
	r := staticRouter(t, writeBundle(t))

	// /metrics has no route while METRICS_ENABLED is off, so it answers with the app.
	for _, path := range []string{"/metrics", "/nope"} {
		w := request(r, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusOK, w.Code, path)
		assert.Equal(t, indexBody, w.Body.String(), path)
	}
}
