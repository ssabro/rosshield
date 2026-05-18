//go:build cis_synth_integration

package converter_test

// cis_synth_integration_test.go — CIS converter 합성 bash 회귀 검증.
//
// ConvertCIS가 생산하는 auditCommand("bash -c '...'")를 실 bash에서 실행 후 PASS/FAIL
// 마커가 의도한대로 출력되는지 mock 환경(sshd/stat 등 함수 stub)으로 검증.
//
// 실 Ubuntu 24.04 환경(sshd-T·stat 실 출력) 부재 시에도 합성 bash의 분기 정확성을 회귀
// 보호한다 — multi-line 흡수·quote escape·정수 비교·case guard 등 휴리스틱이 silent
// regression 일으키면 이 test가 잡는다.
//
// 옵트인 build tag — `go test -tags cis_synth_integration ./cmd/pack-tools/converter/`
// 로 명시 실행. 일반 `go test ./...`는 영향 X (bash exec 비용 회피).
//
// bash 부재 시 t.Skip — Windows git-bash · WSL · Linux native 모두 동작.

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/cmd/pack-tools/converter"
)

// resolveBash는 `ROSSHIELD_BASH_PATH` 환경 변수 또는 PATH에서 bash 위치를 찾는다.
// 미발견 시 빈 string — 호출자가 t.Skip.
func resolveBash() string {
	if p := os.Getenv("ROSSHIELD_BASH_PATH"); p != "" {
		return p
	}
	p, err := exec.LookPath("bash")
	if err != nil {
		return ""
	}
	return p
}

// runSynthesizedAudit는 합성된 auditCommand를 mock 환경 prefix와 함께 실행, 출력 반환.
//
// auditCommand는 `bash -c '<body>'` 형태이고 body 내부에 single quote escape 됨. mock prefix는
// 같은 shell 안에서 함수/변수 정의 후 audit 명령 substitution이 그것들을 활용.
//
// e.g., mockEnv = `sshd() { echo "permitrootlogin no"; }`
//
//	auditCommand = `bash -c 'out="$(sshd -T | grep ...)"; ...'`
//
// 이 둘을 한 shell session에 결선하려면 outer shell에서 함수 export + bash -c 호출 또는
// audit body를 직접 추출해 실행. 후자가 더 명료.
func runSynthesizedAudit(t *testing.T, bashPath, mockEnv, auditCommand string) string {
	t.Helper()
	// auditCommand는 항상 "bash -c '<body>'" 형태로 wrap. body만 추출 후 mock과 결선.
	body, ok := stripBashCWrap(auditCommand)
	if !ok {
		t.Fatalf("auditCommand not in expected bash -c wrap: %q", auditCommand)
	}
	script := mockEnv + "\n" + body
	cmd := exec.Command(bashPath, "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// 합성된 bash가 exit 0 외 코드 반환할 수 있음(synthesizeExpectSSHDNumeric의 exit 0은 의도).
		// stderr는 출력에 포함되므로 syntax error는 grep으로 잡힘.
		t.Logf("bash exit non-zero (output below): %v", err)
	}
	return string(out)
}

// stripBashCWrap는 `bash -c '<body>'` 텍스트에서 single-quoted body를 unescape후 반환.
// CIS converter wrapBash는 본문 안의 single quote를 `'\”` 시퀀스로 escape. 역변환.
func stripBashCWrap(s string) (string, bool) {
	const prefix = "bash -c '"
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	if !strings.HasSuffix(s, "'") {
		return "", false
	}
	inner := s[len(prefix) : len(s)-1]
	// `'\''` → `'`
	return strings.ReplaceAll(inner, `'\''`, `'`), true
}

