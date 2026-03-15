// routes_config.go — health, admin health, and system configuration handlers.

package server

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

// handleHealth returns a minimal public health check.
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": s.version,
	})
}

// handleAdminHealth returns detailed system health including DB and go2rtc status.
// Returns 200 when all subsystems are healthy, 503 when any critical subsystem is degraded.
func (s *Server) handleAdminHealth(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}

	dbStatus := "connected"
	if err := s.db.Ping(); err != nil {
		dbStatus = "error"
		s.logger.Error("database health check failed", "error", err)
	}

	g2rStatus := "connected"
	if err := s.g2r.Health(c.Request.Context()); err != nil {
		g2rStatus = "disconnected"
	}

	camCount, err := s.camRepo.Count(c.Request.Context())
	if err != nil {
		s.logger.Error("camera count failed", "error", err)
	}

	recCount, err := s.recRepo.Count(c.Request.Context(), "", time.Time{}, time.Time{})
	if err != nil {
		s.logger.Error("recording count failed", "error", err)
	}

	statusCode := http.StatusOK
	statusText := "ok"
	if dbStatus == "error" || g2rStatus == "disconnected" {
		statusCode = http.StatusServiceUnavailable
		statusText = "degraded"
	}

	c.JSON(statusCode, gin.H{
		"status":             statusText,
		"version":            s.version,
		"uptime":             time.Since(s.startTime).Round(time.Second).String(),
		"go_version":         runtime.Version(),
		"os":                 runtime.GOOS,
		"arch":               runtime.GOARCH,
		"cameras_configured": camCount,
		"recordings_count":   recCount,
		"database":           dbStatus,
		"go2rtc":             g2rStatus,
	})
}

// handleGetConfig returns the current system configuration with sensitive
// fields stripped for safety.
func (s *Server) handleGetConfig(c *gin.Context) {
	type safeServer struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		LogLevel string `json:"log_level"`
	}
	type safeStorage struct {
		HotPath           string `json:"hot_path,omitempty"`
		ColdPath          string `json:"cold_path,omitempty"`
		HotRetentionDays  int    `json:"hot_retention_days"`
		ColdRetentionDays int    `json:"cold_retention_days"`
		SegmentDuration   int    `json:"segment_duration"`
		SegmentFormat     string `json:"segment_format"`
	}

	// Fetch camera summaries from DB instead of config
	cameras, err := s.camManager.ListCameras(c.Request.Context())
	if err != nil {
		s.logger.Error("failed to list cameras for config", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	type safeCamera struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		Record  bool   `json:"record"`
		Detect  bool   `json:"detect"`
	}
	safeCams := make([]safeCamera, len(cameras))
	for i, cam := range cameras {
		safeCams[i] = safeCamera{
			Name:    cam.Name,
			Enabled: cam.Enabled,
			Record:  cam.Record,
			Detect:  cam.Detect,
		}
	}

	cfg := s.snapConfig()
	role, _ := c.Get("role")
	isAdmin := role == "admin"
	st := safeStorage{
		HotRetentionDays:  cfg.Storage.HotRetentionDays,
		ColdRetentionDays: cfg.Storage.ColdRetentionDays,
		SegmentDuration:   cfg.Storage.SegmentDuration,
		SegmentFormat:     cfg.Storage.SegmentFormat,
	}
	if isAdmin {
		st.HotPath = cfg.Storage.HotPath
		st.ColdPath = cfg.Storage.ColdPath
	}
	// MQTT config — strip password for non-admin users
	mqttResp := gin.H{
		"enabled":      cfg.MQTT.Enabled,
		"broker":       cfg.MQTT.Broker,
		"topic_prefix": cfg.MQTT.TopicPrefix,
		"username":     cfg.MQTT.Username,
		"ha_discovery": cfg.MQTT.HADiscovery,
	}
	if isAdmin {
		mqttResp["password"] = cfg.MQTT.Password
	} else {
		mqttResp["password"] = ""
	}

	// Notifications SMTP config — strip password for non-admin users
	smtpResp := gin.H{
		"host":     cfg.Notifications.SMTP.Host,
		"port":     cfg.Notifications.SMTP.Port,
		"username": cfg.Notifications.SMTP.Username,
		"from":     cfg.Notifications.SMTP.From,
		"tls":      cfg.Notifications.SMTP.TLS,
	}
	if isAdmin {
		smtpResp["password"] = cfg.Notifications.SMTP.Password
	} else {
		smtpResp["password"] = ""
	}

	c.JSON(http.StatusOK, gin.H{
		"server": safeServer{
			Host:     cfg.Server.Host,
			Port:     cfg.Server.Port,
			LogLevel: cfg.Server.LogLevel,
		},
		"storage":   st,
		"detection": gin.H{"enabled": cfg.Detection.Enabled, "backend": cfg.Detection.Backend},
		"mqtt":      mqttResp,
		"notifications": gin.H{
			"smtp": smtpResp,
		},
		"cameras": safeCams,
	})
}

