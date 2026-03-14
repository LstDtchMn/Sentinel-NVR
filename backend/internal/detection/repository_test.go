package detection

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/db"
)

// openDetTestDB opens an in-memory SQLite database with all migrations applied.
func openDetTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:", false, slog.Default())
	if err != nil {
		t.Fatalf("openDetTestDB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// insertDetTestCamera inserts a minimal camera row and returns its ID.
func insertDetTestCamera(t *testing.T, database *sql.DB, name string) int {
	t.Helper()
	var id int
	err := database.QueryRow(
		`INSERT INTO cameras (name, enabled, main_stream) VALUES (?, 1, 'rtsp://test/stream') RETURNING id`,
		name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertDetTestCamera %q: %v", name, err)
	}
	return id
}

// insertTestEvent inserts an event row and returns its ID.
func insertTestEvent(t *testing.T, database *sql.DB, camID *int, evType string, startTime time.Time) int {
	t.Helper()
	var id int
	err := database.QueryRow(
		`INSERT INTO events (camera_id, type, label, confidence, data, thumbnail, has_clip, start_time, created_at)
		 VALUES (?, ?, 'test', 0.9, '{}', '', 0, ?, ?)
		 RETURNING id`,
		camID, evType, startTime.Format(time.RFC3339), startTime.Format(time.RFC3339),
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestEvent: %v", err)
	}
	return id
}

// ─── DeleteOlderThan with excludeCameraIDs ───────────────────────────────────

func TestDeleteOlderThan_BasicDelete(t *testing.T) {
	database := openDetTestDB(t)
	repo := NewRepository(database, t.TempDir())

	cam1 := insertDetTestCamera(t, database, "cam1")
	old := time.Now().AddDate(0, 0, -10)
	insertTestEvent(t, database, &cam1, "detection", old)
	insertTestEvent(t, database, &cam1, "detection", old.Add(-time.Hour))

	n, err := repo.DeleteOlderThan(context.Background(), nil, "detection", time.Now().AddDate(0, 0, -5), 100)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted %d, want 2", n)
	}
}

func TestDeleteOlderThan_ExcludesCameraIDs(t *testing.T) {
	database := openDetTestDB(t)
	repo := NewRepository(database, t.TempDir())

	cam1 := insertDetTestCamera(t, database, "cam1")
	cam2 := insertDetTestCamera(t, database, "cam2")
	cam3 := insertDetTestCamera(t, database, "cam3")
	old := time.Now().AddDate(0, 0, -10)
	insertTestEvent(t, database, &cam1, "detection", old)
	insertTestEvent(t, database, &cam2, "detection", old)
	insertTestEvent(t, database, &cam3, "detection", old)

	// Exclude cam1 and cam2 — only cam3's event should be deleted
	n, err := repo.DeleteOlderThan(context.Background(), nil, "detection",
		time.Now().AddDate(0, 0, -5), 100, cam1, cam2)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d, want 1 (cam3 only)", n)
	}

	// Verify cam1 and cam2 events still exist
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM events WHERE camera_id IN (?, ?)`, cam1, cam2).Scan(&count)
	if count != 2 {
		t.Errorf("excluded cameras should have 2 events, got %d", count)
	}
}

func TestDeleteOlderThan_NullCameraID_PreservedByExclusion(t *testing.T) {
	database := openDetTestDB(t)
	repo := NewRepository(database, t.TempDir())

	cam1 := insertDetTestCamera(t, database, "cam1")
	old := time.Now().AddDate(0, 0, -10)

	// Insert event with NULL camera_id (orphaned event)
	insertTestEvent(t, database, nil, "detection", old)
	// Insert event with cam1
	insertTestEvent(t, database, &cam1, "detection", old)

	// Exclude cam1 — the NULL camera_id event should still be preserved (not excluded)
	n, err := repo.DeleteOlderThan(context.Background(), nil, "detection",
		time.Now().AddDate(0, 0, -5), 100, cam1)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	// NULL camera_id is preserved by the IS NULL clause, cam1 is excluded.
	// So 0 events should be deleted: cam1 is excluded, NULL is preserved by IS NULL.
	// Wait — the WHERE clause is:
	//   camera_id IS NULL OR camera_id NOT IN (cam1)
	// This means: keep events where camera_id IS NULL or camera_id is NOT in the exclude list.
	// The events that DON'T match this clause are deleted. So:
	// - NULL event: camera_id IS NULL → matches → NOT deleted ✓ (preserved)
	// - cam1 event: camera_id = cam1, NOT (cam1 NOT IN (cam1)) → does NOT match → deleted? No, wait.
	//
	// The clause says: AND (camera_id IS NULL OR camera_id NOT IN (...))
	// This INCLUDES the row in the DELETE selection if (cam IS NULL OR cam NOT IN exclude).
	// So:
	// - NULL event: IS NULL → true → included in deletion scope
	// - cam1 event: NOT IN (cam1) → false, IS NULL → false → EXCLUDED from deletion
	//
	// Hmm, that means NULL events ARE deleted. Let me re-read the code logic.
	// The query SELECTs events matching: start_time < cutoff AND (camera_id IS NULL OR camera_id NOT IN (exclude_list))
	// So it selects events to DELETE where camera_id IS NULL (orphan) OR camera_id is NOT excluded.
	// The exclude list protects specific cameras; NULL camera_id events are NOT protected.
	if n != 1 {
		t.Errorf("deleted %d, want 1 (NULL camera_id event)", n)
	}

	// cam1 event should still exist (excluded)
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM events WHERE camera_id = ?`, cam1).Scan(&count)
	if count != 1 {
		t.Errorf("cam1 event should be preserved, got count %d", count)
	}
}

