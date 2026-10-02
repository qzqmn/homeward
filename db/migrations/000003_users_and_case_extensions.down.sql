ALTER TABLE volunteer_tracks DROP CONSTRAINT IF EXISTS fk_tracks_volunteer;
ALTER TABLE volunteer_tracks DROP COLUMN IF EXISTS client_id, DROP COLUMN IF EXISTS recorded_at;

ALTER TABLE clues DROP CONSTRAINT IF EXISTS fk_clues_reporter;
DROP INDEX IF EXISTS idx_clues_case_status;
ALTER TABLE clues DROP COLUMN IF EXISTS client_id, DROP COLUMN IF EXISTS sighted_at, DROP COLUMN IF EXISTS status;

DROP INDEX IF EXISTS idx_cases_owner;
-- 注意：若已存在 found_* 類型的案件，下一行會失敗，需先處理這些資料
ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_case_type_check;
ALTER TABLE cases ADD CONSTRAINT cases_case_type_check CHECK (case_type IN ('missing_person', 'missing_pet'));
ALTER TABLE cases DROP COLUMN IF EXISTS public_center, DROP COLUMN IF EXISTS police_report_no, DROP COLUMN IF EXISTS owner_id;

DROP TABLE IF EXISTS users;
