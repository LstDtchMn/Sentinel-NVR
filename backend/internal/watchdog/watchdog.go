// Package watchdog implements the supervisor process (R4).
// It monitors camera pipelines and disk space, restarting failed components
// and logging "System Restart" events to the timeline on startup.
package watchdog

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
)

// diskWarningThreshold is the fraction of disk used that triggers a warning event.
const diskWarningThreshold = 0.90

// CameraManager is the subset of camera.Manager used by the watchdog.
// Defined as an interface to allow unit testing with a mock.
type CameraManager interface {
	ListCameras(ctx context.Context) ([]camera.CameraWithStatus, error)
	RestartCamera(ctx context.Context, name string) error
}

// Watchdog monitors camera pipelines and core subsystems, restarting failed
// components and logging "System Restart" events to the timeline (R4, CG9).
type Watchdog struct {
	cfg        *config.WatchdogConfig
	storageCfg *config.StorageConfig
	manager    CameraManager
	bus        *eventbus.Bus
	logger     *slog.Logger
	stopCh     chan struct{}
	doneCh     chan struct{} // closed when Start() returns so Stop() can wait
	stopOnce   sync.Once

	// errorSince tracks when each camera first entered StateError.
	// Only accessed from the single goroutine running check(); no mutex needed.
	errorSince map[string]time.Time

	// diskWarned tracks storage paths that have already triggered a warning
	// to avoid spamming the log and event bus on every tick. Reset when usage drops.
	diskWarned map[string]bool
}

// New creates a watchdog with the given health check configuration.
func New(
	cfg *config.WatchdogConfig,
	storageCfg *config.StorageConfig,
	manager CameraManager,
	bus *eventbus.Bus,
	logger *slog.Logger,
) *Watchdog {
	return &Watchdog{
		cfg:        cfg,
		storageCfg: storageCfg,
		manager:    manager,
		bus:        bus,
		logger:     logger.With("component", "watchdog"),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		errorSince: make(map[string]time.Time),
		diskWarned: make(map[string]bool),
	}
}

// Start begins the watchdog monitoring loop.
// It publishes a "system.restart" event on startup to mark the timeline (R4),
// then checks camera pipelines and disk space on each health-check tick.
// It closes doneCh when it returns, allowing Stop() callers to wait.
func (w *Watchdog) Start() {
	defer close(w.doneCh)

	if !w.cfg.Enabled {
		w.logger.Info("watchdog disabled")
		return
	}

	// Publish system restart event to mark the timeline (R4).
	// Fires every time the process starts so operators can see recording gaps
	// caused by crashes or intentional restarts.
	w.bus.Publish(eventbus.Event{
		Type:  "system.restart",
		Label: "System started",
	})
	w.logger.Info("system restart event published to timeline")

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

// check runs a single health-check pass: disk space then camera pipelines.
func (w *Watchdog) check() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	w.checkDiskSpace()
	w.checkCameraPipelines(ctx)
}

// checkDiskSpace inspects hot and cold storage paths and publishes
// "storage.almost_full" events when either path exceeds diskWarningThreshold.
// Warning state resets once usage drops back below the threshold so operators
// receive a recovery log line when the problem is resolved.
func (w *Watchdog) checkDiskSpace() {
	type storageDir struct{ label, path string }
	dirs := []storageDir{{"hot", w.storageCfg.HotPath}}
	if w.storageCfg.ColdPath != "" {
		dirs = append(dirs, storageDir{"cold", w.storageCfg.ColdPath})
	}

	for _, d := range dirs {
		avail, total, err := diskUsage(d.path)
		if err != nil {
			w.logger.Warn("disk space check failed", "tier", d.label, "path", d.path, "error", err)
			continue
		}
		if total == 0 {
			continue
		}

		usedFraction := float64(total-avail) / float64(total)
		usedPct := int(usedFraction * 100)

		if usedFraction >= diskWarningThreshold {
			if !w.diskWarned[d.label] {
				w.logger.Warn("storage almost full",
					"tier", d.label,
					"path", d.path,
					"used_pct", usedPct,
				)
				w.bus.Publish(eventbus.Event{
					Type:  "storage.almost_full",
					Label: fmt.Sprintf("%s storage %d%% full (%s)", d.label, usedPct, d.path),
				})
				w.diskWarned[d.label] = true
			}
		} else {
			if w.diskWarned[d.label] {
				w.logger.Info("storage usage recovered below warning threshold",
					"tier", d.label, "used_pct", usedPct)
				w.diskWarned[d.label] = false
			}
			w.logger.Debug("disk space ok", "tier", d.label, "used_pct", usedPct)
		}
	}
}

// checkCameraPipelines inspects each enabled camera's pipeline state and restarts
// any that have been stuck in StateError for longer than the configured restart delay.
// Cameras that recover on their own (pipeline's internal retry loop) clear the error
// timer automatically — the watchdog restart is a last-resort after sustained failure.
func (w *Watchdog) checkCameraPipelines(ctx context.Context) {
	cameras, err := w.manager.ListCameras(ctx)
	if err != nil {
		w.logger.Warn("watchdog could not list cameras", "error", err)
		return
	}

	restartDelay := time.Duration(w.cfg.RestartDelay) * time.Second

	for _, cam := range cameras {
		name := cam.Name
		state := cam.PipelineStatus.State

		if !cam.Enabled {
			// Disabled cameras are expected to be idle — clear any stale error timer.
			delete(w.errorSince, name)
			continue
		}

		switch state {
		case camera.StateError:
			first, tracking := w.errorSince[name]
			if !tracking {
				// First tick seeing this camera in error — start the clock.
				w.errorSince[name] = time.Now()
				w.logger.Warn("camera pipeline entered error state",
					"camera", name,
					"last_error", cam.PipelineStatus.LastError,
				)
			} else if time.Since(first) >= restartDelay {
				// Camera has been in error for longer than restart_delay — force restart.
				w.logger.Info("restarting camera pipeline after sustained error",
					"camera", name,
					"error_duration", time.Since(first).Round(time.Second),
					"last_error", cam.PipelineStatus.LastError,
				)
				if err := w.manager.RestartCamera(ctx, name); err != nil {
					w.logger.Error("watchdog failed to restart camera pipeline",
						"camera", name, "error", err)
				} else {
					w.logger.Info("camera pipeline restarted by watchdog", "camera", name)
					w.bus.Publish(eventbus.Event{
						Type:  "camera.restarted",
						Label: name,
					})
					// Reset timer so we give the freshly-started pipeline a chance to stabilize
					// before triggering another restart on the next tick.
					delete(w.errorSince, name)
				}
			}

		default:
			// Pipeline is healthy (streaming, recording, connecting, or idle).
			// Clear any tracked error time and log a recovery message if we were tracking one.
			if _, wasErroring := w.errorSince[name]; wasErroring {
				w.logger.Info("camera pipeline recovered",
					"camera", name, "state", string(state))
				delete(w.errorSince, name)
			}
		}
	}

	// Remove stale entries for cameras that were deleted while in error state.
	active := make(map[string]bool, len(cameras))
	for _, cam := range cameras {
		active[cam.Name] = true
	}
	for name := range w.errorSince {
		if !active[name] {
			delete(w.errorSince, name)
		}
	}
}
