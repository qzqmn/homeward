DROP INDEX IF EXISTS uq_notifications_case_nearby;
DROP INDEX IF EXISTS idx_users_watch_center;
ALTER TABLE users DROP COLUMN IF EXISTS watch_radius_m, DROP COLUMN IF EXISTS watch_center;
ALTER TABLE cases DROP COLUMN IF EXISTS photo_path;
