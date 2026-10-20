-- 名字取得提示：索引列在 down 時會隨欄位刪除而自動消失（依賴該欄位），
-- 但 Postgres 對 IF EXISTS 的 column drop 會自動移除依賴索引，只需 drop column。
ALTER TABLE users
    DROP COLUMN IF EXISTS volunteer_requested_at;
