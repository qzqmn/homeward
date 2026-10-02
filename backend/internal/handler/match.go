// Package handler：撿到/發現案件與走失案件的配對建議。
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
)

type MatchHandler struct {
	Matches *repository.MatchRepo
	Cases   *repository.CaseRepo
}

// List 處理 GET /api/v1/cases/:id/matches：列出系統找到的配對建議。
// 只有案主能看（配對建議會透露對方案件的存在，不宜公開）。
func (h *MatchHandler) List(c *gin.Context) {
	caseID := c.Param("id")
	if !uuidRe.MatchString(caseID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}
	if !h.isOwner(c, caseID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the case owner can view matches"})
		return
	}

	matches, err := h.Matches.ListForCase(c.Request.Context(), caseID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list matches"})
		return
	}

	out := make([]gin.H, 0, len(matches))
	for _, m := range matches {
		out = append(out, gin.H{
			"matched_case_id": m.MatchedCaseID,
			"score":           m.Score,
			"status":          m.Status,
			"title":           m.Title,
			"case_type":       m.CaseType,
			"lng":             m.Lng, // 一律模糊座標：配對建議不代表已確認，不外洩精確位置
			"lat":             m.Lat,
		})
	}
	c.JSON(http.StatusOK, gin.H{"matches": out})
}

type updateMatchBody struct {
	Status string `json:"status" binding:"required"` // confirmed / rejected
}

// UpdateStatus 處理 PATCH /api/v1/cases/:id/matches/:matchedId：案主確認或排除一筆配對建議。
func (h *MatchHandler) UpdateStatus(c *gin.Context) {
	caseID := c.Param("id")
	matchedID := c.Param("matchedId")
	if !uuidRe.MatchString(caseID) || !uuidRe.MatchString(matchedID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body updateMatchBody
	if err := c.ShouldBindJSON(&body); err != nil || (body.Status != "confirmed" && body.Status != "rejected") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be confirmed or rejected"})
		return
	}
	if !h.isOwner(c, caseID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the case owner can update matches"})
		return
	}

	err := h.Matches.UpdateStatus(c.Request.Context(), caseID, matchedID, body.Status)
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update match"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *MatchHandler) isOwner(c *gin.Context, caseID string) bool {
	userID, ok := middleware.UserID(c)
	if !ok {
		return false
	}
	cs, err := h.Cases.GetByID(c.Request.Context(), caseID)
	if err != nil {
		return false
	}
	return cs.OwnerID != nil && *cs.OwnerID == userID
}
