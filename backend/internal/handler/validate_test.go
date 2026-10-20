package handler

import "testing"

func TestValidLngLat(t *testing.T) {
	cases := []struct {
		name     string
		lng, lat float64
		want     bool
	}{
		{"HK", 114.1694, 22.3193, true},
		{"bounds", 180, 90, true},
		{"neg bounds", -180, -90, true},
		{"lng too big", 181, 0, false},
		{"lat too big", 0, 91, false},
		{"nan-ish zero", 0, 0, true}, // 0,0 是合法座標（雖然在海裡）
	}
	for _, tc := range cases {
		if got := validLngLat(tc.lng, tc.lat); got != tc.want {
			t.Errorf("%s: validLngLat(%f, %f) = %v, want %v", tc.name, tc.lng, tc.lat, got, tc.want)
		}
	}
}

func TestPhotoPathRe(t *testing.T) {
	valid := []string{
		"2026/10/0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d.jpg",
		"2020/01/ffffffffffffffffffffffffffffffff.png",
	}
	for _, p := range valid {
		if !photoPathRe.MatchString(p) {
			t.Errorf("expected valid: %q", p)
		}
	}
	invalid := []string{
		"", // 空（呼叫端允許非空才驗，這裡單測 regex 本身）
		"../etc/passwd",
		"2026/13/0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d.jpg", // 13 月
		"2026/00/0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d.jpg", // 00 月
		"2026/10/short.jpg",
		"2026/10/0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d.gif", // 只收 jpg/png
		"2026/10/0A1B2C3D4E5F6A7B8C9D0E1F2A3B4C5D.jpg", // 大寫不收（上傳端小寫化）
		"https://evil.example/x/2026/10/0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d.jpg",
		"2026/10/0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d.jpg.exe",
	}
	for _, p := range invalid {
		if p == "" {
			continue // 空字串由呼叫端以 != "" 判斷
		}
		if photoPathRe.MatchString(p) {
			t.Errorf("expected invalid: %q", p)
		}
	}
}

func TestUUIDRe(t *testing.T) {
	if !uuidRe.MatchString("550e8400-e29b-41d4-a716-446655440000") {
		t.Error("valid uuid should match")
	}
	if uuidRe.MatchString("550e8400e29b41d4a716446655440000") {
		t.Error("uuid without dashes should not match")
	}
	if uuidRe.MatchString("_zzzz8400-e29b-41d4-a716-446655440000") {
		t.Error("non-hex should not match")
	}
}
