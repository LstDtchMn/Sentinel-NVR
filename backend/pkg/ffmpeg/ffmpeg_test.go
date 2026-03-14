package ffmpeg

import (
	"strings"
	"testing"
)

// ─── HWAccel constants ──────────────────────────────────────────────────────

func TestHWAccelConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		accel HWAccel
		want  string
	}{
		{"none", HWAccelNone, "none"},
		{"auto", HWAccelAuto, "auto"},
		{"vaapi", HWAccelVAAPI, "vaapi"},
		{"qsv", HWAccelQSV, "qsv"},
		{"cuda", HWAccelCUDA, "cuda"},
		{"videotoolbox", HWAccelVideoToolbox, "videotoolbox"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.accel) != tt.want {
				t.Errorf("HWAccel %s = %q, want %q", tt.name, string(tt.accel), tt.want)
			}
		})
	}
}

// ─── DetectArgs ─────────────────────────────────────────────────────────────

func TestDetectArgs_NoHWAccel(t *testing.T) {
	t.Parallel()

	args := DetectArgs("rtsp://go2rtc:8554/front-door", 320, 240, 5, HWAccelNone)
	argsStr := strings.Join(args, " ")

	// Should NOT contain -hwaccel flag when acceleration is disabled
	if strings.Contains(argsStr, "-hwaccel") {
		t.Errorf("expected no -hwaccel flag for HWAccelNone, args: %s", argsStr)
	}

	// Must contain RTSP transport and input URL
	if !strings.Contains(argsStr, "-rtsp_transport tcp") {
		t.Error("expected -rtsp_transport tcp in args")
	}
	if !strings.Contains(argsStr, "-i rtsp://go2rtc:8554/front-door") {
		t.Errorf("expected input URL in args, got: %s", argsStr)
	}

	// Must contain scale, fps, pixel format, and rawvideo output
	if !strings.Contains(argsStr, "scale=320:240") {
		t.Errorf("expected scale=320:240 in args, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "fps=5") {
		t.Errorf("expected fps=5 in args, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-pix_fmt rgb24") {
		t.Error("expected -pix_fmt rgb24")
	}
	if !strings.Contains(argsStr, "-f rawvideo") {
		t.Error("expected -f rawvideo")
	}
	if !strings.Contains(argsStr, "pipe:1") {
		t.Error("expected pipe:1 for stdout output")
	}
}

func TestDetectArgs_WithHWAccel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		accel           HWAccel
		wantHWAccel     string
		wantDeviceFlag  bool
		wantDeviceValue string
	}{
		{
			name:           "auto",
			accel:          HWAccelAuto,
			wantHWAccel:    "auto",
			wantDeviceFlag: false,
		},
		{
			name:            "vaapi with device",
			accel:           HWAccelVAAPI,
			wantHWAccel:     "vaapi",
			wantDeviceFlag:  true,
			wantDeviceValue: "/dev/dri/renderD128",
		},
		{
			name:           "qsv",
			accel:          HWAccelQSV,
			wantHWAccel:    "qsv",
			wantDeviceFlag: false,
		},
		{
			name:           "cuda",
			accel:          HWAccelCUDA,
			wantHWAccel:    "cuda",
			wantDeviceFlag: false,
		},
		{
			name:           "videotoolbox",
			accel:          HWAccelVideoToolbox,
			wantHWAccel:    "videotoolbox",
			wantDeviceFlag: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := DetectArgs("rtsp://localhost:8554/cam1", 640, 480, 2, tt.accel)
			argsStr := strings.Join(args, " ")

			if !strings.Contains(argsStr, "-hwaccel "+tt.wantHWAccel) {
				t.Errorf("expected -hwaccel %s in args: %s", tt.wantHWAccel, argsStr)
			}

			hasDevice := strings.Contains(argsStr, "-hwaccel_device")
			if tt.wantDeviceFlag && !hasDevice {
				t.Errorf("expected -hwaccel_device flag for %s", tt.name)
			}
			if !tt.wantDeviceFlag && hasDevice {
				t.Errorf("unexpected -hwaccel_device flag for %s", tt.name)
			}
			if tt.wantDeviceFlag && !strings.Contains(argsStr, tt.wantDeviceValue) {
				t.Errorf("expected device %q, args: %s", tt.wantDeviceValue, argsStr)
			}
		})
	}
}

func TestDetectArgs_ScaleAndFPS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		width      int
		height     int
		fps        int
		wantScale  string
		wantFPS    string
	}{
		{"320x240 @ 5fps", 320, 240, 5, "scale=320:240", "fps=5"},
		{"640x480 @ 1fps", 640, 480, 1, "scale=640:480", "fps=1"},
		{"1920x1080 @ 30fps", 1920, 1080, 30, "scale=1920:1080", "fps=30"},
		{"416x416 @ 10fps", 416, 416, 10, "scale=416:416", "fps=10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := DetectArgs("rtsp://localhost/stream", tt.width, tt.height, tt.fps, HWAccelNone)
			argsStr := strings.Join(args, " ")

			if !strings.Contains(argsStr, tt.wantScale) {
				t.Errorf("expected %q in args: %s", tt.wantScale, argsStr)
			}
			if !strings.Contains(argsStr, tt.wantFPS) {
				t.Errorf("expected %q in args: %s", tt.wantFPS, argsStr)
			}
		})
	}
}

