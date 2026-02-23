// This file implements the per-camera pipeline lifecycle (R1, R2, R3, CG1, CG4).
// The pipeline monitors go2rtc stream health and manages ffmpeg recording.

package camera

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/go2rtc"
)

// State represents the lifecycle state of a camera pipeline.
type State string

const (
	StateIdle       State = "idle"
	StateConnecting State = "connecting"
	StateStreaming   State = "streaming"
	StateRecording   State = "recording" // set when ffmpeg is actively writing segments
	StateError       State = "error"
	StateStopped     State = "stopped"
)

// PipelineStatus is a snapshot of a camera pipeline's current state.
type PipelineStatus struct {
	State       State      `json:"state"`
	MainStream  bool       `json:"main_stream_active"`
	SubStream   bool       `json:"sub_stream_active"`
	Recording   bool       `json:"recording"`
	Detecting   bool       `json:"detecting"`
	LastError   string     `json:"last_error,omitempty"`
	ConnectedAt *time.Time `json:"connected_at,omitempty"` // pointer so unset serializes as null, not "0001-01-01"
}

// Pipeline manages the full lifecycle of a single camera:
//   - Monitors go2rtc stream health (producers present = stream active)
//   - Manages ffmpeg recording subprocess when stream is active and cam.Record=true (CG4, CG9)
//   - Manages AI detection pipeline when stream is active and cam.Detect=true (R3, Phase 5)
type Pipeline struct {
	cam         *CameraRecord
	g2r         *go2rtc.Client
	bus         *eventbus.Bus
	recorder    *Recorder                   // nil if cam.Record is false
	detPipeline *detection.DetectionPipeline // nil if cam.Detect is false or detection disabled
	logger      *slog.Logger
	ctx         context.Context    // cancelled by Stop() to unblock in-flight go2rtc calls
	ctxCancel   context.CancelFunc
	stopCh      chan struct{}
	stopOnce    sync.Once
	startDone   chan struct{} // closed when Start() returns; used to wait for full pipeline exit

	// recordingStartFailed suppresses repeated error logs after first recorder.Start() failure.
	// Only accessed from the Start() goroutine's health check loop; no mutex needed.
	// recordingFailCount tracks consecutive failures; a periodic log fires every 12 ticks (~1 min)
	// so operators see ongoing recorder errors (e.g. ffmpeg not installed) — not just the first one.
	recordingStartFailed bool
	recordingFailCount   int

	// detStartFailed / detFailCount serve the same throttle purpose for detection.
	detStartFailed bool
	detFailCount   int

	mu     sync.RWMutex
	status PipelineStatus
}

// NewPipeline creates a pipeline for a single camera in idle state.
// If the camera has recording enabled, a Recorder is created (but not started).
// If the camera has detection enabled and a Detector is provided, a DetectionPipeline
// is created (but not started) — it will be started by checkStreamHealth when
// the stream becomes active.
func NewPipeline(
	cam *CameraRecord,
	g2r *go2rtc.Client,
	rtspBase string,
	hotPath string,
	segmentDuration int,
	recRepo *recording.Repository,
	detector detection.Detector, // nil if detection is disabled globally
	detCfg config.DetectionConfig,
	bus *eventbus.Bus,
	logger *slog.Logger,
) *Pipeline {
	ctx, ctxCancel := context.WithCancel(context.Background())
	p := &Pipeline{
		cam:       cam,
		g2r:       g2r,
		bus:       bus,
		logger:    logger.With("camera", cam.Name),
		ctx:       ctx,
		ctxCancel: ctxCancel,
		stopCh:    make(chan struct{}),
		startDone: make(chan struct{}),
		status:    PipelineStatus{State: StateIdle},
	}

	if cam.Record {
		p.recorder = NewRecorder(cam, rtspBase, hotPath, segmentDuration, recRepo, bus, logger)
	}

	// Create detection pipeline when the camera has detect=true AND a global detector
	// has been configured. The sub stream is preferred (lower resolution → faster inference);
	// falls back to the main stream when no sub stream is configured.
	if cam.Detect && detector != nil {
		streamName := cam.Name
		if cam.SubStream != "" {
			streamName = cam.Name + "_sub"
		}
		snapshotDir := filepath.Join(detCfg.SnapshotPath, SanitizeName(cam.Name))
		p.detPipeline = detection.NewDetectionPipeline(
			detection.CameraInfo{ID: cam.ID, Name: cam.Name},
			streamName,
			g2r,
			detector,
			snapshotDir,
			detCfg.ConfidenceThresholdValue(),
			time.Duration(detCfg.FrameInterval)*time.Second,
			bus,
			logger,
		)
	}

	return p
}

