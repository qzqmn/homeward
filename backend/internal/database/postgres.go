// Package database 負責 PostgreSQL(PostGIS) 與 Redis 的連線初始化。
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgres 建立連線池，並在啟動階段重試 Ping（容器啟動順序不保證 DB 已就緒）。
func NewPostgres(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	cfg.MaxConns = 10

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	var lastErr error
	for i := 0; i < 10; i++ {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		lastErr = pool.Ping(pctx)
		cancel()
		if lastErr == nil {
			return pool, nil
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	pool.Close()
	return nil, fmt.Errorf("postgres not reachable: %w", lastErr)
}

// PostGISVersion 用來確認 PostGIS 擴充已啟用（init.sql 已執行）。
func PostGISVersion(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var v string
	err := pool.QueryRow(ctx, "SELECT PostGIS_Version()").Scan(&v)
	return v, err
}
