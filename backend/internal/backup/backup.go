// Package backup provides scheduled SQLite database backups using VACUUM INTO.
// Backups are atomic and safe with WAL mode — no locking or checkpoint required.
package backup

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// BackupInfo describes a completed backup file.
type BackupInfo struct {
	Filename  string    `json:"filename"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// Manager handles scheduled SQLite backups with automatic pruning.
type Manager struct {
	db        *sql.DB
	dir       string        // directory where .db backup files are stored
	keep      int           // maximum number of backups to retain
	interval  time.Duration // time between scheduled backups
	logger    *slog.Logger
	stopCh    chan struct{}
	stopped   sync.WaitGroup
}

// New creates a backup Manager. Call Start() to begin the scheduled ticker.
func New(db *sql.DB, dir string, keep int, interval time.Duration, logger *slog.Logger) *Manager {
	return &Manager{
		db:       db,
		dir:      dir,
		keep:     keep,
		interval: interval,
		logger:   logger.With("component", "backup"),
		stopCh:   make(chan struct{}),
	}
}

// Start creates the backup directory (if needed) and launches the background
// ticker goroutine that performs periodic backups.
func (m *Manager) Start() {
	if err := os.MkdirAll(m.dir, 0750); err != nil {
		m.logger.Error("failed to create backup directory", "dir", m.dir, "error", err)
		return
	}

	m.stopped.Add(1)
	go func() {
		defer m.stopped.Done()
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				if _, err := m.RunNow(); err != nil {
					m.logger.Error("scheduled backup failed", "error", err)
				}
			}
		}
	}()

	m.logger.Info("backup manager started",
		"dir", m.dir,
		"interval", m.interval.String(),
		"keep", m.keep,
	)
}

// Stop signals the background goroutine to exit and waits for it to finish.
func (m *Manager) Stop() {
	close(m.stopCh)
	m.stopped.Wait()
	m.logger.Info("backup manager stopped")
}

// RunNow performs an immediate backup using VACUUM INTO and prunes old backups.
// Returns the BackupInfo of the newly created backup.
func (m *Manager) RunNow() (BackupInfo, error) {
	if err := os.MkdirAll(m.dir, 0750); err != nil {
		return BackupInfo{}, fmt.Errorf("creating backup dir: %w", err)
	}

	filename := fmt.Sprintf("sentinel_%s.db", time.Now().UTC().Format("20060102_150405"))
	destPath := filepath.Join(m.dir, filename)

	// VACUUM INTO creates a standalone, self-contained copy of the database.
	// It is safe to run concurrently with readers/writers in WAL mode and does
	// not require a checkpoint or any lock escalation.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// SQLite VACUUM INTO requires the path as a string literal in the SQL statement.
	// Use filepath.ToSlash for cross-platform compatibility (Windows backslashes
	// cause syntax errors in SQLite).
	safePath := filepath.ToSlash(destPath)
	_, err := m.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", safePath))
	if err != nil {
		return BackupInfo{}, fmt.Errorf("VACUUM INTO failed: %w", err)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("stat backup file: %w", err)
	}

	result := BackupInfo{
		Filename:  filename,
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime(),
	}

	m.logger.Info("backup created", "filename", filename, "size_bytes", info.Size())

	// Prune old backups beyond the keep limit.
	if err := m.prune(); err != nil {
		m.logger.Warn("failed to prune old backups", "error", err)
	}

	return result, nil
}

// List returns all .db backup files in the backup directory, sorted newest-first.
func (m *Manager) List() ([]BackupInfo, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupInfo{}, nil
		}
		return nil, fmt.Errorf("reading backup dir: %w", err)
	}

	var backups []BackupInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, BackupInfo{
			Filename:  entry.Name(),
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime(),
		})
	}

	// Sort newest-first by modification time.
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// prune removes the oldest backups when the count exceeds m.keep.
func (m *Manager) prune() error {
	backups, err := m.List()
	if err != nil {
		return err
	}

	if len(backups) <= m.keep {
		return nil
	}

	// backups is sorted newest-first; remove entries beyond the keep limit.
	for _, b := range backups[m.keep:] {
		path := filepath.Join(m.dir, b.Filename)
		if err := os.Remove(path); err != nil {
			m.logger.Warn("failed to remove old backup", "file", b.Filename, "error", err)
		} else {
			m.logger.Info("pruned old backup", "file", b.Filename)
		}
	}

	return nil
}
