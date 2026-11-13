package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"meerkat/config"
	"meerkat/i18n"
	"meerkat/logger"
	"time"

	"gorm.io/gorm"
)

const (
	passwordResetTokenBytes = 32
	passwordResetTTL        = time.Hour
)

// GeneratePasswordResetToken creates a secure token and its hashed representation.
func GeneratePasswordResetToken() (string, string, error) {
	raw := make([]byte, passwordResetTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := HashPasswordResetToken(token)
	return token, hash, nil
}

// HashPasswordResetToken hashes a reset token for database storage.
func HashPasswordResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// PasswordResetExpiry returns when a reset token should expire.
func PasswordResetExpiry(now time.Time) time.Time {
	return now.Add(passwordResetTTL)
}

// SendPasswordResetEmail queues a reset email (sent in the background, retried by the
// outbox until the link expires) when an email channel is configured.
// The lang parameter specifies the user's preferred language for the email content.
func SendPasswordResetEmail(db *gorm.DB, userID uint, email, token, lang string, cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}

	if !cfg.EmailEnabled() {
		logger.Warn().Str("email", email).Msg("No email channel configured; password reset email not sent")
		return nil
	}

	// Default to English if language not set
	if lang == "" {
		lang = i18n.DefaultLanguage
	}

	htmlBody, err := renderPasswordResetEmail(PasswordResetEmailData{
		Intro:       i18n.T(lang, "email.passwordReset.intro"),
		Instruction: i18n.T(lang, "email.passwordReset.instruction"),
		Token:       token,
		Ignore:      i18n.T(lang, "email.passwordReset.ignore"),
		Footer:      i18n.T(lang, "email.footer"),
	})
	if err != nil {
		return fmt.Errorf("failed to render password reset email: %w", err)
	}

	// Retries stop when the link is dead anyway.
	if err := QueueEmailUntil(db, *cfg, &userID, EmailMessage{
		To:      email,
		Subject: i18n.T(lang, "email.passwordReset.subject"),
		HTML:    htmlBody,
	}, PasswordResetExpiry(queueNow())); err != nil {
		return err
	}

	logger.Info().Str("email", email).Str("language", lang).Msg("Password reset email queued")
	return nil
}
