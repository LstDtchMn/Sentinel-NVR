// Package recording provides database CRUD operations for MP4 recording segments.
// Each segment is a fixed-duration (default 10 min) independently playable MP4 file
// written by ffmpeg's segment muxer (CG4).
package recording

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("recording not found")

// Record represents a single MP4 recording segment stored on disk.
type Record struct {
	ID         int        `json:"id"`
	CameraID   int        `json:"camera_id"`
	CameraName string     `json:"camera_name"`
	Path       string     `json:"path"`
	StartTime  time.Time  `json:"start_time"`
	EndTime    *time.Time `json:"end_time"`    // nil while segment is still being written
	DurationS  float64    `json:"duration_s"`
	SizeBytes  int64      `json:"size_bytes"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Repository provides CRUD access to the recordings table.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a recording repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// parseSQLiteTime parses a timestamp string from SQLite (CURRENT_TIMESTAMP or driver-formatted).
// modernc.org/sqlite stores time.Time as text and does not auto-parse on scan.
//
// Layout coverage:
//   - "2006-01-02 15:04:05"         — SQLite CURRENT_TIMESTAMP (always UTC)
//   - "2006-01-02T15:04:05Z"        — ISO8601 UTC with Z suffix
//   - "2006-01-02T15:04:05"         — ISO8601 without timezone (treat as UTC)
//   - time.RFC3339 / RFC3339Nano    — driver-formatted time.Time with offset (e.g. +05:30)
func parseSQLiteTime(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse timestamp %q", s)
}

// Create inserts a new recording segment with all available metadata.
// For completed segments (reported by ffmpeg's segment_list), all fields are available.
// For in-progress segments, end_time can be nil and duration_s/size_bytes zero.
func (r *Repository) Create(ctx context.Context, rec *Record) (*Record, error) {
	var createdStr string
	row := r.db.QueryRowContext(ctx,
		`INSERT INTO recordings (camera_id, camera_name, path, start_time, end_time, duration_s, size_bytes)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 RETURNING id, created_at`,
		rec.CameraID, rec.CameraName, rec.Path, rec.StartTime,
		rec.EndTime, rec.DurationS, rec.SizeBytes,
	)
	// Return a copy — never mutate the caller's input pointer. This matches
	// the convention in camera.Repository.Create().
	var id int
	if err := row.Scan(&id, &createdStr); err != nil {
		return nil, fmt.Errorf("inserting recording: %w", err)
	}
	createdAt, err := parseSQLiteTime(createdStr)
	if err != nil {
		return nil, fmt.Errorf("inserting recording: invalid created_at %q: %w", createdStr, err)
	}
	result := *rec
	result.ID = id
	result.CreatedAt = createdAt
	return &result, nil
}

// Get returns a single recording by ID.
func (r *Repository) Get(ctx context.Context, id int) (*Record, error) {
	rec := &Record{}
	var startStr, createdStr string
	var endStr *string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, camera_id, camera_name, path, start_time, end_time, duration_s, size_bytes, created_at
		 FROM recordings WHERE id = ?`, id,
	).Scan(&rec.ID, &rec.CameraID, &rec.CameraName, &rec.Path, &startStr,
		&endStr, &rec.DurationS, &rec.SizeBytes, &createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting recording: %w", err)
	}
	startTime, err := parseSQLiteTime(startStr)
	if err != nil {
		return nil, fmt.Errorf("getting recording %d: invalid start_time %q: %w", id, startStr, err)
	}
	rec.StartTime = startTime
	createdAt, err := parseSQLiteTime(createdStr)
	if err != nil {
		return nil, fmt.Errorf("getting recording %d: invalid created_at %q: %w", id, createdStr, err)
	}
	rec.CreatedAt = createdAt
	if endStr != nil {
		t, err := parseSQLiteTime(*endStr)
		if err != nil {
			return nil, fmt.Errorf("getting recording %d: invalid end_time %q: %w", id, *endStr, err)
		}
		rec.EndTime = &t
	}
	return rec, nil
}

