package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// AppConfigHandler 提供前端啟動時需要的公開設定。這裡只能放「公開資訊」，
// 絕對不能放任何密鑰（例如 bot token / VAPID 私鑰）。
type AppConfigHandler struct {
	TelegramBotUsername string
	// PublicKey 取 VAPID 公鑰給瀏覽器 pushManager.subscribe；空字串 = 未啟用
	//（金鑰生成失敗或尚未重啟）。公鑰本來就是公開的。
	PublicKey func(ctx context.Context) string
}

// Get 處理 GET /api/v1/config。
func (h *AppConfigHandler) Get(c *gin.Context) {
	var vapid string
	if h.PublicKey != nil {
		vapid = h.PublicKey(c.Request.Context())
	}
	c.JSON(http.StatusOK, gin.H{
		"telegram_bot_username": h.TelegramBotUsername, // 空字串 = 尚未設定
		"vapid_public_key":      vapid,
	})
}
