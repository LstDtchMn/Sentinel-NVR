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
	Server        ServerConfig       `yaml:"server"`
	Auth          AuthConfig         `yaml:"auth"`          // Phase 7: local auth + JWT (CG6)
	Notifications NotificationConfig `yaml:"notifications"` // Phase 8: FCM/APNs push (R9)
	Storage       StorageConfig      `yaml:"storage"`
	Database      DatabaseConfig     `yaml:"database"`
	Detection     DetectionConfig    `yaml:"detection"`
	Go2RTC        Go2RTCConfig       `yaml:"go2rtc"`
	Cameras       []CameraConfig     `yaml:"cameras"`
	Watchdog      WatchdogConfig     `yaml:"watchdog"`
}

// NotificationConfig holds push notification provider settings (Phase 8, R9).
// Set enabled: true and configure at least one provider (fcm, apns, or webhook)
// to receive push notifications on detection and camera events.
type NotificationConfig struct {
	Enabled       bool       `yaml:"enabled"`
	RetryInterval int        `yaml:"retry_interval"` // seconds between crash-recovery scans; default 60
	FCM           FCMConfig  `yaml:"fcm"`
	APNs          APNsConfig `yaml:"apns"`
}

// FCMConfig holds Firebase Cloud Messaging service account credentials (Phase 8, R9).
// Obtain the service account JSON from Firebase Console →
// Project Settings → Service Accounts → Generate new private key.
type FCMConfig struct {
	ServiceAccountJSON string `yaml:"service_account_json"` // absolute path to service account .json file
}

// APNsConfig holds Apple Push Notification service credentials (Phase 8, R9).
// Obtain the .p8 auth key from Apple Developer portal →
// Certificates, Identifiers & Profiles → Keys.
type APNsConfig struct {
	KeyPath  string `yaml:"key_path"`  // absolute path to .p8 auth key file
	KeyID    string `yaml:"key_id"`    // 10-character key identifier from Apple
	TeamID   string `yaml:"team_id"`   // 10-character Apple team identifier
	BundleID string `yaml:"bundle_id"` // app bundle ID, e.g. "com.example.SentinelNVR"
	Sandbox  bool   `yaml:"sandbox"`   // true = development APNs endpoint; false = production
}