// List returns recording segments with optional filtering and pagination.
// Pass empty cameraName to list across all cameras.
// Pass zero-value times to skip time filtering.
func (r *Repository) List(ctx context.Context, cameraName string, start, end time.Time, limit, offset int) ([]Record, error) {
	query := `SELECT id, camera_id, camera_name, path, start_time, end_time, duration_s, size_bytes, created_at
		 FROM recordings WHERE 1=1`
	args := []any{}

	if cameraName != "" {
		query += " AND camera_name = ?"
		args = append(args, cameraName)
	}
	if !start.IsZero() {
		query += " AND start_time >= ?"
		args = append(args, start)
	}
	if !end.IsZero() {
		query += " AND start_time <= ?"
		args = append(args, end)
	}

	query += " ORDER BY start_time DESC"
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing recordings: %w", err)
	}
	defer rows.Close()

	recordings := []Record{}
	for rows.Next() {
		var rec Record
		var startStr, createdStr string
		var endStr *string
		if err := rows.Scan(&rec.ID, &rec.CameraID, &rec.CameraName, &rec.Path, &startStr,
			&endStr, &rec.DurationS, &rec.SizeBytes, &createdStr); err != nil {
			return nil, fmt.Errorf("scanning recording: %w", err)
		}
		startTime, err := parseSQLiteTime(startStr)
		if err != nil {
			return nil, fmt.Errorf("scanning recording: invalid start_time %q: %w", startStr, err)
		}
		rec.StartTime = startTime
		createdAt, err := parseSQLiteTime(createdStr)
		if err != nil {
			return nil, fmt.Errorf("scanning recording: invalid created_at %q: %w", createdStr, err)
		}
		rec.CreatedAt = createdAt
		if endStr != nil {
			t, err := parseSQLiteTime(*endStr)
			if err != nil {
				return nil, fmt.Errorf("scanning recording: invalid end_time %q: %w", *endStr, err)
			}
			rec.EndTime = &t
		}
		recordings = append(recordings, rec)
	}
	return recordings, rows.Err()
}

// Delete removes a recording record by ID.
// The caller should delete the DB record first, then the file from disk.
// A leaked file is recoverable by a maintenance job; a dangling DB row is not.
func (r *Repository) Delete(ctx context.Context, id int) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM recordings WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting recording: %w", err)
	}
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("checking rows affected: %w", rowsErr)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByCameraName removes all recording records for a camera.
// Called when a camera is deleted. The caller handles file cleanup.
func (r *Repository) DeleteByCameraName(ctx context.Context, name string) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM recordings WHERE camera_name = ?`, name)
	if err != nil {
		return 0, fmt.Errorf("deleting recordings for camera %q: %w", name, err)
	}
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return 0, fmt.Errorf("checking rows affected: %w", rowsErr)
	}
	return rows, nil
}

// ExistsForCameraAtTime reports whether a completed recording segment for cameraID spans time t
// (start_time <= t < end_time). Used by persistEvents to set has_clip on detection events (Phase 6).
//
// Only checks completed segments (end_time IS NOT NULL). The recorder inserts rows only when ffmpeg
// finalizes a segment, so in-progress segments are never in the DB. has_clip for detections that
// fire during an active recording is set retroactively in persistEvents when the covering
// recording.segment_complete event arrives.
func (r *Repository) ExistsForCameraAtTime(ctx context.Context, cameraID int, t time.Time) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM recordings
		   WHERE camera_id = ?
		     AND start_time <= ?
		     AND end_time > ?
		     AND end_time IS NOT NULL
		   LIMIT 1
		 )`,
		cameraID, t, t,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking recording for camera %d at %v: %w", cameraID, t, err)
	}
	return exists, nil
}

// Count returns the total number of recording segments.
func (r *Repository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recordings`).Scan(&count)
	return count, err
}

// TimelineSegment is a recording segment projected for timeline rendering (R6).
// Omits path for security — the frontend uses /recordings/:id/play for playback.
type TimelineSegment struct {
	ID        int       `json:"id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	DurationS float64   `json:"duration_s"`
}

