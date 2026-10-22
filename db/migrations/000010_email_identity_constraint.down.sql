-- 回到 000006 的原始定義（email 不算身份識別；000009 之後才有的功能回不去）。
-- 注意：這只會在某一段歷史可能存在的部署上執行，極少用到。
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_identity_present;

ALTER TABLE users
    ADD CONSTRAINT users_identity_present
    CHECK (phone_e164 IS NOT NULL OR telegram_id IS NOT NULL);