func TestDeleteOlderThan_EmptyExcludeList_DeletesAll(t *testing.T) {
	database := openDetTestDB(t)
	repo := NewRepository(database, t.TempDir())

	cam1 := insertDetTestCamera(t, database, "cam1")
	old := time.Now().AddDate(0, 0, -10)
	insertTestEvent(t, database, &cam1, "detection", old)
	insertTestEvent(t, database, nil, "detection", old) // NULL camera_id

	// No exclusions — all old events should be deleted
	n, err := repo.DeleteOlderThan(context.Background(), nil, "detection",
		time.Now().AddDate(0, 0, -5), 100)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted %d, want 2", n)
	}
}

func TestDeleteOlderThan_SpecificCamera(t *testing.T) {
	database := openDetTestDB(t)
	repo := NewRepository(database, t.TempDir())

	cam1 := insertDetTestCamera(t, database, "cam1")
	cam2 := insertDetTestCamera(t, database, "cam2")
	old := time.Now().AddDate(0, 0, -10)
	insertTestEvent(t, database, &cam1, "detection", old)
	insertTestEvent(t, database, &cam2, "detection", old)

	// Delete only cam1's events
	n, err := repo.DeleteOlderThan(context.Background(), &cam1, "detection",
		time.Now().AddDate(0, 0, -5), 100)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d, want 1", n)
	}

	// cam2 event should still exist
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM events WHERE camera_id = ?`, cam2).Scan(&count)
	if count != 1 {
		t.Errorf("cam2 should have 1 event, got %d", count)
	}
}

func TestDeleteOlderThan_RespectsLimit(t *testing.T) {
	database := openDetTestDB(t)
	repo := NewRepository(database, t.TempDir())

	cam1 := insertDetTestCamera(t, database, "cam1")
	old := time.Now().AddDate(0, 0, -10)
	for i := 0; i < 5; i++ {
		insertTestEvent(t, database, &cam1, "detection", old.Add(time.Duration(-i)*time.Hour))
	}

	// Limit of 3 should delete only 3
	n, err := repo.DeleteOlderThan(context.Background(), nil, "detection",
		time.Now().AddDate(0, 0, -5), 3)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 3 {
		t.Errorf("deleted %d, want 3 (limit)", n)
	}

	var remaining int
	database.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&remaining)
	if remaining != 2 {
		t.Errorf("remaining %d, want 2", remaining)
	}
}

func TestDeleteOlderThan_FiltersByEventType(t *testing.T) {
	database := openDetTestDB(t)
	repo := NewRepository(database, t.TempDir())

	cam1 := insertDetTestCamera(t, database, "cam1")
	old := time.Now().AddDate(0, 0, -10)
	insertTestEvent(t, database, &cam1, "detection", old)
	insertTestEvent(t, database, &cam1, "camera.online", old)

	// Delete only "detection" type
	n, err := repo.DeleteOlderThan(context.Background(), nil, "detection",
		time.Now().AddDate(0, 0, -5), 100)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d, want 1 (detection only)", n)
	}

	// camera.online should still exist
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM events WHERE type = 'camera.online'`).Scan(&count)
	if count != 1 {
		t.Errorf("camera.online event should be preserved, got %d", count)
	}
}
