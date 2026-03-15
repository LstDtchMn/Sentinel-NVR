package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// validConfig returns a Config with defaults that pass Validate on any OS.
// On Windows, default Unix paths like /media/hot are not absolute,
// so we override them with temp-dir-based paths.
func validConfig(t *testing.T) *Config {
	t.Helper()
	cfg := &Config{}
	setDefaults(cfg)
	if runtime.GOOS == "windows" {
		dir := t.TempDir()
		cfg.Storage.HotPath = filepath.Join(dir, "hot")
		cfg.Detection.SnapshotPath = filepath.Join(dir, "snapshots")
		cfg.Database.Path = filepath.Join(dir, "sentinel.db")
	}
	return cfg
}

func TestSetDefaults(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.Port != 8099 {
		t.Errorf("Server.Port = %d, want 8099", cfg.Server.Port)
	}
	if cfg.Server.LogLevel != "info" {
		t.Errorf("Server.LogLevel = %q, want %q", cfg.Server.LogLevel, "info")
	}
	if cfg.Storage.HotPath != "/media/hot" {
		t.Errorf("Storage.HotPath = %q, want %q", cfg.Storage.HotPath, "/media/hot")
	}
	if cfg.Storage.SegmentDuration != 10 {
		t.Errorf("Storage.SegmentDuration = %d, want 10", cfg.Storage.SegmentDuration)
	}
	if cfg.Detection.Backend != "remote" {
		t.Errorf("Detection.Backend = %q, want %q", cfg.Detection.Backend, "remote")
	}
	if cfg.Detection.FrameInterval != 1 {
		t.Errorf("Detection.FrameInterval = %d, want 1", cfg.Detection.FrameInterval)
	}
	if cfg.Auth.AccessTokenTTL != 900 {
		t.Errorf("Auth.AccessTokenTTL = %d, want 900", cfg.Auth.AccessTokenTTL)
	}
	if cfg.Auth.RefreshTokenTTL != 604800 {
		t.Errorf("Auth.RefreshTokenTTL = %d, want 604800", cfg.Auth.RefreshTokenTTL)
	}
	if cfg.Detection.FaceRecognition.MatchThreshold == nil || *cfg.Detection.FaceRecognition.MatchThreshold != 0.6 {
		t.Errorf("FaceRecognition.MatchThreshold = %v, want 0.6", cfg.Detection.FaceRecognition.MatchThreshold)
	}
	if cfg.Detection.AudioClassification.ConfidenceThreshold == nil || *cfg.Detection.AudioClassification.ConfidenceThreshold != 0.7 {
		t.Errorf("AudioClassification.ConfidenceThreshold = %v, want 0.7", cfg.Detection.AudioClassification.ConfidenceThreshold)
	}
	if cfg.Detection.AudioClassification.SampleInterval != 3 {
		t.Errorf("AudioClassification.SampleInterval = %d, want 3", cfg.Detection.AudioClassification.SampleInterval)
	}
}

func TestValidateMinimal(t *testing.T) {
	cfg := validConfig(t)
	if err := Validate(cfg); err != nil {
		t.Errorf("minimal config with defaults should validate: %v", err)
	}
}

func TestValidatePortRange(t *testing.T) {
	cfg := validConfig(t)

	cfg.Server.Port = 0
	if err := Validate(cfg); err == nil {
		t.Error("port 0 should fail validation")
	}

	cfg.Server.Port = 70000
	if err := Validate(cfg); err == nil {
		t.Error("port 70000 should fail validation")
	}

	cfg.Server.Port = 8099
	if err := Validate(cfg); err != nil {
		t.Errorf("port 8099 should pass: %v", err)
	}
}

func TestValidateLogLevel(t *testing.T) {
	cfg := validConfig(t)

	for _, level := range []string{"debug", "info", "warn", "error"} {
		cfg.Server.LogLevel = level
		if err := Validate(cfg); err != nil {
			t.Errorf("log level %q should be valid: %v", level, err)
		}
	}

	cfg.Server.LogLevel = "trace"
	if err := Validate(cfg); err == nil {
		t.Error("log level 'trace' should fail validation")
	}
}

func TestValidateSegmentDuration(t *testing.T) {
	cfg := validConfig(t)

	cfg.Storage.SegmentDuration = 0
	if err := Validate(cfg); err == nil {
		t.Error("segment duration 0 should fail")
	}

	cfg.Storage.SegmentDuration = 61
	if err := Validate(cfg); err == nil {
		t.Error("segment duration 61 should fail (max 60)")
	}

	cfg.Storage.SegmentDuration = 10
	if err := Validate(cfg); err != nil {
		t.Errorf("segment duration 10 should pass: %v", err)
	}
}