// Start begins monitoring the camera's go2rtc streams. It blocks until Stop()
// is called. The monitoring loop polls go2rtc every 5 seconds to check whether
// the stream has active producers (i.e. the RTSP source is connected).
// When the stream is active and recording is enabled, ffmpeg is started.
func (p *Pipeline) Start() {
	defer close(p.startDone)
	p.setStatus(func(s *PipelineStatus) {
		s.State = StateConnecting
	})

	p.logger.Info("monitoring camera stream via go2rtc", "name", p.cam.Name)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Do an immediate check before waiting for the first tick
	p.checkStreamHealth()

	for {
		select {
		case <-ticker.C:
			p.checkStreamHealth()
		case <-p.stopCh:
			// Stop recording and detection before setting final state.
			// Detection Stop() blocks until the goroutine exits (bounded by 10s ctx
			// timeout on the in-flight processFrame call) — acceptable for shutdown.
			if p.recorder != nil && p.recorder.IsActive() {
				p.recorder.Stop()
			}
			if p.detPipeline != nil && p.detPipeline.IsActive() {
				p.detPipeline.Stop()
			}
			p.setStatus(func(s *PipelineStatus) {
				s.State = StateStopped
				s.MainStream = false
				s.SubStream = false
				s.Recording = false
				s.Detecting = false
			})
			return
		}
	}
}

