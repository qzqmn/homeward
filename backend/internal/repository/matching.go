// Package repository：case_matches 相關存取（走失案 ↔ 撿到/發現案 的配對建議）。
package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type MatchRepo struct{ db *pgxpool.Pool }

func NewMatchRepo(db *pgxpool.Pool) *MatchRepo { return &MatchRepo{db: db} }

// CandidateCase 是配對演算法比對用的最小案件資訊。
type CandidateCase struct {
	ID         string
	Lng, Lat   float64
	LastSeenAt *time.Time // 走失/發現時間，可能未填；評分時退化為只看距離
	DistanceM  float64
}

// oppositeType 回傳配對對象的案件類型："missing_pet" 對應 "found_pet"，反之亦然。
func oppositeType(caseType string) string {
	switch caseType {
	case "missing_pet":
		return "found_pet"
	case "found_pet":
		return "missing_pet"
	case "missing_person":
		return "found_person"
	case "found_person":
		return "missing_person"
	default:
		return ""
	}
}

// FindCandidates 找出與給定案件「類型相對」（missing_pet↔found_pet 等）、狀態仍為 open、
// 且座標在 radiusM 內的案件，依距離由近到遠排序，供配對評分使用。
func (r *MatchRepo) FindCandidates(ctx context.Context, caseID, caseType string, lng, lat float64, radiusM int, limit int) ([]CandidateCase, error) {
	opp := oppositeType(caseType)
	if opp == "" {
		return nil, nil // 未知類型：不比對（理論上不會發生，case_type 已由 handler 驗證過）
	}

	const q = `
		SELECT id::text,
		       ST_X(center), ST_Y(center), last_seen_at,
		       ST_Distance(center::geography, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography)
		FROM cases
		WHERE id <> $1::uuid
		  AND case_type = $2
		  AND status = 'open'
		  AND ST_DWithin(center::geography,
		                 ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography,
		                 $5)
		ORDER BY ST_Distance(center::geography, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography)
		LIMIT $6`
	rows, err := r.db.Query(ctx, q, caseID, opp, lng, lat, radiusM, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CandidateCase
	for rows.Next() {
		var c CandidateCase
		if err := rows.Scan(&c.ID, &c.Lng, &c.Lat, &c.LastSeenAt, &c.DistanceM); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type NewMatch struct {
	CaseID        string
	MatchedCaseID string
	Score         float32
}

// Upsert 寫入或更新一筆配對建議；同一組 (case_id, matched_case_id) 重算分數時直接覆蓋。
func (r *MatchRepo) Upsert(ctx context.Context, m NewMatch) error {
	const q = `
		INSERT INTO case_matches (case_id, matched_case_id, score)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (case_id, matched_case_id) DO UPDATE SET score = EXCLUDED.score`
	_, err := r.db.Exec(ctx, q, m.CaseID, m.MatchedCaseID, m.Score)
	return err
}

type Match struct {
	MatchedCaseID string
	Score         float32
	Status        string
	Title         string
	CaseType      string
	Lng, Lat      float64 // 對方案件的公開（模糊）座標——配對建議頁一律不外洩精確位置
}

// ListForCase 列出某案件目前的配對建議，依分數由高到低排序（含對方案件標題、類型與模糊座標）。
//
// 配對只會在「新建立的那個案件」那一側寫一筆（RunFor 只對新案件呼叫），
// 所以同一組配對對另一個案件來說，caseID 可能出現在 case_id 或 matched_case_id
// 任一欄——這裡用 OR 兩邊都查，並用 CASE 取出「對方」的 id，確保從任一案件
// 查都看得到同一筆配對，不會因為查的方向不對而漏掉。
func (r *MatchRepo) ListForCase(ctx context.Context, caseID string) ([]Match, error) {
	const q = `
		SELECT
		       (CASE WHEN m.case_id = $1::uuid THEN m.matched_case_id ELSE m.case_id END)::text AS other_id,
		       m.score, m.status,
		       c.title, c.case_type, ST_X(c.public_center), ST_Y(c.public_center)
		FROM case_matches m
		JOIN cases c ON c.id = (CASE WHEN m.case_id = $1::uuid THEN m.matched_case_id ELSE m.case_id END)
		WHERE m.case_id = $1::uuid OR m.matched_case_id = $1::uuid
		ORDER BY m.score DESC`
	rows, err := r.db.Query(ctx, q, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Match
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.MatchedCaseID, &m.Score, &m.Status, &m.Title, &m.CaseType, &m.Lng, &m.Lat); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateStatus 讓案主確認或排除一筆配對建議（confirmed / rejected）。
// 同樣要兩個方向都比對，理由見 ListForCase 的註解。
func (r *MatchRepo) UpdateStatus(ctx context.Context, caseID, matchedCaseID, status string) error {
	const q = `
		UPDATE case_matches SET status = $3
		WHERE (case_id = $1::uuid AND matched_case_id = $2::uuid)
		   OR (case_id = $2::uuid AND matched_case_id = $1::uuid)`
	tag, err := r.db.Exec(ctx, q, caseID, matchedCaseID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
