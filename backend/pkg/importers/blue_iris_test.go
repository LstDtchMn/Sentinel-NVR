package importers

import (
	"strings"
	"testing"
)

func TestParseBlueIrisValidSingleCamera(t *testing.T) {
	t.Parallel()
	input := `Windows Registry Editor Version 5.00

[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{ABC-123}]
"shortname"="Front Door"
"ip"="192.168.1.100"
"port"=dword:0000022a
"main_url"="/Streaming/Channels/101"
"sub_url"="/Streaming/Channels/102"
"user"="admin"
"pw"="secret123"
"enable"=dword:00000001
"record"=dword:00000001
`
	result := ParseBlueIris([]byte(input))
	if result.Format != "blue_iris" {
		t.Errorf("format = %q, want %q", result.Format, "blue_iris")
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	cam := result.Cameras[0]
	if cam.Name != "Front Door" {
		t.Errorf("name = %q, want %q", cam.Name, "Front Door")
	}
	if !cam.Enabled {
		t.Error("expected enabled=true")
	}
	if !cam.Record {
		t.Error("expected record=true")
	}
	if cam.Detect {
		t.Error("expected detect=false for Blue Iris imports")
	}
	// Port 554 (0x22a = 554) → standard RTSP, no port in URL
	if !strings.Contains(cam.MainStream, "192.168.1.100") {
		t.Errorf("main stream should contain IP, got %q", cam.MainStream)
	}
	if !strings.HasPrefix(cam.MainStream, "rtsp://") {
		t.Errorf("main stream should start with rtsp://, got %q", cam.MainStream)
	}
	if !strings.Contains(cam.MainStream, "/Streaming/Channels/101") {
		t.Errorf("main stream should contain path, got %q", cam.MainStream)
	}
	if cam.SubStream == "" {
		t.Error("expected sub stream to be set")
	}
	if cam.ONVIFHost != "192.168.1.100" {
		t.Errorf("onvif host = %q, want %q", cam.ONVIFHost, "192.168.1.100")
	}
	if cam.ONVIFUser != "admin" {
		t.Errorf("onvif user = %q, want %q", cam.ONVIFUser, "admin")
	}
	if cam.ONVIFPass != "secret123" {
		t.Errorf("onvif pass = %q, want %q", cam.ONVIFPass, "secret123")
	}
	if cam.ONVIFPort != 80 {
		t.Errorf("onvif port = %d, want %d", cam.ONVIFPort, 80)
	}
}

func TestParseBlueIrisNonStandardPort(t *testing.T) {
	t.Parallel()
	// Port 0x230 = 560 (non-standard), should appear in URL
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{D1}]
"shortname"="Garage"
"ip"="10.0.0.5"
"port"=dword:00000230
"main_url"="/live"
"user"="user1"
"pw"="pass1"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	cam := result.Cameras[0]
	// 0x230 = 560, which != 554, so the port must be in the URL
	if !strings.Contains(cam.MainStream, ":560") {
		t.Errorf("main stream should contain non-standard port :560, got %q", cam.MainStream)
	}
}

func TestParseBlueIrisStandardPortOmitted(t *testing.T) {
	t.Parallel()
	// Port 0x22a = 554 (standard RTSP), should NOT appear in URL
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{D2}]
"shortname"="Front"
"ip"="10.0.0.5"
"port"=dword:0000022a
"main_url"="/live"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	cam := result.Cameras[0]
	if strings.Contains(cam.MainStream, ":554") {
		t.Errorf("standard port 554 should be omitted from URL, got %q", cam.MainStream)
	}
}

func TestParseBlueIrisCRLFLineEndings(t *testing.T) {
	t.Parallel()
	input := "[HKEY_LOCAL_MACHINE\\SOFTWARE\\Perspective Software\\Blue Iris\\Cameras\\{W1}]\r\n" +
		"\"shortname\"=\"WindowsCam\"\r\n" +
		"\"ip\"=\"192.168.1.1\"\r\n" +
		"\"main_url\"=\"/stream\"\r\n"
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	if result.Cameras[0].Name != "WindowsCam" {
		t.Errorf("name = %q, want %q", result.Cameras[0].Name, "WindowsCam")
	}
}

