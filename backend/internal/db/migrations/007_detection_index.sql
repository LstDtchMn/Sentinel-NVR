-- 007_detection_index.sql
-- Adds a composite covering index on events(camera_id, type, start_time) to
-- speed up the List query when filtering by camera, event type, and date range.
-- The existing idx_events_camera_time covers (camera_id, start_time) but cannot
-- satisfy an equality filter on type using the index alone.

CREATE INDEX IF NOT EXISTS idx_events_camera_type_time ON events(camera_id, type, start_time);
