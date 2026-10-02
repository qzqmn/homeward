-- 「半徑 N 公尺內」查詢需要 geography（單位：公尺）。
-- 要命中下列函式索引，查詢必須使用「完全相同的表達式」，例如：
--   SELECT id FROM cases
--   WHERE ST_DWithin(center::geography,
--                    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,  -- $1=經度 lng, $2=緯度 lat
--                    $3);                                                 -- $3=公尺
-- 原本的 geometry GIST 索引保留，供地圖視窗範圍查詢（&& / ST_MakeEnvelope）使用。
CREATE INDEX IF NOT EXISTS idx_cases_center_geog    ON cases            USING GIST ((center::geography));
CREATE INDEX IF NOT EXISTS idx_tracks_location_geog ON volunteer_tracks USING GIST ((location::geography));
CREATE INDEX IF NOT EXISTS idx_clues_location_geog  ON clues            USING GIST ((location::geography));
