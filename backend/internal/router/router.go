// Package router 集中註冊所有路由。
package router

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"homeward/internal/config"
	"homeward/internal/handler"
	"homeward/internal/middleware"
	"homeward/internal/repository"
	"homeward/internal/service"
)

type Deps struct {
	Cfg   config.Config
	Pg    *pgxpool.Pool
	Redis *redis.Client
}

// Built 把 New 組好的 engine 與需要在背景排程重複使用的 service 一起回傳，
// 讓 main.go 可以用同一個 NotifyService 實例啟動定時任務，不必重新組裝一次依賴。
type Built struct {
	Engine *gin.Engine
	Notify *service.NotifyService
}

// New 是 Build 的簡化版本，只回傳 engine；大多數情境（測試、未來擴充）用這個就夠。
func New(d Deps) *gin.Engine {
	return Build(d).Engine
}

func Build(d Deps) Built {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.MaxMultipartMemory = 8 << 20 // 超過 8MB 的部分暫存磁碟

	// 只信任 Docker/私網內的反向代理（Nginx）帶來的 X-Forwarded-For，
	// 否則客戶端可偽造 IP 繞過限流。
	if err := r.SetTrustedProxies([]string{"172.16.0.0/12", "10.0.0.0/8", "192.168.0.0/16"}); err != nil {
		log.Printf("WARN: set trusted proxies: %v", err)
	}

	// 健康檢查：同時確認 Postgres 與 Redis 可用（供 Docker healthcheck 使用）
	r.GET("/healthz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := d.Pg.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "postgres down"})
			return
		}
		if err := d.Redis.Ping(ctx).Err(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "redis down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// ---- 組裝各層：repository（DB 存取）→ service（跨表商業邏輯）→ handler（HTTP）----
	userRepo := repository.NewUserRepo(d.Pg)
	caseRepo := repository.NewCaseRepo(d.Pg)
	clueRepo := repository.NewClueRepo(d.Pg)
	trackRepo := repository.NewTrackRepo(d.Pg)
	channelRepo := repository.NewChannelRepo(d.Pg)
	notificationRepo := repository.NewNotificationRepo(d.Pg)
	matchRepo := repository.NewMatchRepo(d.Pg)

	authSvc := service.NewAuthService(d.Redis)
	matchingSvc := service.NewMatchingService(matchRepo)
	notifySvc := service.NewNotifyService(userRepo, notificationRepo, d.Cfg.NotifyWebhookURL)

	photoH := &handler.PhotoHandler{MediaDir: d.Cfg.MediaDir, MaxBytes: d.Cfg.MaxUploadBytes}
	authH := &handler.AuthHandler{
		Auth:             authSvc,
		Users:            userRepo,
		Channels:         channelRepo,
		TelegramBotToken: d.Cfg.TelegramBotToken,
	}
	caseH := &handler.CaseHandler{Cases: caseRepo, Users: userRepo, Matching: matchingSvc, Notify: notifySvc}
	clueH := &handler.ClueHandler{Clues: clueRepo}
	trackH := &handler.TrackHandler{Tracks: trackRepo}
	meH := &handler.MeHandler{Users: userRepo, Channels: channelRepo}
	matchH := &handler.MatchHandler{Matches: matchRepo, Cases: caseRepo}
	shareH := &handler.ShareHandler{Cases: caseRepo, BaseURL: d.Cfg.BaseURL}

	requireAuth := middleware.AuthRequired(authSvc)
	optionalAuth := middleware.AuthOptional(authSvc)

	// 分享預覽頁：掛在根路徑而非 /api/v1，網址短、也方便之後直接讓 CDN 快取。
	r.GET("/c/:id", shareH.Page)

	v1 := r.Group("/api/v1")
	{
		// 登入：每 IP 每分鐘最多 5 次請求驗證碼，避免被用來轟炸簡訊
		otp := v1.Group("/auth/otp", middleware.RateLimit(d.Redis, "otp", 5, time.Minute))
		otp.POST("/request", authH.RequestOTP)
		otp.POST("/verify", authH.VerifyOTP)

		// Telegram Login：同樣掛限流，避免有人暴力嘗試偽造簽章
		v1.POST("/auth/telegram", middleware.RateLimit(d.Redis, "telegram-login", 10, time.Minute), authH.TelegramLogin)

		// 上傳限流：每 IP 每分鐘 10 次，避免被濫用刷爆 NAS
		v1.POST("/cases/upload-photo", middleware.RateLimit(d.Redis, "upload", 10, time.Minute), photoH.Upload)

		me := v1.Group("/me", requireAuth)
		{
			me.POST("/watch-area", meH.SetWatchArea)  // 設定關注區域，供半徑推播比對
			me.POST("/channels", meH.RegisterChannel) // 註冊通知渠道
		}

		cases := v1.Group("/cases")
		{
			cases.GET("", caseH.ListNearby)                                          // 公開：附近案件（一律模糊座標）
			cases.POST("", requireAuth, caseH.Create)                                // 發案需登入；會觸發配對比對與通知排程
			cases.GET("/:id", optionalAuth, caseH.Get)                               // 詳情：依權限決定精確/模糊座標
			cases.PATCH("/:id/status", requireAuth, caseH.UpdateStatus)              // 結案／標記已團聚，僅限案主
			cases.POST("/:id/clues", optionalAuth, clueH.Create)                     // 提交線索免登入，登入可關聯身份
			cases.POST("/:id/tracks", requireAuth, trackH.Create)                    // 上報軌跡需登入（志願者搜索模式）
			cases.GET("/:id/coverage", trackH.Coverage)                              // 公開：搜索覆蓋熱圖
			cases.GET("/:id/matches", requireAuth, matchH.List)                      // 配對建議，僅案主可見
			cases.PATCH("/:id/matches/:matchedId", requireAuth, matchH.UpdateStatus) // 確認／排除配對建議
		}
	}
	return Built{Engine: r, Notify: notifySvc}
}
