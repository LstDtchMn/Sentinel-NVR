// routes_recordings.go — recording CRUD, playback, timeline, days, and storage stats handlers.

package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/pathutil"
)

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
	if !s.requireAdmin(c) {
		return
	}

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
