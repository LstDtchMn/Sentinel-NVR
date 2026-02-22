package camera

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

// Sentinel errors for camera repository operations.
var (
	ErrNotFound  = errors.New("camera not found")
	ErrDuplicate = errors.New("camera name already exists")
)

// CameraRecord represents a row in the cameras table.
type CameraRecord struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Enabled    bool      `json:"enabled"`
	MainStream string    `json:"main_stream"`
	SubStream  string    `json:"sub_stream"`
	Record     bool      `json:"record"`
	Detect     bool      `json:"detect"`
	ONVIFHost  string    `json:"onvif_host,omitempty"`
	ONVIFPort  int       `json:"onvif_port,omitempty"`
	ONVIFUser  string    `json:"onvif_user,omitempty"`
	ONVIFPass  string    `json:"-"` // never expose in API responses
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Repository provides CRUD operations for cameras in SQLite.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a camera repository backed by the given database.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// List returns all cameras ordered by name.
func (r *Repository) List(ctx context.Context) ([]CameraRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, enabled, main_stream, sub_stream, record, detect,
		       onvif_host, onvif_port, onvif_user, onvif_pass, created_at, updated_at
		FROM cameras ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listing cameras: %w", err)
	}
	defer rows.Close()

	var cameras []CameraRecord
	for rows.Next() {
		cam, err := scanCamera(rows)
		if err != nil {
			return nil, err
		}
		cameras = append(cameras, cam)
	}
	return cameras, rows.Err()
}

// GetByName returns a single camera by its unique name.
func (r *Repository) GetByName(ctx context.Context, name string) (*CameraRecord, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, enabled, main_stream, sub_stream, record, detect,
		       onvif_host, onvif_port, onvif_user, onvif_pass, created_at, updated_at
		FROM cameras WHERE name = ?`, name)

	cam, err := scanCameraRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting camera %q: %w", name, err)
	}
	return &cam, nil
}

// Create inserts a new camera and returns the created record with ID and timestamps.
func (r *Repository) Create(ctx context.Context, cam *CameraRecord) (*CameraRecord, error) {
	var id int
	var createdAt, updatedAt time.Time

	err := r.db.QueryRowContext(ctx, `
		INSERT INTO cameras (name, enabled, main_stream, sub_stream, record, detect,
		                     onvif_host, onvif_port, onvif_user, onvif_pass)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at`,
		cam.Name, cam.Enabled, cam.MainStream, cam.SubStream, cam.Record, cam.Detect,
		cam.ONVIFHost, cam.ONVIFPort, cam.ONVIFUser, cam.ONVIFPass,
	).Scan(&id, &createdAt, &updatedAt)

	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("creating camera %q: %w", cam.Name, err)
	}

	// Return a copy — never mutate the caller's input pointer.
	result := *cam
	result.ID = id
	result.CreatedAt = createdAt
	result.UpdatedAt = updatedAt
	return &result, nil
}

// Update modifies an existing camera by name and returns the updated record.
func (r *Repository) Update(ctx context.Context, name string, cam *CameraRecord) (*CameraRecord, error) {
	var id int
	var createdAt, updatedAt time.Time

	err := r.db.QueryRowContext(ctx, `
		UPDATE cameras
		SET enabled = ?, main_stream = ?, sub_stream = ?, record = ?, detect = ?,
		    onvif_host = ?, onvif_port = ?, onvif_user = ?, onvif_pass = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE name = ?
		RETURNING id, created_at, updated_at`,
		cam.Enabled, cam.MainStream, cam.SubStream, cam.Record, cam.Detect,
		cam.ONVIFHost, cam.ONVIFPort, cam.ONVIFUser, cam.ONVIFPass,
		name,
	).Scan(&id, &createdAt, &updatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("updating camera %q: %w", name, err)
	}

	// Return a copy — never mutate the caller's input pointer.
	result := *cam
	result.ID = id
	result.Name = name
	result.CreatedAt = createdAt
	result.UpdatedAt = updatedAt
	return &result, nil
}

// Delete removes a camera by name.
func (r *Repository) Delete(ctx context.Context, name string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM cameras WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("deleting camera %q: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking delete result: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Count returns the total number of cameras in the database.
func (r *Repository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cameras`).Scan(&count)
	return count, err
}

// SeedFromConfig inserts cameras from the YAML config into the database.
// Only runs if the cameras table is empty (first-run migration from YAML → DB).
// The check and inserts are wrapped in a single transaction so a partial failure
// rolls back cleanly and the next restart can retry.
func (r *Repository) SeedFromConfig(ctx context.Context, cameras []config.CameraConfig) error {
	if len(cameras) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting seed transaction: %w", err)
	}
	defer tx.Rollback() // no-op after commit

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cameras`).Scan(&count); err != nil {
		return fmt.Errorf("checking camera count for seed: %w", err)
	}
	if count > 0 {
		return nil // DB already has cameras, skip seeding
	}

	for _, cam := range cameras {
		// Validate each camera from the YAML config the same way the API does,
		// so invalid names or stream URLs don't silently enter the database.
		rec := &CameraRecord{
			Name:       cam.Name,
			MainStream: cam.MainStream,
		}
		if err := ValidateCameraInput(rec); err != nil {
			return fmt.Errorf("seeding camera %q: invalid config: %w", cam.Name, err)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO cameras (name, enabled, main_stream, sub_stream, record, detect,
			                     onvif_host, onvif_port, onvif_user, onvif_pass)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			cam.Name, cam.Enabled, cam.MainStream, cam.SubStream, cam.Record, cam.Detect,
			cam.ONVIF.Host, cam.ONVIF.Port, cam.ONVIF.User, cam.ONVIF.Password,
		)
		if err != nil {
			return fmt.Errorf("seeding camera %q: %w", cam.Name, err)
		}
	}
	return tx.Commit()
}

// scanCamera scans a CameraRecord from a *sql.Rows iterator.
func scanCamera(rows *sql.Rows) (CameraRecord, error) {
	var cam CameraRecord
	var enabled, record, detect int64
	err := rows.Scan(
		&cam.ID, &cam.Name, &enabled, &cam.MainStream, &cam.SubStream,
		&record, &detect,
		&cam.ONVIFHost, &cam.ONVIFPort, &cam.ONVIFUser, &cam.ONVIFPass,
		&cam.CreatedAt, &cam.UpdatedAt,
	)
	if err != nil {
		return cam, fmt.Errorf("scanning camera row: %w", err)
	}
	cam.Enabled = enabled != 0
	cam.Record = record != 0
	cam.Detect = detect != 0
	return cam, nil
}

// scanCameraRow scans a CameraRecord from a *sql.Row.
func scanCameraRow(row *sql.Row) (CameraRecord, error) {
	var cam CameraRecord
	var enabled, record, detect int64
	err := row.Scan(
		&cam.ID, &cam.Name, &enabled, &cam.MainStream, &cam.SubStream,
		&record, &detect,
		&cam.ONVIFHost, &cam.ONVIFPort, &cam.ONVIFUser, &cam.ONVIFPass,
		&cam.CreatedAt, &cam.UpdatedAt,
	)
	if err != nil {
		return cam, err
	}
	cam.Enabled = enabled != 0
	cam.Record = record != 0
	cam.Detect = detect != 0
	return cam, nil
}

// isUniqueConstraintError checks if a SQLite error is a UNIQUE constraint violation.
// Only matches "UNIQUE constraint failed" — not CHECK, NOT NULL, or FK constraint errors,
// which would be masked as duplicate-name errors (ErrDuplicate) if we matched too broadly.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
