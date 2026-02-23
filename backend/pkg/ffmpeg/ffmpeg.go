// Package ffmpeg provides a subprocess-based wrapper around FFmpeg.
// We deliberately avoid cgo FFmpeg bindings for build simplicity and stability.
// All interaction with FFmpeg happens via os/exec and stdin/stdout pipes.
package ffmpeg

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// HWAccel represents a hardware acceleration method.
type HWAccel string

const (
	HWAccelNone   HWAccel = "none"
	HWAccelAuto   HWAccel = "auto"
	HWAccelVAAPI  HWAccel = "vaapi"   // Intel iGPU (Linux)
	HWAccelQSV    HWAccel = "qsv"     // Intel Quick Sync
	HWAccelCUDA   HWAccel = "cuda"    // NVIDIA
	HWAccelVideoToolbox HWAccel = "videotoolbox" // macOS
)

// ProbeResult contains stream information from ffprobe.
type ProbeResult struct {
	Width     int
	Height    int
	Codec     string
	FPS       float64
	HasAudio  bool
}

// Probe queries stream metadata without starting a full decode.
func Probe(ctx context.Context, streamURL string) (*ProbeResult, error) {
	// TODO: Phase 4 (Playback) — implement ffprobe -v quiet -print_format json -show_streams <url>
	return nil, fmt.Errorf("not yet implemented")
}

// DetectArgs builds FFmpeg arguments for decoding a sub-stream for AI detection.
// Outputs raw frames to stdout at a reduced FPS for the detection pipeline.
func DetectArgs(streamURL string, width, height, fps int, hwaccel HWAccel) []string {
	args := []string{}

	if hwaccel != HWAccelNone {
		args = append(args, "-hwaccel", string(hwaccel))
		if hwaccel == HWAccelVAAPI {
			args = append(args, "-hwaccel_device", "/dev/dri/renderD128")
		}
	}

	args = append(args,
		"-rtsp_transport", "tcp",
		"-i", streamURL,
		"-vf", fmt.Sprintf("scale=%d:%d,fps=%d", width, height, fps),
		"-pix_fmt", "rgb24",
		"-f", "rawvideo",
		"pipe:1",
	)
	return args
}

// DetectVersion checks if FFmpeg is available and returns its version string.
func DetectVersion(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "ffmpeg", "-version").Output()
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found: %w", err)
	}
	firstLine := strings.SplitN(string(out), "\n", 2)[0]
	return firstLine, nil
}

// DetectHWAccel probes available hardware acceleration methods.
func DetectHWAccel(ctx context.Context) ([]HWAccel, error) {
	out, err := exec.CommandContext(ctx, "ffmpeg", "-hwaccels").Output()
	if err != nil {
		return nil, fmt.Errorf("detecting hwaccels: %w", err)
	}

	var accels []HWAccel
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case "vaapi":
			accels = append(accels, HWAccelVAAPI)
		case "qsv":
			accels = append(accels, HWAccelQSV)
		case "cuda":
			accels = append(accels, HWAccelCUDA)
		case "videotoolbox":
			accels = append(accels, HWAccelVideoToolbox)
		}
	}
	return accels, nil
}
