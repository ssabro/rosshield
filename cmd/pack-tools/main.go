// pack-tools — rosshield 벤치마크 팩 변환·서명 CLI (E12).
//
// 서브커맨드:
//
//	convert   외부 baseline JSON을 rosshield pack 디렉터리로 변환 (Stage B·C)
//	archive   pack 디렉터리를 MANIFEST + SIGNATURE + tar.gz로 묶음 (Stage D)
//	keygen    Ed25519 keypair 생성 — pack 서명용 (raw 64-byte private + hex public)
//	docs      degraded(자동 변환 안 된) 항목들 markdown 가이드 생성 — 운영자 수동 변환 도움
//
// Phase 1 Exit는 "CIS Ubuntu 팩으로 감사"가 필수 — 본 도구가 nrobotcheck baseline을
// rosshield pack format으로 변환하는 entry point (`docs/design/12-*` §12.4).
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ssabro/rosshield/cmd/pack-tools/converter"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "convert":
		return runConvert(args[1:])
	case "archive":
		return runArchive(args[1:])
	case "keygen":
		return runKeygen(args[1:])
	case "docs":
		return runDocs(args[1:])
	case "-h", "--help", "help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "pack-tools: unknown subcommand %q\n\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `pack-tools — rosshield 벤치마크 팩 변환·서명 도구 (E12)

서브커맨드:
  convert   외부 baseline JSON을 rosshield pack 디렉터리로 변환 (Stage B·C)
  archive   pack 디렉터리를 MANIFEST + SIGNATURE + tar.gz로 묶음 (Stage D)
  keygen    Ed25519 keypair 생성 — pack 서명용 (raw 64-byte private + hex public)
  docs      degraded(자동 변환 안 된) 항목들 markdown 가이드 생성

사용법:
  pack-tools convert -input <baseline.json> -format <ros2-framework-v1|cis-ubuntu-json-v1> -output <dir>
                     [-vendor <s>] [-pack-name <s>] [-pack-version <s>] [-description <s>]
  pack-tools archive -input <dir> -signer-key <ed25519.key> -output <pack>.tar.gz
  pack-tools keygen  -out <signer.key> [-pub-out <signer.pub.hex>] [-force]
  pack-tools docs    -input <baseline.json> -format <cis-ubuntu-json-v1> -output <docs.md>

archive 옵션:
  -signer-key  raw 64-byte Ed25519 private key 파일 (pack-tools keygen 또는
               internal/platform/signer/soft.LoadOrCreatePrivateKey 결과)

keygen 옵션:
  -out         private key 출력 경로 (raw 64 bytes, 0o600)
  -pub-out     public key 출력 경로 (hex string + LF, 옵션)
  -force       기존 파일 덮어쓰기 (default false)`)
}

func runConvert(args []string) int {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	var (
		input         = fs.String("input", "", "baseline JSON 입력 경로 (필수)")
		format        = fs.String("format", "", "변환 포맷: ros2-framework-v1 | cis-ubuntu-json-v1 (필수)")
		output        = fs.String("output", "", "출력 디렉터리 — 존재하면 거부 (필수)")
		vendor        = fs.String("vendor", "rosshield", "pack metadata.vendor")
		packName      = fs.String("pack-name", "", "pack metadata.name (미지정 시 format별 fallback)")
		packVersion   = fs.String("pack-version", "1.0.0", "pack metadata.version")
		description   = fs.String("description", "", "pack metadata.description")
		preferEnglish = fs.Bool("english", false, "ros2-framework-v1: 영어 필드(name_en/description_en/...) 우선")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *input == "" || *format == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "convert: -input·-format·-output 모두 필수")
		fs.Usage()
		return 2
	}

	switch *format {
	case "ros2-framework-v1":
		return runConvertROS2(*input, *output, converter.ROS2ConvertOptions{
			PackName: *packName, PackVersion: *packVersion, PackVendor: *vendor,
			PackDescription: *description, PreferEnglish: *preferEnglish,
		})
	case "cis-ubuntu-json-v1":
		return runConvertCIS(*input, *output, converter.CISConvertOptions{
			PackName: *packName, PackVersion: *packVersion, PackVendor: *vendor,
			PackDescription: *description,
		})
	default:
		fmt.Fprintf(os.Stderr, "convert: unknown format %q (allowed: ros2-framework-v1, cis-ubuntu-json-v1)\n", *format)
		return 2
	}
}

