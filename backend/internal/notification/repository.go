// Package notification repository: CRUD for tokens, prefs, and delivery log (Phase 8, R9).
package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/dbutil"
)

// Repository provides access to notification_tokens, notification_prefs, and
// notification_log tables.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a Repository backed by db.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// ─── Tokens ────────────────────────────────────────────────────────────────

// UpsertToken inserts a device token or updates its label if the
// (user_id, provider, token) triple already exists. Returns the stored record.
func (r *Repository) UpsertToken(ctx context.Context, userID int, token, provider, label string) (*TokenRecord, error) {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO notification_tokens (user_id, token, provider, label)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id, provider, token) DO UPDATE
		 SET label=excluded.label, updated_at=CURRENT_TIMESTAMP`,
		userID, token, provider, label,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert token: %w", err)
	}
	return r.tokenByValue(ctx, userID, provider, token)
}

// tokenByValue retrieves a token row by its unique (user_id, provider, token) key.
func (r *Repository) tokenByValue(ctx context.Context, userID int, provider, token string) (*TokenRecord, error) {
	rec := &TokenRecord{}
	var createdAt, updatedAt string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, token, provider, label, created_at, updated_at
		 FROM notification_tokens WHERE user_id=? AND provider=? AND token=?`,
		userID, provider, token,
	).Scan(&rec.ID, &rec.UserID, &rec.Token, &rec.Provider, &rec.Label, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.CreatedAt, err = dbutil.ParseSQLiteTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	rec.UpdatedAt, err = dbutil.ParseSQLiteTime(updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}
	return rec, nil
}

// ListTokensByUser returns all registered device tokens for a user.
func (r *Repository) ListTokensByUser(ctx context.Context, userID int) ([]TokenRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, token, provider, label, created_at, updated_at
		 FROM notification_tokens WHERE user_id=? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []TokenRecord
	for rows.Next() {
		var rec TokenRecord
		var createdAt, updatedAt string
		if err := rows.Scan(&rec.ID, &rec.UserID, &rec.Token, &rec.Provider, &rec.Label, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var parseErr error
		rec.CreatedAt, parseErr = dbutil.ParseSQLiteTime(createdAt)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing created_at: %w", parseErr)
		}
		rec.UpdatedAt, parseErr = dbutil.ParseSQLiteTime(updatedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing updated_at: %w", parseErr)
		}
		tokens = append(tokens, rec)
	}
	if tokens == nil {
		tokens = []TokenRecord{}
	}
	return tokens, rows.Err()
}

// GetTokenByID retrieves a single token by its primary key and owning user.
// Returns ErrNotFound if the row does not exist or belongs to a different user.
func (r *Repository) GetTokenByID(ctx context.Context, id, userID int) (*TokenRecord, error) {
	rec := &TokenRecord{}
	var createdAt, updatedAt string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, token, provider, label, created_at, updated_at
		 FROM notification_tokens WHERE id=? AND user_id=?`,
		id, userID,
	).Scan(&rec.ID, &rec.UserID, &rec.Token, &rec.Provider, &rec.Label, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var parseErr error
	rec.CreatedAt, parseErr = dbutil.ParseSQLiteTime(createdAt)
	if parseErr != nil {
		return nil, fmt.Errorf("parsing created_at: %w", parseErr)
	}
	rec.UpdatedAt, parseErr = dbutil.ParseSQLiteTime(updatedAt)
	if parseErr != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", parseErr)
	}
	return rec, nil
}

// DeleteToken removes a token by (id, userID). Returns ErrNotFound if the row
// does not exist or belongs to a different user.
func (r *Repository) DeleteToken(ctx context.Context, id, userID int) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM notification_tokens WHERE id=? AND user_id=?`, id, userID,
	)
	if err != nil {
		return err
	}
	if n, rowsErr := res.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("checking rows affected: %w", rowsErr)
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// TokensByUserAndProvider returns all tokens for a user matching a given provider.
// Used by the service to find delivery targets.
func (r *Repository) TokensByUserAndProvider(ctx context.Context, userID int, provider string) ([]TokenRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, token, provider, label, created_at, updated_at
		 FROM notification_tokens WHERE user_id=? AND provider=? ORDER BY id`,
		userID, provider,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []TokenRecord
	for rows.Next() {
		var rec TokenRecord
		var createdAt, updatedAt string
		if err := rows.Scan(&rec.ID, &rec.UserID, &rec.Token, &rec.Provider, &rec.Label, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var parseErr error
		rec.CreatedAt, parseErr = dbutil.ParseSQLiteTime(createdAt)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing created_at: %w", parseErr)
		}
		rec.UpdatedAt, parseErr = dbutil.ParseSQLiteTime(updatedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing updated_at: %w", parseErr)
		}
		tokens = append(tokens, rec)
	}
	return tokens, rows.Err()
}

// TokensForUser returns all tokens for a given user across all providers.
func (r *Repository) TokensForUser(ctx context.Context, userID int) ([]TokenRecord, error) {
	return r.ListTokensByUser(ctx, userID)
}

// ─── Prefs ──────────────────────────────────────────────────────────────────

// UpsertPref inserts or updates a notification preference row.
func (r *Repository) UpsertPref(ctx context.Context, p PrefRecord) (*PrefRecord, error) {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO notification_prefs (user_id, event_type, camera_id, enabled, critical)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, event_type, COALESCE(camera_id, -1))
		 DO UPDATE SET enabled=excluded.enabled, critical=excluded.critical, updated_at=datetime('now')`,
		p.UserID, p.EventType, p.CameraID, boolToInt(p.Enabled), boolToInt(p.Critical),
	)
	if err != nil {
		return nil, fmt.Errorf("upsert pref: %w", err)
	}
	return r.getPrefByKey(ctx, p.UserID, p.EventType, p.CameraID)
}

