// routes_notifications.go — notification token/pref/log CRUD, test, and webhook validation.

package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/notification"
)

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
	// TODO(review): L16 — deduplicate with storage.KnownEventTypes
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

// handleTestNotification sends a test notification to a registered device token.
// POST /api/v1/notifications/test
// Body: {"token_id": <int>}
func (s *Server) handleTestNotification(c *gin.Context) {
	if !s.notifAvailable(c) {
		return
	}
	var req struct {
		TokenID int `json:"token_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token_id is required"})
		return
	}

	userID := s.notifUserID(c)
	if userID < 0 {
		return // notifUserID already aborted with 500
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Look up the token — scoped to the current user to prevent cross-user abuse.
	tok, err := s.notifRepo.GetTokenByID(ctx, req.TokenID, userID)
	if errors.Is(err, notification.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to look up notification token", "token_id", req.TokenID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	sender, ok := s.notifSenders[tok.Provider]
	if !ok {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": fmt.Sprintf("no sender configured for provider %q", tok.Provider)})
		return
	}

	testNotif := notification.Notification{
		Title:     "Sentinel NVR Test",
		Body:      "This is a test notification. If you see this, notifications are working!",
		EventType: "test",
		Timestamp: time.Now(),
	}

	if err := sender.Send(ctx, tok.Token, testNotif); err != nil {
		s.logger.Warn("test notification delivery failed", "provider", tok.Provider, "token_id", tok.ID, "error", err)
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": fmt.Sprintf("delivery failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "sent"})
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
		// Reject on DNS lookup failure: allowing an unresolvable domain at registration
		// time opens a gap where the domain is later configured to point at an internal
		// address (e.g. the Docker bridge IP of go2rtc) before delivery is attempted.
		addrs, lookupErr := net.LookupHost(hostname)
		if lookupErr != nil {
			return fmt.Errorf("webhook URL: could not resolve hostname %q: %w", hostname, lookupErr)
		}
		for _, addr := range addrs {
			resolved := net.ParseIP(addr)
			if resolved != nil && (resolved.IsLoopback() || resolved.IsPrivate() || resolved.IsLinkLocalUnicast()) {
				return fmt.Errorf("webhook URL resolves to a private or loopback address")
			}
		}
	}
	return nil
}
