package recording

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/db"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

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

// newTestRepo creates a Repository backed by an in-memory database.
func newTestRepo(t *testing.T) (*Repository, *sql.DB) {
	t.Helper()
	database := openTestDB(t)
	return NewRepository(database), database
}

// mustCreate inserts a recording and fails the test on error.
func mustCreate(t *testing.T, repo *Repository, rec *Record) *Record {
	t.Helper()
	created, err := repo.Create(context.Background(), rec)
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	return created
}

// ─── NewRepository ───────────────────────────────────────────────────────────

func TestNewRepository(t *testing.T) {
	t.Parallel()
	database := openTestDB(t)
	repo := NewRepository(database)
	if repo == nil {
		t.Fatal("NewRepository returned nil")
	}
	if repo.db != database {
		t.Error("repository.db does not match provided database")
	}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestCreate_InsertsRecordAndReturnsIDAndCreatedAt(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "front-door")

	now := time.Now().UTC().Truncate(time.Second)
	endTime := now.Add(10 * time.Minute)
	rec := &Record{
		CameraID:   camID,
		CameraName: "front-door",
		Path:       "/recordings/front-door/2025-01-15/08/00.00.mp4",
		StartTime:  now,
		EndTime:    &endTime,
		DurationS:  600.0,
		SizeBytes:  1024000,
	}

	created := mustCreate(t, repo, rec)

	if created.ID == 0 {
		t.Error("expected non-zero ID after create")
	}
	if created.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt after create")
	}
	if created.CameraName != "front-door" {
		t.Errorf("CameraName = %q, want %q", created.CameraName, "front-door")
	}
	if created.Path != rec.Path {
		t.Errorf("Path = %q, want %q", created.Path, rec.Path)
	}
	if created.DurationS != 600.0 {
		t.Errorf("DurationS = %f, want 600.0", created.DurationS)
	}
	if created.SizeBytes != 1024000 {
		t.Errorf("SizeBytes = %d, want 1024000", created.SizeBytes)
	}
}

func TestCreate_DoesNotMutateInput(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "garage")

	now := time.Now().UTC().Truncate(time.Second)
	rec := &Record{
		CameraID:   camID,
		CameraName: "garage",
		Path:       "/recordings/garage/2025-01-15/08/10.00.mp4",
		StartTime:  now,
		SizeBytes:  512,
	}

	created := mustCreate(t, repo, rec)

	// The caller's rec should not have been mutated
	if rec.ID != 0 {
		t.Error("Create() mutated input record's ID")
	}
	if !rec.CreatedAt.IsZero() {
		t.Error("Create() mutated input record's CreatedAt")
	}
	// The returned copy should have the ID set
	if created.ID == 0 {
		t.Error("returned record should have non-zero ID")
	}
}

func TestCreate_NilEndTime(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "side-door")

	rec := &Record{
		CameraID:   camID,
		CameraName: "side-door",
		Path:       "/recordings/side-door/2025-01-15/09/00.00.mp4",
		StartTime:  time.Now().UTC().Truncate(time.Second),
		EndTime:    nil, // in-progress segment
		DurationS:  0,
		SizeBytes:  0,
	}

	created := mustCreate(t, repo, rec)

	if created.EndTime != nil {
		t.Error("expected nil EndTime for in-progress segment")
	}
}

func TestCreate_DuplicatePathFails(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "patio")

	rec := &Record{
		CameraID:   camID,
		CameraName: "patio",
		Path:       "/recordings/patio/2025-01-15/08/00.00.mp4",
		StartTime:  time.Now().UTC().Truncate(time.Second),
	}

	mustCreate(t, repo, rec)

	// Second insert with same path should fail (UNIQUE constraint)
	_, err := repo.Create(context.Background(), rec)
	if err == nil {
		t.Error("expected error inserting duplicate path, got nil")
	}
}

// ─── Get ─────────────────────────────────────────────────────────────────────

func TestGet_ReturnsExistingRecord(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "lobby")

	now := time.Now().UTC().Truncate(time.Second)
	endTime := now.Add(10 * time.Minute)
	rec := &Record{
		CameraID:   camID,
		CameraName: "lobby",
		Path:       "/recordings/lobby/2025-01-15/10/00.00.mp4",
		StartTime:  now,
		EndTime:    &endTime,
		DurationS:  600.0,
		SizeBytes:  2048000,
	}

	created := mustCreate(t, repo, rec)

	got, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}
	if got.CameraID != camID {
		t.Errorf("CameraID = %d, want %d", got.CameraID, camID)
	}
	if got.CameraName != "lobby" {
		t.Errorf("CameraName = %q, want %q", got.CameraName, "lobby")
	}
	if got.Path != rec.Path {
		t.Errorf("Path = %q, want %q", got.Path, rec.Path)
	}
	if got.DurationS != 600.0 {
		t.Errorf("DurationS = %f, want 600.0", got.DurationS)
	}
	if got.SizeBytes != 2048000 {
		t.Errorf("SizeBytes = %d, want 2048000", got.SizeBytes)
	}
	if got.EndTime == nil {
		t.Fatal("EndTime should not be nil")
	}
}

func TestGet_NilEndTime(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "hallway")

	rec := &Record{
		CameraID:   camID,
		CameraName: "hallway",
		Path:       "/recordings/hallway/2025-01-15/11/00.00.mp4",
		StartTime:  time.Now().UTC().Truncate(time.Second),
	}

	created := mustCreate(t, repo, rec)

	got, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.EndTime != nil {
		t.Error("expected nil EndTime for in-progress segment")
	}
}