func runConvertROS2(inputPath, outputDir string, opts converter.ROS2ConvertOptions) int {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert: read input: %v\n", err)
		return 1
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:] // utf-8 BOM 제거
	}

	pack, report, err := converter.ConvertROS2(data, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert: %v\n", err)
		return 1
	}
	if err := converter.WriteToDir(pack, outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "convert: write output: %v\n", err)
		return 1
	}

	fmt.Printf("ROS2 framework 변환 완료: %s\n", outputDir)
	fmt.Printf("  total: %d, auto-converted: %d, degraded: %d (%.1f%% auto)\n",
		report.TotalItems, report.Converted, len(report.Degraded),
		float64(report.Converted)/float64(report.TotalItems)*100)
	if len(report.Degraded) > 0 {
		fmt.Println("\nDegraded checks (Phase 2 fixture 필요):")
		for _, d := range report.Degraded {
			fmt.Printf("  - %s\n", d)
		}
	}
	return 0
}

func runConvertCIS(inputPath, outputDir string, opts converter.CISConvertOptions) int {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert: read input: %v\n", err)
		return 1
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:] // utf-8 BOM
	}

	pack, report, err := converter.ConvertCIS(data, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert: %v\n", err)
		return 1
	}
	// D-MAN-1: outputDir 안의 manual fixture 서브디렉토리(checks/manual/, selftest/manual/)는
	// 운영자 수동 작성 — 자동 변환 결과로 덮어쓰면 안 됨. WriteToDir는 outputDir이
	// 존재하면 ErrOutputExists로 거부하므로, manual/ 백업 → 디렉토리 삭제 → WriteToDir →
	// 복원의 round-trip 패턴으로 보존한다.
	manualBackup, err := backupManualFixtures(outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert: backup manual fixtures: %v\n", err)
		return 1
	}
	defer func() {
		if manualBackup != "" {
			_ = os.RemoveAll(manualBackup)
		}
	}()
	if _, err := os.Stat(outputDir); err == nil {
		if err := os.RemoveAll(outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "convert: cleanup output: %v\n", err)
			return 1
		}
	}
	if err := converter.WriteToDir(pack, outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "convert: write output: %v\n", err)
		return 1
	}
	if manualBackup != "" {
		if err := restoreManualFixtures(manualBackup, outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "convert: restore manual fixtures: %v\n", err)
			return 1
		}
	}

	fmt.Printf("CIS Ubuntu 변환 완료: %s\n", outputDir)
	fmt.Printf("  total: %d, auto-converted: %d (%.1f%% auto)\n",
		report.TotalItems, report.Converted,
		float64(report.Converted)/float64(report.TotalItems)*100)
	fmt.Printf("  degraded: Manual=%d, NoMarker=%d (Phase 2 fixture 필요)\n",
		report.DegradedManual, report.DegradedNoMarker)
	return 0
}

// backupManualFixtures는 outputDir 안의 운영자 수동 작성 fixture 디렉토리
// (checks/manual/ + selftest/manual/)를 임시 디렉토리로 복사한다.
//
// outputDir이 없거나 두 디렉토리 모두 없으면 ""(빈 문자열) + nil 반환 — 호출자는
// restore 단계 skip. 임시 디렉토리는 호출자가 RemoveAll로 정리한다.
func backupManualFixtures(outputDir string) (string, error) {
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	subs := []string{
		filepath.Join("checks", "manual"),
		filepath.Join("selftest", "manual"),
	}
	hasAny := false
	for _, sub := range subs {
		if _, err := os.Stat(filepath.Join(outputDir, sub)); err == nil {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return "", nil
	}
	tmp, err := os.MkdirTemp("", "rosshield-pack-manual-*")
	if err != nil {
		return "", err
	}
	for _, sub := range subs {
		src := filepath.Join(outputDir, sub)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyDir(src, filepath.Join(tmp, sub)); err != nil {
			_ = os.RemoveAll(tmp)
			return "", err
		}
	}
	return tmp, nil
}

// restoreManualFixtures는 backupManualFixtures가 만든 임시 디렉토리에서
// outputDir로 manual fixture를 복원한다.
func restoreManualFixtures(backup, outputDir string) error {
	subs := []string{
		filepath.Join("checks", "manual"),
		filepath.Join("selftest", "manual"),
	}
	for _, sub := range subs {
		src := filepath.Join(backup, sub)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(outputDir, sub)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := copyDir(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst, info.Mode())
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyDir(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func runArchive(args []string) int {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	var (
		input     = fs.String("input", "", "변환된 pack 디렉터리 경로 (필수)")
		signerKey = fs.String("signer-key", "", "raw 64-byte Ed25519 private key 파일 (필수)")
		output    = fs.String("output", "", "출력 .tar.gz 경로 (필수)")
		force     = fs.Bool("force", false, "출력 파일이 존재하면 덮어쓰기")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *input == "" || *signerKey == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "archive: -input·-signer-key·-output 모두 필수")
		fs.Usage()
		return 2
	}

	priv, err := loadEd25519PrivateKey(*signerKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "archive: load signer key: %v\n", err)
		return 1
	}

	if !*force {
		if _, err := os.Stat(*output); err == nil {
			fmt.Fprintf(os.Stderr, "archive: output %q already exists (use -force to overwrite)\n", *output)
			return 1
		} else if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "archive: stat output: %v\n", err)
			return 1
		}
	}

	data, err := converter.BuildArchive(*input, priv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "archive: %v\n", err)
		return 1
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "archive: write output: %v\n", err)
		return 1
	}

	fmt.Printf("archive 생성 완료: %s (%d bytes)\n", *output, len(data))
	return 0
}

