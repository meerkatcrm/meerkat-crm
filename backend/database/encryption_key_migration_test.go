package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration37_EncryptionKeyCheckHoldsOneRow(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	m := newMigrator(t, db)
	require.NoError(t, m.Migrate(37))

	_, err = db.Exec(`INSERT INTO encryption_key_check (id, value) VALUES (1, 'v2:x')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO encryption_key_check (id, value) VALUES (2, 'v2:y')`)
	assert.Error(t, err, "encryption_key_check holds one row only")

	require.NoError(t, m.Migrate(36))
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'encryption_key_check'`).Scan(&n))
	assert.Zero(t, n, "encryption_key_check still exists after down")
}
