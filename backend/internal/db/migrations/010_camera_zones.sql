-- 010_camera_zones.sql
-- Phase 9 prep: add zones JSON column to cameras table.
-- Zones are stored as a JSON array of objects:
--   [{"id":"z1","name":"Driveway","type":"include","points":[{"x":0.1,"y":0.2},...]}, ...]
-- Default '[]' means no zones configured — detections apply to the full frame.
ALTER TABLE cameras ADD COLUMN zones TEXT NOT NULL DEFAULT '[]'