func runKeygen(args []string) int {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	var (
		out    = fs.String("out", "", "private key 출력 경로 (raw 64 bytes, 필수)")
		pubOut = fs.String("pub-out", "", "public key 출력 경로 (hex string + LF, 옵션)")
		force  = fs.Bool("force", false, "기존 파일 덮어쓰기")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "keygen: -out 필수")
		fs.Usage()
		return 2
	}
	if !*force {
		if _, err := os.Stat(*out); err == nil {
			fmt.Fprintf(os.Stderr, "keygen: %q already exists (use -force)\n", *out)
			return 1
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keygen: GenerateKey: %v\n", err)
		return 1
	}
	if err := os.WriteFile(*out, priv, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "keygen: write private key: %v\n", err)
		return 1
	}
	if *pubOut != "" {
		if err := os.WriteFile(*pubOut, []byte(hex.EncodeToString(pub)+"\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "keygen: write public key: %v\n", err)
			return 1
		}
	}
	fmt.Printf("keygen 완료\n  private: %s (%d bytes)\n  public : %s\n",
		*out, ed25519.PrivateKeySize, hex.EncodeToString(pub))
	if *pubOut != "" {
		fmt.Printf("  public file: %s\n", *pubOut)
	}
	return 0
}

// runDocs는 baseline JSON에서 자동 변환 안 된 항목들의 markdown 가이드 파일을 생성합니다.
//
// 운영자 수동 변환 작업용 — 각 degraded 항목별로 audit·remediation·rationale 정리.
// Manual 항목과 NoMarker 항목을 섹션 분리하고 ID로 정렬.
func runDocs(args []string) int {
	fs := flag.NewFlagSet("docs", flag.ContinueOnError)
	var (
		input  = fs.String("input", "", "baseline JSON 입력 경로 (필수)")
		format = fs.String("format", "cis-ubuntu-json-v1", "변환 포맷 (현재 cis-ubuntu-json-v1만 지원)")
		output = fs.String("output", "", "출력 markdown 경로 (필수)")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "docs: -input·-output 필수")
		fs.Usage()
		return 2
	}
	if *format != "cis-ubuntu-json-v1" {
		fmt.Fprintf(os.Stderr, "docs: format %q 미지원 (현재 cis-ubuntu-json-v1만)\n", *format)
		return 2
	}

	data, err := os.ReadFile(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "docs: read input: %v\n", err)
		return 1
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:] // utf-8 BOM
	}

	degraded, err := converter.ListCISDegraded(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "docs: %v\n", err)
		return 1
	}

	md := renderCISDegradedMarkdown(degraded)
	if err := os.WriteFile(*output, []byte(md), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "docs: write output: %v\n", err)
		return 1
	}

	manualCount := 0
	for _, d := range degraded {
		if strings.Contains(d.Reason, "Manual") {
			manualCount++
		}
	}
	fmt.Printf("docs 생성 완료: %s\n", *output)
	fmt.Printf("  degraded total: %d (Manual: %d, NoMarker: %d)\n",
		len(degraded), manualCount, len(degraded)-manualCount)
	return 0
}

