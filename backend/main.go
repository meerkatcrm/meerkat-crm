package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"meerkat/clock"
	"meerkat/config"
	"meerkat/controllers"
	"meerkat/database"
	apperrors "meerkat/errors"
	"meerkat/i18n"
	"meerkat/logger"
	"meerkat/metrics"
	"meerkat/middleware"
	"meerkat/routes"
	"meerkat/services"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-co-op/gocron"
	"github.com/golang-jwt/jwt/v4"
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
	if weak := cfg.WeakJWTSecret(); weak != "" {
		log.Printf("⚠️  %s\n"+
			"    Make a new value with: openssl rand -base64 32\n"+
			"    Changing JWT_SECRET_KEY signs everyone out. Do not change ENCRYPTION_KEY.", weak)
	}

	clk, fakeClock, err := clock.FromEnv(cfg.FakeNow)
	if err != nil {
		logger.Fatal().Err(err).Msg("Invalid FAKE_NOW")
	}
	if fakeClock {
		logger.Warn().Str("fake_now", cfg.FakeNow).Msg("FAKE_NOW is set: the clock started at this time. Do not use this on a real instance")
	}

	logger.Info().Msg("Loading database and running migrations...")
	db, err := database.InitDB(cfg.DBPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize database")
	}
	// created_at and updated_at follow the same clock as everything else
	db.NowFunc = clk.Now
	// JWT expiry is checked against the clock that set it
	jwt.TimeFunc = clk.Now

	prepareEncryptionKey(db, cfg)

	// Thumbnails used to live in the database as base64: move them into files (idempotent,
	// before serving, so lists never miss a thumbnail).
	if n, err := services.ExportLegacyThumbnails(db, cfg.ProfilePhotoDir); err != nil {
		logger.Error().Err(err).Msg("Could not move contact thumbnails out of the database; old thumbnails are served from the database until this works (is PROFILE_PHOTO_DIR writable?)")
	} else if n > 0 {
		logger.Info().Int("thumbnails", n).Msg("Moved contact thumbnails from the database into files")
	}

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

	var metricsSrv *http.Server
	if cfg.MetricsEnabled {
		services.RegisterJobMetrics(metrics.Default, db)
		services.RegisterOutboxMetrics(metrics.Default, db)
		switch {
		case cfg.MetricsAddr != "":
			metricsSrv = metrics.NewServer(cfg.MetricsAddr, metrics.Default, cfg.MetricsToken)
		case cfg.MetricsToken == "":
			logger.Warn().Msg("METRICS_ENABLED without METRICS_TOKEN or METRICS_ADDR: /metrics is open on the main port. " +
				"Set a token, a separate listener, or block /metrics at your reverse proxy (see docs/deployment.md)")
		}
	}

	logger.Info().Msg("Running scheduler...")
	// Schedule the reminder task daily
	if !cfg.EmailEnabled() {
		logger.Warn().Msg("No emails will be sent: neither Resend nor SMTP is configured")
	}
	s := gocron.NewScheduler(cfg.GetReminderLocation())
	task := func() {
		// Use rate-limited version to prevent duplicate emails during rapid restarts
		if err := services.SendRemindersWithRateLimit(db, *cfg, clk); err != nil {
			logger.Error().Err(err).Msg("Error sending reminders")
		}
	}
	schedule(s.Every(1).Day().At(cfg.ReminderTime).Do(task))
	go task() // Run initially once on startup (rate-limited to prevent duplicates)
	// The delivery outbox (webhook and email retries, and first tries a restart interrupted).
	// Singleton: a run that outlasts the interval must not overlap with the next one.
	schedule(s.Every(1).Minute().SingletonMode().Do(func() {
		services.ProcessWebhookOutbox(db, *cfg, clk)
		services.ProcessEmailOutbox(db, *cfg, clk)
	}))
	schedule(s.Every(1).Hour().Do(func() {
		if fakeClock {
			// Purges delete for good by age: a clock set into the future would erase real data
			logger.Warn().Msg("FAKE_NOW is set: skipping the outbox purge jobs")
			return
		}
		now := clk.Now()
		if n, err := services.PurgeEmailOutbox(db, now); err != nil {
			logger.Error().Err(err).Msg("Error purging the email outbox")
		} else if n > 0 {
			logger.Info().Int64("emails", n).Msg("Purged old emails from the outbox")
		}
		if n, err := services.PurgeWebhookDeliveries(db, now); err != nil {
			logger.Error().Err(err).Msg("Error purging webhook deliveries")
		} else if n > 0 {
			logger.Info().Int64("deliveries", n).Msg("Purged old webhook deliveries")
		}
	}))
	schedule(s.Every(10).Minutes().Do(controllers.CleanupImportSessions))
	// Sync calendar subscriptions regularly (rate-limited via job lock).
	// Interval jobs also run once when the scheduler starts, so unlike the daily
	// reminder task above they need no separate startup call.
	calendarSyncTask := func() {
		services.SyncCalendarsWithRateLimit(db, *cfg, clk)
	}
	schedule(s.Every(cfg.CalDAVSyncIntervalHours).Hours().Do(calendarSyncTask))
	// Sync CardDAV contact connections regularly (rate-limited via job lock)
	contactSyncTask := func() {
		services.SyncContactsWithRateLimit(db, *cfg, clk)
	}
	schedule(s.Every(cfg.CardDAVSyncIntervalHours).Hours().Do(contactSyncTask))
	go s.StartBlocking()

	r := gin.Default()
	if cfg.MetricsEnabled {
		// Early, so rejected requests are counted too
		r.Use(metrics.DefaultHTTP().Middleware())
	}

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

	// Take the time once per request; handlers read it with middleware.RequestNow
	controllers.SetImportClock(clk)
	r.Use(middleware.ClockMiddleware(clk))

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

	if metricsSrv != nil {
		// Bind before serving: a typo or a busy port must stop the start, not leave the
		// operator with metrics that silently never appear.
		ln, err := net.Listen("tcp", metricsSrv.Addr)
		if err != nil {
			logger.Fatal().Err(err).Str("addr", metricsSrv.Addr).Msg("Failed to listen for metrics (METRICS_ADDR)")
		}
		go func() {
			logger.Info().Str("addr", ln.Addr().String()).Msg("Serving metrics on a separate listener")
			if err := metricsSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
				logger.Error().Err(err).Msg("Metrics listener failed")
			}
		}()
	}

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

	if metricsSrv != nil {
		metricsCtx, metricsCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer metricsCancel()
		if err := metricsSrv.Shutdown(metricsCtx); err != nil {
			logger.Error().Err(err).Msg("Metrics listener forced to shutdown")
		}
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