func TestParseBlueIrisMultipleCameras(t *testing.T) {
	t.Parallel()
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{AAA}]
"shortname"="Cam1"
"ip"="10.0.0.1"
"main_url"="/stream1"
"enable"=dword:00000001

[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{BBB}]
"shortname"="Cam2"
"ip"="10.0.0.2"
"main_url"="/stream2"
"enable"=dword:00000000

[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{CCC}]
"shortname"="Cam3"
"ip"="10.0.0.3"
"main_url"="/stream3"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
	if len(result.Cameras) != 3 {
		t.Fatalf("expected 3 cameras, got %d", len(result.Cameras))
	}
	// Check that the disabled camera (Cam2) has enabled=false
	found := false
	for _, c := range result.Cameras {
		if c.Name == "Cam2" {
			found = true
			if c.Enabled {
				t.Error("Cam2 should be disabled (enable=0)")
			}
		}
	}
	if !found {
		t.Error("Cam2 not found in results")
	}
}

func TestParseBlueIrisEmptyFile(t *testing.T) {
	t.Parallel()
	result := ParseBlueIris([]byte(""))
	if len(result.Cameras) != 0 {
		t.Errorf("expected 0 cameras, got %d", len(result.Cameras))
	}
	if len(result.Errors) == 0 {
		t.Error("expected error for empty file")
	}
	if result.Errors[0] != "no camera entries found in .reg file" {
		t.Errorf("unexpected error: %q", result.Errors[0])
	}
}

func TestParseBlueIrisNoRelevantKeys(t *testing.T) {
	t.Parallel()
	input := `Windows Registry Editor Version 5.00

[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\SomeOtherSection]
"key"="value"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 0 {
		t.Errorf("expected 0 cameras, got %d", len(result.Cameras))
	}
	if len(result.Errors) == 0 {
		t.Error("expected error for no camera entries")
	}
}

func TestParseBlueIrisMissingIP(t *testing.T) {
	t.Parallel()
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{NO-IP}]
"shortname"="No IP Camera"
"main_url"="/stream"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 0 {
		t.Errorf("expected 0 cameras (missing IP), got %d", len(result.Cameras))
	}
	if len(result.Errors) == 0 {
		t.Error("expected error about missing IP")
	}
	if !strings.Contains(result.Errors[0], "no IP address found") {
		t.Errorf("error should mention missing IP: %q", result.Errors[0])
	}
}

func TestParseBlueIrisMissingMainURL(t *testing.T) {
	t.Parallel()
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{NO-URL}]
"shortname"="No URL Camera"
"ip"="192.168.1.1"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 0 {
		t.Errorf("expected 0 cameras (missing URL), got %d", len(result.Cameras))
	}
	if len(result.Errors) == 0 {
		t.Error("expected error about missing main stream")
	}
	if !strings.Contains(result.Errors[0], "no main stream path found") {
		t.Errorf("error should mention missing stream: %q", result.Errors[0])
	}
}

func TestParseBlueIrisMissingShortnameUsesIP(t *testing.T) {
	t.Parallel()
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{IP-NAME}]
"ip"="10.0.0.50"
"main_url"="/ch1"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	// Name should be based on the IP (sanitized)
	if !strings.Contains(result.Cameras[0].Name, "10") {
		t.Errorf("expected name derived from IP, got %q", result.Cameras[0].Name)
	}
}

func TestParseBlueIrisEncodedPassword(t *testing.T) {
	t.Parallel()
	// Password starting with "$" looks encoded — should trigger warning
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{ENC}]
"shortname"="EncodedPw"
"ip"="10.0.0.1"
"main_url"="/live"
"user"="admin"
"pw"="$2a$10$encodedPasswordHash"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	cam := result.Cameras[0]
	if cam.ONVIFPass != "" {
		t.Errorf("encoded password should be empty, got %q", cam.ONVIFPass)
	}
	foundWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "password appears encoded") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected warning about encoded password")
	}
}

func TestParseBlueIrisLongPassword(t *testing.T) {
	t.Parallel()
	// Password >= 128 chars looks encoded
	longPw := strings.Repeat("x", 128)
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{LONG}]
"shortname"="LongPw"
"ip"="10.0.0.1"
"main_url"="/live"
"user"="admin"
"pw"="` + longPw + `"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	if result.Cameras[0].ONVIFPass != "" {
		t.Error("long password (>=128 chars) should be treated as encoded")
	}
}

func TestParseBlueIrisPasswordWithSpecialChars(t *testing.T) {
	t.Parallel()
	// regStrRE uses "(.*)" — passwords can contain double quotes
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{SPEC}]
"shortname"="SpecialPw"
"ip"="10.0.0.1"
"main_url"="/live"
"user"="admin"
"pw"="p@ss:w/ord!#$"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	cam := result.Cameras[0]
	// Password with special chars but < 128 and no $ prefix → should be kept
	// Note: "p@ss:w/ord!#$" does NOT start with "$" (it starts with "p")
	if cam.ONVIFPass != `p@ss:w/ord!#$` {
		t.Errorf("password = %q, want %q", cam.ONVIFPass, `p@ss:w/ord!#$`)
	}
	// Credentials with special chars must be percent-encoded in the RTSP URL
	if !strings.Contains(cam.MainStream, "rtsp://admin:") {
		t.Errorf("main stream should contain encoded credentials, got %q", cam.MainStream)
	}
}

