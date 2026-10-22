package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"homeward/internal/repository"
	"homeward/internal/service"
)

// 只接受 E.164 格式（例如 +85291234567），避免各種本地格式混入資料庫。
var phoneRe = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

// Email 登入的收件人格式：一般常見 email（不接受換行/空格），且上限 254 字（RFC）。
var emailRe = regexp.MustCompile(`^[^@\s]{1,64}@[^@\s.]+\.[^@\s]{2,}$`)

type AuthHandler struct {
	Auth             *service.AuthService
	Users            *repository.UserRepo
	Channels         *repository.ChannelRepo
	Mailer           *service.Mailer // 忘記密碼的驗證碼一樣走 Resend
	TelegramBotToken string          // 空字串表示沒設定，Telegram 登入會回 503
	// Admin 非空時：telegram_id 在 ADMIN_TELEGRAM_IDS 名單內的帳號登入時自動升級
	// 為 admin（第一個管理員的初始化通道，不必進 VPS 跑 SQL）。
	Admin *AdminHandler
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
			"is_admin":           user.Role == "admin",
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
	case errors.Is(err, service.ErrTelegramInvalidData):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid telegram login data"})
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

	// ADMIN_TELEGRAM_IDS 名單內的帳號自動升級成 admin（失敗不擋登入主流程）。
	if h.Admin != nil {
		h.Admin.MaybeAutoPromoteAdmin(c.Request.Context(), telegramID)
		// 重新讀取更新後的角色，讓下面的 /me 資料與登入回傳反映最新權限
		if fresh, err := h.Users.GetByID(c.Request.Context(), user.ID); err == nil {
			user = fresh
		}
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
			"is_admin":           user.Role == "admin",
		},
	})
}

// ---- Email 登入（Resend 寄驗證碼；管理後台可改設定）----

type requestEmailCodeBody struct {
	Email string `json:"email" binding:"required"`
}

// RequestEmailCode 處理 POST /api/v1/auth/email/request：email 格式驗證後，
// 透過 AuthService.RequestEmailCode（Resend）寄 6 位驗證碼。
// 未設定 Resend（ErrNotConfigured）回 503，讓前端提示「管理員未配置寄信服務」。
func (h *AuthHandler) RequestEmailCode(c *gin.Context) {
	var body requestEmailCodeBody
	if err := c.ShouldBindJSON(&body); err != nil || !emailRe.MatchString(body.Email) || len(body.Email) > 254 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email address"})
		return
	}

	err := h.Auth.RequestEmailCode(c.Request.Context(), body.Email)
	switch {
	case errors.Is(err, service.ErrCooldown):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "請稍候再重新發送"})
	case errors.Is(err, service.ErrNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "email login not configured"})
	case err != nil:
		log.Printf("WARN: send email otp to %s: %v", maskEmail(body.Email), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send code"})
	default:
		c.JSON(http.StatusOK, gin.H{"message": "code sent"})
	}
}

