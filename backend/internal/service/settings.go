package service

import (
	"context"
	"sync"
	"time"

	"homeward/internal/repository"
)

// AppSettings 是通知/寄信的動態設定快照（來源：app_settings 表，管理後台可改）。
type AppSettings struct {
	ResendAPIKey string
	ResendFrom   string
	WebhookURL   string
}

// ConfiguredResend 表示 Resend 是否已設定完整（key + from 缺一不可）。
func (s AppSettings) ConfiguredResend() bool {
	return s.ResendAPIKey != "" && s.ResendFrom != ""
}

// SettingsService 帶短 TTL 快照的設定讀取器：每次發送通知都查一次 DB 太奢侈，
// 但完全快取又會讓「管理後台改完要等很久才生效」——折衷快照 30 秒。
type SettingsService struct {
	repo *repository.SettingsRepo

	mu       sync.Mutex
	cached   AppSettings
	fetched  time.Time
	cacheTTL time.Duration
}

func NewSettingsService(repo *repository.SettingsRepo) *SettingsService {
	return &SettingsService{repo: repo, cacheTTL: 30 * time.Second}
}

// Snapshot 取得目前的設定（快取失效時重讀 DB）。DB 錯誤回最後一次成功的
// 快照或零值，讓發送失敗不至於把整個排程打死（會在 log 看得到）。
func (s *SettingsService) Snapshot(ctx context.Context) AppSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.fetched) < s.cacheTTL {
		return s.cached
	}

	next := AppSettings{}
	if v, ok, err := s.repo.Get(ctx, repository.SettingsKeyResendAPIKey); err == nil && ok {
		next.ResendAPIKey = v
	}
	if v, ok, err := s.repo.Get(ctx, repository.SettingsKeyResendFrom); err == nil && ok {
		next.ResendFrom = v
	}
	if v, ok, err := s.repo.Get(ctx, repository.SettingsKeyNotifyWebhook); err == nil && ok {
		next.WebhookURL = v
	}
	// 全部 key 讀取都出錯（DB 掛了）時，不更新 fetched，下次輪詢再試
	s.cached = next
	s.fetched = time.Now()
	return next
}

// Invalidate 讓下一次 Snapshot 立即重讀 DB（管理後台改設定後呼叫）。
func (s *SettingsService) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fetched = time.Time{}
}

// Set 寫入單一設定（空字串 = 刪除），成功後讓快照立刻失效。
func (s *SettingsService) Set(ctx context.Context, key, value string) error {
	if err := s.repo.Set(ctx, key, value); err != nil {
		return err
	}
	s.Invalidate()
	return nil
}
