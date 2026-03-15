-- Migration 015: add per-camera notification cooldown and detection interval
ALTER TABLE cameras ADD COLUMN notification_cooldown_seconds INTEGER NOT NULL DEFAULT 60;
ALTER TABLE cameras ADD COLUMN detection_interval INTEGER NOT NULL DEFAULT 0;
