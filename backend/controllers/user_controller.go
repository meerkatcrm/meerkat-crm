package controllers

import (
	"errors"
	"meerkat/clock"
	"net/http"
	"slices"
	"strings"
	"time"

	"meerkat/config"
	apperrors "meerkat/errors"
	"meerkat/logger"
	"meerkat/middleware"
	"meerkat/models"
	"meerkat/services"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// passwordResetCooldown limits how often one account can be sent a reset email.
const passwordResetCooldown = time.Minute

// passwordResetSlots limits concurrent background reset work, each of which may wait on the mail provider.
var passwordResetSlots = make(chan struct{}, 10)

func RegisterUser(cfg *config.Config) gin.HandlerFunc {
	return func(context *gin.Context) {
		if !cfg.RegistrationEnabled() {
			context.JSON(http.StatusForbidden, gin.H{"error": gin.H{
				"code":    "registration_disabled",
				"message": "Registration is disabled.",
			}})
			return
		}

		// Get validated registration input from middleware
		// Uses UserRegistrationInput DTO which intentionally excludes IsAdmin to prevent mass assignment
		input, err := middleware.GetValidated[models.UserRegistrationInput](context)
		if err != nil {
			apperrors.AbortWithError(context, err)
			return
		}

		hashedPassword, hashErr := services.HashPassword(input.Password)
		if hashErr != nil {
			if errors.Is(hashErr, services.ErrPasswordTooLong) {
				apperrors.AbortWithError(context, apperrors.ErrValidation(hashErr.Error()))
			} else {
				apperrors.AbortWithError(context, apperrors.ErrInternal("Could not hash password").WithError(hashErr))
			}
			return
		}

		db := context.MustGet("db").(*gorm.DB)

		// Grant admin to the first registered user
		var userCount int64
		db.Model(&models.User{}).Count(&userCount)

		user := models.User{
			Username: strings.ToLower(input.Username),
			Email:    strings.ToLower(input.Email),
			Password: hashedPassword,
			Language: input.Language,
			IsAdmin:  userCount == 0,
		}

		if err := db.Create(&user).Error; err != nil {
			apperrors.AbortWithError(context, apperrors.ErrAlreadyExists("User").WithDetails("email", user.Email))
			return
		}

		context.JSON(http.StatusCreated, gin.H{"message": "User registered successfully"})
	}
}

// accountLockedError is the 429 for a login that is locked out after repeated failures. The
// rate limiter counts the lockout on the wall clock, so its end is on the wall clock too.
func accountLockedError(retryAfterSecs int) *apperrors.AppError {
	return apperrors.NewError(apperrors.ErrCodeRateLimitExceeded, "Too many failed login attempts. Please try again later.", http.StatusTooManyRequests).
		WithDetails("retry_after", retryAfterSecs).
		WithDetails("retry_after_at", clock.Real().Now().Add(time.Duration(retryAfterSecs)*time.Second).Format(time.RFC3339))
}

// LoginInput represents the DTO for login requests
type LoginInput struct {
	Identifier string `json:"identifier"` // Can be username or email
	Email      string `json:"email"`      // Legacy field for backward compatibility
	Password   string `json:"password"`
}

// errPasswordLoginDisabled answers password sign-in and reset on an SSO-only instance.
func errPasswordLoginDisabled() *apperrors.AppError {
	return apperrors.NewError("password_login_disabled", "Password sign-in is disabled. Sign in with SSO.", http.StatusForbidden)
}

func LoginUser(context *gin.Context, cfg *config.Config) {
	if !cfg.PasswordLoginEnabled() {
		apperrors.AbortWithError(context, errPasswordLoginDisabled())
		return
	}

	var input LoginInput
	var foundUser models.User

	err := context.ShouldBindJSON(&input)
	if err != nil {
		apperrors.AbortWithError(context, apperrors.ErrInvalidInput("", err.Error()))
		return
	}

	// Support both "identifier" and legacy "email" field
	identifier := input.Identifier
	if identifier == "" {
		identifier = input.Email
	}

	if identifier == "" {
		apperrors.AbortWithError(context, apperrors.ErrMissingField("identifier"))
		return
	}

	// Normalize to lowercase for case-insensitive matching
	identifier = strings.ToLower(identifier)

	if input.Password == "" {
		apperrors.AbortWithError(context, apperrors.ErrMissingField("password"))
		return
	}

	// Check per-account rate limiting before attempting authentication
	accountLimiter := middleware.GetAccountRateLimiter()
	if isLocked, remainingSecs := accountLimiter.IsLocked(identifier); isLocked {
		apperrors.AbortWithError(context, accountLockedError(remainingSecs))
		return
	}

	db := context.MustGet("db").(*gorm.DB)

	// Check if identifier contains @ to determine if it's an email or username
	var query *gorm.DB
	if strings.Contains(identifier, "@") {
		query = db.Where("email = ?", identifier)
	} else {
		query = db.Where("username = ?", identifier)
	}

	if err := query.First(&foundUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Burn the same bcrypt cost as a real comparison to not reveal if an account exists
			models.BurnPasswordCheck(input.Password)
			// Record failed attempt even for non-existent users to prevent enumeration
			accountLimiter.RecordFailedAttempt(identifier)
			apperrors.AbortWithError(context, apperrors.ErrInvalidCredentials())
		} else {
			apperrors.AbortWithError(context, apperrors.ErrDatabase("Failed to query user").WithError(err))
		}
		return
	}

	// Compare the hashed password
	if err := foundUser.CheckPassword(input.Password); err != nil {
		// Record failed attempt for password mismatch
		isLocked, lockoutSecs := accountLimiter.RecordFailedAttempt(identifier)
		if isLocked {
			apperrors.AbortWithError(context, accountLockedError(lockoutSecs))
			return
		}
		apperrors.AbortWithError(context, apperrors.ErrInvalidCredentials())
		return
	}

	// Successful login - clear any failed attempt tracking
	accountLimiter.RecordSuccessfulLogin(identifier)

	// Create JWT token
	tokenString, err := services.GenerateToken(foundUser, cfg, middleware.RequestNow(context))
	if err != nil {
		apperrors.AbortWithError(context, apperrors.ErrInternal("Could not generate token").WithError(err))
		return
	}

	// Set httpOnly cookie with the JWT token
	maxAge := cfg.JWTExpiryHours * 3600 // Convert hours to seconds
	context.SetSameSite(http.SameSiteLaxMode)
	context.SetCookie(
		"auth_token",     // name
		tokenString,      // value
		maxAge,           // maxAge in seconds
		"/",              // path
		cfg.CookieDomain, // domain (empty = current domain)
		cfg.CookieSecure, // secure (true = HTTPS only)
		true,             // httpOnly (not accessible via JavaScript)
	)

	// Return user preferences (token is now in httpOnly cookie)
	context.JSON(http.StatusOK, gin.H{
		"language":    foundUser.Language,
		"date_format": foundUser.DateFormat,
	})
}