func TestGet_NotFound(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	_, err := repo.Get(context.Background(), 99999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ─── Count ───────────────────────────────────────────────────────────────────

func TestCount_AllRecordings(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "cam1")

	for i := 0; i < 5; i++ {
		mustCreate(t, repo, &Record{
			CameraID:   camID,
			CameraName: "cam1",
			Path:       "/recordings/cam1/2025-01-15/08/" + time.Duration(i*10).String() + ".mp4",
			StartTime:  time.Now().UTC().Add(time.Duration(i*10) * time.Minute),
		})
	}

	count, err := repo.Count(context.Background(), "", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Count(): %v", err)
	}
	if count != 5 {
		t.Errorf("Count() = %d, want 5", count)
	}
}

func TestCount_FilterByCameraName(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	cam1ID := insertTestCamera(t, database, "alpha")
	cam2ID := insertTestCamera(t, database, "beta")

	for i := 0; i < 3; i++ {
		mustCreate(t, repo, &Record{
			CameraID:   cam1ID,
			CameraName: "alpha",
			Path:       "/recordings/alpha/" + string(rune('a'+i)) + ".mp4",
			StartTime:  time.Now().UTC(),
		})
	}
	for i := 0; i < 2; i++ {
		mustCreate(t, repo, &Record{
			CameraID:   cam2ID,
			CameraName: "beta",
			Path:       "/recordings/beta/" + string(rune('a'+i)) + ".mp4",
			StartTime:  time.Now().UTC(),
		})
	}

	count, err := repo.Count(context.Background(), "alpha", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Count(alpha): %v", err)
	}
	if count != 3 {
		t.Errorf("Count(alpha) = %d, want 3", count)
	}

	count, err = repo.Count(context.Background(), "beta", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Count(beta): %v", err)
	}
	if count != 2 {
		t.Errorf("Count(beta) = %d, want 2", count)
	}
}

func TestCount_FilterByTimeRange(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "cam-time")

	base := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		mustCreate(t, repo, &Record{
			CameraID:   camID,
			CameraName: "cam-time",
			Path:       "/recordings/cam-time/" + base.Add(time.Duration(i)*time.Hour).Format("150405") + ".mp4",
			StartTime:  base.Add(time.Duration(i) * time.Hour),
		})
	}

	// Filter: start_time >= base+2h AND start_time <= base+4h → 3 records (at 2h, 3h, 4h)
	start := base.Add(2 * time.Hour)
	end := base.Add(4 * time.Hour)
	count, err := repo.Count(context.Background(), "", start, end)
	if err != nil {
		t.Fatalf("Count(time range): %v", err)
	}
	if count != 3 {
		t.Errorf("Count(time range) = %d, want 3", count)
	}
}

func TestCount_EmptyResult(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	count, err := repo.Count(context.Background(), "nonexistent", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Count(): %v", err)
	}
	if count != 0 {
		t.Errorf("Count() = %d, want 0", count)
	}
}

// ─── List ────────────────────────────────────────────────────────────────────

func TestList_ReturnsRecordingsOrderedByStartTimeDesc(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "list-cam")

	base := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		endTime := base.Add(time.Duration(i)*10*time.Minute + 10*time.Minute)
		mustCreate(t, repo, &Record{
			CameraID:   camID,
			CameraName: "list-cam",
			Path:       "/recordings/list-cam/seg" + string(rune('0'+i)) + ".mp4",
			StartTime:  base.Add(time.Duration(i) * 10 * time.Minute),
			EndTime:    &endTime,
			DurationS:  600,
			SizeBytes:  1024,
		})
	}

	recs, err := repo.List(context.Background(), "list-cam", time.Time{}, time.Time{}, 10, 0)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(recs) != 5 {
		t.Fatalf("List() returned %d records, want 5", len(recs))
	}

	// Should be in descending order by start_time
	for i := 1; i < len(recs); i++ {
		if recs[i].StartTime.After(recs[i-1].StartTime) {
			t.Errorf("records not in descending order: [%d].StartTime=%v > [%d].StartTime=%v",
				i, recs[i].StartTime, i-1, recs[i-1].StartTime)
		}
	}
}

func TestList_Pagination(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "page-cam")

	base := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		mustCreate(t, repo, &Record{
			CameraID:   camID,
			CameraName: "page-cam",
			Path:       "/recordings/page-cam/p" + string(rune('a'+i)) + ".mp4",
			StartTime:  base.Add(time.Duration(i) * 10 * time.Minute),
		})
	}

	// Page 1: limit=3, offset=0
	page1, err := repo.List(context.Background(), "", time.Time{}, time.Time{}, 3, 0)
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	if len(page1) != 3 {
		t.Errorf("page1 len = %d, want 3", len(page1))
	}

	// Page 2: limit=3, offset=3
	page2, err := repo.List(context.Background(), "", time.Time{}, time.Time{}, 3, 3)
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2) != 3 {
		t.Errorf("page2 len = %d, want 3", len(page2))
	}

	// Pages should not overlap
	if page1[0].ID == page2[0].ID {
		t.Error("page1 and page2 have overlapping first records")
	}
}