// TimelineForDay returns all completed recording segments for a camera on a given
// date, sorted chronologically. Designed for rendering the 24h timeline scrubber (R6).
// Excludes in-progress segments (end_time IS NULL) and omits the path field.
// The date parameter should be any time within the target day — it is truncated to
// midnight internally. At most 144 segments per day (24h × 6/hr at 10-min each).
func (r *Repository) TimelineForDay(ctx context.Context, cameraName string, date time.Time) ([]TimelineSegment, error) {
	// Use time.Date with time.Local to get local-calendar midnight, not UTC epoch
	// midnight. date.Truncate(24h) truncates to the UTC epoch which is wrong for
	// any server not in UTC — e.g. UTC+5 would shift the day window by 5 hours.
	// Recordings are stored in local time (time.ParseInLocation), so boundaries must match.
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)
	dayEnd := dayStart.Add(24 * time.Hour)

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, start_time, end_time, duration_s
		 FROM recordings
		 WHERE camera_name = ?
		   AND start_time >= ?
		   AND start_time < ?
		   AND end_time IS NOT NULL
		 ORDER BY start_time ASC`,
		cameraName, dayStart, dayEnd,
	)
	if err != nil {
		return nil, fmt.Errorf("querying timeline: %w", err)
	}
	defer rows.Close()

	var segments []TimelineSegment
	for rows.Next() {
		var seg TimelineSegment
		var startStr, endStr string
		if err := rows.Scan(&seg.ID, &startStr, &endStr, &seg.DurationS); err != nil {
			return nil, fmt.Errorf("scanning timeline segment: %w", err)
		}
		startTime, err := parseSQLiteTime(startStr)
		if err != nil {
			return nil, fmt.Errorf("scanning timeline segment: invalid start_time %q: %w", startStr, err)
		}
		seg.StartTime = startTime
		endTime, err := parseSQLiteTime(endStr)
		if err != nil {
			return nil, fmt.Errorf("scanning timeline segment: invalid end_time %q: %w", endStr, err)
		}
		seg.EndTime = endTime
		segments = append(segments, seg)
	}
	if segments == nil {
		segments = []TimelineSegment{}
	}
	return segments, rows.Err()
}

// DaysWithRecordings returns date strings (YYYY-MM-DD) on which at least one
// completed recording exists for the given camera and month. Used by the date
// picker to highlight available dates (R6).
func (r *Repository) DaysWithRecordings(ctx context.Context, cameraName string, year int, month time.Month) ([]string, error) {
	// Use time.Local (not time.UTC) so the month boundary aligns with the server's
	// local calendar — recordings are stored in local time via time.ParseInLocation.
	monthStart := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	monthEnd := monthStart.AddDate(0, 1, 0)

	// Select start_time strings directly rather than using SQLite's DATE() function.
	// DATE(start_time) converts timezone-aware RFC3339 timestamps to UTC before
	// extracting the date, which produces wrong calendar dates for servers not in UTC
	// (e.g. a recording at 23:30 UTC-5 would return the next day's date). We extract
	// the local-calendar date in Go using the time's own Location after parsing.
	rows, err := r.db.QueryContext(ctx,
		`SELECT start_time
		 FROM recordings
		 WHERE camera_name = ?
		   AND start_time >= ?
		   AND start_time < ?
		   AND end_time IS NOT NULL
		 ORDER BY start_time ASC`,
		cameraName, monthStart, monthEnd,
	)
	if err != nil {
		return nil, fmt.Errorf("querying recording days: %w", err)
	}
	defer rows.Close()

	seen := map[string]struct{}{}
	var days []string
	for rows.Next() {
		var startStr string
		if err := rows.Scan(&startStr); err != nil {
			return nil, fmt.Errorf("scanning recording day: %w", err)
		}
		t, err := parseSQLiteTime(startStr)
		if err != nil {
			return nil, fmt.Errorf("scanning recording day: invalid start_time %q: %w", startStr, err)
		}
		// Format using the time's own Location so the date matches the server's local
		// calendar rather than UTC — avoids off-by-one date shifts for non-UTC servers.
		day := t.In(time.Local).Format("2006-01-02")
		if _, ok := seen[day]; !ok {
			seen[day] = struct{}{}
			days = append(days, day)
		}
	}
	if days == nil {
		days = []string{}
	}
	return days, rows.Err()
}
