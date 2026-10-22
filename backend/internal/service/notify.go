package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"homeward/internal/repository"
)

// radiusSteps 定義「防通知疲勞」的半徑擴大策略：案件剛建立時只通知最近的使用者，
// 若過了一段時間仍是 open，才逐步擴大範圍找更多志願者，避免一開案就轟炸一大片人。
var radiusSteps = []struct {
	After  time.Duration
	Meters int
}{
	{After: 0, Meters: 1500},
	{After: 2 * time.Hour, Meters: 3000},
	{After: 6 * time.Hour, Meters: 5000},
}

// quietHourStart/End：香港時間 23:00–07:00 不新發通知（已排進佇列的留到早上一起送）。
const quietHourStart, quietHourEnd = 23, 7

const dailyCapPerUser = 5 // 同一使用者每天最多收到幾則「附近有案件」通知

// NearbyCase 是排「附近有案件」通知時需要的案件資訊。
// payload 裡放案件標題／分享連結／封面照網址，webhook 那端的 Worker 才知道
// 要通知使用者「什麼」（之前 payload 只有 radius_m，Worker 無從得知通知內容）。
type NearbyCase struct {
	ID        string
	CaseType  string
	Title     string
	PhotoPath *string
}

// NotifyService 負責半徑推播的佇列寫入，以及把到期的通知實際送出（見 sendViaChannel）。
// 送達優先順序：Email → Resend 直接寄；Telegram → Bot API 直接寄；有設定
// webhook（管理後台或 .env）則轉送給自架 Worker；什麼都沒有只印 log（開發）。
type NotifyService struct {
	users         *repository.UserRepo
	notifications *repository.NotificationRepo
	settings      *SettingsService
	mailer        *Mailer
	loc           *time.Location // 靜音時段判斷（香港時間）
	envWebhookURL string         // .env NOTIFY_WEBHOOK_URL（相容既有部署；管理後台設定優先）
	baseURL       string         // 對外網域，組 share_url 塞進 payload 用
	httpClient    *http.Client
	// web_push 直發（瀏覽器推播；nil 時 web_push 退回 webhook 後備）
	push *WebPushService
	// 死推播訂閱清理（404/410 時刪 user_channels）；nil 時只保留佇列重試
	channels *repository.ChannelRepo
}

