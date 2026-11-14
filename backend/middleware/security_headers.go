package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// apiCSP is the policy for API responses. They are JSON, XML, calendar or file
// downloads, never pages, so nothing may load or run from them. The SPA gets its own,
// looser policy from SPAHeaders.
const apiCSP = "default-src 'none'; frame-ancestors 'none'"

// spaCSP is the policy for the web app itself, which the API policy would break:
//
//	script-src 'self'          the bundle only, no inline scripts, no eval
//	style-src 'unsafe-inline'  MUI/emotion injects <style> tags at runtime
//	img-src data: blob:        avatars and uploaded photo previews
//	connect-src 'self'         the API is same-origin, this server answers both
const spaCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; manifest-src 'self'; " +
	"worker-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// setHSTS sends the header only when the browser is on HTTPS, so it can never lock anyone
// out of a plain-HTTP instance for the whole max-age.
func setHSTS(c *gin.Context, enableHSTS bool) {
	switch {
	case enableHSTS:
		// The operator declared HTTPS (COOKIE_SECURE), not a header a client could send.
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	case c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https"):
		c.Header("Strict-Transport-Security", "max-age=31536000")
	}
}

// SecurityHeadersMiddleware sets security headers on every backend response.
//
// enableHSTS should be true when the server is reached via HTTPS (COOKIE_SECURE).
// HSTS is also sent (without includeSubDomains, which only an explicit enableHSTS adds) when
// the request arrived over TLS or through a proxy that sets X-Forwarded-Proto: https; otherwise browsers could refuse plain-HTTP access for the
// duration of max-age.
func SecurityHeadersMiddleware(enableHSTS bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy", apiCSP)
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		c.Header("Cross-Origin-Opener-Policy", "same-origin")
		// same-site, not same-origin: the SPA may live on a sibling subdomain and load photos.
		c.Header("Cross-Origin-Resource-Policy", "same-site")
		setHSTS(c, enableHSTS)
		c.Next()
	}
}

// SPAHeaders sets the headers of the web app's own responses over the API policy that
// SecurityHeadersMiddleware put on every response: `default-src 'none'` would refuse the
// bundle its own scripts. A function rather than only a middleware because the bundle's
// fallback is registered as the engine's NoRoute, which takes one handler and no chain.
func SPAHeaders(c *gin.Context, enableHSTS bool) {
	c.Header("Content-Security-Policy", spaCSP)
	c.Header("X-Frame-Options", "DENY")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
	c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	c.Header("Cross-Origin-Opener-Policy", "same-origin")
	// The app's own files, not data another page may embed: stricter than the API's same-site.
	c.Header("Cross-Origin-Resource-Policy", "same-origin")
	setHSTS(c, enableHSTS)
}

// SPAHeadersMiddleware is SPAHeaders for the route groups that serve the bundle.
func SPAHeadersMiddleware(enableHSTS bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		SPAHeaders(c, enableHSTS)
		c.Next()
	}
}