// auditFromFixture는 jsonFixture를 ConvertCIS에 통과시켜 첫 check의 auditCommand를 반환.
// 변환 실패 또는 check가 비면 t.Fatal.
func auditFromFixture(t *testing.T, jsonFixture string) string {
	t.Helper()
	pack, _, err := converter.ConvertCIS([]byte(jsonFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if len(pack.Checks) == 0 {
		t.Fatal("no checks emitted")
	}
	return pack.Checks[0].AuditCommand
}

// === 테스트 케이스 ===

// expect-empty: cmd 출력이 비어 있으면 PASS.
func TestSynth_ExpectEmpty_PASS_WhenOutputEmpty(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.empty.pass", "assessment_status": "Automated",
    "audit": "Verify output is empty:\n# myverify\nNothing should be returned"
  }]
}`
	mock := `myverify() { :; }` // 빈 출력
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for empty output, got: %s", out)
	}
}

func TestSynth_ExpectEmpty_FAIL_WhenOutputNonEmpty(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.empty.fail", "assessment_status": "Automated",
    "audit": "Verify output is empty:\n# myverify\nNothing should be returned"
  }]
}`
	mock := `myverify() { echo "weak-cipher-found"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for non-empty output, got: %s", out)
	}
}

// expect-non-empty: cmd 출력이 비어 있지 않으면 PASS (is installed/enabled/active).
func TestSynth_ExpectNonEmpty_PASS_WhenOutputNonEmpty(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.nonempty.pass", "assessment_status": "Automated",
    "audit": "Verify package is installed:\n# checkpkg\npkg is installed"
  }]
}`
	mock := `checkpkg() { echo "pkg installed"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for non-empty output, got: %s", out)
	}
}

func TestSynth_ExpectNonEmpty_FAIL_WhenOutputEmpty(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.nonempty.fail", "assessment_status": "Automated",
    "audit": "Verify package is installed:\n# checkpkg\npkg is installed"
  }]
}`
	mock := `checkpkg() { :; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for empty output, got: %s", out)
	}
}

// stat-perm: octal mode ≤ expected + Uid 0/root → PASS.
func TestSynth_StatPerm_PASS_WhenModeWithinLimitAndOwnerRoot(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.stat.pass", "assessment_status": "Automated",
    "audit": "Verify perm is 0644 or more restrictive:\n# stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: ( %g/ %G)' /etc/passwd\nAccess: (0644/-rw-r--r--) Uid: ( 0/ root) Gid: ( 0/ root)"
  }]
}`
	// stat 명령을 함수로 교체 — actual 출력 흉내.
	mock := `stat() { echo 'Access: (0600/-rw-------) Uid: ( 0/ root) Gid: ( 0/ root)'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for mode 0600 ≤ 0644 + root, got: %s", out)
	}
}

func TestSynth_StatPerm_FAIL_WhenModeExceedsLimit(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.stat.fail.mode", "assessment_status": "Automated",
    "audit": "Verify perm is 0640 or more restrictive:\n# stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: ( %g/ %G)' /etc/foo\nAccess: (0640/-rw-r-----) Uid: ( 0/ root) Gid: ( 0/ root)"
  }]
}`
	mock := `stat() { echo 'Access: (0644/-rw-r--r--) Uid: ( 0/ root) Gid: ( 0/ root)'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for mode 0644 > 0640, got: %s", out)
	}
}

func TestSynth_StatPerm_FAIL_WhenOwnerNotRoot(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.stat.fail.owner", "assessment_status": "Automated",
    "audit": "Verify perm is 0644 or more restrictive:\n# stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: ( %g/ %G)' /etc/foo\nAccess: (0644/-rw-r--r--) Uid: ( 0/ root) Gid: ( 0/ root)"
  }]
}`
	mock := `stat() { echo 'Access: (0600/-rw-------) Uid: ( 1000/ alice) Gid: ( 1000/ alice)'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for owner alice (not root), got: %s", out)
	}
}

// sshd boolean: 마지막 토큰 = expected (yes/no).
func TestSynth_SSHDBool_PASS_WhenValueMatches(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.sshdbool.pass", "assessment_status": "Automated",
    "audit": "Verify IgnoreRhosts is set to yes:\n# sshd -T | grep ignorerhosts\nignorerhosts yes"
  }]
}`
	mock := `sshd() { echo "ignorerhosts yes"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for value 'yes' = expected 'yes', got: %s", out)
	}
}

func TestSynth_SSHDBool_FAIL_WhenValueMismatches(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.sshdbool.fail", "assessment_status": "Automated",
    "audit": "Verify IgnoreRhosts is set to yes:\n# sshd -T | grep ignorerhosts\nignorerhosts yes"
  }]
}`
	mock := `sshd() { echo "ignorerhosts no"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for value 'no' != expected 'yes', got: %s", out)
	}
}

// sshd numeric ≤ N: 모든 라인 마지막 토큰 ≤ N.
func TestSynth_SSHDNumericLE_PASS_WhenValueWithinLimit(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.sshdnum.pass", "assessment_status": "Automated",
    "audit": "Verify MaxAuthTries is 4 or less:\n# sshd -T | grep maxauthtries\nmaxauthtries 4"
  }]
}`
	mock := `sshd() { echo "maxauthtries 3"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for 3 ≤ 4, got: %s", out)
	}
}

func TestSynth_SSHDNumericLE_FAIL_WhenValueExceedsLimit(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.sshdnum.fail", "assessment_status": "Automated",
    "audit": "Verify MaxAuthTries is 4 or less:\n# sshd -T | grep maxauthtries\nmaxauthtries 4"
  }]
}`
	mock := `sshd() { echo "maxauthtries 5"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for 5 > 4, got: %s", out)
	}
}

// sshd numeric > 0: multi-option grep 두 라인 모두 > 0.
func TestSynth_SSHDNumericGT_PASS_WhenAllLinesPositive(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.sshdpos.pass", "assessment_status": "Automated",
    "audit": "Verify ClientAliveInterval and ClientAliveCountMax are greater than zero:\n# sshd -T | grep -Pi -- '(clientaliveinterval|clientalivecountmax)'\nclientaliveinterval 15\nclientalivecountmax 3"
  }]
}`
	mock := `sshd() { printf '%s\n%s\n' 'clientaliveinterval 15' 'clientalivecountmax 3'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for both 15>0 and 3>0, got: %s", out)
	}
}

func TestSynth_SSHDNumericGT_FAIL_WhenAnyLineZero(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.sshdpos.fail", "assessment_status": "Automated",
    "audit": "Verify ClientAliveInterval and ClientAliveCountMax are greater than zero:\n# sshd -T | grep -Pi -- '(clientaliveinterval|clientalivecountmax)'\nclientaliveinterval 15\nclientalivecountmax 3"
  }]
}`
	mock := `sshd() { printf '%s\n%s\n' 'clientaliveinterval 15' 'clientalivecountmax 0'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL when one line = 0, got: %s", out)
	}
}

func TestSynth_SSHDNumericGT_FAIL_WhenOutputEmpty(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.sshdpos.empty", "assessment_status": "Automated",
    "audit": "Verify ClientAliveInterval and ClientAliveCountMax are greater than zero:\n# sshd -T | grep -Pi -- '(clientaliveinterval|clientalivecountmax)'\n"
  }]
}`
	mock := `sshd() { :; }` // 빈 출력
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for empty grep output, got: %s", out)
	}
}

