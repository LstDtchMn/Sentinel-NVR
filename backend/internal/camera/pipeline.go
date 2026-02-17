package camera

import (
	"log/slog"
	"sync"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

// State represents the lifecycle state of a camera pipeline.
type State string

const (
	StateIdle         State = "idle"
	StateConnecting   State = "connecting"
	StateStreaming     State = "streaming"
	StateRecording    State = "recording"
	StateError        State = "error"
	StateStopped      State = "stopped"
)

type PipelineStatus struct {
	State       State     `json:"state"`
	MainStream  bool      `json:"main_stream_active"`
	SubStream   bool      `json:"sub_stream_active"`
	Recording   bool      `json:"recording"`
	Detecting   bool      `json:"detecting"`
	LastError   string    `json:"last_error,omitempty"`
	ConnectedAt time.Time `json:"connected_at,omitempty"`
}

// Pipeline manages the full lifecycle of a single camera:
//   - Main stream → Direct-to-Disk recording (no decode)
//   - Sub stream  → Decode for AI detection
//   - Fallback:     If sub-stream fails, decode main stream via HW accel
type Pipeline struct {
	cam    *config.CameraConfig
	logger *slog.Logger
	stopCh chan struct{}

	mu     sync.RWMutex
	status PipelineStatus
}

func NewPipeline(cam *config.CameraConfig, logger *slog.Logger) *Pipeline {
	return &Pipeline{
		cam:    cam,
		logger: logger.With("camera", cam.Name),
		stopCh: make(chan struct{}),
		status: PipelineStatus{State: StateIdle},
	}
}

// Start connects to the camera streams and begins processing.
// This is a stub — the actual FFmpeg subprocess pipeline will be wired in Phase 1b.
func (p *Pipeline) Start() {
	p.setStatus(func(s *PipelineStatus) {
		s.State = StateConnecting
	})

	p.logger.Info("connecting to camera",
		"main_stream", p.cam.MainStream,
		"sub_stream", p.cam.SubStream,
	)

	// TODO: Phase 1b — Launch FFmpeg subprocess for main stream (remux, no decode)
	//   ffmpeg -rtsp_transport tcp -i <main_stream> -c copy -f segment \
	//     -segment_time <segment_duration> -segment_format mp4 <output_pattern>

	// TODO: Phase 1b — Launch FFmpeg subprocess for sub stream (decode for detection)
	//   ffmpeg -rtsp_transport tcp -i <sub_stream> -vf fps=5 -f rawvideo pipe:1

	// TODO: Phase 1b — Implement "Messy Stream Handling" (R2):
	//   If sub_stream is empty or fails, fall back to decoding main stream with HW accel:
	//   ffmpeg -hwaccel auto -i <main_stream> -vf "scale=640:360,fps=5" -f rawvideo pipe:1

	// Simulate connection for now
	p.setStatus(func(s *PipelineStatus) {
		s.State = StateStreaming
		s.MainStream = true
		s.SubStream = p.cam.SubStream != ""
		s.ConnectedAt = time.Now()
	})

	p.logger.Info("camera pipeline running (stub mode)")

	// Block until stopped
	<-p.stopCh

	p.setStatus(func(s *PipelineStatus) {
		s.State = StateStopped
		s.MainStream = false
		s.SubStream = false
		s.Recording = false
		s.Detecting = false
	})
}

// Stop signals the pipeline to shut down.
func (p *Pipeline) Stop() {
	close(p.stopCh)
	p.logger.Info("camera pipeline stopped")
}

// Status returns a snapshot of the current pipeline state.
func (p *Pipeline) Status() PipelineStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
}

func (p *Pipeline) setStatus(fn func(s *PipelineStatus)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(&p.status)
}
