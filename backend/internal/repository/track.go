package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type NewTrack struct {
	VolunteerID string
	CaseID      string
	Lng, Lat    float64
	AccuracyM   *float32
	RecordedAt  time.Time // 客戶端定位時間
	ClientID    string    // 冪等鍵：離線重送去重
}

type TrackRepo struct{ db *pgxpool.Pool }

func NewTrackRepo(db *pgxpool.Pool) *TrackRepo { return &TrackRepo{db: db} }

// Create 同樣以 client_id 冪等；軌跡點寫入頻繁，這裡不查詢既有紀錄回傳，
// 重送只需確保「不重複寫入」，呼叫端不需要拿到既有列的內容。
func (r *TrackRepo) Create(ctx context.Context, in NewTrack) error {
	// volunteer_id / case_id / client_id 皆為 uuid 型別，cast 成 ::uuid 讓 Postgres
	// 明確用 uuid 的方式解析這幾個字串參數。
	const q = `
		INSERT INTO volunteer_tracks (volunteer_id, case_id, location, accuracy_m, recorded_at, client_id)
		VALUES ($1::uuid, $2::uuid, ST_SetSRID(ST_MakePoint($3, $4), 4326), $5, $6, $7::uuid)
		ON CONFLICT (client_id) DO NOTHING`
	_, err := r.db.Exec(ctx, q, in.VolunteerID, in.CaseID, in.Lng, in.Lat, in.AccuracyM, in.RecordedAt, in.ClientID)
	return err
}

type CoverageCell struct {
	Lng, Lat    float64 // 格網中心點
	PointCount  int
	LastVisited time.Time
}

// CoverageGrid 把某案件的志願者軌跡聚合成格網熱圖：每格回傳中心點座標、
// 軌跡點數量（可粗略反映搜索密度）、以及最後一次有人經過的時間（幾小時前搜過 vs. 幾天前）。
// cellSizeDeg 建議 0.001（約 100 公尺見方）；範圍太大時可以放大格子避免回傳過多格。
func (r *TrackRepo) CoverageGrid(ctx context.Context, caseID string, cellSizeDeg float64) ([]CoverageCell, error) {
	const q = `
		SELECT ST_X(cell), ST_Y(cell), count(*), max(recorded_at)
		FROM (
			SELECT ST_SnapToGrid(location, $2, $2) AS cell, recorded_at
			FROM volunteer_tracks
			WHERE case_id = $1::uuid
		) g
		GROUP BY cell`
	rows, err := r.db.Query(ctx, q, caseID, cellSizeDeg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CoverageCell
	for rows.Next() {
		var c CoverageCell
		if err := rows.Scan(&c.Lng, &c.Lat, &c.PointCount, &c.LastVisited); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
