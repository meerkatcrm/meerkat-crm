package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"meerkat/config"
	"meerkat/controllers"
	"meerkat/database"
	apperrors "meerkat/errors"
	"meerkat/i18n"
	"meerkat/logger"
	"meerkat/middleware"
	"meerkat/routes"
	"meerkat/services"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-co-op/gocron"
	"gorm.io/gorm"
)

func main() {
	// Initialize logger first
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	isPretty := os.Getenv("LOG_PRETTY")
	prettyLog := isPretty == "true" || isPretty == "1"

	// In development, use pretty logs by default
	if os.Getenv("GIN_MODE") != "release" {
		prettyLog = true
	}

	logger.InitLogger(logger.Config{
		Level:  logLevel,
		Pretty: prettyLog,
	})

	logger.Info().Msg("Loading server...")

	logger.Info().Msg("Loading configuration...")
	cfg := config.LoadConfig()

	logger.Info().Msg("Validating configuration...")
	cfg.ValidateOrPanic()

	logger.Info().Msg("Loading database and running migrations...")
	db, err := database.InitDB(cfg.DBPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize database")
	}

	prepareEncryptionKey(db, cfg)

	if warn, users, err := services.PrivateURLWarning(db, cfg.PrivateURLsAllowed()); err != nil {
		logger.Warn().Err(err).Msg("Could not check the private URL setting")
	} else if warn {
		logger.Warn().Int64("users", users).Strs("features", cfg.PrivateURLFeatures()).
			Msg("Webhooks, calendar/contact sync or Monica import may reach private and loopback addresses " +
				"on an instance with several users. Set BLOCK_PRIVATE_URLS=true to block them")
	}

	logger.Info().Msg("Initializing i18n translations...")
	if err := i18n.Init(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize i18n")
	}

	logger.Info().Msg("Running scheduler...")
	// Schedule the reminder task daily
	if !cfg.EmailEnabled() {
		logger.Warn().Msg("No emails will be sent: neither Resend nor SMTP is configured")
	}
	s := gocron.NewScheduler(cfg.GetReminderLocation())
	task := func() {
		// Use rate-limited version to prevent duplicate emails during rapid restarts
		if err := services.SendRemindersWithRateLimit(db, *cfg); err != nil {
			logger.Error().Err(err).Msg("Error sending reminders")
		}
	}
	schedule(s.Every(1).Day().At(cfg.ReminderTime).Do(task))
	go task() // Run initially once on startup (rate-limited to prevent duplicates)
	schedule(s.Every(5).Minutes().Do(func() {
		services.ProcessWebhookRetries(db, *cfg)
	}))
	schedule(s.Every(10).Minutes().Do(controllers.CleanupImportSessions))
	// Sync calendar subscriptions regularly (rate-limited via job lock).
	// Interval jobs also run once when the scheduler starts, so unlike the daily
	// reminder task above they need no separate startup call.
	calendarSyncTask := func() {
		services.SyncCalendarsWithRateLimit(db, *cfg)
	}
	schedule(s.Every(cfg.CalDAVSyncIntervalHours).Hours().Do(calendarSyncTask))
	// Sync CardDAV contact connections regularly (rate-limited via job lock)
	contactSyncTask := func() {
		services.SyncContactsWithRateLimit(db, *cfg)
	}
	schedule(s.Every(cfg.CardDAVSyncIntervalHours).Hours().Do(contactSyncTask))
	go s.StartBlocking()

	r := gin.Default()

	// Limit multipart form memory to 10MB to prevent DoS via large request bodies
	r.MaxMultipartMemory = 10 << 20 // 10 MB

	// For production, set FRONTEND_URL to specific origin(s) like "https://yourdomain.com"
	corsConfig := cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "PROPFIND", "REPORT", "MKCOL", "COPY", "MOVE"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Depth", "If-Match", "If-None-Match"},
		ExposeHeaders:    []string{"Content-Length", "ETag", "Content-Disposition"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour, // Cache preflight for 12 hours
	}

	// Handle wildcard "*" for development: allow any origin
	if cfg.FrontendURL == "*" {
		corsConfig.AllowOriginFunc = func(origin string) bool {
			return true // Allow all origins in development
		}
	} else {
		// Production: allow specific origin(s)
		corsConfig.AllowOrigins = []string{cfg.FrontendURL}
	}

	r.Use(cors.New(corsConfig))

	// Add security headers (HSTS only when serving over HTTPS, signaled by COOKIE_SECURE)
	r.Use(middleware.SecurityHeadersMiddleware(cfg.CookieSecure))

	// Add request body size limit middleware (10MB default) to prevent DoS
	r.Use(middleware.DefaultBodySizeLimitMiddleware())

	// Add request ID middleware for tracing
	r.Use(middleware.RequestIDMiddleware())

	// Add logging middleware (after request ID)
	r.Use(middleware.LoggingMiddleware())

	// Add error handling middleware
	r.Use(apperrors.ErrorHandlerMiddleware())

	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		logger.Fatal().Err(err).Strs("proxies", cfg.TrustedProxies).Msg("Failed to set trusted proxies")
	}

	// Inject db and cfg into context
	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})

	// Initialize OIDC provider if configured
	var oidcProvider *services.OIDCProvider
	if cfg.OIDC.Enabled {
		logger.Info().Str("provider", cfg.OIDC.ProviderURL).Msg("Initializing OIDC provider...")
		oidcCtx, oidcCancel := context.WithTimeout(context.Background(), 30*time.Second)
		var oidcErr error
		oidcProvider, oidcErr = services.InitOIDCProvider(oidcCtx, cfg)
		oidcCancel()
		if oidcErr != nil {
			logger.Fatal().Err(oidcErr).Msg("Failed to initialize OIDC provider")
		}
		logger.Info().Msg("OIDC provider initialized successfully")
	}

	// Register all routes from routes.go
	routes.RegisterRoutes(r, cfg, db, oidcProvider)

	// Create HTTP server with timeout configuration
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      r,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.IdleTimeout) * time.Second,
	}

	logger.Info().
		Str("port", cfg.Port).
		Int("read_timeout", cfg.ReadTimeout).
		Int("write_timeout", cfg.WriteTimeout).
		Int("idle_timeout", cfg.IdleTimeout).
		Msg("Starting server")

	// Graceful shutdown handling
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("Failed to run server")
		}
	}()

	logger.Info().Msg("Server is ready to handle requests")

	// Block until we receive a shutdown signal
	<-quit
	logger.Info().Msg("Shutting down server...")

	// Stop the scheduler first to prevent new jobs from starting
	logger.Info().Msg("Stopping scheduler...")
	s.Stop()

	// Create a deadline to wait for active requests to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Attempt graceful shutdown of HTTP server
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("Server forced to shutdown")
	}

	// Close database connection
	logger.Info().Msg("Closing database connection...")
	sqlDB, err := db.DB()
	if err == nil {
		if err := sqlDB.Close(); err != nil {
			logger.Error().Err(err).Msg("Error closing database connection")
		}
	}

	logger.Info().Msg("Server exited gracefully")
}

