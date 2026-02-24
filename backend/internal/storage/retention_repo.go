package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// RetentionRule is a per-camera × per-event-type event retention override.
// Nil CameraID means "all cameras"; nil EventType means "all event types".
type RetentionRule struct {
	ID         int     `json:"id"`
	CameraID   *int    `json:"camera_id"`  // null = all cameras
	EventType  *string `json:"event_type"` // null = all types
	EventsDays int     `json:"events_days"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

var (
	ErrRuleNotFound = errors.New("retention rule not found")
	ErrRuleConflict = errors.New("a rule for this camera/event-type combination already exists")
)

// RetentionRepository provides CRUD for the retention_rules table.
type RetentionRepository struct {
	db *sql.DB
}

// NewRetentionRepository creates a retention repository.
func NewRetentionRepository(db *sql.DB) *RetentionRepository {
	return &RetentionRepository{db: db}
}

// List returns all retention rules ordered by specificity (camera, then event type).
func (r *RetentionRepository) List(ctx context.Context) ([]RetentionRule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, camera_id, event_type, events_days, created_at, updated_at
		FROM retention_rules
		ORDER BY
			(CASE WHEN camera_id  IS NOT NULL THEN 0 ELSE 1 END),
			(CASE WHEN event_type IS NOT NULL THEN 0 ELSE 1 END),
			id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []RetentionRule{} // non-nil slice so JSON marshals as [] not null
	for rows.Next() {
		var rule RetentionRule
		if err := rows.Scan(&rule.ID, &rule.CameraID, &rule.EventType,
			&rule.EventsDays, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

// Get returns a single rule by ID.
func (r *RetentionRepository) Get(ctx context.Context, id int) (*RetentionRule, error) {
	var rule RetentionRule
	err := r.db.QueryRowContext(ctx, `
		SELECT id, camera_id, event_type, events_days, created_at, updated_at
		FROM retention_rules WHERE id = ?`, id).
		Scan(&rule.ID, &rule.CameraID, &rule.EventType,
			&rule.EventsDays, &rule.CreatedAt, &rule.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRuleNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// Create inserts a new retention rule. Returns ErrRuleConflict on duplicate key.
func (r *RetentionRepository) Create(ctx context.Context, cameraID *int, eventType *string, eventsDays int) (*RetentionRule, error) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO retention_rules (camera_id, event_type, events_days, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		cameraID, eventType, eventsDays, now, now)
	if err != nil {
		if isRetentionUniqueError(err) {
			return nil, ErrRuleConflict
		}
		return nil, err
	}
	id, _ := result.LastInsertId()
	return r.Get(ctx, int(id))
}

// Update changes the events_days for an existing rule.
func (r *RetentionRepository) Update(ctx context.Context, id, eventsDays int) (*RetentionRule, error) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	res, err := r.db.ExecContext(ctx,
		`UPDATE retention_rules SET events_days = ?, updated_at = ? WHERE id = ?`,
		eventsDays, now, id)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrRuleNotFound
	}
	return r.Get(ctx, id)
}

// Delete removes a rule by ID. Returns ErrRuleNotFound when no row existed.
func (r *RetentionRepository) Delete(ctx context.Context, id int) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM retention_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// EffectiveDays returns the most specific matching events_days for a given
// (cameraID, eventType) pair, or -1 when no rule matches.
// Priority (highest → lowest):
//
//	(camera=X, type=T) > (camera=X, type=NULL) > (camera=NULL, type=T) > (camera=NULL, type=NULL)
func (r *RetentionRepository) EffectiveDays(ctx context.Context, cameraID int, eventType string) (int, error) {
	var days int
	err := r.db.QueryRowContext(ctx, `
		SELECT events_days FROM retention_rules
		WHERE (camera_id = ? OR camera_id IS NULL)
		  AND (event_type = ? OR event_type IS NULL)
		ORDER BY
			(CASE WHEN camera_id  IS NOT NULL THEN 1 ELSE 0 END) DESC,
			(CASE WHEN event_type IS NOT NULL THEN 1 ELSE 0 END) DESC
		LIMIT 1`, cameraID, eventType).Scan(&days)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, nil
	}
	return days, err
}

func isRetentionUniqueError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
