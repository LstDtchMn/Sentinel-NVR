// Sentinel NVR — the main entry point.
// Startup order: config → validate → logging → SQLite → event bus → auth (keys + admin) →
//   camera repo (seed) → go2rtc → camera manager → HTTP server.
// Graceful shutdown on SIGINT/SIGTERM with 30s timeout.
// Shutdown order: event bus (unblocks SSE handlers) → persister (drain) → HTTP server →
//   cameras → watchdog → database.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/auth"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/db"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/notification"
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

	// Initialize auth service — loads or generates JWT secret + credential key on first run (Phase 7, CG6).
	// Runs before camera repo so camera credential encryption is available during seed.
	authRepo := auth.NewRepository(database)
	var authService *auth.Service
	if cfg.Auth.Enabled {
		initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
		authService, err = auth.New(initCtx, authRepo, cfg.Auth.AccessTokenTTL, cfg.Auth.RefreshTokenTTL)
		initCancel()
		if err != nil {
			logger.Error("auth service initialization failed", "error", err)
			os.Exit(1)
		}
		// Clean up stale refresh tokens left from previous runs.
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if cleanErr := authRepo.DeleteExpiredRefreshTokens(cleanCtx); cleanErr != nil {
			logger.Warn("failed to clean expired refresh tokens", "error", cleanErr)
		}
		cleanCancel()
		// Ensure at least one admin user exists (first-run setup).
		if ensureErr := ensureAdminUser(authRepo, logger); ensureErr != nil {
			logger.Error("failed to ensure admin user", "error", ensureErr)
			os.Exit(1)
		}
		logger.Info("auth service initialized")
	} else {
		logger.Warn("authentication is disabled (auth.enabled=false) — all API endpoints are publicly accessible")
	}

	// Initialize and start watchdog (R4)
	wd := watchdog.New(&cfg.Watchdog, logger)
	go wd.Start()

	// Initialize camera repository and seed from config (one-time YAML → DB migration)
	camRepo := camera.NewRepository(database, authService)
	if err := camRepo.SeedFromConfig(context.Background(), cfg.Cameras); err != nil {
		logger.Error("failed to seed cameras from config", "error", err)
		os.Exit(1)
	}
	if camCount, err := camRepo.Count(context.Background()); err != nil {
		logger.Warn("could not read camera count from DB", "error", err)
	} else {
		logger.Info("camera repository initialized", "cameras_in_db", camCount)
	}

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

	// Start event persister — writes all events to SQLite and marks has_clip (Phase 6).
	// Started after recRepo so clip association queries are available immediately.
	var persisterWg sync.WaitGroup
	persisterWg.Add(1)
	go func() {
		defer persisterWg.Done()
		persistEvents(database, recRepo, bus, logger)
	}()

	// Initialize AI detection backend and event repository (Phase 5, R3).
	// NewDetector returns (nil, nil) when detection.enabled=false, which is safe —
	// camera pipelines check for nil detector before creating DetectionPipeline instances.
	detector, err := detection.NewDetector(&cfg.Detection, logger)
	if err != nil {
		logger.Error("failed to initialize detection backend", "error", err)
		os.Exit(1)
	}
	if detector != nil {
		logger.Info("detection backend initialized", "backend", cfg.Detection.Backend)
	}
	detRepo := detection.NewRepository(database)
	logger.Info("event repository initialized") // always active; serves /api/v1/events regardless of detection.enabled

	// Initialize notification repository + service (Phase 8, R9).
	// The repository is always created (serves the /notifications REST API regardless of
	// notifications.enabled) so that tokens and prefs can be pre-registered before enabling delivery.
	notifRepo := notification.NewRepository(database)
	var notifService *notification.Service
	if cfg.Notifications.Enabled {
		senders := map[string]notification.Sender{}

		// FCM sender: requires a Google service account JSON file.
		if cfg.Notifications.FCM.ServiceAccountJSON != "" {
			fcmSender, err := notification.NewFCMSender(cfg.Notifications.FCM.ServiceAccountJSON, logger)
			if err != nil {
				logger.Error("failed to initialize FCM sender", "error", err)
				os.Exit(1)
			}
			senders["fcm"] = fcmSender
			logger.Info("FCM sender initialized")
		}

		// APNs sender: requires a .p8 key file plus Apple credential identifiers.
		if cfg.Notifications.APNs.KeyPath != "" {
			apns := cfg.Notifications.APNs
			apnsSender, err := notification.NewAPNsSender(
				apns.KeyPath, apns.KeyID, apns.TeamID, apns.BundleID, apns.Sandbox, logger)
			if err != nil {
				logger.Error("failed to initialize APNs sender", "error", err)
				os.Exit(1)
			}
			senders["apns"] = apnsSender
			logger.Info("APNs sender initialized", "sandbox", apns.Sandbox)
		}

		// Webhook sender is always available; no credential file required.
		senders["webhook"] = notification.NewWebhookSender()

		notifService = notification.NewService(notifRepo, senders, bus, logger)
		notifService.Start()
		logger.Info("notification service started",
			"providers", func() []string {
				p := make([]string, 0, len(senders))
				for k := range senders {
					p = append(p, k)
				}
				return p
			}(),
		)
	} else {
		logger.Info("notifications disabled (notifications.enabled=false)")
	}

	// Initialize and start camera manager (DB-backed + go2rtc sync + recording + detection).
	// Bounded context prevents startup from hanging indefinitely if go2rtc is slow.
	camManager := camera.NewManager(camRepo, g2rClient, bus, cfg.Storage, cfg.Go2RTC.RTSPURL, recRepo, detector, cfg.Detection, logger)
	startCtx, startCancel := context.WithTimeout(context.Background(), 60*time.Second)
	if err := camManager.Start(startCtx); err != nil {
		startCancel()
		logger.Error("camera manager failed to start", "error", err)
		os.Exit(1)
	}
	startCancel()

	// Start HTTP server (CG2, CG7).
	serverErr := make(chan error, 1)
	srv := server.New(cfg, version, database, authService, camManager, camRepo, recRepo, detRepo, g2rClient, bus, notifRepo, logger)
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
	// bus.Close() must run BEFORE srv.Shutdown() so SSE handlers unblock immediately:
	// the SSE handler's "case event, ok := <-ch" fires with ok=false when the subscriber
	// channel is closed, allowing the handler to return and srv.Shutdown() to complete
	// quickly rather than hanging for 30s waiting for long-lived SSE connections.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bus.Close()        // unblocks SSE handlers + signals persister to exit when channel drains
	persisterWg.Wait() // drain any buffered events queued before bus closed

	// Notification service goroutine exits when the bus channel closes (range over ch terminates).
	// Wait here so any in-flight dispatch completes before the DB closes.
	if notifService != nil {
		notifService.Stop()
	}

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}

	camManager.Stop() // bus.Publish is a safe no-op after bus.Close()
	wd.Stop()

	database.Close()

	logger.Info("Sentinel NVR stopped cleanly")
}

