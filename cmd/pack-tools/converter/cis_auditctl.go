// CIS 6.2.3.x auditd rules 자동 변환 — Stage 1 (인식기·추출기).
//
// 본 파일은 cis.go의 합성 dispatch에서 호출되는 auditctl 전용 인식기·추출기를 분리합니다.
// cis.go가 800줄 한계 임박이라 D-N-8 결정에 따라 신규 파일로 분리. 향후 6.2.4.x 확장
// 시 동일 파일에 추가.
//
// 6.2.3.x 21건 중 신규 cover 19건(6.2.3.20 이미 합성 + 6.2.3.21 Manual). 합성 함수
// (synthesizeAuditctlMatch + normalize)는 Stage 2 후속.
//
// 참조: docs/design/notes/cis-6-2-3-auditd-design.md §6.1 Stage 1.

package converter

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// regexpAuditctlList는 audit text 어디든 `auditctl -l` 명령이 포함되어 있는지 감지합니다.
// 6.2.3.x running 검증의 시그니처 — 합성 대상 1차 게이트. 7.2.x 등 다른 grep verify
// 패턴(auditctl 미사용)을 negative로 거름. prefix 다양(`# auditctl -l`, `{`, `&& auditctl -l`,
// `[ -n "${UID_MIN}" ] && auditctl -l |` 등)이라 단어 경계만 검사.
var regexpAuditctlList = regexp.MustCompile(`\bauditctl\s+-l\b`)

// regexpAuditctlVerifyMatches는 "Verify the output (matches|includes)" 또는
// "Verify output of (matches|includes)" phrase를 검출합니다(6.2.3.4 변형 cover).
// 6.2.3.x audit text는 on-disk + running 각각 1회씩 등장 → 2회 매칭 기대.
var regexpAuditctlVerifyMatches = regexp.MustCompile(`(?i)Verify\s+(the\s+)?output\s+(of\s+)?(matches|includes)\b`)

// regexpAuditctlRule은 expected 라인이 audit rule 형식인지 검증합니다.
//
//	-w /path -p X -k key                         (file watch)
//	-a always,exit -F arch=... -S ... -k key     (syscall rule)
var regexpAuditctlRule = regexp.MustCompile(`^(-w\s+/\S+|-a\s+(always|never),(exit|entry|user|task|exclude)\b)`)

// isAuditctlAuditText는 audit text가 6.2.3.x auditd 합성 대상인지 판정합니다.
//
// 3 조건 AND:
//  1. `auditctl -l` 명령 포함 (running 검증 존재)
//  2. "Verify the output matches/includes" phrase 1+회
//  3. audit rule 형식 라인 1+ 존재 (`-w /...` 또는 `-a always,exit ...`)
//
// 7.2.x grep verify 등 auditctl 미사용 패턴은 negative.
func isAuditctlAuditText(audit string) bool {
	if !regexpAuditctlList.MatchString(audit) {
		return false
	}
	if !regexpAuditctlVerifyMatches.MatchString(audit) {
		return false
	}
	for _, raw := range strings.Split(audit, "\n") {
		if regexpAuditctlRule.MatchString(strings.TrimSpace(raw)) {
			return true
		}
	}
	return false
}

// extractAuditctlExpectedRules는 audit text에서 on-disk + running expected 라인을 추출합니다.
//
// audit text 구조:
//
//	On disk configuration
//	... # cmd ...
//	Verify the output matches:
//	  <rules>           ← block 1 (on-disk)
//	Running configuration
//	... # auditctl -l ...
//	Verify the output matches:
//	  <rules>           ← block 2 (running)
//
// 두 phrase 직후 라인부터 다음 빈 줄·heading·"#" 시작 라인 직전까지 audit rule 라인만 수집.
// 멀티라인 wrap은 Stage 2 normalize에서 처리(본 함수는 raw 라인만 추출).
//
// 반환:
//   - onDisk, running []string: 각각 expected 라인 슬라이스
//   - ok bool: 두 block 모두 1+ 라인 추출 시 true
func extractAuditctlExpectedRules(audit string) (onDisk, running []string, ok bool) {
	indexes := regexpAuditctlVerifyMatches.FindAllStringIndex(audit, -1)
	if len(indexes) < 2 {
		return nil, nil, false
	}
	blocks := make([]string, 0, len(indexes))
	for i, idx := range indexes {
		start := idx[1]
		// trailing colon ":" skip
		if start < len(audit) && audit[start] == ':' {
			start++
		}
		end := len(audit)
		if i+1 < len(indexes) {
			end = indexes[i+1][0]
		}
		blocks = append(blocks, audit[start:end])
	}
	onDisk = collectAuditRuleLines(blocks[0])
	running = collectAuditRuleLines(blocks[1])
	if len(onDisk) == 0 || len(running) == 0 {
		return nil, nil, false
	}
	return onDisk, running, true
}

