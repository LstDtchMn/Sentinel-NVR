// This file defines Gin middleware for logging, CORS, and rate limiting.

package server

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// loginRateLimiter tracks failed login attempts per IP address.
// After maxAttempts failures within the window, further login requests are
// rejected with 429 Too Many Requests until the window expires (CG6).
type loginRateLimiter struct {
	mu          sync.Mutex
	attempts    map[string]*ipAttempts
	maxAttempts int
	window      time.Duration
	stopCh      chan struct{} // signals the background cleanup goroutine to exit
}

type ipAttempts struct {
	count   int
	resetAt time.Time
}

func newLoginRateLimiter(maxAttempts int, window time.Duration) *loginRateLimiter {
	rl := &loginRateLimiter{
		attempts:    make(map[string]*ipAttempts),
		maxAttempts: maxAttempts,
		window:      window,
		stopCh:      make(chan struct{}),
	}
	// Background cleanup: prune entries whose window has expired to prevent
	// unbounded map growth when serving many distinct IP addresses (CG6).
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.mu.Lock()
				now := time.Now()
				for ip, a := range rl.attempts {
					if now.After(a.resetAt) {
						delete(rl.attempts, ip)
					}
				}
				rl.mu.Unlock()
			case <-rl.stopCh:
				return
			}
		}
	}()
	return rl
}

// stopCleanup stops the background cleanup goroutine. Called from Server.Shutdown.
func (rl *loginRateLimiter) stopCleanup() {
	close(rl.stopCh)
}

// allow returns true if the IP has not exceeded the rate limit.
func (rl *loginRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	a, ok := rl.attempts[ip]
	if !ok || now.After(a.resetAt) {
		rl.attempts[ip] = &ipAttempts{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	a.count++
	return a.count <= rl.maxAttempts
}

// reset clears the attempt counter for an IP (called on successful login).
func (rl *loginRateLimiter) reset(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, ip)
}

// loggerMiddleware bridges Gin HTTP requests into slog structured logging.
// Health checks are logged at Debug level to avoid polluting production logs —
// they fire every 30s from the Docker healthcheck and every 10s from the Dashboard.
func (s *Server) loggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		logFn := s.logger.Info
		if path == "/api/v1/health" {
			logFn = s.logger.Debug
		}
		logFn("request",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"duration", time.Since(start).String(),
			"ip", c.ClientIP(),
		)
	}
}

// corsMiddleware handles cross-origin requests from the Vite dev server.
// When auth is enabled, cookies require a specific origin (not wildcard) and
// Access-Control-Allow-Credentials: true. The allowed origins list is read
// from cfg.Auth.AllowedOrigins (Phase 7, CG6).
func (s *Server) corsMiddleware() gin.HandlerFunc {
	// Build a set for O(1) origin lookups.
	allowed := make(map[string]bool, len(s.cfg.Auth.AllowedOrigins))
	for _, o := range s.cfg.Auth.AllowedOrigins {
		allowed[o] = true
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			// Reflect the specific origin so browsers accept cookies.
			// Access-Control-Allow-Origin: * cannot be combined with credentials.
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
