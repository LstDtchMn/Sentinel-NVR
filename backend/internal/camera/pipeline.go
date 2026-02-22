// This file implements the per-camera pipeline lifecycle (R1, R2, R3, CG1, CG4).
// The pipeline monitors go2rtc stream health and manages ffmpeg recording.

package camera

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

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
//   - Phase 5 will add: Sub stream → AI detection pipeline
type Pipeline struct {
	cam       *CameraRecord
	g2r       *go2rtc.Client
	recorder  *Recorder // nil if cam.Record is false
	logger    *slog.Logger
	ctx       context.Context    // cancelled by Stop() to unblock in-flight go2rtc calls
	ctxCancel context.CancelFunc
	stopCh    chan struct{}
	stopOnce  sync.Once

	recordingStartFailed bool // suppresses repeated error logs after first recorder.Start() failure

	mu     sync.RWMutex
	status PipelineStatus
}

// NewPipeline creates a pipeline for a single camera in idle state.
// If the camera has recording enabled, a Recorder is created (but not started).
func NewPipeline(
	cam *CameraRecord,
	g2r *go2rtc.Client,
	rtspBase string,
	hotPath string,
	segmentDuration int,
	recRepo *recording.Repository,
	bus *eventbus.Bus,
	logger *slog.Logger,
) *Pipeline {
	ctx, ctxCancel := context.WithCancel(context.Background())
	p := &Pipeline{
		cam:       cam,
		g2r:       g2r,
		logger:    logger.With("camera", cam.Name),
		ctx:       ctx,
		ctxCancel: ctxCancel,
		stopCh:    make(chan struct{}),
		status:    PipelineStatus{State: StateIdle},
	}

	if cam.Record {
		p.recorder = NewRecorder(cam, rtspBase, hotPath, segmentDuration, recRepo, bus, logger)
	}

	return p
}

// Start begins monitoring the camera's go2rtc streams. It blocks until Stop()
// is called. The monitoring loop polls go2rtc every 5 seconds to check whether
// the stream has active producers (i.e. the RTSP source is connected).
// When the stream is active and recording is enabled, ffmpeg is started.
func (p *Pipeline) Start() {
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
			// Stop recording before setting final state
			if p.recorder != nil && p.recorder.IsActive() {
				p.recorder.Stop()
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

		// Stop recording if go2rtc is unreachable
		if p.recorder != nil && p.recorder.IsActive() {
			p.recorder.Stop()
			p.setStatus(func(s *PipelineStatus) {
				s.Recording = false
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

	recorderActive := p.recorder != nil && p.recorder.IsActive()

	p.setStatus(func(s *PipelineStatus) {
		s.MainStream = mainActive
		s.SubStream = subActive

		if mainActive {
			if s.State != StateStreaming && s.State != StateRecording {
				logStreamActive = true
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
		} else if mainExists && mainInfo != nil {
			// Stream is registered but has no producer — camera disconnected
			s.State = StateConnecting
			s.LastError = "waiting for camera to connect"
			s.ConnectedAt = nil
			if recorderActive {
				needsStopRecording = true
				s.Recording = false
			}
		} else {
			// Stream not registered in go2rtc — needs re-sync (go2rtc restart recovery)
			needsResync = true
			s.State = StateError
			s.LastError = "stream not registered in go2rtc"
			s.ConnectedAt = nil
			if recorderActive {
				needsStopRecording = true
				s.Recording = false
			}
		}
	})

	if logStreamActive {
		p.logger.Info("camera stream active")
	}

	// Stop recording first if needed (before re-sync or other actions)
	if needsStopRecording && p.recorder != nil {
		p.recorder.Stop()
		p.recordingStartFailed = false // reset so the next start attempt is logged fresh
		p.logger.Info("recording stopped (stream lost)")
	}

	// Auto-recovery: re-register streams in go2rtc after a restart.
	if needsResync {
		p.reregisterStreams()
	}

	// Start recording if stream is active and recorder is not running.
	// ensureDirectories() is called inside Recorder.Start() — no pre-call needed.
	if needsStartRecording && p.recorder != nil {
		if err := p.recorder.Start(); err != nil {
			// Log only on the first consecutive failure to avoid log spam.
			if !p.recordingStartFailed {
				p.logger.Error("failed to start recording", "error", err)
				p.recordingStartFailed = true
			}
			p.setStatus(func(s *PipelineStatus) {
				s.LastError = fmt.Sprintf("recording failed: %v", err)
			})
		} else {
			p.recordingStartFailed = false
			p.setStatus(func(s *PipelineStatus) {
				s.State = StateRecording
				s.Recording = true
			})
			p.logger.Info("recording started")
		}
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
