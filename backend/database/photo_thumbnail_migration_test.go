package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration40_AddsThumbnailFileAndKeepsTheLegacyColumn(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	m := newMigrator(t, db)
	require.NoError(t, m.Migrate(39))
	_, err = db.Exec(`INSERT INTO users (id, username, email, password) VALUES (1, 'u', 'u@example.com', 'x')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO contacts (id, user_id, firstname, photo_thumbnail) VALUES (1, 1, 'A', 'data:image/jpeg;base64,AA')`)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(40))
	var file, legacy string
	require.NoError(t, db.QueryRow(`SELECT photo_thumbnail_file, photo_thumbnail FROM contacts WHERE id = 1`).Scan(&file, &legacy))
	assert.Empty(t, file, "the export at startup fills it")
	assert.Equal(t, "data:image/jpeg;base64,AA", legacy, "the legacy copy stays for a rollback")

	require.NoError(t, m.Migrate(39))
	require.NoError(t, db.QueryRow(`SELECT photo_thumbnail FROM contacts WHERE id = 1`).Scan(&legacy))
	assert.Equal(t, "data:image/jpeg;base64,AA", legacy, "a rollback keeps the old thumbnails")
}
