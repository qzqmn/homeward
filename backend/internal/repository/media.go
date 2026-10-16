// Package repository：資料庫存取層（媒體檔案引用查詢）。
package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MediaRepo 處理「媒體檔案是否仍被引用」的查詢，供孤兒檔案清理使用。
type MediaRepo struct{ db *pgxpool.Pool }

func NewMediaRepo(db *pgxpool.Pool) *MediaRepo { return &MediaRepo{db: db} }

// PathReferenced 回傳給定的相對路徑是否已被 cases.photo_path（封面照）
// 或 clues.photo_path（線索照片）引用。兩者皆為同一層相對路徑格式
// （YYYY/MM/xxx.jpg），用等值查詢即可。
func (r *MediaRepo) PathReferenced(ctx context.Context, relPath string) (bool, error) {
	const q = `
		SELECT EXISTS (
		        SELECT 1 FROM cases WHERE photo_path = $1
		       ) OR EXISTS (
		        SELECT 1 FROM clues WHERE photo_path = $1
		       )`
	var exists bool
	err := r.db.QueryRow(ctx, q, relPath).Scan(&exists)
	return exists, err
}
