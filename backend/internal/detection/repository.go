package detection

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// EventRecord represents a single row from the events table.
// The Data field is returned as a raw JSON string (array of DetectedObject or "{}").
type EventRecord struct {
	ID         int        `json:"id"`
	CameraID   *int       `json:"camera_id"`   // null if camera was deleted
	Type       string     `json:"type"`
	Label      string     `json:"label"`
	Confidence float64    `json:"confidence"`
	Data       string     `json:"data"`       // raw JSON — callers unmarshal as needed
	Thumbnail  string     `json:"thumbnail"`
	HasClip    bool       `json:"has_clip"`
	StartTime  time.Time  `json:"start_time"`
	EndTime    *time.Time `json:"end_time"`
	CreatedAt  time.Time  `json:"created_at"`
}

// HeatmapBucket is a 5-minute time bucket of detection event density for timeline overlay (Phase 6, R6).
type HeatmapBucket struct {
	BucketStart    time.Time `json:"bucket_start"`    // start of the 5-minute window (server local time)
	DetectionCount int       `json:"detection_count"` // number of detection events in this bucket
}

// ListFilter specifies optional filters for Repository.List.
// Zero values are ignored (no filter applied for that field).
type ListFilter struct {
	CameraID *int   // filter by camera
	Type     string // e.g. "detection", "camera.connected"
	Date     string // "YYYY-MM-DD" in server local time
	Limit    int    // 1–500; defaults to 50 if zero
	Offset   int
}

// Repository provides CRUD access to the events table.
type Repository struct {
	db *sql.DB
}

// NewRepository creates an events repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// List returns events matching the filter in reverse chronological order,
// along with the total count of matching events (for pagination).
func (r *Repository) List(ctx context.Context, f ListFilter) ([]EventRecord, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 500 {
		f.Limit = 500
	}

	where, args := buildWhere(f)

	// Count query shares the same WHERE clause (no LIMIT/OFFSET).
	var total int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM events"+where, args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting events: %w", err)
	}

	// Data query — append LIMIT and OFFSET after the shared args.
	dataArgs := append(args, f.Limit, f.Offset) //nolint:gocritic
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, camera_id, type, label, confidence, data, thumbnail, has_clip,
		        start_time, end_time, created_at
		 FROM events`+where+` ORDER BY start_time DESC LIMIT ? OFFSET ?`,
		dataArgs...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("listing events: %w", err)
	}
	defer rows.Close()

	events := []EventRecord{}
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning event row: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating event rows: %w", err)
	}
	return events, total, nil
}

// GetByID returns a single event by ID or ErrNotFound if it does not exist.
func (r *Repository) GetByID(ctx context.Context, id int) (*EventRecord, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, camera_id, type, label, confidence, data, thumbnail, has_clip,
		        start_time, end_time, created_at
		 FROM events WHERE id = ?`, id,
	)
	ev, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting event %d: %w", id, err)
	}
	return &ev, nil
}

