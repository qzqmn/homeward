package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SosEvent 對應 sos_events（志願者搜索中/路上需要更多人手幫忙的求救事件）。
type SosEvent struct {
	ID         int64
	UserID     string
	CaseID     *string
	Lng, Lat   float64
	Note       string
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

type SosRepo struct{ db *pgxpool.Pool }

func NewSosRepo(db *pgxpool.Pool) *SosRepo { return &SosRepo{db: db} }

// Create 建立 SOS 事件（case_id 可為 nil：不固定在哪個案件旁邊）。
func (r *SosRepo) Create(ctx context.Context, userID string, caseID *string, lng, lat float64, note string) (SosEvent, error) {
	const q = `
		INSERT INTO sos_events (user_id, case_id, location, note)
		VALUES ($1::uuid, CASE WHEN $2 = '' THEN NULL ELSE $2::uuid END,
		        ST_SetSRID(ST_MakePoint($3, $4), 4326), $5)
		RETURNING id, user_id::text, case_id::text,
		          ST_X(location), ST_Y(location), note, created_at, resolved_at`
	var e SosEvent
	err := r.db.QueryRow(ctx, q, userID, derefStr(caseID), lng, lat, note).
		Scan(&e.ID, &e.UserID, &e.CaseID, &e.Lng, &e.Lat, &e.Note, &e.CreatedAt, &e.ResolvedAt)
	return e, err
}

// ListOpen 回傳尚未結案的 SOS（新的在前），管理員檢視用。
func (r *SosRepo) ListOpen(ctx context.Context, limit int) ([]SosEvent, error) {
	const q = `
		SELECT id, user_id::text, case_id::text,
		       ST_X(location), ST_Y(location), note, created_at, resolved_at
		FROM sos_events
		WHERE resolved_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1`
	rows, err := r.db.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SosEvent
	for rows.Next() {
		var e SosEvent
		if err := rows.Scan(&e.ID, &e.UserID, &e.CaseID, &e.Lng, &e.Lat, &e.Note, &e.CreatedAt, &e.ResolvedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Resolve 標記 SOS 已處理（管理員從清單按「已處理」）。
func (r *SosRepo) Resolve(ctx context.Context, id int64) error {
	const q = `UPDATE sos_events SET resolved_at = now() WHERE id = $1 AND resolved_at IS NULL`
	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