func TestList_FilterByCameraName(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	cam1ID := insertTestCamera(t, database, "left")
	cam2ID := insertTestCamera(t, database, "right")

	mustCreate(t, repo, &Record{
		CameraID: cam1ID, CameraName: "left",
		Path: "/recordings/left/a.mp4", StartTime: time.Now().UTC(),
	})
	mustCreate(t, repo, &Record{
		CameraID: cam2ID, CameraName: "right",
		Path: "/recordings/right/a.mp4", StartTime: time.Now().UTC(),
	})

	recs, err := repo.List(context.Background(), "left", time.Time{}, time.Time{}, 10, 0)
	if err != nil {
		t.Fatalf("List(left): %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("List(left) returned %d, want 1", len(recs))
	}
	if recs[0].CameraName != "left" {
		t.Errorf("CameraName = %q, want %q", recs[0].CameraName, "left")
	}
}

func TestList_EmptyResultReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	recs, err := repo.List(context.Background(), "nonexistent", time.Time{}, time.Time{}, 10, 0)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if recs == nil {
		t.Error("List() returned nil, expected empty slice")
	}
	if len(recs) != 0 {
		t.Errorf("List() returned %d records, want 0", len(recs))
	}
}

func TestList_FilterByTimeRange(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "time-cam")

	base := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		mustCreate(t, repo, &Record{
			CameraID:   camID,
			CameraName: "time-cam",
			Path:       "/recordings/time-cam/d" + string(rune('0'+i)) + ".mp4",
			StartTime:  base.Add(time.Duration(i) * 24 * time.Hour),
		})
	}

	// Filter: day 1 to day 3 inclusive
	start := base.Add(1 * 24 * time.Hour)
	end := base.Add(3 * 24 * time.Hour)
	recs, err := repo.List(context.Background(), "", start, end, 10, 0)
	if err != nil {
		t.Fatalf("List(time): %v", err)
	}
	if len(recs) != 3 {
		t.Errorf("List(time range) = %d records, want 3", len(recs))
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestDelete_RemovesExistingRecord(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "del-cam")

	created := mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "del-cam",
		Path: "/recordings/del-cam/del.mp4", StartTime: time.Now().UTC(),
	})

	if err := repo.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("Delete(): %v", err)
	}

	// Get should return ErrNotFound
	_, err := repo.Get(context.Background(), created.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: expected ErrNotFound, got %v", err)
	}
}

func TestDelete_NotFoundReturnsError(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	err := repo.Delete(context.Background(), 99999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(nonexistent) expected ErrNotFound, got %v", err)
	}
}

// ─── DeleteByCameraName ──────────────────────────────────────────────────────

func TestDeleteByCameraName_RemovesAllForCamera(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "bulk-del")
	otherID := insertTestCamera(t, database, "keep-me")

	for i := 0; i < 4; i++ {
		mustCreate(t, repo, &Record{
			CameraID: camID, CameraName: "bulk-del",
			Path:      "/recordings/bulk-del/" + string(rune('a'+i)) + ".mp4",
			StartTime: time.Now().UTC(),
		})
	}
	mustCreate(t, repo, &Record{
		CameraID: otherID, CameraName: "keep-me",
		Path: "/recordings/keep-me/a.mp4", StartTime: time.Now().UTC(),
	})

	rows, err := repo.DeleteByCameraName(context.Background(), "bulk-del")
	if err != nil {
		t.Fatalf("DeleteByCameraName(): %v", err)
	}
	if rows != 4 {
		t.Errorf("rows affected = %d, want 4", rows)
	}

	// Other camera's recordings should be untouched
	count, err := repo.Count(context.Background(), "keep-me", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Count(keep-me): %v", err)
	}
	if count != 1 {
		t.Errorf("keep-me count = %d, want 1", count)
	}
}

func TestDeleteByCameraName_NoMatch(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	rows, err := repo.DeleteByCameraName(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("DeleteByCameraName(ghost): %v", err)
	}
	if rows != 0 {
		t.Errorf("rows affected = %d, want 0", rows)
	}
}

// ─── ListOlderThan ───────────────────────────────────────────────────────────

func TestListOlderThan_ReturnsCompletedRecordsOnly(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "old-cam")

	old := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := old.Add(10 * time.Minute)

	// Completed recording (has end_time)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "old-cam",
		Path: "/recordings/old-cam/completed.mp4", StartTime: old,
		EndTime: &endTime, DurationS: 600, SizeBytes: 1024,
	})

	// In-progress recording (no end_time) — should NOT be returned
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "old-cam",
		Path: "/recordings/old-cam/progress.mp4", StartTime: old,
	})

	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	recs, err := repo.ListOlderThan(context.Background(), cutoff, 100, 0)
	if err != nil {
		t.Fatalf("ListOlderThan(): %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("ListOlderThan returned %d, want 1 (only completed)", len(recs))
	}
	if recs[0].Path != "/recordings/old-cam/completed.mp4" {
		t.Errorf("Path = %q, want completed.mp4", recs[0].Path)
	}
}

func TestListOlderThan_CursorPagination(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "cursor-cam")

	old := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := old.Add(10 * time.Minute)

	var ids []int
	for i := 0; i < 5; i++ {
		created := mustCreate(t, repo, &Record{
			CameraID: camID, CameraName: "cursor-cam",
			Path:      "/recordings/cursor-cam/c" + string(rune('a'+i)) + ".mp4",
			StartTime: old.Add(time.Duration(i) * time.Minute),
			EndTime:   &endTime, DurationS: 600, SizeBytes: 512,
		})
		ids = append(ids, created.ID)
	}

	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// First batch: afterID=0, limit=2
	batch1, err := repo.ListOlderThan(context.Background(), cutoff, 2, 0)
	if err != nil {
		t.Fatalf("batch1: %v", err)
	}
	if len(batch1) != 2 {
		t.Fatalf("batch1 len = %d, want 2", len(batch1))
	}

	// Second batch: afterID=last ID from batch 1
	afterID := batch1[len(batch1)-1].ID
	batch2, err := repo.ListOlderThan(context.Background(), cutoff, 2, afterID)
	if err != nil {
		t.Fatalf("batch2: %v", err)
	}
	if len(batch2) != 2 {
		t.Fatalf("batch2 len = %d, want 2", len(batch2))
	}

	// No overlap between batches
	for _, r1 := range batch1 {
		for _, r2 := range batch2 {
			if r1.ID == r2.ID {
				t.Errorf("batches overlap on ID %d", r1.ID)
			}
		}
	}

	// Third batch: should get 1 remaining
	afterID = batch2[len(batch2)-1].ID
	batch3, err := repo.ListOlderThan(context.Background(), cutoff, 2, afterID)
	if err != nil {
		t.Fatalf("batch3: %v", err)
	}
	if len(batch3) != 1 {
		t.Errorf("batch3 len = %d, want 1", len(batch3))
	}
}

