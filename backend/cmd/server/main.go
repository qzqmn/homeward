// 歸途 Homeward — API 伺服器入口。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"homeward/internal/config"
	"homeward/internal/database"
	"homeward/internal/repository"
	"homeward/internal/router"
	"homeward/internal/service"
)

func main() {
	cfg := config.Load()

	// 收到 SIGTERM/SIGINT（docker stop）時優雅關閉
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pg, err := database.NewPostgres(ctx, cfg.PostgresDSN())
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pg.Close()

	if v, err := database.PostGISVersion(ctx, pg); err != nil {
		log.Printf("WARN: PostGIS not available (init.sql executed?): %v", err)
	} else {
		log.Printf("PostGIS: %s", v)
	}

	rdb, err := database.NewRedis(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	built := router.Build(router.Deps{Cfg: cfg, Pg: pg, Redis: rdb})
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           built.Engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second, // 手機網路上傳大圖需要較長時間
		WriteTimeout:      60 * time.Second,
	}

	// 背景排程：半徑推播擴大 + 實際送出通知。與 HTTP server 共用同一個 ctx，
	// 收到 SIGTERM/SIGINT 時一起結束，不需要另外處理關閉邏輯。
	go service.RunNotifyLoop(ctx, time.Duration(cfg.NotifyIntervalSec)*time.Second,
		repository.NewCaseRepo(pg), built.Notify)

	go func() {
		log.Printf("homeward api listening on %s (media dir: %s)", srv.Addr, cfg.MediaDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
