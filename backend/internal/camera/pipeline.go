// This file implements the per-camera pipeline lifecycle (R1, R2, R3, CG1).
// The pipeline monitors go2rtc stream health rather than managing RTSP directly.

package camera

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/go2rtc"
)

// State represents the lifecycle state of a camera pipeline.
type State string

const (
	StateIdle       State = "idle"
	StateConnecting State = "connecting"
	StateStreaming   State = "streaming"
	StateRecording   State = "recording" // Phase 2: set when ffmpeg is actively writing segments
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
//   - Phase 2 will add: ffmpeg subprocess for direct-to-disk recording (CG4, CG9).
//     Each Pipeline will own its own ffmpeg process, crash-isolated per camera.
//     Recording path: {hot_path}/{camera_name}/{YYYY-MM-DD}/{HH}/{MM.SS}.mp4
//     StorageConfig must be threaded through Manager → Pipeline for hot_path.
//     Camera names with spaces need sanitization for filesystem paths.
//   - Phase 5 will add: Sub stream → AI detection pipeline
type Pipeline struct {
	cam    *CameraRecord
	g2r    *go2rtc.Client
	logger *slog.Logger
	stopCh chan struct{}
	stopOnce sync.Once

	mu     sync.RWMutex
	status PipelineStatus
}

// NewPipeline creates a pipeline for a single camera in idle state.
func NewPipeline(cam *CameraRecord, g2r *go2rtc.Client, logger *slog.Logger) *Pipeline {
	return &Pipeline{
		cam:    cam,
		g2r:    g2r,
		logger: logger.With("camera", cam.Name),
		stopCh: make(chan struct{}),
		status: PipelineStatus{State: StateIdle},
	}
}

// Start begins monitoring the camera's go2rtc streams. It blocks until Stop()
// is called. The monitoring loop polls go2rtc every 5 seconds to check whether
// the stream has active producers (i.e. the RTSP source is connected).
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
// by re-registering them.
func (p *Pipeline) checkStreamHealth() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		return
	}

	// Check main stream — guard against nil StreamInfo (go2rtc could return null)
	mainInfo, mainExists := streams[p.cam.Name]
	mainActive := mainExists && mainInfo != nil && len(mainInfo.Producers) > 0

	// Check sub stream
	subName := p.cam.Name + "_sub"
	subInfo, subExists := streams[subName]
	subActive := subExists && subInfo != nil && len(subInfo.Producers) > 0

	// Side effects (logging, go2rtc re-registration) are collected as flags inside the
	// setStatus closure and executed after the lock is released, avoiding I/O under mutex.
	var logStreamActive bool
	var needsResync bool

	p.setStatus(func(s *PipelineStatus) {
		s.MainStream = mainActive
		s.SubStream = subActive

		if mainActive {
			if s.State != StateStreaming {
				logStreamActive = true
			}
			s.State = StateStreaming
			if s.ConnectedAt == nil {
				now := time.Now()
				s.ConnectedAt = &now
			}
			s.LastError = ""
		} else if mainExists && mainInfo != nil {
			// Stream is registered but has no producer — camera disconnected
			s.State = StateConnecting
			s.LastError = "waiting for camera to connect"
			s.ConnectedAt = nil
		} else {
			// Stream not registered in go2rtc — needs re-sync (go2rtc restart recovery)
			needsResync = true
			s.State = StateError
			s.LastError = "stream not registered in go2rtc"
			s.ConnectedAt = nil
		}
	})

	if logStreamActive {
		p.logger.Info("camera stream active")
	}

	// Auto-recovery: re-register streams in go2rtc after a restart.
	// Uses a separate timeout so we don't depend on leftover time from Streams().
	if needsResync {
		p.reregisterStreams()
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
func (p *Pipeline) Stop() {
	p.stopOnce.Do(func() {
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