// regexpSyscallList은 `-S syscall1,syscall2,...` 토큰 매칭. syscall 이름은 [a-zA-Z_] + 숫자.
var regexpSyscallList = regexp.MustCompile(` -S [a-zA-Z_][a-zA-Z0-9_,]*`)

// regexpKeyShort는 `-k <name>` 토큰 매칭(short form). running config는 `-F key=<name>` (long form) 사용.
var regexpKeyShort = regexp.MustCompile(` -k ([!-~]+)`)

// regexpAuidNotEq은 `-F auid!=N` (-1, 4294967295 등) 토큰 매칭. unset/-1/4294967295는 동치 — unset로 통일.
var regexpAuidNotEq = regexp.MustCompile(` -F auid!=-?[0-9]+`)

// regexpAuidGE는 `-F auid>=<N>` 토큰 매칭. UID_MIN 환경 변수 치환을 위해 N을 placeholder로.
var regexpAuidGE = regexp.MustCompile(` -F auid>=([0-9]+)`)

// normalizeAuditctlRule은 audit rule 라인의 표기 차이를 정규화합니다.
//
// 4 변환:
//  1. `-S syscall1,syscall2,...` syscall set을 alphabet sort (running config에서 순서 다른 경우 cover, D26 §3.3 6.2.3.{4,5,7,9,13})
//  2. `-k name` → `-F key=name` (on-disk short form ↔ running long form 통일)
//  3. `-F auid!=-1` / `-F auid!=4294967295` → `-F auid!=unset` (CIS 표준 표현)
//  4. `-F auid>=1000` → `-F auid>=__UID_MIN__` (환경 UID_MIN ≠ 1000 false FAIL 회피, 런타임 sed 치환)
//
// 입력은 단일 라인(continuation join 후 — Stage 1 collectAuditRuleLines가 처리).
// 출력은 정규화된 단일 라인. file watch(`-w /path -p X -k key`)는 syscall 변환 X, key만 통일.
func normalizeAuditctlRule(rule string) string {
	out := rule
	// 1. syscall set 정렬
	out = regexpSyscallList.ReplaceAllStringFunc(out, func(match string) string {
		// match = " -S syscall1,syscall2,..."
		const prefix = " -S "
		list := strings.TrimPrefix(match, prefix)
		parts := strings.Split(list, ",")
		sort.Strings(parts)
		return prefix + strings.Join(parts, ",")
	})
	// 2. -k → -F key=
	out = regexpKeyShort.ReplaceAllString(out, " -F key=$1")
	// 3. auid!= 동치 통일
	out = regexpAuidNotEq.ReplaceAllString(out, " -F auid!=unset")
	// 4. auid>= placeholder
	out = regexpAuidGE.ReplaceAllString(out, " -F auid>=__UID_MIN__")
	return out
}

