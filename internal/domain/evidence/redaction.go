// Package evidence는 SSH 캡처 증거(stdout/stderr·exit·메타)를 redact·hash·blob 저장하는
// 도메인 패키지입니다. 본 파일(Stage A)은 Redaction 엔진만 제공합니다.
//
// 결정 근거:
//   - R9-1 자체 구현 only (gitleaks·trufflehog 코드 import 금지, 패턴 텍스트만 참고)
//   - R9-5 Phase 1 패턴 8개 (PEM·AWS·GitHub·Slack·Bearer·password·api_key·private_key)
//   - R9-6 redact는 호출자가 평문을 받자마자 적용 — 본 패키지는 메모리 변환만 책임
//   - 외부 dep 0
//
// 도메인 결합 규칙: 본 패키지는 다른 도메인을 import하지 않습니다 (P5 + depguard 예정).
package evidence

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
)

// RedactionMark는 원본 raw bytes에서 마스킹된 한 구간을 가리킵니다.
//
// Offset/Length는 원본 바이트 인덱스 기준이며, 출력 buffer가 아닌 입력 좌표계를 따릅니다.
// Type은 매치된 패턴의 라벨로, 우선순위 병합 후 가장 specific한 라벨이 선택됩니다.
type RedactionMark struct {
	Offset int
	Length int
	Type   string
}

// maxMatchesPerRule은 룰당 RegexpAll 매치 상한입니다 (R9-5 메모리 spike 방어).
//
// 거대 stdout(상한 10MiB)에서 패턴이 수만 번 매치되면 슬라이스가 수십 MB로 부풀 수 있습니다.
// 이 상한 초과 분은 룰 단위로 잘라내고, 추가 경고 마크는 부착하지 않습니다 (Phase 2 검토).
const maxMatchesPerRule = 10000

// Redact는 raw bytes에서 비밀 패턴을 찾아 [REDACTED:type:N] 마커로 치환하고
// 원본 위치 정보(RedactionMark 슬라이스, offset 오름차순)를 반환합니다.
//
// 입력 mutation 0 — 새 byte slice 반환. nil 또는 empty 입력은 그대로 반환합니다.
// 패턴이 겹치면 우선순위 적용 후 단일 마커로 병합합니다.
func Redact(raw []byte) ([]byte, []RedactionMark) {
	if len(raw) == 0 {
		return raw, nil
	}

	marks := collectMarks(raw)
	if len(marks) == 0 {
		// 출력은 입력의 별개 복사본을 돌려준다 (호출자가 mutate해도 안전).
		out := make([]byte, len(raw))
		copy(out, raw)
		return out, nil
	}

	sortMarks(marks)
	merged := mergeMarks(marks)
	out := substitute(raw, merged)
	return out, merged
}

// rule은 단일 redaction 룰입니다.
//
// re는 RE2 호환 정규식, group은 마스킹할 캡처 그룹 번호 (0이면 전체 매치).
// validate는 false positive 후처리 (예: password=true 거부) — nil 가능.
type rule struct {
	name     string
	re       *regexp.Regexp
	group    int
	validate func(value []byte) bool
}

// truthyKeywords는 password/api_key/private_key 의 false positive 거부 목록입니다.
//
// 노트 §함정 6: `password_required=true` 같은 설정 키 값은 비밀이 아니므로 redact 제외.
var truthyKeywords = map[string]struct{}{
	"true":     {},
	"false":    {},
	"yes":      {},
	"no":       {},
	"enabled":  {},
	"disabled": {},
	"0":        {},
	"1":        {},
	"null":     {},
	"nil":      {},
	"none":     {},
	"empty":    {},
}

// notTruthy는 캡처된 value가 truthy 키워드가 아닐 때만 통과시킵니다.
func notTruthy(value []byte) bool {
	v := bytes.ToLower(bytes.TrimSpace(value))
	if len(v) == 0 {
		return false
	}
	_, isTruthy := truthyKeywords[string(v)]
	return !isTruthy
}

// rules는 Phase 1 패턴 8개를 우선순위와 무관하게 컴파일해 둡니다.
//
// 우선순위는 priorityRank()로 별도 적용 — pem > {github,slack,aws} > bearer > {password,api,private}.
// PEM 룰은 (?s) dot-all로 멀티라인 처리합니다 (노트 §함정 1).
var rules = []rule{
	{
		name:  "pem",
		re:    regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
		group: 0,
	},
	{
		name:  "aws-access-key",
		re:    regexp.MustCompile(`\b(?:A3T[A-Z0-9]|AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`),
		group: 0,
	},
	{
		name:  "github-token",
		re:    regexp.MustCompile(`\bgh[pos]_[A-Za-z0-9_]{36,255}\b`),
		group: 0,
	},
	{
		name:  "slack-token",
		re:    regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
		group: 0,
	},
	{
		// Authorization 헤더 라벨은 보존하고 토큰 본문(group 1)만 마스킹한다.
		// 노트 §함정 5: bearer/JWT 겹침은 mergeMarks가 흡수.
		name:  "bearer-token",
		re:    regexp.MustCompile(`(?i)Authorization\s*:\s*(?:Bearer|Basic)\s+([A-Za-z0-9._\-+/=]{8,})`),
		group: 1,
	},
	{
		name:     "password",
		re:       regexp.MustCompile(`(?i)\b(?:password|passwd|pwd)\s*[:=]\s*["']?([^\s"'&;]{1,128})`),
		group:    0,
		validate: notTruthy,
	},
	{
		name:     "api-key",
		re:       regexp.MustCompile(`(?i)\bapi[_-]?key\s*[:=]\s*["']?([^\s"'&;]{1,128})`),
		group:    0,
		validate: notTruthy,
	},
	{
		name:     "private-key",
		re:       regexp.MustCompile(`(?i)\bprivate[_-]?key\s*[:=]\s*["']?([^\s"'&;]{1,128})`),
		group:    0,
		validate: notTruthy,
	},
}

