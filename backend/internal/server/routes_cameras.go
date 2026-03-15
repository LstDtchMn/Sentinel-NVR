// routes_cameras.go — camera CRUD, snapshot, test-stream, ONVIF discovery, and zone handlers.

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/onvif"
)

// cameraInput is the request body for creating/updating a camera.
type cameraInput struct {
	Name                 string          `json:"name"`
	Enabled              *bool           `json:"enabled"`
	MainStream           string          `json:"main_stream"`
	SubStream            string          `json:"sub_stream"`
	Record               *bool           `json:"record"`
	Detect               *bool           `json:"detect"`
	ONVIFHost            string          `json:"onvif_host"`
	ONVIFPort            int             `json:"onvif_port"`
	ONVIFUser            string          `json:"onvif_user"`
	ONVIFPass            string          `json:"onvif_pass"`
	Zones                json.RawMessage `json:"zones"` // Phase 9: nil = preserve existing zones (manager handles)
	NotificationCooldown *int            `json:"notification_cooldown_seconds"`
	DetectionInterval    *int            `json:"detection_interval"`
}

func (ci *cameraInput) toRecord() *camera.CameraRecord {
	rec := &camera.CameraRecord{
		Name:                 ci.Name,
		Enabled:              true, // default
		MainStream:           ci.MainStream,
		SubStream:            ci.SubStream,
		Record:               true, // default
		Detect:               false,
		ONVIFHost:            ci.ONVIFHost,
		ONVIFPort:            ci.ONVIFPort,
		ONVIFUser:            ci.ONVIFUser,
		ONVIFPass:            ci.ONVIFPass,
		Zones:                normalizeZonesRaw(ci.Zones), // nil when not provided or "null" — manager preserves existing zones
		NotificationCooldown: 60, // default 60 seconds
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
	if ci.NotificationCooldown != nil {
		rec.NotificationCooldown = *ci.NotificationCooldown
	}
	if ci.DetectionInterval != nil {
		rec.DetectionInterval = *ci.DetectionInterval
	}
	return rec
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

	s.logger.Info("camera created", "name", rec.Name, "user", c.GetString("username"))
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

	s.logger.Info("camera updated", "name", name, "user", c.GetString("username"))
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

	s.logger.Info("camera deleted", "name", name, "user", c.GetString("username"))
	c.Status(http.StatusNoContent)
}

// handleRenameCamera renames an existing camera.
// PATCH /api/v1/cameras/:name/rename — admin-only.
// Body: { "new_name": "New Camera Name" }
func (s *Server) handleRenameCamera(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	oldName := c.Param("name")

	var input struct {
		NewName string `json:"new_name"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	if input.NewName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new_name is required"})
		return
	}

	if err := camera.ValidateCameraName(input.NewName); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	result, err := s.camManager.RenameCamera(ctx, oldName, input.NewName)
	if errors.Is(err, camera.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}
	if errors.Is(err, camera.ErrDuplicate) {
		c.JSON(http.StatusConflict, gin.H{"error": "a camera with that name already exists"})
		return
	}
	if err != nil {
		s.logger.Error("failed to rename camera", "old_name", oldName, "new_name", input.NewName, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	s.logger.Info("camera renamed", "old_name", oldName, "new_name", input.NewName, "user", c.GetString("username"))
	c.JSON(http.StatusOK, result)
}

// handleRestartCamera stops and restarts a camera's pipeline without modifying the DB.
// POST /api/v1/cameras/:name/restart (admin only)
func (s *Server) handleRestartCamera(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	name := c.Param("name")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	err := s.camManager.RestartCamera(ctx, name)
	if errors.Is(err, camera.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to restart camera pipeline", "name", name, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	s.logger.Info("camera pipeline restarted via API", "name", name, "user", c.GetString("username"))
	c.JSON(http.StatusOK, gin.H{"status": "restarted"})
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
	// TODO(review): L15 — add main-stream fallback when sub-stream snapshot fails
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

// handleTestStream tests whether a stream URL is reachable via go2rtc.
// POST /api/v1/cameras/test-stream
// Body: {"url": "<stream_url>"}
func (s *Server) handleTestStream(c *gin.Context) {
	// Rate limit: at most one test-stream call every 5 seconds to prevent go2rtc resource exhaustion.
	testStreamMu.Lock()
	if time.Since(lastTestStreamTime) < 5*time.Second {
		testStreamMu.Unlock()
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Please wait before testing another stream"})
		return
	}
	lastTestStreamTime = time.Now()
	testStreamMu.Unlock()

	var req struct {
		URL string `json:"url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}

	// Validate URL scheme.
	parsed, err := url.Parse(req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid URL"})
		return
	}
	switch parsed.Scheme {
	case "rtsp", "rtsps", "rtmp", "http", "https":
		// valid
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "url scheme must be rtsp, rtsps, rtmp, http, or https"})
		return
	}

	// Generate a unique temporary stream name.
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		s.logger.Error("failed to generate random bytes for test stream", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	testName := "__test_" + hex.EncodeToString(randBytes)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Add the test stream to go2rtc.
	if err := s.g2r.AddStream(ctx, testName, req.URL); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": fmt.Sprintf("stream unreachable: %v", err)})
		return
	}
	// Always clean up the test stream, even on failure.
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanCancel()
		if err := s.g2r.RemoveStream(cleanCtx, testName); err != nil {
			s.logger.Warn("failed to remove test stream from go2rtc", "name", testName, "error", err)
		}
	}()

	// Try to grab a JPEG frame to verify the stream is actually producing video.
	if _, err := s.g2r.FrameJPEG(ctx, testName); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": fmt.Sprintf("stream unreachable: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Stream is reachable"})
}

