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

	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	cancel   context.CancelFunc
	active   bool
	stopping bool           // true between Stop() releasing the lock and ffmpeg exiting
	done     chan struct{}   // closed when ffmpeg process + all I/O goroutines finish
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

	// Don't start while active or while a Stop() is in flight. Without the
	// stopping guard, a health tick that sees active=false (set by Stop before
	// ffmpeg exits) could call Start() which overwrites r.done, causing the
	// old waiter goroutine to close the new channel — and the new waiter to
	// double-close it → panic.
	if r.active || r.stopping {
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

	// Set process group for direct signal delivery (Unix only; no-op on Windows)
	setSysProcAttr(cmd)

	// Create stdin pipe for graceful shutdown (Windows: write 'q' to quit ffmpeg)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("creating stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting ffmpeg: %w", err)
	}

	r.cmd = cmd
	r.stdin = stdin
	r.cancel = cancel
	r.active = true
	r.done = make(chan struct{})

	r.logger.Info("ffmpeg recording started",
		"pid", cmd.Process.Pid,
		"stream", RedactStreamURL(r.rtspBase+"/"+r.cam.Name),
		"segment_duration", r.segmentDuration,
	)
	r.bus.Publish(eventbus.Event{
		Type:     "recording.started",
		CameraID: r.cam.ID,
		Label:    r.cam.Name,
	})

	// Track I/O goroutines so done isn't closed while they're still running.
	// This prevents use-after-free when callers proceed after Stop() returns.
	var ioWg sync.WaitGroup
	ioWg.Add(2)

	go func() {
		defer ioWg.Done()
		r.readSegmentList(stdout)
	}()

	go func() {
		defer ioWg.Done()
		r.drainStderr(stderr)
	}()

	// Capture done locally — the waiter goroutine must close THIS channel, not
	// r.done (which Start() may overwrite if called between Stop releasing the
	// mutex and ffmpeg actually exiting).
	done := r.done

	// Wait for ffmpeg exit, then wait for I/O goroutines, then signal done
	go func() {
		err := cmd.Wait()
		cancel() // always release context resources (prevents leak on graceful exit path)
		r.mu.Lock()
		wasActive := r.active
		r.active = false
		// Do NOT clear stopping here — I/O goroutines (readSegmentList, drainStderr)
		// are still running. Clearing stopping before they finish would allow a
		// concurrent Start() to launch new I/O goroutines while old ones are active.
		r.mu.Unlock()

		if wasActive {
			r.logger.Warn("ffmpeg exited unexpectedly", "error", err)
		} else {
			r.logger.Info("ffmpeg stopped cleanly")
		}
		r.bus.Publish(eventbus.Event{
			Type:     "recording.stopped",
			CameraID: r.cam.ID,
			Label:    r.cam.Name,
		})

		ioWg.Wait() // wait for readSegmentList and drainStderr to exit

		// NOW it is safe to allow Start() again — ffmpeg and all I/O are fully done.
		r.mu.Lock()
		r.stopping = false
		r.mu.Unlock()

		close(done) // close the captured channel, not r.done
	}()

	return nil
}

// Stop gracefully shuts down the ffmpeg process.
// Sends interrupt for a clean segment finalization, then SIGKILL after timeout.
// Safe to call concurrently — all callers block until ffmpeg and I/O goroutines finish.
func (r *Recorder) Stop() {
	r.mu.Lock()
	done := r.done
	if done == nil {
		r.mu.Unlock()
		return // never started
	}

	needsSignal := r.active
	if needsSignal {
		r.active = false
		r.stopping = true // prevents Start() until the waiter goroutine clears it
	}
	cmd := r.cmd
	stdin := r.stdin
	cancel := r.cancel
	r.mu.Unlock()

	if needsSignal && cmd != nil && cmd.Process != nil {
		// Send interrupt for graceful shutdown (ffmpeg finalizes the current segment).
		// Log at Debug so failures (e.g. process already dead, stdin closed) are diagnosable.
		if err := sendInterrupt(cmd.Process, stdin); err != nil {
			r.logger.Debug("ffmpeg interrupt signal failed", "error", err)
		}

		// Wait up to 5 seconds for ffmpeg to exit gracefully
		select {
		case <-done:
			return
		case <-time.After(5 * time.Second):
			r.logger.Warn("ffmpeg did not exit after interrupt, forcing kill")
			cancel() // context cancellation sends SIGKILL via CommandContext
		}
	}

	<-done // all callers wait for full cleanup (process exit + I/O goroutine drain)
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
	// Check for pipe errors after the scan loop. Without this, a broken stdout
	// pipe (e.g. bufio.ErrTooLong on an unexpectedly long path) causes silent
	// data loss — ffmpeg keeps recording but no segments are inserted into the DB.
	if err := scanner.Err(); err != nil {
		r.logger.Error("segment list read error — segments may not be recorded to DB",
			"camera", r.cam.Name, "error", err)
	}
}

