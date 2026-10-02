package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Case struct {
	ID             string
	CaseType       string
	Status         string
	Title          string
	Description    string
	LastSeenAt     *time.Time
	Lng, Lat       float64 // center（精確座標）：只在呼叫端已確認有權限時才填入／回傳
	PublicLng      float64
	PublicLat      float64 // public_center（模糊座標，格網吸附）：對外公開頁一律使用這組
	SearchRadiusM  int
	PoliceReportNo *string
	PhotoPath      *string // 封面照（分享 OG 圖／海報用），相對 NAS 媒體根目錄
	OwnerID        *string
	CreatedAt      time.Time
}

type CaseRepo struct{ db *pgxpool.Pool }

func NewCaseRepo(db *pgxpool.Pool) *CaseRepo { return &CaseRepo{db: db} }

type NewCase struct {
	OwnerID        string
	CaseType       string // missing_person / missing_pet / found_person / found_pet
	Title          string
	Description    string
	LastSeenAt     *time.Time
	Lng, Lat       float64
	SearchRadiusM  int
	PoliceReportNo *string
	PhotoPath      *string
}

func (r *CaseRepo) Create(ctx context.Context, in NewCase) (Case, error) {
	// id / owner_id 是 uuid 型別，一律 cast 成 ::text 再 Scan 進 Go string，
	// 避免依賴 pgx 對 uuid 型別的預設解碼行為。
	const q = `
		INSERT INTO cases (owner_id, case_type, title, description, last_seen_at,
		                    center, search_radius_m, police_report_no, photo_path)
		VALUES ($1::uuid, $2, $3, $4, $5, ST_SetSRID(ST_MakePoint($6, $7), 4326), $8, $9, $10)
		RETURNING id::text, case_type, status, title, description, last_seen_at,
		          ST_X(center), ST_Y(center), ST_X(public_center), ST_Y(public_center),
		          search_radius_m, police_report_no, photo_path, owner_id::text, created_at`
	var c Case
	err := r.db.QueryRow(ctx, q,
		in.OwnerID, in.CaseType, in.Title, in.Description, in.LastSeenAt,
		in.Lng, in.Lat, in.SearchRadiusM, in.PoliceReportNo, in.PhotoPath,
	).Scan(&c.ID, &c.CaseType, &c.Status, &c.Title, &c.Description, &c.LastSeenAt,
		&c.Lng, &c.Lat, &c.PublicLng, &c.PublicLat,
		&c.SearchRadiusM, &c.PoliceReportNo, &c.PhotoPath, &c.OwnerID, &c.CreatedAt)
	return c, err
}

func (r *CaseRepo) GetByID(ctx context.Context, id string) (Case, error) {
	const q = `
		SELECT id::text, case_type, status, title, description, last_seen_at,
		       ST_X(center), ST_Y(center), ST_X(public_center), ST_Y(public_center),
		       search_radius_m, police_report_no, photo_path, owner_id::text, created_at
		FROM cases WHERE id = $1::uuid`
	var c Case
	err := r.db.QueryRow(ctx, q, id).
		Scan(&c.ID, &c.CaseType, &c.Status, &c.Title, &c.Description, &c.LastSeenAt,
			&c.Lng, &c.Lat, &c.PublicLng, &c.PublicLat,
			&c.SearchRadiusM, &c.PoliceReportNo, &c.PhotoPath, &c.OwnerID, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Case{}, ErrNotFound
	}
	return c, err
}

// ListNearby 依 geography 距離查詢（公尺），只回傳公開（模糊）座標所需的欄位。
// 注意：WHERE 子句用 public_center::geography 才會命中 000002 migration 建立的函式索引。
func (r *CaseRepo) ListNearby(ctx context.Context, lng, lat float64, radiusM int, limit int) ([]Case, error) {
	const q = `
		SELECT id::text, case_type, status, title, description, last_seen_at,
		       ST_X(public_center), ST_Y(public_center),
		       ST_X(public_center), ST_Y(public_center),
		       search_radius_m, police_report_no, photo_path, owner_id::text, created_at
		FROM cases
		WHERE status = 'open'
		  AND ST_DWithin(
		        public_center::geography,
		        ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
		        $3)
		ORDER BY created_at DESC
		LIMIT $4`
	rows, err := r.db.Query(ctx, q, lng, lat, radiusM, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Case
	for rows.Next() {
		var c Case
		if err := rows.Scan(&c.ID, &c.CaseType, &c.Status, &c.Title, &c.Description, &c.LastSeenAt,
			&c.Lng, &c.Lat, &c.PublicLng, &c.PublicLat,
			&c.SearchRadiusM, &c.PoliceReportNo, &c.PhotoPath, &c.OwnerID, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateStatus 處理結案／標記尋回（PATCH /cases/:id 的底層邏輯）。
func (r *CaseRepo) UpdateStatus(ctx context.Context, id, status string) error {
	const q = `UPDATE cases SET status = $2, updated_at = now() WHERE id = $1::uuid`
	tag, err := r.db.Exec(ctx, q, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateStatus 處理結案／標記尋回（PATCH /cases/:id 的底層邏輯）。
func (r *CaseRepo) UpdateStatus(ctx context.Context, id, status string) error {
	const q = `UPDATE cases SET status = $2, updated_at = now() WHERE id = $1::uuid`
	tag, err := r.db.Exec(ctx, q, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type EscalationCandidate struct {
	ID        string
	Lng, Lat  float64
	CreatedAt time.Time
}

// ListOpenMissingForEscalation 列出仍為 open 的走失案件（missing_person / missing_pet），
// 供背景排程判斷是否該擴大半徑、通知更多關注該區域的使用者。
func (r *CaseRepo) ListOpenMissingForEscalation(ctx context.Context) ([]EscalationCandidate, error) {
	const q = `
		SELECT id::text, ST_X(center), ST_Y(center), created_at
		FROM cases
		WHERE status = 'open' AND case_type IN ('missing_person', 'missing_pet')`
	rows, err := r.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EscalationCandidate
	for rows.Next() {
		var e EscalationCandidate
		if err := rows.Scan(&e.ID, &e.Lng, &e.Lat, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
