// routes_backup.go — database backup management handlers.

package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleListBackups returns all available database backups, newest-first.
// GET /api/v1/admin/backups
func (s *Server) handleListBackups(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	if s.backupMgr == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backups not configured"})
		return
	}

	backups, err := s.backupMgr.List()
	if err != nil {
		s.logger.Error("backup: list failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list backups"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"backups": backups})
}

// handleTriggerBackup runs an immediate database backup and returns the result.
// POST /api/v1/admin/backup
func (s *Server) handleTriggerBackup(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	if s.backupMgr == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backups not configured"})
		return
	}

	info, err := s.backupMgr.RunNow()
	if err != nil {
		s.logger.Error("backup: trigger failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "backup failed"})
		return
	}
	c.JSON(http.StatusOK, info)
}