// LogoutUser clears the auth cookie to log out the user
func LogoutUser(context *gin.Context, cfg *config.Config) {
	context.SetSameSite(http.SameSiteLaxMode)
	context.SetCookie(
		"auth_token",     // name
		"",               // value (empty)
		-1,               // maxAge -1 = delete cookie
		"/",              // path
		cfg.CookieDomain, // domain
		cfg.CookieSecure, // secure
		true,             // httpOnly
	)
	context.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// CheckPasswordStrength evaluates password strength without registration
func CheckPasswordStrength(context *gin.Context) {
	var request struct {
		Password string `json:"password" binding:"required"`
	}

	if err := context.ShouldBindJSON(&request); err != nil {
		apperrors.AbortWithError(context, apperrors.ErrMissingField("password"))
		return
	}

	strength := middleware.EvaluatePasswordStrength(request.Password)
	context.JSON(http.StatusOK, strength)
}

// RequestPasswordReset generates a reset token and sends instructions to the user.
func RequestPasswordReset(context *gin.Context, cfg *config.Config) {
	// Check if demo mode is enabled - password changes are disabled in demo
	if cfg.DemoMode {
		apperrors.AbortWithError(context, apperrors.ErrForbidden("Password changes are disabled in demo mode"))
		return
	}
	if !cfg.PasswordLoginEnabled() {
		apperrors.AbortWithError(context, errPasswordLoginDisabled())
		return
	}

	log := logger.FromContext(context)

	input, appErr := middleware.GetValidated[models.PasswordResetRequestInput](context)
	if appErr != nil {
		apperrors.AbortWithError(context, appErr)
		return
	}

	// Normalize email to lowercase for case-insensitive matching
	email := strings.ToLower(input.Email)

	db := context.MustGet("db").(*gorm.DB)

	// Everything after this point runs in the background: known and unknown addresses must get
	// the same response in the same time, including when the lookup or the mail provider fails.
	select {
	case passwordResetSlots <- struct{}{}:
		clk := middleware.RequestClock(context)
		go func() {
			defer func() { <-passwordResetSlots }()
			processPasswordResetRequest(db, cfg, log, email, clk)
		}()
	default:
		log.Warn().Msg("Password reset request dropped: too many in progress")
	}

	context.JSON(http.StatusOK, gin.H{"message": "If an account exists, password reset instructions were sent"})
}

// processPasswordResetRequest stores a new reset token and emails it. Failures are only logged,
// since the caller has already been answered.
func processPasswordResetRequest(db *gorm.DB, cfg *config.Config, log *zerolog.Logger, email string, clk clock.Clock) {
	// This runs outside Gin's recovery middleware, so a panic would take the server down.
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("Panic while processing password reset request")
		}
	}()

	var user models.User
	if err := db.Where("email = ?", email).First(&user).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Error().Err(err).Msg("Failed to lookup user for password reset")
		}
		return
	}

	now := clk.Now()
	if user.PasswordResetRequestedAt != nil && now.Sub(*user.PasswordResetRequestedAt) < passwordResetCooldown {
		log.Info().Uint("user_id", user.ID).Msg("Password reset request ignored during cooldown")
		return
	}

	token, hash, err := services.GeneratePasswordResetToken()
	if err != nil {
		log.Error().Err(err).Msg("Failed to generate password reset token")
		return
	}

	if err := db.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"password_reset_token_hash":   hash,
		"password_reset_expires_at":   services.PasswordResetExpiry(now),
		"password_reset_requested_at": now,
	}).Error; err != nil {
		log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to persist password reset token")
		return
	}

	if err := services.SendPasswordResetEmail(db, user.ID, user.Email, token, user.Language, cfg); err != nil {
		log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to queue password reset email")
		// Nothing was delivered, so a retry should not have to wait out the cooldown
		if err := db.Model(&models.User{}).Where("id = ?", user.ID).Update("password_reset_requested_at", nil).Error; err != nil {
			log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to clear password reset cooldown")
		}
	}
}