func TestParseBlueIrisSubKeyNotMatched(t *testing.T) {
	t.Parallel()
	// regKeyRE uses [^\\]+ to prevent matching sub-keys like {GUID}\Settings.
	// A sub-key line does NOT match regKeyRE, so it does NOT create a new camera
	// entry. However, since it also doesn't reset currentGUID, any values that
	// follow the sub-key line are applied to the PREVIOUS camera entry.
	// This test verifies that sub-keys do not create SEPARATE camera entries.
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{CAM1}]
"shortname"="RealCam"
"ip"="10.0.0.1"
"main_url"="/stream"

[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{CAM1}\Settings]
"shortname"="OverriddenName"

[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{CAM2}]
"shortname"="SecondCam"
"ip"="10.0.0.2"
"main_url"="/stream2"
`
	result := ParseBlueIris([]byte(input))
	// Sub-key should NOT produce a separate camera — only {CAM1} and {CAM2} exist.
	if len(result.Cameras) != 2 {
		t.Fatalf("expected 2 cameras (sub-key should not create separate entry), got %d; errors=%v",
			len(result.Cameras), result.Errors)
	}
}

func TestRegKeyRESubKeyDoesNotMatch(t *testing.T) {
	t.Parallel()
	// Verify regKeyRE rejects sub-key patterns directly
	subKey := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{ABC}\Settings]`
	m := regKeyRE.FindStringSubmatch(subKey)
	if m != nil {
		t.Errorf("sub-key should not match regKeyRE, got %v", m)
	}

	// Direct key should match
	directKey := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{ABC}]`
	m = regKeyRE.FindStringSubmatch(directKey)
	if m == nil {
		t.Fatal("direct key should match regKeyRE")
	}
	if m[1] != "{ABC}" {
		t.Errorf("captured GUID = %q, want %q", m[1], "{ABC}")
	}
}

func TestParseBlueIrisCommentLines(t *testing.T) {
	t.Parallel()
	input := `; This is a comment
; Another comment
[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{C1}]
; inline comment line
"shortname"="CommentCam"
"ip"="10.0.0.1"
"main_url"="/stream"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
}

func TestParseBlueIrisDefaultEnabledWhenNotSet(t *testing.T) {
	t.Parallel()
	// When "enable" is not set (camEntry.enabled == -1), camera should be enabled by default
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{DEF}]
"shortname"="DefaultEnabled"
"ip"="10.0.0.1"
"main_url"="/stream"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	if !result.Cameras[0].Enabled {
		t.Error("camera should be enabled by default when 'enable' key is not present")
	}
}

func TestParseBlueIrisDefaultRecordWhenNotSet(t *testing.T) {
	t.Parallel()
	// When "record" is not set (camEntry.record == -1), record should be false (explicit opt-in)
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{REC}]
"shortname"="NoRecord"
"ip"="10.0.0.1"
"main_url"="/stream"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	if result.Cameras[0].Record {
		t.Error("camera should have record=false when 'record' key is not present")
	}
}

