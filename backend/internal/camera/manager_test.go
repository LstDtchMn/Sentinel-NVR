package camera

import (
	"encoding/json"
	"testing"
)

func TestValidCameraName(t *testing.T) {
	valid := []string{
		"a",
		"FrontDoor",
		"front_door",
		"camera-1",
		"Camera 1",
		"a1",
		"abc123_-ABC",
	}
	for _, name := range valid {
		if !validCameraName.MatchString(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}

	invalid := []string{
		"",
		" leading-space",
		"trailing-space ",
		"-starts-with-dash",
		"_starts_with_underscore",
		"has@special",
		"has/slash",
		// 65 chars (max is 64)
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	for _, name := range invalid {
		if validCameraName.MatchString(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"Front Door", "front_door"},
		{"camera-1", "camera-1"},
		{"CAMERA 2", "camera_2"},
		{"already_sanitized", "already_sanitized"},
	}
	for _, tt := range tests {
		got := SanitizeName(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRedactStreamURL(t *testing.T) {
	// With credentials: must strip user/pass, keep host
	got := RedactStreamURL("rtsp://admin:password@192.168.1.100:554/stream")
	if contains(got, "password") {
		t.Errorf("RedactStreamURL leaked password: %q", got)
	}
	if contains(got, "admin") {
		t.Errorf("RedactStreamURL leaked username: %q", got)
	}
	if !contains(got, "192.168.1.100") {
		t.Errorf("RedactStreamURL should preserve host: %q", got)
	}

	// Without credentials: returned unchanged
	noAuth := "rtsp://192.168.1.100:554/stream"
	if got := RedactStreamURL(noAuth); got != noAuth {
		t.Errorf("RedactStreamURL(%q) = %q, want unchanged", noAuth, got)
	}

	// Truly invalid URL (Go url.Parse is very lenient, so use an unparseable one)
	got = RedactStreamURL("://")
	if got != "<invalid-url>" {
		t.Errorf("RedactStreamURL(invalid) = %q, want %q", got, "<invalid-url>")
	}
}

// TODO(review): L17 — replace with strings.Contains (edge-case difference on empty substr)
func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && containsHelper(s, substr)
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestValidateCameraInput(t *testing.T) {
	// Valid camera
	valid := &CameraRecord{
		Name:       "FrontDoor",
		MainStream: "rtsp://192.168.1.100:554/stream",
	}
	if err := ValidateCameraInput(valid); err != nil {
		t.Errorf("valid camera failed: %v", err)
	}

	// Missing name
	noName := &CameraRecord{MainStream: "rtsp://a:554/s"}
	if err := ValidateCameraInput(noName); err == nil {
		t.Error("empty name should fail")
	}

	// Invalid name pattern
	badName := &CameraRecord{Name: " invalid", MainStream: "rtsp://a:554/s"}
	if err := ValidateCameraInput(badName); err == nil {
		t.Error("name starting with space should fail")
	}

	// Missing main stream
	noStream := &CameraRecord{Name: "test"}
	if err := ValidateCameraInput(noStream); err == nil {
		t.Error("empty main_stream should fail")
	}

	// HTTP MJPEG stream (e.g. older IP camera CGI endpoint)
	httpCam := &CameraRecord{Name: "test", MainStream: "http://192.168.1.1/video.cgi"}
	if err := ValidateCameraInput(httpCam); err != nil {
		t.Errorf("http MJPEG stream should be valid: %v", err)
	}

	// HTTPS MJPEG stream
	httpsCam := &CameraRecord{Name: "test", MainStream: "https://192.168.1.1/video.cgi"}
	if err := ValidateCameraInput(httpsCam); err != nil {
		t.Errorf("https MJPEG stream should be valid: %v", err)
	}

	// Still-invalid protocol
	badProto := &CameraRecord{Name: "test", MainStream: "ftp://192.168.1.1/stream"}
	if err := ValidateCameraInput(badProto); err == nil {
		t.Error("ftp protocol should fail")
	}

	// Valid with sub-stream
	withSub := &CameraRecord{
		Name:       "test",
		MainStream: "rtsp://192.168.1.100:554/main",
		SubStream:  "rtsp://192.168.1.100:554/sub",
	}
	if err := ValidateCameraInput(withSub); err != nil {
		t.Errorf("valid camera with sub-stream failed: %v", err)
	}

	// Invalid sub-stream
	badSub := &CameraRecord{
		Name:       "test",
		MainStream: "rtsp://192.168.1.100:554/main",
		SubStream:  "ftp://192.168.1.100/sub",
	}
	if err := ValidateCameraInput(badSub); err == nil {
		t.Error("ftp sub-stream should fail")
	}

	// Invalid ONVIF port
	badPort := &CameraRecord{
		Name:       "test",
		MainStream: "rtsp://a:554/s",
		ONVIFPort:  -1,
	}
	if err := ValidateCameraInput(badPort); err == nil {
		t.Error("negative ONVIF port should fail")
	}
}

func TestNormalizedJSON(t *testing.T) {
	// Same content, different formatting
	a := json.RawMessage(`{"a":1,"b":2}`)
	b := json.RawMessage(`{  "b" : 2, "a" : 1  }`)

	na := normalizedJSON(a)
	nb := normalizedJSON(b)

	if na != nb {
		t.Errorf("normalized JSON should be equal: %q vs %q", na, nb)
	}
}

func TestNormalizedJSONInvalid(t *testing.T) {
	raw := json.RawMessage(`not-json`)
	result := normalizedJSON(raw)
	if result != "not-json" {
		t.Errorf("invalid JSON should fall back to raw: got %q", result)
	}
}
