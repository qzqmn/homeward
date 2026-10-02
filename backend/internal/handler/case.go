package handler

import (
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
	"homeward/internal/service"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type CaseHandler struct {
	Cases    *repository.CaseRepo
	Users    *repository.UserRepo
	Matching *service.MatchingService
	Notify   *service.NotifyService
}

var validCaseStatuses = map[string]bool{"open": true, "found": true, "closed": true}

var validCaseTypes = map[string]bool{
	"missing_person": true, "missing_pet": true,
	"found_person": true, "found_pet": true,
}

type createCaseBody struct {
	CaseType       string     `json:"case_type" binding:"required"`
	Title          string     `json:"title" binding:"required"`
	Description    string     `json:"description"`
	LastSeenAt     *time.Time `json:"last_seen_at"`
	Lng            float64    `json:"lng" binding:"required"`
	Lat            float64    `json:"lat" binding:"required"`
	SearchRadiusM  int        `json:"search_radius_m"`
	PoliceReportNo *string    `json:"police_report_no"` // 尋人案：選填報案編號
	PhotoPath      *string    `json:"photo_path"`       // 封面照：由 upload-photo 先取得 path 再填入
}

// Create 處理 POST /api/v1/cases（需登入；owner_id 取自 token）。
func (h *CaseHandler) Create(c *gin.Context) {
	userID, _ := middleware.UserID(c) // 路由已掛 AuthRequired，必定存在

	var body createCaseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !validCaseTypes[body.CaseType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case_type"})
		return
	}
	if !validLngLat(body.Lng, body.Lat) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lng/lat"})
		return
	}
	if body.SearchRadiusM <= 0 {
		body.SearchRadiusM = 3000
	}

	created, err := h.Cases.Create(c.Request.Context(), repository.NewCase{
		OwnerID:        userID,
		CaseType:       body.CaseType,
		Title:          body.Title,
		Description:    body.Description,
		LastSeenAt:     body.LastSeenAt,
		Lng:            body.Lng,
		Lat:            body.Lat,
		SearchRadiusM:  body.SearchRadiusM,
		PoliceReportNo: body.PoliceReportNo,
		PhotoPath:      body.PhotoPath,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create case"})
		return
	}

	// 配對比對與通知排程都是「錦上添花」的附加效果：失敗不應該讓建案本身回傳錯誤，
	// 只記錄 log，案主仍然成功拿到剛建立的案件。
	if err := h.Matching.RunFor(c.Request.Context(), created); err != nil {
		log.Printf("WARN: matching for case %s failed: %v", created.ID, err)
	}
	if created.CaseType == "missing_person" || created.CaseType == "missing_pet" {
		if err := h.Notify.EscalateAndEnqueue(c.Request.Context(), created.ID, created.Lng, created.Lat, created.CreatedAt); err != nil {
			log.Printf("WARN: notify enqueue for case %s failed: %v", created.ID, err)
		}
	}

	// 建立者一律看得到精確座標（自己的案件）
	c.JSON(http.StatusCreated, caseJSON(created, true))
}

type updateStatusBody struct {
	Status string `json:"status" binding:"required"` // open / found / closed
}

// UpdateStatus 處理 PATCH /api/v1/cases/:id/status：案主結案或標記「已團聚」（status=found）。
// 只有案主本人可以操作。
func (h *CaseHandler) UpdateStatus(c *gin.Context) {
	id := c.Param("id")
	if !uuidRe.MatchString(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}
	var body updateStatusBody
	if err := c.ShouldBindJSON(&body); err != nil || !validCaseStatuses[body.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be one of: open, found, closed"})
		return
	}

	cs, err := h.Cases.GetByID(c.Request.Context(), id)
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load case"})
		return
	}
	userID, _ := middleware.UserID(c)
	if cs.OwnerID == nil || *cs.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the case owner can update status"})
		return
	}

	if err := h.Cases.UpdateStatus(c.Request.Context(), id, body.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update status"})
		return
	}
	c.Status(http.StatusNoContent)
}

// Get 處理 GET /api/v1/cases/:id。精確座標（center）只回給案主或已認證志願者，
// 其餘一律回傳 public_center（格網模糊化後的座標）。
func (h *CaseHandler) Get(c *gin.Context) {
	id := c.Param("id")
	if !uuidRe.MatchString(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}

	cs, err := h.Cases.GetByID(c.Request.Context(), id)
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load case"})
		return
	}

	c.JSON(http.StatusOK, caseJSON(cs, h.canSeePrecise(c, cs)))
}

// canSeePrecise：本人的案件，或已通過志願者認證的登入使用者，才能看精確座標。
func (h *CaseHandler) canSeePrecise(c *gin.Context, cs repository.Case) bool {
	userID, ok := middleware.UserID(c)
	if !ok {
		return false
	}
	if cs.OwnerID != nil && *cs.OwnerID == userID {
		return true
	}
	user, err := h.Users.GetByID(c.Request.Context(), userID)
	if err != nil {
		return false
	}
	return user.IsVerifiedVolunteer()
}

// ListNearby 處理 GET /api/v1/cases?lat=..&lng=..&radius_m=3000（公開，一律回傳模糊座標）。
func (h *CaseHandler) ListNearby(c *gin.Context) {
	lat, latErr := strconv.ParseFloat(c.Query("lat"), 64)
	lng, lngErr := strconv.ParseFloat(c.Query("lng"), 64)
	if latErr != nil || lngErr != nil || !validLngLat(lng, lat) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lat/lng required and must be valid coordinates"})
		return
	}
	radius := 5000
	if v, err := strconv.Atoi(c.Query("radius_m")); err == nil && v > 0 {
		radius = v
	}
	if radius > 50000 {
		radius = 50000 // 避免一次掃過大範圍拖慢查詢
	}

	cases, err := h.Cases.ListNearby(c.Request.Context(), lng, lat, radius, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list cases"})
		return
	}

	out := make([]gin.H, 0, len(cases))
	for _, cs := range cases {
		out = append(out, caseJSON(cs, false)) // 列表一律模糊座標；詳情頁才依權限決定
	}
	c.JSON(http.StatusOK, gin.H{"cases": out})
}

func caseJSON(cs repository.Case, precise bool) gin.H {
	lng, lat := cs.PublicLng, cs.PublicLat
	if precise {
		lng, lat = cs.Lng, cs.Lat
	}
	var photoURL *string
	if cs.PhotoPath != nil {
		u := "/media/" + *cs.PhotoPath
		photoURL = &u
	}
	return gin.H{
		"id":               cs.ID,
		"case_type":        cs.CaseType,
		"status":           cs.Status,
		"title":            cs.Title,
		"description":      cs.Description,
		"last_seen_at":     cs.LastSeenAt,
		"lng":              lng,
		"lat":              lat,
		"precise_location": precise, // 前端據此顯示「精確位置」或「大概區域」提示
		"search_radius_m":  cs.SearchRadiusM,
		"police_report_no": cs.PoliceReportNo,
		"photo_url":        photoURL,
		"created_at":       cs.CreatedAt,
	}
}

func validLngLat(lng, lat float64) bool {
	return lng >= -180 && lng <= 180 && lat >= -90 && lat <= 90
}