// schedule logs a failure to register a scheduled job.
func schedule(_ *gocron.Job, err error) {
	if err != nil {
		logger.Error().Err(err).Msg("Failed to schedule job")
	}
}

// prepareEncryptionKey stops the server when ENCRYPTION_KEY does not fit the database, and
// moves the passwords from before it to it (docs/deployment.md#encryption-key).
func prepareEncryptionKey(db *gorm.DB, cfg *config.Config) {
	moved, err := services.PrepareCredentialKeys(db, services.CredentialKeysOf(*cfg), cfg.EncryptionKeyReset)
	switch {
	case errors.Is(err, services.ErrEncryptionKeyNeeded), errors.Is(err, services.ErrEncryptionKeyWrong):
		log.Printf("❌ %v.\n"+
			"    To fix this, set ENCRYPTION_KEY to the value this database was encrypted with and\n"+
			"    restart Meerkat. If you restored a backup, use the key from the time of the backup.\n"+
			"    If the key is lost: set a new ENCRYPTION_KEY (openssl rand -base64 32) and\n"+
			"    ENCRYPTION_KEY_RESET=true, start Meerkat once, then remove ENCRYPTION_KEY_RESET.\n"+
			"    This deletes the stored CardDAV and calendar passwords; users must enter them again.\n"+
			"    See docs/deployment.md#encryption-key.", err)
		logger.Fatal().Msg("ENCRYPTION_KEY does not fit the database")
	case err != nil:
		logger.Fatal().Err(err).Msg("Failed to prepare ENCRYPTION_KEY")
	case cfg.EncryptionKeyBytes() == nil:
		log.Printf("❌ ENCRYPTION_KEY is not set. Meerkat starts and keeps the stored CardDAV and calendar\n"+
			"    passwords encrypted with a key derived from JWT_SECRET_KEY, but a future version will\n"+
			"    not start without it.\n%s", config.EncryptionKeySteps)
	}
	switch {
	case moved.Reset:
		log.Printf("❌ ENCRYPTION_KEY_RESET: the values of the lost key were deleted. Stored CardDAV and\n" +
			"    calendar passwords must be entered again. Remove ENCRYPTION_KEY_RESET now.")
	case cfg.EncryptionKeyReset:
		logger.Warn().Msg("ENCRYPTION_KEY_RESET is set, but ENCRYPTION_KEY fits the database: nothing was reset. Remove ENCRYPTION_KEY_RESET")
	}
	if moved.Moved > 0 {
		logger.Info().Int("passwords", moved.Moved).Msg("Moved stored CardDAV and calendar passwords to ENCRYPTION_KEY")
	}
	if moved.Failed > 0 {
		logger.Warn().Int("passwords", moved.Failed).Msg("Stored CardDAV or calendar passwords could not be decrypted and stay unchanged; their users must enter them again")
	}
}
