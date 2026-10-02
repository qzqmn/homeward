-- ============================================================
-- case_updates：案件時間軸（進展、狀態變更、線索、配對、結案）
-- ============================================================
CREATE TABLE IF NOT EXISTS case_updates (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    case_id    UUID NOT NULL REFERENCES cases (id) ON DELETE CASCADE,
    author_id  UUID REFERENCES users (id) ON DELETE SET NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('note', 'status_change', 'clue', 'sighting', 'match', 'system')),
    body       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_case_updates_case_time ON case_updates (case_id, created_at DESC);

-- ============================================================
-- user_channels：使用者的通知渠道（Web Push 訂閱 / Telegram chat id / WhatsApp / Email）
-- ============================================================
CREATE TABLE IF NOT EXISTS user_channels (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    channel     TEXT NOT NULL CHECK (channel IN ('web_push', 'telegram', 'whatsapp', 'email')),
    address     TEXT NOT NULL,                -- push subscription JSON、chat id、電話或 email
    verified_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, channel, address)
);

-- ============================================================
-- notifications：發送佇列與紀錄（配合每日上限、靜音時段、半徑擴大策略）
-- ============================================================
CREATE TABLE IF NOT EXISTS notifications (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    case_id    UUID REFERENCES cases (id) ON DELETE CASCADE,
    channel    TEXT NOT NULL CHECK (channel IN ('web_push', 'telegram', 'whatsapp', 'email')),
    kind       TEXT NOT NULL CHECK (kind IN ('case_nearby', 'case_update', 'clue', 'match', 'system')),
    payload    JSONB NOT NULL DEFAULT '{}'::jsonb,
    status     TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'failed')),
    attempts   SMALLINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_notifications_queue ON notifications (created_at) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS idx_notifications_user_time ON notifications (user_id, created_at DESC);

-- ============================================================
-- reports：舉報（詐騙、騷擾、私隱、垃圾內容）
-- ============================================================
CREATE TABLE IF NOT EXISTS reports (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reporter_id UUID REFERENCES users (id) ON DELETE SET NULL,
    target_type TEXT NOT NULL CHECK (target_type IN ('case', 'clue', 'user')),
    target_id   UUID NOT NULL,                -- cases.id / clues.id / users.id 皆為 UUID
    reason      TEXT NOT NULL CHECK (reason IN ('scam', 'harassment', 'privacy', 'spam', 'other')),
    detail      TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'reviewing', 'actioned', 'dismissed')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_reports_status ON reports (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reports_target ON reports (target_type, target_id);

-- ============================================================
-- sos_events：志願者求助
-- ============================================================
CREATE TABLE IF NOT EXISTS sos_events (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    case_id     UUID REFERENCES cases (id) ON DELETE SET NULL,
    location    GEOMETRY(Point, 4326) NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_sos_open ON sos_events (created_at DESC) WHERE resolved_at IS NULL;

-- ============================================================
-- case_matches：「走失」與「撿到/發現」案件的配對建議
-- ============================================================
CREATE TABLE IF NOT EXISTS case_matches (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    case_id         UUID NOT NULL REFERENCES cases (id) ON DELETE CASCADE,   -- 走失案
    matched_case_id UUID NOT NULL REFERENCES cases (id) ON DELETE CASCADE,   -- 撿到/發現案
    score           REAL NOT NULL DEFAULT 0,                                  -- 規則分數（距離、時間、物種、特徵）
    status          TEXT NOT NULL DEFAULT 'suggested' CHECK (status IN ('suggested', 'confirmed', 'rejected')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (case_id, matched_case_id),
    CHECK (case_id <> matched_case_id)
);
