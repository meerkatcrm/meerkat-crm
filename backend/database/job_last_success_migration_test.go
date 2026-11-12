package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration38_AddsJobLastSuccess(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	m := newMigrator(t, db)
	require.NoError(t, m.Migrate(37))
	_, err = db.Exec(`INSERT INTO job_executions (job_name, last_run_at) VALUES ('daily_reminders', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(38))
	var success sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT last_success_at FROM job_executions WHERE job_name = 'daily_reminders'`).Scan(&success))
	assert.False(t, success.Valid, "unknown until the next successful run")

	require.NoError(t, m.Migrate(37))
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM job_executions`).Scan(&n))
	assert.Equal(t, 1, n, "rolling back keeps the rows")
}
