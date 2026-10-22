package service

// Web Push（瀏覽器推播）：VAPID 金鑰對第一次啟動時自動生成、存 app_settings，
// 公鑰讓瀏覽器 pushManager.subscribe 用（經 GET /config 下發）、私鑰簽每次推播。
// 訂閱 JSON 存 user_channels（channel='web_push'，address=訂閱 JSON），
// 發送時由 NotifyService 呼叫 Send。404/410（訂閱已失效）回 ErrPushGone，
// 呼叫端把死訂閱刪掉，通知留在佇列等下個有效渠道。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"homeward/internal/repository"
)

var ErrPushGone = errors.New("webpush: subscription gone (404/410)")

// tungsten TTL：推送過期時間（秒）；瀏覽器端在期限後可拒收。1 小時足夠
// 「即時案件推播」的時效需求。
const pushTTLSeconds = 3600

type WebPushService struct {
	settings   *SettingsService
	httpClient *http.Client
}

func NewWebPushService(settings *SettingsService) *WebPushService {
	return &WebPushService{
		settings:   settings,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// EnsureKeys 首次啟動生成 VAPID 金鑰對並存進 app_settings（已有就跳過）。
// 在 server 啟動流程呼叫一次；生成失敗僅記 log，之後每次發送會再補生成。
func (w *WebPushService) EnsureKeys(ctx context.Context) error {
	pub, okPub, err := w.settings.repo.Get(ctx, repository.SettingsKeyWebPushPublic)
	if err != nil {
		return fmt.Errorf("read webpush key: %w", err)
	}
	priv, okPriv, err := w.settings.repo.Get(ctx, repository.SettingsKeyWebPushPrivate)
	if err != nil {
		return fmt.Errorf("read webpush key: %w", err)
	}
	if okPub && okPriv && pub != "" && priv != "" {
		return nil
	}
	newPriv, newPub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return fmt.Errorf("generate vapid keys: %w", err)
	}
	if err := w.settings.Set(ctx, repository.SettingsKeyWebPushPublic, newPub); err != nil {
		return err
	}
	if err := w.settings.Set(ctx, repository.SettingsKeyWebPushPrivate, newPriv); err != nil {
		return err
	}
	log.Println("web push: generated new VAPID key pair into app_settings")
	return nil
}

// PublicKey 回傳 VAPID 公鑰（前端訂閱用；公鑰本來就是公開的）。讀不到回空字串。
func (w *WebPushService) PublicKey(ctx context.Context) string {
	v, _, err := w.settings.repo.Get(ctx, repository.SettingsKeyWebPushPublic)
	if err != nil {
		return ""
	}
	return v
}

// pushPayload 是發給 Service Worker 的 JSON 內容。
type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"` // 點通知要打開的頁面（share_url）
}

// Send 發送一則推播。subscriptionJSON 是 user_channels.address 原值
// （pushManager.subscribe() 回傳的 JSON）。
func (w *WebPushService) Send(ctx context.Context, subscriptionJSON, kind string, payload []byte) error {
	pub, okPub, err := w.settings.repo.Get(ctx, repository.SettingsKeyWebPushPublic)
	if err != nil {
		return fmt.Errorf("read vapid key: %w", err)
	}
	priv, okPriv, err := w.settings.repo.Get(ctx, repository.SettingsKeyWebPushPrivate)
	if err != nil {
		return fmt.Errorf("read vapid key: %w", err)
	}
	if !okPub || !okPriv || pub == "" || priv == "" {
		return ErrNotConfigured // 沒金鑰＝瀏覽器推播未啟用，走 webhook 後備
	}

	var sub webpush.Subscription
	if err := json.Unmarshal([]byte(subscriptionJSON), &sub); err != nil || sub.Endpoint == "" {
		return fmt.Errorf("bad subscription json: %w", err)
	}

	// 通知文案：標題/內文按 kind 組中文摘要；點開的頁面用 payload 的 share_url。
	subject, text := resolveEmailContent(kind, payload)
	var shareURL string
	var pl map[string]any
	if json.Unmarshal(payload, &pl) == nil {
		if u, ok := pl["share_url"].(string); ok {
			shareURL = u
		}
	}
	body, err := json.Marshal(pushPayload{Title: subject, Body: text, URL: shareURL})
	if err != nil {
		return fmt.Errorf("marshal push payload: %w", err)
	}

	resp, err := webpush.SendNotificationWithContext(ctx, body, &sub, &webpush.Options{
		VAPIDPublicKey:  pub,
		VAPIDPrivateKey: priv,
		TTL:             pushTTLSeconds,
		HTTPClient:      w.httpClient,
	})
	if err != nil {
		return fmt.Errorf("webpush send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrPushGone // 讓 NotifyService 刪掉這個死訂閱
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	default:
		return fmt.Errorf("webpush HTTP %d", resp.StatusCode)
	}
}
