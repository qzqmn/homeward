package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type NotificationRepo struct{ db *pgxpool.Pool }

func NewNotificationRepo(db *pgxpool.Pool) *NotificationRepo { return &NotificationRepo{db: db} }

// EnqueueCaseNearby 幫每位使用者已註冊的每個通知渠道各排一筆「附近有案件」通知。
// 靠 000007 migration 的 uq_notifications_case_nearby 唯一索引（含 channel）防止同一
// 使用者、同一案件被重複推播（例如半徑擴大時，已收過通知的人不會再收到第二次），
// 同時不同渠道各自保留一筆。
func (r *NotificationRepo) EnqueueCaseNearby(ctx context.Context, caseID string, userIDs []string, payload []byte) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	const q = `
		INSERT INTO notifications (user_id, case_id, channel, kind, payload)
		SELECT u.id::uuid, $1::uuid, c.channel, 'case_nearby', $2::jsonb
		FROM unnest($3::uuid[]) AS u(id)
		JOIN user_channels c ON c.user_id = u.id
		ON CONFLICT (user_id, case_id, channel, kind) WHERE kind = 'case_nearby' DO NOTHING`
	tag, err := r.db.Exec(ctx, q, caseID, payload, userIDs)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// EnqueueToUser 把一則通知排給「某一位使用者」的所有已註冊渠道（例如案主的新線索通知）。
// kind 必須是 notifications 表 CHECK 允許、且「沒有」去重唯一索引的種類——
// 線索通知屬於重複發生的事件（每一則新線索都該通知一次），由呼叫端單次呼叫保證
// 不重複，所以這裡用動態 SQL 組 kind。傳入未列於白名單的 kind 會回傳錯誤。
func (r *NotificationRepo) EnqueueToUser(ctx context.Context, userID, caseID, kind string, payload []byte) (int, error) {
	var channelKind string
	switch kind {
	case "clue", "case_update", "match", "system":
		channelKind = kind
	default:
		return 0, ErrInvalidNotificationKind
	}

	const q = `
		INSERT INTO notifications (user_id, case_id, channel, kind, payload)
		SELECT $1::uuid, CASE WHEN $2 = '' THEN NULL ELSE $2::uuid END, c.channel, $3::text, $4::jsonb
		FROM user_channels c WHERE c.user_id = $1::uuid`
	tag, err := r.db.Exec(ctx, q, userID, caseID, channelKind, payload)
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

// MarkSendFailed 處理發送失敗：只遞增 attempts，狀態維持 queued 讓下一次排程重試；
// 達到 maxSendAttempts 次才標記 failed（不再被 FetchDueForSend 取出）。
// 之前的寫法（第一次失敗就直接 failed）會讓 webhook 短暫逾時就永久丟掉通知。
func (r *NotificationRepo) MarkSendFailed(ctx context.Context, id int64) error {
	const q = `
		UPDATE notifications
		SET attempts = attempts + 1,
		    status   = CASE WHEN attempts + 1 >= 5 THEN 'failed' ELSE 'queued' END
		WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id)
	return err
}
