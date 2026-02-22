// Package db provides SQLite database initialization with WAL mode (CG2, CG9).
// SQLite is a single-writer database, so the connection pool is pinned to one
// connection to ensure PRAGMAs apply consistently and avoid SQLITE_BUSY errors.
package db

import (
	"database/sql"
	"fmt"
	"log/slog"

	_ "modernc.org/sqlite"
)

// Open initializes a SQLite database at dbPath with optional WAL mode.
// It pins the pool to a single connection (SQLite is single-writer),
// sets performance pragmas, and applies any pending migrations.
func Open(dbPath string, walMode bool, logger *slog.Logger) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// SQLite only supports one writer at a time. Pinning to a single connection
	// ensures all PRAGMAs (busy_timeout, foreign_keys) apply to every query,
	// and avoids SQLITE_BUSY errors from concurrent pool connections.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // never expire the connection

	// SQLite tuning for NVR workload
	pragmas := []string{
		"PRAGMA busy_timeout = 5000", // retry for 5s on SQLITE_BUSY
		"PRAGMA foreign_keys = ON",
	}
	if walMode {
		pragmas = append(pragmas, "PRAGMA journal_mode = WAL")
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting pragma %q: %w", p, err)
		}
	}

	logger.Info("database opened", "path", dbPath, "wal_mode", walMode)

	if err := runMigrations(db, logger); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}
