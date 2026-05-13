package converter_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/cmd/pack-tools/converter"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

// === 자동 변환 가능 — Automated + PASS 마커 + bash hashbang ===
const cisAutomatedFixture = `{
  "benchmark": "CIS Ubuntu Linux 24.04 LTS Benchmark",
  "version": "v1.0.0",
  "items": [
    {
      "id": "1.1.1.1",
      "title": "1.1.1.1 Ensure cramfs kernel module is not available (Automated)",
      "assessment_status": "Automated",
      "profile_applicability": ["Level 1 - Server"],
      "description": "The cramfs filesystem...",
      "rationale": "Removing support for unneeded filesystem types reduces attack surface.",
      "audit": "Run the following script to verify:\n- IF - the cramfs kernel module is available\n#!/usr/bin/env bash\n{\n  if lsmod | grep cramfs; then\n    printf '%s\\n' \"\" \"- Audit Result:\" \" ** FAIL **\"\n  else\n    printf '%s\\n' \"\" \"- Audit Result:\" \" ** PASS **\"\n  fi\n}",
      "remediation": "Add modprobe blacklist."
    }
  ]
}`

// === assessment_status=Manual ===
const cisManualFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "1.1.1.10",
      "title": "Manual review item",
      "assessment_status": "Manual",
      "audit": "Manually inspect ...",
      "rationale": "User judgment required."
    }
  ]
}`

// === Pattern 2: "Nothing should be returned" 자연어 + 마지막 shell line ===
const cisExpectEmptyFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "1.1.2.1.2",
      "title": "Ensure nodev option set on /tmp partition",
      "assessment_status": "Automated",
      "audit": "- IF - a separate partition exists for /tmp, verify that the nodev option is set.\nRun the following command to verify that the nodev mount option is set.\nExample:\n# findmnt -kn /tmp | grep -v nodev\nNothing should be returned"
    }
  ]
}`

// === Pattern 3: "is installed" 긍정 기대 + dpkg-query ===
const cisExpectInstalledFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "1.3.1.1",
      "title": "Ensure AppArmor is installed",
      "assessment_status": "Automated",
      "audit": "Run the following command to verify that apparmor is installed:\n# dpkg-query -s apparmor &>/dev/null && echo apparmor is installed\napparmor is installed"
    }
  ]
}`

// === audit가 자연어만 (no marker) — 어떤 자동 변환 패턴에도 안 잡혀야 함 ===
const cisNoMarkerFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "X.99",
      "title": "Verify random thing",
      "assessment_status": "Automated",
      "audit": "Run the following command to inspect the value:\n# echo something\nThe operator should review.",
      "rationale": "Manual review required."
    }
  ]
}`

// === Pattern 4: stat 권한 검증 — Access octal mode + Uid 0/root ===
const cisExpectStatPermFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "7.1.1",
      "title": "Ensure permissions on /etc/passwd are configured",
      "assessment_status": "Automated",
      "audit": "Run the following command to verify access to /etc/passwd is 0644 or more restrictive, Uid is 0/root and Gid is 0/root.\n# stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: ( %g/ %G)' /etc/passwd\nAccess: (0644/-rw-r--r--) Uid: ( 0/ root) Gid: ( 0/ root)"
    }
  ]
}`

// === Pattern 5: sshd -T grep — set to yes/no ===
const cisExpectSSHDOptionFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "5.1.11",
      "title": "Ensure sshd IgnoreRhosts is enabled",
      "assessment_status": "Automated",
      "audit": "Run the following command to verify IgnoreRhosts is set to yes:\n# sshd -T | grep ignorerhosts\nignorerhosts yes"
    },
    {
      "id": "5.1.9",
      "title": "Ensure sshd GSSAPIAuthentication is disabled",
      "assessment_status": "Automated",
      "audit": "Run the following command to verify GSSAPIAuthentication is set to no:\n# sshd -T | grep gssapiauthentication\ngssapiauthentication no"
    }
  ]
}`

