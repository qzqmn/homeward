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

// NearbyCase 是排「附近有案件」通知時需要的案件資訊。
// payload 裡放案件標題／分享連結／封面照網址，webhook 那端的 Worker 才知道
// 要通知使用者「什麼」（之前 payload 只有 radius_m，Worker 無從得知通知內容）。
type NearbyCase struct {
	ID        string
	CaseType  string
	Title     string
	PhotoPath *string
}

// NotifyService 負責半徑推播的佇列寫入，以及把到期的通知丟給 webhook 實際發送
// （見 sendViaChannel）。後端本身不認識 Telegram／簡訊／whatsapp 的 API，只
// 負責「決定要不要發、發給誰」，實際怎麼送交給 webhookURL 那一端處理。
type NotifyService struct {
	users         *repository.UserRepo
	notifications *repository.NotificationRepo
	loc           *time.Location
	webhookURL    string
	baseURL       string // 對外網域，組 share_url 塞進 payload 用
	httpClient    *http.Client
}

func NewNotifyService(users *repository.UserRepo, notifications *repository.NotificationRepo, webhookURL, baseURL string) *NotifyService {
	loc, err := time.LoadLocation("Asia/Hong_Kong")
	if err != nil {
		loc = time.UTC // 容器內若缺時區資料庫，退回 UTC，避免啟動失敗（僅影響靜音時段判斷）
	}
	return &NotifyService{
		users:         users,
		notifications: notifications,
		loc:           loc,
		webhookURL:    webhookURL,
		baseURL:       baseURL,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
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
		if sendErr := s.sendViaChannel(ctx, n.Channel, n.Address, n.Kind, n.Payload); sendErr != nil {
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

// sendViaChannel 沒有設定 webhookURL 時只印 log（本機開發／還沒接 webhook 時的
// 退路）；設定了就 POST 這則通知的內容過去，由 webhook 那端（例如你自己的
// Cloudflare Worker）決定怎麼送出去。後端完全不需要知道 Telegram bot token
// 之類的憑證——那些留在 webhook 那端管理，這裡只負責「决定要發什麼」。
//
// 只看 HTTP 狀態碼判斷成功與否：2xx 視為送達，其餘（含逾時、連線失敗）一律
// 當作失敗，讓這筆通知留在佇列裡等下次重試。
func (s *NotifyService) sendViaChannel(ctx context.Context, channel, address, kind string, payload []byte) error {
	if s.webhookURL == "" {
		// 收件地址屬個資（chat id／電話／email），只印渠道與 kind，
		// payload 內容也不印（案件標題同屬個資）。
		fmt.Printf("[DEV] notify via %s kind=%s (payload omitted, NOTIFY_WEBHOOK_URL not set)\n", channel, kind)
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

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
