package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Storage   StorageConfig   `yaml:"storage"`
	Database  DatabaseConfig  `yaml:"database"`
	Detection DetectionConfig `yaml:"detection"`
	Cameras   []CameraConfig  `yaml:"cameras"`
	Watchdog  WatchdogConfig  `yaml:"watchdog"`
}

type ServerConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	LogLevel string `yaml:"log_level"`
}

type StorageConfig struct {
	HotPath           string `yaml:"hot_path"`
	ColdPath          string `yaml:"cold_path"`
	HotRetentionDays  int    `yaml:"hot_retention_days"`
	ColdRetentionDays int    `yaml:"cold_retention_days"`
	SegmentDuration   int    `yaml:"segment_duration"`
	SegmentFormat     string `yaml:"segment_format"`
}

type DatabaseConfig struct {
	Path    string `yaml:"path"`
	WALMode bool   `yaml:"wal_mode"`
}

type DetectionConfig struct {
	Enabled             bool    `yaml:"enabled"`
	Backend             string  `yaml:"backend"`
	Model               string  `yaml:"model"`
	GPUDevice           string  `yaml:"gpu_device"`
	ConfidenceThreshold float64 `yaml:"confidence_threshold"`
}

type CameraConfig struct {
	Name       string      `yaml:"name"`
	Enabled    bool        `yaml:"enabled"`
	MainStream string      `yaml:"main_stream"`
	SubStream  string      `yaml:"sub_stream"`
	Record     bool        `yaml:"record"`
	Detect     bool        `yaml:"detect"`
	ONVIF      ONVIFConfig `yaml:"onvif,omitempty"`
}

type ONVIFConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

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

// Save writes the current configuration back to a YAML file.
// Used by the Web UI when users change settings visually.
func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
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
		cfg.Storage.SegmentFormat = "fmp4"
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
	if cfg.Detection.ConfidenceThreshold == 0 {
		cfg.Detection.ConfidenceThreshold = 0.6
	}
	if cfg.Watchdog.HealthInterval == 0 {
		cfg.Watchdog.HealthInterval = 30
	}
	if cfg.Watchdog.RestartDelay == 0 {
		cfg.Watchdog.RestartDelay = 5
	}
}
