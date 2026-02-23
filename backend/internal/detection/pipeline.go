package detection

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/go2rtc"
)

// DetectionPipeline runs a periodic frame-grab and inference loop for a single camera.
// It fetches JPEG snapshots from go2rtc, runs inference via a Detector, and publishes
// detection events to the event bus (R3, CG10).
//
// Lifecycle:
//   - Start() spawns a goroutine and returns immediately (non-blocking). Safe to call
//     multiple times — subsequent calls while the goroutine is running are no-ops.
//   - Stop() cancels in-flight HTTP calls, signals the goroutine to exit, and blocks
//     until it does. Safe to call before Start() — no deadlock.
//   - IsActive() reports whether the goroutine is running.
type DetectionPipeline struct {
	cam           CameraInfo
	streamName    string        // go2rtc stream name to grab frames from
	g2r           *go2rtc.Client
	detector      Detector
	snapshotDir   string        // pre-built absolute path: {snapshotPath}/{sanitizedCamName}
	threshold     float64       // minimum confidence to trigger a detection event
	frameInterval time.Duration // how often to grab and process a frame
	bus           *eventbus.Bus
	logger        *slog.Logger

	// ctx is cancelled by Stop() to immediately unblock any in-flight HTTP calls
	// (FrameJPEG to go2rtc, Detect to the remote backend).
	ctx       context.Context
	ctxCancel context.CancelFunc

	stopCh   chan struct{}
	stopOnce sync.Once
	done     chan struct{} // closed when the goroutine exits

	// started tracks whether Start() was ever called; used to guard <-dp.done in Stop()
	// so Stop()-before-Start() does not deadlock. Set with CompareAndSwap before goroutine
	// spawn to prevent double-Start races from the camera pipeline health-check loop.
	started atomic.Bool
	active  atomic.Bool // true while goroutine is running

	// Separate fail counters for frame-grab and inference failures so each counter
	// resets independently and log throttle fires at the correct cadence per failure type.
	frameFailCount  int
	detectFailCount int
}

// NewDetectionPipeline creates a DetectionPipeline for a single camera.
// snapshotDir is the camera-specific snapshot directory (e.g. /data/snapshots/front_door);
// the caller (camera/pipeline.go) constructs this path to avoid the detection package
// depending on the camera package's SanitizeName function.
func NewDetectionPipeline(
	cam CameraInfo,
	streamName string,
	g2r *go2rtc.Client,
	detector Detector,
	snapshotDir string,
	threshold float64,
	frameInterval time.Duration,
	bus *eventbus.Bus,
	logger *slog.Logger,
) *DetectionPipeline {
	// Guard against a zero or negative interval to prevent ticker(0) panic.
	// The config validator enforces >= 1s in production; this protects test callers
	// that construct a pipeline directly without going through config.
	if frameInterval < time.Second {
		frameInterval = time.Second
	}
	ctx, ctxCancel := context.WithCancel(context.Background())
	return &DetectionPipeline{
		cam:           cam,
		streamName:    streamName,
		g2r:           g2r,
		detector:      detector,
		snapshotDir:   snapshotDir,
		threshold:     threshold,
		frameInterval: frameInterval,
		bus:           bus,
		logger:        logger.With("camera", cam.Name, "component", "detection_pipeline"),
		ctx:           ctx,
		ctxCancel:     ctxCancel,
		stopCh:        make(chan struct{}),
		done:          make(chan struct{}),
	}
}

// Start spawns the detection goroutine. Safe to call multiple times — if the
// goroutine is already running, subsequent calls are a no-op (no panic, no second goroutine).
func (dp *DetectionPipeline) Start() {
	if !dp.started.CompareAndSwap(false, true) {
		return // already started; prevent double-goroutine race from health-check loop
	}

	// Create the snapshot directory once at start time rather than on every frame.
	// Misconfiguration is logged here so operators see it immediately, not buried in
	// per-frame "failed to save snapshot" warnings.
	if err := os.MkdirAll(dp.snapshotDir, 0755); err != nil {
		dp.logger.Warn("failed to create snapshot directory; detection snapshots will be skipped",
			"dir", dp.snapshotDir, "error", err)
	}

	// Set active synchronously before spawning the goroutine so IsActive() returns
	// true immediately after Start() returns, eliminating the TOCTOU window that
	// existed when active was set inside the goroutine.
	dp.active.Store(true)
	go dp.run()
}

// IsActive reports whether the detection goroutine is currently running.
func (dp *DetectionPipeline) IsActive() bool {
	return dp.active.Load()
}

// Stop signals the detection goroutine to exit and blocks until it does.
// Safe to call multiple times, and safe to call before Start().
// ctxCancel is called first to immediately unblock any in-flight HTTP calls, so
// Stop returns quickly even if a frame is currently being processed.
func (dp *DetectionPipeline) Stop() {
	dp.ctxCancel()                                  // unblock in-flight FrameJPEG / Detect calls
	dp.stopOnce.Do(func() { close(dp.stopCh) })
	if dp.started.Load() {
		<-dp.done // wait only if Start() was ever called; avoids deadlock if Stop-before-Start
	}
}

