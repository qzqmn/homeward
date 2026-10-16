-- 通知系統修正：
-- 1) uq_notifications_case_nearby 原本只涵蓋 (user_id, case_id, kind)，同一使用者
--    同案件的不同渠道會被 ON CONFLICT DO NOTHING 默默丟掉，導致一人註冊多個
--    通知渠道（telegram + web_push）時只會收到其中一條。改成 channel 一併納入
--    唯一索引，每個渠道各排一筆。
-- 2) kind='clue'（案主收新線索通知）刻意「不」建去重索引：每一則新線索都是獨立
--    的通知事件，不應被 (user, case, channel) 的唯一性吃掉；防重複由佇列的
--    MarkSent／排程單 worker 保證。
-- 3) notifications.case_id 與 sos_events.case_id 的 FK 帶 ON DELETE CASCADE / SET NULL，
--    但 Postgres 不會替 FK 自動建索引——結案刪除（或日後清理）案件時的 cascade
--    會掃全表，補上索引。
DROP INDEX IF EXISTS uq_notifications_case_nearby;
CREATE UNIQUE INDEX uq_notifications_case_nearby
    ON notifications (user_id, case_id, channel, kind) WHERE kind = 'case_nearby';

CREATE INDEX IF NOT EXISTS idx_notifications_case_id ON notifications (case_id);
CREATE INDEX IF NOT EXISTS idx_sos_events_case_id    ON sos_events (case_id);
