package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"
)

// User represents a row in the users table.
type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"` // never expose in API responses
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// RefreshToken represents a row in the refresh_tokens table.
type RefreshToken struct {
	ID        int
	UserID    int
	Token     string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Repository provides access to the users, refresh_tokens, and system_settings tables.
type Repository struct {
	db *sql.DB
}

// NewRepository creates an auth repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// CreateUser inserts a new user with a pre-hashed password.
func (r *Repository) CreateUser(ctx context.Context, username, passwordHash, role string) (*User, error) {
	var u User
	var createdStr, updatedStr string
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO users (username, password_hash, role)
		 VALUES (?, ?, ?)
		 RETURNING id, username, password_hash, role, created_at, updated_at`,
		username, passwordHash, role,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &createdStr, &updatedStr)
	if err != nil {
		return nil, fmt.Errorf("creating user %q: %w", username, err)
	}
	u.CreatedAt, _ = parseSQLiteTime(createdStr)
	u.UpdatedAt, _ = parseSQLiteTime(updatedStr)
	return &u, nil
}

// GetUserByUsername returns a user by username (case-insensitive).
// Returns ErrNotFound if the user does not exist.
func (r *Repository) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	var createdStr, updatedStr string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, created_at, updated_at
		 FROM users WHERE username = ? COLLATE NOCASE`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting user %q: %w", username, err)
	}
	u.CreatedAt, _ = parseSQLiteTime(createdStr)
	u.UpdatedAt, _ = parseSQLiteTime(updatedStr)
	return &u, nil
}

// GetUserByID returns a user by ID. Returns ErrNotFound if not found.
func (r *Repository) GetUserByID(ctx context.Context, id int) (*User, error) {
	var u User
	var createdStr, updatedStr string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting user %d: %w", id, err)
	}
	u.CreatedAt, _ = parseSQLiteTime(createdStr)
	u.UpdatedAt, _ = parseSQLiteTime(updatedStr)
	return &u, nil
}

// CreateRefreshToken stores a new refresh token for the given user.
func (r *Repository) CreateRefreshToken(ctx context.Context, userID int, token string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token, expires_at) VALUES (?, ?, ?)`,
		userID, token, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("storing refresh token: %w", err)
	}
	return nil
}

// GetRefreshToken returns a refresh token record by token value.
// Returns ErrNotFound if not found, ErrTokenExpired if past expiry.
func (r *Repository) GetRefreshToken(ctx context.Context, token string) (*RefreshToken, error) {
	var rt RefreshToken
	var expiresStr, createdStr string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, token, expires_at, created_at
		 FROM refresh_tokens WHERE token = ?`, token,
	).Scan(&rt.ID, &rt.UserID, &rt.Token, &expiresStr, &createdStr)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting refresh token: %w", err)
	}
	expiresAt, err := parseSQLiteTime(expiresStr)
	if err != nil {
		return nil, fmt.Errorf("getting refresh token: parsing expires_at %q: %w", expiresStr, err)
	}
	rt.ExpiresAt = expiresAt
	rt.CreatedAt, _ = parseSQLiteTime(createdStr) // created_at is informational; parse failure non-fatal
	if time.Now().After(rt.ExpiresAt) {
		return nil, ErrTokenExpired
	}
	return &rt, nil
}

// DeleteRefreshToken removes a refresh token (logout).
func (r *Repository) DeleteRefreshToken(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE token = ?`, token)
	return err
}

// DeleteExpiredRefreshTokens removes all tokens past their expiry time.
// Call on startup and periodically to keep the table small.
func (r *Repository) DeleteExpiredRefreshTokens(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at < ?`, time.Now())
	return err
}

// GetSetting returns the value of a system_settings key.
// Returns ErrNotFound if the key does not exist.
func (r *Repository) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx,
		`SELECT value FROM system_settings WHERE key = ?`, key,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("getting setting %q: %w", key, err)
	}
	return value, nil
}

// SetSetting upserts a key-value pair in system_settings.
func (r *Repository) SetSetting(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO system_settings (key, value, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("setting %q: %w", key, err)
	}
	return nil
}

// GetOrGenerateKey loads a base64-encoded 32-byte key from system_settings,
// or generates and stores a new one if the key does not exist.
// Used to initialize the JWT secret and credential encryption key on first run.
//
// Race-safe: generates a candidate key, then uses INSERT OR IGNORE so that
// concurrent startups cannot overwrite an already-stored key. The value that
// "wins" the INSERT is always re-read from the DB so all callers converge on
// the same key.
func (r *Repository) GetOrGenerateKey(ctx context.Context, settingKey string) ([]byte, error) {
	// Generate a candidate. If two processes race, only one INSERT wins.
	candidate, genErr := GenerateKey()
	if genErr != nil {
		return nil, genErr
	}
	_, _ = r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO system_settings (key, value, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)`,
		settingKey, base64.StdEncoding.EncodeToString(candidate),
	)

	// Always read back the authoritative stored value (ours or the race winner's).
	stored, err := r.GetSetting(ctx, settingKey)
	if err != nil {
		return nil, fmt.Errorf("reading key %q after insert: %w", settingKey, err)
	}
	key, decErr := base64.StdEncoding.DecodeString(stored)
	if decErr != nil {
		return nil, fmt.Errorf("decoding key %q: %w", settingKey, decErr)
	}
	return key, nil
}

// parseSQLiteTime parses the time formats the modernc/sqlite driver may produce.
// Duplicated across packages (recording, detection, auth) since each uses private helpers.
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