// checkStreamHealth queries go2rtc for this camera's stream status.
// If streams are missing (e.g. after a go2rtc restart), it auto-recovers
// by re-registering them. Starts/stops recording based on stream health.
func (p *Pipeline) checkStreamHealth() {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	streams, err := p.g2r.Streams(ctx)
	if err != nil {
		p.setStatus(func(s *PipelineStatus) {
			s.State = StateError
			s.LastError = fmt.Sprintf("go2rtc unreachable: %v", err)
			s.MainStream = false
			s.SubStream = false
		})
		p.logger.Warn("go2rtc unreachable", "error", err)

		// Stop recording and detection if go2rtc is unreachable.
		if p.recorder != nil && p.recorder.IsActive() {
			p.recorder.Stop()
			p.setStatus(func(s *PipelineStatus) {
				s.Recording = false
			})
		}
		if p.detPipeline != nil && p.detPipeline.IsActive() {
			p.detPipeline.Stop()
			p.detStartFailed = false
			p.detFailCount = 0
			p.setStatus(func(s *PipelineStatus) {
				s.Detecting = false
			})
		}
		return
	}

	// Check main stream — guard against nil StreamInfo (go2rtc could return null)
	mainInfo, mainExists := streams[p.cam.Name]
	mainActive := mainExists && mainInfo != nil && len(mainInfo.Producers) > 0

	// Check sub stream
	subName := p.cam.Name + "_sub"
	subInfo, subExists := streams[subName]
	subActive := subExists && subInfo != nil && len(subInfo.Producers) > 0

	// Side effects are collected as flags inside the closure and executed after
	// the lock is released, avoiding I/O under mutex.
	var logStreamActive bool
	var needsResync bool
	var needsStartRecording bool
	var needsStopRecording bool
	var needsStartDetection bool
	var needsStopDetection bool
	var publishConnected bool
	var publishDisconnected bool

	recorderActive := p.recorder != nil && p.recorder.IsActive()

	p.setStatus(func(s *PipelineStatus) {
		s.MainStream = mainActive
		s.SubStream = subActive

		if mainActive {
			if s.State != StateStreaming && s.State != StateRecording {
				logStreamActive = true
				publishConnected = true // first tick the stream becomes active (CG8)
			}
			if s.ConnectedAt == nil {
				now := time.Now()
				s.ConnectedAt = &now
			}
			s.LastError = ""

			// Decide recording state
			if p.recorder != nil && recorderActive {
				s.State = StateRecording
				s.Recording = true
			} else if p.recorder != nil && !recorderActive {
				// Recorder exists but not active — need to start it
				needsStartRecording = true
				s.State = StateStreaming // will transition to Recording after start
			} else {
				s.State = StateStreaming
			}

			// Decide detection state (independent of recording state).
			// Use s.Detecting (the authoritative status field, under the mutex) rather than
			// IsActive() (which has a TOCTOU window between Start() return and goroutine
			// scheduling). Only start detection if the stream the pipeline actually reads
			// from is active — when a sub-stream is configured, detection runs off the
			// sub-stream; starting when only the main is active would cause persistent
			// FrameJPEG errors against an offline sub-stream.
			detStreamActive := mainActive
			if p.cam.SubStream != "" {
				detStreamActive = subActive
			}
			if p.detPipeline != nil && !s.Detecting && detStreamActive {
				needsStartDetection = true
			}
		} else if mainExists && mainInfo != nil {
			// Stream is registered but has no producer — camera disconnected
			if s.State == StateStreaming || s.State == StateRecording {
				publishDisconnected = true // stream was active, now lost (CG8)
			}
			s.State = StateConnecting
			s.LastError = "waiting for camera to connect"
			s.ConnectedAt = nil
			if recorderActive {
				needsStopRecording = true
				s.Recording = false
			}
			if s.Detecting {
				needsStopDetection = true
				s.Detecting = false
			}
		} else {
			// Stream not registered in go2rtc — needs re-sync (go2rtc restart recovery)
			if s.State == StateStreaming || s.State == StateRecording {
				publishDisconnected = true // stream was active, now unregistered (CG8)
			}
			needsResync = true
			s.State = StateError
			s.LastError = "stream not registered in go2rtc"
			s.ConnectedAt = nil
			if recorderActive {
				needsStopRecording = true
				s.Recording = false
			}
			if s.Detecting {
				needsStopDetection = true
				s.Detecting = false
			}
		}
	})

	if logStreamActive {
		p.logger.Info("camera stream active")
	}

	// Publish state transition events to the bus (CG8) for Phase 3 SSE and Phase 6 timeline.
	if publishConnected {
		p.bus.Publish(eventbus.Event{
			Type:     "camera.connected",
			CameraID: p.cam.ID,
			Label:    p.cam.Name,
		})
	}
	if publishDisconnected {
		p.bus.Publish(eventbus.Event{
			Type:     "camera.disconnected",
			CameraID: p.cam.ID,
			Label:    p.cam.Name,
		})
	}

	// Stop recording and detection first if needed (before re-sync or other actions).
	if needsStopRecording && p.recorder != nil {
		p.recorder.Stop()
		p.recordingStartFailed = false // reset so the next start attempt is logged fresh
		p.recordingFailCount = 0
		p.logger.Info("recording stopped (stream lost)")
	}
	if needsStopDetection && p.detPipeline != nil {
		p.detPipeline.Stop()
		p.detStartFailed = false
		p.detFailCount = 0
		p.logger.Info("detection stopped (stream lost)")
	}

	// Auto-recovery: re-register streams in go2rtc after a restart.
	if needsResync {
		p.reregisterStreams()
	}

	// Start recording if stream is active and recorder is not running.
	// ensureDirectories() is called inside Recorder.Start() — no pre-call needed.
	if needsStartRecording && p.recorder != nil {
		if err := p.recorder.Start(); err != nil {
			p.recordingFailCount++
			// Log on first failure, then every 12 ticks (~1 min at 5s interval), so operators
			// see persistent failures (e.g. ffmpeg not installed) without log flooding.
			if !p.recordingStartFailed || p.recordingFailCount%12 == 0 {
				p.logger.Error("failed to start recording",
					"error", err, "consecutive_failures", p.recordingFailCount)
				p.recordingStartFailed = true
			}
			p.setStatus(func(s *PipelineStatus) {
				// Set state to Error so the API reflects an inconsistent condition
				// rather than showing state=streaming with a non-empty last_error.
				// The next health tick will retry recording start, resetting state
				// to Recording if it succeeds.
				s.State = StateError
				s.LastError = fmt.Sprintf("recording failed: %v", err)
			})
		} else {
			p.recordingStartFailed = false
			p.recordingFailCount = 0
			p.setStatus(func(s *PipelineStatus) {
				s.State = StateRecording
				s.Recording = true
			})
			p.logger.Info("recording started")
		}
	}

	// Start detection pipeline if stream is active and detector is not running.
	// Unlike Recorder.Start() (which launches ffmpeg), DetectionPipeline.Start()
	// is pure Go and never returns an error — retry throttling is inside the pipeline
	// itself (processFrame failCount). We still track detStartFailed for consistency
	// if the pattern changes, but it is not currently set.
	if needsStartDetection && p.detPipeline != nil {
		p.detPipeline.Start()
		p.detStartFailed = false
		p.detFailCount = 0
		p.setStatus(func(s *PipelineStatus) {
			s.Detecting = true
		})
		p.logger.Info("detection started")
	}

	// Periodically ensure next hour's directory exists (runs every health check = 5s, cheap no-op)
	if p.recorder != nil && p.recorder.IsActive() {
		_ = p.recorder.ensureDirectories()
	}
}

