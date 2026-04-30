// peer.go — R14-5 fleet 평균 - 1σ 미달 robot 탐지.
//
// 알고리즘:
//
//  1. fleet 내 모든 robot의 마지막(가장 최근 completed) session results 수집.
//  2. robot별 pass 비율 = pass_count / total_count (pass+fail+indeterminate+error+skipped 모두 분모).
//  3. 모든 robot의 pass 비율로 fleet 평균 μ, 표준편차 σ 계산 (population stdev).
//  4. robot 수 < 2 또는 σ == 0이면 결과 없음 (single robot fleet edge).
//  5. robot pass_ratio < μ - DefaultPeerSigmaMultiplier × σ면 Insight.
//
// pure function — input 받아서 []Insight 반환, side effect 0.

package insight

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// DetectPeer는 fleet 단위로 robot별 pass 비율 outlier(아래 방향만)를 Insight로 산출합니다.
//
// sessions는 호출자가 같은 fleet의 직전 N개를 전달 — 그 중 robot별 가장 최근 completed session 1개만 사용.
// 어떤 fleet에 robot이 1개뿐이면 σ=0 — 결과 없음 (insufficient peers).
//
// scope.RobotID, scope.FleetID 채움. CheckID는 비움 (peer는 robot 전체 비율 단위).
func DetectPeer(now time.Time, fleetID string, sessions []ScanSessionView, resultsBySession map[string][]ScanResultView) ([]Insight, error) {
	tenantID := pickTenantID(sessions)

	// robot → 가장 최근 completed session (이 fleet 한정).
	type robotLatest struct {
		robotID   string
		sessionID string
		ts        time.Time
	}
	latestByRobot := make(map[string]robotLatest)
	for _, s := range sessions {
		if s.FleetID != fleetID {
			continue
		}
		ts := sessionTime(s)
		for _, r := range resultsBySession[s.ID] {
			cur, ok := latestByRobot[r.RobotID]
			if !ok || ts.After(cur.ts) {
				latestByRobot[r.RobotID] = robotLatest{robotID: r.RobotID, sessionID: s.ID, ts: ts}
			}
		}
	}

	if len(latestByRobot) < 2 {
		return nil, nil // single robot fleet — peer 비교 불가.
	}

	// robot → pass_ratio.
	type robotStat struct {
		robotID   string
		passRatio float64
		passCount int
		total     int
	}
	stats := make([]robotStat, 0, len(latestByRobot))
	for _, latest := range latestByRobot {
		results := resultsBySession[latest.sessionID]
		var pass, total int
		for _, r := range results {
			if r.RobotID != latest.robotID {
				continue
			}
			total++
			if r.Outcome == "pass" {
				pass++
			}
		}
		if total == 0 {
			continue
		}
		stats = append(stats, robotStat{
			robotID:   latest.robotID,
			passRatio: float64(pass) / float64(total),
			passCount: pass,
			total:     total,
		})
	}
	if len(stats) < 2 {
		return nil, nil
	}

	// μ, σ 계산 (population).
	var sum float64
	for _, s := range stats {
		sum += s.passRatio
	}
	mu := sum / float64(len(stats))
	var sqSum float64
	for _, s := range stats {
		d := s.passRatio - mu
		sqSum += d * d
	}
	sigma := math.Sqrt(sqSum / float64(len(stats)))
	if sigma == 0 {
		return nil, nil // 모든 robot pass 비율 동일 — outlier 없음.
	}
	threshold := mu - DefaultPeerSigmaMultiplier*sigma

	// 결정론적 출력을 위해 robot ID 정렬.
	sort.Slice(stats, func(i, j int) bool { return stats[i].robotID < stats[j].robotID })

	var insights []Insight
	for _, s := range stats {
		if s.passRatio >= threshold {
			continue
		}
		insights = append(insights, makePeerInsight(tenantID, now, fleetID, s.robotID, s.passRatio, mu, sigma, threshold))
	}
	return insights, nil
}

func makePeerInsight(tenantID storage.TenantID, now time.Time, fleetID, robotID string, passRatio, mu, sigma, threshold float64) Insight {
	sev := SeverityMedium
	summary := fmt.Sprintf("robot=%s pass=%.0f%% (fleet 평균 %.0f%% 미달)",
		robotID, passRatio*100, mu*100)
	reasoning := fmt.Sprintf("robot pass=%.0f%% (fleet 평균 %.0f%%, σ=%.0f%% — %.0f%% 임계 미달)",
		passRatio*100, mu*100, sigma*100, threshold*100)
	return Insight{
		TenantID:     tenantID,
		Kind:         KindPeer,
		Severity:     sev,
		Scope:        Scope{RobotID: robotID, FleetID: fleetID},
		Summary:      summary,
		Reasoning:    reasoning,
		EvidenceJSON: []byte("[]"),
		RulesApplied: []string{"peer_fleet_avg_1sigma"},
		Confidence:   0.7,
		ProducedBy:   ProducedByRules,
		CreatedAt:    now,
	}
}