func TestDetectArgs_VFilterCombinesScaleAndFPS(t *testing.T) {
	t.Parallel()

	args := DetectArgs("rtsp://host/cam", 320, 240, 5, HWAccelNone)
	argsStr := strings.Join(args, " ")

	// The -vf flag should combine scale and fps into a single filter chain
	expected := "-vf scale=320:240,fps=5"
	if !strings.Contains(argsStr, expected) {
		t.Errorf("expected combined filter %q in args: %s", expected, argsStr)
	}
}

func TestDetectArgs_StreamURLPreserved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{"simple RTSP", "rtsp://192.168.1.100:554/stream1"},
		{"RTSP with credentials", "rtsp://admin:password@camera.local:554/Streaming/Channels/101"},
		{"go2rtc URL", "rtsp://go2rtc:8554/front-door"},
		{"URL with query params", "rtsp://host/stream?transport=tcp&channel=1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := DetectArgs(tt.url, 320, 240, 5, HWAccelNone)

			// Find the -i flag and check the next argument
			found := false
			for i, arg := range args {
				if arg == "-i" && i+1 < len(args) {
					if args[i+1] != tt.url {
						t.Errorf("input URL = %q, want %q", args[i+1], tt.url)
					}
					found = true
					break
				}
			}
			if !found {
				t.Errorf("-i flag not found in args: %v", args)
			}
		})
	}
}

func TestDetectArgs_OutputIsPipeStdout(t *testing.T) {
	t.Parallel()

	args := DetectArgs("rtsp://host/cam", 320, 240, 5, HWAccelNone)

	lastArg := args[len(args)-1]
	if lastArg != "pipe:1" {
		t.Errorf("last arg = %q, want pipe:1", lastArg)
	}
}

func TestDetectArgs_HWAccelBeforeInput(t *testing.T) {
	t.Parallel()

	// -hwaccel must come before -i in the argument list (ffmpeg requirement)
	args := DetectArgs("rtsp://host/cam", 320, 240, 5, HWAccelCUDA)

	hwIdx := -1
	inputIdx := -1
	for i, arg := range args {
		if arg == "-hwaccel" {
			hwIdx = i
		}
		if arg == "-i" {
			inputIdx = i
		}
	}

	if hwIdx == -1 {
		t.Fatal("-hwaccel flag not found")
	}
	if inputIdx == -1 {
		t.Fatal("-i flag not found")
	}
	if hwIdx >= inputIdx {
		t.Errorf("-hwaccel (idx %d) must come before -i (idx %d)", hwIdx, inputIdx)
	}
}