// ConfirmPasswordReset validates the token and updates the password.
func ConfirmPasswordReset(context *gin.Context) {
	// Check if demo mode is enabled - password changes are disabled in demo
	cfg := currentConfig(context)
	if cfg.DemoMode {
		apperrors.AbortWithError(context, apperrors.ErrForbidden("Password changes are disabled in demo mode"))
		return
	}
	if !cfg.PasswordLoginEnabled() {
		apperrors.AbortWithError(context, errPasswordLoginDisabled())
		return
	}

	log := logger.FromContext(context)

	input, appErr := middleware.GetValidated[models.PasswordResetConfirmInput](context)
	if appErr != nil {
		apperrors.AbortWithError(context, appErr)
		return
	}

	db := context.MustGet("db").(*gorm.DB)

	tokenHash := services.HashPasswordResetToken(input.Token)

	var user models.User
	if err := db.Where("password_reset_token_hash = ?", tokenHash).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(context, apperrors.ErrInvalidInput("token", "Password reset token is invalid or expired"))
			return
		}

		log.Error().Err(err).Msg("Failed to lookup password reset token")
		apperrors.AbortWithError(context, apperrors.ErrDatabase("query user").WithError(err))
		return
	}

	if user.PasswordResetExpiresAt == nil || middleware.RequestNow(context).After(*user.PasswordResetExpiresAt) {
		user.PasswordResetTokenHash = nil
		user.PasswordResetExpiresAt = nil
		user.PasswordResetRequestedAt = nil
		if err := db.Save(&user).Error; err != nil {
			log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to clear expired reset token")
		}
		apperrors.AbortWithError(context, apperrors.ErrInvalidInput("token", "Password reset token is invalid or expired"))
		return
	}

	hashedPassword, err := services.HashPassword(input.Password)
	if err != nil {
		if errors.Is(err, services.ErrPasswordTooLong) {
			apperrors.AbortWithError(context, apperrors.ErrValidation(err.Error()))
		} else {
			log.Error().Err(err).Msg("Failed to hash password during reset")
			apperrors.AbortWithError(context, apperrors.ErrInternal("Could not hash password").WithError(err))
		}
		return
	}

	user.Password = hashedPassword
	passwordChangedAt := middleware.RequestNow(context)
	user.PasswordChangedAt = &passwordChangedAt
	user.PasswordResetTokenHash = nil
	user.PasswordResetExpiresAt = nil
	user.PasswordResetRequestedAt = nil

	if err := db.Save(&user).Error; err != nil {
		log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to persist password reset")
		apperrors.AbortWithError(context, apperrors.ErrDatabase("update user").WithError(err))
		return
	}

	context.JSON(http.StatusOK, gin.H{"message": "Password reset successful"})
}

