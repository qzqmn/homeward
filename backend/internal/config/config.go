// Package config 從環境變數讀取所有設定（憑證不寫死在程式碼裡）。
package config

import (
	"net"
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	Port           string
	MediaDir       string // 容器內的 NAS 掛載點
	MaxUploadBytes int64
	BaseURL        string // 對外網域（含 https://），組分享連結與 OG 絕對網址用

	PGHost, PGPort, PGUser, PGPassword, PGDatabase string

	RedisAddr, RedisPassword string
	RedisDB                  int

	NotifyIntervalSec int // 背景通知排程的執行間隔（秒）

	// TelegramBotToken 只用來驗證 Telegram Login Widget 回傳資料的簽章
	// （HMAC），不會拿去呼叫 Telegram API 發訊息——發通知是另外一條路徑，
	// 見 NotifyWebhookURL。
	TelegramBotToken string

	// NotifyWebhookURL：設定了的話，通知改成 POST 到這個網址（例如你自己的
	// Cloudflare Worker），由那邊決定怎麼轉發到 Telegram／其他管道；bot token
	// 留在 Worker 那端，後端完全不需要知道。沒設定時只印 log（本機測試用）。
	// 可以隨時只改這個環境變數、不需要重新編譯或改程式碼。
	NotifyWebhookURL string
}

func Load() Config {
	return Config{
		Port:           getenv("PORT", "8080"),
		MediaDir:       getenv("MEDIA_DIR", "/data/media"),
		MaxUploadBytes: int64(getenvInt("MAX_UPLOAD_MB", 10)) << 20,
		BaseURL:        getenv("BASE_URL", "http://localhost:8080"),

		PGHost:     getenv("POSTGRES_HOST", "postgis-db"),
		PGPort:     getenv("POSTGRES_PORT", "5432"),
		PGUser:     getenv("POSTGRES_USER", "homeward"),
		PGPassword: os.Getenv("POSTGRES_PASSWORD"),
		PGDatabase: getenv("POSTGRES_DB", "homeward"),

		RedisAddr:     getenv("REDIS_ADDR", "redis-cache:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       getenvInt("REDIS_DB", 0),

		NotifyIntervalSec: getenvInt("NOTIFY_INTERVAL_SEC", 300),

		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		NotifyWebhookURL: os.Getenv("NOTIFY_WEBHOOK_URL"),
	}
}

// PostgresDSN 組出連線字串；用 url.UserPassword 確保密碼含特殊字元也能正確轉義。
func (c Config) PostgresDSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.PGUser, c.PGPassword),
		Host:   net.JoinHostPort(c.PGHost, c.PGPort),
		Path:   c.PGDatabase,
	}
	q := u.Query()
	q.Set("sslmode", "disable") // 同一 Docker 網路內通訊；若 DB 移到 Tailscale 另一台，Tailscale 本身已加密
	u.RawQuery = q.Encode()
	return u.String()
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
