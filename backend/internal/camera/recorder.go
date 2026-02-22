// This file implements the per-camera ffmpeg recording subprocess (CG4, CG9, R2).
// Each Recorder wraps a single ffmpeg process that reads from go2rtc's RTSP re-stream
// and writes clock-aligned MP4 segments to hot storage via the segment muxer.
//
// Lifecycle: Pipeline calls Start() when stream is active and cam.Record=true,
// Stop() when stream drops or pipeline shuts down. On ffmpeg crash, the pipeline's
// next health check detects IsActive()=false and restarts.

package camera

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
)

// Recorder manages a single ffmpeg subprocess for recording a camera stream.
type Recorder struct {
	cam             *CameraRecord
	rtspBase        string // e.g. "rtsp://go2rtc:8554"
	hotPath         string
	segmentDuration int // minutes
	recRepo         *recording.Repository
	bus             *eventbus.Bus
	logger          *slog.Logger

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	active bool
	done   chan struct{} // closed when ffmpeg process + reader goroutine finish
}

// NewRecorder creates a recorder for a camera. Does not start ffmpeg.
func NewRecorder(
	cam *CameraRecord,
	rtspBase string,
	hotPath string,
	segmentDuration int,
	recRepo *recording.Repository,
	bus *eventbus.Bus,
	logger *slog.Logger,
) *Recorder {
	return &Recorder{
		cam:             cam,
		rtspBase:        rtspBase,
		hotPath:         hotPath,
		segmentDuration: segmentDuration,
		recRepo:         recRepo,
		bus:             bus,
		logger:          logger.With("component", "recorder", "camera", cam.Name),
	}
}

// Start launches the ffmpeg subprocess. Returns an error if the process can't be started.
// The recorder runs until Stop() is called or ffmpeg exits unexpectedly.
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.active {
		return nil // already running
	}

	if err := r.ensureDirectories(); err != nil {
		return fmt.Errorf("creating recording directories: %w", err)
	}

	args := r.buildFFmpegArgs()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	// Capture stdout for segment_list output (completed segment paths)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Capture stderr for ffmpeg log output
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("creating stderr pipe: %w", err)
	}

	// Set process group so we can signal the ffmpeg process directly
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting ffmpeg: %w", err)
	}

	r.cmd = cmd
	r.cancel = cancel
	r.active = true
	r.done = make(chan struct{})

	r.logger.Info("ffmpeg recording started",
		"pid", cmd.Process.Pid,
		"stream", RedactStreamURL(r.rtspBase+"/"+r.cam.Name),
		"segment_duration", r.segmentDuration,
	)

	// Goroutine: read completed segment paths from stdout
	go r.readSegmentList(stdout)

	// Goroutine: drain stderr so ffmpeg doesn't block, log warnings
	go r.drainStderr(stderr)

	// Goroutine: wait for ffmpeg to exit
	go func() {
		defer close(r.done)
		err := cmd.Wait()
		r.mu.Lock()
		wasActive := r.active
		r.active = false
		r.mu.Unlock()

		if wasActive {
			// Unexpected exit — ffmpeg crashed or stream died
			r.logger.Warn("ffmpeg exited unexpectedly", "error", err)
		} else {
			r.logger.Info("ffmpeg stopped cleanly")
		}
	}()

	return nil
}

// Stop gracefully shuts down the ffmpeg process.
// Sends SIGINT for a clean segment finalization, then SIGKILL after timeout.
func (r *Recorder) Stop() {
	r.mu.Lock()
	if !r.active {
		r.mu.Unlock()
		return
	}
	r.active = false
	cmd := r.cmd
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		// Send SIGINT for graceful shutdown (ffmpeg finalizes the current segment)
		_ = cmd.Process.Signal(syscall.SIGINT)

		// Wait up to 5 seconds for ffmpeg to exit gracefully
		select {
		case <-done:
			// Clean exit
		case <-time.After(5 * time.Second):
			r.logger.Warn("ffmpeg did not exit after SIGINT, sending SIGKILL")
			cancel() // context cancellation sends SIGKILL via CommandContext
			<-done   // wait for SIGKILL to take effect
		}
	} else if done != nil {
		<-done
	}

	r.logger.Info("recorder stopped")
}

// IsActive returns whether the ffmpeg process is currently running.
func (r *Recorder) IsActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// readSegmentList reads completed segment filenames from ffmpeg's stdout
// (populated by -segment_list pipe:1). Each line is the absolute path to a
// completed MP4 segment. We stat the file and record it in the database.
func (r *Recorder) readSegmentList(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		segPath := strings.TrimSpace(scanner.Text())
		if segPath == "" {
			continue
		}

		r.processCompletedSegment(segPath)
	}
}

