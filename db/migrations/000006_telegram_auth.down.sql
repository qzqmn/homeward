ALTER TABLE users DROP CONSTRAINT IF EXISTS users_identity_present;
-- 注意：若已有 telegram_id 為唯一登入方式（phone_e164 為 NULL）的使用者，
-- 下一行會失敗，需先補上這些使用者的 phone_e164 或刪除該帳號。
ALTER TABLE users ALTER COLUMN phone_e164 SET NOT NULL;
ALTER TABLE users DROP COLUMN IF EXISTS telegram_id;
