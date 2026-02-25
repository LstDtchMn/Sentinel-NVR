package storage

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/db"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
)

// openTestDB opens an in-memory SQLite database with all migrations applied.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:", false, slog.Default())
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// insertTestCamera inserts a minimal camera row and returns its ID.
func insertTestCamera(t *testing.T, database *sql.DB, name string) int {
	t.Helper()
	var id int
	err := database.QueryRow(
		`INSERT INTO cameras (name, enabled, main_stream) VALUES (?, 1, 'rtsp://test/stream') RETURNING id`,
		name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestCamera %q: %v", name, err)
	}
	return id
}

// insertTestRecording creates a dummy file on disk and inserts a recordings row.
func insertTestRecording(t *testing.T, database *sql.DB, camID int, camName, path string, startTime time.Time) int {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for recording: %v", err)
	}
	if err := os.WriteFile(path, []byte("dummy mp4"), 0o644); err != nil {
		t.Fatalf("write dummy file: %v", err)
	}
	var id int
	err := database.QueryRow(
		`INSERT INTO recordings (camera_id, camera_name, path, start_time, duration_s, size_bytes)
		 VALUES (?, ?, ?, ?, 600, 1024) RETURNING id`,
		camID, camName, path, startTime.UTC().Format("2006-01-02T15:04:05Z"),
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestRecording: %v", err)
	}
	return id
}

// ─── moveFile ────────────────────────────────────────────────────────────────

func TestMoveFile_SameFilesystem(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")
	dst := filepath.Join(dir, "dst.mp4")
	content := []byte("video content 12345")

	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source file should be gone after move")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading moved file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("moved content = %q, want %q", got, content)
	}
}

func TestMoveFile_SourceNotFound(t *testing.T) {
	dir := t.TempDir()
	err := moveFile(filepath.Join(dir, "nonexistent.mp4"), filepath.Join(dir, "dst.mp4"))
	if err == nil {
		t.Error("expected error moving nonexistent src, got nil")
	}
}

func TestMoveFile_DestinationInNewDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")
	dst := filepath.Join(dir, "subdir", "dst.mp4")
	content := []byte("nested file content")

	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	// Create subdirectory (moveFile relies on caller to have created parent dirs)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile to new dir: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading moved file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch after move")
	}
}

// ─── copyFile ────────────────────────────────────────────────────────────────

func TestCopyFile_CopiesContent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")
	dst := filepath.Join(dir, "dst.mp4")
	content := []byte("fake h264 data ABCDEF")

	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("copied content = %q, want %q", got, content)
	}
	// Source still exists
	if _, err := os.Stat(src); err != nil {
		t.Error("source should still exist after copy")
	}
}

func TestCopyFile_RefusesExistingDst(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")
	dst := filepath.Join(dir, "dst.mp4")

	if err := os.WriteFile(src, []byte("src"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	// copyFile uses O_EXCL — must fail if dst exists
	if err := copyFile(src, dst); err == nil {
		t.Error("expected error when dst already exists, got nil")
	}
	// dst must not be modified
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, []byte("existing")) {
		t.Error("existing dst content was modified unexpectedly")
	}
}

func TestCopyFile_CleansUpPartialOnSrcError(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.mp4")

	// Source doesn't exist — copyFile should return error and not leave a partial dst.
	if err := copyFile(filepath.Join(dir, "nope.mp4"), dst); err == nil {
		t.Fatal("expected error for missing src, got nil")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("partial dst should have been cleaned up on error")
	}
}

// ─── Manager.Start ───────────────────────────────────────────────────────────

func TestManagerStart_CreatesDirs(t *testing.T) {
	hot := filepath.Join(t.TempDir(), "hot")
	cold := filepath.Join(t.TempDir(), "cold")

	cfg := &config.StorageConfig{
		HotPath:           hot,
		ColdPath:          cold,
		HotRetentionDays:  1,
		ColdRetentionDays: 30,
		SegmentDuration:   10,
	}
	recRepo := recording.NewRepository(openTestDB(t))
	mgr := NewManager(cfg, recRepo, nil, nil, slog.Default())

	if err := mgr.Start(); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	defer mgr.Stop()

	for _, d := range []string{hot, cold} {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("directory %q not created: %v", d, err)
		} else if !info.IsDir() {
			t.Errorf("%q is not a directory", d)
		}
	}
}

// ─── Migrator integration tests ──────────────────────────────────────────────

