-- 支援 Telegram Login 作為登入方式：phone_e164 改為選填（之後若要加真正的
-- 簡訊/WhatsApp 驗證，原本的 OTP 流程還在，兩種方式可以並存），新增
-- telegram_id 作為另一種身份識別。至少要有一種登入方式。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS telegram_id BIGINT UNIQUE,
    ALTER COLUMN phone_e164 DROP NOT NULL;

ALTER TABLE users
    ADD CONSTRAINT users_identity_present CHECK (phone_e164 IS NOT NULL OR telegram_id IS NOT NULL);
