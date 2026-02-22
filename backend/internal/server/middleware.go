// This file defines Gin middleware for logging and CORS.

package server

import (
	"time"

	"github.com/gin-gonic/gin"
)

// loggerMiddleware bridges Gin HTTP requests into slog structured logging.
func (s *Server) loggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		s.logger.Info("request",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"duration", time.Since(start).String(),
			"ip", c.ClientIP(),
		)
	}
}

// corsMiddleware allows cross-origin requests from the frontend dev server.
// TODO: Phase 7 — Replace wildcard origin with configured frontend URL,
// add Access-Control-Allow-Credentials: true, and add Authorization to
// allowed headers once JWT auth is implemented.
func (s *Server) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