func NewNotifyService(users *repository.UserRepo, notifications *repository.NotificationRepo, settings *SettingsService, mailer *Mailer, envWebhookURL, baseURL string) *NotifyService {
	loc, err := time.LoadLocation("Asia/Hong_Kong")
	if err != nil {
		loc = time.UTC // 容器內若缺時區資料庫，退回 UTC，避免啟動失敗（僅影響靜音時段判斷）
	}
	return &NotifyService{
		users:         users,
		notifications: notifications,
		settings:      settings,
		mailer:        mailer,
		loc:           loc,
		envWebhookURL: envWebhookURL,
		baseURL:       baseURL,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

// WithWebPush 啟用瀏覽器推播直發（router 組裝時呼叫）。
func (s *NotifyService) WithWebPush(push *WebPushService, channels *repository.ChannelRepo) *NotifyService {
	s.push = push
	s.channels = channels
	return s
}

// EscalateAndEnqueue 依案件建立至今的經過時間，找出目前該用的半徑，為範圍內、
// 尚未收過通知的「關注該區域」使用者各排一則通知。建案當下與之後的排程 worker
// 都呼叫這支函式；已通知過的人不會因為半徑擴大而重複收到（見 repository 層的唯一索引）。
func (s *NotifyService) EscalateAndEnqueue(ctx context.Context, cs repository.EscalationCandidate) error {
	radius := currentRadius(time.Since(cs.CreatedAt))
	watchers, err := s.users.FindWatchersInRadius(ctx, cs.ID, cs.Lng, cs.Lat, radius)
	if err != nil {
		return fmt.Errorf("find watchers: %w", err)
	}
	if len(watchers) == 0 {
		return nil
	}

	userIDs := make([]string, len(watchers))
	for i, w := range watchers {
		userIDs[i] = w.UserID
	}
	payload, err := json.Marshal(s.caseNearbyPayload(cs.ID, cs.CaseType, cs.Title, cs.PhotoPath, radius))
	if err != nil {
		return fmt.Errorf("marshal case_nearby payload: %w", err)
	}
	_, err = s.notifications.EnqueueCaseNearby(ctx, cs.ID, userIDs, payload)
	return err
}

// NotifyOwnerOfClue 在新線索建立後，通知案主「有人回報目擊」。每一則新線索都會
// 排一次（這是重複事件，靠 kind='clue' 沒有去重索引），payload 帶案件與線索摘要，
// 讓 webhook 那端知道要送什麼內容。沒有案主（理論上不可能，建案一律登入）就略過。
func (s *NotifyService) NotifyOwnerOfClue(ctx context.Context, cs repository.Case, clue repository.Clue) error {
	if cs.OwnerID == nil {
		return nil
	}

	payload := map[string]any{
		"case_id":     cs.ID,
		"case_type":   cs.CaseType,
		"case_title":  cs.Title,
		"share_url":   s.baseURL + "/c/" + cs.ID,
		"clue_id":     clue.ID,
		"clue_note":   truncateRunes(clue.Note, 100),
		"clue_lng":    clue.Lng,
		"clue_lat":    clue.Lat,
		"sighted_at":  clue.SightedAt,
		"clue_status": clue.Status,
	}
	if clue.PhotoPath != "" {
		payload["clue_photo_url"] = "/media/" + clue.PhotoPath
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal clue payload: %w", err)
	}

	_, err = s.notifications.EnqueueToUser(ctx, *cs.OwnerID, cs.ID, "clue", raw)
	return err
}

// caseNearbyPayload 組「附近有案件」通知的內容。payload 帶案件標題、類型、
// 分享網址與封面照相對路徑，webhook 那端的 Worker 才知道要通知使用者「什麼」。
// radius_m 欄位名維持不變，讓已上線的 webhook Worker 不會突然讀不到。
func (s *NotifyService) caseNearbyPayload(caseID, caseType, caseTitle string, photoPath *string, radiusM int) map[string]any {
	payload := map[string]any{
		"radius_m":   radiusM,
		"case_id":    caseID,
		"case_type":  caseType,
		"case_title": caseTitle,
		"share_url":  s.baseURL + "/c/" + caseID,
	}
	if photoPath != nil && *photoPath != "" {
		payload["photo_url"] = "/media/" + *photoPath
	}
	return payload
}

func currentRadius(elapsed time.Duration) int {
	radius := radiusSteps[0].Meters
	for _, step := range radiusSteps {
		if elapsed >= step.After {
			radius = step.Meters
		}
	}
	return radius
}

// DispatchQueued 從佇列取出一批「尚未達每日上限」的通知並嘗試送出；
// 設計成由背景排程（time.Ticker）定期呼叫，不在 HTTP 請求流程裡執行。
func (s *NotifyService) DispatchQueued(ctx context.Context, batchSize int) (sent, failed int, err error) {
	if s.inQuietHours(time.Now()) {
		return 0, 0, nil // 靜音時段：留在佇列，下次排程再處理
	}

	due, err := s.notifications.FetchDueForSend(ctx, dailyCapPerUser, batchSize)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch due notifications: %w", err)
	}

	for _, n := range due {
		if sendErr := s.sendViaChannel(ctx, n.UserID, n.Channel, n.Address, n.Kind, n.Payload); sendErr != nil {
			_ = s.notifications.MarkSendFailed(ctx, n.ID)
			failed++
			continue
		}
		if err := s.notifications.MarkSent(ctx, n.ID); err != nil {
			return sent, failed, fmt.Errorf("mark sent: %w", err)
		}
		sent++
	}
	return sent, failed, nil
}

func (s *NotifyService) inQuietHours(t time.Time) bool {
	hour := t.In(s.loc).Hour()
	return hour >= quietHourStart || hour < quietHourEnd
}

// webhookEnvelope 是 POST 給 webhookURL 的 JSON 內容。payload 直接放
// notifications.payload 這個 JSONB 欄位的原始內容（含案件標題、分享網址等），
// 用 json.RawMessage 讓它原樣內嵌，不會被二次編碼成字串。
type webhookEnvelope struct {
	Channel string          `json:"channel"` // "telegram"／"web_push"／"whatsapp"／"email"
	Address string          `json:"address"` // user_channels.address：Telegram chat id／push 訂閱 JSON／電話／email
	Kind    string          `json:"kind"`    // "case_nearby" 等通知種類
	Payload json.RawMessage `json:"payload"`
}

// sendViaChannel 決定這則通知實際怎麼送出：
//  1. channel=email 且 Resend 已在管理後台設定好 → 直接寄（SendEmail）。
//  2. channel=telegram 且 .env 有 bot token → 走 Bot API 直接發（不用自架 Worker）。
//  3. channel=web_push 且 VAPID 金鑰已生成 → webpush 協議直發；
//     訂閱失效（404/410）就把死訂閱從 user_channels 刪掉並視為已處理。
//  4. 有 webhook（管理後台的 notify_webhook_url，或 .env 的 NOTIFY_WEBHOOK_URL）
//     → POST 給自架 Worker，由那端轉發（相容既有部署，也是 whatsapp 的出路）。
//  5. 什麼都沒有 → 印 log（本機開發；正式環境不會走到這裡）。
//
// 只看 HTTP 狀態碼判斷成功與否：2xx 視為送達，其餘（含逾時、連線失敗）一律
// 當作失敗，讓這筆通知留在佇列裡等下次重試。
func (s *NotifyService) sendViaChannel(ctx context.Context, userID, channel, address, kind string, payload []byte) error {
	snap := s.settings.Snapshot(ctx)

	switch {
	case channel == "email" && snap.ConfiguredResend():
		subject, text := resolveEmailContent(kind, payload)
		return s.mailer.SendEmail(ctx, address, subject, text)

	case channel == "telegram" && s.mailer != nil && s.mailer.botToken != "":
		subject, text := resolveEmailContent(kind, payload)
		return s.mailer.SendTelegram(ctx, address, subject+"\n"+text)

	case channel == "web_push" && s.push != nil:
		err := s.push.Send(ctx, address, kind, payload)
		if errors.Is(err, ErrPushGone) {
			// 訂閱已被瀏覽器/訊息服務註銷：刪掉死訂閱，通知視為送出
			//（該使用者若有 email/telegram 渠道，同一案件的佇列會再送一份）。
			if s.channels != nil && userID != "" {
				_ = s.channels.Delete(ctx, userID, channel, address)
			}
			return nil
		}
		return err
	}

	webhook := snap.WebhookURL
	if webhook == "" {
		webhook = s.envWebhookURL
	}
	if webhook == "" {
		// 收件地址屬個資（chat id／電話／email），只印渠道與 kind，
		// payload 內容也不印（案件標題同屬個資）。
		fmt.Printf("[DEV] notify via %s kind=%s (payload omitted, no resend/webhook configured)\n", channel, kind)
		return nil
	}
	return s.sendWebhook(ctx, webhook, channel, address, kind, payload)
}

// sendWebhook 保留原本的 webhook 轉發路徑（.env 或管理後台設定的網址）。
func (s *NotifyService) sendWebhook(ctx context.Context, webhookURL, channel, address, kind string, payload []byte) error {
	body, err := json.Marshal(webhookEnvelope{Channel: channel, Address: address, Kind: kind, Payload: payload})
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// resolveEmailContent 把通知 payload 轉成人手機/信箱可讀的主旨與內文。
// payload 是 jsonb 欄位原樣內容（case_nearby 帶 case_title/share_url、
// clue 帶 clue_note 等），badger 欄位缺失也不會 error。
func resolveEmailContent(kind string, payload []byte) (subject, text string) {
	var p map[string]any
	_ = json.Unmarshal(payload, &p)
	str := func(key string) string {
		if v, ok := p[key].(string); ok {
			return v
		}
		return ""
	}
	switch kind {
	case "case_nearby":
		title := str("case_title")
		subject = "附近有新的協尋案件：" + title
		text = "在你關注的範圍內，有新的協尋案件「" + title + "」。\n詳情：" + str("share_url")
	case "clue":
		subject = "你的案件有新的目擊線索"
		text = "你的案件收到新的目擊回報：\n" + str("clue_note") + "\n詳情：" + str("share_url")
	default:
		subject = "【歸途 Homeward】案件更新通知"
		text = "你的案件有新進展，請開啟 App 查看：" + str("share_url")
	}
	return subject, text
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
