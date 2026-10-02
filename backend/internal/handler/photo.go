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
)

// 只接受這三種圖片；以檔案內容（magic bytes）判斷，不信任客戶端給的副檔名。
var allowedTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

type PhotoHandler struct {
	MediaDir string // NAS 掛載點（容器內）
	MaxBytes int64
}

// Upload 處理 POST /api/v1/cases/upload-photo（multipart/form-data，欄位名 "photo"）。
// 圖片依年/月分目錄存入 NAS，回傳相對路徑（之後寫入 clues.photo_path）與可公開讀取的 URL。
func (h *PhotoHandler) Upload(c *gin.Context) {
	// 限制整個請求 body 大小，避免惡意大檔耗盡資源
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.MaxBytes+(1<<20))

	fh, err := c.FormFile("photo")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": `missing form file "photo"`})
		return
	}
	if fh.Size > h.MaxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large"})
		return
	}

	src, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot read upload"})
		return
	}
	defer src.Close()

	// 嗅探前 512 bytes 判斷真實類型
	head := make([]byte, 512)
	n, err := io.ReadFull(src, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read upload"})
		return
	}
	ext, ok := allowedTypes[http.DetectContentType(head[:n])]
	if !ok {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "only jpeg/png/webp allowed"})
		return
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot read upload"})
		return
	}

	// 隨機檔名，避免衝突與路徑穿越；相對路徑用 "/" 分隔（DB 與 URL 通用）
	name, err := randomName()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	now := time.Now()
	rel := path.Join(now.Format("2006"), now.Format("01"), name+ext)
	dst := filepath.Join(h.MediaDir, filepath.FromSlash(rel))

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot create media dir"})
		return
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // 0644：讓 Nginx 可讀
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot write file"})
		return
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		os.Remove(dst)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot write file"})
		return
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot write file"})
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
