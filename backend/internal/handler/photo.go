// Package handler 放 HTTP 處理函式。
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"homeward/internal/media"
)

// 只接受這兩種圖片；以檔案內容（magic bytes）判斷，不信任客戶端給的副檔名。
// 刻意「不」收 webp：Go 標準庫只能解碼 webp 無法編碼，無法用「重新編碼剝
// metadata」的安全做法（webp 又常帶 EXIF chunk），寧可擋下也不放未處理的圖。
var allowedTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
}

type PhotoHandler struct {
	MediaDir string // NAS 掛載點（容器內）
	MaxBytes int64
}

// saveImage 處理 multipart 上傳圖片（限大小、內容嗅探、重新編碼剝 EXIF、
// 寫入 NAS 年月目錄）。成功回相對路徑；任何問題都已寫好錯誤回應並回 false。
// Upload 與「案件補傳照片」共用同一條安全管道。
func (h *PhotoHandler) saveImage(c *gin.Context) (string, bool) {
	// 限制整個請求 body 大小，避免惡意大檔耗盡資源
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.MaxBytes+(1<<20))

	fh, err := c.FormFile("photo")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large"})
			return "", false
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": `missing form file "photo"`})
		return "", false
	}
	if fh.Size > h.MaxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large"})
		return "", false
	}

	src, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot read upload"})
		return "", false
	}
	defer src.Close()

	// 嗅探前 512 bytes 判斷真實類型
	head := make([]byte, 512)
	n, err := io.ReadFull(src, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read upload"})
		return "", false
	}
	contentType := http.DetectContentType(head[:n])
	ext, ok := allowedTypes[contentType]
	if !ok {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "only jpeg/png allowed"})
		return "", false
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot read upload"})
		return "", false
	}

	// 重新編碼剝除 metadata：手機照片多帶 GPS 座標，而 /media/ 是公開目錄，
	// 不剝除等於把 cases.public_center 特意模糊化掉的位置直接洩漏出去。
	stripped, err := media.Strip(src, contentType)
	if err != nil {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "invalid image content"})
		return "", false
	}

	// 隨機檔名，避免衝突與路徑穿越；相對路徑用 "/" 分隔（DB 與 URL 通用）
	name, err := randomName()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return "", false
	}
	now := time.Now()
	rel := path.Join(now.Format("2006"), now.Format("01"), name+ext)
	dst := filepath.Join(h.MediaDir, filepath.FromSlash(rel))

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot create media dir"})
		return "", false
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // 0644：讓 Nginx 可讀
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot write file"})
		return "", false
	}
	if _, err := io.Copy(out, stripped); err != nil {
		out.Close()
		os.Remove(dst)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot write file"})
		return "", false
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot write file"})
		return "", false
	}
	return rel, true
}

// Upload 處理 POST /api/v1/cases/upload-photo（multipart/form-data，欄位名 "photo"）。
// 圖片依年/月分目錄存入 NAS，回傳相對路徑（之後寫入 clues.photo_path）與可公開讀取的 URL。
func (h *PhotoHandler) Upload(c *gin.Context) {
	rel, ok := h.saveImage(c)
	if !ok {
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"path": rel,
		"url":  "/media/" + rel, // Nginx 直接從 NAS 讀取
	})
}

func randomName() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
