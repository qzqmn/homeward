-- 歸途 Homeward — PostGIS 初始化
-- 座標一律使用 SRID 4326（WGS84 經緯度；ST_MakePoint(經度 lng, 緯度 lat)）

CREATE EXTENSION IF NOT EXISTS postgis;

-- ============================================================
-- a. cases：案件表（尋人 / 尋寵）
-- ============================================================
CREATE TABLE IF NOT EXISTS cases (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_type       TEXT NOT NULL CHECK (case_type IN ('missing_person', 'missing_pet')),
    status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'found', 'closed')),
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    last_seen_at    TIMESTAMPTZ,                         -- 最後目擊時間
    center          GEOMETRY(Point, 4326) NOT NULL,      -- 走失中心點
    search_radius_m INTEGER NOT NULL DEFAULT 3000,       -- 建議搜尋半徑（公尺）
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cases_center ON cases USING GIST (center);  -- 空間索引
CREATE INDEX IF NOT EXISTS idx_cases_status ON cases (status, created_at DESC);

-- ============================================================
-- b. volunteer_tracks：志願者軌跡表（高頻寫入，主鍵用 BIGINT）
-- ============================================================
CREATE TABLE IF NOT EXISTS volunteer_tracks (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    volunteer_id UUID NOT NULL,                          -- 之後可加 FK 指向 users 表
    case_id      UUID NOT NULL REFERENCES cases (id) ON DELETE CASCADE,
    location     GEOMETRY(Point, 4326) NOT NULL,         -- 軌跡點
    accuracy_m   REAL,                                   -- GPS 精度（公尺，可選）
    reported_at  TIMESTAMPTZ NOT NULL DEFAULT now()      -- 上報時間
);
CREATE INDEX IF NOT EXISTS idx_tracks_location  ON volunteer_tracks USING GIST (location);
CREATE INDEX IF NOT EXISTS idx_tracks_case_time ON volunteer_tracks (case_id, reported_at DESC);
CREATE INDEX IF NOT EXISTS idx_tracks_vol_time  ON volunteer_tracks (volunteer_id, reported_at DESC);

-- ============================================================
-- c. clues：線索表
-- ============================================================
CREATE TABLE IF NOT EXISTS clues (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id     UUID NOT NULL REFERENCES cases (id) ON DELETE CASCADE,
    photo_path  TEXT NOT NULL,                           -- 相對 NAS 媒體根目錄，例：2026/09/ab12.jpg
    location    GEOMETRY(Point, 4326) NOT NULL,          -- 線索座標
    note        TEXT NOT NULL DEFAULT '',
    reporter_id UUID,                                    -- 上報者（可匿名）
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_clues_location  ON clues USING GIST (location);
CREATE INDEX IF NOT EXISTS idx_clues_case_time ON clues (case_id, created_at DESC);
