package repository

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type NotificationRepo struct{ db *pgxpool.Pool }

func NewNotificationRepo(db *pgxpool.Pool) *NotificationRepo { return &NotificationRepo{db: db} }

// EnqueueCaseNearby 幫每位使用者已註冊的每個通知渠道各排一筆「附近有案件」通知。
// 靠 000005 migration 的 uq_notifications_case_nearby 唯一索引防止同一使用者、
// 同一案件被重複推播（例如半徑擴大時，已收過通知的人不會再收到第二次）。
func (r *NotificationRepo) EnqueueCaseNearby(ctx context.Context, caseID string, userIDs []string, radiusM int) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	payload, err := json.Marshal(map[string]any{"radius_m": radiusM})
	if err != nil {
		return 0, err
	}

	const q = `
		INSERT INTO notifications (user_id, case_id, channel, kind, payload)
		SELECT u.id::uuid, $1::uuid, c.channel, 'case_nearby', $2::jsonb
		FROM unnest($3::uuid[]) AS u(id)
		JOIN user_channels c ON c.user_id = u.id
		ON CONFLICT (user_id, case_id, kind) WHERE kind = 'case_nearby' DO NOTHING`
	tag, err := r.db.Exec(ctx, q, caseID, payload, userIDs)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

type QueuedNotification struct {
	ID       int64
	UserID   string
	Channel  string
	Address  string // user_channels.address：push subscription JSON / telegram chat id / 電話 / email
	Kind     string
	Payload  []byte
	Attempts int16
}

// FetchDueForSend 取出尚待送出、且該使用者「今天」送出次數未達上限的通知（防通知疲勞）。
// 一次只鎖 batchSize 筆，供背景 worker 分批處理。
func (r *NotificationRepo) FetchDueForSend(ctx context.Context, dailyCapPerUser, batchSize int) ([]QueuedNotification, error) {
	const q = `
		SELECT n.id, n.user_id::text, n.channel, uc.address, n.kind, n.payload, n.attempts
		FROM notifications n
		JOIN user_channels uc ON uc.user_id = n.user_id AND uc.channel = n.channel
		WHERE n.status = 'queued'
		  AND n.attempts < 5
		  AND (
		        SELECT count(*) FROM notifications sent
		        WHERE sent.user_id = n.user_id AND sent.status = 'sent'
		          AND sent.sent_at >= date_trunc('day', now())
		      ) < $1
		ORDER BY n.created_at
		LIMIT $2
		FOR UPDATE OF n SKIP LOCKED`
	rows, err := r.db.Query(ctx, q, dailyCapPerUser, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []QueuedNotification
	for rows.Next() {
		var n QueuedNotification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Channel, &n.Address, &n.Kind, &n.Payload, &n.Attempts); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *NotificationRepo) MarkSent(ctx context.Context, id int64) error {
	const q = `UPDATE notifications SET status = 'sent', sent_at = now() WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id)
	return err
}

func (r *NotificationRepo) MarkFailed(ctx context.Context, id int64) error {
	const q = `UPDATE notifications SET status = 'failed', attempts = attempts + 1 WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id)
	return err
}
