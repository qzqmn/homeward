-- 一案多張照片：封面照（cases.photo_path）之外，案主/認證志願者可以補傳
-- 現場對比照（例如目擊、海報、監視器截圖）。上傳走同一個 upload 管道
-- （重新編碼剝 EXIF），photo_path 格式與 photoPathRe 一致。
CREATE TABLE IF NOT EXISTS case_photos (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    case_id     UUID NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    photo_path  TEXT NOT NULL,
    uploaded_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_case_photos_case ON case_photos (case_id, created_at);
