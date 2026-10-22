-- 帳號密碼登入：用戶名與密碼。註冊續以 Email 驗證碼（000010 之前流程不變），
-- 之後用 用戶名/email + 密碼 登入。兩者皆選填——沒密碼的使用者仍可用
-- Email 驗證碼或 Telegram 登入（忘記密碼的後備通路）。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS username      TEXT,
    ADD COLUMN IF NOT EXISTS password_hash TEXT,
    ADD COLUMN IF NOT EXISTS password_set_at TIMESTAMPTZ;

-- 用戶名唯一（不分大小寫；4–20 字元在應用層驗證，這裡只卡唯一性）
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_username_lower
    ON users (lower(username)) WHERE username IS NOT NULL;
