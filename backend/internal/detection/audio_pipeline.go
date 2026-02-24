package detection

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
)

// AudioClassifier runs inference on a raw audio sample (PCM16 mono 16kHz)
// and returns classified audio events.
type AudioClassifier interface {
	Classify(ctx context.Context, pcmData []byte) ([]AudioDetection, error)
}

// AudioDetection is a single audio classification result.
type AudioDetection struct {
	Label      string  `json:"label"`      // e.g. "glass_break", "dog_bark", "baby_cry"
	Confidence float64 `json:"confidence"` // 0.0-1.0
}

// AudioPipeline periodically extracts audio from a camera stream and classifies it (R12).
// It follows the same lifecycle pattern as DetectionPipeline:
//   - Start() spawns a goroutine; safe to call multiple times (no-op if already running).
//   - Stop() signals the goroutine to exit and blocks until it does. Safe before Start().
//   - IsActive() reports whether the goroutine is running.
type AudioPipeline struct {
	cam            CameraInfo
	classifier     AudioClassifier
	threshold      float64
	sampleInterval time.Duration
	rtspURL        string // go2rtc RTSP URL for this camera's stream (e.g. rtsp://go2rtc:8554/front_door)
	bus            *eventbus.Bus
	logger         *slog.Logger

	ctx       context.Context
	ctxCancel context.CancelFunc

	stopCh   chan struct{}
	stopOnce sync.Once
	done     chan struct{}

	started atomic.Bool
	active  atomic.Bool

	// Separate fail counters for extraction and classification, throttled like DetectionPipeline.
	extractFailCount  int
	classifyFailCount int
}

// NewAudioPipeline creates an AudioPipeline for a single camera.
// rtspURL is the full RTSP URL for the camera's stream in go2rtc
// (e.g. "rtsp://go2rtc:8554/front_door") used for ffmpeg audio extraction.
func NewAudioPipeline(
	cam CameraInfo,
	classifier AudioClassifier,
	threshold float64,
	sampleInterval time.Duration,
	rtspURL string,
	bus *eventbus.Bus,
	logger *slog.Logger,
) *AudioPipeline {
	if sampleInterval < time.Second {
		sampleInterval = time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &AudioPipeline{
		cam:            cam,
		classifier:     classifier,
		threshold:      threshold,
		sampleInterval: sampleInterval,
		rtspURL:        rtspURL,
		bus:            bus,
		logger:         logger.With("camera", cam.Name, "component", "audio_pipeline"),
		ctx:            ctx,
		ctxCancel:      cancel,
		stopCh:         make(chan struct{}),
		done:           make(chan struct{}),
	}
}

// Start spawns the audio classification goroutine.
func (ap *AudioPipeline) Start() {
	if !ap.started.CompareAndSwap(false, true) {
		return
	}
	ap.active.Store(true)
	go ap.run()
}

// IsActive reports whether the audio pipeline goroutine is running.
func (ap *AudioPipeline) IsActive() bool {
	return ap.active.Load()
}

// Stop signals the goroutine to exit and blocks until it does.
func (ap *AudioPipeline) Stop() {
	ap.ctxCancel()
	ap.stopOnce.Do(func() { close(ap.stopCh) })
	if ap.started.Load() {
		<-ap.done
	}
}

func (ap *AudioPipeline) run() {
	defer func() {
		ap.active.Store(false)
		close(ap.done)
	}()

	ap.logger.Info("audio pipeline started",
		"interval", ap.sampleInterval,
		"threshold", ap.threshold,
		"rtsp_url", ap.rtspURL,
	)

	ticker := time.NewTicker(ap.sampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ap.stopCh:
			ap.logger.Info("audio pipeline stopped")
			return
		case <-ticker.C:
			ap.processSample()
		}
	}
}

// processSample extracts a short PCM audio segment from the camera's RTSP stream
// via ffmpeg, runs it through the audio classifier, and publishes events for
// classifications above the configured threshold (R12).
//
// The extraction uses ffmpeg to read from go2rtc's RTSP re-stream and output
// PCM16 mono 16kHz audio to stdout. This format matches the YAMNet model's
// expected input (16kHz signed 16-bit little-endian mono PCM).
func (ap *AudioPipeline) processSample() {
	if ap.classifier == nil {
		return
	}

	// Extraction takes sampleInterval seconds of real-time audio capture + ffmpeg startup.
	// Use a separate context so the classification call gets its own full time budget.
	extractTimeout := ap.sampleInterval + 10*time.Second
	extractCtx, extractCancel := context.WithTimeout(ap.ctx, extractTimeout)
	defer extractCancel()

	sampleDuration := fmt.Sprintf("%.0f", ap.sampleInterval.Seconds())
	pcmData, err := ap.extractAudio(extractCtx, sampleDuration)
	if err != nil {
		ap.extractFailCount++
		if ap.extractFailCount == 1 || ap.extractFailCount%12 == 0 {
			ap.logger.Warn("failed to extract audio from stream",
				"error", err,
				"consecutive_failures", ap.extractFailCount,
			)
		}
		return
	}
	ap.extractFailCount = 0

	if len(pcmData) == 0 {
		ap.logger.Debug("extracted empty audio sample; skipping classification")
		return
	}

	// Classification gets its own context — independent of extraction timing.
	classifyCtx, classifyCancel := context.WithTimeout(ap.ctx, 15*time.Second)
	defer classifyCancel()

	classifications, err := ap.classifier.Classify(classifyCtx, pcmData)
	if err != nil {
		ap.classifyFailCount++
		if ap.classifyFailCount == 1 || ap.classifyFailCount%12 == 0 {
			ap.logger.Warn("audio classification failed",
				"error", err,
				"consecutive_failures", ap.classifyFailCount,
			)
		}
		return
	}
	ap.classifyFailCount = 0

	// Publish events for classifications above threshold.
	for _, cls := range classifications {
		if cls.Confidence < ap.threshold {
			continue
		}

		ap.bus.Publish(eventbus.Event{
			Type:       "audio_detection",
			CameraID:   ap.cam.ID,
			Label:      cls.Label,
			Confidence: cls.Confidence,
			Data: map[string]any{
				"label":      cls.Label,
				"confidence": cls.Confidence,
				"source":     "audio",
			},
		})

		ap.logger.Debug("audio detection event published",
			"label", cls.Label,
			"confidence", fmt.Sprintf("%.3f", cls.Confidence),
		)
	}
}

// extractAudio runs ffmpeg to extract PCM16 mono 16kHz audio from the RTSP stream.
// Returns the raw PCM bytes written to stdout.
func (ap *AudioPipeline) extractAudio(ctx context.Context, durationSec string) ([]byte, error) {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", ap.rtspURL,
		"-t", durationSec,
		"-vn",                // no video
		"-acodec", "pcm_s16le",
		"-f", "s16le",
		"-ar", "16000",       // 16kHz sample rate
		"-ac", "1",           // mono
		"pipe:1",             // output to stdout
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Include stderr in the error for ffmpeg diagnostics.
		// Truncate at rune boundary to avoid splitting multi-byte UTF-8 sequences.
		stderrStr := stderr.String()
		runes := []rune(stderrStr)
		if len(runes) > 200 {
			stderrStr = string(runes[:200]) + "..."
		}
		return nil, fmt.Errorf("ffmpeg audio extraction: %w (stderr: %s)", err, stderrStr)
	}

	return stdout.Bytes(), nil
}
