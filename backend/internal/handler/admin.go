// Package handler：管理員（users.role='admin'）專屬後台 API——志願者審核、
// 案件管理（狀態調整）、使用者管理（升級/撤銷管理員）。
package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
	"homeward/internal/service"
)

type AdminHandler struct {
	Users            *repository.UserRepo
	Cases            *repository.CaseRepo
	Updates          *repository.UpdateRepo
	Settings         *service.SettingsService // app_settings（Resend/webhook 動態設定）
	AdminTelegramIDs []int64                  // .env ADMIN_TELEGRAM_IDS：這些 Telegram id 登入時自動升級為 admin
}

func adminFilterParams(c *gin.Context) (limit, offset int, q string) {
	limit = 100
	offset = 0
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	if v, err := strconv.Atoi(c.Query("offset")); err == nil && v > 0 {
		offset = v
	}
	q = strings.TrimSpace(c.Query("q"))
	return limit, offset, q
}

// ListVolunteerRequests 處理 GET /api/v1/admin/volunteer-requests：審核清單
// （申請過的使用者，含目前已認證/未認證的狀態）。
func (h *AdminHandler) ListVolunteerRequests(c *gin.Context) {
	if !isAdmin(c, h.Users) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	candidates, err := h.Users.ListVolunteerRequests(c.Request.Context(), 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list volunteer requests"})
		return
	}
	out := make([]gin.H, 0, len(candidates))
	for _, cand := range candidates {
		contact := ""
		switch {
		case cand.TelegramID != nil:
			contact = "telegram:" + strconv.FormatInt(*cand.TelegramID, 10)
		case cand.PhoneE164 != nil:
			contact = "phone:" + *cand.PhoneE164
		}
		out = append(out, gin.H{
			"id":           cand.ID,
			"display_name": cand.DisplayName,
			"contact":      contact,
			"verified":     cand.VolunteerVerifiedAt != nil,
			"requested_at": cand.VolunteerRequestedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"requests": out})
}

type verifyVolunteerBody struct {
	Verified bool `json:"verified"`
}

// VerifyVolunteer 處理 POST /api/v1/admin/volunteer-requests/:userId：授予或撤銷
// 志願者認證。這是權限核心操作（認證後可看精確位置與所有案件的線索列表）。
func (h *AdminHandler) VerifyVolunteer(c *gin.Context) {
	if !isAdmin(c, h.Users) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	userID := c.Param("userId")
	if !uuidRe.MatchString(userID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	var body verifyVolunteerBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 撤銷時也清空申請時間，讓清單不再列出已撤銷的人（除非重新申請）
	if err := h.Users.SetVolunteerVerified(c.Request.Context(), userID, body.Verified); errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update volunteer status"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- 案件管理 ----

// ListCases 處理 GET /api/v1/admin/cases：所有案件（含已結案）的最新在前列表，
// 給管理員快速下架詐騙案/恢復誤結案。只回基本欄位，公開座標（不必要的隱私不出 admin 列表）。
func (h *AdminHandler) ListCases(c *gin.Context) {
	if !isAdmin(c, h.Users) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	limit, offset, q := adminFilterParams(c)
	cases, err := h.Cases.ListAll(c.Request.Context(), limit, offset, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list cases"})
		return
	}
	out := make([]gin.H, 0, len(cases))
	for _, cs := range cases {
		out = append(out, gin.H{
			"id":         cs.ID,
			"title":      cs.Title,
			"case_type":  cs.CaseType,
			"status":     cs.Status,
			"created_at": cs.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"cases": out})
}

type adminCaseStatusBody struct {
	Status string `json:"status" binding:"required"`
}

// SetCaseStatus 處理 PATCH /api/v1/admin/cases/:id/status：管理員改任一案件的狀態
// （下架詐騙案＝closed、誤標團聚＝open 等）。同時寫進案件時間軸讓變更可追蹤。
func (h *AdminHandler) SetCaseStatus(c *gin.Context) {
	adminID, ok := adminUserID(c, h.Users)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	caseID := c.Param("id")
	if !uuidRe.MatchString(caseID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}
	var body adminCaseStatusBody
	if err := c.ShouldBindJSON(&body); err != nil || !validCaseStatuses[body.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be one of: open, found, closed"})
		return
	}
	if err := h.Cases.UpdateStatus(c.Request.Context(), caseID, body.Status); errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update case"})
		return
	}
	if h.Updates != nil {
		label := map[string]string{
			"open":   "管理員開啟此案件（誤結案復原或重新啟動搜尋）",
			"found":  "管理員將此案件標記為已團聚",
			"closed": "管理員結束此案件",
		}[body.Status]
		author := adminID
		_ = h.Updates.Add(c.Request.Context(), caseID, &author, "status_change", label) // 失敗不擋主流程
	}
	c.Status(http.StatusNoContent)
}

// ---- 使用者管理 ----

// ListUsers 處理 GET /api/v1/admin/users：所有未刪除帳號的概觀（新在前）。
// 只回顯示名稱/聯絡/角色/志願者狀態，不放任何其他個人資料。
func (h *AdminHandler) ListUsers(c *gin.Context) {
	if !isAdmin(c, h.Users) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	limit, offset, q := adminFilterParams(c)
	users, err := h.Users.ListUsers(c.Request.Context(), limit, offset, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}
	out := make([]gin.H, 0, len(users))
	for _, u := range users {
		contact := ""
		switch {
		case u.TelegramID != nil:
			contact = "telegram:" + strconv.FormatInt(*u.TelegramID, 10)
		case u.PhoneE164 != nil:
			contact = "phone:" + *u.PhoneE164
		}
		out = append(out, gin.H{
			"id":        u.ID,
			"contact":   contact,
			"role":      u.Role,
			"volunteer": u.VolunteerVerifiedAt != nil,
			"name":      u.DisplayName,
			"joined":    u.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"users": out})
}

type adminSetRoleBody struct {
	Role string `json:"role" binding:"required"`
}

// SetUserRole 處理 POST /api/v1/admin/users/:userId/role：升級（user→admin）或
// 撤銷管理員（admin→user）。「誰能當管理員」的最終開關。
func (h *AdminHandler) SetUserRole(c *gin.Context) {
	adminID, ok := adminUserID(c, h.Users)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	userID := c.Param("userId")
	if !uuidRe.MatchString(userID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	var body adminSetRoleBody
	if err := c.ShouldBindJSON(&body); err != nil || (body.Role != "user" && body.Role != "admin") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be user or admin"})
		return
	}
	// 自我降級防呆：最後一位管理員把自己降級會把整個平台鎖在外面（下一個 admin 只能跑 SQL）。
	if userID == adminID && body.Role != "admin" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot demote yourself; ask another admin or use SQL"})
		return
	}
	if err := h.Users.SetRole(c.Request.Context(), userID, body.Role); errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to set role"})
		return
	}
	c.Status(http.StatusNoContent)
}

// adminUserID 是 isAdmin 的加強版：確認 admin 之餘也把 userID 帶出來
// （給時間軸 author 與自我降級防呆用）。
func adminUserID(c *gin.Context, users *repository.UserRepo) (string, bool) {
	userID, ok := middleware.UserID(c)
	if !ok {
		return "", false
	}
	user, err := users.GetByID(c.Request.Context(), userID)
	if err != nil || user.Role != "admin" {
		return "", false
	}
	return userID, true
}

// ParseAdminTelegramIDs 解析 .env 的 ADMIN_TELEGRAM_IDS（逗號分隔的 Telegram id）。
// 空字串回 empty slice；亂碼的值忽略（不爆破管理，聚合在登入流程的比對上）。
func ParseAdminTelegramIDs(s string) []int64 {
	var out []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if id, err := strconv.ParseInt(part, 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// MaybeAutoPromoteAdmin 在 Telegram 登入成功後檢查：帳號的 telegram_id 若在
// ADMIN_TELEGRAM_IDS 名單內，自動升級為 admin（第一次部署設定第一個管理員用，
// 不需要進 VPS 跑 SQL）。名單還是只在後端設定檔有效，不會被 client 端影響。
func (h *AdminHandler) MaybeAutoPromoteAdmin(ctx context.Context, telegramID int64) {
	if len(h.AdminTelegramIDs) == 0 {
		return
	}
	for _, id := range h.AdminTelegramIDs {
		if id == telegramID {
			if u, err := h.Users.GetByTelegramID(ctx, telegramID); err == nil && u.Role != "admin" {
				_ = h.Users.SetRole(ctx, u.ID, "admin")
				_ = h.Users.SetVolunteerVerified(ctx, u.ID, true) // 第一個管理員通常也會幫忙搜索
			}
			break
		}
	}
}

// ---- 營運設定（Resend / 通知 webhook；管理後台動態修改）----

// adminSettingsBody 欄位一律 *string：nil = 沒要動、"" = 清空、有值 = 覆寫。
// resend_api_key 特別一點："" 也保留既有 key（避免誤清），要換 key 就直接填新的。
type adminSettingsBody struct {
	ResendAPIKey  *string `json:"resend_api_key"`
	ResendFrom    *string `json:"resend_from"`
	NotifyWebhook *string `json:"notify_webhook_url"`
}

// GetSettings 處理 GET /api/v1/admin/settings：只回 from/webhook 原值與「key
// 是否已設定」的旗標；API key 本身絕不回傳（避免一次截圖洩漏憑證）。
func (h *AdminHandler) GetSettings(c *gin.Context) {
	if !isAdmin(c, h.Users) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	snap := h.Settings.Snapshot(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"resend_from":        snap.ResendFrom,
		"resend_api_key_set": snap.ResendAPIKey != "",
		"notify_webhook_url": snap.WebhookURL,
	})
}

// SetSettings 處理 PUT /api/v1/admin/settings：一次可部分更新多個欄位，
// 存進 DB 後 Invalidate 讓通知發送的 30 秒快照立即重讀，改完不用重啟後端。
func (h *AdminHandler) SetSettings(c *gin.Context) {
	if _, ok := adminUserID(c, h.Users); !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}

	var body adminSettingsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.ResendFrom != nil && *body.ResendFrom != "" && !emailRe.MatchString(*body.ResendFrom) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resend_from, expected e.g. alert@your.domain"})
		return
	}

	var firstErr error // 一個請求可以同時改多個 key；任一失敗都回 500
	if body.ResendAPIKey != nil && *body.ResendAPIKey != "" {
		if err := h.Settings.Set(c.Request.Context(), repository.SettingsKeyResendAPIKey, *body.ResendAPIKey); err != nil {
			firstErr = err
		}
	}
	if body.ResendFrom != nil && firstErr == nil {
		if err := h.Settings.Set(c.Request.Context(), repository.SettingsKeyResendFrom, *body.ResendFrom); err != nil {
			firstErr = err
		}
	}
	if body.NotifyWebhook != nil && firstErr == nil {
		if err := h.Settings.Set(c.Request.Context(), repository.SettingsKeyNotifyWebhook, *body.NotifyWebhook); err != nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save settings"})
		return
	}
	h.Settings.Invalidate()
	c.Status(http.StatusNoContent)
}
