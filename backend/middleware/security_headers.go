package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// apiCSP is the policy for API responses. They are JSON, XML, calendar or file
// downloads, never pages, so nothing may load or run from them. The SPA gets its own,
// looser policy from the web server (frontend/security-headers.conf).
const apiCSP = "default-src 'none'; frame-ancestors 'none'"

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
		switch {
		case enableHSTS:
			// The operator declared HTTPS (COOKIE_SECURE), not a header a client could send.
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		case c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https"):
			c.Header("Strict-Transport-Security", "max-age=31536000")
		}
		c.Next()
	}
}