// updateOwnPreference writes one column of the caller's user row. The caller is
// identified by id: the username in a session token goes stale when an admin
// renames the account, and API tokens carry none. A single-column write also
// never rewrites the password hash or reset token with a stale copy.
func updateOwnPreference(c *gin.Context, column, value string) bool {
	userID, ok := currentUserID(c)
	if !ok {
		return false
	}
	db := c.MustGet("db").(*gorm.DB)
	result := db.Model(&models.User{}).Where("id = ?", userID).Update(column, value)
	if result.Error != nil {
		logger.FromContext(c).Error().Err(result.Error).Uint("user_id", userID).Str("column", column).Msg("Failed to update user preference")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update user").WithError(result.Error))
		return false
	}
	if result.RowsAffected == 0 {
		apperrors.AbortWithError(c, apperrors.ErrUnauthorized("Authentication required"))
		return false
	}
	return true
}

// UpdateLanguage updates the authenticated user's language preference
func UpdateLanguage(context *gin.Context) {
	input, appErr := middleware.GetValidated[models.UpdateLanguageInput](context)
	if appErr != nil {
		apperrors.AbortWithError(context, appErr)
		return
	}

	if !updateOwnPreference(context, "language", input.Language) {
		return
	}

	context.JSON(http.StatusOK, gin.H{"message": "Language updated successfully", "language": input.Language})
}

// UpdateDateFormat updates the authenticated user's date format preference
func UpdateDateFormat(context *gin.Context) {
	input, appErr := middleware.GetValidated[models.UpdateDateFormatInput](context)
	if appErr != nil {
		apperrors.AbortWithError(context, appErr)
		return
	}

	if !updateOwnPreference(context, "date_format", input.DateFormat) {
		return
	}

	context.JSON(http.StatusOK, gin.H{"message": "Date format updated successfully", "date_format": input.DateFormat})
}