// handleDiscoverCameras runs a WS-Discovery multicast probe for ONVIF cameras on the LAN.
// POST /api/v1/cameras/discover — admin-only, no request body.
// Returns {"cameras": [...], "warning": "..."} (warning set when multicast fails, e.g. Docker bridge).
func (s *Server) handleDiscoverCameras(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
	defer cancel()

	devices, err := onvif.Discover(ctx, 5*time.Second)
	if err != nil {
		s.logger.Warn("onvif discovery failed", "error", err)
		// Return empty list with warning — multicast often fails in Docker
		c.JSON(http.StatusOK, gin.H{
			"cameras": []struct{}{},
			"warning": "ONVIF multicast discovery failed (this is expected in Docker bridge networking). Use the probe endpoint to query cameras by IP instead.",
		})
		return
	}

	if devices == nil {
		devices = []onvif.DiscoveredDevice{}
	}

	c.JSON(http.StatusOK, gin.H{"cameras": devices})
}

// probeCameraRequest is the JSON body for POST /cameras/discover/probe.
type probeCameraRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleProbeCamera queries a specific ONVIF camera by IP/port.
// POST /api/v1/cameras/discover/probe — admin-only.
// Without credentials: returns device info (unauthenticated GetDeviceInformation).
// With credentials: also returns stream profiles with RTSP URIs.
func (s *Server) handleProbeCamera(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}

	var req probeCameraRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host is required"})
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		req.Port = 80
	}

	// Validate host is an IP or hostname — reject URLs or paths
	if net.ParseIP(req.Host) == nil {
		// Not a bare IP — check it looks like a hostname (no scheme, no path)
		if strings.Contains(req.Host, "/") || strings.Contains(req.Host, ":") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "host must be an IP address or hostname (not a URL)"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()

	// Always try to get device info (unauthenticated)
	deviceInfo, err := onvif.ProbeDevice(ctx, req.Host, req.Port)
	if err != nil {
		s.logger.Warn("onvif probe failed", "host", req.Host, "port", req.Port, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("could not reach ONVIF device at %s:%d — verify the IP and port are correct", req.Host, req.Port)})
		return
	}

	result := gin.H{"device": deviceInfo}

	// If credentials provided, also fetch stream profiles
	if req.Username != "" && req.Password != "" {
		xaddr := fmt.Sprintf("http://%s:%d/onvif/device_service", req.Host, req.Port)
		streams, streamErr := onvif.GetStreamProfiles(ctx, xaddr, req.Username, req.Password)
		if streamErr != nil {
			s.logger.Warn("onvif stream profile fetch failed", "host", req.Host, "error", streamErr)
			result["streams"] = []struct{}{}
			result["stream_error"] = "failed to retrieve stream profiles — verify credentials are correct"
		} else {
			if streams == nil {
				streams = []onvif.StreamProfile{}
			}
			result["streams"] = streams
		}
	}

	c.JSON(http.StatusOK, result)
}

// normalizeZonesRaw converts a JSON "null" literal (or absent field) to Go nil so the
// manager's zone-preservation logic (cam.Zones == nil → keep existing) fires for absent
// and explicit-null payloads.
// "[]" is intentionally NOT treated as nil — an explicit empty array clears all zones.
// This lets clients distinguish "don't touch zones" (omit field / send null) from
// "remove all zones" (send []).
func normalizeZonesRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
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
