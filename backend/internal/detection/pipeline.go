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
	cam                CameraInfo
	streamName         string // go2rtc stream name to grab frames from (e.g. "front_door_sub")
	fallbackStreamName string // R2: main stream fallback when sub-stream fails (e.g. "front_door"); empty = no fallback
	g2r                *go2rtc.Client
	detector           Detector
	snapshotDir        string        // pre-built absolute path: {snapshotPath}/{sanitizedCamName}
	threshold          float64       // minimum confidence to trigger a detection event
	frameInterval      time.Duration // how often to grab and process a frame
	bus                *eventbus.Bus
	logger             *slog.Logger

	// Phase 13: optional face recognition (R11). Nil when face_recognition.enabled=false.
	faceRecognizer FaceRecognizer
	faceRepo       *FaceRepository
	faceThreshold  float64 // cosine similarity threshold for face matching
	maxFaces       int     // max faces to extract per frame

	// faceCache avoids a full table scan on every detection frame by caching
	// the enrolled face list for faceCacheTTL (60s). The cache is single-goroutine
	// (only accessed from the detection goroutine), so no mutex is needed.
	faceCache       []FaceRecord
	faceCacheExpiry time.Time

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
	faceFailCount   int
}

// NewDetectionPipeline creates a DetectionPipeline for a single camera.
// snapshotDir is the camera-specific snapshot directory (e.g. /data/snapshots/front_door);
// the caller (camera/pipeline.go) constructs this path to avoid the detection package
// depending on the camera package's SanitizeName function.
// fallbackStreamName is the go2rtc stream name to try when the primary stream fails (R2);
// pass "" to disable fallback.
func NewDetectionPipeline(
	cam CameraInfo,
	streamName string,
	fallbackStreamName string,
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
		cam:                cam,
		streamName:         streamName,
		fallbackStreamName: fallbackStreamName,
		g2r:                g2r,
		detector:           detector,
		snapshotDir:        snapshotDir,
		threshold:          threshold,
		frameInterval:      frameInterval,
		bus:                bus,
		logger:             logger.With("camera", cam.Name, "component", "detection_pipeline"),
		ctx:                ctx,
		ctxCancel:          ctxCancel,
		stopCh:             make(chan struct{}),
		done:               make(chan struct{}),
	}
}

// SetFaceRecognition configures the optional face recognition pass (Phase 13, R11).
// Must be called before Start(). When set, "person" detections trigger a secondary
// face embedding + matching step against the enrolled faces database.
func (dp *DetectionPipeline) SetFaceRecognition(recognizer FaceRecognizer, repo *FaceRepository, threshold float64, maxFaces int) {
	dp.faceRecognizer = recognizer
	dp.faceRepo = repo
	dp.faceThreshold = threshold
	dp.maxFaces = maxFaces
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
	if err != nil && dp.fallbackStreamName != "" {
		// R2 "Messy Stream Handling": sub-stream frame grab failed, transparently
		// fall back to the main stream. This handles corrupted sub-streams, cameras
		// that don't support sub-streams properly, and transient sub-stream failures.
		jpegBytes, err = dp.g2r.FrameJPEG(ctx, dp.fallbackStreamName)
		if err == nil && dp.frameFailCount > 0 {
			dp.logger.Info("sub-stream unavailable, using main stream fallback (R2)",
				"primary", dp.streamName,
				"fallback", dp.fallbackStreamName,
			)
		}
	}
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

	// Zone filtering (Phase 9, R5): when zones are configured, restrict detections
	// to those whose bbox centre passes the include/exclude polygon rules.
	// If no zones are defined (dp.cam.Zones is empty), all detections pass through.
	above = filterByZones(above, dp.cam.Zones)
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

	// Phase 13 (R11): if face recognition is enabled and any "person" was detected,
	// run a secondary face embedding + matching pass against enrolled faces.
	dp.tryFaceRecognition(ctx, jpegBytes, above, snapshotPath)
}