// reregisterStreams re-registers this camera's streams in go2rtc.
// Called automatically when the pipeline detects its streams are missing,
// which happens after a go2rtc restart. Logs with redacted URLs for security.
//
// Note: go2rtc AddStream may return HTTP 400 when the config is mounted :ro,
// but the stream IS registered in memory despite the error. The next health
// poll will detect the stream and transition state to streaming/connecting.
// Early return on main stream failure skips sub stream registration since
// sub stream is useless if the main stream can't be registered.
func (p *Pipeline) reregisterStreams() {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	if err := p.g2r.AddStream(ctx, p.cam.Name, p.cam.MainStream); err != nil {
		p.logger.Warn("failed to re-register main stream in go2rtc",
			"error", err,
			"stream", RedactStreamURL(p.cam.MainStream),
		)
		return // skip sub stream if main stream registration failed
	}
	if p.cam.SubStream != "" {
		if err := p.g2r.AddStream(ctx, p.cam.Name+"_sub", p.cam.SubStream); err != nil {
			p.logger.Warn("failed to re-register sub stream in go2rtc",
				"error", err,
				"stream", RedactStreamURL(p.cam.SubStream),
			)
			p.setStatus(func(s *PipelineStatus) {
				s.LastError = fmt.Sprintf("sub stream registration failed: %v", err)
			})
			return
		}
	}
	p.logger.Info("re-registered streams in go2rtc (auto-recovery)")
}

// Stop signals the pipeline to shut down. Safe to call multiple times.
// ctxCancel is called first to immediately unblock any in-flight go2rtc calls.
func (p *Pipeline) Stop() {
	p.stopOnce.Do(func() {
		p.ctxCancel()  // unblock any in-flight go2rtc HTTP calls immediately
		close(p.stopCh)
		p.logger.Info("camera pipeline stopped")
	})
}

// Status returns a snapshot of the current pipeline state.
func (p *Pipeline) Status() PipelineStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
}

// setStatus applies a mutation to the pipeline status under the write lock.
// The callback pattern allows callers to set flags for side effects (logging,
// network calls) that execute after the lock is released.
func (p *Pipeline) setStatus(fn func(s *PipelineStatus)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(&p.status)
}
