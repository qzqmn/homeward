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
	Auth  *service.AuthService
	Users *repository.UserRepo
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