// persistEvents subscribes to all bus events and writes them to SQLite (CG8).
// Additionally handles two Phase 6 responsibilities:
//
//  1. Retroactive has_clip: when a recording.segment_complete event arrives, updates
//     has_clip=1 on detection events whose timestamp falls within that segment's window.
//     Detection events fire DURING active recording (before the segment is in the DB),
//     so has_clip cannot be set at insert time — it must be updated after the segment finalizes.
//
//  2. SSE notification: after each successful INSERT, publishes an "events.persisted" event
//     containing the DB-assigned ID and a safe payload (no absolute thumbnail paths,
//     correct "start_time" field) so SSE clients receive EventRecord-compatible data.
//
// Runs in its own goroutine; returns when the bus is closed and the channel drains.
func persistEvents(database *sql.DB, recRepo *recording.Repository, bus *eventbus.Bus, logger *slog.Logger) {
	ch := bus.Subscribe("*")
	for event := range ch {
		// Skip our own SSE notification events — persisting them would create an infinite loop.
		if event.Type == "events.persisted" {
			continue
		}

		// Retroactive has_clip update (Phase 6 clip association).
		// When a recording segment completes, mark any detection events whose timestamp
		// falls within [startTime, endTime) as has_clip=1. The recorder publishes
		// recording.segment_complete with start_time and end_time in the Data map.
		if event.Type == "recording.segment_complete" && event.CameraID != 0 {
			if data, ok := event.Data.(map[string]any); ok {
				startTime, startOK := data["start_time"].(time.Time)
				endTime, endOK := data["end_time"].(time.Time)
				if startOK && endOK {
					clipCtx, clipCancel := context.WithTimeout(context.Background(), 5*time.Second)
					_, clipErr := database.ExecContext(clipCtx,
						`UPDATE events SET has_clip = 1
						 WHERE type = 'detection'
						   AND camera_id = ?
						   AND start_time >= ?
						   AND start_time < ?
						   AND has_clip = 0`,
						event.CameraID, startTime, endTime,
					)
					clipCancel()
					if clipErr != nil {
						logger.Warn("failed to retroactively update has_clip",
							"error", clipErr, "camera_id", event.CameraID,
							"start", startTime, "end", endTime)
					}
				}
			}
			// Fall through: also persist the recording.segment_complete event to DB.
		}

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

		// At insert time, check if an already-completed recording covers this detection.
		// Handles the edge case where a detection fires just after a segment finalized.
		// In the common case (detection during active recording) the segment isn't in the DB
		// yet, ExistsForCameraAtTime returns false, and has_clip is updated retroactively above.
		hasClip := 0
		if event.Type == "detection" && event.CameraID != 0 {
			clipCtx, clipCancel := context.WithTimeout(context.Background(), 3*time.Second)
			exists, clipErr := recRepo.ExistsForCameraAtTime(clipCtx, event.CameraID, event.Timestamp)
			clipCancel()
			if clipErr != nil {
				logger.Warn("failed to check clip association; has_clip will be updated retroactively",
					"error", clipErr, "camera_id", event.CameraID)
			} else if exists {
				hasClip = 1
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		result, err := database.ExecContext(ctx,
			`INSERT INTO events (camera_id, type, label, confidence, data, thumbnail, has_clip, start_time)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			cameraID, event.Type, event.Label, event.Confidence, dataJSON,
			event.Thumbnail, hasClip, event.Timestamp,
		)
		cancel()
		if err != nil {
			logger.Error("failed to persist event", "error", err, "type", event.Type)
			continue
		}

		// Publish "events.persisted" so SSE clients receive an EventRecord-compatible payload.
		// Raw eventbus.Event has two issues for SSE: Thumbnail is an absolute filesystem path
		// (security leak) and the timestamp key is "timestamp" not "start_time" (schema mismatch).
		// The persisted payload uses the DB-assigned ID and maps Timestamp → start_time.
		// thumbnail is set to "yes" (non-empty → truthy) when a snapshot exists — never the path.
		if id, idErr := result.LastInsertId(); idErr == nil {
			thumbIndicator := ""
			if event.Thumbnail != "" {
				thumbIndicator = "yes" // absolute path never leaves the server
			}
			bus.Publish(eventbus.Event{
				Type:      "events.persisted",
				CameraID:  event.CameraID,
				Timestamp: event.Timestamp,
				Data: map[string]any{
					"id":         id,
					"camera_id":  cameraID, // nil (JSON null) when camera_id is unknown
					"type":       event.Type,
					"label":      event.Label,
					"confidence": event.Confidence,
					"thumbnail":  thumbIndicator,
					"has_clip":   hasClip != 0,
					"start_time": event.Timestamp.Format(time.RFC3339),
					"created_at": time.Now().Format(time.RFC3339),
				},
			})
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

// ensureAdminUser creates the initial admin account on first run if no users exist.
// The password is read from the SENTINEL_ADMIN_PASSWORD environment variable.
// If the env var is absent, a random 16-character password is generated and
// printed to stdout so the operator can retrieve it from the container logs.
func ensureAdminUser(repo *auth.Repository, logger *slog.Logger) error {
	// 30-second budget: GetUserByUsername is fast, but bcrypt.GenerateFromPassword
	// can take 2-3 seconds on low-spec hardware (Raspberry Pi, constrained Docker).
	// A 5-second context would leave too little room for the final CreateUser call.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := repo.GetUserByUsername(ctx, "admin")
	if err == nil {
		return nil // admin already exists
	}
	if !errors.Is(err, auth.ErrNotFound) {
		return fmt.Errorf("checking for admin user: %w", err)
	}

	// First run — no admin user yet.
	password := os.Getenv("SENTINEL_ADMIN_PASSWORD")
	generated := false
	if password == "" {
		var genErr error
		password, genErr = generateAdminPassword()
		if genErr != nil {
			return fmt.Errorf("generating admin password: %w", genErr)
		}
		generated = true
	}

	// HashPassword (bcrypt) does not take a context; the elapsed time still counts
	// against the deadline, which is why we use a 30s budget above.
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hashing admin password: %w", err)
	}
	if _, err := repo.CreateUser(ctx, "admin", hash, "admin"); err != nil {
		return fmt.Errorf("creating admin user: %w", err)
	}

	if generated {
		// Log at Warn so it appears even at non-debug log levels.
		logger.Warn("===== INITIAL ADMIN PASSWORD (set SENTINEL_ADMIN_PASSWORD to choose your own) =====",
			"username", "admin",
			"password", password,
		)
	} else {
		logger.Info("created initial admin user", "username", "admin")
	}
	return nil
}

// generateAdminPassword returns a cryptographically random 16-character alphanumeric password.
func generateAdminPassword() (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 16)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		b[i] = charset[n.Int64()]
	}
	return string(b), nil
}
