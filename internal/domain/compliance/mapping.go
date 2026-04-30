package compliance

// scan outcome string은 scan 도메인 5-값 enum과 동일 — compliance가 scan을 import하지 않도록
// 문자열 const로 복제. 변경 시 양쪽 동기화 필요(자주 변경되지 않음).
const (
	outcomePass          = "pass"
	outcomeFail          = "fail"
	outcomeIndeterminate = "indeterminate"
	outcomeError         = "error"
	outcomeNotApplicable = "not_applicable"
	outcomeSkipped       = "skipped"
)

// AggregateControlStatuses는 ScanResultView 슬라이스를 ControlDefinition 매핑에 따라
// ControlStatus 슬라이스로 집계합니다 (§08.13).
//
// 알고리즘:
//
//  1. results를 CheckID → outcomes 맵으로 인덱싱 (한 check가 여러 robot에서 실행되므로 슬라이스).
//  2. 각 ControlDefinition에 대해:
//     a. MappedCheckIDs == ∅                              → Unmapped (PassCount/FailCount=0)
//     b. 매핑된 results 합계 == 0                          → Unmapped
//     c. NA + skipped만 있으면                             → NotApplicable
//     d. passCount > 0 && failCount == 0                  → Pass
//     e. passCount == 0 && failCount > 0                  → Fail
//     f. 나머지 (혼합)                                     → Partial
//
// 여기서 failCount는 fail + error + indeterminate의 합 — 보수적(감사 관점에서 미확정은 실패).
// 결과 슬라이스 순서는 controls 순서를 그대로 보존합니다.
func AggregateControlStatuses(controls []ControlDefinition, results []ScanResultView) []ControlStatus {
	idx := indexResultsByCheckID(results)
	out := make([]ControlStatus, 0, len(controls))
	for _, c := range controls {
		out = append(out, evaluateControl(c, idx))
	}
	return out
}

// indexResultsByCheckID는 results를 CheckID 키로 그룹화합니다.
// 같은 (session, robot, check)가 여러 번 등장할 일은 없으나, 여러 robot에서 같은 check가 실행되면
// 한 CheckID에 여러 outcome이 모입니다.
func indexResultsByCheckID(results []ScanResultView) map[string][]string {
	idx := make(map[string][]string, len(results))
	for _, r := range results {
		idx[r.CheckID] = append(idx[r.CheckID], r.Outcome)
	}
	return idx
}

func evaluateControl(c ControlDefinition, idx map[string][]string) ControlStatus {
	if len(c.MappedCheckIDs) == 0 {
		return ControlStatus{ControlID: c.ID, Status: StatusUnmapped}
	}

	var passCount, failCount, naCount, totalMapped int
	for _, checkID := range c.MappedCheckIDs {
		outcomes, ok := idx[checkID]
		if !ok {
			continue
		}
		for _, o := range outcomes {
			totalMapped++
			switch o {
			case outcomePass:
				passCount++
			case outcomeFail, outcomeError, outcomeIndeterminate:
				// 보수적: error·indeterminate도 fail에 산입 (감사 관점).
				failCount++
			case outcomeNotApplicable, outcomeSkipped:
				naCount++
			default:
				// 알 수 없는 outcome은 실패로 분류 (안전한 기본값).
				failCount++
			}
		}
	}

	if totalMapped == 0 {
		return ControlStatus{ControlID: c.ID, Status: StatusUnmapped}
	}
	if naCount == totalMapped {
		return ControlStatus{ControlID: c.ID, Status: StatusNotApplicable}
	}
	switch {
	case passCount > 0 && failCount == 0:
		return ControlStatus{
			ControlID: c.ID,
			Status:    StatusPass,
			PassCount: passCount,
			FailCount: failCount,
		}
	case passCount == 0 && failCount > 0:
		return ControlStatus{
			ControlID: c.ID,
			Status:    StatusFail,
			PassCount: passCount,
			FailCount: failCount,
		}
	default:
		return ControlStatus{
			ControlID: c.ID,
			Status:    StatusPartial,
			PassCount: passCount,
			FailCount: failCount,
		}
	}
}

// ScoreFromStatuses는 통제별 status를 0.0~1.0 점수로 변환합니다 (§08.13).
//
// 가중치:
//
//	Pass     = 1.0
//	Partial  = 0.5
//	Fail     = 0.0
//	Unmapped = 점수 산정 제외 (미평가)
//	NotApplicable = 점수 산정 제외 (해당 없음)
//
// 점수 = sum(가중치) / count(pass + fail + partial).
// 평가 가능한 통제가 0개면 1.0 (vacuous truth — 평가할 통제가 없으면 부적합 사유 없음).
func ScoreFromStatuses(statuses []ControlStatus) float64 {
	var sum float64
	var n int
	for _, s := range statuses {
		switch s.Status {
		case StatusPass:
			sum += 1.0
			n++
		case StatusPartial:
			sum += 0.5
			n++
		case StatusFail:
			n++
		case StatusUnmapped, StatusNotApplicable:
			// 점수 산정 제외.
		}
	}
	if n == 0 {
		return 1.0
	}
	return sum / float64(n)
}

// CountStatuses는 status별 개수를 반환합니다 (FrameworkSnapshot 컬럼 채우기용).
func CountStatuses(statuses []ControlStatus) (pass, fail, partial, na, unmapped int) {
	for _, s := range statuses {
		switch s.Status {
		case StatusPass:
			pass++
		case StatusFail:
			fail++
		case StatusPartial:
			partial++
		case StatusNotApplicable:
			na++
		case StatusUnmapped:
			unmapped++
		}
	}
	return
}
