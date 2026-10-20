package service

import (
	"testing"
	"time"

	"homeward/internal/repository"
)

// mkCandidate 組一個距離 a 點 distM 的候選（評分只用到 DistanceM/ID/LastSeenAt）。
func mkCandidate(id string, distM float64, lastSeen *time.Time) repository.CandidateCase {
	return repository.CandidateCase{
		ID: id, Lng: 114.1694, Lat: 22.3193,
		LastSeenAt: lastSeen, DistanceM: distM,
	}
}

func mkCase(lastSeen *time.Time) repository.Case {
	return repository.Case{ID: "a", CaseType: "missing_pet", Title: "走失", Lng: 114.1694, Lat: 22.3193, LastSeenAt: lastSeen}
}

// 距離極近 + 時間完全一致 → 接近滿分
func TestScoreCandidateBestCase(t *testing.T) {
	ts := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	score := scoreCandidate(mkCase(&ts), mkCandidate("b", 0, &ts))
	if score < 0.95 || score > 1.0 {
		t.Fatalf("expected ~1.0, got %f", score)
	}
}

// 距離 5 公里上限 → distScore = 0，僅剩時間分數（0.4 滿分）
func TestScoreCandidateFarDistance(t *testing.T) {
	ts := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	score := scoreCandidate(mkCase(&ts), mkCandidate("b", 5000, &ts))
	if score < 0.399 || score > 0.401 {
		t.Fatalf("expected ~0.4 (time only), got %f", score)
	}
}

// 時間差超過 14 天窗口 → 直接不推薦（回 0）
func TestScoreCandidateOutOfTimeWindow(t *testing.T) {
	ts := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	old := ts.Add(-15 * 24 * time.Hour)
	score := scoreCandidate(mkCase(&ts), mkCandidate("b", 0, &old))
	if score != 0 {
		t.Fatalf("expected 0 for out-of-window candidate, got %f", score)
	}
}

// 任一邊缺 last_seen_at → 時間分數取中性 0.5（不因缺資料歸零）
func TestScoreCandidateMissingTime(t *testing.T) {
	ts := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	score := scoreCandidate(mkCase(&ts), mkCandidate("b", 1250, nil)) // distScore=0.75
	want := 0.4*0.5 + 0.6*0.75
	if score < float32(want-0.001) || score > float32(want+0.001) {
		t.Fatalf("expected %f, got %f", want, score)
	}
}

// 距離超過比對半徑（理論上 FindCandidates 會擋掉，防禦參數）不小於 0
func TestScoreCandidateNegativeDistanceClamps(t *testing.T) {
	ts := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	score := scoreCandidate(mkCase(&ts), mkCandidate("b", 9000, &ts))
	if score < 0.399 || score > 0.401 {
		t.Fatalf("expected time-only score ~0.4, got %f", score)
	}
}
