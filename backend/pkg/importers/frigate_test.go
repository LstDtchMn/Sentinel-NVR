package importers

import (
	"strings"
	"testing"
)

func TestParseFrigateValidConfig(t *testing.T) {
	t.Parallel()
	input := `
cameras:
  front_door:
    enabled: true
    ffmpeg:
      inputs:
        - path: rtsp://admin:pass@192.168.1.100:554/stream1
          roles:
            - record
        - path: rtsp://admin:pass@192.168.1.100:554/stream2
          roles:
            - detect
    detect:
      enabled: true
    record:
      enabled: true
`
	result := ParseFrigate([]byte(input))
	if result.Format != "frigate" {
		t.Errorf("format = %q, want %q", result.Format, "frigate")
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	cam := result.Cameras[0]
	if cam.Name != "front_door" {
		t.Errorf("name = %q, want %q", cam.Name, "front_door")
	}
	if !cam.Enabled {
		t.Error("expected enabled=true")
	}
	if !cam.Record {
		t.Error("expected record=true")
	}
	if !cam.Detect {
		t.Error("expected detect=true")
	}
	if cam.MainStream != "rtsp://admin:pass@192.168.1.100:554/stream1" {
		t.Errorf("main stream = %q, want record-role path", cam.MainStream)
	}
	if cam.SubStream != "rtsp://admin:pass@192.168.1.100:554/stream2" {
		t.Errorf("sub stream = %q, want detect-role path", cam.SubStream)
	}
	// ONVIF host/user extracted from RTSP URL
	if cam.ONVIFHost != "192.168.1.100" {
		t.Errorf("onvif host = %q, want %q", cam.ONVIFHost, "192.168.1.100")
	}
	if cam.ONVIFUser != "admin" {
		t.Errorf("onvif user = %q, want %q", cam.ONVIFUser, "admin")
	}
	if cam.ONVIFPass != "pass" {
		t.Errorf("onvif pass = %q, want %q", cam.ONVIFPass, "pass")
	}
}

func TestParseFrigateMultipleCameras(t *testing.T) {
	t.Parallel()
	input := `
cameras:
  cam1:
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.1/ch1
          roles:
            - record
    record:
      enabled: true
  cam2:
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.2/ch1
          roles:
            - record
        - path: rtsp://10.0.0.2/ch2
          roles:
            - detect
    detect:
      enabled: true
  cam3:
    enabled: false
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.3/ch1
          roles:
            - record
`
	result := ParseFrigate([]byte(input))
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
	if len(result.Cameras) != 3 {
		t.Fatalf("expected 3 cameras, got %d", len(result.Cameras))
	}

	// Verify the disabled camera
	for _, c := range result.Cameras {
		if c.Name == "cam3" {
			if c.Enabled {
				t.Error("cam3 should be disabled")
			}
		}
	}
}

func TestParseFrigateEnabledDefaultTrue(t *testing.T) {
	t.Parallel()
	// When "enabled" is omitted, it defaults to true
	input := `
cameras:
  default_cam:
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.1/ch1
          roles:
            - record
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	if !result.Cameras[0].Enabled {
		t.Error("camera should be enabled by default when 'enabled' key is omitted")
	}
}

func TestParseFrigateRecordDetectDefaultFalse(t *testing.T) {
	t.Parallel()
	// When record.enabled and detect.enabled are omitted, they default to false
	input := `
cameras:
  noflags_cam:
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.1/ch1
          roles:
            - record
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	cam := result.Cameras[0]
	if cam.Record {
		t.Error("record should be false when record.enabled is omitted")
	}
	if cam.Detect {
		t.Error("detect should be false when detect.enabled is omitted")
	}
}

func TestParseFrigateSameMainAndSubCleared(t *testing.T) {
	t.Parallel()
	// When record and detect roles point to the same URL, sub-stream is cleared
	input := `
cameras:
  same_url_cam:
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.1/stream
          roles:
            - record
            - detect
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	cam := result.Cameras[0]
	if cam.SubStream != "" {
		t.Errorf("sub stream should be empty when same as main, got %q", cam.SubStream)
	}
}

func TestParseFrigateNoRoleFallback(t *testing.T) {
	t.Parallel()
	// When no input has a "record" role, the first input should be used as main
	input := `
cameras:
  norole_cam:
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.1/stream
          roles:
            - someother
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	cam := result.Cameras[0]
	if cam.MainStream != "rtsp://10.0.0.1/stream" {
		t.Errorf("main stream = %q, want first input as fallback", cam.MainStream)
	}
	// Should produce a warning about no 'record' role
	foundWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "no 'record' role found") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected warning about missing 'record' role")
	}
}

