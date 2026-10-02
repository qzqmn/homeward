-- ============================================================
-- users：手機 OTP 登入（OTP 本身存 Redis，不入庫）
-- ============================================================
CREATE TABLE IF NOT EXISTS users (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone_e164            TEXT NOT NULL UNIQUE,            -- 例：+85291234567
    display_name          TEXT NOT NULL DEFAULT '',
    locale                TEXT NOT NULL DEFAULT 'zh-HK' CHECK (locale IN ('zh-HK', 'en')),
    role                  TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    volunteer_verified_at TIMESTAMPTZ,                     -- 非 NULL = 認證志願者（可看精確位置）
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at            TIMESTAMPTZ                      -- 帳號刪除請求（私隱條例：不再需要即應刪除）
);

-- ============================================================
-- cases：擁有者、報案編號、公開模糊位置、新增「撿到/發現」類型
-- ============================================================
ALTER TABLE cases
    ADD COLUMN IF NOT EXISTS owner_id         UUID REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS police_report_no TEXT,        -- 尋人案：選填報案編號
    -- 公開頁面只用 public_center：把座標吸附到約 550m 的格網中心（固定值，無法靠重整取平均還原）
    -- 精確的 center 只提供給案主與認證志願者，API 層必須做權限判斷
    ADD COLUMN IF NOT EXISTS public_center GEOMETRY(Point, 4326)
        GENERATED ALWAYS AS (ST_SnapToGrid(center, 0.0025, 0.0025, 0.005, 0.005)) STORED;

ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_case_type_check;
ALTER TABLE cases ADD CONSTRAINT cases_case_type_check
    CHECK (case_type IN ('missing_person', 'missing_pet', 'found_person', 'found_pet'));

CREATE INDEX IF NOT EXISTS idx_cases_owner ON cases (owner_id);

-- ============================================================
-- clues：狀態分級、實際目擊時間、離線重送的冪等鍵
-- ============================================================
ALTER TABLE clues
    ADD COLUMN IF NOT EXISTS status     TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'credible', 'dismissed')),   -- 待確認 / 可信 / 已排除
    ADD COLUMN IF NOT EXISTS sighted_at TIMESTAMPTZ NOT NULL DEFAULT now(),  -- 客戶端記錄的目擊時間（離線上傳時 != created_at）
    ADD COLUMN IF NOT EXISTS client_id  UUID UNIQUE;               -- 客戶端產生；重送時 INSERT ... ON CONFLICT (client_id) DO NOTHING

ALTER TABLE clues
    ADD CONSTRAINT fk_clues_reporter FOREIGN KEY (reporter_id) REFERENCES users (id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_clues_case_status ON clues (case_id, status);

-- ============================================================
-- volunteer_tracks：客戶端記錄時間、冪等鍵、關聯 users
-- ============================================================
ALTER TABLE volunteer_tracks
    ADD COLUMN IF NOT EXISTS recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),  -- 客戶端定位時間；reported_at 為伺服器收到時間
    ADD COLUMN IF NOT EXISTS client_id   UUID UNIQUE;

ALTER TABLE volunteer_tracks
    ADD CONSTRAINT fk_tracks_volunteer FOREIGN KEY (volunteer_id) REFERENCES users (id) ON DELETE CASCADE;
