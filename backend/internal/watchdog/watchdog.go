// Package watchdog implements the supervisor process (R4).
// It monitors camera pipelines and the core engine, restarting
// failed components and logging "System Restart" events to the timeline.
package watchdog

import (
	"log/slog"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

type Watchdog struct {
	cfg    *config.WatchdogConfig
	logger *slog.Logger
	stopCh chan struct{}
}

func New(cfg *config.WatchdogConfig, logger *slog.Logger) *Watchdog {
	return &Watchdog{
		cfg:    cfg,
		logger: logger.With("component", "watchdog"),
		stopCh: make(chan struct{}),
	}
}

// Start begins the watchdog monitoring loop.
func (w *Watchdog) Start() {
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

// Stop shuts down the watchdog.
func (w *Watchdog) Stop() {
	close(w.stopCh)
}

func (w *Watchdog) check() {
	// TODO: Phase 1b — Check camera pipeline health
	// TODO: Phase 1b — Check disk space on hot/cold storage
	// TODO: Phase 1b — Log "System Restart" events to database on recovery
	w.logger.Debug("health check passed")
}
