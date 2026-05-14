// CIS gsettings get scalar 자동 변환 — E-1 epic Stage 1 (G7 sub-set, true/false 정확 매칭).
//
// audit text가 `# gsettings get <schema> <key>` 명령들 + 각 명령의 다음 라인이 정확히
// `true` 또는 `false`로만 구성된 경우 합성 대상. uint32 N + threshold 비교(1.7.4)는
// 별 변형 — 본 합성 함수는 boolean만 cover (false positive 회피).
//
// 잠재 변환률: 1.7.6 + 1.7.8 = 2건 → +0.6%p (312 기준).
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G7 + §4 E-1 후보.

package converter

import (
	"fmt"
	"regexp"
	"strings"
)

// regexpGsettingsGetCmd는 `# gsettings get <schema> <key>` 명령 라인을 감지합니다.
// schema·key 모두 dot-segment 식별자(`org.gnome.desktop.session` 등).
var regexpGsettingsGetCmd = regexp.MustCompile(`^#\s+gsettings\s+get\s+(\S+)\s+(\S+)\s*$`)

// regexpGsettingsBool은 expected 라인이 정확히 `true` 또는 `false`인지 검사합니다.
var regexpGsettingsBool = regexp.MustCompile(`^(true|false)\s*$`)

// regexpGsettingsUint32는 expected 라인이 `uint32 N` 형태인지 검사합니다(N: 비음수 정수).
var regexpGsettingsUint32 = regexp.MustCompile(`^uint32\s+(\d+)\s*$`)

// gsettingsCheck는 단일 `gsettings get` cmd × expected boolean 쌍입니다.
type gsettingsCheck struct {
	schema, key, expected string
}

// extractGsettingsBoolChecks는 audit text에서 (schema, key, expected) triples를 추출합니다.
//
// 인식 조건:
//   - 1+ `# gsettings get ...` 명령 라인 존재
//   - 각 명령 직후 라인이 정확히 `true` 또는 `false` (whitespace 무시)
//   - 명령 1개라도 expected가 boolean이 아니면 ok=false (전체 보류, false positive 회피)
//
// 반환 ok=false 시 다른 합성 분기(또는 degraded fallback)에 위임.
func extractGsettingsBoolChecks(audit string) ([]gsettingsCheck, bool) {
	lines := strings.Split(audit, "\n")
	var checks []gsettingsCheck
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		m := regexpGsettingsGetCmd.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// 명령 직후 라인이 expected — 빈 라인 건너뛰지 않음(audit 형식 단순).
		if i+1 >= len(lines) {
			return nil, false
		}
		next := strings.TrimSpace(lines[i+1])
		if !regexpGsettingsBool.MatchString(next) {
			return nil, false
		}
		checks = append(checks, gsettingsCheck{schema: m[1], key: m[2], expected: next})
	}
	if len(checks) == 0 {
		return nil, false
	}
	return checks, true
}

// isGsettingsBoolAuditText는 G7-bool 합성 대상인지 판정합니다.
func isGsettingsBoolAuditText(audit string) bool {
	_, ok := extractGsettingsBoolChecks(audit)
	return ok
}

