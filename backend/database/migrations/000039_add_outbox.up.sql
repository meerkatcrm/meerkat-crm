-- Outbox: webhook deliveries become a queue (one row per event and webhook, updated in place
-- on every try) and emails get their own queue. status: pending, delivered, dead.
ALTER TABLE webhook_deliveries ADD COLUMN status TEXT NOT NULL DEFAULT 'delivered';
ALTER TABLE webhook_deliveries ADD COLUMN locked_at DATETIME;

-- Old rows: one row per try, a retry replayed the same payload as a new row with attempts + 1.
-- A row that still waits for its retry stays pending. A failed row without a retry is dead
-- only when no later try of the same payload exists; one that a later try superseded is
-- history of a delivery that went on (it stays 'delivered', the default, and is not counted
-- as dead).
UPDATE webhook_deliveries SET status = 'pending' WHERE next_retry_at IS NOT NULL;
UPDATE webhook_deliveries SET status = 'dead'
WHERE next_retry_at IS NULL AND error IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM webhook_deliveries later
    WHERE later.webhook_id = webhook_deliveries.webhook_id
      AND later.payload = webhook_deliveries.payload
      AND later.attempts > webhook_deliveries.attempts);

CREATE INDEX idx_webhook_deliveries_status_retry ON webhook_deliveries (status, next_retry_at);

CREATE TABLE email_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    to_addr TEXT NOT NULL,
    subject TEXT NOT NULL,
    html TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME NOT NULL,
    expires_at DATETIME,
    locked_at DATETIME,
    last_error TEXT NOT NULL DEFAULT '',
    sent_at DATETIME,
    created_at DATETIME,
    updated_at DATETIME
);
CREATE INDEX idx_email_outbox_user_id ON email_outbox (user_id);
CREATE INDEX idx_email_outbox_status_next ON email_outbox (status, next_attempt_at);
