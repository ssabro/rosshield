// drift.go — R14-3 직전 5 session 대비 (robot, check) outcome 전이 탐지.
//
// 알고리즘:
//
//  1. sessions가 N(=DefaultDriftWindow)개 미만이면 ErrInsufficientHistory.
//  2. session들을 시간순(과거 → 현재)으로 정렬해 (robot, check) 별 outcome 시퀀스 구성.
//  3. 마지막 outcome과 직전 outcome이 다르면 transition. 단, error/indeterminate/skipped는
//     비교 대상에서 제외 (pass↔fail만 안정 비교).
//  4. transition severity:
//     pass → fail = high
//     fail → pass = info
//     기타 (예: indeterminate에서 fail 등) = medium
//
// pure function — input 받아서 []Insight 반환, side effect 0.

package insight

import (
	"fmt"
	"sort"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// DetectDrift는 sessions(시간순 무관)와 그에 대응하는 results를 받아 drift Insight들을 산출합니다.
//
// sessions는 같은 fleet에 속해야 하며 detector가 completed_at ASC로 자체 정렬합니다.
// resultsBySession은 sessionID → []ScanResultView 맵 — 호출자가 미리 구성.
//
// session 수 < DefaultDriftWindow면 ErrInsufficientHistory.
// scope.RobotID, scope.CheckID는 채워지나 scope.FleetID는 비움 (drift는 robot 단위).
func DetectDrift(now time.Time, sessions []ScanSessionView, resultsBySession map[string][]ScanResultView) ([]Insight, error) {
	if len(sessions) < DefaultDriftWindow {
		return nil, ErrInsufficientHistory
	}

	// session을 completed_at ASC로 정렬 (과거 → 현재).
	ordered := make([]ScanSessionView, len(sessions))
	copy(ordered, sessions)
	sort.Slice(ordered, func(i, j int) bool {
		return sessionTime(ordered[i]).Before(sessionTime(ordered[j]))
	})
	// 직전 N개만 사용 — 가장 최근 N (window 끝).
	if len(ordered) > DefaultDriftWindow {
		ordered = ordered[len(ordered)-DefaultDriftWindow:]
	}

	tenantID := pickTenantID(sessions)

	// (robot, check) → outcome sequence.
	type key struct {
		robotID string
		checkID string
	}
	seqs := make(map[key][]string)
	for _, s := range ordered {
		for _, r := range resultsBySession[s.ID] {
			k := key{robotID: r.RobotID, checkID: r.CheckID}
			seqs[k] = append(seqs[k], r.Outcome)
		}
	}

	// 결정론적 출력을 위해 키 정렬.
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
		if len(seq) < 2 {
			continue // 비교할 prev가 없음.
		}
		last := seq[len(seq)-1]
		prev := seq[len(seq)-2]

		stable := isStableOutcome(last) && isStableOutcome(prev)
		if !stable {
			// noise involved.
			if last == prev {
				continue // 둘 다 같으면 transition 없음.
			}
			// noise → 다른 outcome. medium severity.
			insights = append(insights, makeDriftInsight(tenantID, now, k.robotID, k.checkID, prev, last, SeverityMedium, seq))
			continue
		}
		// pass ↔ fail.
		if last == prev {
			continue
		}
		var sev Severity
		switch {
		case prev == "pass" && last == "fail":
			sev = SeverityHigh
		case prev == "fail" && last == "pass":
			sev = SeverityInfo
		default:
			sev = SeverityMedium
		}
		insights = append(insights, makeDriftInsight(tenantID, now, k.robotID, k.checkID, prev, last, sev, seq))
	}
	return insights, nil
}

// makeDriftInsight는 transition 1건을 Insight로 packaging합니다.
func makeDriftInsight(tenantID storage.TenantID, now time.Time, robotID, checkID, prev, last string, sev Severity, seq []string) Insight {
	failCount := 0
	passCount := 0
	for _, o := range seq {
		switch o {
		case "fail":
			failCount++
		case "pass":
			passCount++
		}
	}
	summary := fmt.Sprintf("robot=%s check=%s outcome %s → %s", robotID, checkID, prev, last)
	reasoning := fmt.Sprintf("직전 %d session 중 fail=%d pass=%d (마지막 outcome=%s, 직전=%s)",
		len(seq), failCount, passCount, last, prev)
	return Insight{
		TenantID:     tenantID,
		Kind:         KindDrift,
		Severity:     sev,
		Scope:        Scope{RobotID: robotID, CheckID: checkID},
		Summary:      summary,
		Reasoning:    reasoning,
		EvidenceJSON: []byte("[]"),
		RulesApplied: []string{"drift_window_5"},
		Confidence:   1.0,
		ProducedBy:   ProducedByRules,
		CreatedAt:    now,
	}
}

// isStableOutcome은 pass/fail만 안정 비교 대상으로 인정합니다.
func isStableOutcome(o string) bool {
	return o == "pass" || o == "fail"
}

// sessionTime은 정렬용 — completed_at이 nil이면 zero time(가장 과거).
func sessionTime(s ScanSessionView) time.Time {
	if s.CompletedAt == nil {
		return time.Time{}
	}
	return *s.CompletedAt
}

// pickTenantID는 sessions에서 첫 번째 비어있지 않은 TenantID를 반환합니다.
// detector는 같은 tenant의 fleet에 대해 호출되므로 모두 동일.
func pickTenantID(sessions []ScanSessionView) storage.TenantID {
	for _, s := range sessions {
		if s.TenantID != "" {
			return s.TenantID
		}
	}
	return ""
}