// processCompletedSegment handles a newly completed MP4 segment:
// stats the file, inserts a DB record, and publishes an event.
func (r *Recorder) processCompletedSegment(segPath string) {
	info, err := os.Stat(segPath)
	if err != nil {
		r.logger.Warn("could not stat completed segment", "path", segPath, "error", err)
		return
	}

	// Parse start time from the segment filename (e.g., "10.00.mp4" → minute 10, second 0)
	startTime := r.parseSegmentTime(segPath)
	endTime := startTime.Add(time.Duration(r.segmentDuration) * time.Minute)
	durationS := float64(r.segmentDuration * 60)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rec := &recording.Record{
		CameraID:   r.cam.ID,
		CameraName: r.cam.Name,
		Path:       segPath,
		StartTime:  startTime,
		EndTime:    &endTime,
		DurationS:  durationS,
		SizeBytes:  info.Size(),
	}

	created, err := r.recRepo.Create(ctx, rec)
	if err != nil {
		r.logger.Error("failed to record completed segment in DB",
			"path", segPath, "error", err)
		return
	}

	r.logger.Info("segment recorded",
		"path", segPath,
		"size_bytes", info.Size(),
		"start_time", startTime.Format(time.RFC3339),
	)

	r.bus.Publish(eventbus.Event{
		Type:     "recording.segment_complete",
		CameraID: r.cam.ID,
		Label:    r.cam.Name,
		Data: map[string]any{
			"recording_id": created.ID,
			"path":         segPath,
			"size_bytes":   info.Size(),
			"start_time":   startTime,
			"end_time":     endTime,
		},
	})
}

// parseSegmentTime extracts the timestamp from a segment path.
// Path format: {hot_path}/{cam_name}/{YYYY-MM-DD}/{HH}/{MM.SS}.mp4
// Falls back to file modification time if parsing fails.
func (r *Recorder) parseSegmentTime(segPath string) time.Time {
	// Extract components from path
	dir := filepath.Dir(segPath)           // .../2025-01-15/08
	hourDir := filepath.Base(dir)          // "08"
	dateDir := filepath.Base(filepath.Dir(dir)) // "2025-01-15"
	base := filepath.Base(segPath)         // "10.00.mp4"
	minSec := strings.TrimSuffix(base, filepath.Ext(base)) // "10.00"

	// Parse "YYYY-MM-DD HH:MM:SS" using the local timezone.
	// Filenames are created by ffmpeg using strftime with the system clock (TZ env),
	// so time.ParseInLocation(time.Local) is required — time.Parse always returns UTC.
	timeStr := fmt.Sprintf("%s %s:%s", dateDir, hourDir, strings.Replace(minSec, ".", ":", 1))
	t, err := time.ParseInLocation("2006-01-02 15:04:05", timeStr, time.Local)
	if err != nil {
		// Fallback: use current time minus segment duration
		r.logger.Warn("could not parse segment time from path, using estimate",
			"path", segPath, "parsed_str", timeStr, "error", err)
		return time.Now().Add(-time.Duration(r.segmentDuration) * time.Minute)
	}
	return t
}

// drainStderr reads and logs ffmpeg's stderr output so it doesn't block.
func (r *Recorder) drainStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		// Only log warnings and errors from ffmpeg (skip info/progress lines)
		if strings.Contains(line, "error") || strings.Contains(line, "Error") ||
			strings.Contains(line, "warning") || strings.Contains(line, "Warning") {
			r.logger.Warn("ffmpeg", "output", line)
		}
	}
	if err := scanner.Err(); err != nil {
		r.logger.Warn("ffmpeg stderr read error", "error", err)
	}
}

// ensureDirectories creates the recording directory structure for the current
// and next hour so ffmpeg can write segments immediately.
func (r *Recorder) ensureDirectories() error {
	sanitized := SanitizeName(r.cam.Name)
	now := time.Now()

	// Create current hour directory
	currentDir := filepath.Join(r.hotPath, sanitized,
		now.Format("2006-01-02"), fmt.Sprintf("%02d", now.Hour()))
	if err := os.MkdirAll(currentDir, 0755); err != nil {
		return fmt.Errorf("creating current dir %s: %w", currentDir, err)
	}

	// Create next hour directory (so ffmpeg doesn't fail at hour boundary)
	nextHour := now.Add(time.Hour)
	nextDir := filepath.Join(r.hotPath, sanitized,
		nextHour.Format("2006-01-02"), fmt.Sprintf("%02d", nextHour.Hour()))
	if err := os.MkdirAll(nextDir, 0755); err != nil {
		return fmt.Errorf("creating next dir %s: %w", nextDir, err)
	}

	return nil
}

// buildFFmpegArgs constructs the ffmpeg command-line arguments for segment recording.
func (r *Recorder) buildFFmpegArgs() []string {
	sanitized := SanitizeName(r.cam.Name)
	segDurationSec := r.segmentDuration * 60
	outputPattern := filepath.Join(r.hotPath, sanitized, "%Y-%m-%d", "%H", "%M.%S.mp4")

	return []string{
		"-hide_banner",
		"-loglevel", "warning",
		// Input: go2rtc RTSP re-stream (TCP transport for reliability)
		"-rtsp_transport", "tcp",
		"-timeout", "10000000", // 10s RTSP timeout in microseconds
		"-i", fmt.Sprintf("%s/%s", r.rtspBase, r.cam.Name),
		// Output: copy streams (zero transcoding)
		"-c", "copy",
		// Segment muxer configuration
		"-f", "segment",
		"-segment_time", fmt.Sprintf("%d", segDurationSec),
		"-segment_atclocktime", "1",     // align segments to wall clock
		"-strftime", "1",                // use strftime tokens in output path
		"-reset_timestamps", "1",        // each segment starts at t=0 (independently playable)
		"-segment_list", "pipe:1",       // write completed segment paths to stdout
		"-segment_list_type", "flat",    // one path per line
		"-break_non_keyframes", "1",     // allow splitting at non-keyframe (more accurate timing)
		outputPattern,
	}
}
