// routes_export.go — clip export API handlers.

package server

import (
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
)

// exportIDRE validates export IDs to prevent path traversal (8 hex chars from uuid prefix).
var exportIDRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

type exportInput struct {
	CameraName string `json:"camera_name" binding:"required"`
	Start      string `json:"start" binding:"required"` // RFC3339
	End        string `json:"end" binding:"required"`   // RFC3339
}

// handleExportClip extracts a sub-clip from recorded segments.
// POST /api/v1/recordings/export
func (s *Server) handleExportClip(c *gin.Context) {
	var input exportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	start, err := time.Parse(time.RFC3339, input.Start)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start time (use RFC3339)"})
		return
	}
	end, err := time.Parse(time.RFC3339, input.End)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end time (use RFC3339)"})
		return
	}

	if s.exportService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "export service not configured"})
		return
	}

	result, err := s.exportService.ExportClip(c.Request.Context(), input.CameraName, start, end)
	if err != nil {
		// Distinguish client errors from server errors
		status := http.StatusInternalServerError
		msg := err.Error()
		if msg == "end time must be after start time" ||
			msg == "maximum export duration is 5 minutes" ||
			msg == "no recordings found for the requested time range" {
			status = http.StatusBadRequest
		}
		if len(msg) > 19 && msg[:19] == "too many concurrent" {
			status = http.StatusTooManyRequests
		}
		c.JSON(status, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, result)
}

// handleExportDownload serves an exported clip file.
// GET /api/v1/recordings/export/:id/download
func (s *Server) handleExportDownload(c *gin.Context) {
	exportID := c.Param("id")
	if !exportIDRE.MatchString(exportID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid export ID"})
		return
	}

	if s.exportService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "export service not configured"})
		return
	}

	path := s.exportService.ServePath(exportID)
	if path == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "export not found or expired"})
		return
	}

	c.Header("Content-Disposition", "attachment; filename="+exportID+".mp4")
	c.File(path)
}
