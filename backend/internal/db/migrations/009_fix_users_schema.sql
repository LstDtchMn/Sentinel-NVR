-- 009_fix_users_schema.sql
-- Fix schema inconsistency: migration 001 created users with an 'enabled' column
-- that migration 004 omitted. Since 004 used IF NOT EXISTS, the old schema persisted.
-- The 'enabled' column is unused by application code — drop it for consistency.
--
-- Also add a case-insensitive unique index on username. The original UNIQUE constraint
-- (case-sensitive) stays because ALTER TABLE can't drop it without a full table
-- reconstruct, but this index prevents case-variant duplicates (e.g. "Admin" vs "admin").
ALTER TABLE users DROP COLUMN enabled;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_nocase ON users(LOWER(username))