func (r *Repository) getPrefByKey(ctx context.Context, userID int, eventType string, cameraID *int) (*PrefRecord, error) {
	rec := &PrefRecord{}
	var enabled, critical int64

	var err error
	if cameraID == nil {
		err = r.db.QueryRowContext(ctx,
			`SELECT id, user_id, event_type, camera_id, enabled, critical
			 FROM notification_prefs
			 WHERE user_id=? AND event_type=? AND camera_id IS NULL`,
			userID, eventType,
		).Scan(&rec.ID, &rec.UserID, &rec.EventType, &rec.CameraID, &enabled, &critical)
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT id, user_id, event_type, camera_id, enabled, critical
			 FROM notification_prefs
			 WHERE user_id=? AND event_type=? AND camera_id=?`,
			userID, eventType, *cameraID,
		).Scan(&rec.ID, &rec.UserID, &rec.EventType, &rec.CameraID, &enabled, &critical)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.Enabled = enabled != 0
	rec.Critical = critical != 0
	return rec, nil
}

// ListPrefsByUser returns all preferences for a user.
func (r *Repository) ListPrefsByUser(ctx context.Context, userID int) ([]PrefRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, event_type, camera_id, enabled, critical
		 FROM notification_prefs WHERE user_id=? ORDER BY event_type, camera_id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prefs []PrefRecord
	for rows.Next() {
		var rec PrefRecord
		var enabled, critical int64
		if err := rows.Scan(&rec.ID, &rec.UserID, &rec.EventType, &rec.CameraID, &enabled, &critical); err != nil {
			return nil, err
		}
		rec.Enabled = enabled != 0
		rec.Critical = critical != 0
		prefs = append(prefs, rec)
	}
	if prefs == nil {
		prefs = []PrefRecord{}
	}
	return prefs, rows.Err()
}

// DeletePref removes a preference by (id, userID).
func (r *Repository) DeletePref(ctx context.Context, id, userID int) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM notification_prefs WHERE id=? AND user_id=?`, id, userID,
	)
	if err != nil {
		return err
	}
	if n, rowsErr := res.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("checking rows affected: %w", rowsErr)
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MatchingPrefs returns all enabled prefs that match the given event type and
// optional camera. A pref matches when:
//   - event_type = eventType OR event_type = '*'
//   - camera_id IS NULL (all cameras) OR camera_id = cameraID
//   - enabled = 1
//
// cameraID = 0 means the event has no associated camera.
func (r *Repository) MatchingPrefs(ctx context.Context, eventType string, cameraID int) ([]PrefRecord, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if cameraID != 0 {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, user_id, event_type, camera_id, enabled, critical
			 FROM notification_prefs
			 WHERE enabled=1
			   AND (event_type=? OR event_type='*')
			   AND (camera_id IS NULL OR camera_id=?)
			 ORDER BY critical DESC`,
			eventType, cameraID,
		)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, user_id, event_type, camera_id, enabled, critical
			 FROM notification_prefs
			 WHERE enabled=1
			   AND (event_type=? OR event_type='*')
			   AND camera_id IS NULL
			 ORDER BY critical DESC`,
			eventType,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prefs []PrefRecord
	for rows.Next() {
		var rec PrefRecord
		var enabled, critical int64
		if err := rows.Scan(&rec.ID, &rec.UserID, &rec.EventType, &rec.CameraID, &enabled, &critical); err != nil {
			return nil, err
		}
		rec.Enabled = enabled != 0
		rec.Critical = critical != 0
		prefs = append(prefs, rec)
	}
	return prefs, rows.Err()
}

// ─── Log ────────────────────────────────────────────────────────────────────

// CreateLog inserts a new notification_log row with status='pending'.
// Returns the assigned log ID.
func (r *Repository) CreateLog(ctx context.Context, l LogRecord) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO notification_log (event_id, token_id, provider, title, body, deep_link, status, attempts)
		 VALUES (?, ?, ?, ?, ?, ?, 'pending', 0)`,
		l.EventID, l.TokenID, l.Provider, l.Title, l.Body, l.DeepLink,
	)
	if err != nil {
		return 0, fmt.Errorf("create log: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting log ID: %w", err)
	}
	return int(id), nil
}

// MarkSent sets a log row to status='sent' and records the sent timestamp.
func (r *Repository) MarkSent(ctx context.Context, logID int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE notification_log
		 SET status='sent', sent_at=CURRENT_TIMESTAMP, attempts=attempts+1
		 WHERE id=?`,
		logID,
	)
	return err
}

