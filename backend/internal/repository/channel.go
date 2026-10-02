package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ChannelRepo struct{ db *pgxpool.Pool }

func NewChannelRepo(db *pgxpool.Pool) *ChannelRepo { return &ChannelRepo{db: db} }

var validChannels = map[string]bool{
	"web_push": true, "telegram": true, "whatsapp": true, "email": true,
}

// Register 新增或更新一個通知渠道（例如 Web Push 訂閱 JSON、Telegram chat id）。
// 同一使用者對同一 (channel, address) 重複註冊時直接視為成功（冪等）。
func (r *ChannelRepo) Register(ctx context.Context, userID, channel, address string) error {
	if !validChannels[channel] {
		return ErrInvalidChannel
	}
	const q = `
		INSERT INTO user_channels (user_id, channel, address)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (user_id, channel, address) DO NOTHING`
	_, err := r.db.Exec(ctx, q, userID, channel, address)
	return err
}
