-- Email 登入：users 增加選填 email（唯一），UpsertByEmail 用。
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_email ON users (email) WHERE email IS NOT NULL;

-- app_settings：管理後台可動態修改的營運設定（Resend API key/寄信地址、通知
-- webhook 等）。刻意放資料庫而不是 .env：改設定不用重啟後端，管理員可在
-- 管理後台自己完成（API key 為 write-only，GET 時不回傳）。
CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
