// Package storage handles tiered storage management (R13/R14).
// Hot storage (SSD) holds recent recordings. Cold storage (HDD/NAS) holds archives.
// A background migrator moves segments from hot → cold after hot_retention_days.
// A background cleaner purges cold segments older than cold_retention_days.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/pathutil"
)

const (
	batchSize = 100
	dbTimeout = 15 * time.Second
)

// Manager orchestrates hot/cold storage directories and retention cleanup (R13, R14).
type Manager struct {
	cfg     *config.StorageConfig
	recRepo *recording.Repository
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	logger  *slog.Logger

	// Resolved (symlink-expanded) versions of HotPath and ColdPath.
	// NewRecorder also resolves hotPath via filepath.EvalSymlinks, so DB paths
	// are stored under the real path. We must use the same resolved base for
	// IsUnderPath comparisons or symlinked storage roots will silently skip all records.
	resolvedHotPath  string
	resolvedColdPath string
}

// NewManager creates a storage manager for the given configuration.
func NewManager(cfg *config.StorageConfig, recRepo *recording.Repository, logger *slog.Logger) *Manager {
	return &Manager{
		cfg:     cfg,
		recRepo: recRepo,
		logger:  logger.With("component", "storage"),
	}
}

// Start initializes storage directories and launches the background workers.
func (m *Manager) Start() error {
	// Ensure hot storage directory exists on startup.
	if err := os.MkdirAll(m.cfg.HotPath, 0o755); err != nil {
		return fmt.Errorf("creating hot storage directory %q: %w", m.cfg.HotPath, err)
	}
	// Ensure cold storage directory exists (skip if empty — cold not configured).
	if m.cfg.ColdPath != "" {
		if err := os.MkdirAll(m.cfg.ColdPath, 0o755); err != nil {
			return fmt.Errorf("creating cold storage directory %q: %w", m.cfg.ColdPath, err)
		}
	}

	// Resolve symlinks so IsUnderPath comparisons in the migrator/cleaner match
	// the real paths stored in the DB by NewRecorder (which also resolves hotPath).
	if resolved, err := filepath.EvalSymlinks(m.cfg.HotPath); err == nil {
		m.resolvedHotPath = resolved
	} else {
		m.resolvedHotPath = m.cfg.HotPath
	}
	if m.cfg.ColdPath != "" {
		if resolved, err := filepath.EvalSymlinks(m.cfg.ColdPath); err == nil {
			m.resolvedColdPath = resolved
		} else {
			m.resolvedColdPath = m.cfg.ColdPath
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	m.wg.Add(2)
	go m.runMigrator(ctx)
	go m.runCleaner(ctx)

	m.logger.Info("storage manager started",
		"hot_path", m.cfg.HotPath,
		"cold_path", m.cfg.ColdPath,
		"hot_retention_days", m.cfg.HotRetentionDays,
		"cold_retention_days", m.cfg.ColdRetentionDays,
		"migration_interval_hours", m.cfg.MigrationIntervalHours,
		"cleanup_interval_hours", m.cfg.CleanupIntervalHours,
	)
	return nil
}

// Stop signals both workers to exit and waits for them to finish.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	m.logger.Info("storage manager stopped")
}

// ─── Migrator ───────────────────────────────────────────────────────────────

// runMigrator is the background goroutine that moves recordings from hot → cold storage.
// It runs once immediately on start, then on the configured migration_interval_hours ticker.
func (m *Manager) runMigrator(ctx context.Context) {
	defer m.wg.Done()

	m.runMigratorOnce(ctx)

	ticker := time.NewTicker(time.Duration(m.cfg.MigrationIntervalHours) * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.runMigratorOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// runMigratorOnce processes one batch pass: move all recordings older than
// hot_retention_days from hot storage to cold storage.
func (m *Manager) runMigratorOnce(ctx context.Context) {
	if m.cfg.ColdPath == "" {
		return // cold storage not configured — nothing to migrate to
	}

	cutoff := time.Now().AddDate(0, 0, -m.cfg.HotRetentionDays)
	moved, skipped, failed := 0, 0, 0
	var afterID int // cursor: advance past every batch to prevent re-querying failed records

	for {
		if ctx.Err() != nil {
			return
		}

		dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
		recs, err := m.recRepo.ListOlderThan(dbCtx, cutoff, batchSize, afterID)
		cancel()
		if err != nil {
			m.logger.Warn("migrator: DB query failed", "error", err)
			return
		}
		if len(recs) == 0 {
			break
		}

		for _, rec := range recs {
			if ctx.Err() != nil {
				return
			}
			// Skip recordings already on cold storage or outside hot storage.
			// Use resolved paths: DB stores real paths (NewRecorder calls EvalSymlinks).
			if !pathutil.IsUnderPath(rec.Path, m.resolvedHotPath) {
				skipped++
				continue
			}

			// Compute destination path by swapping the hot prefix for cold.
			rel, relErr := filepath.Rel(m.resolvedHotPath, rec.Path)
			if relErr != nil {
				m.logger.Warn("migrator: failed to compute relative path",
					"id", rec.ID, "path", rec.Path, "hot_path", m.resolvedHotPath, "error", relErr)
				skipped++
				continue
			}
			newPath := filepath.Join(m.resolvedColdPath, rel)

			// Containment check — must not escape the cold directory.
			if !pathutil.IsUnderPath(newPath, m.resolvedColdPath) {
				m.logger.Warn("migrator: computed cold path escapes cold root, skipping",
					"id", rec.ID, "path", rec.Path, "cold_path", newPath)
				skipped++
				continue
			}

			if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
				m.logger.Warn("migrator: failed to create cold directory",
					"dir", filepath.Dir(newPath), "error", err)
				failed++
				continue
			}

			if err := moveFile(rec.Path, newPath); err != nil {
				m.logger.Warn("migrator: failed to move recording",
					"id", rec.ID, "src", rec.Path, "dst", newPath, "error", err)
				failed++
				continue
			}

			dbCtx2, cancel2 := context.WithTimeout(ctx, dbTimeout)
			updateErr := m.recRepo.UpdatePath(dbCtx2, rec.ID, newPath)
			cancel2()
			if updateErr != nil && !errors.Is(updateErr, recording.ErrNotFound) {
				// Path update failed — try to reverse the move to avoid path mismatch.
				if reverseErr := moveFile(newPath, rec.Path); reverseErr != nil {
					m.logger.Warn("migrator: path update failed and reverse-move also failed — DB and disk are now out of sync",
						"id", rec.ID, "new_path", newPath, "old_path", rec.Path,
						"update_error", updateErr, "reverse_error", reverseErr)
				} else {
					m.logger.Warn("migrator: path update failed, reversed move",
						"id", rec.ID, "error", updateErr)
				}
				failed++
				continue
			}

			m.logger.Debug("migrator: moved recording",
				"id", rec.ID, "src", rec.Path, "dst", newPath)
			moved++
		}

		afterID = recs[len(recs)-1].ID // advance cursor past this batch
		if len(recs) < batchSize {
			break // exhausted
		}
	}

	if moved > 0 || failed > 0 {
		m.logger.Info("migrator: cycle complete",
			"moved", moved, "skipped", skipped, "failed", failed,
			"cutoff", cutoff.Format(time.RFC3339))
	}
}

// ─── Cleaner ────────────────────────────────────────────────────────────────

// runCleaner is the background goroutine that deletes recordings older than
// cold_retention_days. It runs once immediately on start, then on the configured
// cleanup_interval_hours ticker.
func (m *Manager) runCleaner(ctx context.Context) {
	defer m.wg.Done()

	m.runCleanerOnce(ctx)

	ticker := time.NewTicker(time.Duration(m.cfg.CleanupIntervalHours) * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.runCleanerOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// runCleanerOnce processes one batch pass: delete all recordings older than
// cold_retention_days from both hot and cold storage.
func (m *Manager) runCleanerOnce(ctx context.Context) {
	cutoff := time.Now().AddDate(0, 0, -m.cfg.ColdRetentionDays)
	deleted, failed := 0, 0
	var afterID int // cursor: advance past every batch to prevent re-querying failed records

	for {
		if ctx.Err() != nil {
			return
		}

		dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
		recs, err := m.recRepo.ListOlderThan(dbCtx, cutoff, batchSize, afterID)
		cancel()
		if err != nil {
			m.logger.Warn("cleaner: DB query failed", "error", err)
			return
		}
		if len(recs) == 0 {
			break
		}

		for _, rec := range recs {
			if ctx.Err() != nil {
				return
			}

			// Containment check — only delete files under known storage roots.
			// Use resolved paths: DB stores real paths (NewRecorder calls EvalSymlinks).
			underHot := m.resolvedHotPath != "" && pathutil.IsUnderPath(rec.Path, m.resolvedHotPath)
			underCold := m.resolvedColdPath != "" && pathutil.IsUnderPath(rec.Path, m.resolvedColdPath)
			if !underHot && !underCold {
				m.logger.Warn("cleaner: path outside hot/cold roots, refusing to delete",
					"id", rec.ID, "path", rec.Path)
				failed++
				continue
			}

			// Delete DB record first — a leaked file is recoverable;
			// a dangling DB row pointing to a missing file is not.
			dbCtx2, cancel2 := context.WithTimeout(ctx, dbTimeout)
			deleteErr := m.recRepo.Delete(dbCtx2, rec.ID)
			cancel2()
			if deleteErr != nil && !errors.Is(deleteErr, recording.ErrNotFound) {
				m.logger.Warn("cleaner: DB delete failed, skipping file removal",
					"id", rec.ID, "error", deleteErr)
				failed++
				continue
			}

			// Remove the file; missing file is not an error (may have been removed manually).
			if err := os.Remove(rec.Path); err != nil && !os.IsNotExist(err) {
				m.logger.Warn("cleaner: file remove failed",
					"id", rec.ID, "path", rec.Path, "error", err)
				// DB record was already deleted — log but don't count as hard failure.
			}

			m.logger.Debug("cleaner: deleted recording", "id", rec.ID, "path", rec.Path)
			deleted++
		}

		afterID = recs[len(recs)-1].ID // advance cursor past this batch

		if len(recs) < batchSize {
			break
		}
	}

	if deleted > 0 || failed > 0 {
		m.logger.Info("cleaner: cycle complete",
			"deleted", deleted, "failed", failed,
			"cutoff", cutoff.Format(time.RFC3339))
	}
}

// ─── File helpers ────────────────────────────────────────────────────────────

// moveFile moves src to dst. It tries os.Rename first (atomic, zero-copy on the same
// filesystem) and falls back to a copy+remove for cross-device moves (e.g. hot=SSD → cold=NAS).
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device: copy then remove.
	if err := copyFile(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		// Copy succeeded but source removal failed (permissions, etc.).
		// Remove the copy to avoid having the file in both locations.
		_ = os.Remove(dst)
		return fmt.Errorf("removing source after cross-device copy: %w", err)
	}
	return nil
}

// copyFile copies src to dst. dst must not already exist (O_EXCL prevents overwrite).
// On error, dst is removed to avoid leaving a partial file.
func copyFile(src, dst string) (retErr error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("creating destination %q: %w", dst, err)
	}
	defer func() {
		out.Close()
		if retErr != nil {
			_ = os.Remove(dst) // clean up partial file on error
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying %q → %q: %w", src, dst, err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("syncing %q: %w", dst, err)
	}
	return nil
}
