-- 搜索任務清單/斷點續傳：把一次搜索切成可認領的小任務，志願者認領 ->
-- 回報進度（文字 + 當前位置）-> 完成。換人/隔天回來從 last_point 續搜，
-- 不用重複走別人已走過的範圍。
CREATE TABLE IF NOT EXISTS search_tasks (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    case_id    UUID NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    note       TEXT,
    status     TEXT NOT NULL DEFAULT 'open',  -- open | claimed | done
    claimed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    claimed_at TIMESTAMPTZ,
    progress_note TEXT,
    last_point GEOGRAPHY(POINT),              -- 志願者最後回報的位置（斷點）
    done_at    TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT search_tasks_status CHECK (status IN ('open', 'claimed', 'done'))
);

CREATE INDEX IF NOT EXISTS idx_search_tasks_case ON search_tasks (case_id, status, created_at);
