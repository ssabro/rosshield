// CIS iptables chain policy 검증 자동 변환 — E-2 epic G4 부분 cover (4.4.2.1).
//
// audit text 패턴 (4.4.2.1): 단일 `# iptables -L` 명령 + 3+ "Chain X (policy Y)" expected.
// 합성: cmd 실행 → 각 expected 라인을 cmd 출력에 substring 매칭 → 모두 통과 PASS.
//
// 4.4.2.2 (multi-line table verify with `iptables -L X -v -n` × 2 cmd, line order + column
// alignment) 는 본 합성기로 cover 안 됨 — 별 epic.
//
// audit text expected가 "DROP" 명시이면 운영 환경이 "REJECT"인 경우 false FAIL — 운영자
// audit text 의미(DROP or REJECT 둘 다 허용) 알고 manual 확인. design doc 정확도 ~85% 의도.
//
// 잠재 변환률: 4.4.2.1 1건 → +0.3%p (312 기준).
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G4 + §4 E-2.

package converter

import (
	"fmt"
	"regexp"
	"strings"
)

// regexpIptablesListCmd는 단일 `# iptables -L` 명령 라인 감지 (정확 매칭, -v/-n 옵션 부재).
var regexpIptablesListCmd = regexp.MustCompile(`^#\s+(iptables\s+-L)\s*$`)

// regexpIptablesChainPolicy는 expected 라인이 `Chain X (policy Y)` 형태인지 검사.
var regexpIptablesChainPolicy = regexp.MustCompile(`^Chain\s+\w+\s+\(policy\s+\w+\)`)

// extractIptablesChainExpecteds: 단일 `# iptables -L` cmd + 3+ "Chain X (policy Y)" expected.
//
// 인식 조건:
//   - 단일 `# iptables -L` 명령 (-v/-n 옵션 미포함, 4.4.2.1 narrow)
//   - 명령 이후 3+ "Chain X (policy Y)" 라인
//
// 4.4.2.2 (`iptables -L INPUT -v -n` 형식)는 본 패턴 미매칭 — 별 합성기 후속.
func extractIptablesChainExpecteds(audit string) (cmd string, expecteds []string, ok bool) {
	lines := strings.Split(audit, "\n")
	cmdIdx := -1
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if cmd == "" {
			if m := regexpIptablesListCmd.FindStringSubmatch(line); m != nil {
				cmd = m[1]
				cmdIdx = i
			}
		}
	}
	if cmd == "" {
		return "", nil, false
	}
	for i := cmdIdx + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "Run") {
			break
		}
		if regexpIptablesChainPolicy.MatchString(line) {
			expecteds = append(expecteds, line)
			continue
		}
		// non-Chain 라인 만나면 종료(다른 expected 형식 미지원).
		break
	}
	if len(expecteds) < 3 {
		return "", nil, false
	}
	return cmd, expecteds, true
}

// isIptablesChainPolicyAuditText는 G4 (4.4.2.1) 합성 대상인지 판정.
func isIptablesChainPolicyAuditText(audit string) bool {
	_, _, ok := extractIptablesChainExpecteds(audit)
	return ok
}

// synthesizeIptablesChainPolicy는 cmd 실행 + expected substring 매칭 합성 bash 생성.
//
// 각 expected("Chain X (policy DROP)" 등)를 cmd 출력에 grep -qF substring 검사.
// 모두 통과해야 PASS, 1+ 미일치 시 FAIL + miss-N diagnostic. audit text의 DROP 명시
// false FAIL(REJECT 환경)은 운영자 manual 확인.
func synthesizeIptablesChainPolicy(audit string) (string, bool) {
	cmd, expecteds, ok := extractIptablesChainExpecteds(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(%s 2>/dev/null)\n", cmd)
	sb.WriteString("missing=0\n")
	for i, exp := range expecteds {
		fmt.Fprintf(&sb,
			"printf '%%s' \"$out\" | grep -qF -- %q || { printf 'miss-%d: %%s\\n' %q; missing=$((missing+1)); }\n",
			exp, i, exp)
	}
	sb.WriteString("if [ \"$missing\" -eq 0 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi")
	return sb.String(), true
}
