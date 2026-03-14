package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/dbutil"
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

// CountUsers returns the total number of user accounts in the database.
// Used by the setup handler to determine if first-run setup is required.
func (r *Repository) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting users: %w", err)
	}
	return count, nil
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
	u.CreatedAt, err = dbutil.ParseSQLiteTime(createdStr)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	u.UpdatedAt, err = dbutil.ParseSQLiteTime(updatedStr)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}
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
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting user %q: %w", username, err)
	}
	u.CreatedAt, err = dbutil.ParseSQLiteTime(createdStr)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	u.UpdatedAt, err = dbutil.ParseSQLiteTime(updatedStr)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}
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
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting user %d: %w", id, err)
	}
	u.CreatedAt, err = dbutil.ParseSQLiteTime(createdStr)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	u.UpdatedAt, err = dbutil.ParseSQLiteTime(updatedStr)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}
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
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting refresh token: %w", err)
	}
	expiresAt, err := dbutil.ParseSQLiteTime(expiresStr)
	if err != nil {
		return nil, fmt.Errorf("getting refresh token: parsing expires_at %q: %w", expiresStr, err)
	}
	rt.ExpiresAt = expiresAt
	rt.CreatedAt, err = dbutil.ParseSQLiteTime(createdStr)
	if err != nil {
		return nil, fmt.Errorf("getting refresh token: parsing created_at %q: %w", createdStr, err)
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, ErrTokenExpired
	}
	return &rt, nil
}

// ClaimRefreshToken atomically deletes and returns a refresh token in one
// statement (DELETE ... RETURNING). Because the DELETE is atomic, two concurrent
// refresh requests racing on the same token will see at most one succeed; the
// second gets sql.ErrNoRows → ErrNotFound, preventing duplicate session minting.
// Returns ErrNotFound if the token does not exist, ErrTokenExpired if expired.
func (r *Repository) ClaimRefreshToken(ctx context.Context, token string) (*RefreshToken, error) {
	var rt RefreshToken
	var expiresStr, createdStr string
	err := r.db.QueryRowContext(ctx,
		`DELETE FROM refresh_tokens WHERE token = ?
		 RETURNING id, user_id, token, expires_at, created_at`, token,
	).Scan(&rt.ID, &rt.UserID, &rt.Token, &expiresStr, &createdStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("claiming refresh token: %w", err)
	}
	expiresAt, err := dbutil.ParseSQLiteTime(expiresStr)
	if err != nil {
		return nil, fmt.Errorf("claiming refresh token: parsing expires_at %q: %w", expiresStr, err)
	}
	rt.ExpiresAt = expiresAt
	rt.CreatedAt, err = dbutil.ParseSQLiteTime(createdStr)
	if err != nil {
		return nil, fmt.Errorf("claiming refresh token: parsing created_at %q: %w", createdStr, err)
	}
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

// GetUserByOIDCSub returns the user linked to the given OIDC subject claim.
// Returns ErrNotFound when no user has that oidc_sub.
func (r *Repository) GetUserByOIDCSub(ctx context.Context, sub string) (*User, error) {
	var u User
	var createdStr, updatedStr string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, created_at, updated_at
		 FROM users WHERE oidc_sub = ?`, sub,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &createdStr, &updatedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting user by OIDC sub: %w", err)
	}
	u.CreatedAt, err = dbutil.ParseSQLiteTime(createdStr)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	u.UpdatedAt, err = dbutil.ParseSQLiteTime(updatedStr)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}
	return &u, nil
}

// CreateOIDCUser inserts a new OIDC-only user (no local password) linked to the given subject.
// Callers must supply a non-empty display name; role should be "viewer" for self-provisioned users.
func (r *Repository) CreateOIDCUser(ctx context.Context, sub, username, role string) (*User, error) {
	var u User
	var createdStr, updatedStr string
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO users (username, password_hash, role, oidc_sub)
		 VALUES (?, '', ?, ?)
		 RETURNING id, username, password_hash, role, created_at, updated_at`,
		username, role, sub,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &createdStr, &updatedStr)
	if err != nil {
		return nil, fmt.Errorf("creating OIDC user %q: %w", username, err)
	}
	u.CreatedAt, err = dbutil.ParseSQLiteTime(createdStr)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	u.UpdatedAt, err = dbutil.ParseSQLiteTime(updatedStr)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}
	return &u, nil
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
	if errors.Is(err, sql.ErrNoRows) {
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
	// INSERT OR IGNORE: if the key already exists (race with concurrent startup),
	// the INSERT is a no-op. Log any *real* DB errors (disk full, read-only) so
	// the subsequent GetSetting failure has clear context.
	if _, insErr := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO system_settings (key, value, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)`,
		settingKey, base64.StdEncoding.EncodeToString(candidate),
	); insErr != nil {
		slog.Default().Warn("GetOrGenerateKey: INSERT failed, will attempt read",
			"key", settingKey, "error", insErr)
	}

	// Always read back the authoritative stored value (ours or the race winner's).
	stored, err := r.GetSetting(ctx, settingKey)
	if err != nil {
		return nil, fmt.Errorf("reading key %q after insert: %w", settingKey, err)
	}
	key, decErr := base64.StdEncoding.DecodeString(stored)
	if decErr != nil {
		return nil, fmt.Errorf("decoding key %q: %w", settingKey, decErr)
	}
	// AES-256 and HS256 both require exactly 32 bytes. A wrong-length key means
	// the database row was corrupted or written by incompatible software.
	if len(key) != 32 {
		return nil, fmt.Errorf("stored key %q has unexpected length %d (expected 32 bytes); database may be corrupt", settingKey, len(key))
	}
	return key, nil
}

