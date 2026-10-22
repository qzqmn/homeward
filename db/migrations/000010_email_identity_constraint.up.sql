-- Email 登入的收尾：000009 加了 users.email 欄位，但漏更新 000006 建的
-- users_identity_present 約束（要求 phone/telegram 至少一種），純 Email
-- 註冊的新使用者 INSERT 會被 CHECK 擋掉（SQLSTATE 23514）。這裡重建約束，
-- 把 email 也算進「至少一種身份識別」。
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_identity_present;

ALTER TABLE users
    ADD CONSTRAINT users_identity_present
    CHECK (phone_e164 IS NOT NULL OR telegram_id IS NOT NULL OR email IS NOT NULL);
