// This file defines all REST API route handlers (CG7).

package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
)

var startTime = time.Now()

// registerRoutes mounts all API v1 endpoints on the Gin router.
func (s *Server) registerRoutes() {
	v1 := s.router.Group("/api/v1")
	{
		v1.GET("/health", s.handleHealth)
		v1.GET("/config", s.handleGetConfig)

		// Camera management (Phase 1)
		v1.GET("/cameras", s.handleListCameras)
		v1.GET("/cameras/:name", s.handleGetCamera)
		v1.GET("/cameras/:name/status", s.handleCameraStatus)
		v1.POST("/cameras", s.handleCreateCamera)
		v1.PUT("/cameras/:name", s.handleUpdateCamera)
		v1.DELETE("/cameras/:name", s.handleDeleteCamera)

		// Recording management (Phase 2)
		v1.GET("/recordings", s.handleListRecordings)
		v1.GET("/recordings/:id", s.handleGetRecording)
		v1.GET("/recordings/:id/play", s.handlePlayRecording)
		v1.DELETE("/recordings/:id", s.handleDeleteRecording)
	}
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
		"uptime":             time.Since(startTime).Round(time.Second).String(),
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
	if cameras == nil {
		cameras = []camera.CameraWithStatus{}
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
func (s *Server) handleCameraStatus(c *gin.Context) {
	name := c.Param("name")
	ps, ok := s.camManager.Status(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
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
		c.JSON(http.StatusConflict, gin.H{"error": "camera name '" + input.Name + "' already exists"})
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
// The camera name comes from the URL path — the name field in the body is ignored (M3).
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

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "camera '" + name + "' deleted"})
}

// handleListRecordings returns recording segments with optional filtering.
// Query params: camera (name), start (RFC3339), end (RFC3339), limit (int, max 1000), offset (int).
func (s *Server) handleListRecordings(c *gin.Context) {
	cameraName := c.Query("camera")
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
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
	if recordings == nil {
		recordings = []recording.Record{}
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

	// Validate that the path is under hot or cold storage to prevent path traversal.
	cleanPath := filepath.Clean(rec.Path)
	hotBase := filepath.Clean(s.cfg.Storage.HotPath)
	coldBase := filepath.Clean(s.cfg.Storage.ColdPath)
	sep := string(filepath.Separator)
	if !strings.HasPrefix(cleanPath, hotBase+sep) && !strings.HasPrefix(cleanPath, coldBase+sep) {
		s.logger.Warn("recording path outside storage directories", "id", id, "path", rec.Path)
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	// Verify file exists before serving
	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		s.logger.Warn("recording file missing from disk", "id", id, "path", rec.Path)
		c.JSON(http.StatusNotFound, gin.H{"error": "recording file not found on disk"})
		return
	}

	// Clear the write deadline so large transfers aren't truncated at the server timeout.
	// http.ServeFile sets Content-Type from the file extension — no manual header needed.
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.logger.Warn("failed to clear write deadline for file transfer", "error", err)
	}
	http.ServeFile(c.Writer, c.Request, cleanPath)
}

// handleDeleteRecording deletes a recording segment from DB and disk.
func (s *Server) handleDeleteRecording(c *gin.Context) {
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
		s.logger.Error("failed to get recording for deletion", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Delete file from disk (ignore error if already gone)
	if err := os.Remove(rec.Path); err != nil && !os.IsNotExist(err) {
		s.logger.Warn("failed to delete recording file", "id", id, "path", rec.Path, "error", err)
	}

	// Delete DB record
	if err := s.recRepo.Delete(ctx, id); err != nil {
		s.logger.Error("failed to delete recording from DB", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "recording deleted"})
}
