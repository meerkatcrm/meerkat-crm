package config

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OIDCConfig holds optional OIDC provider settings.
type OIDCConfig struct {
	Enabled            bool
	ProviderURL        string
	ClientID           string
	ClientSecret       string
	RedirectURL        string // derived from FrontendURL, not configurable
	AllowAutoProvision bool
	TrustEmail         bool // skip email_verified requirement when linking accounts (for trusted self-hosted providers)
}

type Config struct {
	DBPath                   string
	ReminderTime             string
	ReminderTimezone         string
	FrontendURL              string
	Port                     string
	TrustedProxies           []string
	UseResend                bool
	ResendAPIKey             string
	ResendFromEmail          string
	ResendToEmail            string
	UseSMTP                  bool
	SMTPHost                 string
	SMTPPort                 int
	SMTPUsername             string
	SMTPPassword             string
	SMTPFromEmail            string
	SMTPUseTLS               bool // implicit TLS (e.g. port 465); otherwise STARTTLS is used when available
	JWTSecretKey             string
	EncryptionKey            string // ENCRYPTION_KEY: base64 of 32 random bytes for the secrets stored in the database
	EncryptionKeyReset       bool   // ENCRYPTION_KEY_RESET: a new key replaces a lost one; what the lost key encrypted is deleted
	JWTExpiryHours           int
	JWTMaxSessionHours       int    // absolute cap on a session, however active (sliding refresh stops here)
	ReadTimeout              int    // HTTP server read timeout in seconds
	WriteTimeout             int    // HTTP server write timeout in seconds
	IdleTimeout              int    // HTTP server idle timeout in seconds
	ProfilePhotoDir          string // Directory for storing profile photos (must be absolute path)
	CardDAVEnabled           bool   // Enable CardDAV server for contact sync
	CookieSecure             bool   // Set Secure flag on auth cookie (requires HTTPS)
	CookieDomain             string // Domain for auth cookie (empty = current domain only)
	RegistrationDisabled     bool   // Disable new user registration
	DemoMode                 bool   // Public demo instance: password changes and photo uploads are refused
	WebhookBlockPrivateURLs  bool   // Block webhook deliveries to private/loopback addresses (useful for cloud deployments)
	CalDAVSyncIntervalHours  int    // Interval in hours for the scheduled calendar sync job
	CalDAVBlockPrivateURLs   bool   // Block calendar sync requests to private/loopback addresses (useful for cloud deployments)
	CardDAVSyncIntervalHours int    // Interval in hours for the scheduled CardDAV contact sync job
	CardDAVBlockPrivateURLs  bool   // Block contact sync requests to private/loopback addresses (defaults to CALDAV_BLOCK_PRIVATE_URLS)
	MonicaBlockPrivateURLs   bool   // Block Monica import requests to private/loopback addresses (useful for cloud deployments)
	OIDC                     OIDCConfig
}

