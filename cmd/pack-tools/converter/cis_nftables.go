// CIS nftables hook 검증 자동 변환 — E-2 epic G1 (4.3.5 + 4.3.8).
//
// audit text 패턴: 3+ `# nft list ruleset | grep 'hook X'` 명령 + 각 명령 직후 expected
// 1줄 (`type filter hook X priority 0;` 또는 `... policy drop;`).
//
// 합성: 각 cmd 실행 → 출력에 expected substring 매칭이면 PASS, 모두 통과해야 최종 PASS.
// expected가 audit text에서 파생되므로 4.3.5(존재만 검증) vs 4.3.8(policy 검증) 자동 분기.
//
// 잠재 변환률: 4.3.5 + 4.3.8 = 2건 → +0.6%p (312 기준).
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G1 + §4 E-2.

package converter

import (
	"fmt"
	"regexp"
	"strings"
)

// regexpNftListRulesetCmd는 `# nft list ruleset | grep '...'` 명령 라인 감지.
var regexpNftListRulesetCmd = regexp.MustCompile(`^#\s+(nft\s+list\s+ruleset\s+\|\s+grep\s+.+)$`)

// regexpNftListTablesCmd는 `# nft list tables` 단일 명령 라인 감지 (G2 4.3.4).
var regexpNftListTablesCmd = regexp.MustCompile(`^#\s+(nft\s+list\s+tables)\s*$`)

// regexpReturnShouldInclude는 "Return should include" phrase 검출.
var regexpReturnShouldInclude = regexp.MustCompile(`(?i)Return\s+should\s+include`)

// nftHookCheck는 단일 cmd × expected 쌍입니다.
type nftHookCheck struct {
	cmd, expected string
}

// extractNftHookChecks는 audit text에서 nft list ruleset cmd + 각 expected를 추출합니다.
//
// 인식 조건:
//   - 3+ `# nft list ruleset | grep '...'` 명령
//   - 각 명령 직후 non-empty + non-heading 라인이 expected
//
// expected가 1개라도 비어있으면 ok=false. heading은 "Run", "#" 시작.
func extractNftHookChecks(audit string) ([]nftHookCheck, bool) {
	lines := strings.Split(audit, "\n")
	var checks []nftHookCheck
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		m := regexpNftListRulesetCmd.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var exp string
		for j := i + 1; j < len(lines); j++ {
			s := strings.TrimSpace(lines[j])
			if s == "" {
				continue
			}
			if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "Run") {
				break
			}
			exp = s
			break
		}
		if exp == "" {
			return nil, false
		}
		checks = append(checks, nftHookCheck{cmd: m[1], expected: exp})
	}
	if len(checks) < 3 {
		return nil, false
	}
	return checks, true
}

// isNftHookAuditText는 G1 합성 대상인지 판정합니다.
func isNftHookAuditText(audit string) bool {
	_, ok := extractNftHookChecks(audit)
	return ok
}

// synthesizeNftHook는 cmd 실행 + expected substring 매칭 합성 bash를 생성합니다.
//
// 각 cmd 실행 → grep -qF로 expected substring 검사 → 모두 통과하면 PASS, 1+ 미일치 시
// FAIL + miss-N diagnostic. expected는 audit text에서 직접 파생 — chain 존재만 검증
// (4.3.5) 또는 policy drop 포함 검증 (4.3.8) 자동 분기.
func synthesizeNftHook(audit string) (string, bool) {
	checks, ok := extractNftHookChecks(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString("missing=0\n")
	for i, c := range checks {
		fmt.Fprintf(&sb, "out_%d=$(%s 2>/dev/null)\n", i, c.cmd)
		fmt.Fprintf(&sb,
			"printf '%%s' \"$out_%d\" | grep -qF -- %q || { printf 'miss-%d: expected %%s\\n' %q; missing=$((missing+1)); }\n",
			i, c.expected, i, c.expected)
	}
	sb.WriteString("if [ \"$missing\" -eq 0 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi")
	return sb.String(), true
}

// extractNftListTablesExpected는 G2 (4.3.4) audit text에서 단일 cmd + expected 라인을 추출합니다.
//
// 인식 조건:
//   - 단일 `# nft list tables` 명령 (정확)
//   - "Return should include" phrase 존재
//   - phrase 이후 첫 non-empty + non-heading 라인이 expected (`table inet filter` 등)
//
// "Example:" / "Note:" / 빈 라인은 skip하고 다음 의미있는 라인 추출.
func extractNftListTablesExpected(audit string) (cmd, expected string, ok bool) {
	lines := strings.Split(audit, "\n")
	var cmdFound bool
	phraseIdx := -1
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if !cmdFound {
			if m := regexpNftListTablesCmd.FindStringSubmatch(line); m != nil {
				cmd = m[1]
				cmdFound = true
			}
		}
		if phraseIdx < 0 && regexpReturnShouldInclude.MatchString(line) {
			phraseIdx = i
		}
	}
	if !cmdFound || phraseIdx < 0 {
		return "", "", false
	}
	for i := phraseIdx + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Example:") || strings.HasPrefix(line, "Note:") {
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "Run") {
			break
		}
		expected = line
		break
	}
	if expected == "" {
		return "", "", false
	}
	return cmd, expected, true
}

// isNftListTablesAuditText는 G2 합성 대상인지 판정합니다.
func isNftListTablesAuditText(audit string) bool {
	_, _, ok := extractNftListTablesExpected(audit)
	return ok
}

// synthesizeNftListTables는 단일 cmd 실행 + expected substring 매칭 합성 bash 생성.
//
// 4.3.4 특화: `nft list tables` 출력에 expected substring 포함이면 PASS, 미포함 FAIL.
// "should include" semantic이라 정확 라인 매칭 X — substring으로 충분.
func synthesizeNftListTables(audit string) (string, bool) {
	cmd, expected, ok := extractNftListTablesExpected(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(%s 2>/dev/null)\n", cmd)
	fmt.Fprintf(&sb,
		"printf '%%s' \"$out\" | grep -qF -- %q && printf '** PASS **\\n' || { printf 'miss: expected %%s\\n' %q; printf '** FAIL **\\n'; }",
		expected, expected)
	return sb.String(), true
}