// synthesizeAuditctlMatch는 6.2.3.x audit text에서 합성 bash를 생성합니다.
//
// 합성 출력 구조:
//
//   - bash array `need_disk=( "rule1" "rule2" ... )` + `need_run=( ... )` (정규화된 라인)
//   - normalize_fn (shell 함수) — stdin의 각 라인을 정규화(syscall sort + -k 통일 + auid 동치)
//   - cat /etc/audit/rules.d/*.rules + auditctl -l 출력을 normalize → grep -qxF로 매칭
//   - missing 카운트 0이면 `** PASS **`, 그 외 `** FAIL **` (CIS 마커, selftest harness 호환)
//
// `__UID_MIN__` placeholder는 런타임 `${UID_MIN:-1000}` 치환 (D-N-4).
//
// 반환 ok=false: extractAuditctlExpectedRules 실패 (예: phrase 1회만 등장).
func synthesizeAuditctlMatch(audit string) (bash string, ok bool) {
	onDisk, running, ok := extractAuditctlExpectedRules(audit)
	if !ok {
		return "", false
	}
	// 각 라인 normalize.
	for i, r := range onDisk {
		onDisk[i] = normalizeAuditctlRule(r)
	}
	for i, r := range running {
		running[i] = normalizeAuditctlRule(r)
	}
	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env bash\n")
	sb.WriteString("set -u\n")
	sb.WriteString("UID_MIN=${UID_MIN:-1000}\n")
	sb.WriteString("\n")
	sb.WriteString("need_disk=(\n")
	for _, r := range onDisk {
		fmt.Fprintf(&sb, "  %q\n", r)
	}
	sb.WriteString(")\n")
	sb.WriteString("need_run=(\n")
	for _, r := range running {
		fmt.Fprintf(&sb, "  %q\n", r)
	}
	sb.WriteString(")\n")
	sb.WriteString("\n")
	sb.WriteString("normalize_fn() {\n")
	sb.WriteString("  while IFS= read -r line; do\n")
	sb.WriteString("    case \"$line\" in\n")
	sb.WriteString("      *' -S '*) \n")
	sb.WriteString("        sycs=$(printf '%s' \"$line\" | sed -nE 's/.* -S ([a-zA-Z_][a-zA-Z0-9_,]*).*/\\1/p' | tr ',' '\\n' | sort -u | paste -sd ',')\n")
	sb.WriteString("        line=$(printf '%s' \"$line\" | sed -E \"s/ -S [a-zA-Z_][a-zA-Z0-9_,]*/ -S $sycs/\")\n")
	sb.WriteString("        ;;\n")
	sb.WriteString("    esac\n")
	sb.WriteString("    line=$(printf '%s' \"$line\" | sed -E 's/ -k ([!-~]+)/ -F key=\\1/g; s/ -F auid!=-?[0-9]+/ -F auid!=unset/g')\n")
	sb.WriteString("    printf '%s\\n' \"$line\"\n")
	sb.WriteString("  done\n")
	sb.WriteString("}\n")
	sb.WriteString("\n")
	sb.WriteString("disk_out=$(cat /etc/audit/rules.d/*.rules 2>/dev/null | normalize_fn)\n")
	sb.WriteString("run_out=$(auditctl -l 2>/dev/null | normalize_fn)\n")
	sb.WriteString("missing=0\n")
	sb.WriteString("for r in \"${need_disk[@]}\"; do\n")
	sb.WriteString("  r_subst=${r//__UID_MIN__/$UID_MIN}\n")
	sb.WriteString("  printf '%s\\n' \"$disk_out\" | grep -qxF -- \"$r_subst\" || { printf 'miss-disk: %s\\n' \"$r_subst\"; missing=$((missing+1)); }\n")
	sb.WriteString("done\n")
	sb.WriteString("for r in \"${need_run[@]}\"; do\n")
	sb.WriteString("  r_subst=${r//__UID_MIN__/$UID_MIN}\n")
	sb.WriteString("  printf '%s\\n' \"$run_out\" | grep -qxF -- \"$r_subst\" || { printf 'miss-run: %s\\n' \"$r_subst\"; missing=$((missing+1)); }\n")
	sb.WriteString("done\n")
	sb.WriteString("if [ \"$missing\" -eq 0 ]; then printf '** PASS **\\n'; else printf '** FAIL **\\n'; fi\n")
	return sb.String(), true
}

// collectAuditRuleLines는 verify block에서 audit rule 라인만 추출합니다.
// 종료 조건: 빈 줄(rule 1+ 수집 후) / "#" 시작 라인 / audit rule 패턴 미매칭 라인.
// 단, expected 라인이 multi-line wrap된 continuation(공백 시작 + audit rule prefix 부재)은
// 이전 라인에 join — Stage 2 normalize에서 정밀 처리하지만 본 단계에서도 안전한 join 시도.
func collectAuditRuleLines(block string) []string {
	var out []string
	for _, raw := range strings.Split(block, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			if len(out) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		if regexpAuditctlRule.MatchString(line) {
			out = append(out, line)
			continue
		}
		// continuation: 직전 라인이 audit rule이고 본 라인이 `-` 시작(audit rule의 wrap 토큰
		// — 예: `-F arch=...`)이면 join. heading("Running"·"On disk")은 `-` 미시작이라 종료.
		if len(out) > 0 && strings.HasPrefix(line, "-") {
			out[len(out)-1] = out[len(out)-1] + " " + line
			continue
		}
		break
	}
	return out
}
