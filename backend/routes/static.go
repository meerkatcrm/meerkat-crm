package routes

import (
	"fmt"
	"meerkat/config"
	"meerkat/middleware"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// staticPath is where the bundler's content-hashed output is served from; the rest of the
// bundle is served at the root under the same directory name.
const staticPath = "/static"

const textPlain = "text/plain; charset=utf-8"

// notFoundBody is gin's own 404 body, sent so a path the API owns reads the same whether or
// not a bundle is mounted. Context.NotFound is not usable here: it re-enters these handlers.
var notFoundBody = []byte("404 page not found\n")

// bundleGzip compresses the bundle's responses the way nginx' gzip did, same minimum length
// and images left alone. It rides the two bundle routes instead of the engine, so the API
// keeps the Content-Length its responses set without anyone having to list its prefixes here.
func bundleGzip() gin.HandlerFunc {
	return gzip.Gzip(gzip.DefaultCompression, gzip.WithMinLength(1024))
}

// apiNamespaces reports the top-level path segments the registered routes have claimed, so
// that an unmatched request under one of them stays an API call. Taken from the route table
// rather than listed here, because a list goes unnoticed-stale the day someone adds a group:
// /webhooks/incoming/typo would have answered with index.html and a 200.
//
// OnceValue keeps this honest about ordering: it resolves on the first request, so it sees
// every route however main.go orders the registration calls, without walking the trees per request.
func apiNamespaces(router *gin.Engine) func() map[string]bool {
	return sync.OnceValue(func() map[string]bool {
		names := map[string]bool{}
		for _, route := range router.Routes() {
			// The bundle's own paths are the answer, not the question.
			if seg := firstSegment(route.Path); seg != staticPath {
				names[seg] = true
			}
		}
		return names
	})
}

// firstSegment is the top-level namespace of a path: "/api" for both /api/v1/contacts and
// the /api/v1/contacts/:id pattern the route table reports.
func firstSegment(path string) string {
	if len(path) < 2 {
		return path
	}
	if i := strings.IndexByte(path[1:], '/'); i >= 0 {
		return path[:i+1]
	}
	return path
}

// RegisterStaticRoutes serves the built single-page app from cfg.FrontendDir at two paths:
// /static, which holds the bundler's content-hashed output and is pinned in the browser
// cache, and every other path, served from the bundle's root with index.html as the fallback
// so the app's router can own /contacts/17 and friends.
//
// An empty FRONTEND_DIR registers nothing and leaves the API alone, which is what a
// deployment that serves the bundle from its own web server wants.
func RegisterStaticRoutes(router *gin.Engine, cfg *config.Config) error {
	if cfg.FrontendDir == "" {
		return nil
	}
	index := filepath.Join(cfg.FrontendDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		return fmt.Errorf("FRONTEND_DIR %q holds no readable index.html: %w", cfg.FrontendDir, err)
	}
	claimed := apiNamespaces(router)

	// Files with a content hash in the name: the URL changes whenever the bytes do, so the
	// browser may keep them for a year. Served here rather than with router.Static, which
	// hands a missing file to NoRoute and would answer a missing .js with the app and a 200.
	hashed := router.Group(staticPath, middleware.SPAHeadersMiddleware(cfg.CookieSecure), bundleGzip())
	asset := func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		serveBundleFile(c, filepath.Join(cfg.FrontendDir, strings.TrimPrefix(staticPath, "/")), c.Param("filepath"), false)
	}
	hashed.GET("/*filepath", asset)
	hashed.HEAD("/*filepath", asset)

	// A catch-all cannot be a route here: gin refuses /*path alongside the API's own
	// prefixes, so the root falls to the engine's NoRoute. The gzip middleware is that
	// chain's first handler, which is what lets its writer close after the file is served.
	router.NoRoute(bundleGzip(), func(c *gin.Context) {
		readable := c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead
		// A request that is not a read, or one under a namespace the API claimed but that
		// matched no route, is not the app: answering /api/v1/typo with index.html reports a
		// good call whose body is HTML.
		if !readable || claimed()[firstSegment(c.Request.URL.Path)] {
			notFound(c)
			return
		}
		// index.html names the hashed bundles, so nothing under the root is servable from
		// cache without revalidating: a stored copy would keep an updated instance loading
		// the previous release's assets.
		c.Header("Cache-Control", "no-cache")
		middleware.SPAHeaders(c, cfg.CookieSecure)
		serveBundleFile(c, cfg.FrontendDir, c.Request.URL.Path, true)
	})

	return nil
}

func notFound(c *gin.Context) {
	c.Data(http.StatusNotFound, textPlain, notFoundBody)
}

// serveBundleFile serves rel from dir. Without a file behind the request, fallback decides
// between index.html and a plain 404: the root of the app falls back, which is what lets the
// SPA route every path that is neither a file nor the API's, and /static does not, because a
// page under a .js URL turns a broken release into a confusing one.
func serveBundleFile(c *gin.Context, dir, rel string, fallback bool) {
	// The "/" prefix makes Clean discard any .. that would climb out of the bundle.
	name := filepath.Join(dir, filepath.Clean("/"+rel))
	if st, err := os.Stat(name); err != nil || st.IsDir() {
		if !fallback {
			notFound(c)
			return
		}
		name = filepath.Join(dir, "index.html")
	}
	http.ServeFile(c.Writer, c.Request, name)
}
