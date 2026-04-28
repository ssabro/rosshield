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
	"flag"
	"fmt"
	"os"
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
  pack-tools archive -input <dir> -signer-key <ed25519.pem> -output <pack>.tar.gz`)
}

func runConvert(args []string) int {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	var (
		input       = fs.String("input", "", "baseline JSON 입력 경로 (필수)")
		format      = fs.String("format", "", "변환 포맷: ros2-framework-v1 | cis-ubuntu-json-v1 (필수)")
		output      = fs.String("output", "", "출력 디렉터리 — 존재하면 거부 (필수)")
		vendor      = fs.String("vendor", "rosshield", "pack metadata.vendor")
		packName    = fs.String("pack-name", "", "pack metadata.name (미지정 시 baseline에서 추론)")
		packVersion = fs.String("pack-version", "1.0.0", "pack metadata.version")
		description = fs.String("description", "", "pack metadata.description")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *input == "" || *format == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "convert: -input·-format·-output 모두 필수")
		fs.Usage()
		return 2
	}

	// Stage B (ros2-framework-v1) · Stage C (cis-ubuntu-json-v1) 구현 예정.
	// 현재 Stage A는 골격만 — 실제 변환 로직 미구현.
	fmt.Fprintf(os.Stderr, "convert: format=%q 미구현 (Stage B·C에서 추가)\n", *format)
	_ = input
	_ = vendor
	_ = packName
	_ = packVersion
	_ = description
	return 1
}

func runArchive(args []string) int {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	var (
		input     = fs.String("input", "", "변환된 pack 디렉터리 경로 (필수)")
		signerKey = fs.String("signer-key", "", "Ed25519 PEM 서명 키 경로 (필수)")
		output    = fs.String("output", "", "출력 .tar.gz 경로 (필수)")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *input == "" || *signerKey == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "archive: -input·-signer-key·-output 모두 필수")
		fs.Usage()
		return 2
	}
	// Stage D 구현 예정.
	fmt.Fprintln(os.Stderr, "archive: Stage D 미구현")
	return 1
}
