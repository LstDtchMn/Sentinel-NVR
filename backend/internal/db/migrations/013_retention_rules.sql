-- Phase 14 (R14): Per-camera × per-event-type event retention rules.
-- A rule overrides global event retention for events matching (camera_id, event_type).
-- camera_id NULL = applies to all cameras.
-- event_type NULL = applies to all event types for the given camera.
-- Lookup priority at cleanup time (highest specificity wins):
--   (camera=X, type=T) > (camera=X, type=NULL) > (camera=NULL, type=T) > global config.

CREATE TABLE IF NOT EXISTS retention_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id   INTEGER REFERENCES cameras(id) ON DELETE CASCADE, -- NULL = all cameras
    event_type  TEXT,              -- NULL = all types; 'detection', 'face_match', etc.
    events_days INTEGER NOT NULL CHECK (events_days >= 1),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- SQLite UNIQUE treats NULL as distinct from every other value including NULL,
-- so UNIQUE(camera_id, event_type) would allow multiple (NULL, NULL) rows.
-- COALESCE to sentinel values (-1, '') enforces the desired "at most one rule
-- per (camera, type) pair" constraint including the wildcard (NULL, NULL) case.
CREATE UNIQUE INDEX IF NOT EXISTS idx_retention_rules_camera_type
    ON retention_rules(COALESCE(camera_id, -1), COALESCE(event_type, ''));

CREATE INDEX IF NOT EXISTS idx_retention_rules_camera
    ON retention_rules(camera_id);
