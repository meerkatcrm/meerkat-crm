DROP TABLE IF EXISTS email_outbox;
DROP INDEX IF EXISTS idx_webhook_deliveries_status_retry;
ALTER TABLE webhook_deliveries DROP COLUMN locked_at;
ALTER TABLE webhook_deliveries DROP COLUMN status;
