-- Phase 8: Notification infrastructure (R9).
-- Stores device tokens, per-user preferences, and a delivery log for crash recovery.

-- notification_tokens: registered push endpoints per user.
-- provider: "fcm" | "apns" | "webhook"
CREATE TABLE IF NOT EXISTS notification_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token      TEXT    NOT NULL,
    provider   TEXT    NOT NULL,
    label      TEXT    NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, provider, token)
);
CREATE INDEX IF NOT EXISTS idx_notif_tokens_user ON notification_tokens(user_id);

-- notification_prefs: per-user, per-event-type notification rules.
-- camera_id NULL means "all cameras".
-- critical=1 enables iOS Critical Alerts (bypass Do Not Disturb) for this trigger (R9).
CREATE TABLE IF NOT EXISTS notification_prefs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type TEXT    NOT NULL, -- "detection" | "camera.offline" | "*" (any)
    camera_id  INTEGER,          -- NULL = all cameras; FK intentionally not enforced (camera may be deleted)
    enabled    INTEGER NOT NULL DEFAULT 1,
    critical   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, event_type, camera_id)
);
CREATE INDEX IF NOT EXISTS idx_notif_prefs_user ON notification_prefs(user_id);

-- notification_log: delivery tracking per token per event for crash recovery (R9, CG9).
-- On crash recovery: rows with status='pending' older than 5 min are retried on startup.
-- title/body are stored so retry can resend without re-querying the original event.
CREATE TABLE IF NOT EXISTS notification_log (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id     INTEGER,  -- references events(id); nullable (system events have no DB row)
    token_id     INTEGER NOT NULL REFERENCES notification_tokens(id) ON DELETE CASCADE,
    provider     TEXT    NOT NULL,
    title        TEXT    NOT NULL DEFAULT '',
    body         TEXT    NOT NULL DEFAULT '',
    deep_link    TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'pending', -- pending | sent | failed
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    scheduled_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    sent_at      TIMESTAMP,
    created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_notif_log_pending ON notification_log(status, scheduled_at);
CREATE INDEX IF NOT EXISTS idx_notif_log_token   ON notification_log(token_id);
