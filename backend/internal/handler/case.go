package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
	"homeward/internal/service"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// photoPathRe 驗證 photo_path 必須是 upload-photo 回傳的相對路徑格式
// （YYYY/MM/32hex.jpg｜png，月 01-12）。客戶端字串一律先過這關，防止引用
// 任意（甚至別人的）媒體檔案當封面照。body 驗證屬 handler 職責，不只在
// repository 擋 SQL injection。
var photoPathRe = regexp.MustCompile(`^[0-9]{4}/(0[1-9]|1[0-2])/[0-9a-f]{32}\.(jpg|png)$`)

const (
	maxTitleLen       = 100
	maxDescriptionLen = 2000
	maxCaseRadiusM    = 20000
)

type CaseHandler struct {
	Cases    *repository.CaseRepo
	Users    *repository.UserRepo
	Matching *service.MatchingService
	Notify   *service.NotifyService
	Updates  *repository.UpdateRepo    // 案件時間軸（nil 時跳過，方便測試）
	Photos   *repository.CasePhotoRepo // 補傳現場照片（nil 時 Get 不回 photo_urls）
	Uploader *PhotoHandler             // 補傳照片共用同一條安全上傳管道（nil 時無法補傳）
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
	if utf8.RuneCountInString(body.Title) > maxTitleLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title too long (max 100 chars)"})
		return
	}
	if utf8.RuneCountInString(body.Description) > maxDescriptionLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description too long (max 2000 chars)"})
		return
	}
	if body.PhotoPath != nil && *body.PhotoPath != "" && !photoPathRe.MatchString(*body.PhotoPath) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid photo_path"})
		return
	}
	if body.SearchRadiusM <= 0 {
		body.SearchRadiusM = 3000
	}
	if body.SearchRadiusM > maxCaseRadiusM {
		body.SearchRadiusM = maxCaseRadiusM
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
		cand := repository.EscalationCandidate{
			ID: created.ID, CaseType: created.CaseType, Title: created.Title,
			PhotoPath: created.PhotoPath, Lng: created.Lng, Lat: created.Lat,
			CreatedAt: created.CreatedAt,
		}
		if err := h.Notify.EscalateAndEnqueue(c.Request.Context(), cand); err != nil {
			log.Printf("WARN: notify enqueue for case %s failed: %v", created.ID, err)
		}
	}

	// 建立者一律看得到精確座標（自己的案件）
	// 時間軸第一筆：案件建立（kind=system，author=案主）
	if h.Updates != nil {
		if err := h.Updates.Add(c.Request.Context(), created.ID, &userID, "system", "案件建立，開始搜尋"); err != nil {
			log.Printf("WARN: case_updates add create event: %v", err)
		}
	}
	c.JSON(http.StatusCreated, caseJSON(created, true, true))
}

type updateStatusBody struct {
	Status string `json:"status" binding:"required"` // open / found / closed
} // UpdateStatus 處理 PATCH /api/v1/cases/:id/status：案主結案或標記「已團聚」（status=found）。
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

	// 狀態變更寫入時間軸（案主看得到完整過程）
	if h.Updates != nil {
		label := map[string]string{
			"open":   "恢復搜尋",
			"found":  "標記為已團聚！",
			"closed": "結案，停止搜尋",
		}[body.Status]
		if err := h.Updates.Add(c.Request.Context(), id, &userID, "status_change", label); err != nil {
			log.Printf("WARN: case_updates add status event: %v", err)
		}
	}
	c.Status(http.StatusNoContent)
}

// maxCasePhotos 一案補傳照片上限（含封面照之外的 case_photos 總數）。
const maxCasePhotos = 12

// AddPhoto 處理 POST /api/v1/cases/:id/photos（multipart/form-data，欄位 "photo"）：
// 案主或認證志願者補傳現場對比照。走 upload-photo 同一條編碼剝 EXIF 的管道。
func (h *CaseHandler) AddPhoto(c *gin.Context) {
	if h.Photos == nil || h.Uploader == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "photo upload not configured"})
		return
	}
	id := c.Param("id")
	if !uuidRe.MatchString(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}
	userID, _ := middleware.UserID(c) // 路由已掛 requireAuth
	user, err := h.Users.GetByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	if !user.IsVerifiedVolunteer() {
		// 不是志願者 → 只能是案主本人補傳
		cs, err := h.Cases.GetByID(c.Request.Context(), id)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load case"})
			return
		}
		if cs.OwnerID == nil || *cs.OwnerID != userID {
			c.JSON(http.StatusForbidden, gin.H{"error": "only the case owner or a verified volunteer can add photos"})
			return
		}
	}

	n, err := h.Photos.CountForCase(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count photos"})
		return
	}
	if n >= maxCasePhotos {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("photo limit reached (max %d)", maxCasePhotos)})
		return
	}

	rel, ok := h.Uploader.saveImage(c)
	if !ok {
		return
	}
	if err := h.Photos.Add(c.Request.Context(), id, rel, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save photo record"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"path": rel, "url": "/media/" + rel})
}