// UpdateCustomFieldNames updates the authenticated user's custom field definitions
func UpdateCustomFieldNames(c *gin.Context) {
	log := logger.FromContext(c)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Get validated input
	input, err := middleware.GetValidated[models.CustomFieldNamesInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		log.Error().Err(err).Uint("user_id", userID).Msg("Failed to lookup user for custom field names update")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}

	user.CustomFieldNames = input.Names
	if err := db.Model(&user).Select("CustomFieldNames").Updates(&user).Error; err != nil {
		log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to update user custom field names")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update user").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":            "Custom field names updated successfully",
		"custom_field_names": user.CustomFieldNames,
	})
}

// GetCustomFieldNames returns the authenticated user's custom field definitions
func GetCustomFieldNames(c *gin.Context) {
	log := logger.FromContext(c)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		log.Error().Err(err).Uint("user_id", userID).Msg("Failed to lookup user for custom field names")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}

	// Return empty array instead of null if not set
	names := user.CustomFieldNames
	if names == nil {
		names = []string{}
	}

	c.JSON(http.StatusOK, gin.H{
		"custom_field_names": names,
	})
}

// UpdateEnabledContactFields updates which extended contact fields are visible in the UI
func UpdateEnabledContactFields(c *gin.Context) {
	log := logger.FromContext(c)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	input, err := middleware.GetValidated[models.EnabledContactFieldsInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		log.Error().Err(err).Uint("user_id", userID).Msg("Failed to lookup user for enabled contact fields update")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}

	user.EnabledContactFields = input.Fields
	// Scope the write to just this column so we never rewrite the password hash / OIDC fields.
	if err := db.Model(&user).Select("EnabledContactFields").Updates(&user).Error; err != nil {
		log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to update user enabled contact fields")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update user").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":                "Enabled contact fields updated successfully",
		"enabled_contact_fields": user.EnabledContactFields,
	})
}

// GetEnabledContactFields returns which extended contact fields are visible in the UI.
// Returns null when the user has never configured it, so the client applies its defaults.
func GetEnabledContactFields(c *gin.Context) {
	log := logger.FromContext(c)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		log.Error().Err(err).Uint("user_id", userID).Msg("Failed to lookup user for enabled contact fields")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled_contact_fields": user.EnabledContactFields,
	})
}

// UpdateContactFieldDisplay sets whether empty fields are hidden on the contact detail page
func UpdateContactFieldDisplay(c *gin.Context) {
	log := logger.FromContext(c)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	input, err := middleware.GetValidated[models.ContactFieldDisplayInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	// Single-column write so we never rewrite the password hash / OIDC fields.
	hideEmpty := *input.HideEmptyContactFields
	result := db.Model(&models.User{}).Where("id = ?", userID).Update("hide_empty_contact_fields", hideEmpty)
	if result.Error != nil {
		log.Error().Err(result.Error).Uint("user_id", userID).Msg("Failed to update user contact field display")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update user").WithError(result.Error))
		return
	}
	if result.RowsAffected == 0 {
		apperrors.AbortWithError(c, apperrors.ErrNotFound("User"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":                   "Contact field display updated successfully",
		"hide_empty_contact_fields": hideEmpty,
	})
}

// GetCardDAVCircleFilter returns which circles the built-in CardDAV server shows the user's devices
func GetCardDAVCircleFilter(c *gin.Context) {
	log := logger.FromContext(c)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		log.Error().Err(err).Uint("user_id", userID).Msg("Failed to lookup user for CardDAV circle filter")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("query user").WithError(err))
		return
	}

	mode := user.CardDAVCircleMode
	if mode == "" {
		mode = models.CardDAVCircleModeAll
	}
	circles := user.CardDAVCircles
	if circles == nil {
		circles = []string{}
	}

	c.JSON(http.StatusOK, gin.H{"mode": mode, "circles": circles})
}

