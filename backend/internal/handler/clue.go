package handler

import (
	"errors"
	"log"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
	"homeward/internal/service"
)

type ClueHandler struct {
	Clues   *repository.ClueRepo
	Cases   *repository.CaseRepo // List / 通知案主需要案件資訊（owner 權限判斷、標題）
	Users   *repository.UserRepo // List 判斷是否認證志願者
	Notify  *service.NotifyService
	Updates *repository.UpdateRepo // 線索事件寫入案件時間軸（nil 時跳過，方便測試）
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
	if !photoPathRe.MatchString(body.PhotoPath) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid photo_path"})
		return
	}
	if utf8.RuneCountInString(body.Note) > 1000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "note too long (max 1000 chars)"})
		return
	}
	if !uuidRe.MatchString(body.ClientID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_id must be a uuid"})
		return
	}

	// 客戶端時鐘可能不準（或故意偽造未來時間把線索洗到列表最上方），一律 clamp 到現在。
	sightedAt := time.Now()
	if body.SightedAt != nil && body.SightedAt.Before(sightedAt) {
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

	// 讓案主知道有新線索（payload 含案件標題／分享連結）。通知屬附加效果，
	// 失敗只記 log，不要擋住線索提交本身。
	if cs, err := h.Cases.GetByID(c.Request.Context(), caseID); err == nil {
		if err := h.Notify.NotifyOwnerOfClue(c.Request.Context(), cs, clue); err != nil {
			log.Printf("WARN: notify owner of clue %s (case %s): %v", clue.ID, caseID, err)
		}
	} else {
		log.Printf("WARN: load case %s for owner-clue notification: %v", caseID, err)
	}

	// 時間軸記一筆：匿名事件（author = 線索提交者，未登入時 NULL）
	if h.Updates != nil {
		kind := "clue"
		body := "有人回報目擊線索"
		if err := h.Updates.Add(c.Request.Context(), caseID, reporterID, kind, body); err != nil {
			// 時間軸失敗不擋主流程
			log.Printf("WARN: case_updates add clue event for case %s: %v", caseID, err)
		}
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

// listClueJSON 是線索在列表中的序列化格式。位置只回座標本身（線索提交時使用者
// 標記的地點）；列表 API 的呼叫者已限定案主/認證志願者，所以不需要模糊化，
// 但也沒有比案件詳情更多的個人資料——reporter 匿名提交時本來就是 NULL。
func clueJSON(clue repository.Clue) gin.H {
	return gin.H{
		"id":         clue.ID,
		"case_id":    clue.CaseID,
		"photo_url":  "/media/" + clue.PhotoPath,
		"lng":        clue.Lng,
		"lat":        clue.Lat,
		"note":       clue.Note,
		"status":     clue.Status,
		"anonymous":  clue.ReporterID == nil,
		"sighted_at": clue.SightedAt,
		"created_at": clue.CreatedAt,
	}
}

// List 處理GET /api/v1/cases/:id/clues：列出案件的線索。
// 只允許案主本人或已認證志願者——線索可能含未經確認的目擊資訊，公開列表
// 會有騷擾／灌水的風險；案主需要它打通「收到通知 → 看到線索 → 聯絡/結案」的主線。
func (h *ClueHandler) List(c *gin.Context) {
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
		c.JSON(http.StatusForbidden, gin.H{"error": "only the case owner or a verified volunteer can view clues"})
		return
	}

	// 上限 200：防止頻繁查詢拖慢查詢；超過就走分頁擴充（未實作）。
	clues, err := h.Clues.ListByCase(c.Request.Context(), caseID, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list clues"})
		return
	}

	out := make([]gin.H, 0, len(clues))
	for _, clue := range clues {
		out = append(out, clueJSON(clue))
	}
	c.JSON(http.StatusOK, gin.H{"clues": out})
}

var validClueStatuses = map[string]bool{"pending": true, "credible": true, "dismissed": true}

type updateClueStatusBody struct {
	Status string `json:"status" binding:"required"` // credible / dismissed（pending 用於撤銷誤按）
}

// UpdateClueStatus 處理 PATCH /api/v1/cases/:id/clues/:clueId/status：案主把
// 線索標記為「可信」（credible）或「已排除」（dismissed）。只允許案主本人。
func (h *ClueHandler) UpdateStatus(c *gin.Context) {
	caseID, clueID := c.Param("id"), c.Param("clueId")
	if !uuidRe.MatchString(caseID) || !uuidRe.MatchString(clueID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body updateClueStatusBody
	if err := c.ShouldBindJSON(&body); err != nil || !validClueStatuses[body.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be one of: pending, credible, dismissed"})
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
	if !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the case owner can review clues"})
		return
	}

	if err := h.Clues.SetStatus(c.Request.Context(), caseID, clueID, body.Status); errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "clue not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to review clue"})
		return
	}

	// 線索被案主確認是重要進展，寫入時間軸
	if h.Updates != nil {
		label := map[string]string{
			"credible":  "案主確認了一則目擊線索為可信",
			"dismissed": "案主排除了一則目擊線索",
			"pending":   "案主將一則目擊線索改回待確認",
		}[body.Status]
		owner := cs.OwnerID // 案主本人 = author
		if err := h.Updates.Add(c.Request.Context(), caseID, owner, "clue", label); err != nil {
			log.Printf("WARN: case_updates add review event for case %s: %v", caseID, err)
		}
	}
	c.Status(http.StatusNoContent)
}
