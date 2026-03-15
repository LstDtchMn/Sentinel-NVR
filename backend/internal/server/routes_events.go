// routes_events.go — detection event CRUD, thumbnail, heatmap, and SSE stream handlers.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/pathutil"
)

// handleListEvents returns events with optional filtering and pagination.
// Query params: camera_id (int), type (string), date (YYYY-MM-DD), min_confidence (float 0–1), limit (1–500, default 50), offset (int).
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

	if mcStr := c.Query("min_confidence"); mcStr != "" {
		mc, err := strconv.ParseFloat(mcStr, 64)
		if err != nil || mc < 0 || mc > 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "min_confidence must be a float between 0 and 1"})
			return
		}
		f.MinConfidence = &mc
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
	for i := range events {
		sanitizeEventThumbnail(&events[i])
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
	sanitizeEventThumbnail(ev)
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
	if !s.requireAdmin(c) {
		return
	}

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