func (dp *DetectionPipeline) run() {
	defer func() {
		dp.active.Store(false)
		close(dp.done)
	}()

	dp.logger.Info("detection pipeline started",
		"stream", dp.streamName,
		"interval", dp.frameInterval,
		"threshold", dp.threshold,
	)

	ticker := time.NewTicker(dp.frameInterval)
	defer ticker.Stop()

	// Perform an immediate check before the first tick so detections start
	// without waiting a full interval after stream activation.
	dp.processFrame()

	for {
		select {
		case <-dp.stopCh:
			dp.logger.Info("detection pipeline stopped")
			return
		case <-ticker.C:
			dp.processFrame()
		}
	}
}

// processFrame grabs one JPEG from go2rtc, runs inference, and publishes a
// detection event if any objects meet the confidence threshold.
// Frame-grab and inference failures are throttled independently: logged on the
// first failure, then every 12th (≈1 min at a 5-second interval).
func (dp *DetectionPipeline) processFrame() {
	// Derive from dp.ctx so Stop() immediately cancels in-flight HTTP calls,
	// allowing the goroutine to exit without waiting for the full timeout.
	ctx, cancel := context.WithTimeout(dp.ctx, 10*time.Second)
	defer cancel()

	jpegBytes, err := dp.g2r.FrameJPEG(ctx, dp.streamName)
	if err != nil {
		dp.frameFailCount++
		if dp.frameFailCount == 1 || dp.frameFailCount%12 == 0 {
			dp.logger.Warn("failed to grab frame from go2rtc",
				"stream", dp.streamName,
				"error", err,
				"consecutive_failures", dp.frameFailCount,
			)
		}
		return
	}
	if len(jpegBytes) == 0 {
		dp.logger.Warn("received empty JPEG from go2rtc; skipping inference",
			"stream", dp.streamName)
		return
	}
	dp.frameFailCount = 0

	detections, err := dp.detector.Detect(ctx, jpegBytes)
	if err != nil {
		dp.detectFailCount++
		if dp.detectFailCount == 1 || dp.detectFailCount%12 == 0 {
			dp.logger.Warn("detection inference failed",
				"error", err,
				"consecutive_failures", dp.detectFailCount,
			)
		}
		return
	}
	dp.detectFailCount = 0

	// Filter by confidence threshold — use a fresh slice to avoid in-place aliasing
	// of the detections backing array, which would corrupt results if the Detector
	// ever returns a slice backed by shared or pooled memory.
	above := make([]DetectedObject, 0, len(detections))
	for _, d := range detections {
		if d.Confidence >= dp.threshold {
			above = append(above, d)
		}
	}
	if len(above) == 0 {
		return
	}

	// Save snapshot. If saving fails, still publish the event without a thumbnail
	// rather than silently dropping the detection.
	snapshotPath, saveErr := dp.saveSnapshot(jpegBytes)
	if saveErr != nil {
		dp.logger.Warn("failed to save detection snapshot", "error", saveErr)
		snapshotPath = ""
	}

	// Use highest-confidence detection for the event's primary label/confidence.
	best := above[0]
	for _, d := range above[1:] {
		if d.Confidence > best.Confidence {
			best = d
		}
	}

	dp.bus.Publish(eventbus.Event{
		Type:       "detection",
		CameraID:   dp.cam.ID,
		Label:      best.Label,
		Confidence: best.Confidence,
		Thumbnail:  snapshotPath,
		Data:       above, // all detections stored as JSON array in data column
	})

	dp.logger.Debug("detection event published",
		"label", best.Label,
		"confidence", best.Confidence,
		"detections", len(above),
		"snapshot", snapshotPath,
	)
}

// saveSnapshot writes the JPEG bytes to the camera's snapshot directory.
// The snapshot directory is created by Start() — if it doesn't exist here,
// a warning was already logged at Start() and the error is surfaced here so
// detection events are still published without a thumbnail.
// The returned path uses forward slashes regardless of OS so that paths stored
// in SQLite are consistent across platforms.
func (dp *DetectionPipeline) saveSnapshot(jpegBytes []byte) (string, error) {
	// Use UTC timestamp to avoid ambiguity around DST transitions, and forward-slash
	// normalization so the stored path is valid on both Linux and Windows.
	filename := time.Now().UTC().Format("20060102_150405.000") + ".jpg"
	absPath := filepath.Join(dp.snapshotDir, filename)
	if err := os.WriteFile(absPath, jpegBytes, 0644); err != nil {
		return "", fmt.Errorf("writing snapshot %q: %w", absPath, err)
	}
	// Normalize to forward slashes before storing in SQLite so the path is
	// consistent across platforms (Linux Docker and Windows dev environments).
	return filepath.ToSlash(absPath), nil
}