// ListMine 處理 GET /api/v1/me/cases：回傳登入者建立的所有案件（含已結案的），
// 讓案主可以在「我的案件」清單找到自己發過的案子，不再只能從附近清單撈
// （結案後案件會從附近清單消失，案主就再也找不到）。
func (h *CaseHandler) ListMine(c *gin.Context) {
	userID, _ := middleware.UserID(c)

	cases, err := h.Cases.ListByOwner(c.Request.Context(), userID, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list cases"})
		return
	}

	out := make([]gin.H, 0, len(cases))
	for _, cs := range cases {
		// 案主本人：精確座標 + is_owner（前端據此顯示結案按鈕）
		out = append(out, caseJSON(cs, true, true))
	}
	c.JSON(http.StatusOK, gin.H{"cases": out})
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

	j := caseJSON(cs, h.canSeePrecise(c, cs), h.isOwnerOf(c, cs))
	// 補傳的現場照片（封面照之外）：詳情頁相冊用
	if h.Photos != nil {
		paths, err := h.Photos.List(c.Request.Context(), id)
		if err != nil {
			log.Printf("WARN: list case_photos for %s: %v", id, err)
		} else if len(paths) > 0 {
			urls := make([]string, len(paths))
			for i, p := range paths {
				urls[i] = "/media/" + p
			}
			j["photo_urls"] = urls
		}
	}
	c.JSON(http.StatusOK, j)
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

// isOwnerOf 判斷本次請求的使用者是不是案件的建立者。cheap check（不查 DB），
// caseJSON 回傳的 is_owner 欄位就是這個——前端據此決定顯示「結案／已團聚」按鈕。
func (h *CaseHandler) isOwnerOf(c *gin.Context, cs repository.Case) bool {
	userID, ok := middleware.UserID(c)
	if !ok || cs.OwnerID == nil {
		return false
	}
	return *cs.OwnerID == userID
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
		out = append(out, caseJSON(cs, false, h.isOwnerOf(c, cs))) // 列表一律模糊座標；詳情頁才依權限決定
	}
	c.JSON(http.StatusOK, gin.H{"cases": out})
}

// caseJSON 把案件序列化成 API 回應。precise 控制座標是否精確（僅案主/認證志願者），
// isOwner 告訴前端「這是不是我發的案件」（決定顯示結案按鈕）。
func caseJSON(cs repository.Case, precise bool, isOwner bool) gin.H {
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
		"is_owner":         isOwner, // 前端據此顯示結案／狀態變更按鈕
		"search_radius_m":  cs.SearchRadiusM,
		"police_report_no": cs.PoliceReportNo,
		"photo_url":        photoURL,
		"created_at":       cs.CreatedAt,
	}
}

func validLngLat(lng, lat float64) bool {
	return lng >= -180 && lng <= 180 && lat >= -90 && lat <= 90
}

// updateCaseBody 是案主編輯案件內容的請求：全部 pointer、nil = 不改該欄位。
// 座標/search_radius 不開放編輯（會影響模糊化與配對）；要改位置請重新發案。
type updateCaseBody struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
}

// Update 處理 PATCH /api/v1/cases/:id：案主補充/修正文字內容
// （title/description/last_seen_at）。只有案主本人可以。
func (h *CaseHandler) Update(c *gin.Context) {
	id := c.Param("id")
	if !uuidRe.MatchString(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}
	var body updateCaseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Title == nil && body.Description == nil && body.LastSeenAt == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nothing to update"})
		return
	}
	if body.Title != nil && utf8.RuneCountInString(*body.Title) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title cannot be empty"})
		return
	}
	if body.Title != nil && utf8.RuneCountInString(*body.Title) > maxTitleLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title too long (max 100 chars)"})
		return
	}
	if body.Description != nil && utf8.RuneCountInString(*body.Description) > maxDescriptionLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description too long (max 2000 chars)"})
		return
	}

	userID, _ := middleware.UserID(c)
	in := repository.UpdateCaseInput{ID: id, OwnerID: userID, Title: body.Title, Description: body.Description, LastSeenAt: body.LastSeenAt}
	if err := h.Cases.Update(c.Request.Context(), in); errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the case owner can update the case"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update case"})
		return
	}

	// 編輯內容寫進時間軸，案主與志願者看得到改動紀錄
	if h.Updates != nil {
		if err := h.Updates.Add(c.Request.Context(), id, &userID, "note", "案主更新了案件內容"); err != nil {
			log.Printf("WARN: case_updates add edit event: %v", err)
		}
	}

	cs, err := h.Cases.GetByID(c.Request.Context(), id)
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, caseJSON(cs, true, true))
}
