package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"meerkat/clock"
	"meerkat/config"
	"meerkat/logger"
	"meerkat/metrics"
	"meerkat/models"
	"meerkat/netguard"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// An outbox row (webhook delivery or email) is tried up to maxOutboxAttempts times, waiting
// outboxBackoff[n-1] after the n-th failure; then it is dead (kept for inspection).
// outboxLockTimeout is how long a claim holds: a worker that crashed mid-send (restart)
// loses it, and the row is tried again (at-least-once: the receiver may see a duplicate).
// It must stay well above the slowest send (HTTP, SMTP and Resend timeouts), or a live
// send would lose its claim.
const (
	maxOutboxAttempts      = 5
	outboxLockTimeout      = 5 * time.Minute
	webhookDeliveryTimeout = 15 * time.Second

	// Finished rows are only kept to see that they went out; dead ones stay longer so the
	// owner can look at them. Each row stores the full payload.
	sentOutboxRetention = 7 * 24 * time.Hour
	deadOutboxRetention = 30 * 24 * time.Hour

	// Stored on the delivery and shown to its owner. Deliberately generic: the raw transport
	// error says whether a port is closed, filtered or unresolvable, which would turn the
	// test endpoint into a port scanner for any signed-in user. The detail goes to the log.
	webhookErrInvalidURL     = "webhook URL is invalid"
	webhookErrDeliveryFailed = "could not connect to the webhook URL"
	webhookErrPrivateBlocked = "webhook URL resolves to a private or loopback address"
	webhookErrGone           = "webhook deleted, disabled or its owner removed"
)

var outboxBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}

var (
	errWebhookUnreachable    = errors.New("webhook host unreachable")
	errWebhookPrivateAddress = errors.New("webhook host resolves to a private address")

	deliveryClient = &http.Client{Timeout: webhookDeliveryTimeout}
	// guardedDeliveryClient checks the address at dial time, so the check also covers
	// redirects and cannot be bypassed by a DNS answer that changes after validation.
	// netguard.NewTransport ignores HTTP(S)_PROXY: a proxy would be dialled instead of the target.
	guardedDeliveryClient = &http.Client{
		Timeout:   webhookDeliveryTimeout,
		Transport: netguard.NewTransport(errWebhookUnreachable, errWebhookPrivateAddress),
	}
	// semaphore limits concurrent outbound webhook HTTP calls
	deliverySem = make(chan struct{}, 10)

	// startDelivery runs the immediate try of a queued delivery in the background. A seam for tests.
	startDelivery = func(f func()) { go f() }
)

type webhookPayload struct {
	ID        string      `json:"id"`
	Event     string      `json:"event"`
	Timestamp string      `json:"timestamp"`
	Data      interface{} `json:"data"`
}

func buildPayloadBody(eventType string, data interface{}, now time.Time) ([]byte, error) {
	payload := webhookPayload{
		ID:        uuid.New().String(),
		Event:     eventType,
		Timestamp: now.UTC().Format(time.RFC3339),
		Data:      data,
	}
	return json.Marshal(payload)
}

// TriggerWebhooks queues a delivery for every active subscription of the user that matches
// eventType and tries each one at once in the background. A delivery that fails, or that a
// restart interrupts, is retried by the outbox job (ProcessWebhookOutbox).
func TriggerWebhooks(db *gorm.DB, cfg config.Config, userID uint, eventType string, data interface{}, clk clock.Clock) {
	var webhooks []models.Webhook
	if err := db.Where("user_id = ? AND is_active = ? AND deleted_at IS NULL", userID, true).Find(&webhooks).Error; err != nil {
		logger.Error().Err(err).Uint("user_id", userID).Msg("Failed to load webhooks for triggering")
		return
	}

	body, err := buildPayloadBody(eventType, data, clk.Now())
	if err != nil {
		logger.Error().Err(err).Str("event", eventType).Msg("Failed to build webhook payload")
		return
	}

	for _, wh := range webhooks {
		subscribed := false
		for _, e := range wh.Events {
			if e == eventType {
				subscribed = true
				break
			}
		}
		if !subscribed {
			continue
		}

		d, err := enqueueWebhookDelivery(db, wh.ID, eventType, body, clk.Now())
		if err != nil {
			logger.Error().Err(err).Uint("webhook_id", wh.ID).Msg("Failed to queue webhook delivery")
			continue
		}
		// Try at once; if this process dies first, the outbox job picks the row up.
		wh, d := wh, d
		startDelivery(func() {
			deliverySem <- struct{}{}
			defer func() { <-deliverySem }()
			// Claim only once a slot is free: a claim held while waiting would age.
			if claimWebhookDelivery(db, d, clk.Now()) {
				deliverWebhook(db, cfg, wh, d, clk)
			}
		})
	}
}

