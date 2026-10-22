DROP INDEX IF EXISTS uq_users_username_lower;

ALTER TABLE users
    DROP COLUMN IF EXISTS password_set_at,
    DROP COLUMN IF EXISTS password_hash,
    DROP COLUMN IF EXISTS username;