// === Pattern 6: sshd -T grep — 수치 ≤ N / > 0 ===
const cisExpectSSHDNumericFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "5.1.16",
      "title": "Ensure sshd MaxAuthTries is configured",
      "assessment_status": "Automated",
      "audit": "Run the following command and verify that MaxAuthTries is 4 or less:\n# sshd -T | grep maxauthtries\nmaxauthtries 4"
    },
    {
      "id": "5.1.7",
      "title": "Ensure sshd ClientAliveInterval and ClientAliveCountMax are configured",
      "assessment_status": "Automated",
      "audit": "Run the following command and verify ClientAliveInterval and ClientAliveCountMax are greater than zero:\n# sshd -T | grep -Pi -- '(clientaliveinterval|clientalivecountmax)'\nclientaliveinterval 15\nclientalivecountmax 3"
    }
  ]
}`

// === Pattern 7: 'should not be returned' — explicit negative validation (직접 표현) ===
const cisExpectShouldNotBeReturnedFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "X.99.1",
      "title": "Ensure weak option not in use (synthetic)",
      "assessment_status": "Automated",
      "audit": "Verify the option output:\n# echo weakopt\nThe weak option should not be in use."
    }
  ]
}`

// === Pattern 8: multi-line shell line — sshd | grep PCRE regex가 PDF rendering으로 분할 ===
// 5.1.6 Ciphers · 5.1.15 MACs · 5.1.12 KexAlgorithms 형식. extractCISLastShellLine이
// 첫 줄 `# sshd -T | grep -Pi --` (dangling --) 또는 `# sshd ... '...regex...` (unmatched ')
// 만 추출하면 grep 인자 누락 → 빈 출력 → false PASS. multi-line 흡수 후 정확 변환.
const cisExpectMultiLineCipherFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "5.1.6.synth",
      "title": "Ensure sshd Ciphers are configured (synthetic multi-line)",
      "assessment_status": "Automated",
      "audit": "Run the following command to verify none of the weak ciphers are in use:\n# sshd -T | grep -Pi --\n'^ciphers\\h+\\\"?([^#\\n\\r]+,)?((3des|blowfish|cast128|aes(128|192|256))-\ncbc|arcfour(128|256)?|chacha20-\npoly1305@openssh\\.com)\\b'\nNo ciphers in the list below should be returned as they are weak.\n3des-cbc\naes128-cbc"
    }
  ]
}`

// === Pattern 9: multi-line shell line — dangling -- with quoted regex on next line ===
const cisExpectMultiLineKexFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "5.1.12.synth",
      "title": "Ensure sshd KexAlgorithms is configured (synthetic multi-line)",
      "assessment_status": "Automated",
      "audit": "Verify weak Key Exchange:\n# sshd -T | grep -Pi -- 'kexalgorithms\\h+([^#\\n\\r]+,)?(diffie-hellman-group1-\nsha1|diffie-hellman-group14-sha1|diffie-hellman-group-exchange-sha1)\\b'\nNothing should be returned"
    }
  ]
}`