// renderCISDegradedMarkdown은 degraded 항목 list를 운영자 가이드 markdown으로 변환.
//
// 구조: 헤더(생성 시각·통계) → Manual 섹션(assessment_status=Manual) → NoMarker 섹션(자동 변환
// 패턴 미매칭). 각 항목은 ## ID title + 메타 + audit code block + remediation.
func renderCISDegradedMarkdown(degraded []converter.CISDegradedItem) string {
	var b strings.Builder
	manual := make([]converter.CISDegradedItem, 0)
	noMarker := make([]converter.CISDegradedItem, 0)
	for _, d := range degraded {
		if strings.Contains(d.Reason, "Manual") {
			manual = append(manual, d)
		} else {
			noMarker = append(noMarker, d)
		}
	}

	fmt.Fprintf(&b, "# CIS Ubuntu 24.04 — Degraded items 운영자 가이드\n\n")
	fmt.Fprintf(&b, "> 생성: %s · 자동 변환 안 된 항목들의 audit·remediation 정리.\n",
		time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&b, "> 운영자가 각 항목을 수동으로 검토하거나 customer 환경 fixture로 customizing할 때 활용.\n\n")
	fmt.Fprintf(&b, "**통계**: 총 %d건 degraded (Manual: %d / NoMarker: %d).\n\n",
		len(degraded), len(manual), len(noMarker))
	fmt.Fprintf(&b, "---\n\n")

	if len(manual) > 0 {
		fmt.Fprintf(&b, "## Manual review (assessment_status=Manual, %d건)\n\n", len(manual))
		fmt.Fprintf(&b, "CIS 가이드가 명시적으로 manual review를 요구한 항목들. 자동 변환 불가능 — 운영자가 customer 환경 정책에 따라 직접 검증.\n\n")
		for _, d := range manual {
			renderCISDegradedSection(&b, d)
		}
	}

	if len(noMarker) > 0 {
		fmt.Fprintf(&b, "## NoMarker (자동 변환 패턴 미매칭, %d건)\n\n", len(noMarker))
		fmt.Fprintf(&b, "audit text가 9 자동 변환 패턴(PASS marker / Nothing returned / is installed / stat permission / sshd boolean·numeric·range / multi-line cmd / hashbang body wrap / grep verify / awk exact)에 잡히지 않은 항목들. 향후 converter 패턴 확장 또는 수동 fixture 작성으로 cover 가능.\n\n")
		for _, d := range noMarker {
			renderCISDegradedSection(&b, d)
		}
	}
	return b.String()
}

func renderCISDegradedSection(b *strings.Builder, d converter.CISDegradedItem) {
	fmt.Fprintf(b, "### %s — %s\n\n", d.ID, d.Title)
	fmt.Fprintf(b, "**Reason**: `%s`\n\n", d.Reason)
	if d.AssessmentStatus != "" {
		fmt.Fprintf(b, "**Assessment**: %s\n\n", d.AssessmentStatus)
	}
	if len(d.ProfileApplicability) > 0 {
		fmt.Fprintf(b, "**Profile**: %s\n\n", strings.Join(d.ProfileApplicability, ", "))
	}
	if d.Description != "" {
		fmt.Fprintf(b, "**Description**:\n\n%s\n\n", d.Description)
	}
	if d.Rationale != "" {
		fmt.Fprintf(b, "**Rationale**:\n\n%s\n\n", d.Rationale)
	}
	if d.Audit != "" {
		fmt.Fprintf(b, "**Audit guide**:\n\n```\n%s\n```\n\n", d.Audit)
	}
	if d.Remediation != "" {
		fmt.Fprintf(b, "**Remediation**:\n\n```\n%s\n```\n\n", d.Remediation)
	}
	fmt.Fprintf(b, "---\n\n")
}

// loadEd25519PrivateKey는 raw 64-byte Ed25519 private key 파일을 로드합니다.
//
// 형식: ed25519.PrivateKey (seed 32B + public 32B). production
// `internal/platform/signer/soft.LoadOrCreatePrivateKey`가 만드는 파일과 호환.
//
// PEM/PKCS#8 등 외부 형식은 미지원 — 오프라인 도구이므로 단일 형식 강제(설계 단순성).
// PEM이 필요하면 사용자가 외부 도구로 raw bytes 추출 후 사용.
func loadEd25519PrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file %q: %w", path, err)
	}
	if len(data) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("key file %q has size %d, want %d (raw Ed25519 private key)",
			path, len(data), ed25519.PrivateKeySize)
	}
	priv := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	copy(priv, data)
	return priv, nil
}
