// Package handler：與目前登入使用者相關的設定（關注區域、通知渠道）。
package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
	"homeward/internal/service"
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

// Get 處理 GET /api/v1/me：目前登入者的基本資料，前端用來顯示
// 志願者申請狀態與管理員入口（不回傳任何敏感欄位）。
func (h *MeHandler) Get(c *gin.Context) {
	userID, _ := middleware.UserID(c)
	u, err := h.Users.GetByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":                  u.ID,
		"display_name":        u.DisplayName,
		"username":            u.Username,
		"email":               u.Email,
		"has_password":        u.PasswordHash != "",
		"verified_volunteer":  u.IsVerifiedVolunteer(),
		"volunteer_requested": u.VolunteerRequestedAt != nil,
		"is_admin":            u.Role == "admin",
	})
}

// SetCredential 處理 POST /api/v1/me/credential：設定/更改 用戶名與密碼。
// 規則：
//   - 用戶名只能設定一次（不做改名，避免歷史線索/紀錄對不上人）
//   - 已設密碼要改：必須帶 current_password（重設密碼走 /auth/password/reset）
//   - 沒密碼的帳號第一次設：不用 current_password（才進得來這個帳號本身就是憑證）
func (h *MeHandler) SetCredential(c *gin.Context) {
	userID, _ := middleware.UserID(c)

	var body struct {
		Username        string `json:"username"`
		Password        string `json:"password" binding:"required"`
		CurrentPassword string `json:"current_password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password is required"})
		return
	}
	if err := service.ValidPassword(body.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	u, err := h.Users.GetByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}

	// 已有密碼 → 必須驗 current_password 才准改
	if u.PasswordHash != "" {
		if err := service.CheckPassword(u.PasswordHash, body.CurrentPassword); err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "目前密碼不正確"})
			return
		}
	}

	// 用戶名驗證：新設 → 查唯一性；已設過且不同 → 拒絕（MVP 只做一次性設定）
	var usernamePtr *string
	if body.Username != "" {
		if err := service.ValidUsername(body.Username); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if u.Username != nil && !strings.EqualFold(*u.Username, body.Username) {
			c.JSON(http.StatusForbidden, gin.H{"error": "用戶名已設定，不能更改"})
			return
		}
		if h.hasConflictUsername(c.Request.Context(), userID, body.Username) {
			c.JSON(http.StatusConflict, gin.H{"error": "這個用戶名已被使用"})
			return
		}
		name := strings.ToLower(body.Username)
		usernamePtr = &name
	}

	hash, err := service.HashPassword(body.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to set password"})
		return
	}
	u, err = h.Users.SetCredential(c.Request.Context(), userID, usernamePtr, hash)
	if err != nil {
		// 唯一索引兜底：查重與寫入之間的競態（兩人同時設同名）
		c.JSON(http.StatusConflict, gin.H{"error": "這個用戶名已被使用"})
		return
	}
	var username *string
	if u.Username != nil {
		name := *u.Username
		username = &name
	}
	c.JSON(http.StatusOK, gin.H{
		"username":     username,
		"has_password": u.PasswordHash != "",
	})
}

// hasConflictUsername 查用戶名是否已被「別人」佔用。
func (h *MeHandler) hasConflictUsername(ctx context.Context, userID, username string) bool {
	other, err := h.Users.GetByUsername(ctx, username)
	return err == nil && other.ID != userID
}

// RequestVolunteer 處理 POST /api/v1/me/volunteer-request：申請成為認證志願者。
// 審核由管理員在 admin API 完成；這裡只登記意願（冪等，可重複按）。
func (h *MeHandler) RequestVolunteer(c *gin.Context) {
	userID, _ := middleware.UserID(c)

	u, err := h.Users.GetByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	if u.IsVerifiedVolunteer() {
		c.JSON(http.StatusConflict, gin.H{"error": "already a verified volunteer"})
		return
	}
	if err := h.Users.RequestVolunteer(c.Request.Context(), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save request"})
		return
	}
	c.Status(http.StatusNoContent)
}

// GetWatchArea 處理 GET /api/v1/me/watch-area：回傳既有的關注區域
// （頁面打開預填）；尚未設定過回 watch_area=null。
func (h *MeHandler) GetWatchArea(c *gin.Context) {
	userID, _ := middleware.UserID(c)
	lng, lat, radiusM, ok, err := h.Users.GetWatchArea(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load watch area"})
		return
	}
	if !ok {
		c.JSON(http.StatusOK, gin.H{"watch_area": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"watch_area": gin.H{"lng": lng, "lat": lat, "radius_m": radiusM}})
}

// ClearWatchArea 處理 DELETE /api/v1/me/watch-area：清空關注區域
// （使用者不再收到「附近有案件」的推播）。
func (h *MeHandler) ClearWatchArea(c *gin.Context) {
	userID, _ := middleware.UserID(c)
	if err := h.Users.ClearWatchArea(c.Request.Context(), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear watch area"})
		return
	}
	c.Status(http.StatusNoContent)
}
