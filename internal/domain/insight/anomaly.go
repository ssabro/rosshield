// anomaly.go — R14-4 IQR 1.5× outlier 탐지.
//
// 알고리즘:
//
//  1. 같은 (robot, check_id)의 직전 N session duration_ms 시퀀스 구성.
//  2. N >= 4가 안 되면 IQR 계산 의미 없음 → 해당 (robot, check) skip (전체 ErrInsufficientHistory 아님).
//  3. Q1, Q3 = 25/75 percentile (linear interpolation).
//  4. IQR = Q3 - Q1.
//  5. outlier = duration > Q3 + 1.5*IQR or duration < Q1 - 1.5*IQR.
//  6. 마지막 session의 duration이 outlier면 Insight (그 이전 outlier는 무시 — 최신 분만 보고).
//
// IQR=0 (모든 duration 동일) edge: outlier 임계도 Q3·Q1과 같아짐 — 마지막이 정확히 그 값이면 false.
// pure function — input 받아서 []Insight 반환, side effect 0.

package insight

import (
	"fmt"
	"sort"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// minAnomalySamples는 IQR 계산에 필요한 최소 표본 수입니다.
// 4보다 작으면 25/75 percentile이 의미 있게 계산되지 않습니다.
const minAnomalySamples = 4

// DetectAnomaly는 sessions와 results를 받아 마지막 session의 duration outlier를 Insight로 산출합니다.
//
// session 수 < DefaultDriftWindow면 ErrInsufficientHistory (drift와 동일 임계).
// 각 (robot, check)에서 표본 수 < minAnomalySamples면 해당 쌍은 skip.
func DetectAnomaly(now time.Time, sessions []ScanSessionView, resultsBySession map[string][]ScanResultView) ([]Insight, error) {
	if len(sessions) < DefaultDriftWindow {
		return nil, ErrInsufficientHistory
	}

	ordered := make([]ScanSessionView, len(sessions))
	copy(ordered, sessions)
	sort.Slice(ordered, func(i, j int) bool {
		return sessionTime(ordered[i]).Before(sessionTime(ordered[j]))
	})
	if len(ordered) > DefaultDriftWindow {
		ordered = ordered[len(ordered)-DefaultDriftWindow:]
	}
	tenantID := pickTenantID(sessions)

	type key struct {
		robotID string
		checkID string
	}
	seqs := make(map[key][]int64)
	for _, s := range ordered {
		for _, r := range resultsBySession[s.ID] {
			k := key{robotID: r.RobotID, checkID: r.CheckID}
			seqs[k] = append(seqs[k], r.DurationMs)
		}
	}

	keys := make([]key, 0, len(seqs))
	for k := range seqs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].robotID != keys[j].robotID {
			return keys[i].robotID < keys[j].robotID
		}
		return keys[i].checkID < keys[j].checkID
	})

	var insights []Insight
	for _, k := range keys {
		seq := seqs[k]
		if len(seq) < minAnomalySamples {
			continue
		}
		last := seq[len(seq)-1]
		// IQR 계산은 마지막을 제외한 표본으로 — 마지막을 outlier로 비교하기 위함.
		// 단, 표본이 4면 IQR base가 3개로 줄어 unstable. 여전히 가능은 하나 보수적으로 skip.
		base := seq[:len(seq)-1]
		if len(base) < 3 {
			continue
		}
		q1, q3 := quartiles(base)
		iqr := q3 - q1
		upper := q3 + DefaultIQRMultiplier*iqr
		lower := q1 - DefaultIQRMultiplier*iqr

		lastF := float64(last)
		var direction string
		switch {
		case lastF > upper && iqr > 0:
			direction = "high"
		case lastF < lower && iqr > 0:
			direction = "low"
		default:
			continue // outlier 아님 (IQR=0 edge 포함).
		}

		insights = append(insights, makeAnomalyInsight(tenantID, now, k.robotID, k.checkID, last, q1, q3, iqr, upper, lower, direction))
	}
	return insights, nil
}

// quartiles는 정렬된 sample의 Q1, Q3 percentile을 linear interpolation으로 계산합니다.
//
// Tukey method (R-7과 동일):
//
//	pos = (n-1) * p
//	idx = floor(pos), frac = pos - idx
//	value = sample[idx] + frac * (sample[idx+1] - sample[idx])
//
// n >= 2 가정.
func quartiles(samples []int64) (q1, q3 float64) {
	sorted := make([]int64, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	q1 = percentile(sorted, 0.25)
	q3 = percentile(sorted, 0.75)
	return q1, q3
}

func percentile(sorted []int64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return float64(sorted[0])
	}
	pos := float64(len(sorted)-1) * p
	idx := int(pos)
	frac := pos - float64(idx)
	if idx+1 >= len(sorted) {
		return float64(sorted[len(sorted)-1])
	}
	return float64(sorted[idx]) + frac*float64(sorted[idx+1]-sorted[idx])
}

func makeAnomalyInsight(tenantID storage.TenantID, now time.Time, robotID, checkID string, last int64, q1, q3, iqr, upper, lower float64, direction string) Insight {
	var sev = SeverityMedium // 시간 outlier만으로 high는 과함.
	summary := fmt.Sprintf("robot=%s check=%s duration %dms outlier(%s)", robotID, checkID, last, direction)
	var reasoning string
	if direction == "high" {
		reasoning = fmt.Sprintf("duration %dms (Q3+1.5IQR=%.0fms 초과; Q1=%.0f Q3=%.0f IQR=%.0f)",
			last, upper, q1, q3, iqr)
	} else {
		reasoning = fmt.Sprintf("duration %dms (Q1-1.5IQR=%.0fms 미달; Q1=%.0f Q3=%.0f IQR=%.0f)",
			last, lower, q1, q3, iqr)
	}
	return Insight{
		TenantID:     tenantID,
		Kind:         KindAnomaly,
		Severity:     sev,
		Scope:        Scope{RobotID: robotID, CheckID: checkID},
		Summary:      summary,
		Reasoning:    reasoning,
		EvidenceJSON: []byte("[]"),
		RulesApplied: []string{"iqr_1.5x"},
		Confidence:   0.8,
		ProducedBy:   ProducedByRules,
		CreatedAt:    now,
	}
}
