-- 001_initial_schema.sql
-- Phase 0 foundation tables for Sentinel NVR
-- Phase 2 will add 002_recordings.sql with a recordings table for segment metadata.

-- Cameras: canonical source of camera configuration
CREATE TABLE IF NOT EXISTS cameras (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    enabled     INTEGER NOT NULL DEFAULT 1,
    main_stream TEXT NOT NULL,
    sub_stream  TEXT DEFAULT '',
    record      INTEGER NOT NULL DEFAULT 1,
    detect      INTEGER NOT NULL DEFAULT 0,
    onvif_host  TEXT DEFAULT '',
    onvif_port  INTEGER DEFAULT 0,
    onvif_user  TEXT DEFAULT '',
    onvif_pass  TEXT DEFAULT '', -- TODO: Phase 7 — encrypt with AES-256-GCM using server secret
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Events: all system and detection events (CG8 write-ahead log)
CREATE TABLE IF NOT EXISTS events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id   INTEGER REFERENCES cameras(id),
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

CREATE INDEX IF NOT EXISTS idx_events_camera_time ON events(camera_id, start_time);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);

-- Users: ready for Phase 7 auth (CG6)
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer',
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