// === audit에 PASS 마커 있지만 hashbang 없음 — fallback ===
const cisMarkerNoHashbangFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {
      "id": "X.1",
      "title": "Marker but no hashbang",
      "assessment_status": "Automated",
      "audit": "Just text with ** PASS ** mention but no bash"
    }
  ]
}`

// === Mixed — 통계 검증 ===
const cisMixedFixture = `{
  "benchmark": "CIS",
  "version": "1.0",
  "items": [
    {"id":"A","title":"automated good","assessment_status":"Automated","audit":"#!/usr/bin/env bash\n{\n  printf '** PASS **'\n}"},
    {"id":"B","title":"manual","assessment_status":"Manual","audit":"manual text"},
    {"id":"C","title":"no marker","assessment_status":"Automated","audit":"# echo hello\nhello"},
    {"id":"D","title":"marker no hashbang","assessment_status":"Automated","audit":"** PASS ** but no bash"}
  ]
}`

// === E12 T1 — 자동 변환 케이스 ===
func TestConvertCISAutomated(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisAutomatedFixture), converter.CISConvertOptions{
		PackName: "cis-ubuntu", PackVersion: "1.0.0", PackVendor: "rosshield",
	})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.TotalItems != 1 || report.Converted != 1 || len(report.Degraded) != 0 {
		t.Errorf("report = %+v, want {Total:1 Converted:1 Degraded:[]}", report)
	}
	c := pack.Checks[0]
	if c.ID != "1.1.1.1" {
		t.Errorf("ID = %q", c.ID)
	}
	// auditCommand는 bash -c '<hashbang부터 끝까지>'로 wrap.
	if !strings.HasPrefix(c.AuditCommand, "bash -c '") {
		t.Errorf("AuditCommand should start with bash -c ': %q", c.AuditCommand[:50])
	}
	if !strings.Contains(c.AuditCommand, "#!/usr/bin/env bash") {
		t.Errorf("AuditCommand missing hashbang: %q", c.AuditCommand)
	}
	if !strings.Contains(c.AuditCommand, "** PASS **") {
		t.Errorf("AuditCommand missing PASS marker emission")
	}
	// 자연어 prefix는 제거됨.
	if strings.Contains(c.AuditCommand, "Run the following script to verify") {
		t.Errorf("AuditCommand should NOT contain natural-language prefix")
	}
	if string(c.EvaluationRule) != `{"op":"contains","value":"** PASS **"}` {
		t.Errorf("EvaluationRule = %s", c.EvaluationRule)
	}
}

// TestConvertCISExpectEmptyPatternAutoConverts는 "Nothing should be returned" 자연어 +
// 마지막 # <cmd> 라인 추출이 PASS/FAIL 마커 없이 자동 변환되는지 검증합니다.
func TestConvertCISExpectEmptyPatternAutoConverts(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisExpectEmptyFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 1 || report.DegradedNoMarker != 0 || report.DegradedManual != 0 {
		t.Errorf("report = %+v, want Converted:1", report)
	}
	c := pack.Checks[0]
	if !strings.HasPrefix(c.AuditCommand, "bash -c '") {
		t.Errorf("AuditCommand should be bash-wrapped: %q", c.AuditCommand[:min(50, len(c.AuditCommand))])
	}
	// 합성된 bash는 cmd 출력이 비어 있을 때 PASS, 비어 있지 않으면 FAIL.
	if !strings.Contains(c.AuditCommand, "findmnt -kn /tmp | grep -v nodev") {
		t.Errorf("AuditCommand missing extracted shell line: %q", c.AuditCommand)
	}
	if !strings.Contains(c.AuditCommand, "** PASS **") || !strings.Contains(c.AuditCommand, "** FAIL **") {
		t.Errorf("AuditCommand should emit both PASS and FAIL branches")
	}
	if !strings.Contains(c.AuditCommand, "[ -z") {
		t.Errorf("AuditCommand should test for empty output")
	}
	if string(c.EvaluationRule) != `{"op":"contains","value":"** PASS **"}` {
		t.Errorf("EvaluationRule = %s", c.EvaluationRule)
	}
}

// TestConvertCISExpectStatPermAutoConverts는 stat 권한 가이드(`# stat -Lc ... /etc/passwd` +
// "0644 or more restrictive, Uid is 0/root")가 octal mode 비교 + Uid grep 합성으로
// 자동 변환되는지 검증.
func TestConvertCISExpectStatPermAutoConverts(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisExpectStatPermFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 1 || report.DegradedNoMarker != 0 {
		t.Errorf("report = %+v, want Converted:1", report)
	}
	c := pack.Checks[0]
	if !strings.Contains(c.AuditCommand, "stat -Lc") {
		t.Errorf("AuditCommand missing extracted stat line: %q", c.AuditCommand)
	}
	// 합성된 bash는 audit에서 추출한 첫 octal mode("0644")로 8진수 비교.
	if !strings.Contains(c.AuditCommand, "8#0644") {
		t.Errorf("AuditCommand missing 8# octal compare with 0644: %q", c.AuditCommand)
	}
	if !strings.Contains(c.AuditCommand, "Uid: (") {
		t.Errorf("AuditCommand missing Uid: ( root check: %q", c.AuditCommand)
	}
}

