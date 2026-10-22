package handler

// 搜索任務清單/斷點續傳（000013 migration）：
// 案主（或管理員）把一次搜索切成可認領的小任務；志願者在 TrackSearchScreen
// 認領 → 回報進度（文字+當前位置，作為「斷點」）→ 完成。認領者換人時任務
// 自動回到 open，進度保留給下一個人。

import (
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
)

const (
	maxTaskTitleLen    = 100
	maxTaskNoteLen     = 2000
	maxTaskProgressLen = 1000
)

type TaskHandler struct {
	Tasks *repository.SearchTaskRepo
	Cases *repository.CaseRepo
	Users *repository.UserRepo
}

func taskJSON(t repository.SearchTask) gin.H {
	var note string
	if t.Note != nil {
		note = *t.Note
	}
	j := gin.H{
		"id":            t.ID,
		"case_id":       t.CaseID,
		"title":         t.Title,
		"note":          note,
		"status":        t.Status,
		"claimed_by_me": t.ClaimedBy != nil,
		"progress_note": t.ProgressNote,
	}
	if t.LastLng != nil && t.LastLat != nil {
		j["last_point"] = gin.H{"lng": *t.LastLng, "lat": *t.LastLat}
	}
	if t.DoneAt != nil {
		j["done_at"] = t.DoneAt
	}
	j["created_at"] = t.CreatedAt
	return j
}

// authorCheck：使用者對案件可否建立/管理任務（案主本人或管理員）。
func (h *TaskHandler) isCaseManager(c *gin.Context, caseID string) (bool, error) {
	userID, _ := middleware.UserID(c)
	cs, err := h.Cases.GetByID(c.Request.Context(), caseID)
	if err != nil {
		return false, err
	}
	if cs.OwnerID != nil && *cs.OwnerID == userID {
		return true, nil
	}
	user, err := h.Users.GetByID(c.Request.Context(), userID)
	if err != nil {
		return false, err
	}
	return user.Role == "admin", nil
}

// canSee 是認證志願者/案主/管理員——任務清單跟線索清單同一權限水線。
func (h *TaskHandler) canSee(c *gin.Context, caseID string) bool {
	userID, _ := middleware.UserID(c)
	user, err := h.Users.GetByID(c.Request.Context(), userID)
	if err != nil {
		return false
	}
	if user.IsVerifiedVolunteer() {
		return true
	}
	cs, err := h.Cases.GetByID(c.Request.Context(), caseID)
	if err != nil {
		return false
	}
	return cs.OwnerID != nil && *cs.OwnerID == userID
}

// List 處理 GET /api/v1/cases/:id/tasks：案件的任務清單（斷點續傳的入口）。
func (h *TaskHandler) List(c *gin.Context) {
	caseID := c.Param("id")
	if !uuidRe.MatchString(caseID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}
	if !h.canSee(c, caseID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only case owner or verified volunteers can see tasks"})
		return
	}
	tasks, err := h.Tasks.ListByCase(c.Request.Context(), caseID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tasks"})
		return
	}
	// claimed 附帶誰認領的顯示名（斷點交接時要知道找誰）
	out := make([]gin.H, 0, len(tasks))
	for _, t := range tasks {
		j := taskJSON(t)
		if t.ClaimedBy != nil && (t.Status == "claimed" || t.Status == "done") {
			if u, err := h.Users.GetByID(c.Request.Context(), *t.ClaimedBy); err == nil {
				j["claimed_by"] = u.DisplayName
			}
		}
		out = append(out, j)
	}
	c.JSON(http.StatusOK, gin.H{"tasks": out})
}

