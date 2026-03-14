-- Fix: SQLite treats NULL as distinct in UNIQUE constraints, allowing duplicate
-- notification prefs when camera_id IS NULL. Replace inline UNIQUE with an
-- expression index using COALESCE (C6 fix).
-- Also adds updated_at column (missing from original 006 schema).

-- Drop the old inline unique constraint by recreating the table
CREATE TABLE notification_prefs_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    camera_id INTEGER REFERENCES cameras(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL DEFAULT 1,
    critical INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO notification_prefs_new (id, user_id, event_type, camera_id, enabled, critical, created_at, updated_at)
    SELECT id, user_id, event_type, camera_id, enabled, critical, created_at, created_at
    FROM notification_prefs;

DROP TABLE notification_prefs;
ALTER TABLE notification_prefs_new RENAME TO notification_prefs;

-- Expression index that treats NULL camera_id as -1 for uniqueness
CREATE UNIQUE INDEX idx_notif_prefs_unique ON notification_prefs(user_id, event_type, COALESCE(camera_id, -1));