// TestConvertCISExpectSSHDOptionAutoConverts는 sshd -T grep + "set to yes/no" 패턴이
// 마지막 토큰 비교 합성으로 자동 변환되는지 검증.
func TestConvertCISExpectSSHDOptionAutoConverts(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisExpectSSHDOptionFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 2 || report.DegradedNoMarker != 0 {
		t.Errorf("report = %+v, want Converted:2", report)
	}
	// 5.1.11: expected yes
	c1 := pack.Checks[0]
	if !strings.Contains(c1.AuditCommand, "sshd -T | grep ignorerhosts") {
		t.Errorf("5.1.11 missing extracted sshd line: %q", c1.AuditCommand)
	}
	if !strings.Contains(c1.AuditCommand, `"$val" = "yes"`) {
		t.Errorf("5.1.11 expected val == yes compare: %q", c1.AuditCommand)
	}
	// 5.1.9: expected no
	c2 := pack.Checks[1]
	if !strings.Contains(c2.AuditCommand, "sshd -T | grep gssapiauthentication") {
		t.Errorf("5.1.9 missing extracted sshd line: %q", c2.AuditCommand)
	}
	if !strings.Contains(c2.AuditCommand, `"$val" = "no"`) {
		t.Errorf("5.1.9 expected val == no compare: %q", c2.AuditCommand)
	}
}

// TestConvertCISExpectSSHDNumericPatternAutoConverts는 sshd -T grep + "is N or less" /
// "greater than zero" 패턴이 모든 출력 라인 마지막 토큰의 정수 비교로 자동 변환되는지 검증.
func TestConvertCISExpectSSHDNumericPatternAutoConverts(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisExpectSSHDNumericFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 2 || report.DegradedNoMarker != 0 {
		t.Errorf("report = %+v, want Converted:2", report)
	}
	// 5.1.16: MaxAuthTries ≤ 4
	c1 := pack.Checks[0]
	if !strings.Contains(c1.AuditCommand, "sshd -T | grep maxauthtries") {
		t.Errorf("5.1.16 missing extracted sshd line: %q", c1.AuditCommand)
	}
	if !strings.Contains(c1.AuditCommand, `"$val" -le 4`) {
		t.Errorf("5.1.16 missing -le 4 compare: %q", c1.AuditCommand)
	}
	// 5.1.7: ClientAlive* > 0 (multi-line grep)
	c2 := pack.Checks[1]
	if !strings.Contains(c2.AuditCommand, "clientaliveinterval|clientalivecountmax") {
		t.Errorf("5.1.7 missing extracted multi-option grep: %q", c2.AuditCommand)
	}
	if !strings.Contains(c2.AuditCommand, `"$val" -gt 0`) {
		t.Errorf("5.1.7 missing -gt 0 compare: %q", c2.AuditCommand)
	}
	// 비정수 마지막 토큰 즉시 FAIL 보호 — case 분기로 표현
	if !strings.Contains(c2.AuditCommand, "*[!0-9]*") {
		t.Errorf("5.1.7 missing non-integer guard: %q", c2.AuditCommand)
	}
}