// sshd numeric range: out 마지막 토큰이 [lo, hi] 범위면 PASS.
func TestSynth_SSHDRange_PASS_WhenWithinRange(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.sshdrange.pass", "assessment_status": "Automated",
    "audit": "Verify LoginGraceTime is between 1 and 60 seconds:\n# sshd -T | grep logingracetime\nlogingracetime 60"
  }]
}`
	mock := `sshd() { echo "logingracetime 30"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for 30 in [1,60], got: %s", out)
	}
}

func TestSynth_SSHDRange_FAIL_WhenBelowRange(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.sshdrange.below", "assessment_status": "Automated",
    "audit": "Verify LoginGraceTime is between 1 and 60 seconds:\n# sshd -T | grep logingracetime\nlogingracetime 60"
  }]
}`
	mock := `sshd() { echo "logingracetime 0"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for 0 < 1, got: %s", out)
	}
}

func TestSynth_SSHDRange_FAIL_WhenAboveRange(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.sshdrange.above", "assessment_status": "Automated",
    "audit": "Verify LoginGraceTime is between 1 and 60 seconds:\n# sshd -T | grep logingracetime\nlogingracetime 60"
  }]
}`
	mock := `sshd() { echo "logingracetime 120"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for 120 > 60, got: %s", out)
	}
}

