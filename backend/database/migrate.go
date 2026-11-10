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
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer sqlDB.Close()

	// Run migrations
	if err := RunMigrations(sqlDB); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Open GORM connection
	db, err := gorm.Open(sqlite.Open(immediateTxDSN(dbPath)), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect with GORM: %w", err)
	}

	return db, nil
}

// immediateTxDSN makes every transaction take SQLite's write lock at BEGIN.
//
// By default a transaction starts as a reader and upgrades on its first write.
// When another connection is already writing, that upgrade fails with
// SQLITE_BUSY at once: SQLite does not apply busy_timeout, because waiting while
// holding a read lock could deadlock. Any transaction that reads before writing
// would be exposed to this. With BEGIN IMMEDIATE the wait happens at BEGIN, where
// the driver's 5s busy_timeout applies.
func immediateTxDSN(dbPath string) string {
	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	return dbPath + sep + "_txlock=immediate"
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
	sqlDB, err := sql.Open("sqlite", dbPath)
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
