// This file implements WebSocket reverse proxy for go2rtc MSE live streaming (CG3, R7).
// Browser connects to /api/v1/streams/:name/ws → sentinel proxies to go2rtc's MSE endpoint.
// go2rtc is on the backend Docker network only (no browser access by design); sentinel
// bridges both networks. Phase 7 will add JWT auth validation before websocket.Accept.

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"nhooyr.io/websocket"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
)

// handleStreamWS proxies a WebSocket connection to go2rtc's MSE endpoint.
// The browser opens a WS to /api/v1/streams/:name/ws; this handler validates the
// camera exists, upgrades to WebSocket, dials go2rtc, and relays frames bidirectionally.
//
// Context chain: c.Request.Context() propagates graceful server shutdown to all relay
// goroutines so httpServer.Shutdown() drains active stream connections cleanly.
func (s *Server) handleStreamWS(c *gin.Context) {
	name := c.Param("name")

	// Validate camera exists in DB — prevents proxying arbitrary stream names.
	lookupCtx, lookupCancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer lookupCancel()

	_, err := s.camRepo.GetByName(lookupCtx, name)
	if errors.Is(err, camera.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to check camera for stream proxy", "name", name, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Clear WriteTimeout — WebSocket connections are long-lived and must not be
	// killed by the 15s server timeout (same pattern as handlePlayRecording).
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.logger.Warn("failed to clear write deadline for stream proxy", "error", err)
	}

	// Accept WebSocket from browser with origin validation.
	// OriginPatterns lists the allowed origins from the server config (Phase 7, CG6).
	// This replaces the earlier InsecureSkipVerify: the CORS middleware guards HTTP
	// requests, but the WebSocket protocol needs its own origin check at Accept time.
	// Each origin is converted to a pattern by stripping the scheme prefix.
	clientConn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		OriginPatterns: s.wsOriginPatterns(),
	})
	if err != nil {
		s.logger.Error("failed to accept websocket", "camera", name, "error", err)
		return // Accept already wrote the HTTP error response
	}
	defer clientConn.CloseNow()

	// Increase read limit — go2rtc sends binary fMP4 frames that can be large (video keyframes).
	clientConn.SetReadLimit(4 * 1024 * 1024) // 4 MiB

	s.logger.Info("stream proxy connected", "camera", name, "remote", c.ClientIP())

	// Dial go2rtc, inheriting c.Request.Context() as parent so a client that disconnects
	// during the dial cancels it promptly — preventing go2rtc connection exhaustion under
	// high-churn reconnect scenarios. The 10s timeout caps slow go2rtc responses.
	dialCtx, dialCancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer dialCancel()

	go2rtcWSURL := buildGo2rtcWSURL(s.cfg.Go2RTC.APIURL, name)
	upstreamConn, _, err := websocket.Dial(dialCtx, go2rtcWSURL, nil)
	if err != nil {
		s.logger.Error("failed to connect to go2rtc", "camera", name, "url", go2rtcWSURL, "error", err)
		clientConn.Close(websocket.StatusInternalError, "upstream unavailable")
		return
	}
	defer upstreamConn.CloseNow()

	// Increase read limit on upstream too — large keyframes from go2rtc.
	upstreamConn.SetReadLimit(4 * 1024 * 1024) // 4 MiB

	// Relay context inherits c.Request.Context() so that:
	// 1. Graceful server shutdown (httpServer.Shutdown) propagates to relay goroutines.
	// 2. If the browser disconnects, the underlying request context is cancelled,
	//    unblocking both goroutines without waiting for a read/write timeout.
	relayCtx, relayCancel := context.WithCancel(c.Request.Context())
	defer relayCancel()

	errCh := make(chan error, 2)

	// browser → go2rtc (JSON messages: codec negotiation, control)
	go func() {
		errCh <- relayWS(relayCtx, clientConn, upstreamConn, "client→go2rtc")
	}()

	// go2rtc → browser (binary fMP4 frames + JSON status messages)
	go func() {
		errCh <- relayWS(relayCtx, upstreamConn, clientConn, "go2rtc→client")
	}()

	// Wait for the first relay direction to finish. The second goroutine will exit
	// shortly after relayCancel() propagates (bounded by the timeoutLoop reaction time
	// in nhooyr.io/websocket). The channel is buffered=2 so neither goroutine blocks.
	err = <-errCh
	relayCancel()

	// Log abnormal terminations — protocol errors, go2rtc failures, etc.
	// Normal closures (browser navigated away, Focus Mode toggle) are expected and silent.
	if err != nil && !isNormalClose(err) {
		s.logger.Warn("stream proxy relay ended abnormally", "camera", name, "reason", err)
	}

	s.logger.Info("stream proxy disconnected", "camera", name)
}

