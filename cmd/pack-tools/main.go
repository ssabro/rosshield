// pack-tools — rosshield 벤치마크 팩 변환·서명 CLI (E12).
//
// 서브커맨드:
//
//	convert   외부 baseline JSON을 rosshield pack 디렉터리로 변환 (Stage B·C)
//	archive   pack 디렉터리를 MANIFEST + SIGNATURE + tar.gz로 묶음 (Stage D)
//
// Phase 1 Exit는 "CIS Ubuntu 팩으로 감사"가 필수 — 본 도구가 nrobotcheck baseline을
// rosshield pack format으로 변환하는 entry point (`docs/design/12-*` §12.4).
package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"os"

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

사용법:
  pack-tools convert -input <baseline.json> -format <ros2-framework-v1|cis-ubuntu-json-v1> -output <dir>
                     [-vendor <s>] [-pack-name <s>] [-pack-version <s>] [-description <s>]
  pack-tools archive -input <dir> -signer-key <ed25519.key> -output <pack>.tar.gz

archive 옵션:
  -signer-key  raw 64-byte Ed25519 private key 파일
               (생성: openssl genpkey -algorithm ed25519 후 raw bytes 추출,
                또는 internal/platform/signer/soft.LoadOrCreatePrivateKey 결과 키 파일)`)
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
	if err := converter.WriteToDir(pack, outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "convert: write output: %v\n", err)
		return 1
	}

	fmt.Printf("CIS Ubuntu 변환 완료: %s\n", outputDir)
	fmt.Printf("  total: %d, auto-converted: %d (%.1f%% auto)\n",
		report.TotalItems, report.Converted,
		float64(report.Converted)/float64(report.TotalItems)*100)
	fmt.Printf("  degraded: Manual=%d, NoMarker=%d (Phase 2 fixture 필요)\n",
		report.DegradedManual, report.DegradedNoMarker)
	return 0
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