// MarkFailed sets a log row to status='failed' and records the error message.
func (r *Repository) MarkFailed(ctx context.Context, logID int, errMsg string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE notification_log
		 SET status='failed', attempts=attempts+1, last_error=?
		 WHERE id=?`,
		errMsg, logID,
	)
	return err
}

// PendingLog is an internal type for crash-recovery queries, joining log + token.
type PendingLog struct {
	LogID    int
	TokenID  int
	Provider string
	Title    string
	Body     string
	DeepLink string
	EventID  *int
	Token    string
	UserID   int
}

// PendingLogs returns log rows with status='pending' that are older than minAge.
// Used on startup to re-queue notifications that survived a crash (R9, CG9).
func (r *Repository) PendingLogs(ctx context.Context, minAge time.Duration) ([]PendingLog, error) {
	cutoff := time.Now().Add(-minAge)
	rows, err := r.db.QueryContext(ctx,
		`SELECT l.id, l.token_id, l.provider, l.title, l.body, l.deep_link,
		        l.event_id, t.token, t.user_id
		 FROM notification_log l
		 JOIN notification_tokens t ON t.id = l.token_id
		 WHERE l.status = 'pending' AND l.scheduled_at < ?
		 LIMIT 100`,
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PendingLog
	for rows.Next() {
		var pl PendingLog
		var eventID sql.NullInt64
		if err := rows.Scan(
			&pl.LogID, &pl.TokenID, &pl.Provider, &pl.Title, &pl.Body, &pl.DeepLink,
			&eventID, &pl.Token, &pl.UserID,
		); err != nil {
			return nil, err
		}
		if eventID.Valid {
			id := int(eventID.Int64)
			pl.EventID = &id
		}
		result = append(result, pl)
	}
	return result, rows.Err()
}

// ListLogsByUser returns recent notification_log entries for tokens owned by userID.
func (r *Repository) ListLogsByUser(ctx context.Context, userID, limit int) ([]LogRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT l.id, l.event_id, l.token_id, l.provider, l.title, l.body, l.deep_link,
		        l.status, l.attempts, l.last_error, l.scheduled_at, l.sent_at
		 FROM notification_log l
		 JOIN notification_tokens t ON t.id = l.token_id
		 WHERE t.user_id = ?
		 ORDER BY l.scheduled_at DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogRecord
	for rows.Next() {
		var rec LogRecord
		var eventID sql.NullInt64
		var deepLink, lastErr sql.NullString
		var scheduledAt string
		var sentAt sql.NullString
		if err := rows.Scan(
			&rec.ID, &eventID, &rec.TokenID, &rec.Provider,
			&rec.Title, &rec.Body, &deepLink,
			&rec.Status, &rec.Attempts, &lastErr,
			&scheduledAt, &sentAt,
		); err != nil {
			return nil, err
		}
		if eventID.Valid {
			id := int(eventID.Int64)
			rec.EventID = &id
		}
		rec.DeepLink = deepLink.String
		rec.LastError = lastErr.String
		var parseErr error
		rec.ScheduledAt, parseErr = dbutil.ParseSQLiteTime(scheduledAt)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing scheduled_at: %w", parseErr)
		}
		if sentAt.Valid {
			t, parseErr := dbutil.ParseSQLiteTime(sentAt.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parsing sent_at: %w", parseErr)
			}
			rec.SentAt = &t
		}
		logs = append(logs, rec)
	}
	if logs == nil {
		logs = []LogRecord{}
	}
	return logs, rows.Err()
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
