package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxAuthAge：Telegram Login Widget 回傳的資料裡有 auth_date（簽署當下的時間戳），
// 超過這個時間就拒絕，防止有人截獲舊的登入資料重送（replay attack）。
const maxAuthAge = 24 * time.Hour

var (
	ErrTelegramHashMismatch = errors.New("telegram: 簽章驗證失敗")
	ErrTelegramDataTooOld   = errors.New("telegram: 登入資料已過期，請重新登入")
	ErrTelegramMissingID    = errors.New("telegram: 缺少使用者 id")
)

// VerifyTelegramLogin 驗證 Telegram Login Widget 回傳的資料是否真的由 Telegram
// 簽署（而不是有人偽造的請求）。演算法照 Telegram 官方文件：
// https://core.telegram.org/widgets/login#checking-authorization
//
// fields 是 widget 回傳的所有欄位（id、first_name、username、auth_date、hash...），
// hash 本身不算進檢查字串。回傳驗證通過的 Telegram 使用者 id 與顯示名稱。
func VerifyTelegramLogin(fields map[string]string, botToken string) (telegramID int64, displayName string, err error) {
	hash := fields["hash"]
	if hash == "" {
		return 0, "", ErrTelegramHashMismatch
	}

	keys := make([]string, 0, len(fields)-1)
	for k := range fields {
		if k == "hash" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+fields[k])
	}
	dataCheckString := strings.Join(lines, "\n")

	secretKey := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, secretKey[:])
	mac.Write([]byte(dataCheckString))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(strings.ToLower(hash))) {
		return 0, "", ErrTelegramHashMismatch
	}

	if authDateStr := fields["auth_date"]; authDateStr != "" {
		authDateUnix, convErr := strconv.ParseInt(authDateStr, 10, 64)
		if convErr == nil && time.Since(time.Unix(authDateUnix, 0)) > maxAuthAge {
			return 0, "", ErrTelegramDataTooOld
		}
	}

	idStr := fields["id"]
	if idStr == "" {
		return 0, "", ErrTelegramMissingID
	}
	id, convErr := strconv.ParseInt(idStr, 10, 64)
	if convErr != nil {
		return 0, "", ErrTelegramMissingID
	}

	name := strings.TrimSpace(fields["first_name"] + " " + fields["last_name"])
	if name == "" {
		name = fields["username"]
	}
	return id, name, nil
}