func LoadConfig() *Config {

	defaultJWTExpiry := 96
	jwtExpiryHours, err := strconv.Atoi(getEnv("JWT_EXPIRY_HOURS", strconv.Itoa(defaultJWTExpiry)))
	if err != nil {
		log.Println("WARN: Invalid JWT expiry set. Please provide an integer value.")
		jwtExpiryHours = defaultJWTExpiry
	}

	// Never default below JWT_EXPIRY_HOURS, or deployments with a long expiry fail validation on upgrade.
	defaultJWTMaxSession := max(720, jwtExpiryHours)
	jwtMaxSessionHours, err := strconv.Atoi(getEnv("JWT_MAX_SESSION_HOURS", strconv.Itoa(defaultJWTMaxSession)))
	if err != nil {
		log.Println("WARN: Invalid JWT max session set. Please provide an integer value.")
		jwtMaxSessionHours = defaultJWTMaxSession
	}

	// Parse timeout values with defaults
	readTimeout := getIntEnv("HTTP_READ_TIMEOUT", 15)
	writeTimeout := getIntEnv("HTTP_WRITE_TIMEOUT", 15)
	idleTimeout := getIntEnv("HTTP_IDLE_TIMEOUT", 60)

	cfg := &Config{
		DBPath:                  getEnv("SQLITE_DB_PATH", "meerkat.db"),
		ReminderTime:            getEnv("REMINDER_TIME", "12:00"),
		ReminderTimezone:        getEnv("REMINDER_TIMEZONE", "UTC"),
		FrontendURL:             getEnv("FRONTEND_URL", "*"),
		Port:                    getEnv("PORT", "8080"),
		ResendAPIKey:            getEnv("RESEND_API_KEY", ""),
		ResendFromEmail:         getEnv("RESEND_FROM_EMAIL", ""),
		SMTPHost:                getEnv("SMTP_HOST", ""),
		SMTPPort:                getIntEnv("SMTP_PORT", 587),
		SMTPUsername:            getEnv("SMTP_USERNAME", ""),
		SMTPPassword:            getEnv("SMTP_PASSWORD", ""),
		SMTPFromEmail:           getEnv("SMTP_FROM_EMAIL", ""),
		SMTPUseTLS:              getBoolEnv("SMTP_USE_TLS", false),
		JWTSecretKey:            getEnv("JWT_SECRET_KEY", ""),
		EncryptionKey:           getEnv("ENCRYPTION_KEY", ""),
		EncryptionKeyReset:      getBoolEnv("ENCRYPTION_KEY_RESET", false),
		JWTExpiryHours:          jwtExpiryHours,
		JWTMaxSessionHours:      jwtMaxSessionHours,
		TrustedProxies:          getProxies(getEnv("TRUSTED_PROXIES", "")),
		ReadTimeout:             readTimeout,
		WriteTimeout:            writeTimeout,
		IdleTimeout:             idleTimeout,
		ProfilePhotoDir:         getEnv("PROFILE_PHOTO_DIR", ""),
		CardDAVEnabled:          getBoolEnv("CARDDAV_ENABLED", false),
		CookieSecure:            getBoolEnv("COOKIE_SECURE", false),
		CookieDomain:            getEnv("COOKIE_DOMAIN", ""),
		RegistrationDisabled:    getBoolEnv("DISABLE_REGISTRATION", false),
		DemoMode:                getBoolEnv("DEMO_MODE", false),
		WebhookBlockPrivateURLs: getBoolEnv("WEBHOOK_BLOCK_PRIVATE_URLS", false),
		CalDAVSyncIntervalHours: getIntEnv("CALDAV_SYNC_INTERVAL_HOURS", 6),
		CalDAVBlockPrivateURLs:  getBoolEnv("CALDAV_BLOCK_PRIVATE_URLS", false),
		MonicaBlockPrivateURLs:  getBoolEnv("MONICA_BLOCK_PRIVATE_URLS", false),
	}

	if cfg.CalDAVSyncIntervalHours < 1 {
		log.Println("WARN: CALDAV_SYNC_INTERVAL_HOURS must be at least 1, using 1")
		cfg.CalDAVSyncIntervalHours = 1
	}

	cfg.CardDAVSyncIntervalHours = getIntEnv("CARDDAV_SYNC_INTERVAL_HOURS", 6)
	if cfg.CardDAVSyncIntervalHours < 1 {
		log.Println("WARN: CARDDAV_SYNC_INTERVAL_HOURS must be at least 1, using 1")
		cfg.CardDAVSyncIntervalHours = 1
	}
	cfg.CardDAVBlockPrivateURLs = getBoolEnv("CARDDAV_BLOCK_PRIVATE_URLS", cfg.CalDAVBlockPrivateURLs)
	applyBlockPrivateURLs(cfg)

	// An email channel is enabled only when it is fully configured
	cfg.UseResend = cfg.ResendAPIKey != "" && cfg.ResendFromEmail != ""
	cfg.UseSMTP = cfg.SMTPHost != "" && cfg.SMTPFromEmail != ""

	oidcProviderURL := getEnv("OIDC_PROVIDER_URL", "")
	oidcClientID := getEnv("OIDC_CLIENT_ID", "")
	oidcClientSecret := getEnv("OIDC_CLIENT_SECRET", "")
	cfg.OIDC = OIDCConfig{
		Enabled:            oidcProviderURL != "" && oidcClientID != "" && oidcClientSecret != "",
		ProviderURL:        oidcProviderURL,
		ClientID:           oidcClientID,
		ClientSecret:       oidcClientSecret,
		RedirectURL:        cfg.FrontendURL + "/api/v1/auth/oidc/callback",
		AllowAutoProvision: getBoolEnv("OIDC_AUTO_PROVISION", false),
		TrustEmail:         getBoolEnv("OIDC_TRUST_EMAIL", false),
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		intValue, err := strconv.Atoi(value)
		if err != nil {
			log.Printf("WARN: Invalid integer value for %s: %s. Using default: %d", key, value, fallback)
			return fallback
		}
		return intValue
	}
	return fallback
}

