package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestAuthDB opens a SQLite DB with all required tables for auth tests.
func newTestAuthDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "auth_svc.db")
	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	database.Exec(`PRAGMA busy_timeout = 5000`)
	database.Exec(`PRAGMA journal_mode = WAL`)

	// Create minimal schema
	stmts := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE COLLATE NOCASE,
			password_hash TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'viewer',
			oidc_sub TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE refresh_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token TEXT NOT NULL UNIQUE,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE system_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, s := range stmts {
		if _, err := database.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s[:40], err)
		}
	}
	return database
}

// ─── Refresh token rotation ──────────────────────────────────────────────────

func TestService_Refresh_RotatesToken(t *testing.T) {
	db := newTestAuthDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	svc, err := New(ctx, repo, 900, 604800)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Create a user
	hash, _ := HashPassword("pass123")
	user, err := repo.CreateUser(ctx, "testuser", hash, "admin")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Login to get initial tokens
	pair, err := svc.Login(ctx, "testuser", "pass123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// Refresh should succeed and return new tokens
	newPair, err := svc.Refresh(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// New refresh token must differ from the old one (rotation)
	if newPair.RefreshToken == pair.RefreshToken {
		t.Error("new refresh token should differ from old one")
	}
	// New access token must be valid
	claims, err := svc.ValidateAccessToken(newPair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken on new access: %v", err)
	}
	if claims.UserID != user.ID {
		t.Errorf("UserID = %d, want %d", claims.UserID, user.ID)
	}

	// Old refresh token must no longer work (consumed by ClaimRefreshToken)
	_, err = svc.Refresh(ctx, pair.RefreshToken)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("old refresh token should fail with ErrNotFound, got %v", err)
	}
}

func TestService_Refresh_ExpiredToken(t *testing.T) {
	db := newTestAuthDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	svc, err := New(ctx, repo, 900, 604800)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Create user
	hash, _ := HashPassword("pass123")
	repo.CreateUser(ctx, "testuser", hash, "admin")

	// Manually insert an expired refresh token
	expiredToken := "expired-refresh-token"
	expires := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token, expires_at) VALUES (1, ?, ?)`,
		expiredToken, expires,
	)

	_, err = svc.Refresh(ctx, expiredToken)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestService_Logout_Idempotent(t *testing.T) {
	db := newTestAuthDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	svc, err := New(ctx, repo, 900, 604800)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hash, _ := HashPassword("pass123")
	repo.CreateUser(ctx, "testuser", hash, "admin")

	pair, err := svc.Login(ctx, "testuser", "pass123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// First logout should succeed
	if err := svc.Logout(ctx, pair.RefreshToken); err != nil {
		t.Fatalf("first Logout: %v", err)
	}

	// Second logout on same token should also succeed (idempotent)
	if err := svc.Logout(ctx, pair.RefreshToken); err != nil {
		t.Fatalf("second Logout should be idempotent, got: %v", err)
	}
}

func TestService_Login_InvalidCredentials(t *testing.T) {
	db := newTestAuthDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	svc, err := New(ctx, repo, 900, 604800)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hash, _ := HashPassword("correct")
	repo.CreateUser(ctx, "testuser", hash, "admin")

	// Wrong password
	_, err = svc.Login(ctx, "testuser", "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}

	// Non-existent user (should return generic error, not "user not found")
	_, err = svc.Login(ctx, "ghost", "pass")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials for unknown user, got %v", err)
	}
}

func TestService_ValidateAccessToken_SubjectCrossCheck(t *testing.T) {
	db := newTestAuthDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	svc, err := New(ctx, repo, 900, 604800)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hash, _ := HashPassword("pass123")
	repo.CreateUser(ctx, "testuser", hash, "admin")

	pair, err := svc.Login(ctx, "testuser", "pass123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	claims, err := svc.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}

	_ = slog.Default() // avoid unused import

	// Subject should match UserID
	if claims.Username != "testuser" {
		t.Errorf("Username = %q, want %q", claims.Username, "testuser")
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want %q", claims.Role, "admin")
	}
}
