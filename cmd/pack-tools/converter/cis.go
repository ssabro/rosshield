package converter

// cis.go — CIS Ubuntu 24.04 baseline JSON을 rosshield pack으로 변환합니다 (E12 Stage C).
//
// 입력 형식: nrobotcheck `resources/baselines/cis_ubuntu_2404_benchmark.json` (312 items).
//
// R8-3' (보정 후): CIS audit 필드는 사실 자연어 가이드 + bash 블록이 혼합된 매뉴얼 텍스트.
// 312 items 분석 결과:
//
//   - PASS/FAIL 마커 + bash hashbang 모두 갖춘 자동 변환 가능 items: ~61
//   - 마커 없거나 자연어만 있는 items: ~250 → degraded marker
//   - assessment_status="Manual" items: 21 → degraded marker
//
// 자동 변환 가능 items는 `#!/usr/bin/env bash`(또는 `#!/bin/bash`) 이후의 bash 본문만 추출,
// `bash -c '<extracted body>'`로 wrap. evaluationRule은 단순 `{"op":"contains","value":"** PASS **"}`
// — bash 스크립트가 항상 PASS 또는 FAIL 마커 중 하나만 출력하기 때문.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CISConvertOptions는 변환 시 pack 메타를 지정합니다.
type CISConvertOptions struct {
	PackName        string // 미지정 시 "cis-ubuntu-2404"
	PackVersion     string // 미지정 시 "1.0.0"
	PackVendor      string // 미지정 시 "rosshield"
	PackDescription string
}

// CISConvertReport는 변환 통계와 degraded check 목록을 담습니다.
type CISConvertReport struct {
	TotalItems       int
	Converted        int      // 자동 변환된 check 수
	DegradedManual   int      // assessment_status=Manual로 분류된 수
	DegradedNoMarker int      // PASS/FAIL 마커 또는 hashbang 부재로 분류된 수
	Degraded         []string // "<check_id>: <reason>"
}

// CISDegradedItem은 자동 변환 안 된 항목의 원본 정보를 운영자에게 노출 — 수동 변환 가이드.
//
// 본 구조는 docs 서브커맨드 (pack-tools docs)가 markdown 생성에 사용. cisItem은 unexported지만
// 본 구조는 exported로 외부 호출자(main, 다른 패키지)가 활용 가능.
type CISDegradedItem struct {
	ID                   string
	Title                string
	Reason               string // "Manual" 또는 "audit lacks ** PASS ** marker (natural-language manual)" 등
	AssessmentStatus     string // "Automated" / "Manual"
	ProfileApplicability []string
	Description          string
	Rationale            string
	Audit                string
	Remediation          string
}

// CIS JSON 와이어 형식 — 변환에 필요한 필드만 정의 (unknown 필드는 무시).
type cisDocument struct {
	Benchmark string    `json:"benchmark"`
	Version   string    `json:"version"`
	Date      string    `json:"date"`
	Items     []cisItem `json:"items"`
}

type cisItem struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	AssessmentStatus     string   `json:"assessment_status"`
	ProfileApplicability []string `json:"profile_applicability"`
	Description          string   `json:"description"`
	Rationale            string   `json:"rationale"`
	Audit                string   `json:"audit"`
	Remediation          string   `json:"remediation"`
}

// CIS 변환 에러.
var (
	ErrCISDecodeFailed     = errors.New("converter: cis JSON decode failed")
	ErrCISNoItems          = errors.New("converter: cis document has no items")
	ErrCISDuplicateCheckID = errors.New("converter: cis duplicate check ID")
)

// cisAutoEvalRuleJSON은 자동 변환된 CIS check의 evaluationRule입니다.
//
// CIS bash 스크립트는 항상 `printf '%s\n' "" "- Audit Result:" " ** PASS **"` 또는
// `... " ** FAIL **"` 둘 중 하나만 출력하므로 단순 contains로 충분.
var cisAutoEvalRuleJSON = json.RawMessage(`{"op":"contains","value":"** PASS **"}`)

// ConvertCIS는 CIS Ubuntu baseline JSON 바이트를 rosshield pack으로 변환합니다.
//
// items 배열의 각 entry → 1 converter.Check (1:1 매핑).
// 자동 변환 불가능한 items(자연어 audit·Manual 분류)는 degraded marker로 fallback —
// 결코 error로 abort 안 함.
func ConvertCIS(jsonBytes []byte, opts CISConvertOptions) (Pack, CISConvertReport, error) {
	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.UseNumber()
	var doc cisDocument
	if err := dec.Decode(&doc); err != nil {
		return Pack{}, CISConvertReport{}, fmt.Errorf("%w: %v", ErrCISDecodeFailed, err)
	}
	if len(doc.Items) == 0 {
		return Pack{}, CISConvertReport{}, ErrCISNoItems
	}

	pack := Pack{
		Name:        firstNonEmpty(opts.PackName, "cis-ubuntu-2404"),
		Version:     firstNonEmpty(opts.PackVersion, "1.0.0"),
		Vendor:      firstNonEmpty(opts.PackVendor, "rosshield"),
		Description: firstNonEmpty(opts.PackDescription, doc.Benchmark),
	}
	report := CISConvertReport{TotalItems: len(doc.Items)}
	seen := make(map[string]struct{}, len(doc.Items))

	for _, it := range doc.Items {
		if it.ID == "" {
			continue
		}
		if _, dup := seen[it.ID]; dup {
			return Pack{}, CISConvertReport{}, fmt.Errorf("%w: %q", ErrCISDuplicateCheckID, it.ID)
		}
		seen[it.ID] = struct{}{}

		check, degradedReason := convertCISItem(it)
		pack.Checks = append(pack.Checks, check)
		switch {
		case degradedReason == "":
			report.Converted++
		case strings.Contains(degradedReason, "Manual"):
			report.DegradedManual++
		default:
			report.DegradedNoMarker++
		}
		if degradedReason != "" {
			report.Degraded = append(report.Degraded, fmt.Sprintf("%s: %s", it.ID, degradedReason))
		}
	}
	return pack, report, nil
}