// handleUpdateConfig applies partial config updates and persists to disk (Phase 9, admin-only).
// PUT /api/v1/config  body: {"server":{"log_level":"..."},"storage":{"hot_retention_days":N,...}}
// Only non-sensitive, runtime-safe fields are updatable; storage paths require a restart and
// are therefore excluded. Returns the full sanitised config on success.
func (s *Server) handleUpdateConfig(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}

	var input struct {
		Server *struct {
			LogLevel string `json:"log_level"`
		} `json:"server"`
		Storage *struct {
			HotRetentionDays  int `json:"hot_retention_days"`
			ColdRetentionDays int `json:"cold_retention_days"`
			SegmentDuration   int `json:"segment_duration"`
		} `json:"storage"`
		MQTT *struct {
			Enabled     *bool  `json:"enabled"`
			Broker      string `json:"broker"`
			TopicPrefix string `json:"topic_prefix"`
			Username    string `json:"username"`
			Password    string `json:"password"`
			HADiscovery *bool  `json:"ha_discovery"`
		} `json:"mqtt"`
		Notifications *struct {
			SMTP *struct {
				Host     *string `json:"host"`
				Port     *int    `json:"port"`
				Username *string `json:"username"`
				Password *string `json:"password"`
				From     *string `json:"from"`
				TLS      *bool   `json:"tls"`
			} `json:"smtp"`
		} `json:"notifications"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Snapshot the current config, apply changes, and validate OUTSIDE the write
	// lock so that Validate()'s CPU work does not block concurrent readers.
	cfgCopy := s.snapConfig()
	if input.Server != nil && input.Server.LogLevel != "" {
		cfgCopy.Server.LogLevel = input.Server.LogLevel
	}
	if input.Storage != nil {
		if input.Storage.HotRetentionDays > 0 {
			cfgCopy.Storage.HotRetentionDays = input.Storage.HotRetentionDays
		}
		if input.Storage.ColdRetentionDays > 0 {
			cfgCopy.Storage.ColdRetentionDays = input.Storage.ColdRetentionDays
		}
		if input.Storage.SegmentDuration > 0 {
			cfgCopy.Storage.SegmentDuration = input.Storage.SegmentDuration
		}
	}
	if input.MQTT != nil {
		if input.MQTT.Enabled != nil {
			cfgCopy.MQTT.Enabled = *input.MQTT.Enabled
		}
		if input.MQTT.Broker != "" {
			cfgCopy.MQTT.Broker = input.MQTT.Broker
		}
		if input.MQTT.TopicPrefix != "" {
			cfgCopy.MQTT.TopicPrefix = input.MQTT.TopicPrefix
		}
		// Username and password can be set to empty string intentionally
		cfgCopy.MQTT.Username = input.MQTT.Username
		cfgCopy.MQTT.Password = input.MQTT.Password
		if input.MQTT.HADiscovery != nil {
			cfgCopy.MQTT.HADiscovery = *input.MQTT.HADiscovery
		}
	}
	if input.Notifications != nil && input.Notifications.SMTP != nil {
		smtp := input.Notifications.SMTP
		if smtp.Host != nil {
			cfgCopy.Notifications.SMTP.Host = *smtp.Host
		}
		if smtp.Port != nil {
			cfgCopy.Notifications.SMTP.Port = *smtp.Port
		}
		if smtp.Username != nil {
			cfgCopy.Notifications.SMTP.Username = *smtp.Username
		}
		if smtp.Password != nil {
			cfgCopy.Notifications.SMTP.Password = *smtp.Password
		}
		if smtp.From != nil {
			cfgCopy.Notifications.SMTP.From = *smtp.From
		}
		if smtp.TLS != nil {
			cfgCopy.Notifications.SMTP.TLS = *smtp.TLS
		}
	}

	if err := config.Validate(&cfgCopy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.cfgMu.Lock()
	*s.cfg = cfgCopy // only assign after validation passes
	s.cfgMu.Unlock() // release lock before disk I/O so reads are not blocked

	// Apply log level change immediately so operators see the effect without a restart.
	if input.Server != nil && input.Server.LogLevel != "" && s.logLevel != nil {
		s.logLevel.Set(parseSlogLevel(cfgCopy.Server.LogLevel))
	}

	if s.configPath != "" {
		// Re-read the current config under a read lock for saving, instead of using
		// the local cfgCopy. This ensures that when two concurrent PUT /config requests
		// race, the disk always captures the latest in-memory state — preventing a
		// stale cfgCopy from overwriting a newer config value written by a later request.
		s.cfgMu.RLock()
		toSave := *s.cfg
		s.cfgMu.RUnlock()
		if err := config.Save(s.configPath, &toSave); err != nil {
			// Do NOT roll back the in-memory config: rolling back with a captured
			// oldCfg snapshot would race with concurrent PUT /config requests and
			// could overwrite a newer successful write. The in-memory config is
			// already valid; on next restart the stale disk value is overwritten by
			// the first successful save or operator intervention.
			s.logger.Error("failed to save config", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config"})
			return
		}
	}

	s.handleGetConfig(c)
}
