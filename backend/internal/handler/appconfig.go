package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AppConfigHandler 提供前端啟動時需要的公開設定。這裡只能放「公開資訊」，
// 絕對不能放任何密鑰（例如 bot token）。
type AppConfigHandler struct {
	TelegramBotUsername string
}

// Get 處理 GET /api/v1/config。
func (h *AppConfigHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"telegram_bot_username": h.TelegramBotUsername, // 空字串 = 尚未設定
	})
}
