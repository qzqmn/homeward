// Package handler：案件時間軸（case_updates）讀取。
// 給案主與認證志願者回看「這個案件的完整過程」：建立、線索、確認、結案。
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
)

type UpdateHandler struct {
	Updates *repository.UpdateRepo
	Cases   *repository.CaseRepo
	Users   *repository.UserRepo
}

// List 處理 GET /api/v1/cases/:id/updates：案件的時間軸事件（新→舊）。
// 權限同線索列表：案主本人或已認證志願者——時間軸會描述線索的確認過程，
// 公開程度與線索一致。
func (h *UpdateHandler) List(c *gin.Context) {
	caseID := c.Param("id")
	if !uuidRe.MatchString(caseID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}

	cs, err := h.Cases.GetByID(c.Request.Context(), caseID)
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load case"})
		return
	}

	userID, _ := middleware.UserID(c)
	isOwner := cs.OwnerID != nil && userID == *cs.OwnerID
	var isVolunteer bool
	if !isOwner && userID != "" {
		if user, err := h.Users.GetByID(c.Request.Context(), userID); err == nil {
			isVolunteer = user.IsVerifiedVolunteer()
		}
	}
	if !isOwner && !isVolunteer {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the case owner or a verified volunteer can view updates"})
		return
	}

	updates, err := h.Updates.ListForCase(c.Request.Context(), caseID, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list updates"})
		return
	}

	out := make([]gin.H, 0, len(updates))
	for _, u := range updates {
		out = append(out, gin.H{
			"id":         u.ID,
			"kind":       u.Kind,
			"body":       u.Body,
			"created_at": u.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"updates": out})
}
