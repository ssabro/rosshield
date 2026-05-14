// CIS grub.cfg multi-line verify 자동 변환 — G14 (1.4.1, 90% 도달 경로).
//
// audit text 패턴: 2+ `# (grep|awk) ... /boot/grub/grub.cfg` cmd + 각 expected 1줄 (placeholder
// `<username>` 포함). placeholder 제거 후 각 substring 토큰을 cmd 출력에 모두 매칭이면 PASS.
//
// 잠재 변환률: 1.4.1 1건 → +0.3%p (312 기준).
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G14.

package converter

import (
	"fmt"
	"regexp"
	"strings"
)

// regexpGrubCfgCmd는 `# (grep|awk) ... /boot/grub/grub.cfg` 명령 라인 감지.
var regexpGrubCfgCmd = regexp.MustCompile(`^#\s+((grep|awk)\s+.+/boot/grub/grub\.cfg.*)$`)

// grubCheck는 단일 cmd × placeholder-stripped 토큰 슬라이스.
type grubCheck struct {
	cmd    string
	tokens []string // placeholder `<...>` 사이 substring 토큰들
}

// extractGrubChecks는 audit text에서 cmd × expected를 추출 + placeholder 제거.
//
// 인식 조건:
//   - 2+ grub.cfg cmd
//   - 각 cmd 직후 non-empty + non-heading 라인이 expected
//
// expected의 `<placeholder>` 부분을 split → 각 substring 토큰 슬라이스. 모든 cmd 추출 성공.
func extractGrubChecks(audit string) ([]grubCheck, bool) {
	lines := strings.Split(audit, "\n")
	var checks []grubCheck
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		m := regexpGrubCfgCmd.FindStringSubmatch(line)
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
		// placeholder `<...>` 위치를 기준으로 split → 각 substring 토큰.
		tokens := splitByPlaceholder(exp)
		// 빈 토큰 제거 (placeholder가 라인 시작/끝에 있을 때).
		var nonEmpty []string
		for _, t := range tokens {
			t = strings.TrimSpace(t)
			if t != "" {
				nonEmpty = append(nonEmpty, t)
			}
		}
		if len(nonEmpty) == 0 {
			return nil, false
		}
		checks = append(checks, grubCheck{cmd: m[1], tokens: nonEmpty})
	}
	if len(checks) < 2 {
		return nil, false
	}
	return checks, true
}

// splitByPlaceholder는 expected 라인을 `<...>` placeholder 기준으로 split.
// e.g. `set superusers="<username>"` → [`set superusers="`, `"`].
func splitByPlaceholder(s string) []string {
	out := []string{}
	for {
		i := strings.Index(s, "<")
		if i < 0 {
			out = append(out, s)
			break
		}
		j := strings.Index(s[i:], ">")
		if j < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i])
		s = s[i+j+1:]
	}
	return out
}

// isGrubCfgAuditText는 G14 합성 대상인지 판정.
func isGrubCfgAuditText(audit string) bool {
	_, ok := extractGrubChecks(audit)
	return ok
}

// synthesizeGrubCfg는 cmd 실행 + 각 token substring 매칭 합성 bash 생성.
//
// 각 cmd 출력에 모든 token이 substring 포함이면 cmd PASS. 모든 cmd PASS이면 최종 PASS.
// missing 카운트 0이면 PASS.
func synthesizeGrubCfg(audit string) (string, bool) {
	checks, ok := extractGrubChecks(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString("missing=0\n")
	for i, c := range checks {
		fmt.Fprintf(&sb, "out_%d=$(%s 2>/dev/null)\n", i, c.cmd)
		for _, tok := range c.tokens {
			fmt.Fprintf(&sb,
				"printf '%%s' \"$out_%d\" | grep -qF -- %q || { printf 'miss-%d: token %%s\\n' %q; missing=$((missing+1)); }\n",
				i, tok, i, tok)
		}
	}
	sb.WriteString("if [ \"$missing\" -eq 0 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi")
	return sb.String(), true
}
