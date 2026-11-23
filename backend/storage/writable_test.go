package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfRoot: as root every directory is writable, so the cases below cannot fail.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root; nothing is unwritable")
	}
}

// unwritable makes dir (and its content) unwritable, and restores it for the cleanup.
func unwritable(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestCheckWritableAcceptsUsableLocations(t *testing.T) {
	dir := t.TempDir()
	photos := filepath.Join(dir, "photos")
	db := filepath.Join(dir, "meerkat.db")
	require.NoError(t, os.WriteFile(db, []byte("not really a database"), 0o644))

	problems := CheckWritable(
		Probe{Field: "SQLITE_DB_PATH", HostField: "DATA_PATH", Dir: dir, File: db},
		Probe{Field: "PROFILE_PHOTO_DIR", HostField: "PHOTOS_PATH", Dir: photos},
	)
	assert.Empty(t, problems)

	// The photo directory a missing probe location had to create.
	require.DirExists(t, photos)
	// No probe file left behind, in either directory.
	for _, dir := range []string{dir, photos} {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			assert.NotContains(t, e.Name(), probePrefix, "in %s", dir)
		}
	}
	// Opening the database for writing must not truncate it.
	kept, err := os.ReadFile(db)
	require.NoError(t, err)
	assert.Equal(t, "not really a database", string(kept))
}

func TestCheckWritableCreatesAMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	require.Empty(t, CheckWritable(Probe{Field: "SQLITE_DB_PATH", Dir: dir}))
	require.DirExists(t, dir)
}

func TestCheckWritableSkipsAnUnsetLocation(t *testing.T) {
	assert.Empty(t, CheckWritable(Probe{Field: "PROFILE_PHOTO_DIR", Dir: ""}))
}

func TestCheckWritableReportsAnUnwritableDirectory(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	unwritable(t, dir)

	problems := CheckWritable(Probe{Field: "SQLITE_DB_PATH", Dir: dir, File: filepath.Join(dir, "meerkat.db")})
	require.Len(t, problems, 1)
	assert.Equal(t, dir, problems[0].Path)
	assert.Equal(t, "SQLITE_DB_PATH", problems[0].Field)
	assert.Equal(t, os.Getuid(), problems[0].UID)
	assert.Equal(t, os.Getgid(), problems[0].GID)
	// The file behind an unwritable directory is not reported twice.
	assert.NotContains(t, problems[0].Error(), "meerkat.db")
}

func TestCheckWritableReportsADirectoryItCannotCreate(t *testing.T) {
	skipIfRoot(t)
	parent := t.TempDir()
	unwritable(t, parent)
	dir := filepath.Join(parent, "data")

	problems := CheckWritable(Probe{Field: "PROFILE_PHOTO_DIR", Dir: dir})
	require.Len(t, problems, 1)
	assert.Equal(t, dir, problems[0].Path)
	assert.Contains(t, problems[0].Error(), "chown -R")
}

func TestCheckWritableReportsADatabaseOwnedBySomebodyElse(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	db := filepath.Join(dir, "meerkat.db")
	require.NoError(t, os.WriteFile(db, []byte("x"), 0o644))
	require.NoError(t, os.Chmod(db, 0o444))
	t.Cleanup(func() { _ = os.Chmod(db, 0o644) })

	problems := CheckWritable(Probe{Field: "SQLITE_DB_PATH", Dir: dir, File: db})
	require.Len(t, problems, 1, "a writable directory is not enough when the database itself is read-only")
	assert.Equal(t, db, problems[0].Path)
	assert.Contains(t, problems[0].Error(), "meerkat.db")
	assert.Contains(t, problems[0].Error(), "chown -R")
}

func TestCheckWritableIgnoresAnAbsentFile(t *testing.T) {
	problems := CheckWritable(Probe{
		Field: "SQLITE_DB_PATH",
		Dir:   t.TempDir(),
		File:  filepath.Join(t.TempDir(), "fresh", "meerkat.db"),
	})
	assert.Empty(t, problems, "a first start has no database file yet")
}

func TestProblemErrorNamesTheHostPathOfTheMount(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	unwritable(t, dir)
	t.Setenv("DATA_PATH", "./data")

	problems := CheckWritable(Probe{Field: "SQLITE_DB_PATH", HostField: "DATA_PATH", Dir: dir})
	require.Len(t, problems, 1)
	msg := problems[0].Error()
	assert.Contains(t, msg, fmt.Sprintf("uid %d, gid %d", os.Getuid(), os.Getgid()))
	assert.Contains(t, msg, "chown -R "+fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())+" ./data")
	assert.Contains(t, msg, "DATA_PATH")
	assert.Contains(t, msg, dir)
}

func TestProblemErrorWithoutAHostPath(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	unwritable(t, dir)

	problems := CheckWritable(Probe{Field: "PROFILE_PHOTO_DIR", Dir: dir})
	require.Len(t, problems, 1)
	// No DATA_PATH/PHOTOS_PATH to quote (bare `go run`, or volumes set elsewhere).
	assert.Contains(t, problems[0].Error(), "chown -R "+fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())+" <the host directory mounted at "+dir+">")
}

func TestProblemErrorWithoutAChownHint(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "notadir")
	require.NoError(t, os.WriteFile(blocker, []byte("a file where a directory should be"), 0o644))

	problems := CheckWritable(Probe{Field: "PROFILE_PHOTO_DIR", Dir: filepath.Join(blocker, "photos")})
	require.Len(t, problems, 1)
	msg := problems[0].Error()
	assert.Contains(t, msg, "Check the mount")
	assert.False(t, strings.Contains(msg, "chown"), "chowning a path through a file fixes nothing: %s", msg)
}