// tryFaceRecognition runs face recognition when configured and when "person"
// detections are present. Each matched face publishes a "face_match" event.
func (dp *DetectionPipeline) tryFaceRecognition(ctx context.Context, jpegBytes []byte, detections []DetectedObject, snapshotPath string) {
	if dp.faceRecognizer == nil || dp.faceRepo == nil {
		return
	}

	// Check if any detection is a "person" — face recognition only makes sense on people.
	hasPerson := false
	for _, d := range detections {
		if d.Label == "person" {
			hasPerson = true
			break
		}
	}
	if !hasPerson {
		return
	}

	embeddings, err := dp.faceRecognizer.EmbedFaces(ctx, jpegBytes, dp.maxFaces)
	if err != nil {
		dp.faceFailCount++
		if dp.faceFailCount == 1 || dp.faceFailCount%12 == 0 {
			dp.logger.Warn("face embedding failed",
				"error", err,
				"consecutive_failures", dp.faceFailCount,
			)
		}
		return
	}
	dp.faceFailCount = 0

	if len(embeddings) == 0 {
		return
	}

	// Fetch enrolled faces from cache or DB. The cache TTL (60s) amortises the full
	// table scan across all detection frames while keeping the match list fresh enough
	// for newly-enrolled faces. The cache is only accessed from the detection goroutine,
	// so no mutex is needed.
	if time.Now().After(dp.faceCacheExpiry) {
		fresh, err := dp.faceRepo.ListWithEmbeddings(ctx)
		if err != nil {
			dp.logger.Warn("face matching query failed", "error", err)
			return
		}
		dp.faceCache = fresh
		dp.faceCacheExpiry = time.Now().Add(60 * time.Second)
	}
	enrolledFaces := dp.faceCache
	if len(enrolledFaces) == 0 {
		return
	}

	for _, fe := range embeddings {
		face, similarity := matchBestFace(fe.Embedding, enrolledFaces, dp.faceThreshold)
		if face == nil {
			continue // no match above threshold
		}

		dp.bus.Publish(eventbus.Event{
			Type:       "face_match",
			CameraID:   dp.cam.ID,
			Label:      face.Name,
			Confidence: similarity,
			Thumbnail:  snapshotPath,
			Data: map[string]any{
				"face_id":    face.ID,
				"face_name":  face.Name,
				"similarity": similarity,
				"bbox":       fe.BBox,
			},
		})

		dp.logger.Debug("face match published",
			"face_id", face.ID,
			"face_name", face.Name,
			"similarity", fmt.Sprintf("%.3f", similarity),
		)
	}
}

// matchBestFace finds the enrolled face with the highest cosine similarity to the
// given embedding that exceeds the threshold. Returns (nil, 0) if no match.
func matchBestFace(embedding []float32, faces []FaceRecord, threshold float64) (*FaceRecord, float64) {
	var bestFace *FaceRecord
	bestSim := 0.0
	for i := range faces {
		sim := cosineSimilarity(embedding, faces[i].Embedding)
		if sim > bestSim {
			bestSim = sim
			bestFace = &faces[i]
		}
	}
	if bestFace == nil || bestSim < threshold {
		return nil, bestSim
	}
	return bestFace, bestSim
}

// filterByZones applies zone inclusion/exclusion rules to a slice of detections.
// Include zones restrict detections to those whose bbox centre lies inside at
// least one include polygon. Exclude zones suppress detections whose bbox centre
// lies inside any exclude polygon. When no zones are defined, all detections pass
// through unchanged.
func filterByZones(detections []DetectedObject, zones []Zone) []DetectedObject {
	if len(zones) == 0 {
		return detections
	}
	var includes, excludes []Zone
	for _, z := range zones {
		switch z.Type {
		case ZoneInclude:
			includes = append(includes, z)
		case ZoneExclude:
			excludes = append(excludes, z)
		}
	}
	result := detections[:0:0] // fresh slice, same type, no aliasing
	for _, d := range detections {
		cx := (d.BBox.XMin + d.BBox.XMax) / 2
		cy := (d.BBox.YMin + d.BBox.YMax) / 2
		// Include rule: if any include zones exist, bbox centre must be inside at least one.
		if len(includes) > 0 {
			inAny := false
			for _, z := range includes {
				if pointInPolygon(cx, cy, z.Points) {
					inAny = true
					break
				}
			}
			if !inAny {
				continue
			}
		}
		// Exclude rule: bbox centre must not be inside any exclude zone.
		excluded := false
		for _, z := range excludes {
			if pointInPolygon(cx, cy, z.Points) {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, d)
		}
	}
	return result
}

// pointInPolygon reports whether point (x, y) lies inside the polygon defined by
// points using the ray-casting algorithm. Coordinates are normalised to [0.0, 1.0].
// Polygons with fewer than 3 vertices always return false.
func pointInPolygon(x, y float64, points []ZonePoint) bool {
	n := len(points)
	if n < 3 {
		return false
	}
	inside := false
	j := n - 1
	for i := 0; i < n; i++ {
		xi, yi := points[i].X, points[i].Y
		xj, yj := points[j].X, points[j].Y
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
		j = i
	}
	return inside
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
