package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration39_TurnsWebhookDeliveriesIntoAnOutbox(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	m := newMigrator(t, db)
	require.NoError(t, m.Migrate(38))
	for _, q := range []string{
		"INSERT INTO users (id, username, email, password) VALUES (1, 'u', 'u@example.com', 'x')",
		"INSERT INTO webhooks (id, user_id, name, url, secret) VALUES (1, 1, 'w', 'http://x', 's')",
		// succeeded / waiting for a retry / failed for good
		"INSERT INTO webhook_deliveries (id, webhook_id, event_type, payload, status_code, attempts) VALUES (1, 1, 'e', '{}', 204, 1)",
		"INSERT INTO webhook_deliveries (id, webhook_id, event_type, payload, error, attempts, next_retry_at) VALUES (2, 1, 'e', '{}', 'boom', 1, '2030-01-01')",
		"INSERT INTO webhook_deliveries (id, webhook_id, event_type, payload, error, attempts) VALUES (3, 1, 'e', '{}', 'boom', 3)",
		// a failed try that a later try delivered, and a chain that failed to the end
		"INSERT INTO webhook_deliveries (id, webhook_id, event_type, payload, error, attempts) VALUES (4, 1, 'e', 'p2', 'boom', 1)",
		"INSERT INTO webhook_deliveries (id, webhook_id, event_type, payload, status_code, attempts) VALUES (5, 1, 'e', 'p2', 204, 2)",
		"INSERT INTO webhook_deliveries (id, webhook_id, event_type, payload, error, attempts) VALUES (6, 1, 'e', 'p3', 'boom', 1)",
		"INSERT INTO webhook_deliveries (id, webhook_id, event_type, payload, error, attempts) VALUES (7, 1, 'e', 'p3', 'boom', 2)",
	} {
		_, err := db.Exec(q)
		require.NoError(t, err, q)
	}

	require.NoError(t, m.Migrate(39))
	want := map[int]string{1: "delivered", 2: "pending", 3: "dead", 4: "delivered", 5: "delivered", 6: "delivered", 7: "dead"}
	for id, status := range want {
		var got string
		require.NoError(t, db.QueryRow("SELECT status FROM webhook_deliveries WHERE id = ?", id).Scan(&got))
		assert.Equal(t, status, got, "delivery %d", id)
	}
	_, err = db.Exec("INSERT INTO email_outbox (to_addr, subject, html, next_attempt_at) VALUES ('a@b.c', 's', 'h', '2030-01-01')")
	require.NoError(t, err)

	require.NoError(t, m.Migrate(38))
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('webhook_deliveries') WHERE name IN ('status', 'locked_at')").Scan(&n))
	assert.Zero(t, n, "outbox columns still present after down")
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM webhook_deliveries").Scan(&n))
	assert.Equal(t, 7, n)
	_, err = db.Exec("SELECT 1 FROM email_outbox")
	assert.Error(t, err, "email_outbox still present after down")
}
