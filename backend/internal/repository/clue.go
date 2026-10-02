package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Clue struct {
	ID         string
	CaseID     string
	PhotoPath  string
	Lng, Lat   float64
	Note       string
	Status     string
	ReporterID *string
	SightedAt  time.Time
	ClientID   string
	CreatedAt  time.Time
}

type NewClue struct {
	CaseID     string
	PhotoPath  string
	Lng, Lat   float64
	Note       string
	ReporterID *string   // 匿名提交時為 nil
	SightedAt  time.Time // 客戶端記錄的目擊時間（離線補傳時可能早於送達時間）
	ClientID   string    // 客戶端產生的 UUID，用於離線重送去重
}

type ClueRepo struct{ db *pgxpool.Pool }

func NewClueRepo(db *pgxpool.Pool) *ClueRepo { return &ClueRepo{db: db} }

// Create 以 client_id 做冪等：同一 client_id 重送時直接回傳原有紀錄，不會產生重複線索。
// 這是為了支援 iOS Safari 無 Background Sync、必須由前端自行重試上傳的情境。
func (r *ClueRepo) Create(ctx context.Context, in NewClue) (Clue, error) {
	// id / case_id / reporter_id / client_id 皆為 uuid 型別，一律 cast 成 ::text 再 Scan
	// 進 Go string（reporter_id 可能是 NULL，cast 後仍為 NULL，可安全 Scan 進 *string）。
	const insert = `
		INSERT INTO clues (case_id, photo_path, location, note, reporter_id, sighted_at, client_id)
		VALUES ($1::uuid, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326), $5, $6::uuid, $7, $8::uuid)
		ON CONFLICT (client_id) DO NOTHING
		RETURNING id::text, case_id::text, photo_path, ST_X(location), ST_Y(location), note, status,
		          reporter_id::text, sighted_at, client_id::text, created_at`

	var c Clue
	err := r.db.QueryRow(ctx, insert,
		in.CaseID, in.PhotoPath, in.Lng, in.Lat, in.Note, in.ReporterID, in.SightedAt, in.ClientID,
	).Scan(&c.ID, &c.CaseID, &c.PhotoPath, &c.Lng, &c.Lat, &c.Note, &c.Status,
		&c.ReporterID, &c.SightedAt, &c.ClientID, &c.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		// ON CONFLICT DO NOTHING 沒有回傳列 → client_id 已存在，查出既有紀錄回傳（重送視為成功）
		return r.getByClientID(ctx, in.ClientID)
	}
	return c, err
}

func (r *ClueRepo) getByClientID(ctx context.Context, clientID string) (Clue, error) {
	const q = `
		SELECT id::text, case_id::text, photo_path, ST_X(location), ST_Y(location), note, status,
		       reporter_id::text, sighted_at, client_id::text, created_at
		FROM clues WHERE client_id = $1::uuid`
	var c Clue
	err := r.db.QueryRow(ctx, q, clientID).
		Scan(&c.ID, &c.CaseID, &c.PhotoPath, &c.Lng, &c.Lat, &c.Note, &c.Status,
			&c.ReporterID, &c.SightedAt, &c.ClientID, &c.CreatedAt)
	return c, err
}

func (r *ClueRepo) ListByCase(ctx context.Context, caseID string, limit int) ([]Clue, error) {
	const q = `
		SELECT id::text, case_id::text, photo_path, ST_X(location), ST_Y(location), note, status,
		       reporter_id::text, sighted_at, client_id::text, created_at
		FROM clues WHERE case_id = $1::uuid
		ORDER BY sighted_at DESC LIMIT $2`
	rows, err := r.db.Query(ctx, q, caseID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Clue
	for rows.Next() {
		var c Clue
		if err := rows.Scan(&c.ID, &c.CaseID, &c.PhotoPath, &c.Lng, &c.Lat, &c.Note, &c.Status,
			&c.ReporterID, &c.SightedAt, &c.ClientID, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
