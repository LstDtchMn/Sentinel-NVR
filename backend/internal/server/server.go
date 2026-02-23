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

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/go2rtc"
)

// Server wraps a Gin engine inside an http.Server for graceful shutdown.
type Server struct {
	cfg             *config.Config
	version         string
	startTime       time.Time
	resolvedHotPath string // symlink-resolved HotPath for containment checks
	resolvedColdPath string // symlink-resolved ColdPath for containment checks
	db              *sql.DB
	camManager      *camera.Manager
	camRepo         *camera.Repository
	recRepo         *recording.Repository
	g2r             *go2rtc.Client
	eventBus        *eventbus.Bus // Phase 3: used for WebSocket/SSE real-time event streaming
	router          *gin.Engine
	httpServer      *http.Server
	logger          *slog.Logger
}

// New creates a configured HTTP server with all routes registered.
func New(cfg *config.Config, version string, db *sql.DB, camManager *camera.Manager, camRepo *camera.Repository, recRepo *recording.Repository, g2r *go2rtc.Client, eventBus *eventbus.Bus, logger *slog.Logger) *Server {
	if cfg.Server.LogLevel != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Resolve storage path symlinks at startup so isUnderPath comparisons work
	// when /media/hot or /media/cold are symlinks (e.g. bind mounts, NAS).
	// Falls back to the configured path if resolution fails (directory may not exist yet).
	resolvedHot := cfg.Storage.HotPath
	if resolved, err := filepath.EvalSymlinks(cfg.Storage.HotPath); err == nil {
		resolvedHot = resolved
	}
	resolvedCold := cfg.Storage.ColdPath
	if resolved, err := filepath.EvalSymlinks(cfg.Storage.ColdPath); err == nil {
		resolvedCold = resolved
	}

	s := &Server{
		cfg:              cfg,
		version:          version,
		startTime:        time.Now(),
		resolvedHotPath:  resolvedHot,
		resolvedColdPath: resolvedCold,
		db:               db,
		camManager:       camManager,
		camRepo:          camRepo,
		recRepo:          recRepo,
		g2r:              g2r,
		eventBus:         eventBus,
		router:           router,
		logger:           logger.With("component", "http_server"),
	}

	// Middleware
	router.Use(s.loggerMiddleware())
	router.Use(gin.Recovery())
	router.Use(s.corsMiddleware())

	// Routes
	s.registerRoutes()

	// TODO: Phase 3 — WriteTimeout of 15s will kill WebSocket/SSE connections.
	// Phase 2 uses http.ResponseController.SetWriteDeadline() per-connection for
	// file streaming. Phase 3 will need a similar approach for real-time events.
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