// applyBlockPrivateURLs applies BLOCK_PRIVATE_URLS. When it is set it decides for every
// outbound feature (webhooks, CalDAV, CardDAV, Monica) and overrides the older per-feature
// flags. When it is unset the per-feature flags keep working as before. The default stays
// off: LAN servers are a common legitimate setup. It is a security switch, so a value that
// is not a recognised boolean fails closed: blocking is turned on and an error is logged.
func applyBlockPrivateURLs(cfg *Config) {
	value, set := os.LookupEnv("BLOCK_PRIVATE_URLS")
	if !set {
		return
	}
	block, ok := parseSwitch(value)
	if !ok {
		log.Printf("ERROR: Invalid value for BLOCK_PRIVATE_URLS: %q (use true or false). Blocking private URLs to be safe", value)
		block = true
	}
	cfg.WebhookBlockPrivateURLs = block
	cfg.CalDAVBlockPrivateURLs = block
	cfg.CardDAVBlockPrivateURLs = block
	cfg.MonicaBlockPrivateURLs = block
}

// parseSwitch accepts true/false, 1/0, yes/no and on/off in any case.
func parseSwitch(value string) (on, ok bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}

// PrivateURLFeatures lists the outbound features that may still reach private,
// loopback or link-local addresses. It is empty when every feature is blocked.
func (c Config) PrivateURLFeatures() []string {
	features := []string{}
	for _, f := range []struct {
		name    string
		blocked bool
	}{{"webhooks", c.WebhookBlockPrivateURLs}, {"caldav", c.CalDAVBlockPrivateURLs}, {"carddav", c.CardDAVBlockPrivateURLs}, {"monica", c.MonicaBlockPrivateURLs}} {
		if !f.blocked {
			features = append(features, f.name)
		}
	}
	return features
}

// PrivateURLsAllowed reports whether any outbound feature may still reach private,
// loopback or link-local addresses.
func (c Config) PrivateURLsAllowed() bool {
	return len(c.PrivateURLFeatures()) > 0
}

func getBoolEnv(key string, fallback bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		boolValue, err := strconv.ParseBool(value)
		if err != nil {
			log.Printf("WARN: Invalid boolean value for %s: %s. Using default: %v", key, value, fallback)
			return fallback
		}
		return boolValue
	}
	return fallback
}

func getProxies(proxies string) []string {
	if proxies == "" {
		return nil
	}

	proxyList := strings.Split(proxies, ",")
	for i, proxy := range proxyList {
		proxyList[i] = strings.TrimSpace(proxy) // Remove whitespaces
	}
	return proxyList
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("Configuration Error [%s]: %s", e.Field, e.Message)
}