func TestListOlderThan_RespectsOrderByIDAsc(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "asc-cam")

	old := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := old.Add(10 * time.Minute)

	for i := 0; i < 5; i++ {
		mustCreate(t, repo, &Record{
			CameraID: camID, CameraName: "asc-cam",
			Path:      "/recordings/asc-cam/o" + string(rune('a'+i)) + ".mp4",
			StartTime: old.Add(time.Duration(i) * time.Minute),
			EndTime:   &endTime, DurationS: 600, SizeBytes: 256,
		})
	}

	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	recs, err := repo.ListOlderThan(context.Background(), cutoff, 100, 0)
	if err != nil {
		t.Fatalf("ListOlderThan(): %v", err)
	}
	for i := 1; i < len(recs); i++ {
		if recs[i].ID <= recs[i-1].ID {
			t.Errorf("records not in ascending ID order: [%d].ID=%d <= [%d].ID=%d",
				i, recs[i].ID, i-1, recs[i-1].ID)
		}
	}
}

func TestListOlderThan_SkipsRecentRecords(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "recent-cam")

	recent := time.Now().UTC()
	endTime := recent.Add(10 * time.Minute)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "recent-cam",
		Path: "/recordings/recent-cam/new.mp4", StartTime: recent,
		EndTime: &endTime, DurationS: 600, SizeBytes: 1024,
	})

	// Cutoff is 1 hour ago — the recent recording should NOT appear
	cutoff := time.Now().UTC().Add(-1 * time.Hour)
	recs, err := repo.ListOlderThan(context.Background(), cutoff, 100, 0)
	if err != nil {
		t.Fatalf("ListOlderThan(): %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("expected 0 records for recent-only data, got %d", len(recs))
	}
}

// ─── UpdatePath ──────────────────────────────────────────────────────────────

func TestUpdatePath_ChangesPath(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "migrate-cam")

	created := mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "migrate-cam",
		Path: "/hot/migrate-cam/a.mp4", StartTime: time.Now().UTC(),
	})

	newPath := "/cold/migrate-cam/a.mp4"
	if err := repo.UpdatePath(context.Background(), created.ID, newPath); err != nil {
		t.Fatalf("UpdatePath(): %v", err)
	}

	got, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.Path != newPath {
		t.Errorf("Path = %q, want %q", got.Path, newPath)
	}
}

func TestUpdatePath_NotFound(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	err := repo.UpdatePath(context.Background(), 99999, "/cold/ghost.mp4")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdatePath(nonexistent) expected ErrNotFound, got %v", err)
	}
}

// ─── StorageStats ────────────────────────────────────────────────────────────

func TestStorageStats_SeparatesHotAndCold(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "stats-cam")

	now := time.Now().UTC().Truncate(time.Second)
	// 3 hot recordings (1000 bytes each)
	for i := 0; i < 3; i++ {
		mustCreate(t, repo, &Record{
			CameraID: camID, CameraName: "stats-cam",
			Path:      "/media/hot/stats-cam/h" + string(rune('a'+i)) + ".mp4",
			StartTime: now.Add(time.Duration(i) * time.Minute),
			SizeBytes: 1000,
		})
	}
	// 2 cold recordings (2000 bytes each)
	for i := 0; i < 2; i++ {
		mustCreate(t, repo, &Record{
			CameraID: camID, CameraName: "stats-cam",
			Path:      "/media/cold/stats-cam/c" + string(rune('a'+i)) + ".mp4",
			StartTime: now.Add(time.Duration(i+10) * time.Minute),
			SizeBytes: 2000,
		})
	}

	hot, cold, err := repo.StorageStats(context.Background(), "/media/hot", "/media/cold")
	if err != nil {
		t.Fatalf("StorageStats(): %v", err)
	}

	if hot.SegmentCount != 3 {
		t.Errorf("hot.SegmentCount = %d, want 3", hot.SegmentCount)
	}
	if hot.UsedBytes != 3000 {
		t.Errorf("hot.UsedBytes = %d, want 3000", hot.UsedBytes)
	}
	if cold.SegmentCount != 2 {
		t.Errorf("cold.SegmentCount = %d, want 2", cold.SegmentCount)
	}
	if cold.UsedBytes != 4000 {
		t.Errorf("cold.UsedBytes = %d, want 4000", cold.UsedBytes)
	}
}

func TestStorageStats_EmptyColdPath(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "hot-only")

	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "hot-only",
		Path: "/media/hot/hot-only/a.mp4", StartTime: time.Now().UTC(), SizeBytes: 500,
	})

	hot, cold, err := repo.StorageStats(context.Background(), "/media/hot", "")
	if err != nil {
		t.Fatalf("StorageStats(): %v", err)
	}
	if hot.SegmentCount != 1 {
		t.Errorf("hot.SegmentCount = %d, want 1", hot.SegmentCount)
	}
	if cold.SegmentCount != 0 {
		t.Errorf("cold.SegmentCount = %d, want 0 (no cold configured)", cold.SegmentCount)
	}
	if cold.UsedBytes != 0 {
		t.Errorf("cold.UsedBytes = %d, want 0", cold.UsedBytes)
	}
}

