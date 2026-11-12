package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func headersFor(t *testing.T, hsts bool, mutate func(*http.Request)) http.Header {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeadersMiddleware(hsts))
	r.GET("/x", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Header()
}

// API responses are JSON, XML or files, never pages: the strictest CSP applies.
func TestSecurityHeadersOnAPIResponses(t *testing.T) {
	h := headersFor(t, false, nil)
	assert.Equal(t, "default-src 'none'; frame-ancestors 'none'", h.Get("Content-Security-Policy"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", h.Get("Referrer-Policy"))
	assert.Equal(t, "same-origin", h.Get("Cross-Origin-Opener-Policy"))
	assert.Equal(t, "same-site", h.Get("Cross-Origin-Resource-Policy"))
	assert.Contains(t, h.Get("Permissions-Policy"), "camera=()")
	assert.Contains(t, h.Get("Permissions-Policy"), "geolocation=()")
	for name, values := range h {
		assert.Len(t, values, 1, "%s must be sent once", name)
	}
}

func TestHSTSOnlyOverHTTPS(t *testing.T) {
	assert.Empty(t, headersFor(t, false, nil).Get("Strict-Transport-Security"), "plain HTTP")
	assert.Empty(t, headersFor(t, false, func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "http") }).Get("Strict-Transport-Security"))

	want := "max-age=31536000; includeSubDomains"
	assert.Equal(t, want, headersFor(t, true, nil).Get("Strict-Transport-Security"), "COOKIE_SECURE says HTTPS")
	// Derived from the request (a header or TLS): no includeSubDomains
	derived := "max-age=31536000"
	assert.Equal(t, derived, headersFor(t, false, func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") }).Get("Strict-Transport-Security"), "TLS-terminating proxy")
	assert.Equal(t, derived, headersFor(t, false, func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "HTTPS") }).Get("Strict-Transport-Security"))
	assert.Equal(t, derived, headersFor(t, false, func(r *http.Request) { r.TLS = &tls.ConnectionState{} }).Get("Strict-Transport-Security"), "direct TLS")
}