// TestConvertCISExpectShouldNotBeReturnedAutoConverts는 "should not be in use" 직접 표현이
// expect-empty 분기로 자동 변환되는지 검증.
//
// 비포함(별 epic): "No <subject>... should be returned" 형태 (5.1.6 Ciphers / 5.1.15 MACs) —
// audit shell line이 multi-line line-continuation이라 extractCISLastShellLine이 첫 줄만 추출
// → grep 인자 누락 false PASS 위험. multi-line cmd 추출 epic 후 정규식 확장 안전.
func TestConvertCISExpectShouldNotBeReturnedAutoConverts(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisExpectShouldNotBeReturnedFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 1 || report.DegradedNoMarker != 0 {
		t.Errorf("report = %+v, want Converted:1", report)
	}
	c := pack.Checks[0]
	if !strings.Contains(c.AuditCommand, "[ -z") {
		t.Errorf("should use expect-empty branch: %q", c.AuditCommand)
	}
}

// TestConvertCISMultiLineCipherAutoConverts는 multi-line `# sshd -T | grep -Pi --` (dangling
// `--`) + 다음 줄 quoted regex가 흡수되어 grep 인자 누락 없이 변환되는지 검증.
func TestConvertCISMultiLineCipherAutoConverts(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisExpectMultiLineCipherFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 1 || report.DegradedNoMarker != 0 {
		t.Errorf("report = %+v, want Converted:1", report)
	}
	c := pack.Checks[0]
	// 흡수된 cmd는 첫 줄 `sshd -T | grep -Pi --` + 후속 quoted regex token이 join되어 있어야.
	if !strings.Contains(c.AuditCommand, "sshd -T | grep -Pi --") {
		t.Errorf("missing first line: %q", c.AuditCommand)
	}
	if !strings.Contains(c.AuditCommand, "ciphers") {
		t.Errorf("missing absorbed regex content (ciphers): %q", c.AuditCommand)
	}
	// regex 분할 token "aes(128|192|256))-" + "cbc" 가 no-space join으로 "aes(128|192|256))-cbc" 복원.
	if !strings.Contains(c.AuditCommand, "aes(128|192|256))-cbc") {
		t.Errorf("hyphen-broken token not rejoined as -cbc: %q", c.AuditCommand)
	}
	// "No ciphers ... should be returned"가 expect-empty로 잡혀 [ -z ] 분기 합성.
	if !strings.Contains(c.AuditCommand, "[ -z") {
		t.Errorf("should use expect-empty branch: %q", c.AuditCommand)
	}
}

// TestConvertCISMultiLineKexAlgorithmsAutoConverts는 dangling `--` + quoted regex가 같은
// 줄에서 시작 후 다음 줄로 이어지는 케이스 (5.1.12 형식) 검증.
func TestConvertCISMultiLineKexAlgorithmsAutoConverts(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisExpectMultiLineKexFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 1 || report.DegradedNoMarker != 0 {
		t.Errorf("report = %+v, want Converted:1", report)
	}
	c := pack.Checks[0]
	if !strings.Contains(c.AuditCommand, "kexalgorithms") {
		t.Errorf("missing kexalgorithms regex: %q", c.AuditCommand)
	}
	if !strings.Contains(c.AuditCommand, "group1-sha1") {
		t.Errorf("hyphen-split token group1-sha1 not rejoined: %q", c.AuditCommand)
	}
	if !strings.Contains(c.AuditCommand, "[ -z") {
		t.Errorf("should use expect-empty branch (Nothing should be returned): %q", c.AuditCommand)
	}
}

// TestConvertCISExpectInstalledPatternAutoConverts는 "is installed" 긍정 기대 패턴이
// `[ -n ]` 분기로 자동 변환되는지 검증합니다.
func TestConvertCISExpectInstalledPatternAutoConverts(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisExpectInstalledFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 1 {
		t.Errorf("report = %+v, want Converted:1", report)
	}
	c := pack.Checks[0]
	if !strings.Contains(c.AuditCommand, "dpkg-query -s apparmor") {
		t.Errorf("AuditCommand missing dpkg-query line: %q", c.AuditCommand)
	}
	if !strings.Contains(c.AuditCommand, "[ -n") {
		t.Errorf("AuditCommand should test for non-empty output")
	}
}