func TestStorageStats_TrailingSlashNormalization(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "slash-cam")

	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "slash-cam",
		Path: "/data/hot/slash-cam/a.mp4", StartTime: time.Now().UTC(), SizeBytes: 100,
	})

	// Path without trailing slash — should still find the recording
	hot, _, err := repo.StorageStats(context.Background(), "/data/hot", "")
	if err != nil {
		t.Fatalf("StorageStats(): %v", err)
	}
	if hot.SegmentCount != 1 {
		t.Errorf("SegmentCount = %d, want 1 (trailing slash normalization)", hot.SegmentCount)
	}
}

func TestStorageStats_SiblingDirectoryNotMatched(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "sibling-cam")

	// Recording under /media/hot2/ should NOT match /media/hot
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "sibling-cam",
		Path: "/media/hot2/sibling-cam/a.mp4", StartTime: time.Now().UTC(), SizeBytes: 100,
	})

	hot, _, err := repo.StorageStats(context.Background(), "/media/hot", "")
	if err != nil {
		t.Fatalf("StorageStats(): %v", err)
	}
	if hot.SegmentCount != 0 {
		t.Errorf("SegmentCount = %d, want 0 — /media/hot2/ should not match /media/hot/", hot.SegmentCount)
	}
}

func TestStorageStats_EmptyDatabase(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	hot, cold, err := repo.StorageStats(context.Background(), "/media/hot", "/media/cold")
	if err != nil {
		t.Fatalf("StorageStats(): %v", err)
	}
	if hot.UsedBytes != 0 || hot.SegmentCount != 0 {
		t.Errorf("hot should be zero-value, got %+v", hot)
	}
	if cold.UsedBytes != 0 || cold.SegmentCount != 0 {
		t.Errorf("cold should be zero-value, got %+v", cold)
	}
}

func TestStorageStats_LIKESpecialCharsEscaped(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "special-cam")

	// Path with % and _ in the prefix — these must be escaped in the LIKE pattern
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "special-cam",
		Path: "/media/100%_hot/special-cam/a.mp4", StartTime: time.Now().UTC(), SizeBytes: 200,
	})

	hot, _, err := repo.StorageStats(context.Background(), "/media/100%_hot", "")
	if err != nil {
		t.Fatalf("StorageStats(): %v", err)
	}
	if hot.SegmentCount != 1 {
		t.Errorf("SegmentCount = %d, want 1 (LIKE special chars must be escaped)", hot.SegmentCount)
	}
}

// ─── ExistsForCameraAtTime ───────────────────────────────────────────────────

func TestExistsForCameraAtTime_TrueWhenSpanned(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "exists-cam")

	start := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 15, 8, 10, 0, 0, time.UTC)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "exists-cam",
		Path: "/recordings/exists-cam/span.mp4", StartTime: start,
		EndTime: &end, DurationS: 600, SizeBytes: 1024,
	})

	// Time in the middle of the segment
	queryTime := time.Date(2025, 1, 15, 8, 5, 0, 0, time.UTC)
	exists, err := repo.ExistsForCameraAtTime(context.Background(), camID, queryTime)
	if err != nil {
		t.Fatalf("ExistsForCameraAtTime(): %v", err)
	}
	if !exists {
		t.Error("expected true for time within segment span")
	}
}

func TestExistsForCameraAtTime_FalseOutsideSpan(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "outside-cam")

	start := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 15, 8, 10, 0, 0, time.UTC)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "outside-cam",
		Path: "/recordings/outside-cam/span.mp4", StartTime: start,
		EndTime: &end, DurationS: 600, SizeBytes: 1024,
	})

	// Time before the segment
	before := time.Date(2025, 1, 15, 7, 59, 0, 0, time.UTC)
	exists, err := repo.ExistsForCameraAtTime(context.Background(), camID, before)
	if err != nil {
		t.Fatalf("ExistsForCameraAtTime(before): %v", err)
	}
	if exists {
		t.Error("expected false for time before segment")
	}

	// Time after the segment
	after := time.Date(2025, 1, 15, 8, 11, 0, 0, time.UTC)
	exists, err = repo.ExistsForCameraAtTime(context.Background(), camID, after)
	if err != nil {
		t.Fatalf("ExistsForCameraAtTime(after): %v", err)
	}
	if exists {
		t.Error("expected false for time after segment")
	}
}

func TestExistsForCameraAtTime_FalseForInProgress(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "progress-cam")

	start := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	// No end_time — in-progress segment
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "progress-cam",
		Path: "/recordings/progress-cam/inprog.mp4", StartTime: start,
	})

	queryTime := time.Date(2025, 1, 15, 8, 5, 0, 0, time.UTC)
	exists, err := repo.ExistsForCameraAtTime(context.Background(), camID, queryTime)
	if err != nil {
		t.Fatalf("ExistsForCameraAtTime(): %v", err)
	}
	if exists {
		t.Error("expected false for in-progress segment (end_time IS NULL)")
	}
}

