// This file defines all REST API route handlers (CG7).

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/auth"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/notification"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
)

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
	}

	// --- Protected endpoints ---
	// When authService is non-nil (auth.enabled=true), all routes below require
	// a valid sentinel_access JWT cookie. When nil, they are openly accessible.
	protected := v1.Group("")
	if s.authService != nil {
		protected.Use(s.authService.Middleware())
	}
	{
		protected.GET("/config", s.handleGetConfig)

		// Current user info (Phase 7) — returns the authenticated user's public fields.
		protected.GET("/auth/me", s.handleAuthMe)

		// Camera management (Phase 1)
		protected.GET("/cameras", s.handleListCameras)
		protected.GET("/cameras/:name", s.handleGetCamera)
		protected.GET("/cameras/:name/status", s.handleCameraStatus)
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
	}
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
	auth.SetTokenCookies(c, pair, s.cfg.Auth.SecureCookie)
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
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrTokenExpired) {
			auth.ClearTokenCookies(c, s.cfg.Auth.SecureCookie)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "session expired, please log in again"})
			return
		}
		s.logger.Error("refresh error", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "refresh failed"})
		return
	}

	auth.SetTokenCookies(c, pair, s.cfg.Auth.SecureCookie)
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
	auth.ClearTokenCookies(c, s.cfg.Auth.SecureCookie)
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

	recCount, err := s.recRepo.Count(c.Request.Context())
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

	c.JSON(http.StatusOK, gin.H{
		"server": safeServer{
			Host:     s.cfg.Server.Host,
			Port:     s.cfg.Server.Port,
			LogLevel: s.cfg.Server.LogLevel,
		},
		"storage": safeStorage{
			HotPath:           s.cfg.Storage.HotPath,
			ColdPath:          s.cfg.Storage.ColdPath,
			HotRetentionDays:  s.cfg.Storage.HotRetentionDays,
			ColdRetentionDays: s.cfg.Storage.ColdRetentionDays,
			SegmentDuration:   s.cfg.Storage.SegmentDuration,
			SegmentFormat:     s.cfg.Storage.SegmentFormat,
		},
		"detection": gin.H{"enabled": s.cfg.Detection.Enabled, "backend": s.cfg.Detection.Backend},
		"cameras":   safeCams,
	})
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
	Name       string `json:"name"`
	Enabled    *bool  `json:"enabled"`
	MainStream string `json:"main_stream"`
	SubStream  string `json:"sub_stream"`
	Record     *bool  `json:"record"`
	Detect     *bool  `json:"detect"`
	ONVIFHost  string `json:"onvif_host"`
	ONVIFPort  int    `json:"onvif_port"`
	ONVIFUser  string `json:"onvif_user"`
	ONVIFPass  string `json:"onvif_pass"`
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

// handleCreateCamera creates a new camera, syncs to go2rtc, and starts a pipeline.
func (s *Server) handleCreateCamera(c *gin.Context) {
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
	c.JSON(http.StatusOK, recordings)
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
	if !isUnderPath(resolvedPath, s.resolvedHotPath) && !isUnderPath(resolvedPath, s.resolvedColdPath) {
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
		if !isUnderPath(resolvedPath, s.resolvedHotPath) && !isUnderPath(resolvedPath, s.resolvedColdPath) {
			s.logger.Warn("resolved recording path escapes storage boundary", "id", id, "path", rec.Path, "resolved", resolvedPath)
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
	} else if os.IsNotExist(err) {
		// File already gone — lexical check on the raw path before allowing DB cleanup.
		if !isUnderPath(cleanPath, s.resolvedHotPath) && !isUnderPath(cleanPath, s.resolvedColdPath) {
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
	if !isUnderPath(resolvedPath, s.resolvedSnapshotPath) {
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
		resolved, resolveErr := filepath.EvalSymlinks(ev.Thumbnail)
		if resolveErr != nil && !os.IsNotExist(resolveErr) {
			s.logger.Warn("could not resolve thumbnail path; skipping file removal",
				"id", id, "path", ev.Thumbnail, "error", resolveErr)
		} else if resolveErr == nil && !isUnderPath(resolved, s.resolvedSnapshotPath) {
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

	ch := s.eventBus.Subscribe("*")
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
			// Only forward "events.persisted" events — these carry the DB-persisted payload
			// with correct schema and no absolute paths. Dropping other event types here is
			// safe: recording/camera events are only meaningful in the Events page when they
			// exist in the DB, and they emit their own "events.persisted" notification.
			if event.Type != "events.persisted" {
				continue
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

// isUnderPath checks if cleanPath is strictly contained within basePath.
// Returns false if cleanPath equals basePath (the storage root itself is not a valid target).
// Uses filepath.Rel for platform-safe path containment checking.
func isUnderPath(cleanPath, basePath string) bool {
	base := filepath.Clean(basePath)
	rel, err := filepath.Rel(base, cleanPath)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}

// validateWebhookURL checks that a webhook token is a well-formed HTTP/HTTPS URL.
// Blocks file://, ftp://, and other schemes to prevent SSRF via the webhook sender.
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
