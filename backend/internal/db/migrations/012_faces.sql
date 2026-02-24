-- Phase 13: Face recognition data model (R11).
-- Stores known face embeddings for identification.
-- face_id is referenced by detection events via data JSON (not FK — decoupled).

CREATE TABLE IF NOT EXISTS faces (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,                  -- Human-readable label ("John", "Mom")
    embedding   BLOB    NOT NULL,                  -- 512-dim float32 ArcFace embedding (2048 bytes)
    thumbnail   TEXT    NOT NULL DEFAULT '',        -- Path to reference JPEG (forward-slash normalized)
    created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_faces_name ON faces (name);

-- Audio classification events use the existing events table with type='audio_detection'.
-- No schema change needed — label stores the audio class (glass_break, dog_bark, baby_cry)
-- and confidence stores the classification score.
