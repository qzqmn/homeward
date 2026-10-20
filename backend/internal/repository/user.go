// Package repository 是資料庫存取層：只做 SQL 讀寫，不含商業邏輯。
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID                   string
	PhoneE164            *string // 選填：可以只用 Telegram 登入，不一定有手機號碼
	TelegramID           *int64  // 選填：可以只用手機 OTP 登入，不一定連過 Telegram
	DisplayName          string
	Locale               string
	Role                 string     // 'user' | 'admin'（志願者審核後台用 admin）
	VolunteerVerifiedAt  *time.Time // NULL 表示未認證志願者；非 NULL 才可查看案件精確位置
	VolunteerRequestedAt *time.Time // 使用者申請成為志願者的時間（管理員審核清單用）
}

type UserRepo struct{ db *pgxpool.Pool }

func NewUserRepo(db *pgxpool.Pool) *UserRepo { return &UserRepo{db: db} }

// UpsertByPhone 用手機號碼查找使用者，不存在則建立；OTP 驗證通過後呼叫。
//
// id 欄位在 Postgres 是 uuid 型別；這裡一律 cast 成 ::text 再 Scan 進 Go string，
// 避免依賴 pgx 對 uuid 型別的預設解碼行為（不同版本/設定下可能不是 string）。
func (r *UserRepo) UpsertByPhone(ctx context.Context, phoneE164 string) (User, error) {
	const q = `
		INSERT INTO users (phone_e164) VALUES ($1)
		ON CONFLICT (phone_e164) DO UPDATE SET phone_e164 = EXCLUDED.phone_e164
		RETURNING id::text, phone_e164, telegram_id, display_name, locale, role, volunteer_verified_at, volunteer_requested_at`
	var u User
	err := r.db.QueryRow(ctx, q, phoneE164).
		Scan(&u.ID, &u.PhoneE164, &u.TelegramID, &u.DisplayName, &u.Locale, &u.Role, &u.VolunteerVerifiedAt, &u.VolunteerRequestedAt)
	return u, err
}

