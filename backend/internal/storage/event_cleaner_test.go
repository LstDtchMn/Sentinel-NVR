package storage

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/db"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
)

// ─── Mock EventDeleter ───────────────────────────────────────────────────────

// mockEventDeleter tracks calls to DeleteOlderThan for verification.
type mockEventDeleter struct {
	calls []deleteCall
	// result controls how many rows are "deleted" per call
	result int
	err    error
}

type deleteCall struct {
	cameraID         *int
	eventType        string
	cutoff           time.Time
	limit            int
	excludeCameraIDs []int
}

func (m *mockEventDeleter) DeleteOlderThan(ctx context.Context, cameraID *int, eventType string, cutoff time.Time, limit int, excludeCameraIDs ...int) (int, error) {
	m.calls = append(m.calls, deleteCall{
		cameraID:         cameraID,
		eventType:        eventType,
		cutoff:           cutoff,
		limit:            limit,
		excludeCameraIDs: excludeCameraIDs,
	})
	return m.result, m.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func openEventCleanerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:", false, slog.Default())
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// ─── Tests ───────────────────────────────────────────────────────────────────

func TestEventCleaner_GlobalFallback_ExcludesPerCameraCoverage(t *testing.T) {
	database := openEventCleanerTestDB(t)

	// Insert cameras
	var cam1ID, cam2ID int
	database.QueryRow(`INSERT INTO cameras (name, enabled, main_stream) VALUES ('cam1', 1, 'rtsp://x') RETURNING id`).Scan(&cam1ID)
	database.QueryRow(`INSERT INTO cameras (name, enabled, main_stream) VALUES ('cam2', 1, 'rtsp://y') RETURNING id`).Scan(&cam2ID)

	retRepo := NewRetentionRepository(database)

	// Create per-camera rule for cam1 on "detection" (7 days)
	_, err := retRepo.Create(context.Background(), &cam1ID, strPtr("detection"), 7)
	if err != nil {
		t.Fatalf("create retention rule: %v", err)
	}

	deleter := &mockEventDeleter{result: 0}

	cfg := &config.StorageConfig{
		HotPath:           t.TempDir(),
		ColdPath:          t.TempDir(),
		ColdRetentionDays: 30,
		CleanupIntervalHours: 24,
	}
	mgr := &Manager{
		cfg:              cfg,
		recRepo:          recording.NewRepository(database),
		retentionRepo:    retRepo,
		eventDeleter:     deleter,
		logger:           slog.Default(),
		resolvedHotPath:  cfg.HotPath,
		resolvedColdPath: cfg.ColdPath,
	}

	mgr.runEventCleanerOnce(context.Background())

	// Verify: the global fallback for "detection" type should exclude cam1
	var globalDetectionCall *deleteCall
	for i := range deleter.calls {
		c := &deleter.calls[i]
		if c.cameraID == nil && c.eventType == "detection" {
			globalDetectionCall = c
			break
		}
	}
	if globalDetectionCall == nil {
		t.Fatal("expected a global fallback call for 'detection' type")
	}
	if len(globalDetectionCall.excludeCameraIDs) == 0 {
		t.Error("global fallback should exclude cam1 (covered by per-camera rule)")
	}

	found := false
	for _, id := range globalDetectionCall.excludeCameraIDs {
		if id == cam1ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("cam1 (ID=%d) should be in excludeCameraIDs, got %v", cam1ID, globalDetectionCall.excludeCameraIDs)
	}
}

func TestEventCleaner_PerCameraRule_DeletesDirectly(t *testing.T) {
	database := openEventCleanerTestDB(t)

	var camID int
	database.QueryRow(`INSERT INTO cameras (name, enabled, main_stream) VALUES ('cam1', 1, 'rtsp://x') RETURNING id`).Scan(&camID)

	retRepo := NewRetentionRepository(database)
	_, err := retRepo.Create(context.Background(), &camID, strPtr("detection"), 7)
	if err != nil {
		t.Fatalf("create retention rule: %v", err)
	}

	deleter := &mockEventDeleter{result: 0}

	cfg := &config.StorageConfig{
		HotPath:              t.TempDir(),
		ColdPath:             t.TempDir(),
		ColdRetentionDays:    30,
		CleanupIntervalHours: 24,
	}
	mgr := &Manager{
		cfg:              cfg,
		recRepo:          recording.NewRepository(database),
		retentionRepo:    retRepo,
		eventDeleter:     deleter,
		logger:           slog.Default(),
		resolvedHotPath:  cfg.HotPath,
		resolvedColdPath: cfg.ColdPath,
	}

	mgr.runEventCleanerOnce(context.Background())

	// Verify: should have a per-camera call for cam1 + "detection"
	var perCameraCall *deleteCall
	for i := range deleter.calls {
		c := &deleter.calls[i]
		if c.cameraID != nil && *c.cameraID == camID && c.eventType == "detection" {
			perCameraCall = c
			break
		}
	}
	if perCameraCall == nil {
		t.Fatal("expected a per-camera call for cam1 + detection")
	}
	// Per-camera calls should NOT have any exclude list
	if len(perCameraCall.excludeCameraIDs) != 0 {
		t.Errorf("per-camera call should have no excludeCameraIDs, got %v", perCameraCall.excludeCameraIDs)
	}
}

func TestEventCleaner_NoRules_UsesGlobalRetention(t *testing.T) {
	database := openEventCleanerTestDB(t)
	retRepo := NewRetentionRepository(database)
	deleter := &mockEventDeleter{result: 0}

	cfg := &config.StorageConfig{
		HotPath:              t.TempDir(),
		ColdPath:             t.TempDir(),
		ColdRetentionDays:    30,
		CleanupIntervalHours: 24,
	}
	mgr := &Manager{
		cfg:              cfg,
		recRepo:          recording.NewRepository(database),
		retentionRepo:    retRepo,
		eventDeleter:     deleter,
		logger:           slog.Default(),
		resolvedHotPath:  cfg.HotPath,
		resolvedColdPath: cfg.ColdPath,
	}

	mgr.runEventCleanerOnce(context.Background())

	// Should have one global call per known event type, all with nil cameraID and no exclusions
	globalCalls := 0
	for _, c := range deleter.calls {
		if c.cameraID == nil {
			globalCalls++
			if len(c.excludeCameraIDs) != 0 {
				t.Errorf("global call for %q should have no exclusions when no rules exist", c.eventType)
			}
		}
	}
	if globalCalls != len(KnownEventTypes) {
		t.Errorf("expected %d global calls (one per event type), got %d", len(KnownEventTypes), globalCalls)
	}
}

func strPtr(s string) *string { return &s }
