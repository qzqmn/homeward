package service

import (
	"context"
	"fmt"
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

// NotifyService 負責半徑推播的佇列寫入與實際發送（發送目前是 log 佔位，見 sendViaChannel）。
type NotifyService struct {
	users         *repository.UserRepo
	notifications *repository.NotificationRepo
	loc           *time.Location
}

func NewNotifyService(users *repository.UserRepo, notifications *repository.NotificationRepo) *NotifyService {
	loc, err := time.LoadLocation("Asia/Hong_Kong")
	if err != nil {
		loc = time.UTC // 容器內若缺時區資料庫，退回 UTC，避免啟動失敗（僅影響靜音時段判斷）
	}
	return &NotifyService{users: users, notifications: notifications, loc: loc}
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
		if sendErr := sendViaChannel(n.Channel, n.Address, n.Payload); sendErr != nil {
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

// sendViaChannel 是實際發送的替身實作：目前只印 log。
// TODO: 依 channel 串接真正的管道——web_push 用 VAPID 推播、telegram 用 Bot API
// 呼叫 sendMessage、whatsapp/email 則需另外串接服務商。
func sendViaChannel(channel, address string, payload []byte) error {
	fmt.Printf("[DEV] notify via %s to %s: %s (尚未串接真正的推播/訊息服務)\n", channel, address, payload)
	return nil
}
