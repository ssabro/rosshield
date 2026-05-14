// CIS passwd/group awk + alternative 자동 변환 — G16 (5.4.2.2 + 5.4.2.3 + 5.4.2.4).
//
// 두 mode 통합:
//   - exactRootMode (5.4.2.2 + 5.4.2.3): `# awk -F: '...' /etc/passwd|/etc/group` cmd +
//     단일 `root:N` expected. cmd 출력 trim 후 정확 매칭 → PASS.
//   - alternationMode (5.4.2.4): `# passwd -S root | awk '...'` cmd + 2+ User: "..." Password
//     is status: X expected (separator `- OR -`). cmd 출력에 alternation 1+ substring 매칭 → PASS.
//
// 잠재 변환률: 3건 → +1.0%p (312 기준). 90% 도달 경로의 핵심 epic.
//
// 참조: docs/design/notes/cis-nomarker-31-analysis.md §3 G16 + §4 후속 epic 후보.

package converter

import (
	"fmt"
	"regexp"
	"strings"
)

// regexpPasswdAwkCmd는 `# awk -F: ...` 명령 첫 라인 감지(단일 라인 또는 multi-line wrap의
// 첫 라인). /etc/passwd|/etc/group path 검증은 join 후 cmd에서 수행.
var regexpPasswdAwkCmd = regexp.MustCompile(`^#\s+(awk\s+-F:.+)$`)

// regexpPasswdSCmd는 `# passwd -S root | awk '...'` 명령 감지.
var regexpPasswdSCmd = regexp.MustCompile(`^#\s+(passwd\s+-S\s+\S+\s*\|\s*awk\s+.+)$`)

// regexpRootColon는 expected 라인이 정확히 `root:N` (N: 정수) 형태인지 검사.
var regexpRootColon = regexp.MustCompile(`^root:\d+\s*$`)

// regexpUserPasswordStatus는 alternation expected 라인 (5.4.2.4) 감지.
var regexpUserPasswordStatus = regexp.MustCompile(`^User:\s*"\S+"\s+Password\s+is\s+status:\s+\S+\s*$`)

// passwdMode는 두 합성 분기.
type passwdMode int

const (
	passwdExactRootMode   passwdMode = iota // 5.4.2.2/.3 — 단일 root:N exact
	passwdAlternationMode                   // 5.4.2.4 — 2+ alternation substring
)

// passwdCheck는 단일 cmd × mode × expecteds 묶음.
type passwdCheck struct {
	mode      passwdMode
	cmd       string
	expecteds []string
}

// joinPasswdCmdContinuation은 cmd 라인이 multi-line wrap된 경우 다음 라인을 join.
// 휴리스틱: cmd 안 single quote 카운트가 홀수면 unbalanced → 다음 라인 join 필요.
// 5.4.2.2는 `{print\n$1":"$4}'` 형식의 wrap.
func joinPasswdCmdContinuation(lines []string, startIdx int, initialCmd string) (string, int) {
	cmd := initialCmd
	idx := startIdx
	for idx+1 < len(lines) {
		if strings.Count(cmd, "'")%2 == 0 {
			break
		}
		next := strings.TrimSpace(lines[idx+1])
		if next == "" || strings.HasPrefix(next, "#") {
			break
		}
		cmd = cmd + " " + next
		idx++
	}
	return cmd, idx
}

// extractPasswdCheck는 audit text에서 cmd + mode + expecteds 추출.
//
// 인식 조건:
//   - awk -F: + /etc/passwd|/etc/group cmd → exactRootMode + 단일 root:N expected
//   - passwd -S + awk cmd → alternationMode + 2+ User: "..." Password is status: X expected
//
// "Note" / "•" / "Verify" 시작 라인은 종료/skip. separator (`-OR-`/`- OR -`)는 skip.
func extractPasswdCheck(audit string) (passwdCheck, bool) {
	lines := strings.Split(audit, "\n")
	var pc passwdCheck
	cmdIdx := -1
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if pc.cmd != "" {
			break
		}
		if m := regexpPasswdAwkCmd.FindStringSubmatch(line); m != nil {
			joined, idx := joinPasswdCmdContinuation(lines, i, m[1])
			// path 검증 — join 후 /etc/passwd 또는 /etc/group 포함이어야 G16 매칭.
			if !strings.Contains(joined, "/etc/passwd") && !strings.Contains(joined, "/etc/group") {
				continue
			}
			pc.cmd = joined
			pc.mode = passwdExactRootMode
			cmdIdx = idx
		} else if m := regexpPasswdSCmd.FindStringSubmatch(line); m != nil {
			pc.cmd, cmdIdx = joinPasswdCmdContinuation(lines, i, m[1])
			pc.mode = passwdAlternationMode
		}
	}
	if pc.cmd == "" {
		return pc, false
	}
	for i := cmdIdx + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Note") || strings.HasPrefix(line, "•") {
			break
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		if regexpOrSeparatorStat.MatchString(line) {
			continue
		}
		if strings.HasPrefix(line, "Verify") {
			continue
		}
		switch pc.mode {
		case passwdExactRootMode:
			if regexpRootColon.MatchString(line) {
				pc.expecteds = append(pc.expecteds, line)
				// 첫 매치만 — 추가 line은 cover X(audit text가 단일 expected 형식).
			}
		case passwdAlternationMode:
			if regexpUserPasswordStatus.MatchString(line) {
				pc.expecteds = append(pc.expecteds, line)
			}
		}
	}
	if len(pc.expecteds) == 0 {
		return pc, false
	}
	if pc.mode == passwdAlternationMode && len(pc.expecteds) < 2 {
		return pc, false
	}
	return pc, true
}

// isPasswdAwkAuditText는 G16 합성 대상인지 판정.
func isPasswdAwkAuditText(audit string) bool {
	_, ok := extractPasswdCheck(audit)
	return ok
}

// synthesizePasswdAwk는 mode별 합성 bash 생성.
//
// exactRootMode: cmd 출력 trim(awk '{$1=$1};1') 후 expected와 정확 매칭 → PASS, 그 외 FAIL.
// alternationMode: cmd 출력에 alternation 1+ substring 매칭 → PASS.
func synthesizePasswdAwk(audit string) (string, bool) {
	pc, ok := extractPasswdCheck(audit)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "out=$(%s 2>/dev/null)\n", pc.cmd)
	switch pc.mode {
	case passwdExactRootMode:
		sb.WriteString("trimmed=$(printf '%s' \"$out\" | awk '{$1=$1};1')\n")
		fmt.Fprintf(&sb,
			"if [ \"$trimmed\" = %q ]; then printf '** PASS **\\n'; else printf 'fail: got=%%s\\n' \"$trimmed\"; printf '** FAIL **\\n'; fi",
			pc.expecteds[0])
	case passwdAlternationMode:
		sb.WriteString("found=0\n")
		sb.WriteString("for token in")
		for _, e := range pc.expecteds {
			fmt.Fprintf(&sb, " %q", e)
		}
		sb.WriteString("; do\n")
		sb.WriteString("  printf '%s' \"$out\" | grep -qF -- \"$token\" && { found=1; break; }\n")
		sb.WriteString("done\n")
		sb.WriteString("if [ \"$found\" -eq 1 ]; then printf '** PASS **\\n'; else printf 'fail: %s\\n' \"$out\"; printf '** FAIL **\\n'; fi")
	}
	return sb.String(), true
}