func TestExistsForCameraAtTime_FalseForDifferentCamera(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	cam1ID := insertTestCamera(t, database, "cam-a")
	cam2ID := insertTestCamera(t, database, "cam-b")

	start := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 15, 8, 10, 0, 0, time.UTC)
	mustCreate(t, repo, &Record{
		CameraID: cam1ID, CameraName: "cam-a",
		Path: "/recordings/cam-a/seg.mp4", StartTime: start,
		EndTime: &end, DurationS: 600, SizeBytes: 1024,
	})

	queryTime := time.Date(2025, 1, 15, 8, 5, 0, 0, time.UTC)
	exists, err := repo.ExistsForCameraAtTime(context.Background(), cam2ID, queryTime)
	if err != nil {
		t.Fatalf("ExistsForCameraAtTime(): %v", err)
	}
	if exists {
		t.Error("expected false for different camera_id")
	}
}

func TestExistsForCameraAtTime_BoundaryConditions(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "boundary-cam")

	start := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 15, 8, 10, 0, 0, time.UTC)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "boundary-cam",
		Path: "/recordings/boundary-cam/b.mp4", StartTime: start,
		EndTime: &end, DurationS: 600, SizeBytes: 1024,
	})

	tests := []struct {
		name     string
		queryAt  time.Time
		expected bool
	}{
		{"at start_time", start, true},           // start_time <= t
		{"at end_time", end, false},              // end_time > t (exclusive)
		{"1ns before end", end.Add(-1), true},    // just inside
		{"1ns after start", start.Add(1), true},  // just inside
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.ExistsForCameraAtTime(context.Background(), camID, tt.queryAt)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("ExistsForCameraAtTime(%v) = %v, want %v", tt.queryAt, got, tt.expected)
			}
		})
	}
}

// ─── TimelineForDay ──────────────────────────────────────────────────────────

func TestTimelineForDay_ReturnsSegmentsForDay(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "timeline-cam")

	day := time.Date(2025, 3, 10, 0, 0, 0, 0, time.Local)

	// 3 segments on the target day
	for i := 0; i < 3; i++ {
		start := day.Add(time.Duration(i) * time.Hour)
		end := start.Add(10 * time.Minute)
		mustCreate(t, repo, &Record{
			CameraID: camID, CameraName: "timeline-cam",
			Path:      "/recordings/timeline-cam/tl" + string(rune('a'+i)) + ".mp4",
			StartTime: start, EndTime: &end, DurationS: 600, SizeBytes: 1024,
		})
	}

	// 1 segment on a different day — should not appear
	otherDay := day.AddDate(0, 0, 1)
	otherEnd := otherDay.Add(10 * time.Minute)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "timeline-cam",
		Path: "/recordings/timeline-cam/other.mp4", StartTime: otherDay,
		EndTime: &otherEnd, DurationS: 600, SizeBytes: 1024,
	})

	segments, err := repo.TimelineForDay(context.Background(), "timeline-cam", day)
	if err != nil {
		t.Fatalf("TimelineForDay(): %v", err)
	}
	if len(segments) != 3 {
		t.Fatalf("len = %d, want 3", len(segments))
	}

	// Should be in ascending order
	for i := 1; i < len(segments); i++ {
		if segments[i].StartTime.Before(segments[i-1].StartTime) {
			t.Errorf("segments not in ascending order")
		}
	}
}

func TestTimelineForDay_ExcludesInProgress(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "inprog-tl")

	day := time.Date(2025, 3, 10, 8, 0, 0, 0, time.Local)

	// In-progress segment (no end_time)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "inprog-tl",
		Path: "/recordings/inprog-tl/a.mp4", StartTime: day,
	})

	segments, err := repo.TimelineForDay(context.Background(), "inprog-tl", day)
	if err != nil {
		t.Fatalf("TimelineForDay(): %v", err)
	}
	if len(segments) != 0 {
		t.Errorf("expected 0 segments (in-progress excluded), got %d", len(segments))
	}
}

func TestTimelineForDay_EmptyReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	day := time.Date(2025, 3, 10, 0, 0, 0, 0, time.Local)
	segments, err := repo.TimelineForDay(context.Background(), "nonexistent", day)
	if err != nil {
		t.Fatalf("TimelineForDay(): %v", err)
	}
	if segments == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(segments) != 0 {
		t.Errorf("len = %d, want 0", len(segments))
	}
}

