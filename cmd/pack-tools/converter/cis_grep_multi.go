// CIS multi-cmd grep alternation 자동 변환 — E-1 epic G15 (6.2.2.3 auditd.conf).
//
// audit text 패턴: 2+ `# grep -Pi -- '...alternation...' <path>` 명령 + 각 expected가
// `KEY = <alt1|alt2|...>` placeholder. 모든 grep이 non-empty 출력이면 PASS.
//
// 본 합성기는 /etc/audit/auditd.conf 경로 명시 시점에만 narrow 적용 (false positive 회피).
// 향후 다른 multi-cmd grep alternation 패턴 추가 시 본 파일에 확장.
//
// 잠재 변환률: 6.2.2.3 1건 → +0.3%p (312 기준).
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G15 + §4 E-1.

package converter

import (
	"fmt"
	"regexp"
	"strings"
)

// regexpGrepPiAuditdLine은 `# grep -Pi -- '...'` 명령 라인을 감지합니다.
// "-Pi -- '...'" prefix로 narrow — 다른 grep variant 잘못 매칭 회피.
var regexpGrepPiAuditdLine = regexp.MustCompile(`^#\s+(grep\s+-Pi\s+--\s+.+)$`)

// extractAuditdGrepCmds는 audit text에서 grep -Pi 명령들을 추출합니다.
//
// path가 다음 라인으로 wrap된 경우(`# grep -Pi -- '...'\n/etc/audit/auditd.conf`)
// continuation으로 join. 모든 cmd가 /etc/audit/auditd.conf 경로 포함해야 합성 대상.
//
// 반환:
//   - cmds: 완전한 grep 명령 슬라이스(path join 후)
//   - ok: 2+ cmd + 모든 cmd가 auditd.conf 경로 포함 시 true
func extractAuditdGrepCmds(audit string) ([]string, bool) {
	lines := strings.Split(audit, "\n")
	var cmds []string
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		m := regexpGrepPiAuditdLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		cmd := m[1]
		// path가 명령에 미포함이고 다음 라인이 path이면 join.
		if !strings.Contains(cmd, "/etc/") && i+1 < len(lines) {
			nxt := strings.TrimSpace(lines[i+1])
			if strings.HasPrefix(nxt, "/") {
				cmd = cmd + " " + nxt
			}
		}
		cmds = append(cmds, cmd)
	}
	if len(cmds) < 2 {
		return nil, false
	}
	for _, c := range cmds {
		if !strings.Contains(c, "/etc/audit/auditd.conf") {
			return nil, false
		}
	}
	return cmds, true
}

// isMultiGrepAuditdAuditText는 multi-cmd auditd.conf grep 합성 대상인지 판정합니다.
func isMultiGrepAuditdAuditText(audit string) bool {
	_, ok := extractAuditdGrepCmds(audit)
	return ok
}

// synthesizeMultiGrepAuditd는 추출한 grep 명령들을 모두 실행한 뒤 non-empty 검사 합성 bash를 생성합니다.
//
// 각 grep이 non-empty 출력 = audit rule 매칭 = PASS. 모든 grep 통과해야 최종 PASS,
// 1개라도 empty 시 FAIL + miss-N diagnostic. cis_auditctl.go와 동일 마커 사용
// (** PASS **/** FAIL ** — selftest skeleton 자동 호환).
func synthesizeMultiGrepAuditd(audit string) (string, bool) {
	cmds, ok := extractAuditdGrepCmds(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString("missing=0\n")
	for i, c := range cmds {
		fmt.Fprintf(&sb, "out_%d=$(%s 2>/dev/null)\n", i, c)
		fmt.Fprintf(&sb, "[ -n \"$out_%d\" ] || { printf 'miss-%d: %%s\\n' %q; missing=$((missing+1)); }\n",
			i, i, c)
	}
	sb.WriteString("if [ \"$missing\" -eq 0 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi")
	return sb.String(), true
}