func TestDetectArgs_EmptyStreamURL(t *testing.T) {
	t.Parallel()

	// Even with empty URL, DetectArgs should still build args (validation is caller's job)
	args := DetectArgs("", 320, 240, 5, HWAccelNone)

	found := false
	for i, arg := range args {
		if arg == "-i" && i+1 < len(args) {
			if args[i+1] != "" {
				t.Errorf("expected empty input URL, got %q", args[i+1])
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("-i flag not found in args")
	}
}

func TestDetectArgs_NoStimeoutFlag(t *testing.T) {
	t.Parallel()

	// -stimeout was removed in ffmpeg 5.x — DetectArgs must never include it.
	// This test verifies we use the correct timeout approach for ffmpeg 5.x.
	args := DetectArgs("rtsp://host/cam", 320, 240, 5, HWAccelNone)
	argsStr := strings.Join(args, " ")

	if strings.Contains(argsStr, "-stimeout") {
		t.Error("-stimeout should not appear in detect args — removed in ffmpeg 5.x")
	}
}

func TestDetectArgs_AlwaysUsesRTSPTransportTCP(t *testing.T) {
	t.Parallel()

	args := DetectArgs("rtsp://host/cam", 320, 240, 5, HWAccelNone)

	transportIdx := -1
	for i, arg := range args {
		if arg == "-rtsp_transport" {
			transportIdx = i
			break
		}
	}

	if transportIdx == -1 {
		t.Fatal("-rtsp_transport flag not found")
	}
	if transportIdx+1 >= len(args) {
		t.Fatal("-rtsp_transport has no value")
	}
	if args[transportIdx+1] != "tcp" {
		t.Errorf("RTSP transport = %q, want tcp", args[transportIdx+1])
	}
}

func TestDetectArgs_RawVideoFormat(t *testing.T) {
	t.Parallel()

	args := DetectArgs("rtsp://host/cam", 320, 240, 5, HWAccelNone)

	fIdx := -1
	for i, arg := range args {
		if arg == "-f" {
			fIdx = i
			break
		}
	}

	if fIdx == -1 {
		t.Fatal("-f flag not found")
	}
	if fIdx+1 >= len(args) {
		t.Fatal("-f has no value")
	}
	if args[fIdx+1] != "rawvideo" {
		t.Errorf("output format = %q, want rawvideo", args[fIdx+1])
	}
}

func TestDetectArgs_PixelFormatRGB24(t *testing.T) {
	t.Parallel()

	args := DetectArgs("rtsp://host/cam", 320, 240, 5, HWAccelNone)

	pixIdx := -1
	for i, arg := range args {
		if arg == "-pix_fmt" {
			pixIdx = i
			break
		}
	}

	if pixIdx == -1 {
		t.Fatal("-pix_fmt flag not found")
	}
	if pixIdx+1 >= len(args) {
		t.Fatal("-pix_fmt has no value")
	}
	if args[pixIdx+1] != "rgb24" {
		t.Errorf("pixel format = %q, want rgb24", args[pixIdx+1])
	}
}

// ─── DetectArgs argument ordering ───────────────────────────────────────────

func TestDetectArgs_ArgumentOrder(t *testing.T) {
	t.Parallel()

	// Verify the canonical order: [hwaccel opts] -rtsp_transport tcp -i <url> -vf ... -pix_fmt ... -f rawvideo pipe:1
	args := DetectArgs("rtsp://host/cam", 640, 480, 10, HWAccelVAAPI)

	indices := map[string]int{}
	for i, arg := range args {
		switch arg {
		case "-hwaccel", "-hwaccel_device", "-rtsp_transport", "-i", "-vf", "-pix_fmt", "-f":
			indices[arg] = i
		}
	}

	// hwaccel < rtsp_transport < -i < -vf < -pix_fmt < -f
	order := []string{"-hwaccel", "-hwaccel_device", "-rtsp_transport", "-i", "-vf", "-pix_fmt", "-f"}
	for i := 0; i < len(order)-1; i++ {
		idxA, okA := indices[order[i]]
		idxB, okB := indices[order[i+1]]
		if okA && okB && idxA >= idxB {
			t.Errorf("%s (idx %d) should come before %s (idx %d)", order[i], idxA, order[i+1], idxB)
		}
	}
}

// ─── DetectArgs VAAPI-specific tests ────────────────────────────────────────

func TestDetectArgs_VAAPIDevice(t *testing.T) {
	t.Parallel()

	args := DetectArgs("rtsp://host/cam", 320, 240, 5, HWAccelVAAPI)

	deviceIdx := -1
	for i, arg := range args {
		if arg == "-hwaccel_device" {
			deviceIdx = i
			break
		}
	}

	if deviceIdx == -1 {
		t.Fatal("-hwaccel_device not found for VAAPI")
	}
	if deviceIdx+1 >= len(args) {
		t.Fatal("-hwaccel_device has no value")
	}
	if args[deviceIdx+1] != "/dev/dri/renderD128" {
		t.Errorf("VAAPI device = %q, want /dev/dri/renderD128", args[deviceIdx+1])
	}
}

func TestDetectArgs_NonVAAPINoDevice(t *testing.T) {
	t.Parallel()

	accels := []HWAccel{HWAccelAuto, HWAccelQSV, HWAccelCUDA, HWAccelVideoToolbox}
	for _, accel := range accels {
		args := DetectArgs("rtsp://host/cam", 320, 240, 5, accel)
		argsStr := strings.Join(args, " ")
		if strings.Contains(argsStr, "-hwaccel_device") {
			t.Errorf("unexpected -hwaccel_device for %s", string(accel))
		}
	}
}

// ─── DetectArgs returns a new slice each call ───────────────────────────────

func TestDetectArgs_ReturnsNewSlice(t *testing.T) {
	t.Parallel()

	args1 := DetectArgs("rtsp://host/cam1", 320, 240, 5, HWAccelNone)
	args2 := DetectArgs("rtsp://host/cam2", 640, 480, 10, HWAccelCUDA)

	// Modifying one slice should not affect the other
	args1[0] = "MODIFIED"

	for _, arg := range args2 {
		if arg == "MODIFIED" {
			t.Error("DetectArgs should return independent slices")
		}
	}
}

// ─── DetectArgs complete argument count ─────────────────────────────────────

func TestDetectArgs_ArgCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		accel    HWAccel
		wantLen  int
	}{
		// No HWAccel: -rtsp_transport tcp -i URL -vf FILTER -pix_fmt rgb24 -f rawvideo pipe:1 = 11 args
		{"no hwaccel", HWAccelNone, 11},
		// With HWAccel (non-VAAPI): -hwaccel TYPE + above = 13 args
		{"cuda", HWAccelCUDA, 13},
		// With VAAPI: -hwaccel vaapi -hwaccel_device /dev/... + above = 15 args
		{"vaapi", HWAccelVAAPI, 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := DetectArgs("rtsp://host/cam", 320, 240, 5, tt.accel)
			if len(args) != tt.wantLen {
				t.Errorf("len(args) = %d, want %d; args = %v", len(args), tt.wantLen, args)
			}
		})
	}
}
