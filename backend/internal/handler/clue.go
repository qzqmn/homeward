package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
)

type ClueHandler struct {
	Clues *repository.ClueRepo
}

type createClueBody struct {
	PhotoPath string     `json:"photo_path" binding:"required"` // 由 upload-photo 回傳的 path
	Lng       float64    `json:"lng" binding:"required"`
	Lat       float64    `json:"lat" binding:"required"`
	Note      string     `json:"note"`
	SightedAt *time.Time `json:"sighted_at"`                   // 客戶端記錄的目擊時間；離線補傳時可能早於送達時間
	ClientID  string     `json:"client_id" binding:"required"` // 前端產生的 UUID，用於離線重送去重
}

// Create 處理 POST /api/v1/cases/:id/clues。免登入即可提交；若帶有效 token，
// 會把 reporter_id 記為該使用者，否則視為匿名線索。
//
// 冪等：同一 client_id 重送只會回傳原有紀錄，不會重複新增（見 ClueRepo.Create）。
func (h *ClueHandler) Create(c *gin.Context) {
	caseID := c.Param("id")
	if !uuidRe.MatchString(caseID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}

	var body createClueBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !validLngLat(body.Lng, body.Lat) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lng/lat"})
		return
	}
	if !uuidRe.MatchString(body.ClientID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_id must be a uuid"})
		return
	}

	sightedAt := time.Now()
	if body.SightedAt != nil {
		sightedAt = *body.SightedAt
	}

	var reporterID *string
	if userID, ok := middleware.UserID(c); ok {
		reporterID = &userID
	}

	clue, err := h.Clues.Create(c.Request.Context(), repository.NewClue{
		CaseID:     caseID,
		PhotoPath:  body.PhotoPath,
		Lng:        body.Lng,
		Lat:        body.Lat,
		Note:       body.Note,
		ReporterID: reporterID,
		SightedAt:  sightedAt,
		ClientID:   body.ClientID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save clue"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":         clue.ID,
		"case_id":    clue.CaseID,
		"photo_path": clue.PhotoPath,
		"lng":        clue.Lng,
		"lat":        clue.Lat,
		"note":       clue.Note,
		"status":     clue.Status, // pending：新線索一律待確認，由案主或管理者改判
		"sighted_at": clue.SightedAt,
		"created_at": clue.CreatedAt,
	})
}
