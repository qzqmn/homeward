// Package handler：與目前登入使用者相關的設定（關注區域、通知渠道）。
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
)

type MeHandler struct {
	Users    *repository.UserRepo
	Channels *repository.ChannelRepo
}

type setWatchAreaBody struct {
	Lng     float64 `json:"lng" binding:"required"`
	Lat     float64 `json:"lat" binding:"required"`
	RadiusM int     `json:"radius_m"`
}

// SetWatchArea 處理 POST /api/v1/me/watch-area：使用者設定想關注的區域（例如住家、常去的地點），
// 範圍內有新案件時會依半徑推播策略收到通知。
func (h *MeHandler) SetWatchArea(c *gin.Context) {
	userID, _ := middleware.UserID(c) // 路由已掛 AuthRequired

	var body setWatchAreaBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !validLngLat(body.Lng, body.Lat) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lng/lat"})
		return
	}
	if body.RadiusM <= 0 {
		body.RadiusM = 3000
	}
	if body.RadiusM > 20000 {
		body.RadiusM = 20000
	}

	if err := h.Users.SetWatchArea(c.Request.Context(), userID, body.Lng, body.Lat, body.RadiusM); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save watch area"})
		return
	}
	c.Status(http.StatusNoContent)
}

type registerChannelBody struct {
	Channel string `json:"channel" binding:"required"` // web_push / telegram / whatsapp / email
	Address string `json:"address" binding:"required"` // push subscription JSON / chat id / 電話 / email
}

// RegisterChannel 處理 POST /api/v1/me/channels：註冊接收通知的管道。
func (h *MeHandler) RegisterChannel(c *gin.Context) {
	userID, _ := middleware.UserID(c)

	var body registerChannelBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.Channels.Register(c.Request.Context(), userID, body.Channel, body.Address)
	if errors.Is(err, repository.ErrInvalidChannel) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel, expected web_push/telegram/whatsapp/email"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register channel"})
		return
	}
	c.Status(http.StatusNoContent)
}