// processCompletedSegment handles a newly completed MP4 segment:
// stats the file, inserts a DB record, and publishes an event.
func (r *Recorder) processCompletedSegment(segPath string) {
	// Normalize slashes: ffmpeg may output forward slashes even on Windows.
	// Do this first so os.Stat, isUnderPath, and the DB record all use the
	// canonical OS-specific separator.
	segPath = filepath.Clean(filepath.FromSlash(segPath))

	// Containment check: reject paths that escape the hot storage boundary before
	// inserting into the DB. The serve/delete endpoints also check at read time,
	// but early rejection prevents unresolvable DB records and defends against
	// misconfiguration or unexpected ffmpeg output paths.
	if !isUnderPath(segPath, r.hotPath) {
		r.logger.Error("segment path escapes hot storage boundary, refusing to record",
			"path", segPath, "hot_path", r.hotPath)
		return
	}

	info, err := os.Stat(segPath)
	if err != nil {
		r.logger.Warn("could not stat completed segment", "path", segPath, "error", err)
		return
	}
	if info.Size() == 0 {
		// A 0-byte MP4 is unplayable — discard it rather than inserting a misleading DB record.
		// This can occur if the RTSP source drops mid-segment or the disk fills up.
		r.logger.Warn("discarding zero-byte segment", "path", segPath)
		return
	}

	// Parse start time from the segment filename (e.g., "10.00.mp4" → minute 10, second 0).
	// endTime and durationS are estimated from the configured segment duration, not measured
	// from the file. The first segment after recorder start is clock-aligned and may be shorter
	// than segmentDuration; the last segment at shutdown is also short. Phase 4 (Playback)
	// should refine these values using ffprobe or the next segment's start time.
	// Pass info.ModTime() so parseSegmentTime doesn't need a second os.Stat call.
	startTime := r.parseSegmentTime(segPath, info.ModTime())
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
// modTime is the file modification time from the caller's os.Stat — passed in
// to avoid a redundant second stat call when path parsing fails.
func (r *Recorder) parseSegmentTime(segPath string, modTime time.Time) time.Time {
	// ffmpeg may output forward slashes even on Windows; normalize to OS separators
	// so filepath.Dir/Base work correctly on all platforms.
	segPath = filepath.FromSlash(segPath)

	// Extract components from path
	dir := filepath.Dir(segPath)                // .../2025-01-15/08
	hourDir := filepath.Base(dir)               // "08"
	dateDir := filepath.Base(filepath.Dir(dir)) // "2025-01-15"
	base := filepath.Base(segPath)              // "10.00.mp4"
	minSec := strings.TrimSuffix(base, filepath.Ext(base)) // "10.00"

	// Parse "YYYY-MM-DD HH:MM:SS" using the local timezone.
	// Filenames are created by ffmpeg using strftime with the system clock (TZ env),
	// so time.ParseInLocation(time.Local) is required — time.Parse always returns UTC.
	timeStr := fmt.Sprintf("%s %s:%s", dateDir, hourDir, strings.Replace(minSec, ".", ":", 1))
	t, err := time.ParseInLocation("2006-01-02 15:04:05", timeStr, time.Local)
	if err != nil {
		// Fallback: use file modification time minus the configured segment duration as
		// an approximate start time. The mtime is the segment close time; subtracting
		// segment duration is a rough estimate only — it is wrong for the first short
		// segment after recorder start and the last segment at shutdown. Still better
		// than time.Now() which has no relationship to the actual recording time.
		r.logger.Warn("could not parse segment time from path, using estimate from file mtime",
			"path", segPath, "parsed_str", timeStr, "error", err)
		return modTime.Add(-time.Duration(r.segmentDuration) * time.Minute)
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
	// filepath.ToSlash ensures forward-slash separators on all platforms.
	// ffmpeg's strftime expansion is performed by its C runtime, which requires
	// forward slashes in the path template regardless of OS.
	outputPattern := filepath.ToSlash(filepath.Join(r.hotPath, sanitized, "%Y-%m-%d", "%H", "%M.%S.mp4"))

	return []string{
		"-hide_banner",
		"-loglevel", "warning",
		// Input: go2rtc RTSP re-stream (TCP transport for reliability)
		"-rtsp_transport", "tcp",
		"-stimeout", "10000000", // 10s RTSP socket timeout in microseconds (ffmpeg RTSP demuxer option)
		"-i", fmt.Sprintf("%s/%s", strings.TrimRight(r.rtspBase, "/"), r.cam.Name),
		// Output: copy streams (zero transcoding)
		"-c", "copy",
		// Segment muxer configuration
		"-f", "segment",
		"-segment_time", fmt.Sprintf("%d", segDurationSec),
		"-segment_atclocktime", "1",     // align segments to wall clock
		"-strftime", "1",                // use strftime tokens in output path
		"-strftime_mkdir", "1",          // auto-create output directories (hour/day boundaries)
		"-reset_timestamps", "1",        // each segment starts at t=0 (independently playable)
		"-segment_list", "pipe:1",       // write completed segment paths to stdout
		"-segment_list_type", "flat",    // one path per line
		outputPattern,
	}
}
