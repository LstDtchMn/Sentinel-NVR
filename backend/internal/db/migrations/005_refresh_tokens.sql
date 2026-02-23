-- 005_refresh_tokens.sql
-- Creates the refresh_tokens table for JWT session management (Phase 7, CG6).
-- Refresh tokens are randomly generated 32-byte values (hex encoded = 64 chars).
-- They are stored in httpOnly cookies and in this table for server-side revocation.
-- ON DELETE CASCADE ensures tokens are cleaned up when a user is deleted.

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token      TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token ON refresh_tokens(token);
