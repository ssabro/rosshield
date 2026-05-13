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
