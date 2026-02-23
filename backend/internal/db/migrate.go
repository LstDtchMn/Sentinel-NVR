package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// runMigrations applies any pending SQL migrations embedded in the binary.
// Each migration file is split by semicolons and each statement is executed
// individually within a transaction to avoid partial-apply issues with drivers
// that only execute the first statement in a multi-statement Exec call.
func runMigrations(db *sql.DB, logger *slog.Logger) error {
	// Create migrations tracking table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	// Read embedded migration files
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	// Sort by filename (numeric prefix ensures order)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		// Parse version from filename prefix (e.g., "001_initial_schema.sql" → 1)
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			logger.Warn("skipping migration with invalid version", "file", entry.Name())
			continue
		}

		if err := applyMigration(db, entry.Name(), version, logger); err != nil {
			return err
		}
	}

	return nil
}

// applyMigration applies a single migration file within a transaction.
// Extracted from the loop so the deferred rollback is correctly scoped to
// each migration instead of accumulating defers at the outer function level.
func applyMigration(db *sql.DB, filename string, version int, logger *slog.Logger) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction for migration %d: %w", version, err)
	}
	// Log rollback errors other than the expected "tx already done" after a
	// successful Commit. A failed rollback leaves the transaction open, which
	// would block the single-connection pool permanently.
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			logger.Warn("rollback failed for migration", "migration", filename, "error", rbErr)
		}
	}()

	// Check if already applied inside the transaction so the check and the
	// subsequent INSERT are atomic — prevents double-application if two
	// processes start simultaneously.
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM _migrations WHERE version = ?", version).Scan(&count); err != nil {
		return fmt.Errorf("checking migration %d: %w", version, err)
	}
	if count > 0 {
		// Already applied — returning nil causes the defer to call tx.Rollback(),
		// which is correct (no writes made; rolling back a read-only tx is safe).
		return nil
	}

	// Read migration file only after confirming it needs to be applied — avoids
	// unnecessary embedded FS I/O for already-applied migrations on every startup.
	content, err := migrationsFS.ReadFile("migrations/" + filename)
	if err != nil {
		return fmt.Errorf("reading migration %s: %w", filename, err)
	}

	// Split by semicolons and execute each statement individually.
	// This avoids partial execution if the driver only runs the first
	// statement in a multi-statement Exec call.
	//
	// LIMITATION: This naive splitter will break on SQL containing semicolons
	// inside string literals or trigger bodies (BEGIN...END;). If you need
	// triggers or complex seed data, use the "--;;--" delimiter convention
	// or switch to a migration library like golang-migrate.
	for _, stmt := range strings.Split(string(content), ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("executing statement in migration %s: %w\nstatement: %s", filename, err, stmt)
		}
	}

	if _, err := tx.Exec(
		"INSERT INTO _migrations (version, name) VALUES (?, ?)",
		version, filename,
	); err != nil {
		return fmt.Errorf("recording migration %s: %w", filename, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migration %s: %w", filename, err)
	}

	logger.Info("applied migration", "version", version, "name", filename)
	return nil
}
