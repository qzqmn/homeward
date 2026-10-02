package service

import (
	"context"
	"time"

	"homeward/internal/repository"
)

const (
	matchSearchRadiusM  = 5000                // 只比對 5 公里內的案件
	matchTimeWindow     = 14 * 24 * time.Hour // 只比對 14 天內的走失/發現時間
	matchCandidateLimit = 20
)

// MatchingService 在建立「走失」或「撿到/發現」案件後，找出類型相對、距離與時間相近的
// 候選案件並寫入配對建議，供案主自行確認或排除（不會自動判定「已團聚」）。
type MatchingService struct {
	matches *repository.MatchRepo
}

func NewMatchingService(matches *repository.MatchRepo) *MatchingService {
	return &MatchingService{matches: matches}
}

// RunFor 對指定案件執行一次配對比對；建案後呼叫一次即可，之後案主可在配對列表手動觸發重算。
func (s *MatchingService) RunFor(ctx context.Context, newCase repository.Case) error {
	candidates, err := s.matches.FindCandidates(
		ctx, newCase.ID, newCase.CaseType, newCase.Lng, newCase.Lat, matchSearchRadiusM, matchCandidateLimit)
	if err != nil {
		return err
	}

	for _, c := range candidates {
		score := scoreCandidate(newCase, c)
		if score <= 0 {
			continue // 時間差超出窗口，視為不相關，不寫入配對建議
		}
		if err := s.matches.Upsert(ctx, repository.NewMatch{
			CaseID: newCase.ID, MatchedCaseID: c.ID, Score: score,
		}); err != nil {
			return err
		}
	}
	return nil
}

// scoreCandidate 是刻意保持簡單的規則式評分：距離越近分數越高（滿分 0.6），
// 時間差越小分數越高（滿分 0.4）；兩者相乘的加權方式先不做，避免其中一項偏低就整體歸零
// ——距離很近但時間差大仍值得讓案主看一眼（例如寵物在原地徘徊了一段時間才被發現）。
// 這是最基礎的版本，之後可以再加入品種/特徵文字比對或照片相似度。
func scoreCandidate(a repository.Case, b repository.CandidateCase) float32 {
	distScore := 1 - b.DistanceM/matchSearchRadiusM
	if distScore < 0 {
		distScore = 0
	}

	timeScore := 0.5 // 任一邊沒填 last_seen_at 時，時間分數給中性值，不因缺資料而排除配對
	if a.LastSeenAt != nil && b.LastSeenAt != nil {
		diff := a.LastSeenAt.Sub(*b.LastSeenAt)
		if diff < 0 {
			diff = -diff
		}
		if diff > matchTimeWindow {
			return 0 // 超出時間窗口，不視為候選
		}
		timeScore = 1 - float64(diff)/float64(matchTimeWindow)
	}

	return float32(0.6*distScore + 0.4*timeScore)
}
