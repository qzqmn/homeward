// Package repository 是資料庫存取層：只做 SQL 讀寫，不含商業邏輯。
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CasePhotoRepo：一案多張照片（000012 migration 的 case_photos 表）。
type CasePhotoRepo struct{ db *pgxpool.Pool }

func NewCasePhotoRepo(db *pgxpool.Pool) *CasePhotoRepo { return &CasePhotoRepo{db: db} }

// CountForCase 統計案件現有照片數（上限檢查用）。
func (r *CasePhotoRepo) CountForCase(ctx context.Context, caseID string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM case_photos WHERE case_id = $1::uuid`, caseID).Scan(&n)
	return n, err
}

// Add 記錄一張補傳的現場照片。
func (r *CasePhotoRepo) Add(ctx context.Context, caseID, photoPath, uploadedBy string) error {
	var id string
	return r.db.QueryRow(ctx, `
		INSERT INTO case_photos (case_id, photo_path, uploaded_by)
		VALUES ($1::uuid, $2, $3::uuid)
		RETURNING id`, caseID, photoPath, uploadedBy).Scan(&id)
}

// List 回傳案件的照片相對路徑（封面照之外），舊在前。
func (r *CasePhotoRepo) List(ctx context.Context, caseID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT photo_path FROM case_photos WHERE case_id = $1::uuid ORDER BY created_at`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- 搜索任務（000013 migration 的 search_tasks 表）----

var ErrTaskNotClaimable = errors.New("search task not in claimable state")

type SearchTask struct {
	ID           string
	CaseID       string
	Title        string
	Note         *string
	Status       string // open | claimed | done
	ClaimedBy    *string
	ProgressNote *string
	// 斷點：志願者最後回報位置（null = 還沒回報過）
	LastLng   *float64
	LastLat   *float64
	DoneAt    *time.Time
	CreatedAt time.Time
}

type SearchTaskRepo struct{ db *pgxpool.Pool }

func NewSearchTaskRepo(db *pgxpool.Pool) *SearchTaskRepo { return &SearchTaskRepo{db: db} }

// Create 建一個搜索任務（案主或管理員）。
func (r *SearchTaskRepo) Create(ctx context.Context, caseID, title string, note *string, createdBy string) (SearchTask, error) {
	row := r.db.QueryRow(ctx, `
		INSERT INTO search_tasks (case_id, title, note, created_by)
		VALUES ($1::uuid, $2, $3, $4::uuid)
		RETURNING id, case_id::text, title, note, status, claimed_by::text, progress_note, ST_X(last_point), ST_Y(last_point), done_at, created_at`,
		caseID, title, note, createdBy)
	return scanSearchTask(row)
}

// ListByCase 案件的所有任務（新在前；前端自帶排序）。
func (r *SearchTaskRepo) ListByCase(ctx context.Context, caseID string) ([]SearchTask, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, case_id::text, title, note, status, claimed_by::text, progress_note, ST_X(last_point), ST_Y(last_point), done_at, created_at
		FROM search_tasks WHERE case_id = $1::uuid ORDER BY created_at DESC`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchTask
	for rows.Next() {
		t, err := scanSearchTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Claim 認領 open 任務；已被認領的回 ErrTaskNotClaimable（冪等：自己重複認領也算成功）。
func (r *SearchTaskRepo) Claim(ctx context.Context, taskID, userID string) (SearchTask, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE search_tasks
		SET status = 'claimed', claimed_by = $2::uuid, claimed_at = now(), done_at = NULL
		WHERE id = $1::uuid AND (status = 'open' OR claimed_by = $2::uuid)
		RETURNING id, case_id::text, title, note, status, claimed_by::text, progress_note, ST_X(last_point), ST_Y(last_point), done_at, created_at`,
		taskID, userID)
	t, err := scanSearchTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return SearchTask{}, ErrTaskNotClaimable
	}
	return t, err
}

// Release 退回任務（認領者本人）：任務回到 open，進度保留供下一個人參考。
func (r *SearchTaskRepo) Release(ctx context.Context, taskID, userID string) (SearchTask, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE search_tasks
		SET status = 'open', claimed_by = NULL, claimed_at = NULL
		WHERE id = $1::uuid AND status = 'claimed' AND claimed_by = $2::uuid
		RETURNING id, case_id::text, title, note, status, claimed_by::text, progress_note, ST_X(last_point), ST_Y(last_point), done_at, created_at`,
		taskID, userID)
	t, err := scanSearchTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return SearchTask{}, ErrTaskNotClaimable
	}
	return t, err
}

// UpdateProgress 更新斷點：進度文字 + 最後位置（僅認領者或建立者/管理員）。
func (r *SearchTaskRepo) UpdateProgress(ctx context.Context, taskID, userID string, note string, lng, lat float64) (SearchTask, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE search_tasks
		SET progress_note = $3, last_point = ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography
		WHERE id = $1::uuid AND status = 'claimed' AND (claimed_by = $2::uuid OR created_by = $2::uuid)
		RETURNING id, case_id::text, title, note, status, claimed_by::text, progress_note, ST_X(last_point), ST_Y(last_point), done_at, created_at`,
		taskID, userID, note, lng, lat)
	t, err := scanSearchTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return SearchTask{}, ErrTaskNotClaimable
	}
	return t, err
}

// Done 標記完成（認領者本人或管理員——建立者不放行，避免像主誤按）。
func (r *SearchTaskRepo) Done(ctx context.Context, taskID, userID string, isAdmin bool) (SearchTask, error) {
	where := "claimed_by = $2::uuid"
	args := []any{taskID, userID}
	if isAdmin {
		where = "(claimed_by = $2::uuid OR created_by = $2::uuid)"
	}
	row := r.db.QueryRow(ctx, `
		UPDATE search_tasks
		SET status = 'done', done_at = now()
		WHERE id = $1::uuid AND status = 'claimed' AND `+where+`
		RETURNING id, case_id::text, title, note, status, claimed_by::text, progress_note, ST_X(last_point), ST_Y(last_point), done_at, created_at`,
		args...)
	t, err := scanSearchTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return SearchTask{}, ErrTaskNotClaimable
	}
	return t, err
}

func scanSearchTask(row pgx.Row) (SearchTask, error) {
	var t SearchTask
	err := row.Scan(&t.ID, &t.CaseID, &t.Title, &t.Note, &t.Status, &t.ClaimedBy, &t.ProgressNote, &t.LastLng, &t.LastLat, &t.DoneAt, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SearchTask{}, ErrNotFound
	}
	return t, err
}