// awk verify only X: 정확 매칭 — out == expected이면 PASS.
func TestSynth_AwkVerifyOnly_PASS_WhenExactMatch(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.awkonly.pass", "assessment_status": "Automated",
    "audit": "Verify that only \"root\" is returned:\n# awk -F: '($3 == 0) { print $1 }' /etc/passwd\nroot"
  }]
}`
	mock := `awk() { echo "root"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for exact match 'root', got: %s", out)
	}
}

func TestSynth_AwkVerifyOnly_FAIL_WhenExtraValue(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.awkonly.fail", "assessment_status": "Automated",
    "audit": "Verify that only \"root\" is returned:\n# awk -F: '($3 == 0) { print $1 }' /etc/passwd\nroot"
  }]
}`
	mock := "awk() { printf '%s\\n%s\\n' 'root' 'admin'; }" // root 외 추가 user
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for extra UID-0 user 'admin', got: %s", out)
	}
}

// grep verify output matches: grep regex가 valid alternation 포함, 출력 non-empty이면 PASS.
func TestSynth_GrepVerifyOutput_PASS_WhenGrepMatchesValid(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.grep.pass", "assessment_status": "Automated",
    "audit": "Verify the output matches:\n# grep -Pi -- 'disk_full_action\\h*=\\h*(halt|single)' /etc/audit/auditd.conf\ndisk_full_action = halt"
  }]
}`
	mock := `grep() { echo 'disk_full_action = halt'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for grep returning valid match, got: %s", out)
	}
}

func TestSynth_GrepVerifyOutput_FAIL_WhenGrepEmpty(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.grep.fail", "assessment_status": "Automated",
    "audit": "Verify the output matches:\n# grep -Pi -- 'disk_full_action\\h*=\\h*(halt|single)' /etc/audit/auditd.conf\ndisk_full_action = halt"
  }]
}`
	mock := `grep() { :; }` // 매치 없음 = invalid 설정
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for grep empty (no valid value), got: %s", out)
	}
}

// hashbang body expect-empty: base64 인코딩 + sub-shell 실행 → 출력 빈 PASS.
func TestSynth_HashbangBody_PASS_WhenBodyEmptyOutput(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.hashbang.pass", "assessment_status": "Automated",
    "audit": "Run the following script and verify no results are returned:\n#!/usr/bin/env bash\n{\nfor i in 1 2 3; do\n  : # do nothing - silent\ndone\n}"
  }]
}`
	out := runSynthesizedAudit(t, bashPath, "", auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for empty body output, got: %s", out)
	}
}

func TestSynth_HashbangBody_FAIL_WhenBodyNonEmptyOutput(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.hashbang.fail", "assessment_status": "Automated",
    "audit": "Run the following script and verify no results are returned:\n#!/usr/bin/env bash\n{\necho 'duplicate found'\n}"
  }]
}`
	out := runSynthesizedAudit(t, bashPath, "", auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for body emitting output, got: %s", out)
	}
}

// is mounted: findmnt 출력 non-empty이면 PASS (CIS 1.1.2.x.1 partition 검증).
func TestSynth_IsMounted_PASS_WhenFindmntReturnsRow(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.mounted.pass", "assessment_status": "Automated",
    "audit": "Verify /tmp is mounted:\n# findmnt -kn /tmp\nExample: /tmp tmpfs"
  }]
}`
	mock := `findmnt() { echo "/tmp tmpfs tmpfs rw,nosuid,nodev,noexec"; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS for findmnt non-empty, got: %s", out)
	}
}

