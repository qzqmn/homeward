DROP INDEX IF EXISTS idx_sos_events_case_id;
DROP INDEX IF EXISTS idx_notifications_case_id;
DROP INDEX IF EXISTS uq_notifications_case_nearby;
CREATE UNIQUE INDEX uq_notifications_case_nearby
    ON notifications (user_id, case_id, kind) WHERE kind = 'case_nearby';