// UpsertByTelegramID 用 Telegram 使用者 id 查找使用者，不存在則建立並帶入
// Telegram 顯示名稱；Telegram Login Widget 驗證通過後呼叫。
func (r *UserRepo) UpsertByTelegramID(ctx context.Context, telegramID int64, displayName string) (User, error) {
	const q = `
		INSERT INTO users (telegram_id, display_name) VALUES ($1, $2)
		ON CONFLICT (telegram_id) DO UPDATE SET telegram_id = EXCLUDED.telegram_id
		RETURNING id::text, phone_e164, telegram_id, display_name, locale, role, volunteer_verified_at, volunteer_requested_at`
	var u User
	err := r.db.QueryRow(ctx, q, telegramID, displayName).
		Scan(&u.ID, &u.PhoneE164, &u.TelegramID, &u.DisplayName, &u.Locale, &u.Role, &u.VolunteerVerifiedAt, &u.VolunteerRequestedAt)
	return u, err
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (User, error) {
	const q = `SELECT id::text, phone_e164, telegram_id, display_name, locale, role, volunteer_verified_at, volunteer_requested_at
	           FROM users WHERE id = $1::uuid AND deleted_at IS NULL`
	var u User
	err := r.db.QueryRow(ctx, q, id).
		Scan(&u.ID, &u.PhoneE164, &u.TelegramID, &u.DisplayName, &u.Locale, &u.Role, &u.VolunteerVerifiedAt, &u.VolunteerRequestedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// IsVerifiedVolunteer 用於判斷是否可查看案件的精確座標。
func (u User) IsVerifiedVolunteer() bool { return u.VolunteerVerifiedAt != nil }

// RequestVolunteer 記錄使用者想成為認證志願者的時間。冪等：重複申請不報錯，
// 也不會覆蓋已有時間（保留最早的申請時間方便管理員排序審核）。
func (r *UserRepo) RequestVolunteer(ctx context.Context, userID string) error {
	const q = `
		UPDATE users
		SET volunteer_requested_at = coalesce(volunteer_requested_at, now())
		WHERE id = $1::uuid AND deleted_at IS NULL`
	tag, err := r.db.Exec(ctx, q, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// VolunteerCandidate 是審核清單的一列：提出申請的使用者與其當前狀態。
type VolunteerCandidate struct {
	ID                   string
	DisplayName          string
	TelegramID           *int64
	PhoneE164            *string
	VolunteerVerifiedAt  *time.Time
	VolunteerRequestedAt time.Time
}

// ListVolunteerRequests 列出曾申請成為志願者的使用者（未刪除帳號），
// 申請時間新的排前面；管理員據 telegram_id / display_name 判斷是否放行。
func (r *UserRepo) ListVolunteerRequests(ctx context.Context, limit int) ([]VolunteerCandidate, error) {
	const q = `
		SELECT id::text, display_name, telegram_id, phone_e164, volunteer_verified_at, volunteer_requested_at
		FROM users
		WHERE volunteer_requested_at IS NOT NULL AND deleted_at IS NULL
		ORDER BY volunteer_requested_at DESC
		LIMIT $1`
	rows, err := r.db.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []VolunteerCandidate
	for rows.Next() {
		var c VolunteerCandidate
		if err := rows.Scan(&c.ID, &c.DisplayName, &c.TelegramID, &c.PhoneE164, &c.VolunteerVerifiedAt, &c.VolunteerRequestedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetVolunteerVerified 授予／撤銷志願者認證（volunteer_verified_at 非空即可看
// 精確位置與線索列表——這是整個平台的權限核心，務必只從 admin API 呼叫）。
func (r *UserRepo) SetVolunteerVerified(ctx context.Context, userID string, verified bool) error {
	var q string
	if verified {
		q = `UPDATE users SET volunteer_verified_at = now(), volunteer_requested_at = coalesce(volunteer_requested_at, now())
		     WHERE id = $1::uuid AND deleted_at IS NULL`
	} else {
		// 撤銷：同時清掉申請時間，審核清單不再列出（除非重新申請）
		q = `UPDATE users SET volunteer_verified_at = NULL, volunteer_requested_at = NULL
		     WHERE id = $1::uuid AND deleted_at IS NULL`
	}
	tag, err := r.db.Exec(ctx, q, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetWatchArea 設定使用者的「關注區域」：用於半徑推播比對（誰該收到「附近有案件」通知），
// 與 volunteer_tracks（搜索當下的即時軌跡）是不同用途，不會互相影響。
func (r *UserRepo) SetWatchArea(ctx context.Context, userID string, lng, lat float64, radiusM int) error {
	const q = `
		UPDATE users
		SET watch_center = ST_SetSRID(ST_MakePoint($2, $3), 4326), watch_radius_m = $4
		WHERE id = $1::uuid`
	tag, err := r.db.Exec(ctx, q, userID, lng, lat, radiusM)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// WatcherInCase：用來排除已經收過同一案件通知的使用者。
type Watcher struct {
	UserID string
}

// FindWatchersInRadius 找出「關注區域」與案件座標距離在 radiusM 內、且尚未收過
// 該案件 case_nearby 通知的使用者，供半徑推播使用。
func (r *UserRepo) FindWatchersInRadius(ctx context.Context, caseID string, lng, lat float64, radiusM int) ([]Watcher, error) {
	const q = `
		SELECT u.id::text
		FROM users u
		WHERE u.deleted_at IS NULL
		  AND u.watch_center IS NOT NULL
		  AND ST_DWithin(u.watch_center::geography,
		                 ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography,
		                 $4)
		  AND NOT EXISTS (
		        SELECT 1 FROM notifications n
		        WHERE n.user_id = u.id AND n.case_id = $1::uuid AND n.kind = 'case_nearby'
		  )`
	rows, err := r.db.Query(ctx, q, caseID, lng, lat, radiusM)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Watcher
	for rows.Next() {
		var w Watcher
		if err := rows.Scan(&w.UserID); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