func TestConvertCISManualBecomesDegraded(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisManualFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 0 || report.DegradedManual != 1 {
		t.Errorf("report = %+v, want Converted:0 DegradedManual:1", report)
	}
	if len(report.Degraded) != 1 || !strings.Contains(report.Degraded[0], "Manual") {
		t.Errorf("Degraded = %v", report.Degraded)
	}
	c := pack.Checks[0]
	if c.AuditCommand != "true" {
		t.Errorf("AuditCommand = %q, want \"true\"", c.AuditCommand)
	}
	// rationale·fixGuidance는 보존되어 사용자가 수동 검수 가이드.
	if c.Rationale == "" {
		t.Error("Rationale lost — manual review에 필요")
	}
}

func TestConvertCISNoMarkerBecomesDegraded(t *testing.T) {
	t.Parallel()
	_, report, err := converter.ConvertCIS([]byte(cisNoMarkerFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 0 || report.DegradedNoMarker != 1 {
		t.Errorf("report = %+v, want Converted:0 DegradedNoMarker:1", report)
	}
	if !strings.Contains(report.Degraded[0], "PASS") {
		t.Errorf("degraded reason: %q", report.Degraded[0])
	}
}

func TestConvertCISMarkerWithoutHashbangBecomesDegraded(t *testing.T) {
	t.Parallel()
	_, report, err := converter.ConvertCIS([]byte(cisMarkerNoHashbangFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 0 || report.DegradedNoMarker != 1 {
		t.Errorf("report = %+v", report)
	}
	if !strings.Contains(report.Degraded[0], "hashbang") {
		t.Errorf("degraded reason: %q", report.Degraded[0])
	}
}

func TestConvertCISMixedFixtureStatistics(t *testing.T) {
	t.Parallel()
	pack, report, err := converter.ConvertCIS([]byte(cisMixedFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.TotalItems != 4 {
		t.Errorf("TotalItems = %d, want 4", report.TotalItems)
	}
	if report.Converted != 1 {
		t.Errorf("Converted = %d, want 1 (only A)", report.Converted)
	}
	if report.DegradedManual != 1 {
		t.Errorf("DegradedManual = %d, want 1 (B)", report.DegradedManual)
	}
	if report.DegradedNoMarker != 2 {
		t.Errorf("DegradedNoMarker = %d, want 2 (C·D)", report.DegradedNoMarker)
	}
	if len(pack.Checks) != 4 {
		t.Errorf("checks = %d, want 4 (모든 item이 출력에 포함)", len(pack.Checks))
	}
}

func TestConvertCISRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	_, _, err := converter.ConvertCIS([]byte(`{not-json`), converter.CISConvertOptions{})
	if !errors.Is(err, converter.ErrCISDecodeFailed) {
		t.Errorf("err = %v, want ErrCISDecodeFailed", err)
	}
}

func TestConvertCISRejectsNoItems(t *testing.T) {
	t.Parallel()
	_, _, err := converter.ConvertCIS([]byte(`{"items":[]}`), converter.CISConvertOptions{})
	if !errors.Is(err, converter.ErrCISNoItems) {
		t.Errorf("err = %v, want ErrCISNoItems", err)
	}
}

// === T1 통합 — 변환 결과가 benchmark 로더로 라운드트립 ===
func TestConvertCISRoundTripsThroughBenchmarkLoader(t *testing.T) {
	t.Parallel()
	pack, _, err := converter.ConvertCIS([]byte(cisMixedFixture), converter.CISConvertOptions{
		PackName: "cis-test", PackVersion: "1.0.0", PackVendor: "rosshield",
	})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out")
	if err := converter.WriteToDir(pack, out); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}
	packYAML, err := os.ReadFile(filepath.Join(out, "pack.yaml"))
	if err != nil {
		t.Fatalf("read pack.yaml: %v", err)
	}
	if _, err := benchmark.ParsePackYAML(packYAML); err != nil {
		t.Fatalf("ParsePackYAML: %v", err)
	}
	for _, c := range pack.Checks {
		data, err := os.ReadFile(filepath.Join(out, "checks", c.ID+".yaml"))
		if err != nil {
			t.Fatalf("read check %s: %v", c.ID, err)
		}
		check, err := benchmark.ParseCheckYAML(data)
		if err != nil {
			t.Fatalf("ParseCheckYAML %s: %v", c.ID, err)
		}
		if _, err := benchmark.ParseEvalRule(check.EvaluationRule); err != nil {
			t.Errorf("ParseEvalRule %s: %v", c.ID, err)
		}
	}
}

// === T1 — 실 nrobotcheck CIS Ubuntu 24.04 JSON e2e (옵트인) ===
func TestConvertCISRealUbuntu2404(t *testing.T) {
	dir := os.Getenv("ROSSHIELD_NROBOTCHECK_DIR")
	if dir == "" {
		t.Skip("set ROSSHIELD_NROBOTCHECK_DIR to enable e2e (real 1.1MB CIS JSON)")
	}
	path := filepath.Join(dir, "resources", "baselines", "cis_ubuntu_2404_benchmark.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("CIS Ubuntu JSON not found: %v", err)
	}
	jsonBytes := stripBOM(data)

	pack, report, err := converter.ConvertCIS(jsonBytes, converter.CISConvertOptions{
		PackName: "cis-ubuntu-2404", PackVersion: "1.0.0", PackVendor: "rosshield",
	})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	t.Logf("CIS Ubuntu 24.04: total=%d converted=%d (%.1f%% auto), degraded[Manual=%d, NoMarker=%d]",
		report.TotalItems, report.Converted,
		float64(report.Converted)/float64(report.TotalItems)*100,
		report.DegradedManual, report.DegradedNoMarker)
	if report.TotalItems != 312 {
		t.Errorf("TotalItems = %d, want 312", report.TotalItems)
	}
	if report.Converted < 50 {
		t.Errorf("Converted = %d, want ≥ 50 (R8-3' 예상 ~61)", report.Converted)
	}
	// 라운드트립.
	out := filepath.Join(t.TempDir(), "cis-ubuntu-2404")
	if err := converter.WriteToDir(pack, out); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}
	packYAML, _ := os.ReadFile(filepath.Join(out, "pack.yaml"))
	if _, err := benchmark.ParsePackYAML(packYAML); err != nil {
		t.Errorf("loaded pack invalid: %v", err)
	}
}

// === EvaluationRule이 production AST로도 파싱 가능 ===
func TestConvertCISEvalRuleParsableByBenchmark(t *testing.T) {
	t.Parallel()
	pack, _, err := converter.ConvertCIS([]byte(cisAutomatedFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	c := pack.Checks[0]
	node, err := benchmark.ParseEvalRule(json.RawMessage(c.EvaluationRule))
	if err != nil {
		t.Fatalf("ParseEvalRule: %v", err)
	}
	if node == nil {
		t.Fatal("node = nil")
	}
}

// === extractCISBashBody helper 검증 (간접 — Automated 케이스로) ===
func TestConvertCISStripsNaturalLanguagePrefix(t *testing.T) {
	t.Parallel()
	pack, _, err := converter.ConvertCIS([]byte(cisAutomatedFixture), converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	cmd := pack.Checks[0].AuditCommand
	// 자연어 prefix("Run the following script to verify")는 절대 포함되지 않아야 — bash -c가 syntax error 일으킴.
	if strings.Contains(cmd, "Run the following") {
		t.Errorf("natural-language prefix leaked into AuditCommand")
	}
}
