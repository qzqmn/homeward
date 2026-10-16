package middleware

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"homeward/internal/service"
)

const ctxUserIDKey = "userID"

// AuthRequired 驗證 Bearer token，缺少或無效一律 401。
func AuthRequired(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := extractUserID(c, auth)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set(ctxUserIDKey, userID)
		c.Next()
	}
}

// AuthOptional 有帶合法 token 就設定 userID，沒帶或無效則放行（用於「免登入可提交，
// 登入後可關聯身份」的路由，例如提交線索）。
func AuthOptional(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if userID, ok := extractUserID(c, auth); ok {
			c.Set(ctxUserIDKey, userID)
		}
		c.Next()
	}
}

func extractUserID(c *gin.Context, auth *service.AuthService) (string, bool) {
	h := c.GetHeader("Authorization")
	token, found := strings.CutPrefix(h, "Bearer ")
	if !found || token == "" {
		return "", false
	}
	userID, err := auth.UserIDForToken(c.Request.Context(), token)
	if err != nil {
		// Token 不存在（redis.Nil）是正常的未登入情境，不用記錄。但 Redis 連線
		// 故障等其他錯誤會讓「已登入的使用者靜默變成 401」——那樣維運完全看不到
		// 原因，所以一定要 log 出來。
		if !errors.Is(err, redis.Nil) && !errors.Is(err, context.Canceled) {
			log.Printf("WARN: auth token lookup failed: %v", err)
		}
		return "", false
	}
	return userID, true
}

// UserID 從 context 取出已驗證的 user id；配合 AuthRequired 使用時必定存在。
func UserID(c *gin.Context) (string, bool) {
	v, ok := c.Get(ctxUserIDKey)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
