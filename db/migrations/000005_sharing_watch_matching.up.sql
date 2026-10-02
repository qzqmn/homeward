-- 案件封面照（分享用 OG 圖／海報的來源圖片）。走失中心點已模糊化，
-- 但案主主動上傳的照片本來就是要公開讓人辨認，不受模糊化規則影響。
ALTER TABLE cases ADD COLUMN IF NOT EXISTS photo_path TEXT;

-- 使用者的「關注區域」：用於半徑推播比對，而非志願者搜索當下的即時位置
-- （volunteer_tracks 仍是搜索模式當下的軌跡，用途不同，予以保留）。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS watch_center    GEOMETRY(Point, 4326),
    ADD COLUMN IF NOT EXISTS watch_radius_m  INTEGER;
CREATE INDEX IF NOT EXISTS idx_users_watch_center ON users USING GIST ((watch_center::geography))
    WHERE watch_center IS NOT NULL;

-- 避免半徑擴大後對同一使用者、同一案件重複推播「附近有案件」通知
CREATE UNIQUE INDEX IF NOT EXISTS uq_notifications_case_nearby
    ON notifications (user_id, case_id, kind) WHERE kind = 'case_nearby';
