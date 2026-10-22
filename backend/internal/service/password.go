package service

import (
	"errors"
	"fmt"
	"regexp"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// 密碼登入（用戶名 + 密碼）的共用規則與 hash 工具。
// bcrypt 是業界標準、慢 hash——DB 被拆走也難以還原原碼。
// 免費平台的定位是「防著譽」，不追求企業級密碼策略。

var ErrBadUsername = errors.New("password: 用戶名格式不符")
var ErrBadPassword = errors.New("password: 密碼強度不足")
var ErrWrongPassword = errors.New("password: 密碼錯誤")

// usernameRe 用戶名：3–20 個英數/底線（不分大小寫唯一在 DB 索引處理）。
var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,20}$`)

const (
	passwordMinLen = 8
	passwordMaxLen = 72 // bcrypt 只取前 72 字節，超過的截斷；直接在入口擋掉
)

// ValidUsername 檢查用戶名格式；回 nil 表示可用。
func ValidUsername(username string) error {
	if !usernameRe.MatchString(username) {
		return fmt.Errorf("%w：3–20 個英文字母、數字或底線", ErrBadUsername)
	}
	return nil
}

// ValidPassword 檢查密碼：8–72 字、至少含一個字母與一個數字。
func ValidPassword(password string) error {
	runes := []rune(password)
	if len(runes) < passwordMinLen {
		return fmt.Errorf("%w：至少 %d 個字元", ErrBadPassword, passwordMinLen)
	}
	if len(password) > passwordMaxLen {
		return fmt.Errorf("%w：最多 %d 字節", ErrBadPassword, passwordMaxLen)
	}
	var hasLetter, hasDigit bool
	for _, r := range runes {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("%w：須同時含字母與數字", ErrBadPassword)
	}
	return nil
}

// HashPassword 產生 bcrypt hash（預設 cost 10）。
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt hash: %w", err)
	}
	return string(hash), nil
}

// CheckPassword 比對明碼與 hash；不符（或無 hash）回 ErrWrongPassword。
func CheckPassword(hash, password string) error {
	if hash == "" {
		return ErrWrongPassword // 沒設過密碼的帳號不是走密碼登入
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return ErrWrongPassword
	}
	return nil
}