// convertCISItem은 단일 cis item을 converter.Check + (degraded reason 또는 빈 string)으로 변환.
//
// 자동 변환 조건: assessment_status=Automated AND PASS 마커 AND bash hashbang 추출 가능.
// 그 외는 degraded marker — audit/remediation 텍스트는 rationale·fixGuidance에 보존되므로
// 사용자가 수동으로 evaluationRule을 추가할 수 있음.
func convertCISItem(it cisItem) (Check, string) {
	check := Check{
		ID:          it.ID,
		Title:       it.Title,
		Description: it.Description,
		Severity:    classifyCISSeverity(it),
		Rationale:   it.Rationale,
		FixGuidance: it.Remediation,
	}

	if it.AssessmentStatus == "Manual" {
		check.AuditCommand = "true"
		check.EvaluationRule = degradedEvalRuleJSON
		return check, "assessment_status=Manual (manual review required)"
	}

	// 자동 변환 우선순위:
	//  1) 표준 PASS 마커 + bash hashbang (기존 로직)
	//  2) "Nothing should be returned" 패턴 + 마지막 shell line 추출 (CIS 자연어 가이드 다수)
	//  3) "is installed" 또는 dpkg-query &&/echo 긍정 기대 패턴
	//  4) stat/ls 파일 권한 검증 (octal mode + Uid:( 0/root))
	//  5) sshd -T | grep <option> 동적 설정 검증 (yes/no)
	//  6) sshd -T | grep <option> 수치 검증 (-le N / -ge N / -gt 0)
	//  7) bash hashbang body + expect-empty/non-empty 키워드 (PASS 마커 부재 항목, base64 wrap)
	if strings.Contains(it.Audit, "** PASS **") {
		body, ok := extractCISBashBody(it.Audit)
		if !ok {
			check.AuditCommand = "true"
			check.EvaluationRule = degradedEvalRuleJSON
			return check, "audit lacks bash hashbang"
		}
		check.AuditCommand = wrapBash(body)
		check.EvaluationRule = cisAutoEvalRuleJSON
		return check, ""
	}

	// Pattern 2/3/4/5/6: 자연어 가이드 + 마지막 shell line + 기대 결과 키워드 추출.
	if synthesized, ok := synthesizeCISShellAssertion(it.Audit); ok {
		check.AuditCommand = wrapBash(synthesized)
		check.EvaluationRule = cisAutoEvalRuleJSON
		return check, ""
	}

	// Pattern 12 (E-2 G1): `nft list ruleset | grep 'hook X'` 3+ cmds + 각 expected substring 매칭.
	// 4.3.5 (chain 존재 검증) + 4.3.8 (policy drop 포함 검증). expected가 audit text에서 파생.
	if isNftHookAuditText(it.Audit) {
		if synthesized, ok := synthesizeNftHook(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 22 (G13 5.2.6): sudo cache timeout — `timestamp_timeout` 추출 + ≤15 비교.
	if isSudoTimestampTimeoutAuditText(it.Audit) {
		if synthesized, ok := synthesizeSudoTimestampTimeout(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 21 (G6 부분): `ufw status verbose | grep Default:` + alternation 매칭 (4.2.7).
	// 4.2.4 (multi-line table + 2 cmd)는 별 epic.
	if isUfwStatusDefaultAuditText(it.Audit) {
		if synthesized, ok := synthesizeUfwStatusDefault(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 20 (G10 부분): hashbang body 자체 PASSED/FAILED emit (5.4.3.2). body base64 wrap →
	// 실행 → 출력 substring 매칭.
	if isHashbangPassFailEmitAuditText(it.Audit) {
		if synthesized, ok := synthesizeHashbangPassFailEmit(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 20b (G10 7/2 마감 — 5.4.1.6): shebang 없는 `{}` block + "verify nothing is
	// returned" phrase. block base64 wrap → 실행 → 출력 비어있으면 PASS.
	if isBraceBlockEmptyAuditText(it.Audit) {
		if synthesized, ok := synthesizeBraceBlockEmpty(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 19 (G3): nftables include + awk hook block scan (4.3.10) — 90% 도달 마지막 epic.
	// hardcoded 3 hook (input/forward/output) + policy drop substring 검증.
	if isNftIncludeAuditText(it.Audit) {
		if synthesized, ok := synthesizeNftInclude(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 18 (G14): grub.cfg multi-line verify (1.4.1) — 2 cmd × placeholder substring 매칭.
	if isGrubCfgAuditText(it.Audit) {
		if synthesized, ok := synthesizeGrubCfg(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 17 (G16): passwd/group awk + alternative — 5.4.2.2 + 5.4.2.3 (exact root:N) +
	// 5.4.2.4 (alternation User: "..." Password is status: P|L). 90% 도달 경로 핵심 epic.
	if isPasswdAwkAuditText(it.Audit) {
		if synthesized, ok := synthesizePasswdAwk(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 16 (E-3 G8): `apparmor_status | grep profiles/processes` count 추출 + 비교.
	// 1.3.1.3 (either) + 1.3.1.4 (strict). mode phrase 자동 판정 (D-E3-2).
	if isApparmorCountAuditText(it.Audit) {
		if synthesized, ok := synthesizeApparmorCount(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 15 (E-3 G12): `# [ -e <path> ] && stat -Lc '...' <path>` 옵트 + expected substring.
	// 1.6.4 (단일 path) + 7.1.10 (다중 path). 파일 미존재 시 PASS 처리.
	if isStatOptAuditText(it.Audit) {
		if synthesized, ok := synthesizeStatOpt(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 14 (E-3 G9): `# dpkg-query ...` + expected substring 매칭. 1.7.1 (not-installed) +
	// 5.3.1.1 (Status: install ok installed).
	if isDpkgQueryAuditText(it.Audit) {
		if synthesized, ok := synthesizeDpkgQuery(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 14b (E-3 G9 7/7 마감 — 2.1.20): `dpkg-query -s <pkg> &>/dev/null && echo` +
	// "Nothing should be returned" — 패키지 미설치이면 PASS.
	if isDpkgQueryEmptyAuditText(it.Audit) {
		if synthesized, ok := synthesizeDpkgQueryEmpty(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 13 (E-2 G4 부분): 단일 `# iptables -L` + 3+ "Chain X (policy Y)" expected substring 매칭.
	// 4.4.2.1만 cover. 4.4.2.2(`-v -n` + multi-line table)는 별 epic.
	if isIptablesChainPolicyAuditText(it.Audit) {
		if synthesized, ok := synthesizeIptablesChainPolicy(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 12b (E-2 G2): 단일 `# nft list tables` + "Return should include" + expected substring.
	// 4.3.4 — `nft list tables` 출력에 expected(`table inet filter`) 포함이면 PASS.
	if isNftListTablesAuditText(it.Audit) {
		if synthesized, ok := synthesizeNftListTables(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 11 (E-1 G11): `sshd -T | grep <key>` + multi-line OR alternation — 5.1.4 + 5.1.14.
	// cmd 실행 후 expected substring 1+ 매칭이면 PASS (case insensitive).
	if isSshdGrepOrAuditText(it.Audit) {
		if synthesized, ok := synthesizeSshdGrepOr(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 10 (E-1 G15): multi-cmd `grep -Pi -- '...'` /etc/audit/auditd.conf — 6.2.2.3.
	// 2+ grep 명령 모두 non-empty이면 PASS. narrow(auditd.conf 경로 명시)로 false trigger 0.
	if isMultiGrepAuditdAuditText(it.Audit) {
		if synthesized, ok := synthesizeMultiGrepAuditd(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 9 (E-1 G7-bool): `gsettings get <schema> <key>` boolean 정확 매칭. 1.7.6 + 1.7.8.
	if isGsettingsBoolAuditText(it.Audit) {
		if synthesized, ok := synthesizeGsettingsBool(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 9b (E-1 G7-uint32): `gsettings get` uint32 N + threshold 비교. 1.7.4. audit text의
	// uint32 N을 baseline threshold로 사용 (CIS "N seconds or less" 의미).
	if isGsettingsUint32AuditText(it.Audit) {
		if synthesized, ok := synthesizeGsettingsUint32(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 8: 6.2.3.x auditd rules — auditctl -l + Verify the output matches/includes
	// + audit rule lines. on-disk(/etc/audit/rules.d/*.rules) + running(auditctl -l) 양쪽
	// expected를 normalize 후 grep 매칭, missing 카운트 0이면 PASS. design doc:
	// docs/design/notes/cis-6-2-3-auditd-design.md (D Stage 3 결선).
	//
	// hashbang body 가진 6.2.3.19 등도 본 분기로 우선 매칭(Pattern 7보다 specific).
	if isAuditctlAuditText(it.Audit) {
		if synthesized, ok := synthesizeAuditctlMatch(it.Audit); ok {
			check.AuditCommand = wrapBash(synthesized)
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	// Pattern 7: bash hashbang body + expect-empty/non-empty 키워드 (PASS 마커 부재 항목).
	// CIS 5.4.2.7 / 7.2.3·5·6·7·8 / 1.7.10 등 ~12 항목 — bash hashbang body 자체가 검증 명령
	// (출력 없으면 정상)이고 PASS/FAIL 마커는 안 emit. body를 sub-shell 실행 + 출력 검사.
	if hashbangBody, ok := extractCISBashBody(it.Audit); ok {
		if regexpExpectEmpty.MatchString(it.Audit) || isNoResultsReturned(it.Audit) {
			check.AuditCommand = wrapBash(synthesizeBashBodyExpectEmpty(hashbangBody))
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
		if regexpExpectNonEmpty.MatchString(it.Audit) {
			check.AuditCommand = wrapBash(synthesizeBashBodyExpectNonEmpty(hashbangBody))
			check.EvaluationRule = cisAutoEvalRuleJSON
			return check, ""
		}
	}

	check.AuditCommand = "true"
	check.EvaluationRule = degradedEvalRuleJSON
	return check, "audit lacks ** PASS ** marker (natural-language manual)"
}

// ListCISDegraded는 baseline JSON에서 자동 변환 안 된 항목들의 원본 정보를 반환.
//
// 운영자 수동 변환 가이드 markdown 생성용 (pack-tools docs 서브커맨드). ConvertCIS와
// 동일한 분류 로직을 한번 더 호출하지 않고, convertCISItem의 degraded reason을 ID별
// 매핑한 후 cisItem 원본 매칭.
//
// 반환 순서: input JSON 순서 보존 (CIS section 순서 유지).
func ListCISDegraded(jsonBytes []byte) ([]CISDegradedItem, error) {
	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.UseNumber()
	var doc cisDocument
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCISDecodeFailed, err)
	}
	if len(doc.Items) == 0 {
		return nil, ErrCISNoItems
	}

	out := make([]CISDegradedItem, 0)
	for _, it := range doc.Items {
		if it.ID == "" {
			continue
		}
		_, reason := convertCISItem(it)
		if reason == "" {
			continue
		}
		out = append(out, CISDegradedItem{
			ID:                   it.ID,
			Title:                it.Title,
			Reason:               reason,
			AssessmentStatus:     it.AssessmentStatus,
			ProfileApplicability: it.ProfileApplicability,
			Description:          it.Description,
			Rationale:            it.Rationale,
			Audit:                it.Audit,
			Remediation:          it.Remediation,
		})
	}
	return out, nil
}

// classifyCISSeverity는 CIS item을 severity(low/medium/high)로 분류합니다.
//
// 우선순위 (먼저 매치되는 것):
//   1. profile_applicability에 "Level 2" 포함 (단독 또는 mixed) → low
//      Level 2는 CIS 정의상 "defense in depth, may impact functionality" — 운영 환경
//      추가 강화로 미충족이라도 즉각 조치 우선순위 낮음.
//   2. Critical CIS sections (Level 1 한정) → high
//      - 5.x section: sudo + sshd + PAM + password/user (인증·권한 핵심)
//      - 6.1.x section: cron permissions (스케줄 작업 무결성)
//      - 6.2.x section: audit rules (시스템 감사 무결성)
//      - 7.1.x section: system file permissions (/etc/passwd · /etc/shadow 등 핵심 파일)
//   3. 기본 → medium (Level 1, 보통 컴플라이언스 항목)
//
// 운영자 우선순위 부여 — `medium` 일괄 fallback에서 첫 분류 도입. 기존 nrobotcheck
// baseline JSON에는 명시 severity 필드가 없으므로 휴리스틱이 정답.
func classifyCISSeverity(it cisItem) string {
	for _, p := range it.ProfileApplicability {
		if strings.Contains(p, "Level 2") {
			return "low"
		}
	}
	if isCriticalCISSection(it.ID) {
		return "high"
	}
	return DefaultSeverity
}

// isCriticalCISSection은 CIS section ID로 critical 여부 판정.
// 5.x (인증) / 6.1.x (cron) / 6.2.x (audit) / 7.1.x (시스템 파일) 만 high.
// 7.2.x (user/group integrity) · 6.3.x · 1~4.x (filesystem/services/network/firewall) 은 medium.
func isCriticalCISSection(id string) bool {
	if strings.HasPrefix(id, "5.") {
		return true
	}
	if strings.HasPrefix(id, "6.1.") || strings.HasPrefix(id, "6.2.") {
		return true
	}
	if strings.HasPrefix(id, "7.1.") {
		return true
	}
	return false
}

// cisShellLineRe는 audit 텍스트에서 한 줄 shell 명령(`# <cmd>`)을 매칭합니다.
//
// CIS audit 가이드는 공식적으로 `# <command>` 줄로 명령을 표시 — `# Run the following...`
// 같은 자연어 줄은 제외(자체 휴리스틱).
var cisShellLineRe = regexp.MustCompile(`(?m)^\s*#\s+(\S.*\S|\S)\s*$`)

// shellLineSkipRe는 audit 텍스트의 `# ...` 줄 중 자연어/주석 텍스트를 건너뜁니다.
//
// CIS는 `# Run the following`, `# Example output:` 등 자연어를 같은 prefix로 사용.
var shellLineSkipRe = regexp.MustCompile(`(?i)^(run\s|example|verify|the\s|note:|to\s|nothing\s|or\s|where\s|if\s)`)

// extractCISLastShellLine은 audit 본문 마지막 shell line을 반환합니다 (자연어 줄 제외).
//
// 마지막 명령을 우선시하는 이유: CIS audit 가이드는 일반적으로 자연어 설명 → 명령
// 순서로 작성됨. 마지막 명령이 실제 검증 명령일 가능성 가장 높음.
//
// multi-line 흡수: 첫 # 줄 cmd가 dangling token(`--`/`\\`/`|`/`&`) 또는 unmatched quote를
// 가지면 다음 줄들을 close 또는 자연어 경계까지 흡수(no space join — quoted regex 의미 보존).
// CIS audit은 PDF rendering 한계로 단일 PCRE regex가 여러 줄로 분할되어 작성된 케이스
// 다수(5.1.6 Ciphers · 5.1.12 KexAlgorithms · 5.1.15 MACs 등). 단일 줄만 추출하면 grep
// 인자 누락 false PASS 위험.
func extractCISLastShellLine(audit string) (string, bool) {
	indices := cisShellLineRe.FindAllStringSubmatchIndex(audit, -1)
	matches := cisShellLineRe.FindAllStringSubmatch(audit, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		cand := strings.TrimSpace(matches[i][1])
		if cand == "" || shellLineSkipRe.MatchString(cand) {
			continue
		}
		// '#' 시작 자연어 줄 후속(예: `# Example output:`) 회피 — 명령처럼 보이는 줄만 사용.
		// 실 명령은 `findmnt`, `dpkg-query`, `grep`, `stat`, `sshd -T |` 등 식별자로 시작.
		if !looksLikeShellCommand(cand) {
			continue
		}
		// 첫 줄 cmd가 incomplete이면 다음 줄들 흡수 시도 (matches[i][3]은 첫 줄 cmd 끝 offset).
		afterFirst := audit[indices[i][3]:]
		return absorbCISContinuation(cand, afterFirst), true
	}
	return "", false
}

// absorbCISContinuation은 첫 # 줄 cmd가 incomplete(dangling token 또는 unmatched quote)이면
// 다음 줄들을 close 또는 자연어/주석 경계까지 흡수 후 join합니다.
//
// 종료 조건 (먼저 매치되는 것):
//  1. cmd가 complete(quote balanced + no dangling token) — 정상 흡수 완료
//  2. 다음 줄이 # 또는 - 시작 — 다음 명령 또는 자연어 (5.1.6의 "- IF -" 등)
//  3. 다음 줄이 자연어 prefix(Run/Note/Verify/...) — shellLineSkipRe 매칭
//  4. safety limit (8 lines / 4096 chars) — runaway 방어
//
// join 전략 (quote-balance 기반):
//  - accumulated가 unmatched quote(quoted regex 안) → newline 제거 no-space join
//    예: `aes(128|192|256))-` + `cbc|arcfour...` → `aes(128|192|256))-cbc|arcfour...` 정확 복원
//  - accumulated가 balanced quote(보통 dangling flag/operator로 끝남) → space join
//    예: `sshd -T | grep -Pi --` + `'^ciphers...` → `sshd -T | grep -Pi -- '^ciphers...`
//        no-space join 시 `--'^ciphers'` 가 grep `--^ciphers` 단일 패턴으로 파싱되어 false PASS
func absorbCISContinuation(first, after string) string {
	if isCISCmdComplete(first) {
		return first
	}
	const maxLines = 8
	const maxLen = 4096
	accumulated := first
	consumed := 0
	for _, line := range strings.Split(after, "\n") {
		if consumed >= maxLines || len(accumulated) > maxLen {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
			break
		}
		if shellLineSkipRe.MatchString(trimmed) {
			break
		}
		if quotedContext(accumulated) {
			accumulated += trimmed
		} else {
			accumulated += " " + trimmed
		}
		consumed++
		if isCISCmdComplete(accumulated) {
			break
		}
	}
	return accumulated
}

// quotedContext는 cmd가 unmatched quote(quoted regex/string 안) 상태인지 반환.
// true면 다음 줄 join 시 newline 제거 no-space (regex 분할 token 복원).
// false면 space join (dangling flag 다음 인자가 별 토큰으로 분리되어야 안전).
func quotedContext(cmd string) bool {
	return strings.Count(cmd, "'")%2 != 0 || strings.Count(cmd, `"`)%2 != 0
}

// isCISCmdComplete는 cmd가 complete(흡수 종료 가능)인지 판정.
//
// incomplete 조건:
//   - dangling getopt `--` (인자 필요, 5.1.6 `sshd -T | grep -Pi --`)
//   - dangling backslash `\\` (POSIX line continuation)
//   - dangling pipe `|` 또는 `&` (다음 명령 필요)
//   - unmatched single/double quote (quoted regex 미닫힘)
//   - trailing single `-` AND quote balanced — 5.3.3.4.1 같은 path 분할 (`/etc/pam.d/common-`
//     + 다음 줄 `{password,auth,...}`). quoted alt 안 trailing hyphen은 quote unmatched로 잡힘.
func isCISCmdComplete(cmd string) bool {
	trimmed := strings.TrimRight(cmd, " \t")
	if strings.HasSuffix(trimmed, "--") ||
		strings.HasSuffix(trimmed, "\\") ||
		strings.HasSuffix(trimmed, "|") ||
		strings.HasSuffix(trimmed, "&") {
		return false
	}
	quoteBalanced := strings.Count(cmd, "'")%2 == 0 && strings.Count(cmd, `"`)%2 == 0
	if quoteBalanced && strings.HasSuffix(trimmed, "-") {
		return false
	}
	return quoteBalanced
}

// looksLikeShellCommand는 한 줄 텍스트가 실제 shell 명령인지 휴리스틱으로 판정합니다.
//
// 첫 토큰이 알파벳·숫자·`/`·`_`로 시작하면 명령으로 간주. 자연어 prefix는 별도 skip.
func looksLikeShellCommand(line string) bool {
	if line == "" {
		return false
	}
	c := line[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '/' || c == '_'
}

// synthesizeCISShellAssertion은 자연어 가이드에서 expected outcome을 추론해 bash를 합성합니다.
//
// 매핑 (sshd/stat 분기는 일반 expect-empty/non-empty보다 우선 — 같은 audit에 "Nothing should be
// returned"가 동시 존재해도 sshd/stat 검증이 더 정확):
//  1. sshd 수치 옵션 — `is N or less`/`greater than zero` → 모든 출력 라인 마지막 토큰 정수 비교
//  2. sshd boolean — "set to (yes|no)" → 마지막 토큰 lowercase 비교
//  3. stat/ls 파일 권한 — octal mode → ≤ expected 비교 + Uid 검증
//  4. grep + "verify output matches/is" / "ensure output is in compliance" → expect-non-empty
//     (CIS 6.2.2.x auditd config — grep regex가 valid value alternation 포함, 출력 non-empty == valid)
//  5. grep + "is X or Y in" + cmd alternation → expect-non-empty (5.4.1.4 ENCRYPT_METHOD 형식)
//  6. awk + "verify that only X is returned" → 정확 매칭 (5.4.2.1 형식)
//  7. "Nothing should be returned" → expect 빈 출력
//  8. "<X> is installed/enabled/active/mounted" → 비어 있지 않아야 PASS
func synthesizeCISShellAssertion(audit string) (string, bool) {
	cmd, ok := extractCISLastShellLine(audit)
	if !ok {
		return "", false
	}
	if isSSHDCommand(cmd) {
		if lo, hi, ok2 := extractExpectedSSHDRange(audit); ok2 {
			return synthesizeExpectSSHDRange(cmd, lo, hi), true
		}
		if op, threshold, ok2 := extractExpectedSSHDNumeric(audit); ok2 {
			return synthesizeExpectSSHDNumeric(cmd, op, threshold), true
		}
		if val, ok2 := extractExpectedSSHDValue(audit); ok2 {
			return synthesizeExpectSSHDOption(cmd, val), true
		}
	}
	if isStatCommand(cmd) {
		if mode, ok2 := extractExpectedOctalMode(audit); ok2 {
			return synthesizeExpectStatPerm(cmd, mode), true
		}
	}
	// grep + "verify output matches/is" 또는 "ensure output is in compliance" 분기.
	// CIS 6.2.2.x auditd config처럼 grep regex가 valid value alternation(`(halt|single)`)을
	// 포함해 출력 non-empty == valid 설정 인 경우. expect-non-empty 우선 적용.
	if isGrepCommand(cmd) && regexpVerifyOutputMatches.MatchString(audit) {
		return synthesizeExpectNonEmpty(cmd), true
	}
	// grep + "is X or Y in" + cmd 자체 alternation 보유 — 5.4.1.4 ENCRYPT_METHOD 형식.
	// audit text가 "is sha512 or yescrypt in /etc/login.defs" 명시 + cmd grep regex가
	// valid alternation `(SHA512|yescrypt)` 보유 → 출력 non-empty == valid.
	if isGrepCommand(cmd) && regexpIsXOrYIn.MatchString(audit) && cmdHasAlternation(cmd) {
		return synthesizeExpectNonEmpty(cmd), true
	}
	// awk + "verify that only X is returned" — 정확 매칭. 5.4.2.1 root 등.
	if isAwkCommand(cmd) {
		if expected, ok2 := extractExpectedOnlyValue(audit); ok2 {
			return synthesizeExpectExact(cmd, expected), true
		}
	}
	switch {
	case regexpExpectEmpty.MatchString(audit):
		return synthesizeExpectEmpty(cmd), true
	case regexpExpectNonEmpty.MatchString(audit):
		return synthesizeExpectNonEmpty(cmd), true
	}
	return "", false
}

// isAwkCommand는 cmd가 awk 시작인지 검사.
func isAwkCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "awk ")
}

// cmdHasAlternation은 cmd 안에 PCRE alternation `(X|Y)` 패턴 보유 검사.
// 단순 character class `[abc]` 는 제외. valid alternation token list 식별 휴리스틱.
func cmdHasAlternation(cmd string) bool {
	return regexpAlternationToken.MatchString(cmd)
}

// extractExpectedOnlyValue는 audit text에서 "verify that only \"X\" is returned" 형식의
// expected single value를 추출. 5.4.2.1 root 등.
func extractExpectedOnlyValue(audit string) (string, bool) {
	m := regexpVerifyOnlyXReturned.FindStringSubmatch(audit)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

// isGrepCommand는 cmd가 grep으로 시작하는지 검사 (auditd config grep 패턴 detection).
func isGrepCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "grep ")
}

var (
	// regexpExpectEmpty은 "Nothing should be returned" / "no output should be returned" /
	// "should not be returned/in use" / "No <X...> should be returned" / "no results are returned"
	// / "if any line is (found|returned)" 류 자연어를 매칭 (대소문자 무시).
	//
	// 추가 변형 cover:
	//  - "should not be returned/in use" — 직접 부정 표현
	//  - "No <subject>...should be returned" — 5.1.6 Ciphers / 5.1.15 MACs / 5.1.12 KexAlgorithms
	//    "No ciphers in the list below should be returned" 형태 (subject 사이 단어 0~10개)
	//  - "no results are returned" — 7.2.4 등 multi-cmd negative validation
	//  - "if any line is (found|returned)" — 5.2.4 sudo NOPASSWD 등 conditional negative
	//    "If any line is found refer to the remediation procedure below" — 라인 발견 시 조치 필요 = 빈 출력이 정상
	//
	// multi-line cmd 흡수(absorbCISContinuation) 적용 후 안전 — 이전엔 첫 줄만 추출돼
	// grep 인자 누락 false PASS 위험으로 비활성화 상태였음.
	regexpExpectEmpty = regexp.MustCompile(`(?i)nothing\s+(should|is)\s+(be\s+)?returned|no\s+output\s+(should|is)\s+(be\s+)?returned|should\s+not\s+be\s+(returned|in\s+use)|no\s+\S+(?:\s+\S+){0,10}\s+should\s+be\s+returned|no\s+results\s+are\s+returned|if\s+any\s+line\s+is\s+(found|returned)`)
	// regexpExpectNonEmpty은 "<X> is installed" 같은 긍정 echo 기대 패턴 (대소문자 무시).
	// 같은 audit에 "Nothing should be returned"가 있으면 우선순위로 expect-empty가 잡힘 (위 switch).
	//
	// 추가 변형: "is mounted" (CIS 1.1.2.x.1 partition findmnt 검증 — 마지막 shell line이
	// `# findmnt -kn /<path>`이고 출력 non-empty면 mounted, empty면 unmounted = FAIL).
	regexpExpectNonEmpty = regexp.MustCompile(`(?i)(is\s+installed|is\s+enabled|is\s+active|is\s+mounted)\b`)
	// regexpExpectedOctalMode는 audit 텍스트에서 첫 8진수 mode를 추출합니다.
	// CIS 7.x 시스템 파일 권한 가이드는 보통 "Access: (0640/-rw-r-----)" 또는
	// "0640 or more restrictive" 형태로 expected mode를 명시.
	regexpExpectedOctalMode = regexp.MustCompile(`0[0-7]{3,4}`)
	// regexpExpectedSSHDValue는 "set to yes" / "set to no" / "set to a value of yes" 등에서
	// expected boolean 값을 추출합니다 (대소문자 무시, 첫 매치 사용).
	regexpExpectedSSHDValue = regexp.MustCompile(`(?i)set\s+to\s+(?:a\s+value\s+of\s+)?["']?(yes|no)["']?`)
	// regexpExpectedSSHDLessOrEqual는 "is N or less" / "verify ... is N or less" 형태에서
	// 임계값 N을 추출합니다 (5.1.16 MaxAuthTries · 5.1.17 MaxSessions 등).
	regexpExpectedSSHDLessOrEqual = regexp.MustCompile(`(?i)is\s+(\d+)\s+or\s+less`)
	// regexpExpectedSSHDGreaterOrEqual는 "is N or more" 형태에서 N을 추출합니다.
	regexpExpectedSSHDGreaterOrEqual = regexp.MustCompile(`(?i)is\s+(\d+)\s+or\s+more`)
	// regexpExpectedSSHDPositive는 "greater than zero" / "are greater than zero" 형태를 매칭
	// (5.1.7 ClientAliveInterval/CountMax 등 양수 검증).
	regexpExpectedSSHDPositive = regexp.MustCompile(`(?i)(?:are|is)\s+greater\s+than\s+zero`)
	// regexpExpectedSSHDRange는 "is between N and M (seconds)?" 형식의 닫힌 범위 추출
	// (5.1.13 LoginGraceTime "is between 1 and 60 seconds" 등).
	regexpExpectedSSHDRange = regexp.MustCompile(`(?i)is\s+between\s+(\d+)\s+and\s+(\d+)\b`)
	// regexpVerifyOutputMatches는 grep + "verify (the )?output matches/is" 또는 "Output
	// (includes|matches|should be similar)" 또는 "ensure output is in compliance" 자연어를
	// 매칭 (대소문자 무시).
	//
	// CIS 6.2.2.x auditd config + 5.3.3.x PAM grep — regex가 valid value alternation 포함
	// 또는 valid 설정 줄 매칭, 출력 non-empty == valid 설정 = PASS. isGrepCommand 분기와
	// 결합해 false positive 회피.
	//
	// "Output should be similar to" 변형 — CIS PAM 가이드 다수에서 사용 (5.3.3.3.x · 5.3.3.4.x).
	regexpVerifyOutputMatches = regexp.MustCompile(`(?i)verify\s+(the\s+)?output\s+(matches|is)|output\s+(includes|matches|should\s+(match|be\s+similar))|ensure\s+output\s+is\s+in\s+compliance`)
	// regexpIsXOrYIn은 "is X or Y in /etc/foo" 형식의 valid alternation 명시 표현 매칭
	// (5.4.1.4 "is sha512 or yescrypt in /etc/login.defs" 형식).
	// in 다음에 path가 있어야 너무 광범위 false positive 회피.
	regexpIsXOrYIn = regexp.MustCompile(`(?i)is\s+\w+\s+or\s+\w+\s+in\s+\S+`)
	// regexpAlternationToken은 cmd 내 PCRE alternation `(X|Y)` 패턴 보유 검사 (단순 토큰만,
	// quantifier·anchor 없는 형태). 5.4.1.4 `(SHA512|yescrypt)` 같은 valid value list 식별.
	regexpAlternationToken = regexp.MustCompile(`\([A-Za-z][A-Za-z0-9_-]*(\|[A-Za-z][A-Za-z0-9_-]*)+\)`)
	// regexpVerifyOnlyXReturned는 "verify that only \"root\" is returned" 형식에서 expected
	// single value 추출. 5.4.2.1 형식. 따옴표 양쪽 type 호환 (single·double·없음).
	regexpVerifyOnlyXReturned = regexp.MustCompile(`(?i)verify\s+that\s+only\s+["']?([\w\-_:./]+)["']?\s+is\s+returned`)
)

// isStatCommand는 cmd line이 `stat ` 으로 시작하는지 검사 (LS 가이드도 일부 stat 명령으로 정규화됨).
func isStatCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "stat ")
}

// isSSHDCommand는 cmd line이 `sshd ` 또는 `sshd\t` 로 시작하는지 검사.
func isSSHDCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "sshd ")
}

// extractExpectedOctalMode는 audit 텍스트에서 첫 8진수 mode(예 "0640")를 반환.
func extractExpectedOctalMode(audit string) (string, bool) {
	m := regexpExpectedOctalMode.FindString(audit)
	if m == "" {
		return "", false
	}
	return m, true
}

// extractExpectedSSHDValue는 audit 텍스트에서 expected boolean 값("yes" 또는 "no")을 반환.
func extractExpectedSSHDValue(audit string) (string, bool) {
	m := regexpExpectedSSHDValue.FindStringSubmatch(audit)
	if len(m) < 2 {
		return "", false
	}
	return strings.ToLower(m[1]), true
}

// extractExpectedSSHDNumeric은 audit 텍스트에서 sshd 옵션의 수치 비교 op·threshold를 반환.
//
// 우선순위: "is N or less" → ("le", N) / "is N or more" → ("ge", N) / "greater than zero" → ("gt", 0).
// 같은 audit에 둘 이상 매칭 시 첫 발견 우선(보수적). 미매칭 시 ok=false.
func extractExpectedSSHDNumeric(audit string) (op string, threshold int, ok bool) {
	if m := regexpExpectedSSHDLessOrEqual.FindStringSubmatch(audit); len(m) >= 2 {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			return "le", n, true
		}
	}
	if m := regexpExpectedSSHDGreaterOrEqual.FindStringSubmatch(audit); len(m) >= 2 {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			return "ge", n, true
		}
	}
	if regexpExpectedSSHDPositive.MatchString(audit) {
		return "gt", 0, true
	}
	return "", 0, false
}

// extractExpectedSSHDRange는 audit 텍스트에서 "is between N and M" 형식의 수치 범위를 반환.
// 5.1.13 LoginGraceTime "is between 1 and 60 seconds" 같은 닫힌 구간 [lo, hi] 검증용.
// 미매칭 시 ok=false.
func extractExpectedSSHDRange(audit string) (lo, hi int, ok bool) {
	m := regexpExpectedSSHDRange.FindStringSubmatch(audit)
	if len(m) < 3 {
		return 0, 0, false
	}
	l, errL := strconv.Atoi(m[1])
	h, errH := strconv.Atoi(m[2])
	if errL != nil || errH != nil || l > h {
		return 0, 0, false
	}
	return l, h, true
}

// synthesizeExpectEmpty는 cmd 출력이 비어 있으면 PASS, 아니면 FAIL 출력하는 bash를 생성.
func synthesizeExpectEmpty(cmd string) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"if [ -z \"$out\" ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeExpectNonEmpty는 cmd 출력이 비어 있지 않으면 PASS, 비어 있으면 FAIL 출력하는 bash 생성.
func synthesizeExpectNonEmpty(cmd string) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"if [ -n \"$out\" ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeExpectStatPerm는 stat 명령 출력에서 octal mode를 추출해 expectedMode 이하인지(즉
// 더 제한적이거나 같음) 확인 + Uid:( 0/ root) 가 포함되어야 PASS 출력.
//
// CIS 7.x 시스템 파일 권한 가이드는 거의 모두 "owned by root, group root, mode ≤ X" 형태이므로
// expectedOwner는 "root"로 고정. 향후 group 일치(shadow 등)는 별도 epic.
//
// stat 명령 형식 가정: `stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: ( %g/ %G)' /path`
// 이 형식은 CIS audit 가이드의 표준 출력 형태.
func synthesizeExpectStatPerm(cmd, expectedMode string) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"mode=\"$(printf '%s\\n' \"$out\" | sed -n 's|.*Access: (\\([0-7]\\{3,4\\}\\)/.*|\\1|p' | head -1)\"\n" +
		"if [ -n \"$mode\" ] && [ \"$((8#$mode))\" -le \"$((8#" + expectedMode + "))\" ] " +
		"&& printf '%s\\n' \"$out\" | grep -qE 'Uid: \\(\\s*0/'; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// regexpNoResultsReturned는 7.2.x 같은 "verify no results are returned" / "verify nothing
// is returned" 변형 — regexpExpectEmpty가 cover 안 하는 표현 보강.
var regexpNoResultsReturned = regexp.MustCompile(`(?i)(no\s+results\s+are\s+returned|verify\s+nothing\s+is\s+returned)`)

// isNoResultsReturned는 audit text에 "no results are returned" 류 표현이 있는지 검사.
func isNoResultsReturned(audit string) bool {
	return regexpNoResultsReturned.MatchString(audit)
}

// synthesizeBashBodyExpectEmpty는 hashbang body를 base64 인코딩 + sub-shell 실행 후 출력 빈
// 검사로 PASS/FAIL 출력. CIS 7.2.x · 5.4.2.7 등 PASS 마커 부재 항목 cover.
//
// base64 인코딩 사유: hashbang body 안 single quote/double quote가 wrapBash escape 시퀀스와
// 충돌해 syntax error 가능. base64 alphanumeric+`/+=`만 → wrapBash 무영향.
// 운영자 yaml 검토 시 가독성 손실은 trade-off (rationale 필드에 원본 audit 가이드 보존).
func synthesizeBashBodyExpectEmpty(hashbangBody string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(hashbangBody))
	return "out=\"$(printf '%s' '" + encoded + "' | base64 -d | bash 2>/dev/null)\"\n" +
		"if [ -z \"$out\" ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeBashBodyExpectNonEmpty는 hashbang body 출력 non-empty이면 PASS.
func synthesizeBashBodyExpectNonEmpty(hashbangBody string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(hashbangBody))
	return "out=\"$(printf '%s' '" + encoded + "' | base64 -d | bash 2>/dev/null)\"\n" +
		"if [ -n \"$out\" ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeExpectExact은 cmd 출력이 expectedValue와 정확히 일치하면 PASS, 아니면 FAIL.
// 5.4.2.1 같은 "verify that only X is returned" 형식 cover. trim 후 비교 (trailing newline
// 정규화). expectedValue는 single line short string (path/username 등) 가정.
func synthesizeExpectExact(cmd, expectedValue string) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"out=\"$(printf '%s' \"$out\" | sed -e 's/[[:space:]]*$//')\"\n" +
		"if [ \"$out\" = \"" + expectedValue + "\" ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeExpectSSHDRange는 sshd -T grep 출력의 모든 라인 마지막 토큰이 닫힌 범위
// [lo, hi]에 있는지 검증 (5.1.13 LoginGraceTime "is between 1 and 60 seconds").
//
// 빈 출력은 즉시 FAIL (옵션 미설정 = invalid). 비정수 마지막 토큰 case 분기 보호 — false
// positive 회피. lo > hi는 호출자(extractExpectedSSHDRange)에서 미리 차단.
func synthesizeExpectSSHDRange(cmd string, lo, hi int) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"if [ -z \"$out\" ]; then\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"fail=0\n" +
		"while IFS= read -r line; do\n" +
		"  [ -z \"$line\" ] && continue\n" +
		"  val=\"$(printf '%s' \"$line\" | awk '{print $NF}')\"\n" +
		"  case \"$val\" in *[!0-9]*|\"\") fail=1; break ;; esac\n" +
		"  if [ \"$val\" -lt " + strconv.Itoa(lo) + " ] || [ \"$val\" -gt " + strconv.Itoa(hi) + " ]; then\n" +
		"    fail=1\n" +
		"    break\n" +
		"  fi\n" +
		"done <<EOF\n" +
		"$out\n" +
		"EOF\n" +
		"if [ \"$fail\" -eq 0 ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeExpectSSHDOption은 sshd -T grep 출력의 마지막 토큰을 lowercase로 expectedValue와
// 비교해 PASS/FAIL 출력. CIS 5.1.x sshd 옵션 검증의 표준 패턴.
//
// sshd -T 출력은 `<option> <value>` 한 줄 형태이고, grep으로 옵션 필터된 후 awk가
// 마지막 필드 추출. expectedValue는 "yes" 또는 "no" (extractExpectedSSHDValue가 lowercase로 정규화).
func synthesizeExpectSSHDOption(cmd, expectedValue string) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"val=\"$(printf '%s\\n' \"$out\" | awk '{print tolower($NF)}')\"\n" +
		"if [ \"$val\" = \"" + expectedValue + "\" ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// synthesizeExpectSSHDNumeric은 sshd -T grep 출력의 모든 라인 마지막 토큰을 정수로 추출해
// op·threshold와 비교 — 모든 라인이 비교 통과해야 PASS, 출력 비어 있으면 FAIL.
//
// op는 bash test operator: "le" (≤) / "ge" (≥) / "gt" (>) / "lt" (<).
//
// 두 옵션 동시 검증(5.1.7 ClientAliveInterval+CountMax) 케이스도 cover — grep 한 번에 두 라인
// 출력, 둘 다 임계값 비교 통과해야 PASS.
//
// 비정수 마지막 토큰(예: 옵션이 string 값)은 즉시 FAIL — false positive 회피.
func synthesizeExpectSSHDNumeric(cmd, op string, threshold int) string {
	return "out=\"$(" + cmd + " 2>/dev/null)\"\n" +
		"if [ -z \"$out\" ]; then\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"fail=0\n" +
		"while IFS= read -r line; do\n" +
		"  [ -z \"$line\" ] && continue\n" +
		"  val=\"$(printf '%s' \"$line\" | awk '{print $NF}')\"\n" +
		"  case \"$val\" in *[!0-9]*|\"\") fail=1; break ;; esac\n" +
		"  if ! [ \"$val\" -" + op + " " + strconv.Itoa(threshold) + " ]; then\n" +
		"    fail=1\n" +
		"    break\n" +
		"  fi\n" +
		"done <<EOF\n" +
		"$out\n" +
		"EOF\n" +
		"if [ \"$fail\" -eq 0 ]; then\n" +
		"  printf '%s\\n' \"** PASS **\"\n" +
		"else\n" +
		"  printf '%s\\n' \"** FAIL **\"\n" +
		"fi\n"
}

// extractCISBashBody는 CIS audit 텍스트에서 bash hashbang 이후 본문을 추출합니다.
//
// CIS audit 필드는 자연어 가이드 + bash 블록이 혼합된 형태:
//
//	"Run the following script to verify:\n- IF - the cramfs ... \n#!/usr/bin/env bash\n{...}"
//
// hashbang 위치를 찾아 그 이후만 반환 — bash -c는 첫 줄이 #로 시작하면 comment로 무시.
//
// 두 hashbang 모두 미존재 시 ok=false (degraded marker로 분류).
func extractCISBashBody(audit string) (string, bool) {
	for _, hashbang := range []string{"#!/usr/bin/env bash", "#!/bin/bash"} {
		if idx := strings.Index(audit, hashbang); idx >= 0 {
			return audit[idx:], true
		}
	}
	return "", false
}
