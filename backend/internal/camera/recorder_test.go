package camera

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func testRecorderLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ─── buildFFmpegArgs ─────────────────────────────────────────────────────────

func TestBuildFFmpegArgs_ContainsTimeoutFlag(t *testing.T) {
	cam := &CameraRecord{Name: "test-cam", ID: 1}
	r := &Recorder{
		cam:             cam,
		rtspBase:        "rtsp://go2rtc:8554",
		hotPath:         "/data/recordings",
		segmentDuration: 10,
		logger:          testRecorderLogger(),
	}

	args := r.buildFFmpegArgs()
	argsStr := strings.Join(args, " ")

	// Must use -timeout (ffmpeg 5.x+), NOT -stimeout (deprecated)
	if !strings.Contains(argsStr, "-timeout") {
		t.Error("expected -timeout flag in ffmpeg args")
	}
	if strings.Contains(argsStr, "-stimeout") {
		t.Error("-stimeout flag should not be present (deprecated in ffmpeg 5.x)")
	}
}

func TestBuildFFmpegArgs_UsesForwardSlashPaths(t *testing.T) {
	cam := &CameraRecord{Name: "test-cam", ID: 1}
	r := &Recorder{
		cam:             cam,
		rtspBase:        "rtsp://go2rtc:8554",
		hotPath:         `C:\data\recordings`,
		segmentDuration: 10,
		logger:          testRecorderLogger(),
	}

	args := r.buildFFmpegArgs()
	outputPattern := args[len(args)-1] // last arg is the output pattern

	// filepath.ToSlash ensures forward slashes for ffmpeg strftime
	if strings.Contains(outputPattern, `\`) {
		t.Errorf("output pattern contains backslashes: %q — ffmpeg strftime requires forward slashes", outputPattern)
	}
}

func TestBuildFFmpegArgs_ContainsSegmentConfig(t *testing.T) {
	cam := &CameraRecord{Name: "test-cam", ID: 1}
	r := &Recorder{
		cam:             cam,
		rtspBase:        "rtsp://go2rtc:8554",
		hotPath:         "/data/recordings",
		segmentDuration: 10,
		logger:          testRecorderLogger(),
	}

	args := r.buildFFmpegArgs()
	argsStr := strings.Join(args, " ")

	// Segment muxer flags
	for _, flag := range []string{"-f segment", "-segment_atclocktime 1", "-strftime 1", "-strftime_mkdir 1"} {
		if !strings.Contains(argsStr, flag) {
			t.Errorf("missing expected flag %q in ffmpeg args", flag)
		}
	}

	// Segment time should be in seconds (10min * 60 = 600)
	if !strings.Contains(argsStr, "-segment_time 600") {
		t.Errorf("expected -segment_time 600 (10 min), args: %s", argsStr)
	}
}

func TestBuildFFmpegArgs_RTSPInput(t *testing.T) {
	cam := &CameraRecord{Name: "front-door", ID: 1}
	r := &Recorder{
		cam:             cam,
		rtspBase:        "rtsp://go2rtc:8554",
		hotPath:         "/data/recordings",
		segmentDuration: 10,
		logger:          testRecorderLogger(),
	}

	args := r.buildFFmpegArgs()
	argsStr := strings.Join(args, " ")

	// Input should be go2rtc RTSP re-stream URL
	if !strings.Contains(argsStr, "-i rtsp://go2rtc:8554/front-door") {
		t.Errorf("expected RTSP input URL with camera name, args: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-rtsp_transport tcp") {
		t.Error("expected TCP transport for RTSP")
	}
}

func TestBuildFFmpegArgs_OutputPattern(t *testing.T) {
	cam := &CameraRecord{Name: "garage", ID: 1}
	r := &Recorder{
		cam:             cam,
		rtspBase:        "rtsp://go2rtc:8554",
		hotPath:         "/data/recordings",
		segmentDuration: 10,
		logger:          testRecorderLogger(),
	}

	args := r.buildFFmpegArgs()
	outputPattern := args[len(args)-1]

	// Output should include sanitized camera name and strftime tokens
	sanitized := SanitizeName("garage")
	expected := filepath.ToSlash(filepath.Join("/data/recordings", sanitized, "%Y-%m-%d", "%H", "%M.%S.mp4"))
	if outputPattern != expected {
		t.Errorf("output pattern = %q, want %q", outputPattern, expected)
	}
}