// UpdateCardDAVCircleFilter sets which circles the built-in CardDAV server shows the user's devices
func UpdateCardDAVCircleFilter(c *gin.Context) {
	log := logger.FromContext(c)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	input, err := middleware.GetValidated[models.CardDAVCircleFilterInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	// Circles match case-insensitively, so "Family" and "family" are one entry.
	circles := []string{}
	for _, circle := range input.Circles {
		circle = strings.TrimSpace(circle)
		if circle != "" && !slices.ContainsFunc(circles, func(c string) bool { return strings.EqualFold(c, circle) }) {
			circles = append(circles, circle)
		}
	}
	// An empty include list would empty every device's address book; an empty
	// exclude list is just "all" under another name. Both are almost certainly mistakes.
	if input.Mode != models.CardDAVCircleModeAll && len(circles) == 0 {
		apperrors.AbortWithError(c, apperrors.ErrInvalidInput("circles", "Select at least one circle"))
		return
	}
	// "all" ignores the list, so don't store one that would look meaningful.
	if input.Mode == models.CardDAVCircleModeAll {
		circles = []string{}
	}

	db := c.MustGet("db").(*gorm.DB)

	user := models.User{CardDAVCircleMode: input.Mode, CardDAVCircles: circles}
	user.ID = userID
	result := db.Model(&user).Select("CardDAVCircleMode", "CardDAVCircles").Updates(&user)
	if result.Error != nil {
		log.Error().Err(result.Error).Uint("user_id", userID).Msg("Failed to update user CardDAV circle filter")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("update user").WithError(result.Error))
		return
	}
	if result.RowsAffected == 0 {
		apperrors.AbortWithError(c, apperrors.ErrUnauthorized("Authentication required"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"mode": user.CardDAVCircleMode, "circles": user.CardDAVCircles})
}

// ChangePassword lets authenticated users rotate their password.
func ChangePassword(context *gin.Context) {
	// Check if demo mode is enabled - password changes are disabled in demo
	if currentConfig(context).DemoMode {
		apperrors.AbortWithError(context, apperrors.ErrForbidden("Password changes are disabled in demo mode"))
		return
	}

	log := logger.FromContext(context)

	input, appErr := middleware.GetValidated[models.ChangePasswordInput](context)
	if appErr != nil {
		apperrors.AbortWithError(context, appErr)
		return
	}

	userID, ok := currentUserID(context)
	if !ok {
		return
	}

	db := context.MustGet("db").(*gorm.DB)

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(context, apperrors.ErrUnauthorized("Authentication required"))
			return
		}

		log.Error().Err(err).Msg("Failed to lookup user for password change")
		apperrors.AbortWithError(context, apperrors.ErrDatabase("query user").WithError(err))
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.CurrentPassword)); err != nil {
		apperrors.AbortWithError(context, apperrors.ErrInvalidInput("current_password", "Current password is incorrect"))
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.NewPassword)); err == nil {
		apperrors.AbortWithError(context, apperrors.ErrInvalidInput("new_password", "New password must differ from current password"))
		return
	}

	hashedPassword, err := services.HashPassword(input.NewPassword)
	if err != nil {
		if errors.Is(err, services.ErrPasswordTooLong) {
			apperrors.AbortWithError(context, apperrors.ErrValidation(err.Error()))
		} else {
			log.Error().Err(err).Msg("Failed to hash password during change")
			apperrors.AbortWithError(context, apperrors.ErrInternal("Could not hash password").WithError(err))
		}
		return
	}

	user.Password = hashedPassword
	passwordChangedAt := middleware.RequestNow(context)
	user.PasswordChangedAt = &passwordChangedAt
	user.PasswordResetTokenHash = nil
	user.PasswordResetExpiresAt = nil
	user.PasswordResetRequestedAt = nil

	if err := db.Save(&user).Error; err != nil {
		log.Error().Err(err).Uint("user_id", user.ID).Msg("Failed to persist password change")
		apperrors.AbortWithError(context, apperrors.ErrDatabase("update user").WithError(err))
		return
	}

	context.JSON(http.StatusOK, gin.H{"message": "Password updated successfully"})
}
