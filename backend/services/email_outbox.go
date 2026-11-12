package services

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"meerkat/config"
	"meerkat/logger"
	"meerkat/metrics"
	"meerkat/models"

	"gorm.io/gorm"
)

// emailSem limits concurrent sends from the outbox job.
var emailSem = make(chan struct{}, 5)

var (
	// emailSender is the real send; a seam for tests.
	emailSender = SendEmail
	// startEmailDelivery runs the immediate try in the background; a seam for tests.
	startEmailDelivery = func(f func()) { go f() }
)

// QueueEmail stores msg in the email outbox and tries to send it right away in the
// background. Failures are retried with backoff by the outbox job (see ProcessEmailOutbox),
// also after a restart. Like SendEmail it does nothing when no email channel is configured
// or the recipient is empty. userID (optional) ties the row to its owner, so deleting the
// user removes it. The body is cleared once the row is delivered or dead.
func QueueEmail(db *gorm.DB, cfg config.Config, userID *uint, msg EmailMessage) error {
	return QueueEmailUntil(db, cfg, userID, msg, time.Time{})
}

// StubEmailDelivery replaces the background immediate try (tests of other packages); the
// returned function restores it.
func StubEmailDelivery(f func(func())) (restore func()) {
	old := startEmailDelivery
	startEmailDelivery = f
	return func() { startEmailDelivery = old }
}

// QueueEmailUntil is QueueEmail for a message that is useless after expires (a password
// reset link): it is not sent later and goes dead with its body cleared. A zero expires
// means it never expires.
func QueueEmailUntil(db *gorm.DB, cfg config.Config, userID *uint, msg EmailMessage, expires time.Time) error {
	if msg.To == "" {
		logger.Warn().Msg("Skipping email because recipient address is empty")
		return nil
	}
	if strings.ContainsAny(msg.To, "\r\n") || strings.ContainsAny(msg.Subject, "\r\n") {
		return fmt.Errorf("refusing to queue email: line break in recipient or subject")
	}
	if !cfg.EmailEnabled() {
		logger.Warn().Str("to", msg.To).Msg("No email channel configured; email not queued")
		return nil
	}
	e := models.EmailOutbox{
		UserID: userID, ToAddr: msg.To, Subject: msg.Subject, HTML: msg.HTML,
		Status: models.OutboxPending, NextAttemptAt: time.Now(),
	}
	if !expires.IsZero() {
		e.ExpiresAt = &expires
	}
	if err := db.Create(&e).Error; err != nil {
		return fmt.Errorf("queue email: %w", err)
	}
	startEmailDelivery(func() {
		emailSem <- struct{}{}
		defer func() { <-emailSem }()
		if claimEmail(db, e) {
			sendQueuedEmail(db, cfg, e)
		}
	})
	return nil
}

// claimEmail takes a pending email for this worker unless another holds a fresh claim, the
// row is not due, or it changed since e was loaded (see claimWebhookDelivery).
func claimEmail(db *gorm.DB, e models.EmailOutbox) bool {
	now := time.Now()
	res := db.Model(&models.EmailOutbox{}).
		Where("id = ? AND status = ? AND attempts = ? AND next_attempt_at <= ? AND (locked_at IS NULL OR locked_at < ?)",
			e.ID, models.OutboxPending, e.Attempts, now, now.Add(-outboxLockTimeout)).
		Update("locked_at", now)
	return res.Error == nil && res.RowsAffected == 1
}

