// Package config handles loading, validating, and saving the sentinel.yml
// configuration file. It defines all configuration structs and their defaults.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the top-level application configuration loaded from sentinel.yml.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Storage   StorageConfig   `yaml:"storage"`
	Database  DatabaseConfig  `yaml:"database"`
	Detection DetectionConfig `yaml:"detection"`
	Go2RTC    Go2RTCConfig    `yaml:"go2rtc"`
	Cameras   []CameraConfig  `yaml:"cameras"`
	Watchdog  WatchdogConfig  `yaml:"watchdog"`
}

// Go2RTCConfig holds connection settings for the go2rtc sidecar (CG3).
type Go2RTCConfig struct {
	APIURL  string `yaml:"api_url"`
	RTSPURL string `yaml:"rtsp_url"` // Phase 2: ffmpeg reads from go2rtc's RTSP re-stream
}

// ServerConfig holds HTTP server settings (CG2).
type ServerConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	LogLevel string `yaml:"log_level"`
}

// StorageConfig defines hot/cold tiered storage paths and retention (R13, R14).
// Phase 2: this struct must be passed through Manager → Pipeline for recording.
// Pipeline needs HotPath to construct: {hot_path}/{camera_name}/{date}/{time}.mp4
type StorageConfig struct {
	HotPath           string `yaml:"hot_path"`
	ColdPath          string `yaml:"cold_path"`
	HotRetentionDays  int    `yaml:"hot_retention_days"`
	ColdRetentionDays int    `yaml:"cold_retention_days"`
	SegmentDuration   int    `yaml:"segment_duration"`
	SegmentFormat     string `yaml:"segment_format"`
}

// DatabaseConfig holds SQLite database settings (CG2).
type DatabaseConfig struct {
	Path    string `yaml:"path"`
	WALMode bool   `yaml:"wal_mode"`
}

// DetectionConfig holds AI detection backend settings (CG10, R8).
type DetectionConfig struct {
	Enabled             bool     `yaml:"enabled"`
	Backend             string   `yaml:"backend"`
	Model               string   `yaml:"model"`
	GPUDevice           string   `yaml:"gpu_device"`
	ConfidenceThreshold *float64 `yaml:"confidence_threshold"` // pointer to distinguish unset from 0.0
}

// CameraConfig defines a single camera's RTSP streams and behavior (R1, R2).
type CameraConfig struct {
	Name       string      `yaml:"name"`
	Enabled    bool        `yaml:"enabled"`
	MainStream string      `yaml:"main_stream"`
	SubStream  string      `yaml:"sub_stream"`
	Record     bool        `yaml:"record"`
	Detect     bool        `yaml:"detect"`
	ONVIF      ONVIFConfig `yaml:"onvif,omitempty"`
}

// ONVIFConfig holds ONVIF discovery and PTZ credentials for a camera.
type ONVIFConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// WatchdogConfig controls the supervisor process (R4).
type WatchdogConfig struct {
	Enabled        bool `yaml:"enabled"`
	HealthInterval int  `yaml:"health_interval"`
	RestartDelay   int  `yaml:"restart_delay"`
}

// Load reads and parses a YAML configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	setDefaults(cfg)
	return cfg, nil
}