func TestSynth_IsMounted_FAIL_WhenFindmntEmpty(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.mounted.fail", "assessment_status": "Automated",
    "audit": "Verify /tmp is mounted:\n# findmnt -kn /tmp\nExample: /tmp tmpfs"
  }]
}`
	mock := `findmnt() { :; }` // unmounted = 빈 출력
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL for findmnt empty, got: %s", out)
	}
}

// multi-line 흡수 5.1.6 형식: dangling `--` + quoted regex split + "No <X> ... should be returned".
func TestSynth_MultiLineCipher_PASS_WhenNoWeakCipher(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.multiline.pass", "assessment_status": "Automated",
    "audit": "Verify weak ciphers are not in use:\n# sshd -T | grep -Pi --\n'^ciphers\\h+([^#\\n\\r]+,)?(3des|aes(128|192|256))-\ncbc'\nNo ciphers in the list below should be returned."
  }]
}`
	// sshd 출력에 weak cipher 없음 (chacha20만) → grep 빈 출력 → PASS
	mock := `sshd() { echo 'ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS when no weak cipher in sshd output, got: %s", out)
	}
}

func TestSynth_MultiLineCipher_FAIL_WhenWeakCipherPresent(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	const fixture = `{
  "items": [{
    "id": "x.multiline.fail", "assessment_status": "Automated",
    "audit": "Verify weak ciphers are not in use:\n# sshd -T | grep -Pi --\n'^ciphers\\h+([^#\\n\\r]+,)?(3des|aes(128|192|256))-\ncbc'\nNo ciphers in the list below should be returned."
  }]
}`
	// sshd 출력에 weak cipher 있음 (3des-cbc) → grep 매칭 → 출력 non-empty → FAIL
	mock := `sshd() { echo 'ciphers 3des-cbc,aes256-gcm@openssh.com'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL when 3des-cbc present in sshd output, got: %s", out)
	}
}

// === D Stage 3 — auditctl 합성 (synthesizeAuditctlMatch) — 4 case ===
//
// audit text fixture는 6.2.3.1 형식(단순 sudoers watch + on-disk 2 + running 2)을 base로,
// case별 mock의 cat/auditctl 출력을 변경하여 합성 bash의 흐름 검증.

// audit_6_2_3_1_min은 6.2.3.1 essential 형식 — 인식기·추출기 매칭 + on-disk 2 lines + running 2 lines.
const audit_6_2_3_1_min = `On disk\n# awk '/^ *-w/' /etc/audit/rules.d/*.rules\nVerify the output matches:\n-w /etc/sudoers -p wa -k scope\n-w /etc/sudoers.d -p wa -k scope\nRunning\n# auditctl -l | awk '/^ *-w/'\nVerify the output matches:\n-w /etc/sudoers -p wa -k scope\n-w /etc/sudoers.d -p wa -k scope`

