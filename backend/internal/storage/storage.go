// Package storage handles tiered storage management (R13/R14).
// Hot storage (SSD) holds recent recordings. Cold storage (HDD/NAS) holds archives.
// A background worker moves segments from hot → cold based on retention policies.
package storage

import (
	"log/slog"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

// Manager orchestrates hot/cold storage directories and retention cleanup.
type Manager struct {
	cfg    *config.StorageConfig
	logger *slog.Logger
}

// NewManager creates a storage manager for the given configuration.
func NewManager(cfg *config.StorageConfig, logger *slog.Logger) *Manager {
	return &Manager{
		cfg:    cfg,
		logger: logger.With("component", "storage"),
	}
}

// Start initializes storage directories and begins the retention worker.
func (m *Manager) Start() error {
	m.logger.Info("storage manager started",
		"hot_path", m.cfg.HotPath,
		"cold_path", m.cfg.ColdPath,
		"hot_retention_days", m.cfg.HotRetentionDays,
		"cold_retention_days", m.cfg.ColdRetentionDays,
	)

	// TODO: Phase 1b — Ensure directories exist
	// TODO: Phase 1b — Start background worker for hot→cold migration
	// TODO: Phase 1b — Start background worker for cold retention cleanup
	return nil
}