// relayWS copies WebSocket messages from src to dst until the context is cancelled
// or either connection closes. It handles both text (JSON) and binary (fMP4) messages.
// Each write has a 10-second timeout to prevent a slow consumer from blocking the
// relay indefinitely (backpressure).
func relayWS(ctx context.Context, src, dst *websocket.Conn, direction string) error {
	for {
		msgType, data, err := src.Read(ctx)
		if err != nil {
			return fmt.Errorf("%s read: %w", direction, err)
		}

		writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
		err = dst.Write(writeCtx, msgType, data)
		writeCancel()
		if err != nil {
			return fmt.Errorf("%s write: %w", direction, err)
		}
	}
}

// buildGo2rtcWSURL converts the go2rtc API base URL to a WebSocket URL for MSE streaming.
// http://go2rtc:1984 → ws://go2rtc:1984/api/ws?src={camera_name}
func buildGo2rtcWSURL(apiBase, cameraName string) string {
	wsBase := apiBase
	if strings.HasPrefix(wsBase, "https://") {
		wsBase = "wss://" + strings.TrimPrefix(wsBase, "https://")
	} else if strings.HasPrefix(wsBase, "http://") {
		wsBase = "ws://" + strings.TrimPrefix(wsBase, "http://")
	}
	return wsBase + "/api/ws?src=" + url.QueryEscape(cameraName)
}

// wsOriginPatterns converts the configured AllowedOrigins into patterns
// accepted by nhooyr.io/websocket.AcceptOptions.OriginPatterns.
// The library expects patterns like "localhost:5173" or "*.example.com"
// (scheme is handled internally by comparing against the request's scheme).
//
// When auth is enabled and AllowedOrigins is empty, returns an empty slice so
// WebSocket upgrades are denied from all origins (deny-by-default, M-7, CG6).
// When auth is disabled, falls back to "*" for the unauthenticated dev experience.
func (s *Server) wsOriginPatterns() []string {
	origins := s.cfg.Auth.AllowedOrigins
	if len(origins) == 0 {
		if s.authService != nil {
			return []string{} // auth enabled, no origins configured → deny all
		}
		return []string{"*"} // auth disabled → allow all (dev/unauthenticated mode)
	}
	patterns := make([]string, 0, len(origins))
	for _, o := range origins {
		// Strip scheme prefix — OriginPatterns matches on host[:port].
		host := o
		host = strings.TrimPrefix(host, "https://")
		host = strings.TrimPrefix(host, "http://")
		host = strings.TrimRight(host, "/") // strip trailing slashes (M-8)
		if host != "" {
			patterns = append(patterns, host)
		}
	}
	if len(patterns) == 0 {
		if s.authService != nil {
			return []string{}
		}
		return []string{"*"}
	}
	return patterns
}

// isNormalClose returns true if the error represents a normal WebSocket closure
// (client navigated away, Focus Mode toggled, graceful shutdown via context cancellation).
func isNormalClose(err error) bool {
	if err == nil {
		return true
	}
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		return closeErr.Code == websocket.StatusNormalClosure ||
			closeErr.Code == websocket.StatusGoingAway
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, io.EOF)
}
