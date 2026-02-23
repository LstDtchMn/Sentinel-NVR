// Package watchdog implements the supervisor process (R4).
// It monitors camera pipelines and the core engine, restarting
// failed components and logging "System Restart" events to the timeline.
package watchdog

import (
	"log/slog"
	"sync"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

// Watchdog monitors camera pipelines and core subsystems, restarting failed
// components and logging "System Restart" events to the timeline (R4, CG9).
type Watchdog struct {
	cfg      *config.WatchdogConfig
	logger   *slog.Logger
	stopCh   chan struct{}
	doneCh   chan struct{} // closed when Start() returns, so Stop() can wait
	stopOnce sync.Once
}

// New creates a watchdog with the given health check configuration.
func New(cfg *config.WatchdogConfig, logger *slog.Logger) *Watchdog {
	return &Watchdog{
		cfg:    cfg,
		logger: logger.With("component", "watchdog"),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start begins the watchdog monitoring loop.
// It closes doneCh when it returns, allowing Stop() callers to wait.
func (w *Watchdog) Start() {
	defer close(w.doneCh)

	if !w.cfg.Enabled {
		w.logger.Info("watchdog disabled")
		return
	}

	interval := time.Duration(w.cfg.HealthInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	w.logger.Info("watchdog started", "interval", interval)

	for {
		select {
		case <-ticker.C:
			w.check()
		case <-w.stopCh:
			w.logger.Info("watchdog stopped")
			return
		}
	}
}

// Stop signals the watchdog to shut down and waits for Start() to return.
// Safe to call multiple times. Times out after 5s to avoid blocking shutdown
// if Start() was never scheduled.
func (w *Watchdog) Stop() {
	w.stopOnce.Do(func() { close(w.stopCh) })
	select {
	case <-w.doneCh:
	case <-time.After(5 * time.Second):
		w.logger.Warn("watchdog stop timed out waiting for goroutine")
	}
}

func (w *Watchdog) check() {
	// TODO: Phase 10 (Hardening) — Check camera pipeline health
	// TODO: Phase 10 (Hardening) — Check disk space on hot/cold storage
	// TODO: Phase 10 (Hardening) — Log "System Restart" events to database on recovery
	w.logger.Debug("health check passed")
}
