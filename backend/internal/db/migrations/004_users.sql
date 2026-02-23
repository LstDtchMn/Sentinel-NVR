-- 004_users.sql
-- Creates the users table for local authentication and the system_settings
-- key-value store for server-generated secrets (JWT signing key, credential
-- encryption key). Both are required by Phase 7 (CG6).

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer', -- admin | viewer
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- system_settings stores server-generated secrets as base64-encoded values.
-- Keys: 'jwt_secret' (HS256 signing key), 'credential_key' (AES-256 camera cred key).
CREATE TABLE IF NOT EXISTS system_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)
