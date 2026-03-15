// routes_retention.go — retention rule CRUD handlers (R14).

package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/storage"
)

// handleListRetentionRules returns all configured retention rules.
// GET /api/v1/retention/rules
func (s *Server) handleListRetentionRules(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	rules, err := s.retentionRepo.List(ctx)
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
	if !s.requireAdmin(c) {
		return
	}
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
	if !s.requireAdmin(c) {
		return
	}
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
	if !s.requireAdmin(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if err := s.retentionRepo.Delete(ctx, id); err != nil {
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