func TestTimelineForDay_OmitsPath(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "nopath-cam")

	day := time.Date(2025, 3, 10, 8, 0, 0, 0, time.Local)
	end := day.Add(10 * time.Minute)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "nopath-cam",
		Path: "/recordings/nopath-cam/a.mp4", StartTime: day,
		EndTime: &end, DurationS: 600, SizeBytes: 1024,
	})

	segments, err := repo.TimelineForDay(context.Background(), "nopath-cam", day)
	if err != nil {
		t.Fatalf("TimelineForDay(): %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}

	// TimelineSegment struct does not have a Path field — this is enforced at compile time
	// by the struct definition. Just verify the returned data makes sense.
	if segments[0].ID == 0 {
		t.Error("segment ID should be non-zero")
	}
	if segments[0].DurationS != 600 {
		t.Errorf("DurationS = %f, want 600", segments[0].DurationS)
	}
}

func TestTimelineForDay_FiltersByCamera(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	cam1ID := insertTestCamera(t, database, "tl-left")
	cam2ID := insertTestCamera(t, database, "tl-right")

	day := time.Date(2025, 3, 10, 8, 0, 0, 0, time.Local)
	end := day.Add(10 * time.Minute)

	mustCreate(t, repo, &Record{
		CameraID: cam1ID, CameraName: "tl-left",
		Path: "/recordings/tl-left/a.mp4", StartTime: day,
		EndTime: &end, DurationS: 600, SizeBytes: 1024,
	})
	mustCreate(t, repo, &Record{
		CameraID: cam2ID, CameraName: "tl-right",
		Path: "/recordings/tl-right/a.mp4", StartTime: day,
		EndTime: &end, DurationS: 600, SizeBytes: 1024,
	})

	segments, err := repo.TimelineForDay(context.Background(), "tl-left", day)
	if err != nil {
		t.Fatalf("TimelineForDay(): %v", err)
	}
	if len(segments) != 1 {
		t.Errorf("expected 1 segment for tl-left, got %d", len(segments))
	}
}

// ─── DaysWithRecordings ──────────────────────────────────────────────────────

func TestDaysWithRecordings_ReturnsDedupedDays(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "days-cam")

	// Multiple segments on the same day
	day := time.Date(2025, 3, 10, 0, 0, 0, 0, time.Local)
	for i := 0; i < 3; i++ {
		start := day.Add(time.Duration(i) * time.Hour)
		end := start.Add(10 * time.Minute)
		mustCreate(t, repo, &Record{
			CameraID: camID, CameraName: "days-cam",
			Path:      "/recordings/days-cam/d" + string(rune('a'+i)) + ".mp4",
			StartTime: start, EndTime: &end, DurationS: 600, SizeBytes: 1024,
		})
	}

	// One segment on a different day in the same month
	day2 := time.Date(2025, 3, 15, 12, 0, 0, 0, time.Local)
	end2 := day2.Add(10 * time.Minute)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "days-cam",
		Path: "/recordings/days-cam/d2.mp4", StartTime: day2,
		EndTime: &end2, DurationS: 600, SizeBytes: 1024,
	})

	days, err := repo.DaysWithRecordings(context.Background(), "days-cam", 2025, time.March)
	if err != nil {
		t.Fatalf("DaysWithRecordings(): %v", err)
	}
	if len(days) != 2 {
		t.Fatalf("expected 2 unique days, got %d: %v", len(days), days)
	}

	// Verify format is YYYY-MM-DD
	for _, d := range days {
		if len(d) != 10 {
			t.Errorf("day %q is not YYYY-MM-DD format", d)
		}
	}
}

func TestDaysWithRecordings_ExcludesInProgress(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "inprog-days")

	day := time.Date(2025, 3, 10, 8, 0, 0, 0, time.Local)
	// In-progress segment
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "inprog-days",
		Path: "/recordings/inprog-days/a.mp4", StartTime: day,
	})

	days, err := repo.DaysWithRecordings(context.Background(), "inprog-days", 2025, time.March)
	if err != nil {
		t.Fatalf("DaysWithRecordings(): %v", err)
	}
	if len(days) != 0 {
		t.Errorf("expected 0 days for in-progress only, got %d", len(days))
	}
}

func TestDaysWithRecordings_EmptyReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	days, err := repo.DaysWithRecordings(context.Background(), "nonexistent", 2025, time.March)
	if err != nil {
		t.Fatalf("DaysWithRecordings(): %v", err)
	}
	if days == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(days) != 0 {
		t.Errorf("len = %d, want 0", len(days))
	}
}

func TestDaysWithRecordings_FiltersByMonth(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "month-cam")

	// March recording
	march := time.Date(2025, 3, 15, 8, 0, 0, 0, time.Local)
	marchEnd := march.Add(10 * time.Minute)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "month-cam",
		Path: "/recordings/month-cam/march.mp4", StartTime: march,
		EndTime: &marchEnd, DurationS: 600, SizeBytes: 1024,
	})

	// April recording
	april := time.Date(2025, 4, 10, 8, 0, 0, 0, time.Local)
	aprilEnd := april.Add(10 * time.Minute)
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "month-cam",
		Path: "/recordings/month-cam/april.mp4", StartTime: april,
		EndTime: &aprilEnd, DurationS: 600, SizeBytes: 1024,
	})

	// Query March only
	days, err := repo.DaysWithRecordings(context.Background(), "month-cam", 2025, time.March)
	if err != nil {
		t.Fatalf("DaysWithRecordings(): %v", err)
	}
	if len(days) != 1 {
		t.Fatalf("expected 1 day in March, got %d: %v", len(days), days)
	}
	if days[0] != "2025-03-15" {
		t.Errorf("day = %q, want %q", days[0], "2025-03-15")
	}
}

func TestDaysWithRecordings_FiltersByCamera(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	cam1ID := insertTestCamera(t, database, "d-left")
	cam2ID := insertTestCamera(t, database, "d-right")

	day := time.Date(2025, 3, 10, 8, 0, 0, 0, time.Local)
	end := day.Add(10 * time.Minute)

	mustCreate(t, repo, &Record{
		CameraID: cam1ID, CameraName: "d-left",
		Path: "/recordings/d-left/a.mp4", StartTime: day,
		EndTime: &end, DurationS: 600, SizeBytes: 1024,
	})
	mustCreate(t, repo, &Record{
		CameraID: cam2ID, CameraName: "d-right",
		Path: "/recordings/d-right/a.mp4", StartTime: day,
		EndTime: &end, DurationS: 600, SizeBytes: 1024,
	})

	days, err := repo.DaysWithRecordings(context.Background(), "d-left", 2025, time.March)
	if err != nil {
		t.Fatalf("DaysWithRecordings(): %v", err)
	}
	if len(days) != 1 {
		t.Errorf("expected 1 day for d-left, got %d", len(days))
	}
}

// ─── Record struct ───────────────────────────────────────────────────────────

func TestRecord_ZeroEndTimeIsNil(t *testing.T) {
	t.Parallel()

	rec := Record{
		ID:        1,
		CameraID:  1,
		Path:      "/test/path.mp4",
		StartTime: time.Now(),
	}

	if rec.EndTime != nil {
		t.Error("zero-value EndTime should be nil (pointer)")
	}
}

