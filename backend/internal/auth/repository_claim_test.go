package auth

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestAuthRepoForClaim(t *testing.T) *Repository {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "auth_claim.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		t.Fatalf("set busy_timeout: %v", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		t.Fatalf("set journal_mode WAL: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE refresh_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token TEXT NOT NULL UNIQUE,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("create refresh_tokens table: %v", err)
	}

	return NewRepository(db)
}

func insertRefreshToken(t *testing.T, repo *Repository, userID int, token string, expiresAt time.Time) {
	t.Helper()
	// Store timestamps in a format ParseSQLiteTime accepts.
	expires := expiresAt.UTC().Format(time.RFC3339)
	_, err := repo.db.ExecContext(context.Background(),
		`INSERT INTO refresh_tokens (user_id, token, expires_at) VALUES (?, ?, ?)`,
		userID, token, expires,
	)
	if err != nil {
		t.Fatalf("insert refresh token: %v", err)
	}
}

func TestClaimRefreshToken_ConcurrentSingleWinner(t *testing.T) {
	repo := newTestAuthRepoForClaim(t)
	ctx := context.Background()

	const token = "race-token"
	insertRefreshToken(t, repo, 1, token, time.Now().UTC().Add(1*time.Hour))

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			_, errs[idx] = repo.ClaimRefreshToken(ctx, token)
		}(i)
	}

	close(start)
	wg.Wait()

	successes := 0
	notFound := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrNotFound):
			notFound++
		default:
			t.Fatalf("unexpected ClaimRefreshToken error: %v", err)
		}
	}

	if successes != 1 || notFound != 1 {
		t.Fatalf("want 1 success + 1 ErrNotFound, got successes=%d notFound=%d errs=%v", successes, notFound, errs)
	}

	_, err := repo.ClaimRefreshToken(ctx, token)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("token should be consumed, expected ErrNotFound, got %v", err)
	}
}

func TestClaimRefreshToken_Expired(t *testing.T) {
	repo := newTestAuthRepoForClaim(t)
	ctx := context.Background()

	const token = "expired-token"
	insertRefreshToken(t, repo, 1, token, time.Now().UTC().Add(-1*time.Minute))

	_, err := repo.ClaimRefreshToken(ctx, token)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}

	_, err = repo.ClaimRefreshToken(ctx, token)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired token should be removed after claim, expected ErrNotFound, got %v", err)
	}
}
