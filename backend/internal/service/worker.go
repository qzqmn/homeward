package service

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"homeward/internal/repository"
)

const notifyBatchSize = 100

// acquireLoopLock 是背景排程的分散式鎖（Redis SET NX EX）：未來如果跑多副本
// backend，同一時刻只有一個副本會真的執行該輪排程，避免同樣的通知發兩次。
// 各迴圈各自用不同的鎖名（notify/media-gc/track-retention）。單副本時永遠搶得到，
// 成本是一次 Redis 往返。
func acquireLoopLock(ctx context.Context, rdb *redis.Client, name string, ttl time.Duration) bool {
	if rdb == nil {
		return true // 測試環境沒給 Redis：單副本假設照舊
	}
	ok, err := rdb.SetNX(ctx, "loop-lock:"+name, "1", ttl).Result()
	if err != nil {
		log.Printf("WARN: loop lock %s: %v（直接執行）", name, err)
		return true // Redis 故障時寧可重複也不讓排程停擺
	}
	return ok
}

// 孤兒照片清理參數：上傳後未與任何案件/線索綁定的檔案，保留 72 小時後清除。
// （客戶端離線逐筆重試時，照片可能先上傳成功、內容 POST 幾小時後才成功，
// 所以不能太短。）上傳本身是匿名的，若不清理，任何人都能把 NAS 刷爆。
const (
	mediaGCInterval      = 24 * time.Hour
	mediaGCTrustedAge    = 72 * time.Hour
	mediaGCAllowedSuffix = ".jpg|.png" // 只處理 upload-photo 會產出的副檔名
)

// RunNotifyLoop 是背景排程：定期幫仍是 open 的走失案件判斷是否該擴大推播半徑，
// 並把佇列中「尚未達每日上限、且不在靜音時段」的通知實際送出。
//
// 設計成單一 goroutine 的 time.Ticker，不是因為這樣最適合長期擴充（之後案件/使用者
// 一多，應該換成有鎖、可水平擴展的排程器，例如用 Redis 做分散式鎖或換成獨立的
// worker 服務），而是在目前單一 VPS、單一後端副本的規模下，這是最簡單可靠的做法。
func RunNotifyLoop(ctx context.Context, interval time.Duration, rdb *redis.Client, cases *repository.CaseRepo, notify *NotifyService) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !acquireLoopLock(ctx, rdb, "notify", interval*2) {
				continue // 其他副本本輪已認領
			}
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
		if err := notify.EscalateAndEnqueue(ctx, c); err != nil {
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

// RunMediaGCLoop 定期清理媒體目錄中的孤兒檔案：上傳後 72 小時內沒有被任何
// 案件封面照或線索照片引用的檔案就刪除。http 上傳與 case/clue POST 之間沒有
// 所有權綁定，若不清理，匿名使用者可以無限刷滿 NAS 儲存。
// 每 mediaGCInterval 跑一次，與通知排程共用同一 ctx（SIGTERM 一起停）。
func RunMediaGCLoop(ctx context.Context, rdb *redis.Client, mediaDir string, media *repository.MediaRepo) {
	ticker := time.NewTicker(mediaGCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !acquireLoopLock(ctx, rdb, "media-gc", mediaGCInterval*2) {
				continue
			}
			runMediaGCOnce(ctx, mediaDir, media)
		}
	}
}

func runMediaGCOnce(ctx context.Context, mediaDir string, media *repository.MediaRepo) {
	threshold := time.Now().Add(-mediaGCTrustedAge)
	removed, skipped := 0, 0
	err := filepath.WalkDir(mediaDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // 檔案系統讀取錯誤不中止整輪清理
		}
		if d.IsDir() || !strings.Contains(mediaGCAllowedSuffix, filepath.Ext(d.Name())) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().After(threshold) {
			return nil
		}
		rel, err := filepath.Rel(mediaDir, path)
		if err != nil {
			return nil
		}
		// filepath.Rel 與 DB 內 /-分隔的相對路徑一致化
		rel = filepath.ToSlash(rel)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		referenced, err := media.PathReferenced(ctx, rel)
		if err != nil {
			skipped++
			return nil
		}
		if referenced {
			return nil
		}
		if err := os.Remove(path); err != nil {
			skipped++
			return nil
		}
		removed++
		return nil
	})
	if err != nil {
		log.Printf("media gc: walk %s: %v", mediaDir, err)
	}
	if removed > 0 || skipped > 0 {
		log.Printf("media gc: removed=%d skipped=%d", removed, skipped)
	}
}

// 軌跡保留期限：已結案/已團聚的案件，結案 TRACKS_RETENTION_DAYS 天後把軌跡刪除。
// 位置屬個人資料，README 隱私政策承諾會清理；tracks 保留太長只增加外洩風險。
const (
	tracksRetentionInterval = 24 * time.Hour
	tracksRetentionDefault  = 14 * 24 * time.Hour
)

// RunTrackRetentionLoop 定期刪除已完成案件的主要軌跡資料。
// retentionDays 可由 main.go 依環境變數覆寫（0 = 用預設 14 天）。
func RunTrackRetentionLoop(ctx context.Context, rdb *redis.Client, tracks *repository.TrackRepo, retention time.Duration) {
	if retention <= 0 {
		retention = tracksRetentionDefault
	}
	ticker := time.NewTicker(tracksRetentionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !acquireLoopLock(ctx, rdb, "track-retention", tracksRetentionInterval*2) {
				continue
			}
			cutoff := time.Now().Add(-retention)
			n, err := tracks.DeleteForClosedCases(ctx, cutoff)
			if err != nil {
				log.Printf("WARN: track retention: delete: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("track retention: removed=%d (cases closed before %s)", n, cutoff.Format(time.RFC3339))
			}
		}
	}
}
