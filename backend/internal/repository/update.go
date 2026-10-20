// Package repository：case_updates（案件時間軸）存取。
// 案主需要一個「過程紀錄」：什麼時候有人回報、哪則線索被確認、什麼時候結案。
package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CaseUpdate struct {
	ID        int64
	AuthorID  *string
	Kind      string // note / status_change / clue / sighting / match / system
	Body      string
	CreatedAt time.Time
}

type UpdateRepo struct{ db *pgxpool.Pool }

func NewUpdateRepo(db *pgxpool.Pool) *UpdateRepo { return &UpdateRepo{db: db} }

// Add 寫入一則時間軸事件。authorID 為 nil 表示系統/匿名（匿名線索事件）。
func (r *UpdateRepo) Add(ctx context.Context, caseID string, authorID *string, kind, body string) error {
	const q = `
		INSERT INTO case_updates (case_id, author_id, kind, body)
		VALUES ($1::uuid, $2::uuid, $3, $4)`
	_, err := r.db.Exec(ctx, q, caseID, authorID, kind, body)
	return err
}

func (r *UpdateRepo) ListForCase(ctx context.Context, caseID string, limit int) ([]CaseUpdate, error) {
	const q = `
		SELECT id, author_id::text, kind, body, created_at
		FROM case_updates
		WHERE case_id = $1::uuid
		ORDER BY created_at DESC, id DESC
		LIMIT $2`
	rows, err := r.db.Query(ctx, q, caseID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CaseUpdate
	for rows.Next() {
		var u CaseUpdate
		if err := rows.Scan(&u.ID, &u.AuthorID, &u.Kind, &u.Body, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
