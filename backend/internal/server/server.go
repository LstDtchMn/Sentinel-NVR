// Package server provides the Gin-powered HTTP API server (CG2, CG7).
package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/auth"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/notification"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/go2rtc"
)

// Server wraps a Gin engine inside an http.Server for graceful shutdown.
type Server struct {
	cfg                  *config.Config
	version              string
	startTime            time.Time
	resolvedHotPath      string         // symlink-resolved HotPath for containment checks
	resolvedColdPath     string         // symlink-resolved ColdPath for containment checks
	resolvedSnapshotPath string         // symlink-resolved SnapshotPath for thumbnail containment checks (Phase 5)
	db                   *sql.DB
	authService          *auth.Service  // Phase 7: nil when auth.enabled=false
	camManager           *camera.Manager
	camRepo              *camera.Repository
	recRepo              *recording.Repository
	detRepo              *detection.Repository // Phase 5: events CRUD
	g2r                  *go2rtc.Client
	eventBus             *eventbus.Bus         // Phase 3: used for WebSocket/SSE real-time event streaming
	notifRepo            *notification.Repository  // Phase 8: token/pref management API (R9)
	loginLimiter         *loginRateLimiter         // brute-force protection for /auth/login (CG6)
	router               *gin.Engine
	httpServer           *http.Server
	logger               *slog.Logger
}

// New creates a configured HTTP server with all routes registered.
// authService may be nil when auth.enabled=false — all routes are public in that case.
// notifRepo may be nil when notifications.enabled=false.
func New(cfg *config.Config, version string, db *sql.DB, authService *auth.Service, camManager *camera.Manager, camRepo *camera.Repository, recRepo *recording.Repository, detRepo *detection.Repository, g2r *go2rtc.Client, eventBus *eventbus.Bus, notifRepo *notification.Repository, logger *slog.Logger) *Server {
	if cfg.Server.LogLevel != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Resolve storage path symlinks at startup so isUnderPath comparisons work
	// when /media/hot, /media/cold, or /data/snapshots are symlinks (e.g. bind mounts, NAS).
	// Falls back to the configured path if resolution fails (directory may not exist yet).
	resolvedHot := cfg.Storage.HotPath
	if resolved, err := filepath.EvalSymlinks(cfg.Storage.HotPath); err == nil {
		resolvedHot = resolved
	}
	resolvedCold := cfg.Storage.ColdPath
	if resolved, err := filepath.EvalSymlinks(cfg.Storage.ColdPath); err == nil {
		resolvedCold = resolved
	}
	resolvedSnapshot := cfg.Detection.SnapshotPath
	if resolved, err := filepath.EvalSymlinks(cfg.Detection.SnapshotPath); err == nil {
		resolvedSnapshot = resolved
	}

	s := &Server{
		cfg:                  cfg,
		version:              version,
		startTime:            time.Now(),
		resolvedHotPath:      resolvedHot,
		resolvedColdPath:     resolvedCold,
		resolvedSnapshotPath: resolvedSnapshot,
		db:                   db,
		authService:          authService,
		camManager:           camManager,
		camRepo:              camRepo,
		recRepo:              recRepo,
		detRepo:              detRepo,
		g2r:                  g2r,
		eventBus:             eventBus,
		notifRepo:            notifRepo,
		loginLimiter:         newLoginRateLimiter(5, 5*time.Minute),
		router:               router,
		logger:               logger.With("component", "http_server"),
	}

	// Middleware
	router.Use(s.loggerMiddleware())
	router.Use(gin.Recovery())
	router.Use(s.corsMiddleware())

	// Routes
	s.registerRoutes()

	// WriteTimeout is cleared per-connection for long-lived requests:
	// - Phase 2: handlePlayRecording clears it for large file transfers
	// - Phase 3: handleStreamWS clears it for WebSocket MSE proxy
	// - Phase 6: handleEventStream clears it for SSE real-time event streaming
	// - Phase 5: handleEventThumbnail clears it for consistency with future large thumbnails
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start begins listening. Blocks until the server stops.
func (s *Server) Start() error {
	s.logger.Info("starting HTTP server", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down HTTP server")
	return s.httpServer.Shutdown(ctx)
}