// priorityRank는 type 라벨의 specificity를 반환합니다 (높을수록 우선).
//
// 병합 시 더 specific한 타입이 살아남습니다 (노트 §3.3).
// pem > {github,slack,aws} > bearer > {password,api,private} 순.
func priorityRank(t string) int {
	switch t {
	case "pem":
		return 100
	case "github-token", "slack-token", "aws-access-key":
		return 90
	case "bearer-token":
		return 80
	case "password", "api-key", "private-key":
		return 60
	default:
		return 0
	}
}

// collectMarks는 모든 룰에 대해 input 전체를 한 번씩 매칭하고,
// 룰별 매치 상한(maxMatchesPerRule) 내에서 RedactionMark를 누적합니다.
//
// validate 콜백이 false를 돌리면 false positive로 보고 마크를 만들지 않습니다.
func collectMarks(input []byte) []RedactionMark {
	var marks []RedactionMark
	for _, r := range rules {
		// FindAllSubmatchIndex(-1)로 모두 모은 뒤 상한을 적용한다.
		// RE2는 단일 패스이므로 전체 수집의 cost 자체가 input 크기에 선형이다.
		all := r.re.FindAllSubmatchIndex(input, maxMatchesPerRule)
		for _, m := range all {
			start, end := m[2*r.group], m[2*r.group+1]
			if start < 0 || end <= start {
				continue
			}
			if r.validate != nil {
				// validate에 넘기는 캡처는 group(1)이 우선, 없으면 전체.
				vStart, vEnd := start, end
				if len(m) >= 4 && m[2] >= 0 {
					vStart, vEnd = m[2], m[3]
				}
				if !r.validate(input[vStart:vEnd]) {
					continue
				}
			}
			marks = append(marks, RedactionMark{
				Offset: start,
				Length: end - start,
				Type:   r.name,
			})
		}
	}
	return marks
}

// sortMarks는 Offset 오름차순, 동률이면 Length 내림차순(큰 마크 우선)으로 정렬합니다.
func sortMarks(marks []RedactionMark) {
	sort.SliceStable(marks, func(i, j int) bool {
		if marks[i].Offset != marks[j].Offset {
			return marks[i].Offset < marks[j].Offset
		}
		return marks[i].Length > marks[j].Length
	})
}

// mergeMarks는 정렬된 마크에서 겹침/인접(거리 ≤ 1)을 단일 마크로 병합합니다.
//
// 더 specific한 type이 살아남고, 끝점은 둘 중 더 멀리 뻗는 쪽으로 확장됩니다.
func mergeMarks(marks []RedactionMark) []RedactionMark {
	if len(marks) == 0 {
		return nil
	}
	result := make([]RedactionMark, 0, len(marks))
	result = append(result, marks[0])
	for i := 1; i < len(marks); i++ {
		prev := &result[len(result)-1]
		cur := marks[i]
		prevEnd := prev.Offset + prev.Length
		// 거리 ≤ 1이면 인접/겹침으로 본다.
		if cur.Offset <= prevEnd+1 {
			if priorityRank(cur.Type) > priorityRank(prev.Type) {
				prev.Type = cur.Type
			}
			curEnd := cur.Offset + cur.Length
			end := prevEnd
			if curEnd > end {
				end = curEnd
			}
			prev.Length = end - prev.Offset
			continue
		}
		result = append(result, cur)
	}
	return result
}

// substitute는 input의 마크 영역을 [REDACTED:type:N] 마커로 치환한 새 byte slice를 반환합니다.
//
// 단일 패스 — bytes.Buffer.Grow(len(input))로 사전 할당해 메모리 spike를 줄입니다 (R9-5).
// 마커는 ASCII만 사용하므로 UTF-8 invalid byte 환경에서도 바이트 인덱스 기반으로 안전합니다.
func substitute(input []byte, marks []RedactionMark) []byte {
	var buf bytes.Buffer
	buf.Grow(len(input))
	cursor := 0
	for _, m := range marks {
		if m.Offset > cursor {
			buf.Write(input[cursor:m.Offset])
		}
		fmt.Fprintf(&buf, "[REDACTED:%s:%d]", m.Type, m.Length)
		cursor = m.Offset + m.Length
	}
	if cursor < len(input) {
		buf.Write(input[cursor:])
	}
	return buf.Bytes()
}
