// Package handler：管理員（users.role='admin'）審核志願者申請。
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"homeward/internal/repository"
)

type AdminHandler struct {
	Users *repository.UserRepo
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
