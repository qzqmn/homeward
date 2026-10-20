-- 志願者認證流程：使用者主動「申請」成為志願者，管理員（users.role='admin'）在
-- 審核清單裡確認後，volunteer_verified_at 才會被寫上（可看精確位置/線索）。
-- role 欄位早在 000003 就已存在，這裡只補「申請時間」。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS volunteer_requested_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_users_volunteer_requested
    ON users (volunteer_requested_at DESC)
    WHERE volunteer_requested_at IS NOT NULL AND volunteer_verified_at IS NULL;