func TestParseBlueIrisAlternateKeyNames(t *testing.T) {
	t.Parallel()
	// Test "short", "address", "rtsp_path", "rtsp_path_alt", "username", "password", "enabled"
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{ALT}]
"short"="AltNameCam"
"address"="10.0.0.2"
"rtsp_path"="/ch1"
"rtsp_path_alt"="/ch2"
"username"="viewer"
"password"="pass"
"enabled"=dword:00000001
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	cam := result.Cameras[0]
	if cam.Name != "AltNameCam" {
		t.Errorf("name = %q, want %q", cam.Name, "AltNameCam")
	}
	if cam.SubStream == "" {
		t.Error("sub stream should be set from rtsp_path_alt")
	}
	if cam.ONVIFUser != "viewer" {
		t.Errorf("onvif user = %q, want %q", cam.ONVIFUser, "viewer")
	}
}

func TestParseBlueIrisNoSubStreamWarning(t *testing.T) {
	t.Parallel()
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{NOSUB}]
"shortname"="NoSubCam"
"ip"="10.0.0.1"
"main_url"="/stream"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	foundWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "no sub-stream path found") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected warning about missing sub-stream")
	}
}

func TestParseBlueIrisEmptyEntrySkipped(t *testing.T) {
	t.Parallel()
	// An entry with no shortname and no IP (e.g. a parent key) should be skipped
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{EMPTY}]
"record"=dword:00000001

[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{REAL}]
"shortname"="RealCam"
"ip"="10.0.0.1"
"main_url"="/stream"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera (empty entry skipped), got %d", len(result.Cameras))
	}
	if result.Cameras[0].Name != "RealCam" {
		t.Errorf("name = %q, want %q", result.Cameras[0].Name, "RealCam")
	}
}

func TestParseBlueIrisDuplicateGUID(t *testing.T) {
	t.Parallel()
	// Same GUID appearing twice — later values should overwrite earlier ones
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{DUP}]
"shortname"="First"
"ip"="10.0.0.1"
"main_url"="/stream1"

[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{DUP}]
"shortname"="Second"
`
	result := ParseBlueIris([]byte(input))
	// The second key section re-sets currentGUID to {DUP}, but doesn't create a new entry.
	// It updates the existing entry's shortname to "Second".
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera for duplicate GUID, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	if result.Cameras[0].Name != "Second" {
		t.Errorf("name = %q, want %q (second value should win)", result.Cameras[0].Name, "Second")
	}
}

func TestParseBlueIrisCredentialsPercentEncoded(t *testing.T) {
	t.Parallel()
	// Credentials with @ : / characters must be properly escaped in RTSP URL
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{CRED}]
"shortname"="EncodedCreds"
"ip"="10.0.0.1"
"main_url"="/stream"
"user"="user@domain"
"pw"="p@ss:w0rd"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d; errors=%v", len(result.Cameras), result.Errors)
	}
	cam := result.Cameras[0]
	// The URL should be parseable — special chars must be percent-encoded
	if !strings.Contains(cam.MainStream, "rtsp://") {
		t.Errorf("main stream should be valid RTSP URL, got %q", cam.MainStream)
	}
	// @ in username should be encoded so URL parsing doesn't break
	if strings.Count(cam.MainStream, "@") != 1 {
		t.Errorf("expected exactly 1 unescaped @ (separating userinfo from host), got %q", cam.MainStream)
	}
}

func TestParseBlueIrisNoCredentials(t *testing.T) {
	t.Parallel()
	input := `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{NOCRED}]
