// This file defines all REST API route handlers (CG7).

package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/auth"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/notification"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/storage"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/importers"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/models"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/pathutil"
)

// setupUsernameRE validates usernames during first-run setup.
var setupUsernameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _-]{0,62}[a-zA-Z0-9_-]?$`)

// parseSlogLevel converts a sentinel log_level string to a slog.Level constant.
func parseSlogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// registerRoutes mounts all API v1 endpoints on the Gin router.
// Public routes (health, auth) are registered on the base v1 group.
// All other routes are inside a protected group that requires a valid JWT
// when auth.enabled=true. When authService is nil, the protected group has
// no middleware and all routes are effectively public.
func (s *Server) registerRoutes() {
	v1 := s.router.Group("/api/v1")

	// --- Public endpoints (no auth required) ---
	v1.GET("/health", s.handleHealth)

	// Auth endpoints (Phase 7, CG6)
	authGroup := v1.Group("/auth")
	{
		authGroup.POST("/login", s.handleAuthLogin)
		authGroup.POST("/refresh", s.handleAuthRefresh)
		authGroup.POST("/logout", s.handleAuthLogout)
		// OIDC SSO routes — registered only when the provider is wired up (Phase 7, CG6).
		if s.oidcProvider != nil {
			authGroup.GET("/oidc/login", s.handleOIDCLogin)
			authGroup.GET("/oidc/callback", s.handleOIDCCallback)
		}
	}

	// First-run setup (Phase 7, CG6) — public so the UI can check/redirect before login
	v1.GET("/setup", s.handleSetupCheck)
	v1.POST("/setup", s.handleSetupCreate)

	// --- Protected endpoints ---
	// When authService is non-nil (auth.enabled=true), all routes below require
	// a valid sentinel_access JWT cookie. When nil, they are openly accessible.
	protected := v1.Group("")
	if s.authService != nil {
		protected.Use(s.authService.Middleware())
	}
	{
		protected.GET("/config", s.handleGetConfig)
		protected.PUT("/config", s.handleUpdateConfig) // admin-only; Phase 9 settings persistence

		// Current user info (Phase 7) — returns the authenticated user's public fields.
		protected.GET("/auth/me", s.handleAuthMe)

		// Camera management (Phase 1)
		protected.GET("/cameras", s.handleListCameras)
		protected.GET("/cameras/:name", s.handleGetCamera)
		protected.GET("/cameras/:name/status", s.handleCameraStatus)
		protected.GET("/cameras/:name/snapshot", s.handleCameraSnapshot) // Phase 9: zone editor background
		protected.POST("/cameras", s.handleCreateCamera)
		protected.PUT("/cameras/:name", s.handleUpdateCamera)
		protected.DELETE("/cameras/:name", s.handleDeleteCamera)

		// Live streaming (Phase 3) — WebSocket proxy to go2rtc MSE
		protected.GET("/streams/:name/ws", s.handleStreamWS)

		// Events API (Phase 5 — AI detection results, R3)
		// Static sub-routes registered before :id wildcard so Gin routes them correctly.
		protected.GET("/events", s.handleListEvents)
		protected.GET("/events/heatmap", s.handleEventHeatmap)  // Phase 6: detection density (R6)
		protected.GET("/events/stream", s.handleEventStream)     // Phase 6: SSE real-time (CG8)
		protected.GET("/events/:id", s.handleGetEvent)
		protected.GET("/events/:id/thumbnail", s.handleEventThumbnail)
		protected.DELETE("/events/:id", s.handleDeleteEvent)

		// Playback timeline (Phase 4) — registered before :id routes for clear routing
		protected.GET("/recordings/timeline", s.handleRecordingTimeline)
		protected.GET("/recordings/days", s.handleRecordingDays)

		// Storage stats (Phase 10, R13) — aggregate used_bytes per tier
		protected.GET("/storage/stats", s.handleStorageStats)

		// Recording management (Phase 2)
		protected.GET("/recordings", s.handleListRecordings)
		protected.GET("/recordings/:id", s.handleGetRecording)
		protected.GET("/recordings/:id/play", s.handlePlayRecording)
		protected.DELETE("/recordings/:id", s.handleDeleteRecording)

		// Notification management (Phase 8, R9)
		// Tokens: per-user device registration for FCM, APNs, or webhook delivery.
		// Prefs: per-user, per-event-type rules controlling when notifications fire.
		// Log: read-only delivery history for the current user.
		notif := protected.Group("/notifications")
		{
			notif.POST("/tokens", s.handleCreateNotifToken)
			notif.GET("/tokens", s.handleListNotifTokens)
			notif.DELETE("/tokens/:id", s.handleDeleteNotifToken)
			notif.GET("/prefs", s.handleListNotifPrefs)
			notif.PUT("/prefs", s.handleUpsertNotifPref)
			notif.DELETE("/prefs/:id", s.handleDeleteNotifPref)
			notif.GET("/log", s.handleListNotifLog)
		}

		// Retention rules management (R14) — per-camera × per-event-type matrix
		retention := protected.Group("/retention/rules")
		{
			retention.GET("", s.handleListRetentionRules)
			retention.POST("", s.handleCreateRetentionRule)
			retention.PUT("/:id", s.handleUpdateRetentionRule)
			retention.DELETE("/:id", s.handleDeleteRetentionRule)
		}

		// Face recognition management (Phase 13, R11)
		faces := protected.Group("/faces")
		{
			faces.GET("", s.handleListFaces)
			faces.POST("", s.handleCreateFace)         // raw embedding API (admin)
			faces.POST("/enroll", s.handleEnrollFace)  // JPEG upload → sentinel-infer (admin, R11)
			faces.GET("/:id", s.handleGetFace)
			faces.PUT("/:id", s.handleUpdateFace)
			faces.DELETE("/:id", s.handleDeleteFace)
		}

		// AI Model management (R10) — curated download list + manual upload
		mdls := protected.Group("/models")
		{
			mdls.GET("", s.handleListModels)
			mdls.POST("/:filename/download", s.handleDownloadModel)
			mdls.POST("/upload", s.handleUploadModel)
			mdls.DELETE("/:filename", s.handleDeleteModel)
		}

		// Migration / import (Phase 14, R15)
		protected.POST("/import/preview", s.handleImportPreview) // dry-run: parse + validate
		protected.POST("/import", s.handleImportExecute)         // actually create cameras

		// Remote access (Phase 12, CG11, R8)
		protected.GET("/relay/ice-servers", s.handleRelayICEServers)
		protected.POST("/pairing/qr", s.handlePairingQR)
	}

	// Public pairing redeem — mobile app has no auth session when it calls this (Phase 12, CG11).
	v1.POST("/pairing/redeem", s.handlePairingRedeem)
}

// handleSetupCheck reports whether first-run setup is needed (Phase 7, CG6).
// GET /api/v1/setup — public; returns {"needs_setup": bool, "oidc_enabled": bool}.
func (s *Server) handleSetupCheck(c *gin.Context) {
	if s.authService == nil {
		// Auth is disabled — no setup needed, no OIDC.
		c.JSON(http.StatusOK, gin.H{"needs_setup": false, "oidc_enabled": false})
		return
	}
	needs, err := s.authService.NeedsSetup(c.Request.Context())
	if err != nil {
		s.logger.Error("setup check failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "setup check failed"})
		return
	}
	cfg := s.snapConfig()
	c.JSON(http.StatusOK, gin.H{
		"needs_setup":  needs,
		"oidc_enabled": cfg.Auth.OIDC.Enabled,
	})
}

// handleSetupCreate creates the first admin account during first-run setup (Phase 7, CG6).
// POST /api/v1/setup   body: {"username":"...","password":"..."}
// Responds with the created user and sets auth cookies, same as /auth/login.
func (s *Server) handleSetupCreate(c *gin.Context) {
	if s.authService == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "auth is disabled; setup is not required"})
		return
	}

	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}

	// Validate username format — same rules as camera names (printable, no shell-special chars).
	if !setupUsernameRE.MatchString(req.Username) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username must be 1–64 alphanumeric/space/dash/underscore characters"})
		return
	}
	if len(req.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		return
	}
	// bcrypt silently truncates inputs at 72 bytes. A multi-MB password would still
	// consume ~1s of CPU before truncation — enforce the limit explicitly (CG6).
	if len(req.Password) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must not exceed 72 characters"})
		return
	}

	user, pair, err := s.authService.Setup(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrSetupAlreadyDone) {
			c.JSON(http.StatusConflict, gin.H{"error": "setup already completed"})
			return
		}
		s.logger.Error("setup failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "setup failed"})
		return
	}

	auth.SetTokenCookies(c, pair, s.snapConfig().Auth.SecureCookie)
	c.JSON(http.StatusCreated, gin.H{
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

// handleAuthLogin authenticates a user and issues JWT + refresh token cookies (Phase 7, CG6).
// POST /api/v1/auth/login   body: {"username":"...","password":"..."}
func (s *Server) handleAuthLogin(c *gin.Context) {
	if s.authService == nil {
		c.JSON(http.StatusOK, gin.H{"message": "auth disabled"})
		return
	}

	// Rate-limit login attempts per IP to prevent brute-force attacks (CG6).
	ip := c.ClientIP()
	if !s.loginLimiter.allow(ip) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts, try again later"})
		return
	}

	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}
	// bcrypt silently truncates inputs at 72 bytes. A multi-MB password would still
	// consume ~1s of CPU before truncation — enforce the limit explicitly (CG6).
	if len(req.Password) > 72 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	pair, err := s.authService.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		s.logger.Error("login error", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}

	// Reset rate limit on successful login so legitimate users aren't locked out.
	s.loginLimiter.reset(ip)
	auth.SetTokenCookies(c, pair, s.snapConfig().Auth.SecureCookie)
	c.JSON(http.StatusOK, gin.H{"message": "logged in"})
}

// handleAuthRefresh rotates the refresh token and issues a new access token (Phase 7, CG6).
// POST /api/v1/auth/refresh  — reads sentinel_refresh cookie
func (s *Server) handleAuthRefresh(c *gin.Context) {
	if s.authService == nil {
		c.JSON(http.StatusOK, gin.H{"message": "auth disabled"})
		return
	}
	refreshToken, err := c.Cookie(auth.RefreshCookieName)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh token required"})
		return
	}

	pair, err := s.authService.Refresh(c.Request.Context(), refreshToken)
	secureCookie := s.snapConfig().Auth.SecureCookie
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrTokenExpired) {
			auth.ClearTokenCookies(c, secureCookie)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "session expired, please log in again"})
			return
		}
		s.logger.Error("refresh error", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "refresh failed"})
		return
	}

	auth.SetTokenCookies(c, pair, secureCookie)
	c.JSON(http.StatusOK, gin.H{"message": "refreshed"})
}

// handleAuthLogout revokes the refresh token and clears auth cookies (Phase 7, CG6).
// POST /api/v1/auth/logout  — reads sentinel_refresh cookie (no JWT required)
func (s *Server) handleAuthLogout(c *gin.Context) {
	if s.authService != nil {
		if refreshToken, err := c.Cookie(auth.RefreshCookieName); err == nil {
			if err := s.authService.Logout(c.Request.Context(), refreshToken); err != nil {
				// Non-fatal: session may already be expired. Log for operator visibility.
				s.logger.Warn("logout: failed to delete refresh token from DB", "error", err)
			}
		}
	}
	auth.ClearTokenCookies(c, s.snapConfig().Auth.SecureCookie)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// handleAuthMe returns the authenticated user's public profile (Phase 7, CG6).
// GET /api/v1/auth/me — requires valid JWT (via protected group middleware)
func (s *Server) handleAuthMe(c *gin.Context) {
	if s.authService == nil {
		c.JSON(http.StatusOK, gin.H{"authenticated": false, "message": "auth disabled"})
		return
	}
	userID, _ := c.Get(auth.CtxKeyUserID)
	username, _ := c.Get(auth.CtxKeyUsername)
	role, _ := c.Get(auth.CtxKeyRole)
	c.JSON(http.StatusOK, gin.H{
		"id":       userID,
		"username": username,
		"role":     role,
	})
}

// handleHealth returns system health including DB and go2rtc status.
// Returns 200 when all subsystems are healthy, 503 when any critical subsystem is degraded.
func (s *Server) handleHealth(c *gin.Context) {
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

// handleStorageStats returns aggregate used bytes and segment counts per storage tier (Phase 10, R13).
// GET /api/v1/storage/stats — cross-platform, no syscall; queries the recordings DB table.
func (s *Server) handleStorageStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Use resolved (symlink-expanded) paths: DB stores real paths (recorder calls EvalSymlinks),
	// so the LIKE prefix must match the real path, not a symlink alias.
	hot, cold, err := s.recRepo.StorageStats(ctx, s.resolvedHotPath, s.resolvedColdPath)
	if err != nil {
		s.logger.Error("storage stats query failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query storage stats"})
		return
	}

	type tierResp struct {
		Path         string `json:"path"`
		UsedBytes    int64  `json:"used_bytes"`
		SegmentCount int    `json:"segment_count"`
	}

	cfg := s.snapConfig()
	resp := gin.H{
		"hot": tierResp{
			Path:         cfg.Storage.HotPath,
			UsedBytes:    hot.UsedBytes,
			SegmentCount: hot.SegmentCount,
		},
	}
	if cfg.Storage.ColdPath != "" {
		resp["cold"] = tierResp{
			Path:         cfg.Storage.ColdPath,
			UsedBytes:    cold.UsedBytes,
			SegmentCount: cold.SegmentCount,
		}
	} else {
		resp["cold"] = nil
	}
	c.JSON(http.StatusOK, resp)
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
		HotPath           string `json:"hot_path"`
		ColdPath          string `json:"cold_path"`
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
	c.JSON(http.StatusOK, gin.H{
		"server": safeServer{
			Host:     cfg.Server.Host,
			Port:     cfg.Server.Port,
			LogLevel: cfg.Server.LogLevel,
		},
		"storage": safeStorage{
			HotPath:           cfg.Storage.HotPath,
			ColdPath:          cfg.Storage.ColdPath,
			HotRetentionDays:  cfg.Storage.HotRetentionDays,
			ColdRetentionDays: cfg.Storage.ColdRetentionDays,
			SegmentDuration:   cfg.Storage.SegmentDuration,
			SegmentFormat:     cfg.Storage.SegmentFormat,
		},
		"detection": gin.H{"enabled": cfg.Detection.Enabled, "backend": cfg.Detection.Backend},
		"cameras":   safeCams,
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
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	s.cfgMu.Lock()
	// Stage all changes in a local copy so a validation failure never corrupts the live config.
	oldCfg := *s.cfg // snapshot for rollback on save failure
	cfgCopy := *s.cfg
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
	if err := config.Validate(&cfgCopy); err != nil {
		s.cfgMu.Unlock()
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	*s.cfg = cfgCopy // only assign after validation passes
	s.cfgMu.Unlock()  // release lock before disk I/O so reads are not blocked

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
			// Rollback in-memory config so it matches what's on disk.
			s.cfgMu.Lock()
			*s.cfg = oldCfg
			s.cfgMu.Unlock()
			if input.Server != nil && input.Server.LogLevel != "" && s.logLevel != nil {
				s.logLevel.Set(parseSlogLevel(oldCfg.Server.LogLevel))
			}
			s.logger.Error("failed to save config", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config"})
			return
		}
	}

	s.handleGetConfig(c)
}

// handleListCameras returns all cameras from the database with live pipeline status.
func (s *Server) handleListCameras(c *gin.Context) {
	cameras, err := s.camManager.ListCameras(c.Request.Context())
	if err != nil {
		s.logger.Error("failed to list cameras", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, cameras)
}

// handleGetCamera returns a single camera with full detail and pipeline status.
func (s *Server) handleGetCamera(c *gin.Context) {
	name := c.Param("name")
	cam, err := s.camManager.GetCamera(c.Request.Context(), name)
	if errors.Is(err, camera.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to get camera", "name", name, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, cam)
}

// handleCameraStatus returns the detailed pipeline status of a single camera.
// Falls back to a DB lookup for disabled cameras (no active pipeline) so that
// a valid-but-disabled camera returns idle status instead of 404.
func (s *Server) handleCameraStatus(c *gin.Context) {
	name := c.Param("name")
	ps, ok := s.camManager.Status(name)
	if !ok {
		// Camera may exist in DB but be disabled (no pipeline). Check DB to distinguish
		// "disabled camera" (→ idle status) from "unknown camera" (→ 404).
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		cam, err := s.camManager.GetCamera(ctx, name)
		if errors.Is(err, camera.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
			return
		}
		if err != nil {
			s.logger.Error("failed to check camera existence for status", "name", name, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		ps = cam.PipelineStatus
	}
	c.JSON(http.StatusOK, ps)
}

// cameraInput is the request body for creating/updating a camera.
type cameraInput struct {
	Name       string          `json:"name"`
	Enabled    *bool           `json:"enabled"`
	MainStream string          `json:"main_stream"`
	SubStream  string          `json:"sub_stream"`
	Record     *bool           `json:"record"`
	Detect     *bool           `json:"detect"`
	ONVIFHost  string          `json:"onvif_host"`
	ONVIFPort  int             `json:"onvif_port"`
	ONVIFUser  string          `json:"onvif_user"`
	ONVIFPass  string          `json:"onvif_pass"`
	Zones      json.RawMessage `json:"zones"` // Phase 9: nil = preserve existing zones (manager handles)
}

func (ci *cameraInput) toRecord() *camera.CameraRecord {
	rec := &camera.CameraRecord{
		Name:       ci.Name,
		Enabled:    true, // default
		MainStream: ci.MainStream,
		SubStream:  ci.SubStream,
		Record:     true, // default
		Detect:     false,
		ONVIFHost:  ci.ONVIFHost,
		ONVIFPort:  ci.ONVIFPort,
		ONVIFUser:  ci.ONVIFUser,
		ONVIFPass:  ci.ONVIFPass,
		Zones:      normalizeZonesRaw(ci.Zones), // nil when not provided or "null" — manager preserves existing zones
	}
	if ci.Enabled != nil {
		rec.Enabled = *ci.Enabled
	}
	if ci.Record != nil {
		rec.Record = *ci.Record
	}
	if ci.Detect != nil {
		rec.Detect = *ci.Detect
	}
	return rec
}

// requireAdmin returns true when the request is from an admin user (or auth is disabled).
// Writes 403 and aborts when auth is enabled and the caller is not an admin.
func (s *Server) requireAdmin(c *gin.Context) bool {
	if s.authService == nil {
		return true // auth disabled — all users are effectively admin
	}
	role, _ := c.Get(auth.CtxKeyRole)
	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		c.Abort()
		return false
	}
	return true
}

// handleCreateCamera creates a new camera, syncs to go2rtc, and starts a pipeline.
func (s *Server) handleCreateCamera(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	var input cameraInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Validate before calling the manager so we can give precise 400 responses.
	rec := input.toRecord()
	if err := camera.ValidateCameraInput(rec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateZonesJSON(input.Zones); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid zones: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	result, err := s.camManager.AddCamera(ctx, rec)
	if errors.Is(err, camera.ErrDuplicate) {
		c.JSON(http.StatusConflict, gin.H{"error": "a camera with that name already exists"})
		return
	}
	if err != nil {
		s.logger.Error("failed to create camera", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusCreated, result)
}

// handleUpdateCamera updates an existing camera and restarts the pipeline if needed.
// The camera name comes from the URL path — the name field in the body is ignored.
func (s *Server) handleUpdateCamera(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	name := c.Param("name")

	var input cameraInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Canonical name comes from the URL path, not the body.
	rec := input.toRecord()
	rec.Name = name
	if err := camera.ValidateCameraInput(rec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateZonesJSON(input.Zones); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid zones: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	result, err := s.camManager.UpdateCamera(ctx, name, rec)
	if errors.Is(err, camera.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to update camera", "name", name, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// handleDeleteCamera stops the pipeline, removes from go2rtc, and deletes from DB.
func (s *Server) handleDeleteCamera(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	name := c.Param("name")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	err := s.camManager.RemoveCamera(ctx, name)
	if errors.Is(err, camera.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to delete camera", "name", name, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.Status(http.StatusNoContent)
}

// handleCameraSnapshot grabs a single JPEG frame from go2rtc for the named camera
// and returns it as image/jpeg. Used as the background image for the zone editor (Phase 9).
// Prefers the sub-stream when configured (lower resolution → faster grab).
// Returns 503 when the stream is not yet producing frames.
// GET /api/v1/cameras/:name/snapshot
func (s *Server) handleCameraSnapshot(c *gin.Context) {
	name := c.Param("name")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()

	cam, err := s.camRepo.GetByName(ctx, name)
	if errors.Is(err, camera.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to look up camera for snapshot", "name", name, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Disabled cameras have no go2rtc stream — bail early to avoid an 8s timeout.
	if !cam.Enabled {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "camera is disabled"})
		return
	}

	// Prefer sub-stream for snapshots — lower resolution, faster frame grab.
	streamName := cam.Name
	if cam.SubStream != "" {
		streamName = cam.Name + "_sub"
	}

	jpegBytes, err := s.g2r.FrameJPEG(ctx, streamName)
	if err != nil || len(jpegBytes) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stream unavailable"})
		return
	}

	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "image/jpeg", jpegBytes)
}

// handleListRecordings returns recording segments with optional filtering.
// Query params: camera (name), start (RFC3339), end (RFC3339), limit (int, max 1000), offset (int).
func (s *Server) handleListRecordings(c *gin.Context) {
	cameraName := c.Query("camera")
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be between 1 and 1000"})
		return
	}
	if limit > 1000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit exceeds maximum (1000)"})
		return
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset"})
		return
	}

	var start, end time.Time
	if startStr := c.Query("start"); startStr != "" {
		t, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start time (use RFC3339)"})
			return
		}
		start = t
	}
	if endStr := c.Query("end"); endStr != "" {
		t, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end time (use RFC3339)"})
			return
		}
		end = t
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	recordings, err := s.recRepo.List(ctx, cameraName, start, end, limit, offset)
	if err != nil {
		s.logger.Error("failed to list recordings", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	total, err := s.recRepo.Count(ctx, cameraName, start, end)
	if err != nil {
		s.logger.Error("failed to count recordings", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"recordings": recordings, "total": total})
}

// handleGetRecording returns a single recording segment's metadata.
func (s *Server) handleGetRecording(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recording ID"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	rec, err := s.recRepo.Get(ctx, id)
	if errors.Is(err, recording.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "recording not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to get recording", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, rec)
}

// handlePlayRecording serves the MP4 file for a recording segment.
// Uses http.ServeFile which supports Range headers for seeking.
// The server WriteTimeout is cleared before file transfer so large segments
// (200+ MB) are not truncated mid-transfer.
func (s *Server) handlePlayRecording(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recording ID"})
		return
	}

	// DB lookup with a bounded timeout; file transfer runs without a deadline.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	rec, err := s.recRepo.Get(ctx, id)
	if errors.Is(err, recording.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "recording not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to get recording for playback", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Resolve symlinks to get the real path — filepath.Clean alone does not prevent a symlink
	// within HotPath from pointing outside the storage boundary (CG4 security).
	// EvalSymlinks also verifies the file exists, combining the path traversal and existence checks.
	cleanPath := filepath.Clean(rec.Path)
	resolvedPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.logger.Warn("recording file missing from disk", "id", id, "path", rec.Path)
			c.JSON(http.StatusNotFound, gin.H{"error": "recording file not found on disk"})
		} else {
			s.logger.Error("failed to resolve recording path", "id", id, "path", rec.Path, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		}
		return
	}
	underHot := pathutil.IsUnderPath(resolvedPath, s.resolvedHotPath)
	underCold := s.resolvedColdPath != "" && pathutil.IsUnderPath(resolvedPath, s.resolvedColdPath)
	if !underHot && !underCold {
		s.logger.Warn("recording path outside storage directories", "id", id, "path", rec.Path, "resolved", resolvedPath)
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	// Clear the write deadline so large transfers aren't truncated at the server timeout.
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.logger.Warn("failed to clear write deadline for file transfer", "error", err)
	}
	// Use c.File() (which calls http.ServeContent internally) rather than http.ServeFile.
	// http.ServeFile emits a 301 redirect when r.URL.Path != the filesystem path, which
	// always fires here because the request URL (/api/v1/recordings/:id/play) never matches
	// the resolved storage path (/media/hot/cam/…/segment.mp4).
	c.File(resolvedPath)
}

// handleDeleteRecording deletes a recording segment from DB and disk.
// DB record is deleted first — a leaked file is recoverable; a dangling DB row is not.
// Uses separate contexts for the Get and Delete operations so a slow Get cannot
// consume the full budget and leave the Delete with no time remaining.
func (s *Server) handleDeleteRecording(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recording ID"})
		return
	}

	// Lookup context is tied to the request so client cancellation is respected.
	lookupCtx, lookupCancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer lookupCancel()

	rec, err := s.recRepo.Get(lookupCtx, id)
	if errors.Is(err, recording.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "recording not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to get recording for deletion", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Resolve symlinks BEFORE any mutation (DB delete) so a symlink-based path escape
	// is caught before the DB row is irreversibly removed. If the file doesn't exist on
	// disk, fall back to a lexical check — we still want to clean up the DB record.
	cleanPath := filepath.Clean(rec.Path)
	var resolvedPath string
	var fileExists bool
	resolved, err := filepath.EvalSymlinks(cleanPath)
	if err == nil {
		resolvedPath = resolved
		fileExists = true
		underHot := pathutil.IsUnderPath(resolvedPath, s.resolvedHotPath)
		underCold := s.resolvedColdPath != "" && pathutil.IsUnderPath(resolvedPath, s.resolvedColdPath)
		if !underHot && !underCold {
			s.logger.Warn("resolved recording path escapes storage boundary", "id", id, "path", rec.Path, "resolved", resolvedPath)
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
	} else if os.IsNotExist(err) {
		// File already gone — lexical check on the raw path before allowing DB cleanup.
		underHot := pathutil.IsUnderPath(cleanPath, s.resolvedHotPath)
		underCold := s.resolvedColdPath != "" && pathutil.IsUnderPath(cleanPath, s.resolvedColdPath)
		if !underHot && !underCold {
			s.logger.Warn("recording path outside storage directories", "id", id, "path", rec.Path)
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
	} else {
		s.logger.Error("failed to resolve recording path", "id", id, "path", rec.Path, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Delete context uses context.Background() — we want to complete the write even if
	// the client disconnects mid-request. Fresh budget ensures Get timing doesn't affect it.
	deleteCtx, deleteCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer deleteCancel()

	// Delete DB record first — a leaked file on disk is recoverable by a cleanup job;
	// a dangling DB reference after file deletion is not.
	if err := s.recRepo.Delete(deleteCtx, id); err != nil {
		s.logger.Error("failed to delete recording from DB", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Delete the file from disk (containment already verified above).
	if fileExists {
		if err := os.Remove(resolvedPath); err != nil && !os.IsNotExist(err) {
			s.logger.Warn("failed to delete recording file", "id", id, "path", resolvedPath, "error", err)
		}
	}

	c.Status(http.StatusNoContent)
}

// handleRecordingTimeline returns all completed segments for a camera on a given day,
// optimized for timeline rendering (R6). Omits path, sorted chronologically.
// Query params: camera (required), date YYYY-MM-DD (required).
func (s *Server) handleRecordingTimeline(c *gin.Context) {
	cameraName := c.Query("camera")
	if cameraName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "camera query parameter is required"})
		return
	}

	dateStr := c.Query("date")
	if dateStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date query parameter is required (YYYY-MM-DD)"})
		return
	}
	date, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format (use YYYY-MM-DD)"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Verify camera exists — return 404 for non-existent cameras instead of
	// 200 + empty array, which is indistinguishable from "no recordings yet".
	// TOCTOU: a camera deleted between this check and the TimelineForDay query
	// would produce a 200 with an empty array. This is accepted — the window is
	// tiny and the consequence (empty response) is benign.
	if _, err := s.camRepo.GetByName(ctx, cameraName); err != nil {
		if errors.Is(err, camera.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		} else {
			s.logger.Error("failed to verify camera existence for timeline", "camera", cameraName, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		}
		return
	}

	segments, err := s.recRepo.TimelineForDay(ctx, cameraName, date)
	if err != nil {
		s.logger.Error("failed to get timeline", "camera", cameraName, "date", dateStr, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, segments)
}

// handleRecordingDays returns dates with at least one completed recording for a camera
// in a given month. Used by the date picker to highlight available dates (R6).
// Query params: camera (required), month YYYY-MM (required).
func (s *Server) handleRecordingDays(c *gin.Context) {
	cameraName := c.Query("camera")
	if cameraName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "camera query parameter is required"})
		return
	}

	monthStr := c.Query("month")
	if monthStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month query parameter is required (YYYY-MM)"})
		return
	}
	monthDate, err := time.ParseInLocation("2006-01", monthStr, time.Local)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid month format (use YYYY-MM)"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Verify camera exists — return 404 for non-existent cameras instead of
	// 200 + empty array, which is indistinguishable from "no recordings yet".
	// TOCTOU: a camera deleted between this check and the DaysWithRecordings query
	// would produce a 200 with an empty array. This is accepted — the window is
	// tiny and the consequence (empty response) is benign.
	if _, err := s.camRepo.GetByName(ctx, cameraName); err != nil {
		if errors.Is(err, camera.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		} else {
			s.logger.Error("failed to verify camera existence for recording days", "camera", cameraName, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		}
		return
	}

	days, err := s.recRepo.DaysWithRecordings(ctx, cameraName, monthDate.Year(), monthDate.Month())
	if err != nil {
		s.logger.Error("failed to get recording days", "camera", cameraName, "month", monthStr, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, days)
}

// handleListEvents returns events with optional filtering and pagination.
// Query params: camera_id (int), type (string), date (YYYY-MM-DD), limit (1–500, default 50), offset (int).
func (s *Server) handleListEvents(c *gin.Context) {
	f := detection.ListFilter{Limit: 50}

	if camIDStr := c.Query("camera_id"); camIDStr != "" {
		id, err := strconv.Atoi(camIDStr)
		if err != nil || id < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid camera_id"})
			return
		}
		f.CameraID = &id
	}

	f.Type = c.Query("type")

	if dateStr := c.Query("date"); dateStr != "" {
		if _, err := time.ParseInLocation("2006-01-02", dateStr, time.Local); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format; expected YYYY-MM-DD"})
			return
		}
		f.Date = dateStr
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		lim, err := strconv.Atoi(limitStr)
		if err != nil || lim < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be between 1 and 500"})
			return
		}
		if lim > 500 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit exceeds maximum (500)"})
			return
		}
		f.Limit = lim
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		off, err := strconv.Atoi(offsetStr)
		if err != nil || off < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset"})
			return
		}
		f.Offset = off
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	events, total, err := s.detRepo.List(ctx, f)
	if err != nil {
		s.logger.Error("failed to list events", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events, "total": total})
}

// handleGetEvent returns a single event by ID.
func (s *Server) handleGetEvent(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event ID"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	ev, err := s.detRepo.GetByID(ctx, id)
	if errors.Is(err, detection.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to get event", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, ev)
}

// handleEventThumbnail serves the JPEG snapshot for a detection event.
// Uses the same path-containment check as handlePlayRecording for security.
func (s *Server) handleEventThumbnail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event ID"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	ev, err := s.detRepo.GetByID(ctx, id)
	if errors.Is(err, detection.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to get event for thumbnail", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if ev.Thumbnail == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "event has no thumbnail"})
		return
	}

	cleanPath := filepath.Clean(ev.Thumbnail)
	resolvedPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.logger.Warn("thumbnail file missing from disk", "id", id, "path", ev.Thumbnail)
			c.JSON(http.StatusNotFound, gin.H{"error": "thumbnail file not found on disk"})
		} else {
			s.logger.Error("failed to resolve thumbnail path", "id", id, "path", ev.Thumbnail, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		}
		return
	}
	if !pathutil.IsUnderPath(resolvedPath, s.resolvedSnapshotPath) {
		s.logger.Warn("thumbnail path outside snapshot directory", "id", id, "path", ev.Thumbnail, "resolved", resolvedPath)
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	// Clear write deadline — thumbnail files are small but be consistent
	// with the recording play handler so future large thumbnails work correctly.
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.logger.Warn("failed to clear write deadline for thumbnail", "error", err)
	}
	c.File(resolvedPath)
}

// handleDeleteEvent deletes an event from the DB and removes the thumbnail file.
// DB record is deleted first — a leaked file is recoverable; a dangling DB row is not.
func (s *Server) handleDeleteEvent(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event ID"})
		return
	}

	// Look up thumbnail path before deletion so we can clean up the file.
	lookupCtx, lookupCancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer lookupCancel()

	ev, err := s.detRepo.GetByID(lookupCtx, id)
	if errors.Is(err, detection.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to get event for deletion", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Delete context uses context.Background() — we want to complete the write even if
	// the client disconnects. Fresh budget so lookup time doesn't affect delete budget.
	deleteCtx, deleteCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer deleteCancel()

	// Delete DB record first — file cleanup is best-effort.
	if err := s.detRepo.Delete(deleteCtx, id); err != nil {
		if errors.Is(err, detection.ErrNotFound) {
			// Concurrent deletion already removed the row — treat as idempotent success.
			c.Status(http.StatusNoContent)
			return
		}
		s.logger.Error("failed to delete event from DB", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Remove thumbnail from disk — best-effort, DB row is already gone.
	// Validate path containment before removal to prevent an attacker who can
	// write to the events table from deleting arbitrary files.
	if ev.Thumbnail != "" {
		cleanThumbnail := filepath.Clean(ev.Thumbnail)
		resolved, resolveErr := filepath.EvalSymlinks(cleanThumbnail)
		if resolveErr != nil && !os.IsNotExist(resolveErr) {
			s.logger.Warn("could not resolve thumbnail path; skipping file removal",
				"id", id, "path", ev.Thumbnail, "error", resolveErr)
		} else if resolveErr == nil && !pathutil.IsUnderPath(resolved, s.resolvedSnapshotPath) {
			s.logger.Warn("thumbnail path escapes snapshot directory; skipping file removal",
				"id", id, "path", ev.Thumbnail)
		} else if resolveErr == nil {
			if err := os.Remove(resolved); err != nil && !os.IsNotExist(err) {
				s.logger.Warn("failed to delete thumbnail file",
					"id", id, "path", resolved, "error", err)
			}
		}
	}

	c.Status(http.StatusNoContent)
}

// handleStreamWS proxy is defined in stream_proxy.go

// handleEventHeatmap returns detection event density in 5-minute buckets for a camera on a
// given date (Phase 6, R6). Used by the frontend to render the heatmap overlay on the timeline.
// Query params: camera_id (int, required), date YYYY-MM-DD (required).
func (s *Server) handleEventHeatmap(c *gin.Context) {
	camIDStr := c.Query("camera_id")
	if camIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "camera_id query parameter is required"})
		return
	}
	cameraID, err := strconv.Atoi(camIDStr)
	if err != nil || cameraID < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid camera_id"})
		return
	}

	dateStr := c.Query("date")
	if dateStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date query parameter is required (YYYY-MM-DD)"})
		return
	}
	date, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format (use YYYY-MM-DD)"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Verify camera exists — return 404 instead of 200+empty-slice for unknown camera IDs.
	// An empty heatmap is indistinguishable from "camera exists but has no detections" otherwise.
	// Mirrors handleRecordingTimeline's existence check (same rationale, same TOCTOU acceptance).
	if _, err := s.camRepo.GetByID(ctx, cameraID); err != nil {
		if errors.Is(err, camera.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
			return
		}
		s.logger.Error("failed to verify camera existence for heatmap", "camera_id", cameraID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	buckets, err := s.detRepo.GetHeatmap(ctx, cameraID, date)
	if err != nil {
		s.logger.Error("failed to get event heatmap", "camera_id", cameraID, "date", dateStr, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, buckets)
}

// handleEventStream opens a Server-Sent Events connection and streams persisted events
// to the client in real-time (Phase 6, CG8). The connection stays open until the client
// disconnects or the server shuts down. A 30-second heartbeat comment keeps the connection
// alive through proxies that close idle connections.
//
// Only "events.persisted" bus events are forwarded. These are published by persistEvents
// after each successful DB INSERT and carry a safe EventRecord-compatible payload:
// DB-assigned ID, "start_time" (not raw "timestamp"), and a thumbnail indicator instead
// of the absolute filesystem path. Raw eventbus.Event is never sent to clients.
func (s *Server) handleEventStream(c *gin.Context) {
	// Clear the write timeout — this connection is intentionally long-lived.
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.logger.Warn("failed to clear write deadline for SSE", "error", err)
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // disable nginx proxy read buffering

	// Subscribe to "events.persisted" directly — only these events are forwarded to SSE clients.
	// This avoids receiving the full event stream and discarding most of it in the handler.
	ch := s.eventBus.Subscribe("events.persisted")
	defer s.eventBus.Unsubscribe(ch)

	// Send a comment to flush headers immediately so the client knows the connection is open.
	if _, err := fmt.Fprintf(c.Writer, ": connected\n\n"); err != nil {
		return
	}
	c.Writer.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return // client disconnected or server shutdown
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(c.Writer, ": heartbeat\n\n"); err != nil {
				return // client write failed (disconnected)
			}
			c.Writer.Flush()
		case event, ok := <-ch:
			if !ok {
				return // event bus closed (server shutting down)
			}
			data, err := json.Marshal(event.Data)
			if err != nil {
				s.logger.Warn("failed to marshal SSE event payload", "error", err)
				continue
			}
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
				return // client write failed (disconnected)
			}
			c.Writer.Flush()
		}
	}
}

// normalizeZonesRaw converts a JSON "null" literal (or absent field) to Go nil so the
// manager's zone-preservation logic (cam.Zones == nil → keep existing) fires for absent
// and explicit-null payloads.
// "[]" is intentionally NOT treated as nil — an explicit empty array clears all zones.
// This lets clients distinguish "don't touch zones" (omit field / send null) from
// "remove all zones" (send []).
func normalizeZonesRaw(raw json.RawMessage) json.RawMessage {
	if string(raw) == "null" {
		return nil
	}
	return raw
}

// validateZonesJSON validates that zones, when provided, is a valid JSON array of Zone objects.
// nil/absent/null means "don't update zones" — manager preserves existing.
// "[]" is valid and means "clear all zones".
// Returns a descriptive error for malformed JSON or structurally invalid zone data.
func validateZonesJSON(zones json.RawMessage) error {
	if len(zones) == 0 || string(zones) == "null" {
		return nil // absent or null → no update, manager preserves existing
	}
	if string(zones) == "[]" {
		return nil // explicit empty array → clears all zones; structurally valid, no per-zone checks needed
	}
	var parsed []detection.Zone
	if err := json.Unmarshal(zones, &parsed); err != nil {
		return fmt.Errorf("zones must be a JSON array of zone objects: %w", err)
	}
	for i, z := range parsed {
		if z.ID == "" {
			return fmt.Errorf("zone[%d]: id is required", i)
		}
		if z.Name == "" {
			return fmt.Errorf("zone[%d]: name is required", i)
		}
		if z.Type != detection.ZoneInclude && z.Type != detection.ZoneExclude {
			return fmt.Errorf("zone[%d]: type must be %q or %q", i, detection.ZoneInclude, detection.ZoneExclude)
		}
		if len(z.Points) < 3 {
			return fmt.Errorf("zone[%d]: polygon must have at least 3 points", i)
		}
		for j, pt := range z.Points {
			if pt.X < 0 || pt.X > 1 || pt.Y < 0 || pt.Y > 1 {
				return fmt.Errorf("zone[%d] point[%d]: x and y must be normalised to [0.0, 1.0]", i, j)
			}
		}
	}
	return nil
}

// validateWebhookURL checks that a webhook token is a well-formed HTTP/HTTPS URL.
// Blocks file://, ftp://, and other schemes, and also blocks loopback/private
// addresses to prevent SSRF attacks that could reach go2rtc (127.0.0.1:1984)
// or other internal services.
//
// DNS rebinding mitigation: for hostname-based URLs (not literal IPs), the
// hostname is also resolved and each returned address is checked. This removes
// the common attack vector where a domain is registered pointing to a public IP
// at validation time and later re-pointed to a private IP before delivery.
// Full prevention of rebinding at delivery time would require re-validation on
// every webhook call, which is out of scope here.
func validateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook URL must use http or https scheme, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("webhook URL must include a host")
	}
	hostname := u.Hostname()
	switch hostname {
	case "localhost", "::1", "0.0.0.0":
		return fmt.Errorf("webhook URL must not target localhost")
	}
	if ip := net.ParseIP(hostname); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("webhook URL must not target private or loopback addresses")
		}
	} else {
		// Hostname — resolve and check all returned addresses to mitigate DNS rebinding.
		addrs, lookupErr := net.LookupHost(hostname)
		if lookupErr == nil {
			for _, addr := range addrs {
				resolved := net.ParseIP(addr)
				if resolved != nil && (resolved.IsLoopback() || resolved.IsPrivate() || resolved.IsLinkLocalUnicast()) {
					return fmt.Errorf("webhook URL resolves to a private or loopback address")
				}
			}
		}
		// If DNS lookup fails, allow the URL — the webhook delivery will fail gracefully.
	}
	return nil
}

// ─── Notification handlers (Phase 8, R9) ────────────────────────────────────

// notifUserID returns the current user's DB ID from the Gin context.
// When auth is disabled (authService == nil), all requests are treated as
// user ID 1 (the admin account created by ensureAdminUser on first run).
// Returns -1 and aborts with 500 if auth is enabled but the user ID is missing
// from the context (indicates an internal middleware misconfiguration).
func (s *Server) notifUserID(c *gin.Context) int {
	if s.authService == nil {
		return 1 // auth disabled — use the single admin user
	}
	if uid, ok := c.Get(auth.CtxKeyUserID); ok {
		if id, ok := uid.(int); ok {
			return id
		}
	}
	// Should never reach here if the auth middleware is correctly applied.
	s.logger.Error("notifUserID: user ID missing from context — middleware misconfiguration")
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	return -1
}

// notifAvailable returns true when the notification repository is wired up.
func (s *Server) notifAvailable(c *gin.Context) bool {
	if s.notifRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notifications not configured"})
		return false
	}
	return true
}

// handleCreateNotifToken registers a device token for push delivery.
// POST /api/v1/notifications/tokens
// Body: {"provider":"fcm"|"apns"|"webhook", "token":"...", "label":"..."}
func (s *Server) handleCreateNotifToken(c *gin.Context) {
	if !s.notifAvailable(c) {
		return
	}
	var req struct {
		Provider string `json:"provider" binding:"required"`
		Token    string `json:"token"    binding:"required"`
		Label    string `json:"label"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider and token are required"})
		return
	}
	switch req.Provider {
	case "fcm", "apns", "webhook":
		// valid
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider must be fcm, apns, or webhook"})
		return
	}

	// Validate webhook URLs to prevent SSRF — only allow http/https schemes.
	if req.Provider == "webhook" {
		if err := validateWebhookURL(req.Token); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	userID := s.notifUserID(c)
	if userID < 0 {
		return // notifUserID already aborted with 500
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	rec, err := s.notifRepo.UpsertToken(ctx, userID, req.Token, req.Provider, req.Label)
	if err != nil {
		s.logger.Error("failed to upsert notification token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, rec)
}

// handleListNotifTokens returns all registered device tokens for the current user.
// GET /api/v1/notifications/tokens
func (s *Server) handleListNotifTokens(c *gin.Context) {
	if !s.notifAvailable(c) {
		return
	}
	userID := s.notifUserID(c)
	if userID < 0 {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tokens, err := s.notifRepo.ListTokensByUser(ctx, userID)
	if err != nil {
		s.logger.Error("failed to list notification tokens", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, tokens)
}

// handleDeleteNotifToken removes a registered device token.
// DELETE /api/v1/notifications/tokens/:id
func (s *Server) handleDeleteNotifToken(c *gin.Context) {
	if !s.notifAvailable(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token ID"})
		return
	}

	userID := s.notifUserID(c)
	if userID < 0 {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := s.notifRepo.DeleteToken(ctx, id, userID); err != nil {
		if errors.Is(err, notification.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
			return
		}
		s.logger.Error("failed to delete notification token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.Status(http.StatusNoContent)
}

// handleListNotifPrefs returns the current user's notification preferences.
// GET /api/v1/notifications/prefs
func (s *Server) handleListNotifPrefs(c *gin.Context) {
	if !s.notifAvailable(c) {
		return
	}
	userID := s.notifUserID(c)
	if userID < 0 {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	prefs, err := s.notifRepo.ListPrefsByUser(ctx, userID)
	if err != nil {
		s.logger.Error("failed to list notification prefs", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, prefs)
}

// handleUpsertNotifPref creates or updates a notification preference.
// PUT /api/v1/notifications/prefs
// Body: {"event_type":"...", "camera_id":null|int, "enabled":bool, "critical":bool}
func (s *Server) handleUpsertNotifPref(c *gin.Context) {
	if !s.notifAvailable(c) {
		return
	}
	var req struct {
		EventType string `json:"event_type" binding:"required"`
		CameraID  *int   `json:"camera_id"` // null = all cameras
		Enabled   bool   `json:"enabled"`
		Critical  bool   `json:"critical"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "event_type is required"})
		return
	}

	// Validate event_type against the set of event types the system actually emits.
	// This prevents garbage rows and potential stored-XSS if the frontend renders the value.
	validEventTypes := map[string]bool{
		"*":                   true,
		"detection":           true,
		"face_match":          true, // Phase 13 (R11)
		"audio_detection":     true, // Phase 13 (R12)
		"camera.offline":      true,
		"camera.online":       true,
		"camera.connected":    true,
		"camera.disconnected": true,
		"camera.error":        true,
	}
	if !validEventTypes[req.EventType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unrecognised event_type"})
		return
	}

	userID := s.notifUserID(c)
	if userID < 0 {
		return // notifUserID already aborted
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	pref := notification.PrefRecord{
		UserID:    userID,
		EventType: req.EventType,
		CameraID:  req.CameraID,
		Enabled:   req.Enabled,
		Critical:  req.Critical,
	}
	result, err := s.notifRepo.UpsertPref(ctx, pref)
	if err != nil {
		s.logger.Error("failed to upsert notification pref", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// handleDeleteNotifPref removes a notification preference by ID.
// DELETE /api/v1/notifications/prefs/:id
func (s *Server) handleDeleteNotifPref(c *gin.Context) {
	if !s.notifAvailable(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pref ID"})
		return
	}

	userID := s.notifUserID(c)
	if userID < 0 {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := s.notifRepo.DeletePref(ctx, id, userID); err != nil {
		if errors.Is(err, notification.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "pref not found"})
			return
		}
		s.logger.Error("failed to delete notification pref", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.Status(http.StatusNoContent)
}

// handleListNotifLog returns recent notification delivery log entries for the current user.
// GET /api/v1/notifications/log?limit=50
func (s *Server) handleListNotifLog(c *gin.Context) {
	if !s.notifAvailable(c) {
		return
	}
	limit := 50
	if lStr := c.Query("limit"); lStr != "" {
		l, err := strconv.Atoi(lStr)
		if err != nil || l < 1 || l > 500 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be 1–500"})
			return
		}
		limit = l
	}

	userID := s.notifUserID(c)
	if userID < 0 {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	logs, err := s.notifRepo.ListLogsByUser(ctx, userID, limit)
	if err != nil {
		s.logger.Error("failed to list notification log", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, logs)
}

// ─── Remote access handlers (Phase 12, CG11, R8) ────────────────────────────

// handleRelayICEServers returns ICE server config for WebRTC peer connections (Phase 12, CG11).
// GET /api/v1/relay/ice-servers
// Always returns the STUN server; includes TURN credentials when relay.enabled=true.
//
// Design decision: TURN credentials are served to ALL authenticated users (not admin-only).
// This is intentional — every user (including non-admin viewers) needs TURN relay access
// for remote WebRTC streaming when symmetric NAT blocks direct P2P. The credentials are
// long-lived shared secrets from the config, matching go2rtc's own ice_servers config.
// Future improvement: mint short-lived TURN credentials per RFC 5766 §9 (time-limited HMAC)
// instead of exposing the shared secret. For now this is acceptable because:
// 1. The endpoint requires authentication (JWT cookie).
// 2. TURN abuse is bounded by coturn's bandwidth/allocation limits.
// 3. The credentials only grant relay access, not admin access to the NVR.
// The mobile app passes the returned list to flutter_webrtc's RTCConfiguration.
func (s *Server) handleRelayICEServers(c *gin.Context) {
	type iceServer struct {
		URLs       []string `json:"urls"`
		Username   string   `json:"username,omitempty"`
		Credential string   `json:"credential,omitempty"`
	}
	cfg := s.snapConfig()
	servers := []iceServer{
		{URLs: []string{cfg.Relay.STUNServer}},
	}
	if cfg.Relay.Enabled && cfg.Relay.TURNServer != "" {
		servers = append(servers, iceServer{
			URLs:       []string{cfg.Relay.TURNServer},
			Username:   cfg.Relay.TURNUser,
			Credential: cfg.Relay.TURNPass,
		})
	}
	c.JSON(http.StatusOK, gin.H{"ice_servers": servers})
}

// handlePairingQR generates a short-lived pairing code for QR-based mobile pairing (Phase 12, CG11).
// POST /api/v1/pairing/qr  (admin only)
// Returns {"code":"<uuid>","expires_at":"<RFC3339>"} — the web UI encodes this into a QR image.
// The mobile app scans the QR and calls POST /pairing/redeem to exchange the code for a session.
func (s *Server) handlePairingQR(c *gin.Context) {
	// Pairing requires auth to be enabled — without it there's no user/session model
	// and the FK to pairing_codes.user_id would reference a nonexistent user (Issue #6).
	if s.authService == nil {
		c.JSON(http.StatusOK, gin.H{"message": "auth disabled — pairing not available"})
		return
	}
	if !s.requireAdmin(c) {
		return
	}
	userID := s.notifUserID(c)
	if userID < 0 {
		return // notifUserID already aborted with 500
	}

	// Generate UUID v4 using crypto/rand (no new dependencies).
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		s.logger.Error("pairing: rand.Read failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate pairing code"})
		return
	}
	b[6] = (b[6] & 0x0f) | 0x40 // UUID version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant bits
	code := fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)

	expiresAt := time.Now().UTC().Add(15 * time.Minute)

	// Purge expired codes before inserting — prevents unbounded table growth on repeated
	// calls. Uses a short independent timeout so a slow DELETE cannot starve the INSERT.
	delCtx, delCancel := context.WithTimeout(c.Request.Context(), time.Second)
	_, _ = s.db.ExecContext(delCtx,
		`DELETE FROM pairing_codes WHERE expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339),
	)
	delCancel()

	insCtx, insCancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer insCancel()

	_, err := s.db.ExecContext(insCtx,
		`INSERT INTO pairing_codes (code, user_id, expires_at) VALUES (?, ?, ?)`,
		code, userID, expiresAt.Format(time.RFC3339),
	)
	if err != nil {
		s.logger.Error("pairing: insert failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate pairing code"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code":       code,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

// handlePairingRedeem exchanges a valid pairing code for a session (Phase 12, CG11).
// POST /api/v1/pairing/redeem  body: {"code":"<uuid>"}
// Public endpoint — the mobile app has no auth session when it calls this.
// On success, sets httpOnly auth cookies (same as /auth/login) and marks the code as used.
// Returns 401 for invalid/expired/already-used codes.
// Rate-limited with the same limiter as /auth/login to prevent brute-force enumeration.
func (s *Server) handlePairingRedeem(c *gin.Context) {
	if s.authService == nil {
		c.JSON(http.StatusOK, gin.H{"message": "auth disabled"})
		return
	}

	// Rate limit — use a separate key namespace from /auth/login so that repeated
	// failed pairing attempts don't lock out login from the same IP (CG6).
	ip := c.ClientIP()
	if !s.loginLimiter.allow(ip + "_pairing") {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many attempts, try again later"})
		return
	}

	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code is required"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Atomic claim + user lookup via RETURNING: marks used_at and retrieves user_id in a
	// single statement, eliminating the TOCTOU race and the "code consumed but lookup failed"
	// failure mode of the previous two-query approach.
	now := time.Now().UTC().Format(time.RFC3339)
	var userID int
	err := s.db.QueryRowContext(ctx,
		`UPDATE pairing_codes SET used_at = ?
		 WHERE code = ? AND used_at IS NULL AND expires_at > ?
		 RETURNING user_id`,
		now, req.Code, now,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired pairing code"})
		return
	}
	if err != nil {
		s.logger.Error("pairing: claim failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Issue session tokens for the user who generated the code.
	pair, pairErr := s.authService.IssueTokenPairForUserID(ctx, userID)
	if pairErr != nil {
		s.logger.Error("pairing: token issue failed", "error", pairErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	s.loginLimiter.reset(ip)
	auth.SetTokenCookies(c, pair, s.snapConfig().Auth.SecureCookie)
	c.JSON(http.StatusOK, gin.H{"message": "paired"})
}

// ─── OIDC SSO handlers (Phase 7, CG6) ───────────────────────────────────────

// handleOIDCLogin initiates the OIDC authorization code flow.
// GET /api/v1/auth/oidc/login — redirects the browser to the identity provider.
// Only reachable when oidcProvider is non-nil (s.oidcProvider != nil in registerRoutes).
func (s *Server) handleOIDCLogin(c *gin.Context) {
	url, err := s.oidcProvider.AuthURL()
	if err != nil {
		s.logger.Error("OIDC: failed to generate auth URL", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "OIDC unavailable"})
		return
	}
	c.Redirect(http.StatusFound, url)
}

// handleOIDCCallback handles the authorization code redirect from the identity provider.
// GET /api/v1/auth/oidc/callback?code=...&state=...
// Validates state, exchanges the code, finds or provisions a local user, and sets session cookies.
func (s *Server) handleOIDCCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		c.Redirect(http.StatusFound, "/login?error=oidc_missing_params")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	sub, username, email, err := s.oidcProvider.Exchange(ctx, code, state)
	if err != nil {
		s.logger.Warn("OIDC callback: token exchange failed", "error", err)
		c.Redirect(http.StatusFound, "/login?error=oidc_failed")
		return
	}

	pair, err := s.authService.OIDCLoginOrCreate(ctx, sub, username, email)
	if err != nil {
		s.logger.Error("OIDC callback: login/create failed", "error", err)
		c.Redirect(http.StatusFound, "/login?error=oidc_failed")
		return
	}

	auth.SetTokenCookies(c, pair, s.snapConfig().Auth.SecureCookie)
	c.Redirect(http.StatusFound, "/live")
}

// ─── Face recognition handlers (Phase 13, R11) ──────────────────────────────

// handleListFaces returns all enrolled faces (without embeddings).
// GET /api/v1/faces
func (s *Server) handleListFaces(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	faces, err := s.faceRepo.List(ctx)
	if err != nil {
		s.logger.Error("failed to list faces", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"faces": faces})
}

// handleGetFace returns a single enrolled face by ID.
// GET /api/v1/faces/:id
func (s *Server) handleGetFace(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid face ID"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	face, err := s.faceRepo.GetByID(ctx, id)
	if errors.Is(err, detection.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "face not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to get face", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, face)
}

// handleCreateFace enrolls a new face by receiving a name and a JPEG reference photo.
// The embedding is extracted via the sentinel-infer face embedding endpoint.
// POST /api/v1/faces — multipart/form-data: name (text), image (JPEG file)
// For now, accepts a pre-computed embedding JSON array if the inference endpoint
// is not yet available, allowing manual enrollment via API.
func (s *Server) handleCreateFace(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	if !s.requireAdmin(c) {
		return
	}

	var req struct {
		Name      string    `json:"name" binding:"required"`
		Embedding []float32 `json:"embedding" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and embedding are required"})
		return
	}
	if len(req.Name) == 0 || len(req.Name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1-128 characters"})
		return
	}
	// sentinel-infer uses ArcFace which produces 512-dim unit vectors.
	// Reject anything else so cosine similarity calculations are consistent
	// and malformed clients cannot store oversized BLOBs.
	if len(req.Embedding) != 512 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "embedding must have exactly 512 dimensions (ArcFace)"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	face, err := s.faceRepo.Create(ctx, req.Name, req.Embedding, "")
	if err != nil {
		s.logger.Error("failed to create face", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, face)
}

// handleUpdateFace renames an enrolled face.
// PUT /api/v1/faces/:id   body: {"name":"new name"}
func (s *Server) handleUpdateFace(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	if !s.requireAdmin(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid face ID"})
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if len(req.Name) == 0 || len(req.Name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1-128 characters"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := s.faceRepo.Update(ctx, id, req.Name); err != nil {
		if errors.Is(err, detection.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "face not found"})
			return
		}
		s.logger.Error("failed to update face", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	// Return the updated face record — consistent with other update endpoints.
	updated, getErr := s.faceRepo.GetByID(ctx, id)
	if getErr != nil {
		s.logger.Error("failed to fetch updated face", "id", id, "error", getErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// handleDeleteFace removes an enrolled face by ID.
// DELETE /api/v1/faces/:id
func (s *Server) handleDeleteFace(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	if !s.requireAdmin(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid face ID"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := s.faceRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, detection.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "face not found"})
			return
		}
		s.logger.Error("failed to delete face", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.Status(http.StatusNoContent)
}

// handleEnrollFace enrolls a new face from a JPEG photo via sentinel-infer (R11).
// POST /api/v1/faces/enroll — multipart/form-data: name (text field), image (JPEG file)
// The image is forwarded to sentinel-infer /v1/face/embed; the first detected face
// embedding is stored. Returns 422 if no face is detected in the photo.
// When face recognition is disabled (faceRecognizer nil), returns 503.
func (s *Server) handleEnrollFace(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	if s.faceRecognizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition inference is disabled; use the raw embedding API instead"})
		return
	}
	if !s.requireAdmin(c) {
		return
	}

	name := c.PostForm("name")
	if name == "" || len(name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1-128 characters"})
		return
	}

	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image file is required"})
		return
	}

	// Limit photo size to 16 MB (generous for a single reference JPEG).
	const maxPhotoBytes = 16 << 20
	if file.Size > maxPhotoBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "image must be ≤ 16 MB"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read uploaded file"})
		return
	}
	defer f.Close()

	jpegBytes, err := io.ReadAll(io.LimitReader(f, maxPhotoBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read image data"})
		return
	}

	// Call sentinel-infer to extract face embeddings from the photo (30s budget).
	embedCtx, embedCancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer embedCancel()

	embeddings, err := s.faceRecognizer.EmbedFaces(embedCtx, jpegBytes, 1)
	if err != nil {
		s.logger.Warn("face embed call failed", "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("face embedding failed: %v", err)})
		return
	}
	if len(embeddings) == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "no face detected in the uploaded photo"})
		return
	}

	// Use the first (and only, max_faces=1) detected face.
	embedding := embeddings[0].Embedding
	if len(embedding) != 512 {
		s.logger.Warn("sentinel-infer returned unexpected embedding dimension",
			"got", len(embedding), "expected", 512)
		c.JSON(http.StatusBadGateway, gin.H{"error": "unexpected embedding dimension from inference server"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	face, err := s.faceRepo.Create(ctx, name, embedding, "")
	if err != nil {
		s.logger.Error("failed to enroll face", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, face)
}

// ---------- Migration / Import (Phase 14, R15) ----------

// parseImportFile reads the uploaded file and format, then returns a parsed ImportResult.
func (s *Server) parseImportFile(c *gin.Context) (*importers.ImportResult, bool) {
	format := c.PostForm("format")
	if format == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "format is required (blue_iris or frigate)"})
		return nil, false
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file upload is required"})
		return nil, false
	}
	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read uploaded file"})
		return nil, false
	}
	defer f.Close()

	// Use io.ReadAll with LimitReader — f.Read may return fewer bytes than
	// file.Size (partial read), and file.Size can be spoofed by the client.
	data, err := io.ReadAll(io.LimitReader(f, 5*1024*1024+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read file contents"})
		return nil, false
	}
	if len(data) > 5*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file exceeds 5MB limit"})
		return nil, false
	}

	var result *importers.ImportResult
	switch format {
	case "blue_iris":
		result = importers.ParseBlueIris(data)
	case "frigate":
		result = importers.ParseFrigate(data)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported format — use 'blue_iris' or 'frigate'"})
		return nil, false
	}
	return result, true
}

// handleImportPreview parses an uploaded config file and returns a preview of
// what would be imported — without touching the database (dry run).
// POST /api/v1/import/preview — multipart/form-data: format (text), file (upload)
func (s *Server) handleImportPreview(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	result, ok := s.parseImportFile(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, result)
}

// handleImportExecute parses the uploaded file and creates cameras in the database.
// Cameras that already exist (by name) are skipped with a warning.
// POST /api/v1/import — multipart/form-data: format (text), file (upload)
func (s *Server) handleImportExecute(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	result, ok := s.parseImportFile(c)
	if !ok {
		return
	}
	if len(result.Cameras) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"imported": 0,
			"skipped":  0,
			"errors":   result.Errors,
			"warnings": result.Warnings,
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	imported := 0
	skipped := 0
	// Copy parse errors into a separate slice — appending to result.Errors would
	// also append to result.Warnings via shared backing array aliasing.
	importErrors := make([]string, len(result.Errors))
	copy(importErrors, result.Errors)

	for _, cam := range result.Cameras {
		rec := &camera.CameraRecord{
			Name:       cam.Name,
			Enabled:    cam.Enabled,
			MainStream: cam.MainStream,
			SubStream:  cam.SubStream,
			Record:     cam.Record,
			Detect:     cam.Detect,
			ONVIFHost:  cam.ONVIFHost,
			ONVIFPort:  cam.ONVIFPort,
			ONVIFUser:  cam.ONVIFUser,
			ONVIFPass:  cam.ONVIFPass,
		}

		_, err := s.camManager.AddCamera(ctx, rec)
		if err != nil {
			if errors.Is(err, camera.ErrDuplicate) {
				skipped++
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("camera %q: already exists, skipped", cam.Name))
			} else {
				importErrors = append(importErrors,
					fmt.Sprintf("camera %q: %v", cam.Name, err))
			}
			continue
		}
		imported++
	}

	s.logger.Info("import completed",
		"format", result.Format,
		"imported", imported,
		"skipped", skipped,
		"errors", len(importErrors),
	)

	c.JSON(http.StatusOK, gin.H{
		"imported": imported,
		"skipped":  skipped,
		"errors":   importErrors,
		"warnings": result.Warnings,
	})
}

// ─── Retention Rules (R14) ────────────────────────────────────────────────────

// handleListRetentionRules returns all configured retention rules.
// GET /api/v1/retention/rules
func (s *Server) handleListRetentionRules(c *gin.Context) {
	rules, err := s.retentionRepo.List(c.Request.Context())
	if err != nil {
		s.logger.Error("retention rules: list failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list retention rules"})
		return
	}
	c.JSON(http.StatusOK, rules)
}

// handleCreateRetentionRule creates a new retention rule.
// POST /api/v1/retention/rules
// Body: {"camera_id": 1, "event_type": "detection", "events_days": 30}
// camera_id and event_type are optional; omit for wildcard rules.
func (s *Server) handleCreateRetentionRule(c *gin.Context) {
	var req struct {
		CameraID   *int    `json:"camera_id"`
		EventType  *string `json:"event_type"`
		EventsDays int     `json:"events_days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.EventsDays < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "events_days must be at least 1"})
		return
	}
	if req.EventType != nil {
		valid := false
		for _, t := range storage.KnownEventTypes {
			if *req.EventType == t {
				valid = true
				break
			}
		}
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown event_type; valid types: detection, face_match, audio_detection, camera.online, camera.offline, camera.connected, camera.disconnected, camera.error"})
			return
		}
	}

	rule, err := s.retentionRepo.Create(c.Request.Context(), req.CameraID, req.EventType, req.EventsDays)
	if err != nil {
		if errors.Is(err, storage.ErrRuleConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "a rule for this camera/event-type combination already exists"})
			return
		}
		s.logger.Error("retention rules: create failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create retention rule"})
		return
	}
	c.JSON(http.StatusCreated, rule)
}

// handleUpdateRetentionRule updates the events_days for an existing rule.
// PUT /api/v1/retention/rules/:id
// Body: {"events_days": 60}
func (s *Server) handleUpdateRetentionRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}

	var req struct {
		EventsDays int `json:"events_days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.EventsDays < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "events_days must be at least 1"})
		return
	}

	rule, err := s.retentionRepo.Update(c.Request.Context(), id, req.EventsDays)
	if err != nil {
		if errors.Is(err, storage.ErrRuleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "retention rule not found"})
			return
		}
		s.logger.Error("retention rules: update failed", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update retention rule"})
		return
	}
	c.JSON(http.StatusOK, rule)
}

// handleDeleteRetentionRule deletes a retention rule by ID.
// DELETE /api/v1/retention/rules/:id
func (s *Server) handleDeleteRetentionRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}

	if err := s.retentionRepo.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, storage.ErrRuleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "retention rule not found"})
			return
		}
		s.logger.Error("retention rules: delete failed", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete retention rule"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ─── Model Management (R10) ─────────────────────────────────────────────────

// modelEntry is the JSON response for a single model in the list.
type modelEntry struct {
	Filename    string `json:"filename"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SizeBytes   int64  `json:"size_bytes"`
	Installed   bool   `json:"installed"`
	Curated     bool   `json:"curated"` // true = part of the built-in manifest
}

// handleListModels returns the curated manifest merged with locally installed models.
// GET /api/v1/models
func (s *Server) handleListModels(c *gin.Context) {
	local, err := s.modelManager.ListLocal()
	if err != nil {
		s.logger.Error("models: list local failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list local models"})
		return
	}
	localSet := make(map[string]bool, len(local))
	for _, f := range local {
		localSet[f] = true
	}

	var entries []modelEntry

	// Emit curated manifest entries first with installed status.
	seen := make(map[string]bool)
	for _, m := range models.Manifest {
		entries = append(entries, modelEntry{
			Filename:    m.Filename,
			Name:        m.Name,
			Description: m.Description,
			SizeBytes:   m.SizeBytes,
			Installed:   localSet[m.Filename],
			Curated:     true,
		})
		seen[m.Filename] = true
	}

	// Append locally installed models not in the manifest (user uploads).
	for _, f := range local {
		if seen[f] {
			continue
		}
		// Stat the file for size.
		var size int64
		cfg := s.snapConfig()
		if fi, err := os.Stat(filepath.Join(cfg.Models.Dir, f)); err == nil {
			size = fi.Size()
		}
		entries = append(entries, modelEntry{
			Filename:  f,
			Name:      f,
			SizeBytes: size,
			Installed: true,
			Curated:   false,
		})
	}

	c.JSON(http.StatusOK, entries)
}

// handleDownloadModel triggers download of a curated model from the manifest.
// POST /api/v1/models/:filename/download
func (s *Server) handleDownloadModel(c *gin.Context) {
	filename := c.Param("filename")

	// Find the model in the curated manifest.
	var info *models.ModelInfo
	for _, m := range models.Manifest {
		if m.Filename == filename {
			info = &m
			break
		}
	}
	if info == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found in curated manifest"})
		return
	}

	path, err := s.modelManager.EnsureModel(*info)
	if err != nil {
		s.logger.Error("models: download failed", "model", filename, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "download failed: " + err.Error()})
		return
	}

	s.logger.Info("model downloaded", "model", filename, "path", path)
	c.JSON(http.StatusOK, gin.H{"filename": filename, "path": path, "status": "installed"})
}

// handleUploadModel accepts a multipart ONNX file upload.
// POST /api/v1/models/upload  (multipart/form-data, field "file")
func (s *Server) handleUploadModel(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing 'file' field: " + err.Error()})
		return
	}
	defer file.Close()

	filename := header.Filename
	if filepath.Ext(filename) != ".onnx" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only .onnx model files are accepted"})
		return
	}
	// Sanitise filename — strip directory components.
	filename = filepath.Base(filename)

	cfg := s.snapConfig()
	if err := os.MkdirAll(cfg.Models.Dir, 0755); err != nil {
		s.logger.Error("models: mkdir failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create models directory"})
		return
	}

	destPath := filepath.Join(cfg.Models.Dir, filename)
	tmp := destPath + ".upload"
	f, err := os.Create(tmp)
	if err != nil {
		s.logger.Error("models: create temp file failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create temp file"})
		return
	}

	written, err := io.Copy(f, file)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		s.logger.Error("models: write failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write model file"})
		return
	}

	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		s.logger.Error("models: rename failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save model file"})
		return
	}

	s.logger.Info("model uploaded", "model", filename, "bytes", written)
	c.JSON(http.StatusCreated, gin.H{"filename": filename, "size_bytes": written, "status": "installed"})
}

// handleDeleteModel removes a locally installed model file.
// DELETE /api/v1/models/:filename
func (s *Server) handleDeleteModel(c *gin.Context) {
	filename := filepath.Base(c.Param("filename"))
	if filepath.Ext(filename) != ".onnx" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model filename"})
		return
	}

	cfg := s.snapConfig()
	path := filepath.Join(cfg.Models.Dir, filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "model file not found"})
		return
	}

	if err := os.Remove(path); err != nil {
		s.logger.Error("models: delete failed", "model", filename, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete model file"})
		return
	}

	s.logger.Info("model deleted", "model", filename)
	c.Status(http.StatusNoContent)
}
