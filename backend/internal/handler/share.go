// Package handler：給 WhatsApp / Facebook / Telegram 等連結預覽爬蟲讀取的分享頁。
//
// 這些爬蟲不會執行前端 SPA 的 JavaScript，所以連結預覽必須由後端直接吐出含 OG
// meta 標籤的靜態 HTML；真人點擊連結則用 <meta http-equiv="refresh"> 立刻轉去 PWA。
package handler

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"homeward/internal/repository"
)

type ShareHandler struct {
	Cases   *repository.CaseRepo
	BaseURL string // 對外網域，例如 https://homeward.example.com；OG 標籤規定要用絕對網址
}

// Page 處理 GET /c/:id。
func (h *ShareHandler) Page(c *gin.Context) {
	id := c.Param("id")
	if !uuidRe.MatchString(id) {
		c.String(http.StatusBadRequest, "invalid case id")
		return
	}

	cs, err := h.Cases.GetByID(c.Request.Context(), id)
	if err != nil {
		c.String(http.StatusNotFound, "case not found")
		return
	}

	title := shareTitle(cs)
	description := truncate(cs.Description, 160)
	pageURL := h.BaseURL + "/c/" + cs.ID
	appURL := h.BaseURL + "/cases/" + cs.ID // PWA（SPA）內的案件詳情路由

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"zh-Hant\">\n<head>\n<meta charset=\"utf-8\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(title))
	fmt.Fprintf(&b, "<meta property=\"og:title\" content=\"%s\">\n", html.EscapeString(title))
	fmt.Fprintf(&b, "<meta property=\"og:description\" content=\"%s\">\n", html.EscapeString(description))
	fmt.Fprintf(&b, "<meta property=\"og:url\" content=\"%s\">\n", html.EscapeString(pageURL))
	b.WriteString("<meta property=\"og:type\" content=\"website\">\n")
	if cs.PhotoPath != nil {
		imageURL := h.BaseURL + "/media/" + *cs.PhotoPath
		fmt.Fprintf(&b, "<meta property=\"og:image\" content=\"%s\">\n", html.EscapeString(imageURL))
		b.WriteString("<meta name=\"twitter:card\" content=\"summary_large_image\">\n")
	}
	// 沒有封面照時不給 og:image：目前沒有通用預設圖，寧可讓預覽卡缺圖，也不要放一張
	// 跟案件無關的圖片誤導使用者。之後可以在 frontend/dist/og-default.png 補一張預設圖，
	// 並在這裡補上 fallback。
	fmt.Fprintf(&b, "<meta http-equiv=\"refresh\" content=\"0; url=%s\">\n", html.EscapeString(appURL))
	b.WriteString("</head>\n<body>\n")
	fmt.Fprintf(&b, "<p><a href=\"%s\">%s</a></p>\n", html.EscapeString(appURL), html.EscapeString(title))
	b.WriteString("</body>\n</html>\n")

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, b.String())
}

func shareTitle(cs repository.Case) string {
	prefix, ok := map[string]string{
		"missing_person": "尋人",
		"missing_pet":    "尋寵",
		"found_person":   "協尋失主",
		"found_pet":      "協尋主人",
	}[cs.CaseType]
	if !ok {
		prefix = "歸途"
	}
	return prefix + "：" + cs.Title + "｜歸途 Homeward"
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