// case 1: PASS — cat + auditctl 모두 expected 라인 emit (정확 매칭).
func TestSynth_AuditctlMatch_PASS_WhenAllRulesPresent(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	fixture := `{
  "items": [{
    "id": "6.2.3.1", "assessment_status": "Automated",
    "audit": "` + audit_6_2_3_1_min + `"
  }]
}`
	// normalize_fn이 -k → -F key= 변환하므로 mock 출력은 -k 또는 -F key= 어느 쪽이든 OK.
	// 본 case는 -k(short) — normalize_fn이 -F key=로 변환 후 expected와 정확 매칭.
	mock := `cat() { printf '%s\n' '-w /etc/sudoers -p wa -k scope' '-w /etc/sudoers.d -p wa -k scope'; }
auditctl() { printf '%s\n' '-w /etc/sudoers -p wa -k scope' '-w /etc/sudoers.d -p wa -k scope'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS when all rules present, got: %s", out)
	}
}

// case 2: FAIL — on-disk에서 sudoers.d 라인 누락 → missing > 0 → FAIL.
func TestSynth_AuditctlMatch_FAIL_WhenOnDiskMissingRule(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	fixture := `{
  "items": [{
    "id": "6.2.3.1", "assessment_status": "Automated",
    "audit": "` + audit_6_2_3_1_min + `"
  }]
}`
	// cat은 sudoers.d 라인 누락(1 라인만), auditctl은 전부 있음.
	mock := `cat() { printf '%s\n' '-w /etc/sudoers -p wa -k scope'; }
auditctl() { printf '%s\n' '-w /etc/sudoers -p wa -k scope' '-w /etc/sudoers.d -p wa -k scope'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** FAIL **")) {
		t.Errorf("expected FAIL when on-disk missing rule, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("miss-disk:")) {
		t.Errorf("expected miss-disk diagnostic line, got: %s", out)
	}
}

// case 3: PASS — syscall sort normalize 검증.
// expected: `-S adjtimex,settimeofday`, mock auditctl 출력: `-S settimeofday,adjtimex`.
// normalize_fn의 sort + tr + paste pipe가 정렬해서 expected와 매칭되어야 PASS.
const audit_6_2_3_4_min = `On disk\n# awk '/^ *-a *always,exit/' /etc/audit/rules.d/*.rules\nVerify the output matches:\n-a always,exit -F arch=b64 -S adjtimex,settimeofday -k time-change\nRunning\n# auditctl -l | awk '/^ *-a *always,exit/'\nVerify the output includes:\n-a always,exit -F arch=b64 -S adjtimex,settimeofday -F key=time-change`

func TestSynth_AuditctlMatch_PASS_WhenSyscallOrderDiffers(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	fixture := `{
  "items": [{
    "id": "6.2.3.4", "assessment_status": "Automated",
    "audit": "` + audit_6_2_3_4_min + `"
  }]
}`
	// running 출력의 syscall 순서가 expected와 다름(settimeofday,adjtimex vs adjtimex,settimeofday).
	// normalize_fn이 양쪽 모두 정렬하면 매칭 성공.
	mock := `cat() { printf '%s\n' '-a always,exit -F arch=b64 -S adjtimex,settimeofday -k time-change'; }
auditctl() { printf '%s\n' '-a always,exit -F arch=b64 -S settimeofday,adjtimex -F key=time-change'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS after normalize_fn sorts syscalls, got: %s", out)
	}
}

// case 4: PASS — UID_MIN 1001 환경에서 placeholder 치환 + auid!= 동치 통일.
// expected에 `auid>=1000` + `auid!=unset`, mock 출력에 `auid>=1001` + `auid!=-1`.
// 합성 bash가 UID_MIN=1001 환경 변수 + normalize_fn auid!= 통일로 매칭 성공.
const audit_6_2_3_7_min = `On disk\n# {\nUID_MIN=$(awk ...)\n[ -n \"${UID_MIN}\" ] && awk \"/^ *-a *always,exit/ ... -F *auid>=${UID_MIN}\" /etc/audit/rules.d/*.rules\n}\nVerify the output includes:\n-a always,exit -F arch=b64 -S creat,open -F auid>=1000 -F auid!=unset -k access\nRunning\n# {\nUID_MIN=$(awk ...)\n[ -n \"${UID_MIN}\" ] && auditctl -l | awk \"/^ *-a *always,exit/ ...\"\n}\nVerify the output includes:\n-a always,exit -F arch=b64 -S open,creat -F auid>=1000 -F auid!=-1 -F key=access`

func TestSynth_AuditctlMatch_PASS_WhenUIDMin1001AndAuidEquivalence(t *testing.T) {
	bashPath := resolveBash()
	if bashPath == "" {
		t.Skip("bash not found")
	}
	fixture := `{
  "items": [{
    "id": "6.2.3.7", "assessment_status": "Automated",
    "audit": "` + audit_6_2_3_7_min + `"
  }]
}`
	// UID_MIN=1001 환경. expected의 auid>=1000은 placeholder __UID_MIN__로 normalize되고,
	// 합성 bash 안에서 ${UID_MIN}=1001로 치환 → mock 출력의 auid>=1001과 매칭.
	// 또한 auid!=-1, auid!=unset은 normalize_fn에서 모두 auid!=unset로 통일.
	mock := `export UID_MIN=1001
cat() { printf '%s\n' '-a always,exit -F arch=b64 -S creat,open -F auid>=1001 -F auid!=unset -k access'; }
auditctl() { printf '%s\n' '-a always,exit -F arch=b64 -S open,creat -F auid>=1001 -F auid!=-1 -F key=access'; }`
	out := runSynthesizedAudit(t, bashPath, mock, auditFromFixture(t, fixture))
	if !bytes.Contains([]byte(out), []byte("** PASS **")) {
		t.Errorf("expected PASS with UID_MIN=1001 + auid equivalence, got: %s", out)
	}
}
