package handler

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"homeward/internal/repository"
	"homeward/internal/service"
)

// 只接受 E.164 格式（例如 +85291234567），避免各種本地格式混入資料庫。
var phoneRe = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

type AuthHandler struct {
	Auth             *service.AuthService
	Users            *repository.UserRepo
	Channels         *repository.ChannelRepo
	TelegramBotToken string // 空字串表示沒設定，Telegram 登入會回 503
}

type requestOTPBody struct {
	Phone string `json:"phone" binding:"required"`
}

// RequestOTP 處理 POST /api/v1/auth/otp/request。
func (h *AuthHandler) RequestOTP(c *gin.Context) {
	var body requestOTPBody
	if err := c.ShouldBindJSON(&body); err != nil || !phoneRe.MatchString(body.Phone) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid phone, expected E.164 format e.g. +85291234567"})
		return
	}

	_, err := h.Auth.RequestOTP(c.Request.Context(), body.Phone)
	switch {
	case errors.Is(err, service.ErrCooldown):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "請稍候再重新發送"})
	case err != nil:
		// sendSMS 目前只是 log 佔位實作，理論上不會失敗；保留分支以便日後串接真實簡訊網關
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send otp"})
	default:
		c.JSON(http.StatusOK, gin.H{"message": "otp sent"})
	}
}

type verifyOTPBody struct {
	Phone string `json:"phone" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

// VerifyOTP 處理 POST /api/v1/auth/otp/verify：驗證成功建立/取得使用者並回傳登入 token。
func (h *AuthHandler) VerifyOTP(c *gin.Context) {
	var body verifyOTPBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "phone and code are required"})
		return
	}

	token, err := h.Auth.VerifyOTP(c.Request.Context(), body.Phone, body.Code)
	switch {
	case errors.Is(err, service.ErrOTPInvalid):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "驗證碼錯誤或已過期"})
		return
	case errors.Is(err, service.ErrTooManyTries):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "嘗試次數過多，請重新發送驗證碼"})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "verify failed"})
		return
	}

	user, err := h.Users.UpsertByPhone(c.Request.Context(), body.Phone)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}
	if err := h.Auth.CreateSession(c.Request.Context(), token, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":                 user.ID,
			"display_name":       user.DisplayName,
			"locale":             user.Locale,
			"verified_volunteer": user.IsVerifiedVolunteer(),
		},
	})
}

// telegramLoginBody 對應 Telegram Login Widget 回呼給前端的資料，前端原樣轉送過來。
// 全部用 string：Telegram 的簽章驗證要求每個欄位用「原始字串形式」拼接，
// 用數字型別反而容易因為格式化方式不同而驗證失敗。
type telegramLoginBody struct {
	ID        string `json:"id" binding:"required"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	PhotoURL  string `json:"photo_url"`
	AuthDate  string `json:"auth_date" binding:"required"`
	Hash      string `json:"hash" binding:"required"`
}

// TelegramLogin 處理 POST /api/v1/auth/telegram：驗證 Telegram Login Widget
// 的簽章，成功後比照 VerifyOTP 建立/取得使用者並回傳登入 token。
// 另外會自動把這個 Telegram 帳號註冊成通知渠道，使用者不用再手動設定一次。
func (h *AuthHandler) TelegramLogin(c *gin.Context) {
	if h.TelegramBotToken == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "telegram login not configured"})
		return
	}

	var body telegramLoginBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fields := map[string]string{
		"id":         body.ID,
		"first_name": body.FirstName,
		"last_name":  body.LastName,
		"username":   body.Username,
		"photo_url":  body.PhotoURL,
		"auth_date":  body.AuthDate,
		"hash":       body.Hash,
	}
	// 空字串欄位不算進簽章檢查字串：Telegram 只會送出實際有值的欄位
	// （例如沒有設定 last_name 的使用者，widget 根本不會給這個 key）。
	for k, v := range fields {
		if v == "" {
			delete(fields, k)
		}
	}

	telegramID, displayName, err := service.VerifyTelegramLogin(fields, h.TelegramBotToken)
	switch {
	case errors.Is(err, service.ErrTelegramHashMismatch):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "簽章驗證失敗"})
		return
	case errors.Is(err, service.ErrTelegramDataTooOld):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "登入資料已過期，請重新登入"})
		return
	case err != nil:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid telegram login data"})
		return
	}

	user, err := h.Users.UpsertByTelegramID(c.Request.Context(), telegramID, displayName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	// 自動註冊通知渠道；失敗不影響登入本身（只是這次沒順便設定好通知，
	// 使用者之後還是可以透過 /me/channels 補註冊）。
	_ = h.Channels.Register(c.Request.Context(), user.ID, "telegram", body.ID)

	token, err := h.Auth.IssueSession(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":                 user.ID,
			"display_name":       user.DisplayName,
			"locale":             user.Locale,
			"verified_volunteer": user.IsVerifiedVolunteer(),
		},
	})
}