func TestValidateDetectionThreshold(t *testing.T) {
	cfg := validConfig(t)

	below := -0.1
	cfg.Detection.ConfidenceThreshold = &below
	if err := Validate(cfg); err == nil {
		t.Error("threshold -0.1 should fail")
	}

	above := 1.5
	cfg.Detection.ConfidenceThreshold = &above
	if err := Validate(cfg); err == nil {
		t.Error("threshold 1.5 should fail")
	}

	valid := 0.5
	cfg.Detection.ConfidenceThreshold = &valid
	if err := Validate(cfg); err != nil {
		t.Errorf("threshold 0.5 should pass: %v", err)
	}

	// nil threshold should use default (0.6)
	cfg.Detection.ConfidenceThreshold = nil
	if err := Validate(cfg); err != nil {
		t.Errorf("nil threshold should pass: %v", err)
	}
}

func TestValidateDuplicateCameraNames(t *testing.T) {
	cfg := validConfig(t)
	cfg.Cameras = []CameraConfig{
		{Name: "front", Enabled: true, MainStream: "rtsp://a"},
		{Name: "front", Enabled: true, MainStream: "rtsp://b"},
	}
	if err := Validate(cfg); err == nil {
		t.Error("duplicate camera names should fail validation")
	}
}

func TestValidateEmptyCameraName(t *testing.T) {
	cfg := validConfig(t)
	cfg.Cameras = []CameraConfig{
		{Name: "", Enabled: true, MainStream: "rtsp://a"},
	}
	if err := Validate(cfg); err == nil {
		t.Error("empty camera name should fail validation")
	}
}

func TestValidateRelayConfig(t *testing.T) {
	cfg := validConfig(t)
	cfg.Relay.Enabled = true

	if err := Validate(cfg); err == nil {
		t.Error("relay enabled without turn_server should fail")
	}

	cfg.Relay.TURNServer = "turn:coturn:3478"
	cfg.Relay.TURNUser = "sentinel"
	cfg.Relay.TURNPass = "changeme"
	if err := Validate(cfg); err == nil {
		t.Error("turn_pass='changeme' should fail validation")
	}

	cfg.Relay.TURNPass = "secure-password"
	if err := Validate(cfg); err != nil {
		t.Errorf("valid relay config should pass: %v", err)
	}
}

func TestConfidenceThresholdValue(t *testing.T) {
	cfg := DetectionConfig{}
	if v := cfg.ConfidenceThresholdValue(); v != 0.6 {
		t.Errorf("default threshold = %f, want 0.6", v)
	}

	custom := 0.8
	cfg.ConfidenceThreshold = &custom
	if v := cfg.ConfidenceThresholdValue(); v != 0.8 {
		t.Errorf("custom threshold = %f, want 0.8", v)
	}
}

func TestLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yml")

	// Write a minimal config file
	content := `
server:
  port: 9000
  log_level: debug
go2rtc:
  api_url: http://localhost:1984
  rtsp_url: rtsp://localhost:8554
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("Server.Port = %d, want 9000", cfg.Server.Port)
	}
	if cfg.Server.LogLevel != "debug" {
		t.Errorf("Server.LogLevel = %q, want %q", cfg.Server.LogLevel, "debug")
	}

	// Save and reload
	savePath := filepath.Join(dir, "saved.yml")
	if err := Save(savePath, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	reloaded, err := Load(savePath)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if reloaded.Server.Port != cfg.Server.Port {
		t.Errorf("reloaded port = %d, want %d", reloaded.Server.Port, cfg.Server.Port)
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		url   string
		valid bool
	}{
		{"http://localhost:1984", true},
		{"https://example.com", true},
		{"rtsp://cam:554", true},
		{"", false},
		{"no-scheme", false},
	}
	for _, tt := range tests {
		err := validateURL(tt.url, "test_field")
		if tt.valid && err != nil {
			t.Errorf("validateURL(%q) = error %v, want nil", tt.url, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("validateURL(%q) = nil, want error", tt.url)
		}
	}
}

func TestColdPathOptional(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	// No cold path = cold retention days should not be defaulted
	if cfg.Storage.ColdRetentionDays != 0 {
		t.Errorf("ColdRetentionDays should be 0 when ColdPath is empty, got %d", cfg.Storage.ColdRetentionDays)
	}

	// With cold path, it should default to 30
	cfg2 := &Config{Storage: StorageConfig{ColdPath: "/media/cold"}}
	setDefaults(cfg2)
	if cfg2.Storage.ColdRetentionDays != 30 {
		t.Errorf("ColdRetentionDays should be 30 when ColdPath is set, got %d", cfg2.Storage.ColdRetentionDays)
	}
}
