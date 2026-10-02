// Package service 放不直接對應資料表、跨 repository 的商業邏輯（OTP、Session）。
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	otpTTL         = 5 * time.Minute
	otpResendCD    = 60 * time.Second // 同一手機號重新發送的冷卻時間
	otpMaxAttempts = 5                // 同一組 OTP 允許嘗試驗證的次數
	sessionTTL     = 30 * 24 * time.Hour
)

var (
	ErrCooldown     = errors.New("otp: 請稍候再重新發送")
	ErrOTPInvalid   = errors.New("otp: 驗證碼錯誤或已過期")
	ErrTooManyTries = errors.New("otp: 嘗試次數過多，請重新發送驗證碼")
)

// AuthService 封裝 OTP 發送/驗證與登入 session（皆存於 Redis，不落地資料庫）。
//
// 注意：這裡尚未接上真正的簡訊網關，RequestOTP 只把驗證碼寫進 Redis 並回傳於
// log（開發用）。上線前必須替換 sendSMS 成實際的簡訊 / WhatsApp OTP 服務。
type AuthService struct {
	rdb *redis.Client
}

func NewAuthService(rdb *redis.Client) *AuthService {
	return &AuthService{rdb: rdb}
}

func otpKey(phone string) string         { return "otp:code:" + phone }
func otpAttemptsKey(phone string) string { return "otp:attempts:" + phone }
func otpCooldownKey(phone string) string { return "otp:cooldown:" + phone }
func sessionKey(token string) string     { return "session:" + token }

// RequestOTP 產生 6 位數驗證碼並寄出。回傳的 devCode 只在 MEDIA_DIR 以外的「非正式環境」
// 用於除錯；正式環境串接簡訊網關後應移除回傳值，只透過簡訊/WhatsApp 送達使用者。
func (s *AuthService) RequestOTP(ctx context.Context, phoneE164 string) (devCode string, err error) {
	if ok, err := s.rdb.SetNX(ctx, otpCooldownKey(phoneE164), "1", otpResendCD).Result(); err != nil {
		return "", fmt.Errorf("check cooldown: %w", err)
	} else if !ok {
		return "", ErrCooldown
	}

	code, err := randomDigits(6)
	if err != nil {
		return "", fmt.Errorf("generate otp: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, otpKey(phoneE164), code, otpTTL)
	pipe.Del(ctx, otpAttemptsKey(phoneE164))
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("store otp: %w", err)
	}

	if err := sendSMS(phoneE164, code); err != nil {
		// 發送失敗不視為致命錯誤：仍允許使用者用 devCode（開發環境）重試，
		// 但上線後應該讓呼叫端得知簡訊沒送達。
		return code, fmt.Errorf("send sms: %w", err)
	}
	return code, nil
}

// VerifyOTP 核對驗證碼；成功時回傳登入 session token。
func (s *AuthService) VerifyOTP(ctx context.Context, phoneE164, code string) (string, error) {
	attempts, err := s.rdb.Incr(ctx, otpAttemptsKey(phoneE164)).Result()
	if err != nil {
		return "", fmt.Errorf("check attempts: %w", err)
	}
	s.rdb.Expire(ctx, otpAttemptsKey(phoneE164), otpTTL)
	if attempts > otpMaxAttempts {
		return "", ErrTooManyTries
	}

	stored, err := s.rdb.Get(ctx, otpKey(phoneE164)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrOTPInvalid
	}
	if err != nil {
		return "", fmt.Errorf("read otp: %w", err)
	}
	if stored != code {
		return "", ErrOTPInvalid
	}

	// 驗證成功即刻失效，避免同一組碼被重複使用
	s.rdb.Del(ctx, otpKey(phoneE164), otpAttemptsKey(phoneE164))

	token, err := randomHex(32)
	if err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return token, nil
}

// CreateSession 把 token 與 userID 的對應寫進 Redis（呼叫端先確保 user 已存在於資料庫）。
func (s *AuthService) CreateSession(ctx context.Context, token, userID string) error {
	return s.rdb.Set(ctx, sessionKey(token), userID, sessionTTL).Err()
}

// UserIDForToken 驗證 Bearer token 並回傳對應的 user id；不存在或過期回傳 redis.Nil。
func (s *AuthService) UserIDForToken(ctx context.Context, token string) (string, error) {
	return s.rdb.Get(ctx, sessionKey(token)).Result()
}

// sendSMS 是簡訊發送的替身實作：目前只印 log，尚未整合真正的簡訊網關。
// TODO: 串接香港適用的簡訊或 WhatsApp Business API 服務。
func sendSMS(phone, code string) error {
	fmt.Printf("[DEV] send OTP %s to %s (尚未串接簡訊網關，僅供開發除錯)\n", code, phone)
	return nil
}

func randomDigits(n int) (string, error) {
	digits := make([]byte, n)
	for i := range digits {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		digits[i] = byte('0' + d.Int64())
	}
	return string(digits), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
