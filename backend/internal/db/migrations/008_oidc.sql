-- Phase 9: OIDC user identity linking (CG6).
-- Adds oidc_sub column to users table for OIDC SSO login support.
-- OIDC users have oidc_sub set and an empty password_hash.
-- The partial UNIQUE index allows multiple rows with oidc_sub IS NULL (local users)
-- while still enforcing uniqueness across OIDC-linked accounts.
ALTER TABLE users ADD COLUMN oidc_sub TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc_sub ON users(oidc_sub)
    WHERE oidc_sub IS NOT NULL