// ─── ErrNotFound sentinel ────────────────────────────────────────────────────

func TestErrNotFound_IsCheckable(t *testing.T) {
	t.Parallel()

	err := ErrNotFound
	if !errors.Is(err, ErrNotFound) {
		t.Error("ErrNotFound should pass errors.Is check")
	}

	wrapped := errors.New("wrapper: " + err.Error())
	if errors.Is(wrapped, ErrNotFound) {
		// This is expected — simple string wrapping won't match
	}
}

// ─── StorageSummary ──────────────────────────────────────────────────────────

func TestStorageSummary_ZeroValue(t *testing.T) {
	t.Parallel()

	var s StorageSummary
	if s.UsedBytes != 0 || s.SegmentCount != 0 {
		t.Errorf("zero-value StorageSummary should be all zeros, got %+v", s)
	}
}

// ─── Integration: Create → Get round-trip ────────────────────────────────────

func TestCreateGetRoundTrip(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "roundtrip")

	now := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	endTime := time.Date(2025, 1, 15, 8, 10, 0, 0, time.UTC)

	input := &Record{
		CameraID:   camID,
		CameraName: "roundtrip",
		Path:       "/recordings/roundtrip/2025-01-15/08/00.00.mp4",
		StartTime:  now,
		EndTime:    &endTime,
		DurationS:  600.0,
		SizeBytes:  2048000,
	}

	created := mustCreate(t, repo, input)

	got, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}

	// Verify all fields survive the round-trip
	if got.CameraID != camID {
		t.Errorf("CameraID = %d, want %d", got.CameraID, camID)
	}
	if got.CameraName != "roundtrip" {
		t.Errorf("CameraName = %q, want %q", got.CameraName, "roundtrip")
	}
	if got.Path != input.Path {
		t.Errorf("Path = %q, want %q", got.Path, input.Path)
	}
	if !got.StartTime.Equal(now) {
		t.Errorf("StartTime = %v, want %v", got.StartTime, now)
	}
	if got.EndTime == nil {
		t.Fatal("EndTime should not be nil")
	}
	if !got.EndTime.Equal(endTime) {
		t.Errorf("EndTime = %v, want %v", *got.EndTime, endTime)
	}
	if got.DurationS != 600.0 {
		t.Errorf("DurationS = %f, want 600.0", got.DurationS)
	}
	if got.SizeBytes != 2048000 {
		t.Errorf("SizeBytes = %d, want 2048000", got.SizeBytes)
	}
}

// ─── Integration: Create → Delete → Get ──────────────────────────────────────

func TestCreateDeleteGetLifecycle(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "lifecycle")

	created := mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "lifecycle",
		Path: "/recordings/lifecycle/a.mp4", StartTime: time.Now().UTC(),
	})

	// Delete
	if err := repo.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("Delete(): %v", err)
	}

	// Get should return ErrNotFound
	_, err := repo.Get(context.Background(), created.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: expected ErrNotFound, got %v", err)
	}

	// Count should be 0
	count, err := repo.Count(context.Background(), "lifecycle", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Count(): %v", err)
	}
	if count != 0 {
		t.Errorf("Count after Delete = %d, want 0", count)
	}
}

// ─── Integration: Create → UpdatePath → Get ──────────────────────────────────

func TestCreateUpdatePathGetLifecycle(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "upd-path")

	created := mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "upd-path",
		Path: "/hot/upd-path/a.mp4", StartTime: time.Now().UTC(), SizeBytes: 999,
	})

	newPath := "/cold/upd-path/a.mp4"
	if err := repo.UpdatePath(context.Background(), created.ID, newPath); err != nil {
		t.Fatalf("UpdatePath(): %v", err)
	}

	got, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.Path != newPath {
		t.Errorf("Path = %q, want %q", got.Path, newPath)
	}
	// Other fields should be unchanged
	if got.SizeBytes != 999 {
		t.Errorf("SizeBytes = %d, want 999 (should be unchanged)", got.SizeBytes)
	}
}

// ─── ON DELETE CASCADE ───────────────────────────────────────────────────────

func TestCascadeDelete_RemovesRecordingsWhenCameraDeleted(t *testing.T) {
	t.Parallel()
	repo, database := newTestRepo(t)
	camID := insertTestCamera(t, database, "cascade-cam")

	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "cascade-cam",
		Path: "/recordings/cascade-cam/a.mp4", StartTime: time.Now().UTC(),
	})
	mustCreate(t, repo, &Record{
		CameraID: camID, CameraName: "cascade-cam",
		Path: "/recordings/cascade-cam/b.mp4", StartTime: time.Now().UTC(),
	})

	// Verify 2 recordings exist
	count, _ := repo.Count(context.Background(), "cascade-cam", time.Time{}, time.Time{})
	if count != 2 {
		t.Fatalf("expected 2 recordings before cascade delete, got %d", count)
	}

	// Delete the camera — should cascade to recordings
	_, err := database.Exec(`DELETE FROM cameras WHERE id = ?`, camID)
	if err != nil {
		t.Fatalf("deleting camera: %v", err)
	}

	count, _ = repo.Count(context.Background(), "cascade-cam", time.Time{}, time.Time{})
	if count != 0 {
		t.Errorf("expected 0 recordings after cascade delete, got %d", count)
	}
}

// ─── Context cancellation ────────────────────────────────────────────────────

func TestGet_CancelledContext(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := repo.Get(ctx, 1)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func TestList_CancelledContext(t *testing.T) {
	t.Parallel()
	repo, _ := newTestRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.List(ctx, "", time.Time{}, time.Time{}, 10, 0)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}
