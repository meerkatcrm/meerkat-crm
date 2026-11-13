package database

import (
	"database/sql"
	"embed"
	"fmt"
	"meerkat/logger"
	"strings"

	"github.com/glebarez/sqlite"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// InitDB initializes the database connection and runs migrations
func InitDB(dbPath string) (*gorm.DB, error) {
	// Open database connection for migrations
	sqlDB, err := sql.Open("sqlite", migrationDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer sqlDB.Close()

	// Run migrations
	if err := RunMigrations(sqlDB); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}
	// Databases written while foreign keys were off may hold orphan rows.
	warnForeignKeyViolations(sqlDB)

	// Open GORM connection
	db, err := gorm.Open(sqlite.Open(appDSN(dbPath)), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect with GORM: %w", err)
	}

	return db, nil
}

// Connection settings, applied by the driver to every pooled connection
// (the driver itself already sets busy_timeout to 5s).
//
//   - journal_mode=WAL: readers no longer block on a writer, and a commit appends
//     to the -wal file instead of rewriting pages. It persists in the database file.
//     The -wal and -shm files must stay next to the database.
//   - synchronous=NORMAL: in WAL mode a crash can lose the last commits but never
//     corrupts the database, and commits skip one fsync each.
//   - foreign_keys=ON: the schema's REFERENCES and ON DELETE CASCADE take effect.
//   - _txlock=immediate: every transaction takes the write lock at BEGIN. By default
//     a transaction starts as a reader and upgrades on its first write; when another
//     connection is already writing, that upgrade fails with SQLITE_BUSY at once,
//     because waiting while holding a read lock could deadlock. With BEGIN IMMEDIATE
//     the wait happens at BEGIN, where busy_timeout applies.
const (
	pragmaWAL         = "_pragma=journal_mode(WAL)"
	pragmaSyncNormal  = "_pragma=synchronous(NORMAL)"
	pragmaForeignKeys = "_pragma=foreign_keys(1)"
	txLockImmediate   = "_txlock=immediate"
)

// appDSN is the DSN of the application's connection pool.
func appDSN(dbPath string) string {
	return withDSNParams(dbPath, pragmaWAL, pragmaSyncNormal, pragmaForeignKeys, txLockImmediate)
}

// migrationDSN leaves foreign keys off: migrations recreate tables, which with
// enforcement on would cascade deletes into, or fail on, the dependent rows.
func migrationDSN(dbPath string) string {
	return withDSNParams(dbPath, pragmaWAL, pragmaSyncNormal)
}

func withDSNParams(dbPath string, params ...string) string {
	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	return dbPath + sep + strings.Join(params, "&")
}

// foreignKeyViolations counts the rows PRAGMA foreign_key_check reports, per
// "table -> parent". It holds no row data.
func foreignKeyViolations(db *sql.DB) (map[string]int, error) {
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var table, parent string
		var rowid, fkid sql.NullInt64
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return nil, err
		}
		counts[table+" -> "+parent]++
	}
	return counts, rows.Err()
}

func warnForeignKeyViolations(db *sql.DB) {
	counts, err := foreignKeyViolations(db)
	if err != nil {
		logger.Warn().Err(err).Msg("Could not run the foreign key check")
		return
	}
	// The check runs on every start (it takes milliseconds on a CRM-sized database),
	// so the warning repeats until the rows are cleaned up.
	for relation, n := range counts {
		logger.Warn().Str("relation", relation).Int("rows", n).
			Msg(`Rows reference a parent that no longer exists; changing them can fail now that foreign keys are enforced. ` +
				`List them with: sqlite3 <db> "PRAGMA foreign_key_check", then delete them or point them at an existing parent`)
	}
}

// RunMigrations runs all pending database migrations
func RunMigrations(db *sql.DB) error {
	driver, err := withInstance(db, &sqliteConfig{})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}

	// Get current version
	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	if dirty {
		// Dirty means migration `version` failed. Each migration runs in one
		// transaction, so nothing of it was applied: step back and let Up run it
		// again. Forcing `version` itself would mark the failed migration as done
		// and silently leave its changes out of the schema.
		previous := int(version) - 1
		if previous < 1 {
			previous = database.NilVersion
		}
		logger.Warn().Uint("version", version).Msg("Database is in dirty state, retrying the failed migration")
		if err := m.Force(previous); err != nil {
			return fmt.Errorf("failed to reset dirty migration state: %w", err)
		}
	}

	// Run migrations
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	// Get final version
	version, _, err = m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to get final version: %w", err)
	}

	if err == migrate.ErrNilVersion {
		logger.Info().Msg("No migrations applied (database is empty)")
	} else {
		logger.Info().Uint("version", version).Msg("Migrations applied successfully")
	}

	return nil
}

// MigrateDown rolls back the last migration (use with caution)
func MigrateDown(dbPath string) error {
	sqlDB, err := sql.Open("sqlite", migrationDSN(dbPath))
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer sqlDB.Close()

	driver, err := withInstance(sqlDB, &sqliteConfig{})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}

	if err := m.Steps(-1); err != nil {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	logger.Info().Msg("Migration rolled back successfully")
	return nil
}
