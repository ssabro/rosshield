package converter_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ssabro/rosshield/cmd/pack-tools/converter"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

// E12 Stage D · T4 — 통합: convert → archive → benchmark.LoadPackFromTar 라운드트립.
//
// pack-tools 전체 파이프라인이 production loader가 그대로 수용하는 archive를 만드는지
// 검증. CIS·ROS2 두 변환기 모두 같은 표면을 통과해야 한다.

// CIS 합성 fixture 1건만으로 전체 파이프라인을 흐른다 — convert → write → archive → load.
func TestIntegrationCISConvertArchiveLoad(t *testing.T) {
	cisJSON := []byte(`{
		"benchmark": "CIS Ubuntu Linux 24.04 LTS Benchmark",
		"version": "1.0.0",
		"date": "2024-09-01",
		"items": [
			{
				"id": "1.1.1.1",
				"title": "Ensure cramfs kernel module is not available",
				"assessment_status": "Automated",
				"description": "cramfs is not needed",
				"rationale": "Reduces attack surface",
				"audit": "Run the following script to verify:\n#!/usr/bin/env bash\n{ if ! lsmod | grep -E '^cramfs' >/dev/null; then printf '%s\n' '** PASS **'; else printf '%s\n' '** FAIL **'; fi; }",
				"remediation": "Add modprobe blacklist."
			}
		]
	}`)
	pack, report, err := converter.ConvertCIS(cisJSON, converter.CISConvertOptions{})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	if report.Converted != 1 {
		t.Fatalf("Converted = %d, want 1", report.Converted)
	}

	// disk → archive → load 순서.
	dir := filepath.Join(t.TempDir(), "out")
	if err := converter.WriteToDir(pack, dir); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	data, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	loaded, err := benchmark.LoadPackFromTar(data, pub)
	if err != nil {
		t.Fatalf("LoadPackFromTar: %v", err)
	}

	if loaded.Vendor != "rosshield" || loaded.Name != "cis-ubuntu-2404" {
		t.Fatalf("pack meta drift: %+v", loaded)
	}
	if len(loaded.Checks) != 1 {
		t.Fatalf("checks len=%d, want 1", len(loaded.Checks))
	}
	if loaded.Checks[0].CheckID != "1.1.1.1" {
		t.Fatalf("check ID=%q, want 1.1.1.1", loaded.Checks[0].CheckID)
	}
	if loaded.Checks[0].Severity != benchmark.SeverityMedium {
		t.Fatalf("severity=%s, want medium (CIS default)", loaded.Checks[0].Severity)
	}

	// EvaluationRule이 production AST로 평가되어야 한다.
	rule, err := benchmark.ParseEvalRule(loaded.Checks[0].EvaluationRule)
	if err != nil {
		t.Fatalf("ParseEvalRule: %v", err)
	}
	out, err := rule.Eval(benchmark.EvalInput{Stdout: " ** PASS ** ", ExitCode: 0})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if out.Status != benchmark.StatusPass {
		t.Fatalf("Eval=%s, want PASS", out.Status)
	}
}

// ROS2 합성 fixture로 동일 파이프라인 — pass_logic AND/NOT 트리·multi-condition까지.
func TestIntegrationROS2ConvertArchiveLoad(t *testing.T) {
	ros2JSON := []byte(`{
		"schema_version": "1.0",
		"items": [
			{
				"id": "ROS2-001",
				"name": "SSH 활성화",
				"name_en": "SSH enabled",
				"severity": "high",
				"description": "ssh service must be enabled",
				"rationale": "remote management",
				"is_auto": true,
				"audit_command": {
					"conditions": [
						{
							"id": "C1",
							"command": "systemctl is-enabled ssh",
							"value_type": "string",
							"operator": "equals",
							"expected": "enabled"
						}
					]
				}
			}
		]
	}`)
	pack, report, err := converter.ConvertROS2(ros2JSON, converter.ROS2ConvertOptions{
		PackName: "ros2-jazzy", PackVersion: "1.0.0", PreferEnglish: true,
	})
	if err != nil {
		t.Fatalf("ConvertROS2: %v", err)
	}
	if report.Converted != 1 {
		t.Fatalf("Converted=%d, want 1; degraded=%v", report.Converted, report.Degraded)
	}

	dir := filepath.Join(t.TempDir(), "out")
	if err := converter.WriteToDir(pack, dir); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	data, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	loaded, err := benchmark.LoadPackFromTar(data, pub)
	if err != nil {
		t.Fatalf("LoadPackFromTar: %v", err)
	}
	if loaded.Checks[0].Severity != benchmark.SeverityHigh {
		t.Fatalf("severity=%s, want high (ROS2 입력)", loaded.Checks[0].Severity)
	}
	if loaded.Checks[0].Title != "SSH enabled" {
		t.Fatalf("title=%q, want 'SSH enabled' (PreferEnglish=true)", loaded.Checks[0].Title)
	}
}

