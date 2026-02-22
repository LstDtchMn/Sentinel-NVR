-- 002_recordings.sql
-- Phase 2: recording segment metadata for direct-to-disk MP4 recordings.

CREATE TABLE IF NOT EXISTS recordings (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id    INTEGER NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    camera_name  TEXT NOT NULL,           -- denormalized for fast queries without JOIN
    path         TEXT NOT NULL UNIQUE,    -- absolute filesystem path to the .mp4 file
    start_time   TIMESTAMP NOT NULL,
    end_time     TIMESTAMP,               -- NULL while segment is still being written
    duration_s   REAL DEFAULT 0,          -- seconds, filled when segment is finalized
    size_bytes   INTEGER DEFAULT 0,       -- filled when segment is finalized
    created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_recordings_camera_time ON recordings(camera_id, start_time);
CREATE INDEX IF NOT EXISTS idx_recordings_time ON recordings(start_time);
CREATE INDEX IF NOT EXISTS idx_recordings_camera_name ON recordings(camera_name, start_time);