func TestParseFrigateNoInputsError(t *testing.T) {
	t.Parallel()
	input := `
cameras:
  empty_cam:
    ffmpeg:
      inputs: []
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 0 {
		t.Errorf("expected 0 cameras, got %d", len(result.Cameras))
	}
	if len(result.Errors) == 0 {
		t.Error("expected error for camera with no stream URL")
	}
	foundErr := false
	for _, e := range result.Errors {
		if strings.Contains(e, "no stream URL found") {
			foundErr = true
			break
		}
	}
	if !foundErr {
		t.Errorf("expected 'no stream URL found' error, got %v", result.Errors)
	}
}

func TestParseFrigateNoSubStreamWarning(t *testing.T) {
	t.Parallel()
	input := `
cameras:
  main_only:
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.1/main
          roles:
            - record
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	foundWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "no 'detect' role sub-stream found") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected warning about missing sub-stream")
	}
}

func TestParseFrigateInvalidYAML(t *testing.T) {
	t.Parallel()
	input := `
cameras:
  bad_indent:
  ffmpeg
    inputs: [
`
	result := ParseFrigate([]byte(input))
	if len(result.Errors) == 0 {
		t.Error("expected YAML parse error")
	}
	if !strings.Contains(result.Errors[0], "YAML parse error") {
		t.Errorf("error should mention YAML parse: %q", result.Errors[0])
	}
	if len(result.Cameras) != 0 {
		t.Errorf("expected 0 cameras for invalid YAML, got %d", len(result.Cameras))
	}
}

func TestParseFrigateEmptyConfig(t *testing.T) {
	t.Parallel()
	result := ParseFrigate([]byte(""))
	if len(result.Cameras) != 0 {
		t.Errorf("expected 0 cameras, got %d", len(result.Cameras))
	}
	if len(result.Errors) == 0 {
		t.Error("expected error for empty config")
	}
	foundErr := false
	for _, e := range result.Errors {
		if strings.Contains(e, "no cameras found") {
			foundErr = true
			break
		}
	}
	if !foundErr {
		t.Errorf("expected 'no cameras found' error, got %v", result.Errors)
	}
}

func TestParseFrigateNoCamerasSection(t *testing.T) {
	t.Parallel()
	input := `
mqtt:
  host: 10.0.0.1
detectors:
  cpu1:
    type: cpu
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 0 {
		t.Errorf("expected 0 cameras, got %d", len(result.Cameras))
	}
	if len(result.Errors) == 0 {
		t.Error("expected error for missing cameras section")
	}
}

func TestParseFrigateNonRTSPURL(t *testing.T) {
	t.Parallel()
	// HTTP URLs should not have ONVIF host extracted
	input := `
cameras:
  http_cam:
    ffmpeg:
      inputs:
        - path: http://10.0.0.1/video
          roles:
            - record
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	cam := result.Cameras[0]
	if cam.ONVIFHost != "" {
		t.Errorf("ONVIF host should be empty for non-RTSP URL, got %q", cam.ONVIFHost)
	}
}

