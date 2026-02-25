package watchdog_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/watchdog"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockManager implements watchdog.CameraManager for test purposes.
type mockManager struct {
	mu             sync.Mutex
	cameras        []camera.CameraWithStatus
	restartedNames []string
	restartErr     error
}

func (m *mockManager) ListCameras(_ context.Context) ([]camera.CameraWithStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]camera.CameraWithStatus(nil), m.cameras...), nil
}

func (m *mockManager) RestartCamera(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.restartErr != nil {
		return m.restartErr
	}
	m.restartedNames = append(m.restartedNames, name)
	return nil
}

func (m *mockManager) setCameras(cams []camera.CameraWithStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cameras = cams
}

func (m *mockManager) getRestartedNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.restartedNames...)
}

// newTestWatchdog creates a Watchdog configured for fast testing.
func newTestWatchdog(
	t *testing.T,
	interval int,
	restartDelay int,
	mgr watchdog.CameraManager,
	bus *eventbus.Bus,
	storageDir string,
) *watchdog.Watchdog {
	t.Helper()
	cfg := &config.WatchdogConfig{
		Enabled:        true,
		HealthInterval: interval,
		RestartDelay:   restartDelay,
	}
	storageCfg := &config.StorageConfig{
		HotPath: storageDir,
	}
	return watchdog.New(cfg, storageCfg, mgr, bus, discardLogger())
}

// ─── Disabled watchdog ───────────────────────────────────────────────────────

func TestWatchdog_Disabled_StartReturnsImmediately(t *testing.T) {
	cfg := &config.WatchdogConfig{Enabled: false}
	storageCfg := &config.StorageConfig{HotPath: t.TempDir()}
	bus := eventbus.New(64, discardLogger())
	wd := watchdog.New(cfg, storageCfg, &mockManager{}, bus, discardLogger())

	done := make(chan struct{})
	go func() {
		defer close(done)
		wd.Start()
	}()

	select {
	case <-done:
		// OK — Start() returned immediately when disabled
	case <-time.After(time.Second):
		t.Error("disabled watchdog Start() did not return immediately")
	}
}

// ─── Stop before Start ───────────────────────────────────────────────────────

func TestWatchdog_StopBeforeStart_IsNoop(t *testing.T) {
	cfg := &config.WatchdogConfig{Enabled: true, HealthInterval: 30, RestartDelay: 5}
	storageCfg := &config.StorageConfig{HotPath: t.TempDir()}
	bus := eventbus.New(64, discardLogger())
	wd := watchdog.New(cfg, storageCfg, &mockManager{}, bus, discardLogger())

	// Stop without Start — must not block or panic
	done := make(chan struct{})
	go func() {
		defer close(done)
		wd.Stop()
	}()
	select {
	case <-done:
	case <-time.After(6 * time.Second): // Stop() has a 5s internal timeout
		t.Error("Stop() before Start() blocked for >6s")
	}
}

// ─── System restart event ────────────────────────────────────────────────────

func TestWatchdog_PublishesSystemRestartEvent(t *testing.T) {
	bus := eventbus.New(64, discardLogger())
	ch := bus.Subscribe("system.restart")

	// Use a very long interval so the ticker never fires during this test.
	wd := newTestWatchdog(t, 3600, 5, &mockManager{}, bus, t.TempDir())

	go wd.Start()
	defer wd.Stop()

	select {
	case event := <-ch:
		if event.Type != "system.restart" {
			t.Errorf("event.Type = %q, want %q", event.Type, "system.restart")
		}
	case <-time.After(2 * time.Second):
		t.Error("system.restart event was not published within 2s of Start()")
	}
}

// ─── Camera pipeline health monitoring ──────────────────────────────────────

func TestWatchdog_RestartsCameraAfterSustainedError(t *testing.T) {
	bus := eventbus.New(64, discardLogger())
	mgr := &mockManager{}

	// Camera in StateError — restart_delay is 0 so it triggers on the first check.
	mgr.setCameras([]camera.CameraWithStatus{
		{
			CameraRecord:   camera.CameraRecord{Name: "front-door", Enabled: true},
			PipelineStatus: camera.PipelineStatus{State: camera.StateError, LastError: "ffmpeg exited"},
		},
	})

	// restart_delay=0 means the watchdog restarts on the very first tick after entering error.
	// health_interval=1 so the first tick fires quickly.
	wd := newTestWatchdog(t, 1, 0, mgr, bus, t.TempDir())

	go wd.Start()
	defer wd.Stop()

	// Wait for the watchdog to detect and restart the failing camera
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("watchdog did not restart camera within 5s; restarts: %v", mgr.getRestartedNames())
		case <-time.After(100 * time.Millisecond):
			if names := mgr.getRestartedNames(); len(names) > 0 {
				if names[0] != "front-door" {
					t.Errorf("restarted camera = %q, want %q", names[0], "front-door")
				}
				return // success
			}
		}
	}
}

func TestWatchdog_DoesNotRestartHealthyCameras(t *testing.T) {
	bus := eventbus.New(64, discardLogger())
	mgr := &mockManager{}

	// Camera is healthy (StateRecording)
	mgr.setCameras([]camera.CameraWithStatus{
		{
			CameraRecord:   camera.CameraRecord{Name: "garage", Enabled: true},
			PipelineStatus: camera.PipelineStatus{State: camera.StateRecording},
		},
	})

	wd := newTestWatchdog(t, 1, 0, mgr, bus, t.TempDir())

	go wd.Start()
	// Let two health-check ticks fire
	time.Sleep(2500 * time.Millisecond)
	wd.Stop()

	if names := mgr.getRestartedNames(); len(names) != 0 {
		t.Errorf("healthy camera should not be restarted, got restarts: %v", names)
	}
}

func TestWatchdog_IgnoresDisabledCameras(t *testing.T) {
	bus := eventbus.New(64, discardLogger())
	mgr := &mockManager{}

	// Disabled camera in error state — should NOT be restarted
	mgr.setCameras([]camera.CameraWithStatus{
		{
			CameraRecord:   camera.CameraRecord{Name: "offline-cam", Enabled: false},
			PipelineStatus: camera.PipelineStatus{State: camera.StateError},
		},
	})

	wd := newTestWatchdog(t, 1, 0, mgr, bus, t.TempDir())

	go wd.Start()
	time.Sleep(2500 * time.Millisecond)
	wd.Stop()

	if names := mgr.getRestartedNames(); len(names) != 0 {
		t.Errorf("disabled camera in error should NOT be restarted, got: %v", names)
	}
}

func TestWatchdog_PublishesCameraRestartedEvent(t *testing.T) {
	bus := eventbus.New(64, discardLogger())
	restartCh := bus.Subscribe("camera.restarted")
	mgr := &mockManager{}

	mgr.setCameras([]camera.CameraWithStatus{
		{
			CameraRecord:   camera.CameraRecord{Name: "side-gate", Enabled: true},
			PipelineStatus: camera.PipelineStatus{State: camera.StateError},
		},
	})

	wd := newTestWatchdog(t, 1, 0, mgr, bus, t.TempDir())

	go wd.Start()
	defer wd.Stop()

	select {
	case event := <-restartCh:
		if event.Type != "camera.restarted" {
			t.Errorf("event.Type = %q, want %q", event.Type, "camera.restarted")
		}
		if event.Label != "side-gate" {
			t.Errorf("event.Label = %q, want %q", event.Label, "side-gate")
		}
	case <-time.After(5 * time.Second):
		t.Error("camera.restarted event not published within 5s")
	}
}