// AuthConfig controls local authentication and JWT session management (Phase 7, CG6).
type AuthConfig struct {
	Enabled          bool   `yaml:"enabled"`           // default true; set false to disable auth (dev/trusted-LAN only)
	AccessTokenTTL   int    `yaml:"access_token_ttl"`  // seconds; default 900 (15 min)
	RefreshTokenTTL  int    `yaml:"refresh_token_ttl"` // seconds; default 604800 (7 days)
	SecureCookie     bool   `yaml:"secure_cookie"`     // set Secure flag on cookies; enable when running HTTPS
	AllowedOrigins   []string `yaml:"allowed_origins"` // CORS origins for cookie-based auth; default ["http://localhost:5173"]
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

// DetectionConfig holds AI detection backend settings (CG10, R3).
type DetectionConfig struct {
	Enabled             bool     `yaml:"enabled"`
	Backend             string   `yaml:"backend"`
	RemoteURL           string   `yaml:"remote_url"`     // Phase 5: HTTP endpoint for remote detection (CodeProject.AI format)
	FrameInterval       int      `yaml:"frame_interval"` // Phase 5: seconds between frame grabs per camera
	SnapshotPath        string   `yaml:"snapshot_path"`  // Phase 5: absolute path for JPEG snapshot storage
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
// validLogLevels lists accepted values for server.log_level.
// Must match slog.Level constants used in main.go to configure the logger.
var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

func Validate(cfg *Config) error {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port %d is out of range [1-65535]", cfg.Server.Port)
	}
	if !validLogLevels[cfg.Server.LogLevel] {
		return fmt.Errorf("server.log_level %q is invalid (must be debug, info, warn, or error)", cfg.Server.LogLevel)
	}

	if cfg.Storage.SegmentDuration < 1 {
		return fmt.Errorf("storage.segment_duration must be >= 1, got %d", cfg.Storage.SegmentDuration)
	}
	if cfg.Storage.SegmentDuration > 60 {
		return fmt.Errorf("storage.segment_duration %d exceeds maximum (60 minutes)", cfg.Storage.SegmentDuration)
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

	if cfg.Detection.Enabled {
		if cfg.Detection.Backend == "remote" {
			if err := validateURL(cfg.Detection.RemoteURL, "detection.remote_url"); err != nil {
				return err
			}
		}
		if !filepath.IsAbs(cfg.Detection.SnapshotPath) {
			return fmt.Errorf("detection.snapshot_path must be an absolute path, got %q", cfg.Detection.SnapshotPath)
		}
		if cfg.Detection.FrameInterval < 1 {
			return fmt.Errorf("detection.frame_interval must be >= 1, got %d", cfg.Detection.FrameInterval)
		}
	}

	// Validate notification provider config (Phase 8, R9).
	if cfg.Notifications.Enabled {
		if cfg.Notifications.RetryInterval < 1 {
			return fmt.Errorf("notifications.retry_interval must be >= 1, got %d", cfg.Notifications.RetryInterval)
		}
		fcm := cfg.Notifications.FCM
		if fcm.ServiceAccountJSON != "" && !filepath.IsAbs(fcm.ServiceAccountJSON) {
			return fmt.Errorf("notifications.fcm.service_account_json must be an absolute path, got %q", fcm.ServiceAccountJSON)
		}
		apns := cfg.Notifications.APNs
		if apns.KeyPath != "" {
			if !filepath.IsAbs(apns.KeyPath) {
				return fmt.Errorf("notifications.apns.key_path must be an absolute path, got %q", apns.KeyPath)
			}
			if apns.KeyID == "" {
				return fmt.Errorf("notifications.apns.key_id is required when key_path is set")
			}
			if apns.TeamID == "" {
				return fmt.Errorf("notifications.apns.team_id is required when key_path is set")
			}
			if apns.BundleID == "" {
				return fmt.Errorf("notifications.apns.bundle_id is required when key_path is set")
			}
		}
	}

	// Validate watchdog intervals — time.NewTicker panics on non-positive duration.
	if cfg.Watchdog.Enabled {
		if cfg.Watchdog.HealthInterval <= 0 {
			return fmt.Errorf("watchdog.health_interval must be > 0, got %d", cfg.Watchdog.HealthInterval)
		}
		if cfg.Watchdog.RestartDelay < 0 {
			return fmt.Errorf("watchdog.restart_delay must be >= 0, got %d", cfg.Watchdog.RestartDelay)
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
	if cfg.Detection.RemoteURL == "" {
		cfg.Detection.RemoteURL = "http://codeproject-ai:32168"
	}
	if cfg.Detection.FrameInterval == 0 {
		cfg.Detection.FrameInterval = 5 // grab a frame every 5 seconds per camera
	}
	if cfg.Detection.SnapshotPath == "" {
		cfg.Detection.SnapshotPath = "/data/snapshots"
	}
	if cfg.Detection.GPUDevice == "" {
		cfg.Detection.GPUDevice = "auto"
	}
	// ConfidenceThreshold default is handled by ConfidenceThresholdValue() method
	// instead of overwriting 0.0 (which is a valid intentional value).
	// Auth defaults (Phase 7, CG6). Enabled is not defaulted here — it mirrors
	// the pattern of WatchdogConfig.Enabled and DetectionConfig.Enabled (explicit
	// opt-in). Set auth.enabled: true in sentinel.yml to activate authentication.
	if cfg.Auth.AccessTokenTTL == 0 {
		cfg.Auth.AccessTokenTTL = 900 // 15 minutes
	}
	if cfg.Auth.RefreshTokenTTL == 0 {
		cfg.Auth.RefreshTokenTTL = 604800 // 7 days
	}
	if len(cfg.Auth.AllowedOrigins) == 0 {
		cfg.Auth.AllowedOrigins = []string{"http://localhost:5173"}
	}

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

	// Notification defaults (Phase 8, R9)
	if cfg.Notifications.RetryInterval == 0 {
		cfg.Notifications.RetryInterval = 60 // re-check pending deliveries every minute
	}
}
