package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"homeward/internal/middleware"
	"homeward/internal/repository"
)

type TrackHandler struct {
	Tracks *repository.TrackRepo
}

// Coverage 處理 GET /api/v1/cases/:id/coverage：把志願者軌跡聚合成格網熱圖，
// 讓大家看到「哪裡已經搜過、多久以前搜的」，避免重複搜索同一塊區域。
// 公開（不需登入）：這是給所有想參與搜索的人看的工具，不是案件的私密資訊。
func (h *TrackHandler) Coverage(c *gin.Context) {
	caseID := c.Param("id")
	if !uuidRe.MatchString(caseID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}

	const cellSizeDeg = 0.001 // 約 100 公尺見方；案件範圍很大時可考慮改用查詢參數放大格子
	cells, err := h.Tracks.CoverageGrid(c.Request.Context(), caseID, cellSizeDeg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load coverage"})
		return
	}

	out := make([]gin.H, 0, len(cells))
	for _, cell := range cells {
		out = append(out, gin.H{
			"lng":          cell.Lng,
			"lat":          cell.Lat,
			"point_count":  cell.PointCount,
			"last_visited": cell.LastVisited,
		})
	}
	c.JSON(http.StatusOK, gin.H{"cells": out})
}

type createTrackBody struct {
	Lng        float64    `json:"lng" binding:"required"`
	Lat        float64    `json:"lat" binding:"required"`
	AccuracyM  *float32   `json:"accuracy_m"`
	RecordedAt *time.Time `json:"recorded_at"` // 客戶端定位時間
	ClientID   string     `json:"client_id" binding:"required"`
}

// Create 處理 POST /api/v1/cases/:id/tracks。需登入（志願者「搜索模式」開啟時，
// 前端才會呼叫這支 API）。同樣以 client_id 冪等，支援離線佇列重送。
func (h *TrackHandler) Create(c *gin.Context) {
	caseID := c.Param("id")
	if !uuidRe.MatchString(caseID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid case id"})
		return
	}
	userID, _ := middleware.UserID(c) // 路由已掛 AuthRequired

	var body createTrackBody
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

	recordedAt := time.Now()
	if body.RecordedAt != nil {
		recordedAt = *body.RecordedAt
	}

	err := h.Tracks.Create(c.Request.Context(), repository.NewTrack{
		VolunteerID: userID,
		CaseID:      caseID,
		Lng:         body.Lng,
		Lat:         body.Lat,
		AccuracyM:   body.AccuracyM,
		RecordedAt:  recordedAt,
		ClientID:    body.ClientID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save track point"})
		return
	}
	c.Status(http.StatusNoContent) // 軌跡點高頻寫入，成功時不需要回傳內容
}