// Validate checks the configuration for logical errors that would cause
// runtime failures. Call after Load and setDefaults.
func Validate(cfg *Config) error {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port %d is out of range [1-65535]", cfg.Server.Port)
	}

	if cfg.Storage.SegmentDuration < 1 {
		return fmt.Errorf("storage.segment_duration must be >= 1, got %d", cfg.Storage.SegmentDuration)
	}

	if !filepath.IsAbs(cfg.Storage.HotPath) {
		return fmt.Errorf("storage.hot_path must be an absolute path, got %q", cfg.Storage.HotPath)
	}
	if !filepath.IsAbs(cfg.Storage.ColdPath) {
		return fmt.Errorf("storage.cold_path must be an absolute path, got %q", cfg.Storage.ColdPath)
	}
	if cfg.Storage.HotRetentionDays < 1 {
		return fmt.Errorf("storage.hot_retention_days must be >= 1, got %d", cfg.Storage.HotRetentionDays)
	}
	if cfg.Storage.ColdRetentionDays < 1 {
		return fmt.Errorf("storage.cold_retention_days must be >= 1, got %d", cfg.Storage.ColdRetentionDays)
	}

	// Validate confidence threshold is within the model output range [0.0, 1.0].
	// A value > 1.0 silently suppresses all detections; < 0.0 is undefined.
	if cfg.Detection.ConfidenceThreshold != nil {
		t := *cfg.Detection.ConfidenceThreshold
		if t < 0.0 || t > 1.0 {
			return fmt.Errorf("detection.confidence_threshold %g is out of range [0.0, 1.0]", t)
		}
	}

	// Validate go2rtc API and RTSP URLs so misconfiguration is caught at startup,
	// not at the first outbound network call when error messages are harder to correlate.
	if err := validateURL(cfg.Go2RTC.APIURL, "go2rtc.api_url"); err != nil {
		return err
	}
	if err := validateURL(cfg.Go2RTC.RTSPURL, "go2rtc.rtsp_url"); err != nil {
		return err
	}

	// Check for duplicate camera names — duplicates silently overwrite in the manager map
	names := make(map[string]bool)
	for i, cam := range cfg.Cameras {
		if cam.Name == "" {
			return fmt.Errorf("cameras[%d]: name is required", i)
		}
		if names[cam.Name] {
			return fmt.Errorf("cameras[%d]: duplicate camera name %q", i, cam.Name)
		}
		names[cam.Name] = true

		if cam.Enabled && cam.MainStream == "" {
			return fmt.Errorf("camera %q: main_stream is required when enabled", cam.Name)
		}
	}

	return nil
}

// Save writes the current configuration back to a YAML file.
// Used by the Web UI when users change settings visually.
func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// ConfidenceThreshold returns the effective detection confidence threshold,
// defaulting to 0.6 if not explicitly configured.
func (d *DetectionConfig) ConfidenceThresholdValue() float64 {
	if d.ConfidenceThreshold != nil {
		return *d.ConfidenceThreshold
	}
	return 0.6
}

// validateURL checks that a URL string has a valid scheme and host.
// Used by Validate to catch misconfigured go2rtc URLs before they cause
// confusing network errors at runtime.
func validateURL(raw, field string) error {
	if raw == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", field, err)
	}
	if u.Scheme == "" {
		return fmt.Errorf("%s has no scheme (expected http/https/rtsp)", field)
	}
	if u.Host == "" {
		return fmt.Errorf("%s has no host", field)
	}
	return nil
}

func setDefaults(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8099
	}
	if cfg.Server.LogLevel == "" {
		cfg.Server.LogLevel = "info"
	}
	if cfg.Storage.HotPath == "" {
		cfg.Storage.HotPath = "/media/hot"
	}
	if cfg.Storage.ColdPath == "" {
		cfg.Storage.ColdPath = "/media/cold"
	}
	if cfg.Storage.HotRetentionDays == 0 {
		cfg.Storage.HotRetentionDays = 3
	}
	if cfg.Storage.ColdRetentionDays == 0 {
		cfg.Storage.ColdRetentionDays = 30
	}
	if cfg.Storage.SegmentDuration == 0 {
		cfg.Storage.SegmentDuration = 10
	}
	if cfg.Storage.SegmentFormat == "" {
		// Regular MP4 (not fragmented) — each segment is independently playable
		// in VLC, browsers, etc. without needing an init segment (CG4).
		cfg.Storage.SegmentFormat = "mp4"
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "/data/sentinel.db"
	}
	if cfg.Detection.Backend == "" {
		cfg.Detection.Backend = "openvino"
	}
	if cfg.Detection.GPUDevice == "" {
		cfg.Detection.GPUDevice = "auto"
	}
	// ConfidenceThreshold default is handled by ConfidenceThresholdValue() method
	// instead of overwriting 0.0 (which is a valid intentional value).
	if cfg.Go2RTC.APIURL == "" {
		cfg.Go2RTC.APIURL = "http://go2rtc:1984"
	}
	// Environment variable override for go2rtc URL (useful in Docker Compose)
	if env := os.Getenv("GO2RTC_API"); env != "" {
		cfg.Go2RTC.APIURL = env
	}
	if cfg.Go2RTC.RTSPURL == "" {
		cfg.Go2RTC.RTSPURL = "rtsp://go2rtc:8554"
	}
	if env := os.Getenv("GO2RTC_RTSP"); env != "" {
		cfg.Go2RTC.RTSPURL = env
	}
	if cfg.Watchdog.HealthInterval == 0 {
		cfg.Watchdog.HealthInterval = 30
	}
	if cfg.Watchdog.RestartDelay == 0 {
		cfg.Watchdog.RestartDelay = 5
	}
}
