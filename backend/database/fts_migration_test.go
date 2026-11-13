package database

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ftsCount(t *testing.T, db *sql.DB, table, match string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM "+table+" WHERE "+table+" MATCH ?", match).Scan(&n), "%s %q", table, match)
	return n
}

func TestMigration41_BuildsTheSearchIndexAndTriggersKeepItInSync(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	m := newMigrator(t, db)
	require.NoError(t, m.Migrate(40))
	exec := func(q string) {
		t.Helper()
		_, err := db.Exec(q)
		require.NoError(t, err, q)
	}
	exec("INSERT INTO users (id, username, email, password) VALUES (1, 'u', 'u@example.com', 'x')")
	exec(`INSERT INTO contacts (id, user_id, firstname, lastname, emails, phones) VALUES
		(1, 1, 'Zoë', 'Müller', '[{"type":"home","value":"zoe@work.example"}]', '[{"type":"mobile","value":"+41 79 555 0101"}]'),
		(2, 1, 'Bob', 'Smith', 'not json', NULL)`)
	exec("INSERT INTO notes (id, user_id, content, date) VALUES (1, 1, 'Talked about the Zürich trip', '2030-01-01')")
	exec("INSERT INTO activities (id, user_id, title, description, location, date) VALUES (1, 1, 'Café catch-up', 'Planning', 'Münster', '2030-01-01')")

	// Up: existing rows are indexed
	require.NoError(t, m.Migrate(41))
	for table, matches := range map[string][]string{
		"contacts_fts":   {"zoe", "muller", "mül*", "work*", "555", "smith"},
		"notes_fts":      {"zurich", "zür*"},
		"activities_fts": {"cafe", "munster", "plan*"},
	} {
		for _, match := range matches {
			assert.Equal(t, 1, ftsCount(t, db, table, match), "%s MATCH %q", table, match)
		}
	}

	// Triggers: insert, update (only indexed columns matter), delete
	exec(`INSERT INTO contacts (id, user_id, firstname, emails) VALUES (3, 1, 'Carol', '[{"value":"carol@example.org"}]')`)
	assert.Equal(t, 1, ftsCount(t, db, "contacts_fts", "carol"))
	exec("UPDATE contacts SET firstname = 'Karolina' WHERE id = 3")
	assert.Equal(t, 1, ftsCount(t, db, "contacts_fts", "karolina"))
	assert.Equal(t, 1, ftsCount(t, db, "contacts_fts", "carol"), "still matches through the e-mail address")
	exec("UPDATE contacts SET emails = '[]' WHERE id = 3")
	assert.Equal(t, 0, ftsCount(t, db, "contacts_fts", "carol"))
	exec("UPDATE contacts SET archived = 1 WHERE id = 3")
	assert.Equal(t, 1, ftsCount(t, db, "contacts_fts", "karolina"), "an unrelated update keeps the row indexed")
	exec("DELETE FROM contacts WHERE id = 3")
	assert.Equal(t, 0, ftsCount(t, db, "contacts_fts", "karolina"))

	exec("UPDATE notes SET content = 'Lunch plans' WHERE id = 1")
	assert.Equal(t, 0, ftsCount(t, db, "notes_fts", "zurich"))
	assert.Equal(t, 1, ftsCount(t, db, "notes_fts", "lunch"))
	exec("UPDATE activities SET title = 'Dinner' WHERE id = 1")
	assert.Equal(t, 0, ftsCount(t, db, "activities_fts", "cafe"))
	assert.Equal(t, 1, ftsCount(t, db, "activities_fts", "dinner"))
	exec("DELETE FROM notes WHERE id = 1")
	exec("DELETE FROM activities WHERE id = 1")
	assert.Equal(t, 0, ftsCount(t, db, "notes_fts", "lunch"))
	assert.Equal(t, 0, ftsCount(t, db, "activities_fts", "dinner"))

	// Down: tables and triggers go, the data stays
	require.NoError(t, m.Migrate(40))
	var n int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name LIKE '%\\_fts%' ESCAPE '\\'").Scan(&n))
	assert.Zero(t, n, "search objects left after down")
	require.NoError(t, db.QueryRow("SELECT count(*) FROM contacts").Scan(&n))
	assert.Equal(t, 2, n)
}

// A migration that recreates contacts, notes or activities drops their triggers with the
// table, and the search index would silently go stale. It must recreate them.
func TestSearchIndexTriggersSurviveAllMigrations(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := newMigrator(t, db).Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatal(err)
	}
	for _, table := range []string{"contacts", "notes", "activities"} {
		for _, suffix := range []string{"ai", "au", "ad"} {
			name := table + "_fts_" + suffix
			var n int
			require.NoError(t, db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'trigger' AND name = ? AND tbl_name = ?", name, table).Scan(&n))
			assert.Equal(t, 1, n, "trigger %s is missing: recreate it and rebuild %s_fts", name, table)
		}
	}
}