// Create 處理 POST /api/v1/cases/:id/tasks：案主（或管理員）增設任務。
func (h *TaskHandler) Create(c *gin.Context) {
	caseID := c.Param("id")
	if !uuidRe.MatchString(caseID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}
	mgr, err := h.isCaseManager(c, caseID)
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load case"})
		return
	}
	if !mgr {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the case owner or an admin can create tasks"})
		return
	}

	var body struct {
		Title string  `json:"title" binding:"required"`
		Note  *string `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title is required"})
		return
	}
	if utf8.RuneCountInString(body.Title) > maxTaskTitleLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title too long"})
		return
	}
	if body.Note != nil && utf8.RuneCountInString(*body.Note) > maxTaskNoteLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "note too long"})
		return
	}
	userID, _ := middleware.UserID(c)
	t, err := h.Tasks.Create(c.Request.Context(), caseID, body.Title, body.Note, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create task"})
		return
	}
	c.JSON(http.StatusCreated, taskJSON(t))
}

// Claim 處理 POST /api/v1/cases/:id/tasks/:taskId/claim：認證志願者認領任務。
func (h *TaskHandler) Claim(c *gin.Context) {
	taskID, ok := h.transition(c)
	if !ok {
		return
	}
	userID, _ := middleware.UserID(c)
	t, err := h.Tasks.Claim(c.Request.Context(), taskID, userID)
	if errors.Is(err, repository.ErrTaskNotClaimable) {
		c.JSON(http.StatusConflict, gin.H{"error": "任務已被認領或已結束"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to claim task"})
		return
	}
	c.JSON(http.StatusOK, taskJSON(t))
}

// Release 處理 POST /api/v1/cases/:id/tasks/:taskId/release：認領者退回任務
// （進度保留，給下一個人接手）。
func (h *TaskHandler) Release(c *gin.Context) {
	taskID, ok := h.transition(c)
	if !ok {
		return
	}
	userID, _ := middleware.UserID(c)
	t, err := h.Tasks.Release(c.Request.Context(), taskID, userID)
	if errors.Is(err, repository.ErrTaskNotClaimable) {
		c.JSON(http.StatusConflict, gin.H{"error": "只有認領者本人可以退回"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to release task"})
		return
	}
	c.JSON(http.StatusOK, taskJSON(t))
}

// Progress 處理 POST /api/v1/cases/:id/tasks/:taskId/progress：
// 回報斷點（進度文字 + 當前位置）；TrackSearchScreen 的「記錄搜索進度」按鈕。
func (h *TaskHandler) Progress(c *gin.Context) {
	taskID, ok := h.transition(c)
	if !ok {
		return
	}
	var body struct {
		Note string  `json:"note" binding:"required"`
		Lng  float64 `json:"lng" binding:"required"`
		Lat  float64 `json:"lat" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || !validLngLat(body.Lng, body.Lat) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "note and valid lng/lat are required"})
		return
	}
	if utf8.RuneCountInString(body.Note) > maxTaskProgressLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "note too long"})
		return
	}
	userID, _ := middleware.UserID(c)
	t, err := h.Tasks.UpdateProgress(c.Request.Context(), taskID, userID, body.Note, body.Lng, body.Lat)
	if errors.Is(err, repository.ErrTaskNotClaimable) {
		c.JSON(http.StatusConflict, gin.H{"error": "先認領任務才能記錄進度"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update progress"})
		return
	}
	c.JSON(http.StatusOK, taskJSON(t))
}

// Done 處理 POST /api/v1/cases/:id/tasks/:taskId/done：任務完成。
func (h *TaskHandler) Done(c *gin.Context) {
	taskID, ok := h.transition(c)
	if !ok {
		return
	}
	userID, _ := middleware.UserID(c)
	user, err := h.Users.GetByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	t, err := h.Tasks.Done(c.Request.Context(), taskID, userID, user.Role == "admin")
	if errors.Is(err, repository.ErrTaskNotClaimable) {
		c.JSON(http.StatusConflict, gin.H{"error": "只有認領者或管理員可以標記完成"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to complete task"})
		return
	}
	c.JSON(http.StatusOK, taskJSON(t))
}

// transition 驗路由參數（caseId + taskId 形狀）與檢視權限，回 taskId。
func (h *TaskHandler) transition(c *gin.Context) (string, bool) {
	caseID := c.Param("id")
	taskID := c.Param("taskId")
	if !uuidRe.MatchString(caseID) || !uuidRe.MatchString(taskID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return "", false
	}
	if !h.canSee(c, caseID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only case owner or verified volunteers can operate tasks"})
		return "", false
	}
	return taskID, true
}
