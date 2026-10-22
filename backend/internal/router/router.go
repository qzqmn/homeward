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
	updateRepo := repository.NewUpdateRepo(d.Pg)
	reportRepo := repository.NewReportRepo(d.Pg)
	settingsRepo := repository.NewSettingsRepo(d.Pg)
	sosRepo := repository.NewSosRepo(d.Pg)

	// 動態設定（管理後台改 Resend/webhook）→ 直接發送通道 → 通知服務
	settingsSvc := service.NewSettingsService(settingsRepo)
	mailer := service.NewMailer(settingsSvc, d.Cfg.TelegramBotToken)

	authSvc := service.NewAuthService(d.Redis)
	// Email 登入的驗證碼透過 Resend 寄出（未在管理後台設定 key 時回 ErrNotConfigured）
	authSvc.EmailSender = mailer.SendOTPEmail

	matchingSvc := service.NewMatchingService(matchRepo)
	notifySvc := service.NewNotifyService(userRepo, notificationRepo, settingsSvc, mailer, d.Cfg.NotifyWebhookURL, d.Cfg.BaseURL)

	photoH := &handler.PhotoHandler{MediaDir: d.Cfg.MediaDir, MaxBytes: d.Cfg.MaxUploadBytes}
	adminH := &handler.AdminHandler{
		Users:            userRepo,
		Cases:            caseRepo,
		Updates:          updateRepo,
		Settings:         settingsSvc,
		AdminTelegramIDs: handler.ParseAdminTelegramIDs(d.Cfg.AdminTelegramIDs),
	}
	authH := &handler.AuthHandler{
		Auth:             authSvc,
		Users:            userRepo,
		Channels:         channelRepo,
		Mailer:           mailer, // 忘記密碼的重置碼也走 Resend
		TelegramBotToken: d.Cfg.TelegramBotToken,
		Admin:            adminH, // ADMIN_TELEGRAM_IDS 登入時自動升級
	}
	caseH := &handler.CaseHandler{Cases: caseRepo, Users: userRepo, Matching: matchingSvc, Notify: notifySvc, Updates: updateRepo}
	clueH := &handler.ClueHandler{Clues: clueRepo, Cases: caseRepo, Users: userRepo, Notify: notifySvc, Updates: updateRepo}
	trackH := &handler.TrackHandler{Tracks: trackRepo}
	meH := &handler.MeHandler{Users: userRepo, Channels: channelRepo}
	matchH := &handler.MatchHandler{Matches: matchRepo, Cases: caseRepo}
	updateH := &handler.UpdateHandler{Updates: updateRepo, Cases: caseRepo, Users: userRepo}
	reportH := &handler.ReportHandler{Reports: reportRepo, Users: userRepo}
	sosH := &handler.SosHandler{Sos: sosRepo, Users: userRepo, Notify: notificationRepo}
	appConfigH := &handler.AppConfigHandler{TelegramBotUsername: d.Cfg.TelegramBotUsername}
	shareH := &handler.ShareHandler{Cases: caseRepo, BaseURL: d.Cfg.BaseURL}

	requireAuth := middleware.AuthRequired(authSvc)
	optionalAuth := middleware.AuthOptional(authSvc)

	// 分享預覽頁：掛在根路徑而非 /api/v1，網址短、也方便之後直接讓 CDN 快取。
	r.GET("/c/:id", shareH.Page)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/config", appConfigH.Get) // 前端啟動時讀取的公開設定
		// 登入：每 IP 每分鐘最多 5 次請求驗證碼，避免被用來轟炸簡訊
		otp := v1.Group("/auth/otp", middleware.RateLimit(d.Redis, "otp", 5, time.Minute))
		otp.POST("/request", authH.RequestOTP)
		otp.POST("/verify", authH.VerifyOTP)

		// Telegram Login：同樣掛限流，避免有人暴力嘗試偽造簽章
		v1.POST("/auth/telegram", middleware.RateLimit(d.Redis, "telegram-login", 10, time.Minute), authH.TelegramLogin)

		// Email 登入：與 OTP 同結構（request 寄碼 → verify 換 token）。
		// 驗證碼由 Resend 寄出（管理後台動態設定），未設定時 request 回 503。
		emailOtp := v1.Group("/auth/email", middleware.RateLimit(d.Redis, "email-otp", 5, time.Minute))
		emailOtp.POST("/request", authH.RequestEmailCode)
		emailOtp.POST("/verify", authH.VerifyEmailCode)

		// 用戶名/Email + 密碼：註冊仍走上面的驗證碼，之後日常登入用密碼（快、離線-friendly）。
		// 登入/忘記密碼都掛限流：暴力猜密碼的成本主要靠此處緩住。
		pwd := v1.Group("/auth/password", middleware.RateLimit(d.Redis, "pwd-login", 5, time.Minute))
		pwd.POST("/login", authH.PasswordLogin)
		pwd.POST("/forgot", authH.PasswordForgot)
		pwd.POST("/reset", authH.PasswordReset)

		// 登出：撤銷後端 session（之前只清前端 token，30 天內 token 仍可用）
		v1.POST("/auth/logout", requireAuth, authH.Logout)

		// SOS 求救：登入使用者（搜索現場狀況）；管理員會收到 system 通知
		v1.POST("/sos", requireAuth, middleware.RateLimit(d.Redis, "sos-create", 3, time.Minute), sosH.Create)

		// 上傳限流：每 IP 每分鐘 10 次，避免被濫用刷爆 NAS
		v1.POST("/cases/upload-photo", middleware.RateLimit(d.Redis, "upload", 10, time.Minute), photoH.Upload)

		me := v1.Group("/me", requireAuth)
		{
			me.GET("", meH.Get)                                 // 目前使用者基本資料（志願者/管理員狀態）
			me.GET("/watch-area", meH.GetWatchArea)             // 查看現有關注區域（頁面預填）
			me.POST("/watch-area", meH.SetWatchArea)            // 設定關注區域，供半徑推播比對
			me.DELETE("/watch-area", meH.ClearWatchArea)        // 清除關注區域（不再收附近推播）
			me.POST("/channels", meH.RegisterChannel)           // 註冊通知渠道
			me.POST("/credential", meH.SetCredential)           // 設定/更改 用戶名與密碼
			me.GET("/cases", caseH.ListMine)                    // 我的案件（含已結案的），案主清單入口
			me.POST("/volunteer-request", meH.RequestVolunteer) // 申請成為認證志願者（管理員審核）
		}

		// 舉報：登入使用者回報詐騙/騷擾/垃圾內容；限流防灌爆審核清單
		v1.POST("/reports", requireAuth, middleware.RateLimit(d.Redis, "report-create", 5, time.Minute), reportH.Create)

		// 管理後台（handler 內逐一驗 role='admin'；前端畫面在 /admin 路由）。
		// requireAuth 必掛：handler 的 isAdmin 先讀 AuthRequired 解析出的使用者，
		// 缺了它 → 所有 admin API 永遠 403（前期測試踩過的坑）。
		admin := v1.Group("/admin", requireAuth)
		{
			admin.GET("/volunteer-requests", adminH.ListVolunteerRequests)
			admin.POST("/volunteer-requests/:userId", adminH.VerifyVolunteer)
			admin.GET("/reports", reportH.ListOpen)
			admin.PATCH("/reports/:id", reportH.UpdateStatus)
			admin.GET("/cases", adminH.ListCases)                  // 所有案件概觀（處理詐騙案等）
			admin.PATCH("/cases/:id/status", adminH.SetCaseStatus) // 下架/復原/改狀態
			admin.GET("/users", adminH.ListUsers)                  // 使用者概觀
			admin.POST("/users/:userId/role", adminH.SetUserRole)  // 升級/撤銷管理員
			admin.GET("/sos", sosH.AdminListOpen)                  // 未處理的 SOS 清單
			admin.POST("/sos/:id/resolve", sosH.AdminResolve)      // 標記 SOS 已處理
			// 營運設定（Resend key/from、通知 webhook）：key 為 write-only
			admin.GET("/settings", adminH.GetSettings)
			admin.PUT("/settings", adminH.SetSettings)
		}

		cases := v1.Group("/cases")
		{
			cases.GET("", caseH.ListNearby)                             // 公開：附近案件（一律模糊座標）
			cases.POST("", requireAuth, caseH.Create)                   // 發案需登入；會觸發配對比對與通知排程
			cases.GET("/:id", optionalAuth, caseH.Get)                  // 詳情：依權限決定精確/模糊座標
			cases.PATCH("/:id", requireAuth, caseH.Update)              // 案主編輯案件內容（title/description/last_seen_at）
			cases.PATCH("/:id/status", requireAuth, caseH.UpdateStatus) // 結案／標記已團聚，僅限案主
			// 提交線索免登入，登入可關聯身份。掛限流防匿名灌水
			cases.POST("/:id/clues", middleware.RateLimit(d.Redis, "clue-create", 10, time.Minute), optionalAuth, clueH.Create)
			cases.GET("/:id/clues", requireAuth, clueH.List)                          // 線索列表：僅案主本人或認證志願者
			cases.PATCH("/:id/clues/:clueId/status", requireAuth, clueH.UpdateStatus) // 案主審核線索（可信/排除）
			cases.GET("/:id/updates", requireAuth, updateH.List)                      // 案件時間軸：案主/認證志願者
			cases.POST("/:id/tracks", requireAuth, trackH.Create)                     // 上報軌跡需登入（志願者搜索模式）
			cases.GET("/:id/coverage", trackH.Coverage)                               // 公開：搜索覆蓋熱圖
			cases.GET("/:id/matches", requireAuth, matchH.List)                       // 配對建議，僅案主可見
			cases.PATCH("/:id/matches/:matchedId", requireAuth, matchH.UpdateStatus)  // 確認／排除配對建議
		}
	}
	return Built{Engine: r, Notify: notifySvc}
}