func TestMigrator_MovesExpiredRecordings(t *testing.T) {
	database := openTestDB(t)
	camID := insertTestCamera(t, database, "front-door")

	hot := t.TempDir()
	cold := t.TempDir()

	// Recording older than hot_retention_days (3 days ago, threshold is 1 day)
	oldTime := time.Now().UTC().AddDate(0, 0, -3)
	recPath := filepath.Join(hot, "old.mp4")
	recID := insertTestRecording(t, database, camID, "front-door", recPath, oldTime)

	cfg := &config.StorageConfig{
		HotPath:          hot,
		ColdPath:         cold,
		HotRetentionDays: 1,
	}
	mgr := &Manager{
		cfg:              cfg,
		recRepo:          recording.NewRepository(database),
		logger:           slog.Default(),
		resolvedHotPath:  hot,
		resolvedColdPath: cold,
	}

	mgr.runMigratorOnce(context.Background())

	expectedColdPath := filepath.Join(cold, "old.mp4")
	if _, err := os.Stat(expectedColdPath); os.IsNotExist(err) {
		t.Errorf("expected file at cold path %q after migration", expectedColdPath)
	}
	if _, err := os.Stat(recPath); !os.IsNotExist(err) {
		t.Errorf("hot file %q should be gone after migration", recPath)
	}

	// Verify the DB path was updated to the cold location
	var dbPath string
	if err := database.QueryRow(`SELECT path FROM recordings WHERE id = ?`, recID).Scan(&dbPath); err != nil {
		t.Fatalf("querying updated path: %v", err)
	}
	if dbPath != expectedColdPath {
		t.Errorf("DB path = %q, want %q", dbPath, expectedColdPath)
	}
}

func TestMigrator_SkipsRecentRecordings(t *testing.T) {
	database := openTestDB(t)
	camID := insertTestCamera(t, database, "garage")

	hot := t.TempDir()
	cold := t.TempDir()

	// Only 1 hour old — should NOT be migrated when threshold is 1 day
	recentTime := time.Now().UTC().Add(-1 * time.Hour)
	recPath := filepath.Join(hot, "recent.mp4")
	insertTestRecording(t, database, camID, "garage", recPath, recentTime)

	cfg := &config.StorageConfig{
		HotPath:          hot,
		ColdPath:         cold,
		HotRetentionDays: 1,
	}
	mgr := &Manager{
		cfg:              cfg,
		recRepo:          recording.NewRepository(database),
		logger:           slog.Default(),
		resolvedHotPath:  hot,
		resolvedColdPath: cold,
	}
	mgr.runMigratorOnce(context.Background())

	if _, err := os.Stat(recPath); os.IsNotExist(err) {
		t.Errorf("recent recording %q should not have been migrated", recPath)
	}
}

func TestMigrator_NoColdPath_SkipsMigration(t *testing.T) {
	database := openTestDB(t)
	camID := insertTestCamera(t, database, "side-gate")

	hot := t.TempDir()
	oldTime := time.Now().UTC().AddDate(0, 0, -5)
	recPath := filepath.Join(hot, "old.mp4")
	insertTestRecording(t, database, camID, "side-gate", recPath, oldTime)

	cfg := &config.StorageConfig{
		HotPath:          hot,
		ColdPath:         "", // no cold storage configured
		HotRetentionDays: 1,
	}
	mgr := &Manager{
		cfg:             cfg,
		recRepo:         recording.NewRepository(database),
		logger:          slog.Default(),
		resolvedHotPath: hot,
	}
	mgr.runMigratorOnce(context.Background())

	// File must remain in hot storage since there's nowhere to migrate to
	if _, err := os.Stat(recPath); os.IsNotExist(err) {
		t.Errorf("file %q should remain in hot storage when cold path is not configured", recPath)
	}
}

func TestMigrator_PathEscape_IsSkipped(t *testing.T) {
	database := openTestDB(t)
	camID := insertTestCamera(t, database, "roof")

	hot := t.TempDir()
	cold := t.TempDir()
	outside := t.TempDir() // NOT under hot — path escapes storage root

	oldTime := time.Now().UTC().AddDate(0, 0, -5)
	escapedPath := filepath.Join(outside, "escape.mp4")
	insertTestRecording(t, database, camID, "roof", escapedPath, oldTime)

	cfg := &config.StorageConfig{
		HotPath:          hot,
		ColdPath:         cold,
		HotRetentionDays: 1,
	}
	mgr := &Manager{
		cfg:              cfg,
		recRepo:          recording.NewRepository(database),
		logger:           slog.Default(),
		resolvedHotPath:  hot,
		resolvedColdPath: cold,
	}
	mgr.runMigratorOnce(context.Background())

	// File outside hot root must NOT be touched
	if _, err := os.Stat(escapedPath); os.IsNotExist(err) {
		t.Errorf("file %q outside hot root must NOT be moved by migrator", escapedPath)
	}
}

// ─── Cleaner integration tests ───────────────────────────────────────────────