func TestParseFrigateRTSPSScheme(t *testing.T) {
	t.Parallel()
	// rtsps:// URLs should also extract ONVIF host
	input := `
cameras:
  secure_cam:
    ffmpeg:
      inputs:
        - path: rtsps://user:pass@10.0.0.1:322/stream
          roles:
            - record
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	cam := result.Cameras[0]
	if cam.ONVIFHost != "10.0.0.1" {
		t.Errorf("ONVIF host = %q, want %q", cam.ONVIFHost, "10.0.0.1")
	}
	if cam.ONVIFUser != "user" {
		t.Errorf("ONVIF user = %q, want %q", cam.ONVIFUser, "user")
	}
}

func TestParseFrigateEmptyPathInputSkipped(t *testing.T) {
	t.Parallel()
	// An input with empty path should be skipped
	input := `
cameras:
  skip_empty:
    ffmpeg:
      inputs:
        - path: ""
          roles:
            - record
        - path: rtsp://10.0.0.1/real
          roles:
            - record
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	if result.Cameras[0].MainStream != "rtsp://10.0.0.1/real" {
		t.Errorf("main stream = %q, want non-empty path", result.Cameras[0].MainStream)
	}
}

func TestParseFrigateFirstRecordRoleWins(t *testing.T) {
	t.Parallel()
	// If multiple inputs have the "record" role, the first one should be used
	input := `
cameras:
  multi_record:
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.1/first
          roles:
            - record
        - path: rtsp://10.0.0.1/second
          roles:
            - record
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	if result.Cameras[0].MainStream != "rtsp://10.0.0.1/first" {
		t.Errorf("main stream = %q, want first record-role path", result.Cameras[0].MainStream)
	}
}

func TestParseFrigateCameraNameSanitized(t *testing.T) {
	t.Parallel()
	// Camera names with invalid characters should be sanitized
	input := `
cameras:
  "Front Door (2nd Floor)":
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.1/ch1
          roles:
            - record
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	cam := result.Cameras[0]
	// Parentheses and other special chars should be replaced with underscores
	if strings.ContainsAny(cam.Name, "()") {
		t.Errorf("name should not contain parentheses, got %q", cam.Name)
	}
	if len(cam.Name) == 0 {
		t.Error("sanitized name should not be empty")
	}
}

func TestParseFrigateAllCamerasInvalid(t *testing.T) {
	t.Parallel()
	// All cameras fail validation — each produces a per-camera error.
	// The final "no valid cameras found" fallback only fires when there are
	// zero errors (the guard is len(Errors) == 0), so with per-camera errors
	// already present, the fallback is NOT appended.
	input := `
cameras:
  bad1:
    ffmpeg:
      inputs: []
  bad2:
    ffmpeg:
      inputs: []
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 0 {
		t.Errorf("expected 0 cameras, got %d", len(result.Cameras))
	}
	if len(result.Errors) != 2 {
		t.Errorf("expected 2 per-camera errors, got %d: %v", len(result.Errors), result.Errors)
	}
	for _, e := range result.Errors {
		if !strings.Contains(e, "no stream URL found") {
			t.Errorf("expected 'no stream URL found' error, got %q", e)
		}
	}
}

func TestParseFrigateNoCredentialsInURL(t *testing.T) {
	t.Parallel()
	input := `
cameras:
  nocred:
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.1/stream
          roles:
            - record
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	cam := result.Cameras[0]
	if cam.ONVIFUser != "" {
		t.Errorf("ONVIF user should be empty for URL without credentials, got %q", cam.ONVIFUser)
	}
	if cam.ONVIFPass != "" {
		t.Errorf("ONVIF pass should be empty for URL without credentials, got %q", cam.ONVIFPass)
	}
}

func TestParseFrigateMixedValidAndInvalid(t *testing.T) {
	t.Parallel()
	input := `
cameras:
  good:
    ffmpeg:
      inputs:
        - path: rtsp://10.0.0.1/main
          roles:
            - record
  bad:
    ffmpeg:
      inputs: []
`
	result := ParseFrigate([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 valid camera, got %d", len(result.Cameras))
	}
	if result.Cameras[0].Name != "good" {
		t.Errorf("name = %q, want %q", result.Cameras[0].Name, "good")
	}
	if len(result.Errors) == 0 {
		t.Error("expected error for the invalid camera")
	}
}
