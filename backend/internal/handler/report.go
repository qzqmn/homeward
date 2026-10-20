// Package handler：舉報（防詐/騷擾/垃圾內容）與管理員審核。
package handler

import (
	"errors"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
)

// isAdmin 判斷目前登入使用者是不是管理員（users.role='admin'）。
// handler 包內共用的權限 helper：admin API 每個 endpoint 都要先呼叫一次，
// 不放 middleware 是為了避免 middleware 依賴 repository 造成的循環。
func isAdmin(c *gin.Context, users *repository.UserRepo) bool {
	userID, ok := middleware.UserID(c)
	if !ok {
		return false
	}
	user, err := users.GetByID(c.Request.Context(), userID)
	if err != nil {
		return false
	}
	return user.Role == "admin"
}

type ReportHandler struct {
	Reports *repository.ReportRepo
	Users   *repository.UserRepo // admin 端點判斷 role 用
}

var validReasons = map[string]bool{"scam": true, "harassment": true, "privacy": true, "spam": true, "other": true}

type createReportBody struct {
	TargetType string `json:"target_type" binding:"required"` // case / clue / user
	TargetID   string `json:"target_id" binding:"required"`
	Reason     string `json:"reason" binding:"required"`
	Detail     string `json:"detail"`
}

// Create 處理 POST /api/v1/reports（需登入；匿名舉報會讓惡意灌水無法追蹤）。
// 目標必須真實存在；同一人對同一目標可以重複舉報，但 DB 會一起列出給管理員看
// （多篇 = 多人回報同一問題的訊號，刻意不擋）。
func (h *ReportHandler) Create(c *gin.Context) {
	userID, _ := middleware.UserID(c)

	var body createReportBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !validReasons[body.Reason] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reason, expected scam/harassment/privacy/spam/other"})
		return
	}
	if body.TargetType != "case" && body.TargetType != "clue" && body.TargetType != "user" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid target_type, expected case/clue/user"})
		return
	}
	if !uuidRe.MatchString(body.TargetID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid target_id"})
		return
	}
	if utf8.RuneCountInString(body.Detail) > 1000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "detail too long (max 1000 chars)"})
		return
	}

	exists, err := h.Reports.TargetExists(c.Request.Context(), body.TargetType, body.TargetID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate report target"})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "report target not found"})
		return
	}

	rep, err := h.Reports.Create(c.Request.Context(), repository.NewReport{
		ReporterID: &userID,
		TargetType: body.TargetType,
		TargetID:   body.TargetID,
		Reason:     body.Reason,
		Detail:     body.Detail,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save report"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": rep.ID, "status": rep.Status})
}

// ListOpen 處理 GET /api/v1/admin/reports：未處理完結的舉報清單（僅管理員）。
func (h *ReportHandler) ListOpen(c *gin.Context) {
	if !isAdmin(c, h.Users) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	reports, err := h.Reports.ListOpen(c.Request.Context(), 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list reports"})
		return
	}
	out := make([]gin.H, 0, len(reports))
	for _, rep := range reports {
		out = append(out, gin.H{
			"id":          rep.ID,
			"target_type": rep.TargetType,
			"target_id":   rep.TargetID,
			"reason":      rep.Reason,
			"detail":      rep.Detail,
			"status":      rep.Status,
			"created_at":  rep.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"reports": out})
}

var validReportStatuses = map[string]bool{"reviewing": true, "actioned": true, "dismissed": true}

type updateReportBody struct {
	Status string `json:"status" binding:"required"`
}

// UpdateStatus 處理 PATCH /api/v1/admin/reports/:id：管理員處理舉報。
func (h *ReportHandler) UpdateStatus(c *gin.Context) {
	if !isAdmin(c, h.Users) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid report id"})
		return
	}
	var body updateReportBody
	if err := c.ShouldBindJSON(&body); err != nil || !validReportStatuses[body.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be one of: reviewing, actioned, dismissed"})
		return
	}
	if err := h.Reports.SetStatus(c.Request.Context(), id, body.Status); errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "report not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update report"})
		return
	}
	c.Status(http.StatusNoContent)
}