// synthesizeGsettingsBool은 `gsettings get` boolean 정확 매칭 합성 bash를 생성합니다.
//
// 구조:
//
//	missing=0
//	val=$(gsettings get <schema> <key> 2>/dev/null)
//	[ "$val" = "<expected>" ] || { echo 'mismatch: ...'; missing=$((missing+1)); }
//	... (각 cmd 반복) ...
//	if [ "$missing" -eq 0 ]; then printf '** PASS **\n'; else printf '** FAIL **\n'; fi
//
// 모든 cmd × expected가 매칭해야 PASS, 1개라도 미일치 시 FAIL + diagnostic 출력.
// auditctl 합성기와 동일 마커 (`** PASS **`/`** FAIL **`) 사용 — selftest skeleton 자동 호환.
func synthesizeGsettingsBool(audit string) (string, bool) {
	checks, ok := extractGsettingsBoolChecks(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString("missing=0\n")
	for _, c := range checks {
		fmt.Fprintf(&sb, "val=$(gsettings get %s %s 2>/dev/null)\n", c.schema, c.key)
		fmt.Fprintf(&sb, "[ \"$val\" = %q ] || { printf 'mismatch: %s.%s expected %s got %%s\\n' \"$val\"; missing=$((missing+1)); }\n",
			c.expected, c.schema, c.key, c.expected)
	}
	sb.WriteString("if [ \"$missing\" -eq 0 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi")
	return sb.String(), true
}

// gsettingsUint32Check는 단일 cmd × `uint32 N` 쌍입니다 (E-1 G7 1.7.4).
type gsettingsUint32Check struct {
	schema, key string
	threshold   int // audit text의 uint32 N — CIS "N seconds or less" 의미
}

// extractGsettingsUint32Checks는 audit text에서 (schema, key, threshold) triples 추출.
//
// 인식 조건:
//   - 1+ `# gsettings get ...` 명령 + 다음 라인이 `uint32 N` (정수)
//   - 명령 1개라도 expected가 uint32 N이 아니면 ok=false (boolean과 mix 회피).
//
// audit text의 uint32 N을 threshold로 직접 사용 — CIS "N seconds or less" 의미. Notes의
// "not 0" 등 추가 가드는 cover 안 함(false PASS 위험 vs Notes 파싱 복잡도 트레이드오프,
// 운영자 manual confirm 책임).
func extractGsettingsUint32Checks(audit string) ([]gsettingsUint32Check, bool) {
	lines := strings.Split(audit, "\n")
	var checks []gsettingsUint32Check
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		m := regexpGsettingsGetCmd.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if i+1 >= len(lines) {
			return nil, false
		}
		next := strings.TrimSpace(lines[i+1])
		um := regexpGsettingsUint32.FindStringSubmatch(next)
		if um == nil {
			return nil, false
		}
		var threshold int
		if _, err := fmt.Sscanf(um[1], "%d", &threshold); err != nil {
			return nil, false
		}
		checks = append(checks, gsettingsUint32Check{schema: m[1], key: m[2], threshold: threshold})
	}
	if len(checks) == 0 {
		return nil, false
	}
	return checks, true
}

// isGsettingsUint32AuditText는 G7-uint32 합성 대상인지 판정.
func isGsettingsUint32AuditText(audit string) bool {
	_, ok := extractGsettingsUint32Checks(audit)
	return ok
}

// synthesizeGsettingsUint32는 `gsettings get` uint32 + threshold 비교 합성 bash를 생성.
//
// 각 cmd 출력에서 `uint32 ` prefix 제거 → 정수 비교 (`-le threshold`). 비정수 또는 미설정 시
// FAIL. 모든 cmd 통과해야 PASS, diagnostic에 schema.key + got vs threshold 명시.
func synthesizeGsettingsUint32(audit string) (string, bool) {
	checks, ok := extractGsettingsUint32Checks(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString("missing=0\n")
	for _, c := range checks {
		fmt.Fprintf(&sb, "raw=$(gsettings get %s %s 2>/dev/null)\n", c.schema, c.key)
		// "uint32 N" → N 추출. 형식 미일치 시 비교 실패 → missing 증가.
		sb.WriteString("val=${raw#uint32 }\n")
		sb.WriteString("case \"$val\" in\n")
		sb.WriteString("  ''|*[!0-9]*) ")
		fmt.Fprintf(&sb, "printf 'invalid: %s.%s got %%s\\n' \"$raw\"; missing=$((missing+1)) ;;\n", c.schema, c.key)
		fmt.Fprintf(&sb, "  *) [ \"$val\" -le %d ] || { printf 'over: %s.%s got %%s want le %d\\n' \"$val\"; missing=$((missing+1)); } ;;\n",
			c.threshold, c.schema, c.key, c.threshold)
		sb.WriteString("esac\n")
	}
	sb.WriteString("if [ \"$missing\" -eq 0 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi")
	return sb.String(), true
}
