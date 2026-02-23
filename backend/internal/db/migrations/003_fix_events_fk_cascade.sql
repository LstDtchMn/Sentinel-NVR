-- 003_fix_events_fk_cascade.sql
-- Adds ON DELETE CASCADE to events.camera_id so that deleting a camera
-- automatically removes its associated event rows. SQLite does not support
-- ALTER TABLE ... ADD CONSTRAINT, so we recreate the table.
--
-- Note: PRAGMA foreign_keys = ON is already set in db.go; this migration
-- ensures the cascade action is defined in the schema.

CREATE TABLE IF NOT EXISTS events_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id   INTEGER REFERENCES cameras(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,
    label       TEXT DEFAULT '',
    confidence  REAL DEFAULT 0,
    data        TEXT DEFAULT '{}',
    thumbnail   TEXT DEFAULT '',
    has_clip    INTEGER DEFAULT 0,
    start_time  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    end_time    TIMESTAMP,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO events_new SELECT id, camera_id, type, label, confidence, data, thumbnail, has_clip, start_time, end_time, created_at FROM events;

DROP TABLE events;

ALTER TABLE events_new RENAME TO events;

CREATE INDEX IF NOT EXISTS idx_events_camera_time ON events(camera_id, start_time);

CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