// Delete removes an event by ID. Returns ErrNotFound if no row was deleted.
// The caller is responsible for removing any associated thumbnail file.
func (r *Repository) Delete(ctx context.Context, id int) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM events WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting event %d: %w", id, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for event %d: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// buildWhere constructs the WHERE clause and positional args from a ListFilter.
// Returns a string that starts with a space (e.g. " WHERE camera_id = ?") or
// an empty string when no conditions apply.
func buildWhere(f ListFilter) (string, []any) {
	var conds []string
	var args []any

	if f.CameraID != nil {
		conds = append(conds, "camera_id = ?")
		args = append(args, *f.CameraID)
	}
	if f.Type != "" {
		conds = append(conds, "type = ?")
		args = append(args, f.Type)
	}
	if f.Date != "" {
		// Parse date in server local time — events are stored in server local time
		// (persistEvents writes time.Time values which modernc/sqlite stores as RFC3339).
		day, err := time.ParseInLocation("2006-01-02", f.Date, time.Local)
		if err == nil {
			start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
			end := start.Add(24 * time.Hour)
			conds = append(conds, "start_time >= ?")
			args = append(args, start)
			conds = append(conds, "start_time < ?")
			args = append(args, end)
		}
		// Silently ignore an unparseable date — callers should validate before calling List.
	}

	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// scanner is implemented by both *sql.Row and *sql.Rows so scanEvent can serve both.
type scanner interface {
	Scan(dest ...any) error
}

// scanEvent reads one row into an EventRecord.
// Returns sql.ErrNoRows (unwrapped) so callers can detect not-found cleanly.
func scanEvent(s scanner) (EventRecord, error) {
	var ev EventRecord
	var camID *int64
	var hasClipInt int64
	var startStr, createdStr string
	var endStr *string

	if err := s.Scan(
		&ev.ID, &camID, &ev.Type, &ev.Label, &ev.Confidence,
		&ev.Data, &ev.Thumbnail, &hasClipInt,
		&startStr, &endStr, &createdStr,
	); err != nil {
		return EventRecord{}, err
	}

	if camID != nil {
		id := int(*camID)
		ev.CameraID = &id
	}
	ev.HasClip = hasClipInt != 0

	startTime, err := parseSQLiteTime(startStr)
	if err != nil {
		return EventRecord{}, fmt.Errorf("invalid start_time %q: %w", startStr, err)
	}
	ev.StartTime = startTime

	createdAt, err := parseSQLiteTime(createdStr)
	if err != nil {
		return EventRecord{}, fmt.Errorf("invalid created_at %q: %w", createdStr, err)
	}
	ev.CreatedAt = createdAt

	if endStr != nil {
		t, err := parseSQLiteTime(*endStr)
		if err != nil {
			return EventRecord{}, fmt.Errorf("invalid end_time %q: %w", *endStr, err)
		}
		ev.EndTime = &t
	}

	return ev, nil
}

// GetHeatmap returns detection event density in 5-minute buckets for a camera on a given date (Phase 6, R6).
// Returns an empty (non-nil) slice when no detections exist for the day.
// The date parameter's calendar day (server local time) is used — time components are ignored.
// Events are bucketed in Go rather than via SQLite strftime to avoid timezone offset handling
// inconsistencies when RFC3339 timestamps with local offsets are stored by the modernc driver.
func (r *Repository) GetHeatmap(ctx context.Context, cameraID int, date time.Time) ([]HeatmapBucket, error) {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)
	dayEnd := dayStart.Add(24 * time.Hour)

	rows, err := r.db.QueryContext(ctx,
		`SELECT start_time
		 FROM events
		 WHERE camera_id = ?
		   AND type = 'detection'
		   AND start_time >= ?
		   AND start_time < ?
		 ORDER BY start_time ASC`,
		cameraID, dayStart, dayEnd,
	)
	if err != nil {
		return nil, fmt.Errorf("querying detection heatmap for camera %d: %w", cameraID, err)
	}
	defer rows.Close()

	// Bucket by 5-minute windows. 24h / 5min = 288 max buckets.
	// buckets[idx] = count where idx = floor(minutes_since_midnight / 5).
	buckets := make(map[int]int)
	for rows.Next() {
		var startStr string
		if err := rows.Scan(&startStr); err != nil {
			return nil, fmt.Errorf("scanning heatmap row: %w", err)
		}
		t, err := parseSQLiteTime(startStr)
		if err != nil {
			return nil, fmt.Errorf("parsing heatmap start_time %q: %w", startStr, err)
		}
		elapsed := t.Sub(dayStart)
		if elapsed < 0 {
			elapsed = 0 // clamp — shouldn't occur with the WHERE clause above
		}
		// Floor to 5-minute boundary; cap at 287 (last bucket of the day).
		// Use integer arithmetic to avoid float64 precision drift that could
		// misassign events near a 5-minute boundary.
		idx := int(elapsed / (5 * time.Minute))
		if idx > 287 {
			idx = 287
		}
		buckets[idx]++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating heatmap rows: %w", err)
	}

	// Convert map to sorted slice — only non-empty buckets.
	result := make([]HeatmapBucket, 0, len(buckets))
	for idx, count := range buckets {
		result = append(result, HeatmapBucket{
			BucketStart:    dayStart.Add(time.Duration(idx) * 5 * time.Minute),
			DetectionCount: count,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].BucketStart.Before(result[j].BucketStart)
	})
	return result, nil
}

// parseSQLiteTime parses a timestamp string from SQLite.
// Duplicated from recording.parseSQLiteTime — both packages use the same
// modernc/sqlite driver which stores time.Time as text in various layouts.
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