// TestWebhookDelivery delivers a test payload directly to the given webhook, ignoring event subscriptions.
func TestWebhookDelivery(db *gorm.DB, cfg config.Config, wh models.Webhook, clk clock.Clock) models.WebhookDelivery {
	testData := map[string]interface{}{
		"message": "This is a test webhook delivery from Meerkat CRM.",
	}
	body, err := buildPayloadBody("test", testData, clk.Now())
	if err != nil {
		errStr := err.Error()
		d := models.WebhookDelivery{WebhookID: wh.ID, EventType: "test", Payload: "{}", Error: &errStr, Attempts: 1, Status: models.OutboxDead}
		db.Create(&d)
		return d
	}
	d, err := enqueueWebhookDelivery(db, wh.ID, "test", body, clk.Now())
	if err != nil {
		logger.Error().Err(err).Uint("webhook_id", wh.ID).Msg("Failed to queue test webhook delivery")
		errStr := webhookErrDeliveryFailed
		return models.WebhookDelivery{WebhookID: wh.ID, EventType: "test", Error: &errStr, Status: models.OutboxDead}
	}
	if !claimWebhookDelivery(db, d, clk.Now()) {
		return d
	}
	return deliverWebhook(db, cfg, wh, d, clk)
}

// enqueueWebhookDelivery writes the delivery as pending, due now. This row is the queue:
// it is written before any HTTP call, so a restart does not lose the event.
func enqueueWebhookDelivery(db *gorm.DB, webhookID uint, eventType string, body []byte, now time.Time) (models.WebhookDelivery, error) {
	d := models.WebhookDelivery{
		WebhookID:   webhookID,
		EventType:   eventType,
		Payload:     string(body),
		Status:      models.OutboxPending,
		NextRetryAt: &now,
	}
	err := db.Create(&d).Error
	return d, err
}

// claimWebhookDelivery takes a pending delivery for this worker. It fails if another worker
// holds a fresh claim, if the row is not due, or if it changed since d was loaded (another
// worker tried it and released it with a new retry time: d is a stale copy and sending it
// again would duplicate that try). The checks are part of the same UPDATE as the claim, so
// two workers cannot both win.
func claimWebhookDelivery(db *gorm.DB, d models.WebhookDelivery, now time.Time) bool {
	res := db.Model(&models.WebhookDelivery{}).
		Where("id = ? AND status = ? AND attempts = ? AND next_retry_at <= ? AND (locked_at IS NULL OR locked_at < ?)",
			d.ID, models.OutboxPending, d.Attempts, now, now.Add(-outboxLockTimeout)).
		Update("locked_at", now)
	return res.Error == nil && res.RowsAffected == 1
}

// deliverWebhook makes one try for a claimed delivery and records the outcome on the same
// row: delivered, pending with the next try time (backoff), or dead.
func deliverWebhook(db *gorm.DB, cfg config.Config, wh models.Webhook, d models.WebhookDelivery, clk clock.Clock) models.WebhookDelivery {
	statusCode, errStr, permanent := sendWebhook(cfg, wh, d.EventType, []byte(d.Payload))
	// Read after the HTTP call, which can take long: the next try is scheduled from when this one ended.
	return finishWebhookDelivery(db, d, d.Attempts+1, statusCode, errStr, permanent, clk.Now())
}

