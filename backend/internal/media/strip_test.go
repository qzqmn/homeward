package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// 畫一張測試圖（左右兩個色塊）編碼成 JPEG，給 Strip 測試用。
func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			if x < w/2 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStripJPEGPreservesDimensions(t *testing.T) {
	src := jpegBytes(t, 60, 40)
	out, err := Strip(bytes.NewReader(src), "image/jpeg")
	if err != nil {
		t.Fatalf("Strip failed: %v", err)
	}
	decoded, _, err := image.Decode(out)
	if err != nil {
		t.Fatalf("re-decoding stripped output failed: %v", err)
	}
	if got := decoded.Bounds(); got.Dx() != 60 || got.Dy() != 40 {
		t.Fatalf("expected 60x40, got %v", got)
	}
}

func TestStripRejectsUnsupportedType(t *testing.T) {
	// GIF 魔數（不是白名單內容）：應回 ErrUnsupportedImages
	gif := []byte("GIF89a" + strings.Repeat("\x00", 64))
	if _, err := Strip(bytes.NewReader(gif), "image/gif"); err != ErrUnsupportedImages {
		t.Fatalf("expected ErrUnsupportedImages, got %v", err)
	}
}

func TestStripRejectsGarbage(t *testing.T) {
	junk := strings.Repeat("not an image at all\n", 10)
	if _, err := Strip(strings.NewReader(junk), "image/jpeg"); err != ErrUnsupportedImages {
		t.Fatalf("expected ErrUnsupportedImages, got %v", err)
	}
}

func TestStripPNGReencodes(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	img.Set(5, 5, color.RGBA{G: 255, A: 255})
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	out, err := Strip(bytes.NewReader(raw.Bytes()), "image/png")
	if err != nil {
		t.Fatalf("Strip png failed: %v", err)
	}
	decoded, format, err := image.Decode(out)
	if err != nil {
		t.Fatalf("re-decoding failed: %v", err)
	}
	if format != "png" {
		t.Fatalf("expected png output, got %s", format)
	}
	if got := decoded.Bounds(); got.Dx() != 10 || got.Dy() != 10 {
		t.Fatalf("expected 10x10, got %v", got)
	}
}
