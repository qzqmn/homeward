package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured：尚未在管理後台設定寄信相關設定（Resend key/from）時，
// Email 寄送一律回這個錯——handler 依它回 503，而不是 500。
var ErrNotConfigured = errors.New("mailer: not configured")

// resendEndpoint 是 Resend 的寄信 API（POST /emails，https://resend.com/docs）。
const resendEndpoint = "https://api.resend.com/emails"

// telegramAPI 是 Telegram Bot API 的基底網址（token 由 .env 提供，只拿來發訊息）。
const telegramAPI = "https://api.telegram.org"

// Mailer 把「直接發送」的通道集中在這裡：Email 走 Resend REST API、Telegram 走
// 官方 Bot API。Telegram 的 bot token 已在 .env（登入簽章驗證同一個 token），
// 權限上同一個 bot 能 sendMessage 就能發通知，不再需要自架 webhook Worker。
type Mailer struct {
	settings   *SettingsService
	botToken   string
	httpClient *http.Client
}

func NewMailer(settings *SettingsService, botToken string) *Mailer {
	return &Mailer{
		settings:   settings,
		botToken:   botToken,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// SendEmail 用 Resend 寄一封信（單收件人）。設定不完整回 ErrNotConfigured。
// Resend 免費額度 100 封/天，足夠平台通知 + OTP 的輕量使用。
func (m *Mailer) SendEmail(ctx context.Context, to, subject, text string) error {
	snap := m.settings.Snapshot(ctx)
	if !snap.ConfiguredResend() {
		return ErrNotConfigured
	}

	body, err := json.Marshal(map[string]any{
		"from":    snap.ResendFrom,
		"to":      []string{to},
		"subject": subject,
		"text":    text,
	})
	if err != nil {
		return fmt.Errorf("build resend body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+snap.ResendAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call resend: %w", err)
	}
	defer resp.Body.Close()

	// 非成功時把 Resend 的回應內容讀出來（它的錯誤訊息會直接說明原因，
	// 例如「You can only send testing emails to your own address」、域名未驗證等）。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("resend returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	io.Copy(io.Discard, resp.Body) // 清空 body 以便連線重用
	return nil
}

// SendTelegram 用官方 Bot API 發訊息。收件人address是 user_channels 裡存的
// chat id。bot 需要「使用者主動 /start 過」才能發訊息（Telegram 規則），
// 發送失敗（403 等）照一般通知重試邏輯處理。
func (m *Mailer) SendTelegram(ctx context.Context, chatID, text string) error {
	if m.botToken == "" {
		return ErrNotConfigured
	}

	body, err := json.Marshal(map[string]any{
		"chat_id": strings.TrimSpace(chatID),
		"text":    text,
	})
	if err != nil {
		return fmt.Errorf("build telegram body: %w", err)
	}

	url := telegramAPI + "/bot" + m.botToken + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call telegram: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// SendOTPEmail 寄 Email 登入的 6 位驗證碼。正式環境不要把 code 印進 log。
func (m *Mailer) SendOTPEmail(ctx context.Context, to, code string) error {
	return m.SendEmail(
		ctx,
		to,
		"【歸途 Homeward】登入驗證碼",
		"你的登入驗證碼是："+code+"\n\n此驗證碼 5 分鐘內有效，若非本人操作請忽略這封信。",
	)
}