// sendQueuedEmail makes one try for a claimed row and records the outcome on it. A row that
// is delivered or dead keeps no body (it may hold a reset token).
func sendQueuedEmail(db *gorm.DB, cfg config.Config, e models.EmailOutbox) {
	if e.ExpiresAt != nil && !time.Now().Before(*e.ExpiresAt) {
		expireEmail(db, e)
		return
	}
	err := emailSender(cfg, EmailMessage{To: e.ToAddr, Subject: e.Subject, HTML: e.HTML})
	attempt := e.Attempts + 1
	now := time.Now()
	upd := map[string]any{"attempts": attempt, "locked_at": nil}
	switch {
	case err == nil:
		upd["status"], upd["sent_at"], upd["last_error"], upd["html"] = models.OutboxDelivered, now, "", ""
	case attempt >= maxOutboxAttempts:
		upd["status"], upd["last_error"], upd["html"] = models.OutboxDead, err.Error(), ""
		metrics.OutboxDead("email")
		logger.Error().Err(err).Uint("email_id", e.ID).Int("attempts", attempt).Msg("Email gave up after the last try")
	case e.ExpiresAt != nil && !now.Add(outboxBackoff[attempt-1]).Before(*e.ExpiresAt):
		upd["status"], upd["last_error"], upd["html"] = models.OutboxDead, "expired; last error: "+err.Error(), ""
		metrics.OutboxDead("email")
		logger.Warn().Err(err).Uint("email_id", e.ID).Int("attempt", attempt).Msg("Email failed and would expire before the next try")
	default:
		upd["last_error"], upd["next_attempt_at"] = err.Error(), now.Add(outboxBackoff[attempt-1])
		logger.Warn().Err(err).Uint("email_id", e.ID).Int("attempt", attempt).Msg("Email failed, will retry")
	}
	if dbErr := db.Model(&models.EmailOutbox{}).Where("id = ?", e.ID).Updates(upd).Error; dbErr != nil {
		logger.Error().Err(dbErr).Uint("email_id", e.ID).Msg("Failed to save email outbox state")
	}
}

// expireEmail gives up on a claimed email whose useful life is over, without sending it.
func expireEmail(db *gorm.DB, e models.EmailOutbox) {
	metrics.OutboxDead("email")
	err := db.Model(&models.EmailOutbox{}).Where("id = ?", e.ID).Updates(map[string]any{
		"status": models.OutboxDead, "last_error": "expired before it could be sent", "html": "", "locked_at": nil,
	}).Error
	if err != nil {
		logger.Error().Err(err).Uint("email_id", e.ID).Msg("Failed to expire email")
	}
}

// ProcessEmailOutbox sends every pending email that is due: first tries that a restart
// interrupted and retries after a failure. Nothing happens (and no try is counted) while
// no email channel is configured.
func ProcessEmailOutbox(db *gorm.DB, cfg config.Config) {
	expireEmails(db)
	if !cfg.EmailEnabled() {
		return
	}
	var due []models.EmailOutbox
	now := time.Now()
	if err := db.Where("status = ? AND next_attempt_at <= ? AND (locked_at IS NULL OR locked_at < ?)",
		models.OutboxPending, now, now.Add(-outboxLockTimeout)).
		Order("next_attempt_at").Limit(100).Find(&due).Error; err != nil {
		logger.Error().Err(err).Msg("Failed to load the email outbox")
		return
	}
	var wg sync.WaitGroup
	for _, e := range due {
		wg.Add(1)
		go func() {
			defer wg.Done()
			emailSem <- struct{}{}
			defer func() { <-emailSem }()
			// Claim inside the slot: a claim made up front would age while queued.
			if claimEmail(db, e) {
				sendQueuedEmail(db, cfg, e)
			}
		}()
	}
	wg.Wait()
}

// expireEmails gives up on pending emails past their expiry, whether or not a channel is
// configured, so their bodies do not linger.
func expireEmails(db *gorm.DB) {
	now := time.Now()
	res := db.Model(&models.EmailOutbox{}).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at <= ? AND (locked_at IS NULL OR locked_at < ?)",
			models.OutboxPending, now, now.Add(-outboxLockTimeout)).
		Updates(map[string]any{"status": models.OutboxDead, "last_error": "expired before it could be sent", "html": "", "locked_at": nil})
	if res.Error != nil {
		logger.Error().Err(res.Error).Msg("Failed to expire emails in the outbox")
		return
	}
	for i := int64(0); i < res.RowsAffected; i++ {
		metrics.OutboxDead("email")
	}
}

// PurgeEmailOutbox deletes sent emails after a week and dead ones after 30 days.
func PurgeEmailOutbox(db *gorm.DB, now time.Time) (int64, error) {
	res := db.Where("(status = ? AND updated_at < ?) OR (status = ? AND updated_at < ?)",
		models.OutboxDelivered, now.Add(-sentOutboxRetention), models.OutboxDead, now.Add(-deadOutboxRetention)).
		Delete(&models.EmailOutbox{})
	return res.RowsAffected, res.Error
}
