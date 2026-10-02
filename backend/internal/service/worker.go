package service

import (
	"context"
	"log"
	"time"

	"homeward/internal/repository"
)

const notifyBatchSize = 100

// RunNotifyLoop 是背景排程：定期幫仍是 open 的走失案件判斷是否該擴大推播半徑，
// 並把佇列中「尚未達每日上限、且不在靜音時段」的通知實際送出。
//
// 設計成單一 goroutine 的 time.Ticker，不是因為這樣最適合長期擴充（之後案件/使用者
// 一多，應該換成有鎖、可水平擴展的排程器，例如用 Redis 做分散式鎖或換成獨立的
// worker 服務），而是在目前單一 VPS、單一後端副本的規模下，這是最簡單可靠的做法。
func RunNotifyLoop(ctx context.Context, interval time.Duration, cases *repository.CaseRepo, notify *NotifyService) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runNotifyOnce(ctx, cases, notify)
		}
	}
}

func runNotifyOnce(ctx context.Context, cases *repository.CaseRepo, notify *NotifyService) {
	candidates, err := cases.ListOpenMissingForEscalation(ctx)
	if err != nil {
		log.Printf("WARN: notify loop: list open cases: %v", err)
	}
	for _, c := range candidates {
		if err := notify.EscalateAndEnqueue(ctx, c.ID, c.Lng, c.Lat, c.CreatedAt); err != nil {
			log.Printf("WARN: notify loop: escalate case %s: %v", c.ID, err)
		}
	}

	sent, failed, err := notify.DispatchQueued(ctx, notifyBatchSize)
	if err != nil {
		log.Printf("WARN: notify loop: dispatch: %v", err)
		return
	}
	if sent > 0 || failed > 0 {
		log.Printf("notify loop: sent=%d failed=%d", sent, failed)
	}
}
