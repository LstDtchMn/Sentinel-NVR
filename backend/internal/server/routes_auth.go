// routes_auth.go — authentication, setup, and OIDC SSO handlers.

package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/auth"
)

// handleSetupCheck reports whether first-run setup is needed (Phase 7, CG6).
// GET /api/v1/setup — public; returns {"needs_setup": bool, "oidc_enabled": bool}.
func (s *Server) handleSetupCheck(c *gin.Context) {
	if s.authService == nil {
		// Auth is disabled — no setup needed, no OIDC.
		c.JSON(http.StatusOK, gin.H{"needs_setup": false, "oidc_enabled": false})
		return
	}
	needs, err := s.authService.NeedsSetup(c.Request.Context())
	if err != nil {
		s.logger.Error("setup check failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "setup check failed"})
		return
	}
	cfg := s.snapConfig()
	c.JSON(http.StatusOK, gin.H{
		"needs_setup":  needs,
		"oidc_enabled": cfg.Auth.OIDC.Enabled,
	})
}

// handleSetupCreate creates the first admin account during first-run setup (Phase 7, CG6).
// POST /api/v1/setup   body: {"username":"...","password":"..."}
// Responds with the created user and sets auth cookies, same as /auth/login.
func (s *Server) handleSetupCreate(c *gin.Context) {
	if s.authService == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "auth is disabled; setup is not required"})
		return
	}

	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}

	// Validate username format — same rules as camera names (printable, no shell-special chars).
	if !setupUsernameRE.MatchString(req.Username) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username must be 1–64 alphanumeric/space/dash/underscore characters"})
		return
	}
	if len(req.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		return
	}
	// bcrypt silently truncates inputs at 72 bytes. A multi-MB password would still
	// consume ~1s of CPU before truncation — enforce the limit explicitly (CG6).
	if len(req.Password) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must not exceed 72 characters"})
		return
	}

	user, pair, err := s.authService.Setup(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrSetupAlreadyDone) {
			c.JSON(http.StatusConflict, gin.H{"error": "setup already completed"})
			return
		}
		s.logger.Error("setup failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "setup failed"})
		return
	}

	auth.SetTokenCookies(c, pair, s.snapConfig().Auth.SecureCookie)
	c.JSON(http.StatusCreated, gin.H{
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

// handleAuthLogin authenticates a user and issues JWT + refresh token cookies (Phase 7, CG6).
// POST /api/v1/auth/login   body: {"username":"...","password":"..."}
func (s *Server) handleAuthLogin(c *gin.Context) {
	if s.authService == nil {
		c.JSON(http.StatusOK, gin.H{"message": "auth disabled"})
		return
	}

	// Rate-limit login attempts per IP to prevent brute-force attacks (CG6).
	ip := c.ClientIP()
	if !s.loginLimiter.allow(ip) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts, try again later"})
		return
	}

	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}
	// bcrypt silently truncates inputs at 72 bytes. A multi-MB password would still
	// consume ~1s of CPU before truncation — enforce the limit explicitly (CG6).
	if len(req.Password) > 72 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	pair, err := s.authService.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		s.logger.Error("login error", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}

	// Reset rate limit on successful login so legitimate users aren't locked out.
	s.loginLimiter.reset(ip)
	auth.SetTokenCookies(c, pair, s.snapConfig().Auth.SecureCookie)
	s.logger.Info("user logged in", "username", req.Username)
	c.JSON(http.StatusOK, gin.H{"message": "logged in"})
}

// handleAuthRefresh rotates the refresh token and issues a new access token (Phase 7, CG6).
// POST /api/v1/auth/refresh  — reads sentinel_refresh cookie
func (s *Server) handleAuthRefresh(c *gin.Context) {
	if s.authService == nil {
		c.JSON(http.StatusOK, gin.H{"message": "auth disabled"})
		return
	}
	refreshToken, err := c.Cookie(auth.RefreshCookieName)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh token required"})
		return
	}

	pair, err := s.authService.Refresh(c.Request.Context(), refreshToken)
	secureCookie := s.snapConfig().Auth.SecureCookie
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrTokenExpired) {
			auth.ClearTokenCookies(c, secureCookie)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "session expired, please log in again"})
			return
		}
		s.logger.Error("refresh error", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "refresh failed"})
		return
	}

	auth.SetTokenCookies(c, pair, secureCookie)
	c.JSON(http.StatusOK, gin.H{"message": "refreshed"})
}

// handleAuthLogout revokes the refresh token and clears auth cookies (Phase 7, CG6).
// POST /api/v1/auth/logout  — reads sentinel_refresh cookie (no JWT required)
func (s *Server) handleAuthLogout(c *gin.Context) {
	if s.authService != nil {
		if refreshToken, err := c.Cookie(auth.RefreshCookieName); err == nil {
			if err := s.authService.Logout(c.Request.Context(), refreshToken); err != nil {
				// Non-fatal: session may already be expired. Log for operator visibility.
				s.logger.Warn("logout: failed to delete refresh token from DB", "error", err)
			}
		}
	}
	auth.ClearTokenCookies(c, s.snapConfig().Auth.SecureCookie)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// handleAuthMe returns the authenticated user's public profile (Phase 7, CG6).
// GET /api/v1/auth/me — requires valid JWT (via protected group middleware)
func (s *Server) handleAuthMe(c *gin.Context) {
	if s.authService == nil {
		c.JSON(http.StatusOK, gin.H{"authenticated": false, "message": "auth disabled"})
		return
	}
	userID, _ := c.Get(auth.CtxKeyUserID)
	username, _ := c.Get(auth.CtxKeyUsername)
	role, _ := c.Get(auth.CtxKeyRole)
	c.JSON(http.StatusOK, gin.H{
		"id":       userID,
		"username": username,
		"role":     role,
	})
}

// ─── OIDC SSO handlers (Phase 7, CG6) ───────────────────────────────────────

// handleOIDCLogin initiates the OIDC authorization code flow.
// GET /api/v1/auth/oidc/login — redirects the browser to the identity provider.
// Only reachable when oidcProvider is non-nil (s.oidcProvider != nil in registerRoutes).
func (s *Server) handleOIDCLogin(c *gin.Context) {
	url, err := s.oidcProvider.AuthURL()
	if err != nil {
		s.logger.Error("OIDC: failed to generate auth URL", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "OIDC unavailable"})
		return
	}
	c.Redirect(http.StatusFound, url)
}

// handleOIDCCallback handles the authorization code redirect from the identity provider.
// GET /api/v1/auth/oidc/callback?code=...&state=...
// Validates state, exchanges the code, finds or provisions a local user, and sets session cookies.
func (s *Server) handleOIDCCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		c.Redirect(http.StatusFound, "/login?error=oidc_missing_params")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	sub, username, email, err := s.oidcProvider.Exchange(ctx, code, state)
	if err != nil {
		s.logger.Warn("OIDC callback: token exchange failed", "error", err)
		c.Redirect(http.StatusFound, "/login?error=oidc_failed")
		return
	}

	pair, err := s.authService.OIDCLoginOrCreate(ctx, sub, username, email)
	if err != nil {
		s.logger.Error("OIDC callback: login/create failed", "error", err)
		c.Redirect(http.StatusFound, "/login?error=oidc_failed")
		return
	}

	auth.SetTokenCookies(c, pair, s.snapConfig().Auth.SecureCookie)
	c.Redirect(http.StatusFound, "/live")
}
