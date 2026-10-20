package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testBotToken = "123456:TEST-TOKEN"

// signTelegram 用與 VerifyTelegramLogin 相同的演算法替欄位簽章，製造合法測試資料。
func signTelegram(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	// 簡易排序（測試用欄位數量少）
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+fields[k])
	}
	secret := sha256.Sum256([]byte(testBotToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(mac.Sum(nil))
}

func telegramFields() map[string]string {
	f := map[string]string{
		"id":         "555666777",
		"first_name": "小",
		"last_name":  "明",
		"username":   "ming",
		"auth_date":  "1700000000",
	}
	return f
}

func TestVerifyTelegramLoginSuccess(t *testing.T) {
	f := telegramFields()
	f["auth_date"] = strconv.FormatInt(time.Now().Unix(), 10) // 新鮮的登入時間
	f["hash"] = signTelegram(f)

	id, name, err := VerifyTelegramLogin(f, testBotToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 555666777 {
		t.Fatalf("expected id 555666777, got %d", id)
	}
	if name != "小 明" {
		t.Fatalf("expected name 小 明, got %q", name)
	}
}

func TestVerifyTelegramLoginTamperedHash(t *testing.T) {
	f := telegramFields()
	f["hash"] = signTelegram(f)
	f["first_name"] = "壞" // 簽章後改內容
	_, _, gotErr := VerifyTelegramLogin(f, testBotToken)
	if gotErr != ErrTelegramHashMismatch {
		t.Fatalf("tampered payload must not pass, got %v", gotErr)
	}
}

func TestVerifyTelegramLoginExpiredAuthDate(t *testing.T) {
	f := telegramFields()
	f["auth_date"] = "1617000000" // 2021 年，遠超過 24 小時
	f["hash"] = signTelegram(f)
	_, _, err := VerifyTelegramLogin(f, testBotToken)
	if err != ErrTelegramDataTooOld {
		t.Fatalf("expected ErrTelegramDataTooOld, got %v", err)
	}
}

// 曾經的漏洞：auth_date 解析失敗會被靜默放行；現在必須直接拒絕
func TestVerifyTelegramLoginInvalidAuthDateRejected(t *testing.T) {
	f := telegramFields()
	f["auth_date"] = "not-a-number"
	f["hash"] = signTelegram(f)
	_, _, err := VerifyTelegramLogin(f, testBotToken)
	if err != ErrTelegramInvalidData {
		t.Fatalf("expected ErrTelegramInvalidData, got %v", err)
	}
}

func TestVerifyTelegramLoginMissingID(t *testing.T) {
	f := telegramFields()
	delete(f, "id")
	f["auth_date"] = strconv.FormatInt(time.Now().Unix(), 10)
	f["hash"] = signTelegram(f)
	_, _, err := VerifyTelegramLogin(f, testBotToken)
	if err != ErrTelegramMissingID {
		t.Fatalf("expected ErrTelegramMissingID, got %v", err)
	}
}

func TestVerifyTelegramLoginNoHash(t *testing.T) {
	f := telegramFields()
	_, _, err := VerifyTelegramLogin(f, testBotToken)
	if err != ErrTelegramHashMismatch {
		t.Fatalf("expected ErrTelegramHashMismatch, got %v", err)
	}
}
