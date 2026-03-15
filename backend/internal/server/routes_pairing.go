// routes_pairing.go — remote access pairing and relay handlers (Phase 12, CG11, R8).

package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/auth"
)

// handleRelayICEServers returns ICE server config for WebRTC peer connections (Phase 12, CG11).
// GET /api/v1/relay/ice-servers
// Always returns the STUN server; includes TURN credentials when relay.enabled=true.
//
// Design decision: TURN credentials are served to ALL authenticated users (not admin-only).
// This is intentional — every user (including non-admin viewers) needs TURN relay access
// for remote WebRTC streaming when symmetric NAT blocks direct P2P. The credentials are
// long-lived shared secrets from the config, matching go2rtc's own ice_servers config.
// Future improvement: mint short-lived TURN credentials per RFC 5766 §9 (time-limited HMAC)
// instead of exposing the shared secret. For now this is acceptable because:
// 1. The endpoint requires authentication (JWT cookie).
// 2. TURN abuse is bounded by coturn's bandwidth/allocation limits.
// 3. The credentials only grant relay access, not admin access to the NVR.
// The mobile app passes the returned list to flutter_webrtc's RTCConfiguration.
func (s *Server) handleRelayICEServers(c *gin.Context) {
	type iceServer struct {
		URLs       []string `json:"urls"`
		Username   string   `json:"username,omitempty"`
		Credential string   `json:"credential,omitempty"`
	}
	cfg := s.snapConfig()
	servers := []iceServer{
		{URLs: []string{cfg.Relay.STUNServer}},
	}
	if cfg.Relay.Enabled && cfg.Relay.TURNServer != "" {
		servers = append(servers, iceServer{
			URLs:       []string{cfg.Relay.TURNServer},
			Username:   cfg.Relay.TURNUser,
			Credential: cfg.Relay.TURNPass,
		})
	}
	c.JSON(http.StatusOK, gin.H{"ice_servers": servers})
}

// handlePairingQR generates a short-lived pairing code for QR-based mobile pairing (Phase 12, CG11).
// POST /api/v1/pairing/qr  (admin only)
// Returns {"code":"<uuid>","expires_at":"<RFC3339>"} — the web UI encodes this into a QR image.
// The mobile app scans the QR and calls POST /pairing/redeem to exchange the code for a session.
func (s *Server) handlePairingQR(c *gin.Context) {
	// Pairing requires auth to be enabled — without it there's no user/session model
	// and the FK to pairing_codes.user_id would reference a nonexistent user (Issue #6).
	if s.authService == nil {
		c.JSON(http.StatusOK, gin.H{"message": "auth disabled — pairing not available"})
		return
	}
	if !s.requireAdmin(c) {
		return
	}
	userID := s.notifUserID(c)
	if userID < 0 {
		return // notifUserID already aborted with 500
	}

	// Generate UUID v4 using crypto/rand (no new dependencies).
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		s.logger.Error("pairing: rand.Read failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate pairing code"})
		return
	}
	b[6] = (b[6] & 0x0f) | 0x40 // UUID version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant bits
	code := fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)

	expiresAt := time.Now().UTC().Add(15 * time.Minute)

	// Purge expired codes before inserting — prevents unbounded table growth on repeated
	// calls. Uses a short independent timeout so a slow DELETE cannot starve the INSERT.
	delCtx, delCancel := context.WithTimeout(c.Request.Context(), time.Second)
	_, _ = s.db.ExecContext(delCtx,
		`DELETE FROM pairing_codes WHERE expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339),
	)
	delCancel()

	insCtx, insCancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer insCancel()

	_, err := s.db.ExecContext(insCtx,
		`INSERT INTO pairing_codes (code, user_id, expires_at) VALUES (?, ?, ?)`,
		code, userID, expiresAt.Format(time.RFC3339),
	)
	if err != nil {
		s.logger.Error("pairing: insert failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate pairing code"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code":       code,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

// handlePairingRedeem exchanges a valid pairing code for a session (Phase 12, CG11).
// POST /api/v1/pairing/redeem  body: {"code":"<uuid>"}
// Public endpoint — the mobile app has no auth session when it calls this.
// On success, sets httpOnly auth cookies (same as /auth/login) and marks the code as used.
// Returns 401 for invalid/expired/already-used codes.
// Rate-limited with the same limiter as /auth/login to prevent brute-force enumeration.
func (s *Server) handlePairingRedeem(c *gin.Context) {
	if s.authService == nil {
		c.JSON(http.StatusOK, gin.H{"message": "auth disabled"})
		return
	}

	// Rate limit — use a separate key namespace from /auth/login so that repeated
	// failed pairing attempts don't lock out login from the same IP (CG6).
	ip := c.ClientIP()
	if !s.loginLimiter.allow(ip + "_pairing") {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many attempts, try again later"})
		return
	}

	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code is required"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Atomic claim + user lookup via RETURNING: marks used_at and retrieves user_id in a
	// single statement, eliminating the TOCTOU race and the "code consumed but lookup failed"
	// failure mode of the previous two-query approach.
	now := time.Now().UTC().Format(time.RFC3339)
	var userID int
	err := s.db.QueryRowContext(ctx,
		`UPDATE pairing_codes SET used_at = ?
		 WHERE code = ? AND used_at IS NULL AND expires_at > ?
		 RETURNING user_id`,
		now, req.Code, now,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired pairing code"})
		return
	}
	if err != nil {
		s.logger.Error("pairing: claim failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Issue session tokens for the user who generated the code.
	pair, pairErr := s.authService.IssueTokenPairForUserID(ctx, userID)
	if pairErr != nil {
		s.logger.Error("pairing: token issue failed", "error", pairErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	s.loginLimiter.reset(ip + "_pairing") // match the namespace used in allow() above
	auth.SetTokenCookies(c, pair, s.snapConfig().Auth.SecureCookie)
	c.JSON(http.StatusOK, gin.H{"message": "paired"})
}
