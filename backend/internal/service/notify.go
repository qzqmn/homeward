package service

import (
	"bytes"
	"context"
	"encoding/json"
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

// NotifyService 負責半徑推播的佇列寫入，以及把到期的通知丟給 webhook 實際發送
// （見 sendViaChannel）。後端本身不認識 Telegram／簡訊／whatsapp 的 API，只
// 負責「決定要不要發、發給誰」，實際怎麼送交給 webhookURL 那一端處理。
type NotifyService struct {
	users         *repository.UserRepo
	notifications *repository.NotificationRepo
	loc           *time.Location
	webhookURL    string
	httpClient    *http.Client
}

func NewNotifyService(users *repository.UserRepo, notifications *repository.NotificationRepo, webhookURL string) *NotifyService {
	loc, err := time.LoadLocation("Asia/Hong_Kong")
	if err != nil {
		loc = time.UTC // 容器內若缺時區資料庫，退回 UTC，避免啟動失敗（僅影響靜音時段判斷）
	}
	return &NotifyService{
		users:         users,
		notifications: notifications,
		loc:           loc,
		webhookURL:    webhookURL,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

// EscalateAndEnqueue 依案件建立至今的經過時間，找出目前該用的半徑，為範圍內、
// 尚未收過通知的「關注該區域」使用者各排一則通知。建案當下與之後的排程 worker
// 都呼叫這支函式；已通知過的人不會因為半徑擴大而重複收到（見 repository 層的唯一索引）。
func (s *NotifyService) EscalateAndEnqueue(ctx context.Context, caseID string, lng, lat float64, caseCreatedAt time.Time) error {
	radius := currentRadius(time.Since(caseCreatedAt))
	watchers, err := s.users.FindWatchersInRadius(ctx, caseID, lng, lat, radius)
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
	_, err = s.notifications.EnqueueCaseNearby(ctx, caseID, userIDs, radius)
	return err
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
		if sendErr := s.sendViaChannel(ctx, n.Channel, n.Address, n.Kind, n.Payload); sendErr != nil {
			_ = s.notifications.MarkFailed(ctx, n.ID)
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
// notifications.payload 這個 JSONB 欄位的原始內容（例如 {"radius_m":1500}），
// 用 json.RawMessage 讓它原樣內嵌，不會被二次編碼成字串。
type webhookEnvelope struct {
	Channel string          `json:"channel"` // "telegram"／"web_push"／"whatsapp"／"email"
	Address string          `json:"address"` // user_channels.address：Telegram chat id／push 訂閱 JSON／電話／email
	Kind    string          `json:"kind"`    // "case_nearby" 等通知種類
	Payload json.RawMessage `json:"payload"`
}

// sendViaChannel 沒有設定 webhookURL 時只印 log（本機開發／還沒接 webhook 時的
// 退路）；設定了就 POST 這則通知的內容過去，由 webhook 那端（例如你自己的
// Cloudflare Worker）決定怎麼送出去。後端完全不需要知道 Telegram bot token
// 之類的憑證——那些留在 webhook 那端管理，這裡只負責「决定要發什麼」。
//
// 只看 HTTP 狀態碼判斷成功與否：2xx 視為送達，其餘（含逾時、連線失敗）一律
// 當作失敗，讓這筆通知留在佇列裡等下次重試。
func (s *NotifyService) sendViaChannel(ctx context.Context, channel, address, kind string, payload []byte) error {
	if s.webhookURL == "" {
		fmt.Printf("[DEV] notify via %s to %s (%s): %s (尚未設定 NOTIFY_WEBHOOK_URL)\n", channel, address, kind, payload)
		return nil
	}

	body, err := json.Marshal(webhookEnvelope{Channel: channel, Address: address, Kind: kind, Payload: payload})
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
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
