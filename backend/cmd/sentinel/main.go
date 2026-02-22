// Sentinel NVR — the main entry point.
// Startup order: config → validate → logging → SQLite → event bus → camera repo (seed) → go2rtc → camera manager → HTTP server.
// Graceful shutdown on SIGINT/SIGTERM with 30s timeout.
// Shutdown order: HTTP server → camera manager (wait) → watchdog → event bus (wait for persister) → database.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/db"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/server"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/watchdog"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/go2rtc"
)

// version is set at build time via -ldflags="-X main.version=x.y.z".
var version = "0.1.0-dev"

func main() {
	configPath := flag.String("config", "/etc/sentinel/sentinel.yml", "path to configuration file")
	flag.Parse()

	// Load and validate configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config validation error: %v\n", err)
		os.Exit(1)
	}

	// Set up structured logging
	logLevel := parseLogLevel(cfg.Server.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	logger.Info("starting Sentinel NVR",
		"version", version,
		"config", *configPath,
	)

	// Initialize database (CG2, CG9)
	database, err := db.Open(cfg.Database.Path, cfg.Database.WALMode, logger)
	if err != nil {
		logger.Error("database initialization failed", "error", err)
		os.Exit(1)
	}
	logger.Info("database initialized", "path", cfg.Database.Path, "wal_mode", cfg.Database.WALMode)

	// Initialize event bus (CG8)
	bus := eventbus.New(128, logger)
	logger.Info("event bus initialized")

	// Start event persister — writes all events to SQLite (CG8 write-ahead).
	var persisterWg sync.WaitGroup
	persisterWg.Add(1)
	go func() {
		defer persisterWg.Done()
		persistEvents(database, bus, logger)
	}()

	// Initialize and start watchdog (R4)
	wd := watchdog.New(&cfg.Watchdog, logger)
	go wd.Start()

	// Initialize camera repository and seed from config (one-time YAML → DB migration)
	camRepo := camera.NewRepository(database)
	if err := camRepo.SeedFromConfig(context.Background(), cfg.Cameras); err != nil {
		logger.Error("failed to seed cameras from config", "error", err)
		os.Exit(1)
	}
	camCount, _ := camRepo.Count(context.Background())
	logger.Info("camera repository initialized", "cameras_in_db", camCount)

	// Initialize go2rtc client and wait for sidecar to be ready
	g2rClient := go2rtc.NewClient(cfg.Go2RTC.APIURL)
	logger.Info("waiting for go2rtc", "url", cfg.Go2RTC.APIURL)
	g2rCtx, g2rCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := g2rClient.WaitReady(g2rCtx); err != nil {
		logger.Error("go2rtc not reachable", "error", err, "url", cfg.Go2RTC.APIURL)
		os.Exit(1)
	}
	g2rCancel()
	logger.Info("go2rtc connected")

	// Initialize recording repository (Phase 2 — segment tracking)
	recRepo := recording.NewRepository(database)
	logger.Info("recording repository initialized")

	// Initialize and start camera manager (DB-backed + go2rtc sync + recording).
	// Bounded context prevents startup from hanging indefinitely if go2rtc is slow.
	camManager := camera.NewManager(camRepo, g2rClient, bus, cfg.Storage, cfg.Go2RTC.RTSPURL, recRepo, logger)
	startCtx, startCancel := context.WithTimeout(context.Background(), 60*time.Second)
	if err := camManager.Start(startCtx); err != nil {
		startCancel()
		logger.Error("camera manager failed to start", "error", err)
		os.Exit(1)
	}
	startCancel()

	// Start HTTP server (CG2, CG7).
	serverErr := make(chan error, 1)
	srv := server.New(cfg, version, database, camManager, camRepo, recRepo, g2rClient, bus, logger)
	go func() {
		if err := srv.Start(); err != nil {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal (SIGINT/SIGTERM) or server failure
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("received shutdown signal", "signal", sig.String())
	case err := <-serverErr:
		logger.Error("server failed, initiating shutdown", "error", err)
	}

	// Graceful shutdown with 30s timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}

	camManager.Stop()
	wd.Stop()

	bus.Close()
	persisterWg.Wait()

	database.Close()

	logger.Info("Sentinel NVR stopped cleanly")
}

// persistEvents subscribes to all events and writes them to SQLite (CG8).
// Runs in its own goroutine; returns when the bus is closed and the channel drains.
func persistEvents(database *sql.DB, bus *eventbus.Bus, logger *slog.Logger) {
	ch := bus.Subscribe("*")
	for event := range ch {
		dataJSON := "{}"
		if event.Data != nil {
			b, err := json.Marshal(event.Data)
			if err != nil {
				logger.Warn("failed to marshal event data, storing empty object",
					"error", err, "type", event.Type)
			} else {
				dataJSON = string(b)
			}
		}

		var cameraID any
		if event.CameraID != 0 {
			cameraID = event.CameraID
		}

		_, err := database.Exec(
			`INSERT INTO events (camera_id, type, label, confidence, data, start_time)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			cameraID, event.Type, event.Label, event.Confidence, dataJSON, event.Timestamp,
		)
		if err != nil {
			logger.Error("failed to persist event", "error", err, "type", event.Type)
		}
	}
}

func parseLogLevel(level string) slog.Level {
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
