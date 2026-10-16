// Package media 處理上傳圖片的內容安全（剝除 metadata）。
package media

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
)

// JPEG 重編碼品質：高到足以辨識（照片用途），低到足以壓掉連拍大圖的儲存量。
// 上限 10MB 的手機照片重編碼後通常落在數百 KB ~ 數 MB 之間。
const jpegQuality = 85

// ErrUnsupportedImages 在呼叫端傳入非 JPEG／PNG 的內容型別時回傳
// （handler 已先用 DetectContentType 白名單擋過一次，這裡是防禦縱深）。
var ErrUnsupportedImages = errors.New("media: unsupported image type")

// Strip 解碼圖片並重新編碼，藉此剝除 EXIF／XMP／PNG tEXt 等 metadata——
// 尤其手機照片常帶 GPS 座標，而 /media/ 是公開目錄，若不剝除等於繞過
// cases.public_center 的位置模糊化直接洩漏位置。
//
// 重新編碼會略為壓縮畫質（JPEG quality 85），是刻意取捨：安全剝除 metadata
// 的成本。不支援的型別回 ErrUnsupportedImages，由 handler 回 415。
func Strip(r io.Reader, contentType string) (*bytes.Buffer, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, ErrUnsupportedImages
	}

	var buf bytes.Buffer
	switch contentType {
	case "image/jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, err
		}
	case "image/png":
		// PNG 一般無 EXIF；重編碼一併剝掉 tEXt/iTXt（可能含拍攝軟體等資訊）。
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := enc.Encode(&buf, img); err != nil {
			return nil, err
		}
	default:
		return nil, ErrUnsupportedImages
	}
	return &buf, nil
}