func TestCleaner_DeletesExpiredRecordings(t *testing.T) {
	database := openTestDB(t)
	camID := insertTestCamera(t, database, "back-yard")

	cold := t.TempDir()

	expiredTime := time.Now().UTC().AddDate(0, 0, -40) // 40 days old; threshold is 30
	recPath := filepath.Join(cold, "expired.mp4")
	recID := insertTestRecording(t, database, camID, "back-yard", recPath, expiredTime)

	cfg := &config.StorageConfig{
		HotPath:           t.TempDir(),
		ColdPath:          cold,
		ColdRetentionDays: 30,
	}
	mgr := &Manager{
		cfg:              cfg,
		recRepo:          recording.NewRepository(database),
		logger:           slog.Default(),
		resolvedHotPath:  cfg.HotPath,
		resolvedColdPath: cold,
	}
	mgr.runCleanerOnce(context.Background())

	if _, err := os.Stat(recPath); !os.IsNotExist(err) {
		t.Errorf("expired file %q should have been deleted", recPath)
	}
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM recordings WHERE id = ?`, recID).Scan(&count)
	if count != 0 {
		t.Errorf("expired DB row should be deleted, got count = %d", count)
	}
}

func TestCleaner_KeepsRecentRecordings(t *testing.T) {
	database := openTestDB(t)
	camID := insertTestCamera(t, database, "driveway")

	cold := t.TempDir()
	recentTime := time.Now().UTC().AddDate(0, 0, -5) // 5 days old; threshold is 30
	recPath := filepath.Join(cold, "recent.mp4")
	insertTestRecording(t, database, camID, "driveway", recPath, recentTime)

	cfg := &config.StorageConfig{
		HotPath:           t.TempDir(),
		ColdPath:          cold,
		ColdRetentionDays: 30,
	}
	mgr := &Manager{
		cfg:              cfg,
		recRepo:          recording.NewRepository(database),
		logger:           slog.Default(),
		resolvedHotPath:  cfg.HotPath,
		resolvedColdPath: cold,
	}
	mgr.runCleanerOnce(context.Background())

	if _, err := os.Stat(recPath); os.IsNotExist(err) {
		t.Errorf("recent file %q should NOT have been deleted", recPath)
	}
}

func TestCleaner_RefusesPathOutsideRoots(t *testing.T) {
	database := openTestDB(t)
	camID := insertTestCamera(t, database, "garage-roof")

	hot := t.TempDir()
	cold := t.TempDir()
	outside := t.TempDir() // NOT under hot or cold roots

	expiredTime := time.Now().UTC().AddDate(0, 0, -40)
	escapedPath := filepath.Join(outside, "protected.mp4")
	recID := insertTestRecording(t, database, camID, "garage-roof", escapedPath, expiredTime)

	cfg := &config.StorageConfig{
		HotPath:           hot,
		ColdPath:          cold,
		ColdRetentionDays: 30,
	}
	mgr := &Manager{
		cfg:              cfg,
		recRepo:          recording.NewRepository(database),
		logger:           slog.Default(),
		resolvedHotPath:  hot,
		resolvedColdPath: cold,
	}
	mgr.runCleanerOnce(context.Background())

	// File outside roots must NOT be deleted
	if _, err := os.Stat(escapedPath); os.IsNotExist(err) {
		t.Errorf("file %q outside storage roots should NOT have been deleted", escapedPath)
	}
	// DB row must also NOT be deleted (cleaner refuses to process out-of-root paths)
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM recordings WHERE id = ?`, recID).Scan(&count)
	if count != 1 {
		t.Errorf("DB row for out-of-root recording should NOT be deleted, count = %d", count)
	}
}

func TestCleaner_MissingFileIsOK(t *testing.T) {
	// A recording whose file has been manually deleted should not cause the cleaner to fail.
	// The DB row must still be cleaned up.
	database := openTestDB(t)
	camID := insertTestCamera(t, database, "front-porch")

	cold := t.TempDir()
	expiredTime := time.Now().UTC().AddDate(0, 0, -40)

	// Insert DB row pointing to a nonexistent file
	nonexistentPath := filepath.Join(cold, "already-gone.mp4")
	var recID int
	err := database.QueryRow(
		`INSERT INTO recordings (camera_id, camera_name, path, start_time, duration_s, size_bytes)
		 VALUES (?, ?, ?, ?, 600, 1024) RETURNING id`,
		camID, "front-porch", nonexistentPath,
		expiredTime.UTC().Format("2006-01-02T15:04:05Z"),
	).Scan(&recID)
	if err != nil {
		t.Fatalf("inserting orphan recording row: %v", err)
	}

	cfg := &config.StorageConfig{
		HotPath:           t.TempDir(),
		ColdPath:          cold,
		ColdRetentionDays: 30,
	}
	mgr := &Manager{
		cfg:              cfg,
		recRepo:          recording.NewRepository(database),
		logger:           slog.Default(),
		resolvedHotPath:  cfg.HotPath,
		resolvedColdPath: cold,
	}
	// Must not panic
	mgr.runCleanerOnce(context.Background())

	// DB row should still be cleaned up even if the file was missing
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM recordings WHERE id = ?`, recID).Scan(&count)
	if count != 0 {
		t.Errorf("orphan DB row should be deleted even when file is missing, count = %d", count)
	}
}
