// routes_users.go — admin-only user management endpoints.

package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/auth"
)

// handleListUsers returns all user accounts (admin only).
// GET /api/v1/admin/users
func (s *Server) handleListUsers(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	if s.authService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth not enabled"})
		return
	}

	users, err := s.authService.ListUsers(c.Request.Context())
	if err != nil {
		s.logger.Error("failed to list users", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// handleCreateUser creates a new user account (admin only).
// POST /api/v1/admin/users
// Body: {"username": "...", "password": "...", "role": "admin"|"viewer"}
func (s *Server) handleCreateUser(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	if s.authService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth not enabled"})
		return
	}

	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Validate username
	if !setupUsernameRE.MatchString(input.Username) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username must be 1-64 alphanumeric characters (spaces, hyphens, underscores allowed, no leading/trailing spaces)"})
		return
	}
	// Validate password
	if len(input.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		return
	}
	// Validate role
	if input.Role != "admin" && input.Role != "viewer" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be \"admin\" or \"viewer\""})
		return
	}

	user, err := s.authService.CreateUser(c.Request.Context(), input.Username, input.Password, input.Role)
	if err != nil {
		// Check for duplicate username (SQLite UNIQUE constraint error contains "UNIQUE")
		if isDuplicateErr(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "a user with that username already exists"})
			return
		}
		s.logger.Error("failed to create user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	s.logger.Info("user created", "username", user.Username, "role", user.Role, "by", c.GetString("username"))
	c.JSON(http.StatusCreated, user)
}

// handleDeleteUser deletes a user account (admin only).
// Cannot delete the currently logged-in user.
// DELETE /api/v1/admin/users/:id
func (s *Server) handleDeleteUser(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	if s.authService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth not enabled"})
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}

	// Prevent self-deletion
	callerID := s.notifUserID(c)
	if callerID == -1 {
		return // notifUserID already aborted with 500
	}
	if callerID == id {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete your own account"})
		return
	}

	if err := s.authService.DeleteUser(c.Request.Context(), id); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		s.logger.Error("failed to delete user", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	s.logger.Info("user deleted", "id", id, "by", c.GetString("username"))
	c.Status(http.StatusNoContent)
}

// handleUpdateUserRole changes a user's role (admin only).
// PUT /api/v1/admin/users/:id/role
// Body: {"role": "admin"|"viewer"}
func (s *Server) handleUpdateUserRole(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	if s.authService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth not enabled"})
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}

	var input struct {
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	if input.Role != "admin" && input.Role != "viewer" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be \"admin\" or \"viewer\""})
		return
	}

	user, err := s.authService.UpdateUserRole(c.Request.Context(), id, input.Role)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		s.logger.Error("failed to update user role", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	s.logger.Info("user role updated", "id", id, "role", input.Role, "by", c.GetString("username"))
	c.JSON(http.StatusOK, user)
}

// handleUpdateUserPassword changes a user's password (admin only).
// PUT /api/v1/admin/users/:id/password
// Body: {"password": "..."}
func (s *Server) handleUpdateUserPassword(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	if s.authService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth not enabled"})
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}

	var input struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	if len(input.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		return
	}

	if err := s.authService.UpdateUserPassword(c.Request.Context(), id, input.Password); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		s.logger.Error("failed to update user password", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	s.logger.Info("user password updated", "id", id, "by", c.GetString("username"))
	c.JSON(http.StatusOK, gin.H{"status": "password updated"})
}

// isDuplicateErr checks if the error is a SQLite UNIQUE constraint violation.
func isDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
