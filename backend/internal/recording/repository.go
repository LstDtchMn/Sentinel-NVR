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

// Create inserts a new recording segment with all available metadata.
// For completed segments (reported by ffmpeg's segment_list), all fields are available.
// For in-progress segments, end_time can be nil and duration_s/size_bytes zero.
func (r *Repository) Create(ctx context.Context, rec *Record) (*Record, error) {
	row := r.db.QueryRowContext(ctx,
		`INSERT INTO recordings (camera_id, camera_name, path, start_time, end_time, duration_s, size_bytes)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 RETURNING id, created_at`,
		rec.CameraID, rec.CameraName, rec.Path, rec.StartTime,
		rec.EndTime, rec.DurationS, rec.SizeBytes,
	)
	if err := row.Scan(&rec.ID, &rec.CreatedAt); err != nil {
		return nil, fmt.Errorf("inserting recording: %w", err)
	}
	return rec, nil
}

// Get returns a single recording by ID.
func (r *Repository) Get(ctx context.Context, id int) (*Record, error) {
	rec := &Record{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, camera_id, camera_name, path, start_time, end_time, duration_s, size_bytes, created_at
		 FROM recordings WHERE id = ?`, id,
	).Scan(&rec.ID, &rec.CameraID, &rec.CameraName, &rec.Path, &rec.StartTime,
		&rec.EndTime, &rec.DurationS, &rec.SizeBytes, &rec.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting recording: %w", err)
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

	if limit <= 0 {
		limit = 50
	}
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing recordings: %w", err)
	}
	defer rows.Close()

	var recordings []Record
	for rows.Next() {
		var rec Record
		if err := rows.Scan(&rec.ID, &rec.CameraID, &rec.CameraName, &rec.Path, &rec.StartTime,
			&rec.EndTime, &rec.DurationS, &rec.SizeBytes, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning recording: %w", err)
		}
		recordings = append(recordings, rec)
	}
	return recordings, rows.Err()
}

// Delete removes a recording record by ID. The caller is responsible for deleting
// the actual file from disk before calling this.
func (r *Repository) Delete(ctx context.Context, id int) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM recordings WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting recording: %w", err)
	}
	rows, _ := result.RowsAffected()
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
	rows, _ := result.RowsAffected()
	return rows, nil
}

// Count returns the total number of recording segments.
func (r *Repository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recordings`).Scan(&count)
	return count, err
}
