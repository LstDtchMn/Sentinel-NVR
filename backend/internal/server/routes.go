// routes.go — central route registration and shared helpers used across route files.

package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/auth"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
)

// setupUsernameRE validates usernames during first-run setup.
var setupUsernameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _-]{0,62}[a-zA-Z0-9_-]?$`)

// testStreamMu + lastTestStreamTime rate-limit the test-stream endpoint to prevent
// go2rtc resource exhaustion from rapid calls. Minimum 5s between requests.
var (
	testStreamMu       sync.Mutex
	lastTestStreamTime time.Time
)

// parseSlogLevel converts a sentinel log_level string to a slog.Level constant.
func parseSlogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

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
		// OIDC SSO routes — registered only when the provider is wired up (Phase 7, CG6).
		if s.oidcProvider != nil {
			authGroup.GET("/oidc/login", s.handleOIDCLogin)
			authGroup.GET("/oidc/callback", s.handleOIDCCallback)
		}
	}

	// First-run setup (Phase 7, CG6) — public so the UI can check/redirect before login
	v1.GET("/setup", s.handleSetupCheck)
	v1.POST("/setup", s.handleSetupCreate)

	// --- Protected endpoints ---
	// When authService is non-nil (auth.enabled=true), all routes below require
	// a valid sentinel_access JWT cookie. When nil, they are openly accessible.
	protected := v1.Group("")
	if s.authService != nil {
		protected.Use(s.authService.Middleware())
	}
	{
		protected.GET("/admin/health", s.handleAdminHealth) // admin-only detailed health
		protected.GET("/config", s.handleGetConfig)
		protected.PUT("/config", s.handleUpdateConfig) // admin-only; Phase 9 settings persistence

		// Current user info (Phase 7) — returns the authenticated user's public fields.
		protected.GET("/auth/me", s.handleAuthMe)

		// Camera management (Phase 1)
		protected.GET("/cameras", s.handleListCameras)
		protected.GET("/cameras/:name", s.handleGetCamera)
		protected.GET("/cameras/:name/status", s.handleCameraStatus)
		protected.GET("/cameras/:name/snapshot", s.handleCameraSnapshot) // Phase 9: zone editor background
		protected.POST("/cameras", s.handleCreateCamera)
		protected.PUT("/cameras/:name", s.handleUpdateCamera)
		protected.DELETE("/cameras/:name", s.handleDeleteCamera)
		protected.POST("/cameras/:name/restart", s.handleRestartCamera)
		protected.PATCH("/cameras/:name/rename", s.handleRenameCamera)

		// Test camera stream connectivity
		protected.POST("/cameras/test-stream", s.handleTestStream)

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

		// Storage stats (Phase 10, R13) — aggregate used_bytes per tier
		protected.GET("/storage/stats", s.handleStorageStats)

		// Clip export
		protected.POST("/recordings/export", s.handleExportClip)
		protected.GET("/recordings/export/:id/download", s.handleExportDownload)

		// Recording management (Phase 2)
		protected.GET("/recordings", s.handleListRecordings)
		protected.GET("/recordings/:id", s.handleGetRecording)
		protected.GET("/recordings/:id/play", s.handlePlayRecording)
		protected.GET("/recordings/:id/download", s.handleDownloadRecording)
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
			notif.POST("/test", s.handleTestNotification)
		}

		// Retention rules management (R14) — per-camera × per-event-type matrix
		retention := protected.Group("/retention/rules")
		{
			retention.GET("", s.handleListRetentionRules)
			retention.POST("", s.handleCreateRetentionRule)
			retention.PUT("/:id", s.handleUpdateRetentionRule)
			retention.DELETE("/:id", s.handleDeleteRetentionRule)
		}

		// Face recognition management (Phase 13, R11)
		faces := protected.Group("/faces")
		{
			faces.GET("", s.handleListFaces)
			faces.POST("", s.handleCreateFace)         // raw embedding API (admin)
			faces.POST("/enroll", s.handleEnrollFace)  // JPEG upload → sentinel-infer (admin, R11)
			faces.GET("/:id", s.handleGetFace)
			faces.PUT("/:id", s.handleUpdateFace)
			faces.DELETE("/:id", s.handleDeleteFace)
		}

		// AI Model management (R10) — curated download list + manual upload
		mdls := protected.Group("/models")
		{
			mdls.GET("", s.handleListModels)
			mdls.POST("/:filename/download", s.handleDownloadModel)
			mdls.POST("/upload", s.handleUploadModel)
			mdls.DELETE("/:filename", s.handleDeleteModel)
		}

		// Migration / import (Phase 14, R15)
		protected.POST("/import/preview", s.handleImportPreview) // dry-run: parse + validate
		protected.POST("/import", s.handleImportExecute)         // actually create cameras

		// ONVIF camera discovery (admin-only)
		protected.POST("/cameras/discover", s.handleDiscoverCameras)
		protected.POST("/cameras/discover/probe", s.handleProbeCamera)

		// Remote access (Phase 12, CG11, R8)
		protected.GET("/relay/ice-servers", s.handleRelayICEServers)
		protected.POST("/pairing/qr", s.handlePairingQR)

		// Database backup management (admin-only)
		protected.GET("/admin/backups", s.handleListBackups)
		protected.POST("/admin/backup", s.handleTriggerBackup)

		// User management (admin-only)
		adminUsers := protected.Group("/admin/users")
		{
			adminUsers.GET("", s.handleListUsers)
			adminUsers.POST("", s.handleCreateUser)
			adminUsers.DELETE("/:id", s.handleDeleteUser)
			adminUsers.PUT("/:id/role", s.handleUpdateUserRole)
			adminUsers.PUT("/:id/password", s.handleUpdateUserPassword)
		}
	}

	// Public pairing redeem — mobile app has no auth session when it calls this (Phase 12, CG11).
	v1.POST("/pairing/redeem", s.handlePairingRedeem)
}

// ─── Shared helpers used across route files ──────────────────────────────────

// requireAdmin returns true when the request is from an admin user (or auth is disabled).
// Writes 403 and aborts when auth is enabled and the caller is not an admin.
func (s *Server) requireAdmin(c *gin.Context) bool {
	if s.authService == nil {
		return true // auth disabled — all users are effectively admin
	}
	role, _ := c.Get(auth.CtxKeyRole)
	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin role required"})
		c.Abort()
		return false
	}
	return true
}

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

// sanitizeEventThumbnail replaces the raw filesystem thumbnail path with a
// relative API URL so the server's directory layout is never exposed to clients.
func sanitizeEventThumbnail(ev *detection.EventRecord) {
	if ev.Thumbnail != "" {
		ev.Thumbnail = fmt.Sprintf("/api/v1/events/%d/thumbnail", ev.ID)
	}
}