// sendWebhook performs the HTTP call. permanent marks a refusal that retrying cannot fix.
func sendWebhook(cfg config.Config, wh models.Webhook, eventType string, body []byte) (statusCode *int, errMsg *string, permanent bool) {
	fail := func(msg string, code *int, perm bool) (*int, *string, bool) { return code, &msg, perm }

	sig := computeSignature(wh.Secret, body)
	req, err := http.NewRequest("POST", wh.URL, bytes.NewReader(body))
	if err != nil {
		logger.Warn().Err(err).Uint("webhook_id", wh.ID).Msg("Failed to build webhook request")
		return fail(webhookErrInvalidURL, nil, true)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", "sha256="+sig)
	req.Header.Set("X-Meerkat-Event", eventType)

	client := deliveryClient
	if cfg.WebhookBlockPrivateURLs {
		client = guardedDeliveryClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errWebhookPrivateAddress) {
			// A blocked target stays blocked: no retries.
			return fail(webhookErrPrivateBlocked, nil, true)
		}
		logger.Warn().Err(err).Uint("webhook_id", wh.ID).Msg("Webhook delivery failed")
		return fail(webhookErrDeliveryFailed, nil, false)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	code := resp.StatusCode
	if code >= 200 && code < 300 {
		return &code, nil, false
	}
	return fail(fmt.Sprintf("unexpected status %d", code), &code, false)
}

func finishWebhookDelivery(db *gorm.DB, d models.WebhookDelivery, attempt int, statusCode *int, errMsg *string, permanent bool, now time.Time) models.WebhookDelivery {
	d.Attempts = attempt
	d.StatusCode = statusCode
	d.Error = errMsg
	d.LockedAt = nil
	switch {
	case errMsg == nil:
		d.Status, d.NextRetryAt = models.OutboxDelivered, nil
	case permanent || attempt >= maxOutboxAttempts:
		d.Status, d.NextRetryAt = models.OutboxDead, nil
		metrics.OutboxDead("webhook")
	default:
		next := now.Add(outboxBackoff[attempt-1])
		d.Status, d.NextRetryAt = models.OutboxPending, &next
	}
	metrics.WebhookDelivery(errMsg == nil)
	// Explicit column list: a nil pointer must reach the database as NULL.
	err := db.Model(&models.WebhookDelivery{}).Where("id = ?", d.ID).Updates(map[string]any{
		"attempts": d.Attempts, "status_code": d.StatusCode, "error": d.Error,
		"status": d.Status, "next_retry_at": d.NextRetryAt, "locked_at": nil,
	}).Error
	if err != nil {
		logger.Error().Err(err).Uint("delivery_id", d.ID).Msg("Failed to save webhook delivery")
	}
	return d
}

func computeSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return fmt.Sprintf("%x", mac.Sum(nil))
}

// ProcessWebhookOutbox delivers every pending delivery whose time has come: first tries
// that a restart interrupted, and retries after a failure. It returns when all are done.
// A delivery whose webhook was deleted or disabled, or whose owner was deleted, is dead.
func ProcessWebhookOutbox(db *gorm.DB, cfg config.Config, clk clock.Clock) {
	now := clk.Now()
	var due []models.WebhookDelivery
	if err := db.Where("status = ? AND next_retry_at <= ? AND (locked_at IS NULL OR locked_at < ?)",
		models.OutboxPending, now, now.Add(-outboxLockTimeout)).
		Order("next_retry_at").Limit(200).Find(&due).Error; err != nil {
		logger.Error().Err(err).Msg("Failed to load webhook deliveries for retry")
		return
	}

	var wg sync.WaitGroup
	for _, d := range due {
		var wh models.Webhook
		if err := db.Where("id = ? AND is_active = ? AND user_id IN (SELECT id FROM users WHERE deleted_at IS NULL)", d.WebhookID, true).First(&wh).Error; err != nil {
			logger.Warn().Err(err).Uint("webhook_id", d.WebhookID).Msg("Webhook not found, inactive or its user deleted, giving up on the delivery")
			db.Model(&models.WebhookDelivery{}).Where("id = ? AND status = ?", d.ID, models.OutboxPending).
				Updates(map[string]any{"status": models.OutboxDead, "next_retry_at": nil, "error": webhookErrGone, "locked_at": nil})
			continue
		}
		wg.Add(1)
		d, wh := d, wh
		go func() {
			defer wg.Done()
			deliverySem <- struct{}{}
			defer func() { <-deliverySem }()
			// Claim inside the slot: a claim made up front would age while queued.
			if claimWebhookDelivery(db, d, clk.Now()) {
				// The original payload is replayed, so retries share the event ID and timestamp.
				deliverWebhook(db, cfg, wh, d, clk)
			}
		}()
	}
	wg.Wait()
}

// PurgeWebhookDeliveries deletes delivered deliveries after a week, dead ones after 30
// days, and every delivery of a webhook that was deleted (nobody can see those).
func PurgeWebhookDeliveries(db *gorm.DB, now time.Time) (int64, error) {
	res := db.Unscoped().Where("(status = ? AND updated_at < ?) OR (status = ? AND updated_at < ?) OR "+
		"webhook_id NOT IN (SELECT id FROM webhooks WHERE deleted_at IS NULL)",
		models.OutboxDelivered, now.Add(-sentOutboxRetention), models.OutboxDead, now.Add(-deadOutboxRetention)).
		Delete(&models.WebhookDelivery{})
	return res.RowsAffected, res.Error
}