// Validate checks if the configuration is valid and returns detailed errors if not
func (c *Config) Validate() []ValidationError {
	var errors []ValidationError

	// Validate JWT Secret Key - critical for security
	if c.JWTSecretKey == "" {
		errors = append(errors, ValidationError{
			Field:   "JWT_SECRET_KEY",
			Message: "JWT secret key is required for authentication. Set JWT_SECRET_KEY environment variable.",
		})
	} else if len(c.JWTSecretKey) < 32 {
		errors = append(errors, ValidationError{
			Field:   "JWT_SECRET_KEY",
			Message: fmt.Sprintf("JWT secret key is too short (%d characters). Must be at least 32 characters for security.", len(c.JWTSecretKey)),
		})
	}

	if msg := c.encryptionKeyProblem(); msg != "" {
		errors = append(errors, ValidationError{Field: "ENCRYPTION_KEY", Message: msg})
	}

	// Validate Database Path
	if c.DBPath == "" {
		errors = append(errors, ValidationError{
			Field:   "SQLITE_DB_PATH",
			Message: "Database path cannot be empty. Set SQLITE_DB_PATH environment variable.",
		})
	}

	// Validate Profile Photo Directory - must be set and absolute path for security
	if c.ProfilePhotoDir == "" {
		errors = append(errors, ValidationError{
			Field:   "PROFILE_PHOTO_DIR",
			Message: "Profile photo directory is required. Set PROFILE_PHOTO_DIR environment variable to an absolute path.",
		})
	} else if !filepath.IsAbs(c.ProfilePhotoDir) {
		errors = append(errors, ValidationError{
			Field:   "PROFILE_PHOTO_DIR",
			Message: fmt.Sprintf("Profile photo directory '%s' must be an absolute path for security.", c.ProfilePhotoDir),
		})
	}

	// Validate Port
	if c.Port == "" {
		errors = append(errors, ValidationError{
			Field:   "PORT",
			Message: "Server port cannot be empty. Set PORT environment variable.",
		})
	} else {
		portNum, err := strconv.Atoi(c.Port)
		if err != nil || portNum < 1 || portNum > 65535 {
			errors = append(errors, ValidationError{
				Field:   "PORT",
				Message: fmt.Sprintf("Invalid port number '%s'. Must be between 1 and 65535.", c.Port),
			})
		}
	}

	// Validate Reminder Time format (HH:MM)
	timePattern := regexp.MustCompile(`^([0-1][0-9]|2[0-3]):[0-5][0-9]$`)
	if !timePattern.MatchString(c.ReminderTime) {
		errors = append(errors, ValidationError{
			Field:   "REMINDER_TIME",
			Message: fmt.Sprintf("Invalid time format '%s'. Must be in HH:MM format (e.g., 12:00).", c.ReminderTime),
		})
	}

	// Validate Reminder Timezone (must be a valid IANA timezone name)
	if _, err := time.LoadLocation(c.ReminderTimezone); err != nil {
		errors = append(errors, ValidationError{
			Field:   "REMINDER_TIMEZONE",
			Message: fmt.Sprintf("Invalid timezone '%s'. Must be a valid IANA timezone name (e.g., 'UTC', 'Europe/Berlin', 'America/New_York').", c.ReminderTimezone),
		})
	}

	// Validate Frontend URL
	if c.FrontendURL == "" {
		errors = append(errors, ValidationError{
			Field:   "FRONTEND_URL",
			Message: "Frontend URL cannot be empty. Set FRONTEND_URL environment variable (use '*' for development).",
		})
	}

	// Validate JWT Expiry Hours
	if c.JWTExpiryHours < 1 || c.JWTExpiryHours > 8760 {
		errors = append(errors, ValidationError{
			Field:   "JWT_EXPIRY_HOURS",
			Message: fmt.Sprintf("Invalid JWT expiry hours '%d'. Must be between 1 and 8760 (1 year).", c.JWTExpiryHours),
		})
	}

	// Validate JWT Max Session Hours
	if c.JWTMaxSessionHours < c.JWTExpiryHours || c.JWTMaxSessionHours > 8760 {
		errors = append(errors, ValidationError{
			Field:   "JWT_MAX_SESSION_HOURS",
			Message: fmt.Sprintf("Invalid JWT max session hours '%d'. Must be between JWT_EXPIRY_HOURS (%d) and 8760 (1 year).", c.JWTMaxSessionHours, c.JWTExpiryHours),
		})
	}

	// Validate HTTP Timeouts (in seconds)
	if c.ReadTimeout < 1 || c.ReadTimeout > 300 {
		errors = append(errors, ValidationError{
			Field:   "HTTP_READ_TIMEOUT",
			Message: fmt.Sprintf("Invalid read timeout '%d'. Must be between 1 and 300 seconds.", c.ReadTimeout),
		})
	}
	if c.WriteTimeout < 1 || c.WriteTimeout > 300 {
		errors = append(errors, ValidationError{
			Field:   "HTTP_WRITE_TIMEOUT",
			Message: fmt.Sprintf("Invalid write timeout '%d'. Must be between 1 and 300 seconds.", c.WriteTimeout),
		})
	}
	if c.IdleTimeout < 1 || c.IdleTimeout > 300 {
		errors = append(errors, ValidationError{
			Field:   "HTTP_IDLE_TIMEOUT",
			Message: fmt.Sprintf("Invalid idle timeout '%d'. Must be between 1 and 300 seconds.", c.IdleTimeout),
		})
	}

	// Validate Trusted Proxies format (IP addresses or CIDR notation)
	for _, proxy := range c.TrustedProxies {
		if proxy == "" {
			continue
		}
		// Check if it's a valid IP address
		if ip := net.ParseIP(proxy); ip != nil {
			continue
		}
		// Check if it's a valid CIDR
		if _, _, err := net.ParseCIDR(proxy); err == nil {
			continue
		}
		errors = append(errors, ValidationError{
			Field:   "TRUSTED_PROXIES",
			Message: fmt.Sprintf("Invalid proxy '%s'. Must be a valid IP address or CIDR notation.", proxy),
		})
	}

	// Warn if OIDC is partially configured (some vars set but not all required ones)
	oidcVars := []string{c.OIDC.ProviderURL, c.OIDC.ClientID, c.OIDC.ClientSecret}
	oidcSet := 0
	for _, v := range oidcVars {
		if v != "" {
			oidcSet++
		}
	}
	if oidcSet > 0 && oidcSet < 3 {
		log.Println("WARN: OIDC is partially configured. Set OIDC_PROVIDER_URL, OIDC_CLIENT_ID, and OIDC_CLIENT_SECRET to enable SSO.")
	}

	// UseResend and UseSMTP are derived from these fields being set, so a
	// half-configured channel is silently off. Say so instead.
	if (c.ResendAPIKey != "") != (c.ResendFromEmail != "") {
		log.Println("WARN: Resend is partially configured. Set both RESEND_API_KEY and RESEND_FROM_EMAIL to send email through Resend.")
	}
	if (c.SMTPHost != "") != (c.SMTPFromEmail != "") {
		log.Println("WARN: SMTP is partially configured. Set both SMTP_HOST and SMTP_FROM_EMAIL to send email through SMTP.")
	}
	if c.UseSMTP && (c.SMTPPort < 1 || c.SMTPPort > 65535) {
		errors = append(errors, ValidationError{
			Field:   "SMTP_PORT",
			Message: fmt.Sprintf("Invalid SMTP port '%d'. Must be between 1 and 65535.", c.SMTPPort),
		})
	}

	// A wildcard origin is for development. With cookies it lets any site call the
	// API as the logged-in user wherever the browser sends them, and OIDC cannot
	// work at all: the redirect URL is built from FRONTEND_URL.
	if c.FrontendURL == "*" {
		if c.OIDC.Enabled {
			errors = append(errors, ValidationError{
				Field:   "FRONTEND_URL",
				Message: "FRONTEND_URL must be the real origin (not '*') when OIDC is enabled: the OIDC redirect URL is derived from it.",
			})
		}
		if os.Getenv("GIN_MODE") == "release" {
			log.Println("WARN: FRONTEND_URL is '*', so any website may call the API with the user's session. Set it to your frontend's origin.")
		}
	}

	return errors
}

// EmailEnabled reports whether at least one email delivery channel is configured.
func (c *Config) EmailEnabled() bool {
	return c.UseResend || c.UseSMTP
}

// GetReminderLocation returns the parsed time.Location for the configured ReminderTimezone.
// Falls back to UTC if the timezone is invalid (validation should prevent this in practice).
func (c *Config) GetReminderLocation() *time.Location {
	loc, err := time.LoadLocation(c.ReminderTimezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// ValidateOrPanic validates the configuration and panics with detailed error message if invalid
func (c *Config) ValidateOrPanic() {
	errors := c.Validate()
	if len(errors) > 0 {
		log.Println("❌ Configuration validation failed:")
		log.Println("")
		for _, err := range errors {
			log.Printf("  • %s\n", err.Error())
		}
		log.Println("")
		log.Println("Please fix the configuration errors above and restart the server.")
		log.Println("Refer to backend/.env.example for configuration examples.")
		panic("Configuration validation failed")
	}
	log.Println("✓ Configuration validated successfully")
}