// degraded check도 archive에 포함되어 production loader가 수용해야 한다 — production
// `RunPackSelfTests`가 fixture 부재로 Degraded=true 분류, 사용자가 수동 보강하기 전까지는
// 해당 check 결과를 신뢰하지 않음.
func TestIntegrationDegradedChecksLoadable(t *testing.T) {
	pack := converter.Pack{
		Name: "mixed", Version: "1.0.0", Vendor: "rosshield",
		Checks: []converter.Check{
			{ID: "AUTO-1", Title: "auto", Severity: "medium",
				AuditCommand:   "bash -c 'true'",
				EvaluationRule: json.RawMessage(`{"op":"contains","value":"** PASS **"}`)},
			{ID: "DEGR-1", Title: "deg", Severity: "low",
				AuditCommand: "true",
				EvaluationRule: json.RawMessage(
					`{"op":"contains","value":"<degraded — Phase 2 fixture required>"}`),
				Rationale: "needs manual review"},
		},
	}
	dir := filepath.Join(t.TempDir(), "p")
	if err := converter.WriteToDir(pack, dir); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	data, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	loaded, err := benchmark.LoadPackFromTar(data, pub)
	if err != nil {
		t.Fatalf("LoadPackFromTar: %v", err)
	}
	if len(loaded.Checks) != 2 {
		t.Fatalf("checks len=%d, want 2", len(loaded.Checks))
	}

	// degraded check은 fixture가 없으므로 RunCheckSelfTest가 Degraded=true.
	res, err := benchmark.RunCheckSelfTest(loaded.Checks[1], nil)
	if err != nil {
		t.Fatalf("RunCheckSelfTest(degraded): %v", err)
	}
	if !res.Degraded {
		t.Fatalf("degraded check should report Degraded=true (no fixture)")
	}
}

// 실제 nrobotcheck CIS 베이스라인을 변환·archive·load (옵트인) — Phase 1 Exit
// "CIS Ubuntu 팩으로 감사" 흐름의 end-to-end 검증.
func TestIntegrationCISRealEndToEnd(t *testing.T) {
	dir := os.Getenv("ROSSHIELD_NROBOTCHECK_DIR")
	if dir == "" {
		t.Skip("set ROSSHIELD_NROBOTCHECK_DIR to enable e2e (real CIS JSON + full pipeline)")
	}
	path := filepath.Join(dir, "resources", "baselines", "cis_ubuntu_2404_benchmark.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real CIS JSON not found: %v", err)
	}
	jsonBytes := stripBOM(data)

	pack, report, err := converter.ConvertCIS(jsonBytes, converter.CISConvertOptions{
		PackName: "cis-ubuntu-2404", PackVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("ConvertCIS: %v", err)
	}
	t.Logf("CIS Ubuntu 24.04: total=%d converted=%d (%.1f%%)",
		report.TotalItems, report.Converted, float64(report.Converted)/float64(report.TotalItems)*100)

	out := filepath.Join(t.TempDir(), "cis")
	if err := converter.WriteToDir(pack, out); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	archiveBytes, err := converter.BuildArchive(out, priv)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	loaded, err := benchmark.LoadPackFromTar(archiveBytes, pub)
	if err != nil {
		t.Fatalf("LoadPackFromTar: %v", err)
	}
	if len(loaded.Checks) != report.TotalItems {
		t.Errorf("loaded checks=%d, want %d", len(loaded.Checks), report.TotalItems)
	}

	// Self-Test fixture로 자동 변환된 check가 실제 PASS/FAIL로 평가되는지 확인.
	skel := converter.GenerateSelfTestSkeletons(pack)
	t.Logf("self-test skeletons: auto=%d degraded=%d custom=%d",
		len(skel.Skeletons), len(skel.Degraded), len(skel.Custom))

	fixtures := map[string][]byte{}
	for _, sk := range skel.Skeletons {
		fixtures[sk.CheckID] = sk.YAML
	}
	results, err := benchmark.RunPackSelfTests(loaded, fixtures)
	if err != nil {
		t.Fatalf("RunPackSelfTests: %v", err)
	}
	autoPassed, degraded := 0, 0
	for _, r := range results {
		if r.Degraded {
			degraded++
			continue
		}
		if !r.AllPassed() {
			t.Errorf("self-test failed for %s: failures=%v", r.CheckID, r.Failures)
			continue
		}
		autoPassed++
	}
	t.Logf("self-test summary: autoPassed=%d degraded=%d (총 %d)", autoPassed, degraded, len(results))
	if autoPassed != report.Converted {
		t.Errorf("autoPassed=%d, want %d (Converted과 일치)", autoPassed, report.Converted)
	}
}
