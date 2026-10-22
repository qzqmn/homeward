package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SettingsRepo 存取 app_settings（管理後台可動態修改的營運設定）。
// 目前使用的 key（呼叫端註釋常數）：
//   - resend_api_key：Resend 的 API key（寄 Email 通知與 Email OTP 驗證碼）
//   - resend_from：寄信來源（需在 Resend 驗證過的網域，如 alert@688689.xyz）
//   - notify_webhook_url：通知轉發 webhook（優先於直接發送，給自架 Worker 用）
type SettingsRepo struct{ db *pgxpool.Pool }

func NewSettingsRepo(db *pgxpool.Pool) *SettingsRepo { return &SettingsRepo{db: db} }

const (
	SettingsKeyResendAPIKey  = "resend_api_key"
	SettingsKeyResendFrom    = "resend_from"
	SettingsKeyNotifyWebhook = "notify_webhook_url"
)

// Get 回傳 (值, 是否設定過)。未設定過回 ("", false, nil)；連線錯誤照樣回傳錯誤。
func (r *SettingsRepo) Get(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := r.db.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// Set 覆寫或新增（UPSERT）。空字串視為「刪除這個設定」。
func (r *SettingsRepo) Set(ctx context.Context, key, value string) error {
	if value == "" {
		_, err := r.db.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, key)
		return err
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO app_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, value)
	return err
}
