// Package repository：reports（舉報：詐騙、騷擾、私隱、垃圾內容）。
package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Report struct {
	ID         int64
	ReporterID *string
	TargetType string // case / clue / user
	TargetID   string
	Reason     string // scam / harassment / privacy / spam / other
	Detail     string
	Status     string // open / reviewing / actioned / dismissed
	CreatedAt  time.Time
}

type NewReport struct {
	ReporterID *string
	TargetType string
	TargetID   string
	Reason     string
	Detail     string
}

type ReportRepo struct{ db *pgxpool.Pool }

func NewReportRepo(db *pgxpool.Pool) *ReportRepo { return &ReportRepo{db: db} }

// TargetExists 確認被舉報的對象真的存在（防止往任意 uuid 灌垃圾記錄）。
// user 目標存於 users；case / clue 各查自己的表。回傳 false 表示不存在。
func (r *ReportRepo) TargetExists(ctx context.Context, targetType, targetID string) (bool, error) {
	var q string
	switch targetType {
	case "case":
		q = `SELECT EXISTS (SELECT 1 FROM cases WHERE id = $1::uuid)`
	case "clue":
		q = `SELECT EXISTS (SELECT 1 FROM clues WHERE id = $1::uuid)`
	case "user":
		q = `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1::uuid AND deleted_at IS NULL)`
	default:
		return false, ErrNotFound
	}
	var ok bool
	err := r.db.QueryRow(ctx, q, targetID).Scan(&ok)
	return ok, err
}

// Create 寫入一則舉報。target 存在性由呼叫端（handler）先查過。
func (r *ReportRepo) Create(ctx context.Context, in NewReport) (Report, error) {
	const q = `
		INSERT INTO reports (reporter_id, target_type, target_id, reason, detail)
		VALUES ($1::uuid, $2, $3::uuid, $4, $5)
		RETURNING id, reporter_id::text, target_type, target_id, reason, detail, status, created_at`
	var rep Report
	err := r.db.QueryRow(ctx, q, in.ReporterID, in.TargetType, in.TargetID, in.Reason, in.Detail).
		Scan(&rep.ID, &rep.ReporterID, &rep.TargetType, &rep.TargetID, &rep.Reason, &rep.Detail, &rep.Status, &rep.CreatedAt)
	return rep, err
}

// ListOpen 列出尚未處理完結的舉報（admin 用），新的排前面。
func (r *ReportRepo) ListOpen(ctx context.Context, limit int) ([]Report, error) {
	const q = `
		SELECT id, reporter_id::text, target_type, target_id, reason, detail, status, created_at
		FROM reports
		WHERE status IN ('open', 'reviewing')
		ORDER BY created_at DESC
		LIMIT $1`
	rows, err := r.db.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Report
	for rows.Next() {
		var rep Report
		if err := rows.Scan(&rep.ID, &rep.ReporterID, &rep.TargetType, &rep.TargetID, &rep.Reason, &rep.Detail, &rep.Status, &rep.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rep)
	}
	return out, rows.Err()
}

// SetStatus 管理員處理舉報：reviewing（開始處理）/ actioned（已處理，例如案件下架）/
// dismissed（查無不法）。
func (r *ReportRepo) SetStatus(ctx context.Context, id int64, status string) error {
	const q = `UPDATE reports SET status = $2 WHERE id = $1`
	tag, err := r.db.Exec(ctx, q, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