type verifyEmailCodeBody struct {
	Email string `json:"email" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

// VerifyEmailCode 處理 POST /api/v1/auth/email/verify：驗證通過建立/取得使用者
// 並回傳登入 token（與 Telegram 相同的回應形狀；同時自動註冊 email 通知渠道）。
func (h *AuthHandler) VerifyEmailCode(c *gin.Context) {
	var body verifyEmailCodeBody
	if err := c.ShouldBindJSON(&body); err != nil || !emailRe.MatchString(body.Email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and code are required"})
		return
	}

	if err := h.Auth.VerifyEmailCode(c.Request.Context(), body.Email, body.Code); err != nil {
		switch {
		case errors.Is(err, service.ErrOTPInvalid):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "驗證碼錯誤或已過期"})
		case errors.Is(err, service.ErrTooManyTries):
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "嘗試次數過多，請重新發送驗證碼"})
		default:
			log.Printf("WARN: verify email code for %s: %v", maskEmail(body.Email), err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "verify failed"})
		}
		return
	}

	user, err := h.Users.UpsertByEmail(c.Request.Context(), body.Email)
	if err != nil {
		log.Printf("WARN: upsert user by email %s: %v", maskEmail(body.Email), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}
	// email 渠道自動註冊：之後 email 通知（案件/線索）直接可用，不用手動填。
	if err := h.Channels.Register(c.Request.Context(), user.ID, "email", body.Email); err != nil {
		log.Printf("WARN: register email channel for user %s: %v", user.ID, err)
	}

	token, err := h.Auth.IssueSession(c.Request.Context(), user.ID)
	if err != nil {
		log.Printf("WARN: issue session for user %s: %v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	loginResponse(c, user, token)
}

// loginResponse 是各登入端點共通的成功回應：token + 使用者狀態（含帳號憑證
// 概況——前端據 has_password/username 決定要不要先引導「設用戶名密碼」）。
func loginResponse(c *gin.Context, u repository.User, token string) {
	var username *string
	if u.Username != nil {
		name := *u.Username
		username = &name
	}
	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":                 u.ID,
			"display_name":       u.DisplayName,
			"username":           username,
			"has_password":       u.PasswordHash != "",
			"locale":             u.Locale,
			"verified_volunteer": u.IsVerifiedVolunteer(),
			"is_admin":           u.Role == "admin",
		},
	})
}

// Logout 處理 POST /api/v1/auth/logout：撤銷目前 token（後端 session 刪除），
// 讓「登出」真的下線——之前只清前端 token，30 天內被拆走的 token 依然可用。
func (h *AuthHandler) Logout(c *gin.Context) {
	tok := bearerToken(c)
	if tok == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing bearer token"})
		return
	}
	if err := h.Auth.Logout(c.Request.Context(), tok); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to logout"})
		return
	}
	c.Status(http.StatusNoContent)
}

// bearerToken 從 Authorization header 抓 Bearer token（logout 沒有 body 可放）。
func bearerToken(c *gin.Context) string {
	const prefix = "Bearer "
	h := c.GetHeader("Authorization")
	if strings.HasPrefix(h, prefix) {
		return strings.TrimPrefix(h, prefix)
	}
	return ""
}

// maskEmail 在 log 裡遮住 email 本體（a***@domain），避免驗證碼請求的收件人
// 直接出現在 log。
func maskEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 1 {
		return "***" + email[at:]
	}
	return email[:1] + "***" + email[at:]
}

// =====================  用戶名 + 密碼登入  =====================
// 註冊（Email 驗證碼）照舊；之後登入用 用戶名或 email + 密碼。忘記密碼走
// 已驗證的 email 寄重置驗證碼（同 OTP 流程），不需要另外的「忘記密碼」表。

type passwordBody struct {
	Account         string `json:"account" binding:"required"`
	Password        string `json:"password" binding:"required"`
	Code            string `json:"code"`             // 僅 reset 用
	NewPassword     string `json:"new_password"`     // 像 reset 用
	Username        string `json:"username"`         // 註冊後設用戶名；留空只改密碼
	CurrentPassword string `json:"current_password"` // 已有密碼時改密忘記碼重置不必填
}

// resolveAccount 由「用戶名或 email」找到使用者；找不到回 ErrNotFound（query 錯誤路徑不改寫，呼叫端統一 401/200 處理以避免帳號枚舉）。
func (h *AuthHandler) resolveAccount(ctx context.Context, account string) (repository.User, error) {
	if emailRe.MatchString(account) {
		return h.Users.GetByEmail(ctx, strings.ToLower(account))
	}
	return h.Users.GetByUsername(ctx, account)
}

// PasswordLogin 處理 POST /api/v1/auth/password/login：帳號（用戶名或email）
// + 密碼 → session token。統一 401 訊息避免洩露帳號是否存在。
func (h *AuthHandler) PasswordLogin(c *gin.Context) {
	var b passwordBody
	if err := c.ShouldBindJSON(&b); err != nil || b.Account == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account and password are required"})
		return
	}
	if len(b.Password) > 72 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "帳號或密碼不正確"})
		return
	}
	u, err := h.resolveAccount(c.Request.Context(), strings.TrimSpace(b.Account))
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "帳號或密碼不正確"})
		return
	}
	if err != nil {
		log.Printf("WARN: password login lookup %q: %v", maskAccount(b.Account), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}
	if u.PasswordHash == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "該帳號尚未設定密碼，請改用驗證碼或 Telegram 登入"})
		return
	}
	if err := service.CheckPassword(u.PasswordHash, b.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "帳號或密碼不正確"})
		return
	}
	token, err := h.Auth.IssueSession(c.Request.Context(), u.ID)
	if err != nil {
		log.Printf("WARN: issue session for user %s: %v", u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}
	loginResponse(c, u, token)
}

// PasswordForgot 處理 POST /api/v1/auth/password/forgot：對帳號的 email 寄
// 重置驗證碼。無論帳號是否存在都回同一句，避免枚舉。
func (h *AuthHandler) PasswordForgot(c *gin.Context) {
	var b passwordBody
	if err := c.ShouldBindJSON(&b); err != nil || b.Account == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account is required"})
		return
	}
	msg := "如果這個帳號的 email 有效，重置碼已寄出（留意垃圾郵件匣）"
	u, err := h.resolveAccount(c.Request.Context(), strings.TrimSpace(b.Account))
	if err != nil || u.Email == nil {
		c.JSON(http.StatusOK, gin.H{"message": msg})
		return
	}
	// 共用 OTP 寄送（cooldown 60 秒也跟著走）
	err = h.Auth.RequestEmailCode(c.Request.Context(), *u.Email)
	switch {
	case err == nil:
	case errors.Is(err, service.ErrCooldown):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "請稍候再重新發送"})
		return
	case errors.Is(err, service.ErrNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "email login not configured"})
		return
	default:
		log.Printf("WARN: send reset code to %s: %v", maskEmail(*u.Email), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send code"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": msg})
}

// PasswordReset 處理 POST /api/v1/auth/password/reset：驗證碼核對通過即
// 直接設新密碼並登入（省一次輸入）。
func (h *AuthHandler) PasswordReset(c *gin.Context) {
	var b passwordBody
	if err := c.ShouldBindJSON(&b); err != nil || b.Account == "" || b.Code == "" || b.NewPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account, code and new_password are required"})
		return
	}
	if err := service.ValidPassword(b.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	u, err := h.resolveAccount(c.Request.Context(), strings.TrimSpace(b.Account))
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "驗證碼無效或帳號不存在"})
		return
	}
	if err != nil {
		log.Printf("WARN: password reset lookup %q: %v", maskAccount(b.Account), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reset failed"})
		return
	}
	if u.Email == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "該帳號沒有 email，無法用驗證碼重置"})
		return
	}
	if err := h.Auth.VerifyEmailCode(c.Request.Context(), *u.Email, b.Code); err != nil {
		if !errors.Is(err, service.ErrOTPInvalid) && !errors.Is(err, service.ErrTooManyTries) {
			log.Printf("WARN: verify reset code for %s: %v", maskEmail(*u.Email), err)
		}
		switch {
		case errors.Is(err, service.ErrOTPInvalid):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "驗證碼錯誤或已過期"})
		case errors.Is(err, service.ErrTooManyTries):
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "嘗試次數過多，請重新發送驗證碼"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "reset failed"})
		}
		return
	}
	hash, err := service.HashPassword(b.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reset failed"})
		return
	}
	u, err = h.Users.SetCredential(c.Request.Context(), u.ID, nil, hash)
	if err != nil {
		log.Printf("WARN: set credential for user %s: %v", u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to set password"})
		return
	}
	token, err := h.Auth.IssueSession(c.Request.Context(), u.ID)
	if err != nil {
		log.Printf("WARN: issue session for user %s: %v", u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}
	loginResponse(c, u, token)
}

// maskAccount log 裡遮帳號（用戶名照露、遮 email）。
func maskAccount(account string) string {
	if emailRe.MatchString(account) {
		return maskEmail(account)
	}
	return account
}
