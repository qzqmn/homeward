// Package middleware 放共用的 Gin 中介層。
package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// INCR 與 PEXPIRE 放在同一個 Lua 腳本內原子執行，避免程序中途結束造成 key 永不過期。
var rateScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return n
`)

// RateLimit 固定視窗限流：每個來源 IP 在 window 內最多 limit 次。
// name 用來區分不同路由的計數器。Redis 故障時採 fail-open（放行），避免限流器拖垮服務。
func RateLimit(rdb *redis.Client, name string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := "rl:" + name + ":" + c.ClientIP()
		n, err := rateScript.Run(c.Request.Context(), rdb, []string{key}, window.Milliseconds()).Int()
		if err != nil {
			c.Next()
			return
		}
		if n > limit {
			c.Header("Retry-After", strconv.Itoa(int(window.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}
		c.Next()
	}
}