"shortname"="NoCreds"
"ip"="10.0.0.1"
"main_url"="/stream"
`
	result := ParseBlueIris([]byte(input))
	if len(result.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(result.Cameras))
	}
	cam := result.Cameras[0]
	// URL should have no userinfo section
	if strings.Contains(cam.MainStream, "@") {
		t.Errorf("URL without credentials should have no @, got %q", cam.MainStream)
	}
	if cam.MainStream != "rtsp://10.0.0.1/stream" {
		t.Errorf("main stream = %q, want %q", cam.MainStream, "rtsp://10.0.0.1/stream")
	}
}

func TestBuildRTSPURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		user     string
		pass     string
		host     string
		port     int
		path     string
		wantURL  string
		wantEmpty bool
	}{
		{
			name:    "full URL with non-standard port",
			user:    "admin",
			pass:    "pass",
			host:    "10.0.0.1",
			port:    8554,
			path:    "/live",
			wantURL: "rtsp://admin:pass@10.0.0.1:8554/live",
		},
		{
			name:    "standard port omitted",
			user:    "admin",
			pass:    "pass",
			host:    "10.0.0.1",
			port:    554,
			path:    "/live",
			wantURL: "rtsp://admin:pass@10.0.0.1/live",
		},
		{
			name:    "zero port treated as standard",
			user:    "",
			pass:    "",
			host:    "10.0.0.1",
			port:    0,
			path:    "/live",
			wantURL: "rtsp://10.0.0.1/live",
		},
		{
			name:    "no credentials",
			user:    "",
			pass:    "",
			host:    "10.0.0.1",
			port:    554,
			path:    "/live",
			wantURL: "rtsp://10.0.0.1/live",
		},
		{
			name:    "user only no password",
			user:    "viewer",
			pass:    "",
			host:    "10.0.0.1",
			port:    554,
			path:    "/live",
			wantURL: "rtsp://viewer@10.0.0.1/live",
		},
		{
			name:    "path without leading slash",
			user:    "",
			pass:    "",
			host:    "10.0.0.1",
			port:    554,
			path:    "stream",
			wantURL: "rtsp://10.0.0.1/stream",
		},
		{
			name:      "empty host",
			user:      "admin",
			pass:      "pass",
			host:      "",
			port:      554,
			path:      "/live",
			wantEmpty: true,
		},
		{
			name:      "empty path",
			user:      "admin",
			pass:      "pass",
			host:      "10.0.0.1",
			port:      554,
			path:      "",
			wantEmpty: true,
		},
		{
			name:      "both empty",
			user:      "",
			pass:      "",
			host:      "",
			port:      0,
			path:      "",
			wantEmpty: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildRTSPURL(tt.user, tt.pass, tt.host, tt.port, tt.path)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("buildRTSPURL() = %q, want empty", got)
				}
				return
			}
			if got != tt.wantURL {
				t.Errorf("buildRTSPURL() = %q, want %q", got, tt.wantURL)
			}
		})
	}
}

func TestRegKeyRESubKeyRejection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		line    string
		wantNil bool
	}{
		{
			name:    "direct camera key",
			line:    `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{ABC-123}]`,
			wantNil: false,
		},
		{
			name:    "sub-key should not match",
			line:    `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{ABC-123}\Settings]`,
			wantNil: true,
		},
		{
			name:    "deeper sub-key should not match",
			line:    `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Cameras\{ABC-123}\Settings\Advanced]`,
			wantNil: true,
		},
		{
			name:    "non-camera key",
			line:    `[HKEY_LOCAL_MACHINE\SOFTWARE\Perspective Software\Blue Iris\Global]`,
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := regKeyRE.FindStringSubmatch(tt.line)
			if tt.wantNil && m != nil {
				t.Errorf("expected no match for %q, got %v", tt.line, m)
			}
			if !tt.wantNil && m == nil {
				t.Errorf("expected match for %q, got nil", tt.line)
			}
		})
	}
}

func TestRegStrREQuotedPasswords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		wantKey  string
		wantVal  string
		wantNil  bool
	}{
		{
			name:    "simple value",
			line:    `"user"="admin"`,
			wantKey: "user",
			wantVal: "admin",
		},
		{
			name:    "value with special chars",
			line:    `"pw"="p@ss:w/ord!#$"`,
			wantKey: "pw",
			wantVal: `p@ss:w/ord!#$`,
		},
		{
			name:    "empty value",
			line:    `"shortname"=""`,
			wantKey: "shortname",
			wantVal: "",
		},
		{
			name:    "value with spaces",
			line:    `"shortname"="Front Door Camera"`,
			wantKey: "shortname",
			wantVal: "Front Door Camera",
		},
		{
			name:    "not a string value",
			line:    `"port"=dword:00000230`,
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := regStrRE.FindStringSubmatch(tt.line)
			if tt.wantNil {
				if m != nil {
					t.Errorf("expected no match for %q, got %v", tt.line, m)
				}
				return
			}
			if m == nil {
				t.Fatalf("expected match for %q, got nil", tt.line)
			}
			if m[1] != tt.wantKey {
				t.Errorf("key = %q, want %q", m[1], tt.wantKey)
			}
			if m[2] != tt.wantVal {
				t.Errorf("val = %q, want %q", m[2], tt.wantVal)
			}
		})
	}
}
