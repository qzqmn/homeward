// Package handler：SOS 求助（志願者搜索現場需要的救援事件）。
package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
)

type SosHandler struct {
	Sos    *repository.SosRepo
	Users  *repository.UserRepo
	Notify *repository.NotificationRepo
}

type createSosBody struct {
	Lng    float64 `json:"lng" binding:"required"`
	Lat    float64 `json:"lat" binding:"required"`
	Note   string  `json:"note"`    // 說明需要什麼協助（選填）
	CaseID string  `json:"case_id"` // 可選：在哪個案件旁邊發的
}

// Create 處理 POST /api/v1/sos：登入使用者（認證志願者場景）發出求救。
// 建立事件後通知所有管理員（他們的渠道照常走出通知佇列：tel/email/webhook），
// 讓現場狀況有人看得到——不另做新通知渠道，kind 用 'system'。
func (h *SosHandler) Create(c *gin.Context) {
	userID, _ := middleware.UserID(c) // 路由已掛 requireAuth

	var body createSosBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lng/lat required"})
		return
	}
	if !validLngLat(body.Lng, body.Lat) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lng/lat"})
		return
	}
	if len([]rune(body.Note)) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "note too long (max 500 chars)"})
		return
	}
	var caseID *string
	if body.CaseID != "" {
		if !uuidRe.MatchString(body.CaseID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case_id"})
			return
		}
		caseID = &body.CaseID
	}

	ev, err := h.Sos.Create(c.Request.Context(), userID, caseID, body.Lng, body.Lat, body.Note)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create sos"})
		return
	}

	// 通知所有管理員。查管理員 + 發送者名稱屬附加資訊，失敗不擋主流程（SOS 已記錄）。
	u, err := h.Users.GetByID(c.Request.Context(), userID)
	displayName := "某位志願者"
	if err == nil && u.DisplayName != "" {
		displayName = u.DisplayName
	}
	payload := map[string]any{
		"sos_id":     ev.ID,
		"from_user":  displayName,
		"lng":        ev.Lng,
		"lat":        ev.Lat,
		"note":       ev.Note,
		"case_id":    ev.CaseID,
		"started_at": ev.CreatedAt,
	}
	raw, _ := json.Marshal(payload)
	admins, err := h.Users.ListAdminUserIDs(c.Request.Context())
	if err == nil {
		for _, adminID := range admins {
			// 管理員的每個通知渠道都各排一筆（tel/email/webhook 照常走出送流程）
			if _, err := h.Notify.EnqueueToUser(c.Request.Context(), adminID, derefUUID(ev.CaseID), "system", raw); err != nil {
				log.Printf("WARN: sos notify admin %s: %v", adminID, err)
			}
		}
	}

	c.JSON(http.StatusCreated, gin.H{"id": ev.ID, "created_at": ev.CreatedAt})
}

// AdminListOpen 處理 GET /api/v1/admin/sos：仍未處理的 SOS 清單。
func (h *SosHandler) AdminListOpen(c *gin.Context) {
	if !isAdmin(c, h.Users) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	events, err := h.Sos.ListOpen(c.Request.Context(), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list sos"})
		return
	}
	out := make([]gin.H, 0, len(events))
	for _, ev := range events {
		out = append(out, gin.H{
			"id":         ev.ID,
			"user_id":    ev.UserID,
			"case_id":    ev.CaseID,
			"lng":        ev.Lng,
			"lat":        ev.Lat,
			"note":       ev.Note,
			"created_at": ev.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"sos": out})
}

type resolveSosBody struct {
	ID int64 `json:"id" binding:"required"`
}

// AdminResolve 處理 POST /api/v1/admin/sos/:id/resolve：標記 SOS 已處理。
func (h *SosHandler) AdminResolve(c *gin.Context) {
	if !isAdmin(c, h.Users) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sos id"})
		return
	}
	if err := h.Sos.Resolve(c.Request.Context(), id); errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "sos not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve sos"})
		return
	}
	c.Status(http.StatusNoContent)
}

// derefUUID：*string 的 case id 過 h.Notify.EnqueueToUser 需要 string；nil → ""。
func derefUUID(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
