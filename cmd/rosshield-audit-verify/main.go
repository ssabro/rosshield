// Command rosshield-audit-verify는 외부 감사인용 standalone 검증 도구입니다 (E30, R30-4).
//
// rosshield-server·rosshield CLI 없이 작은 단일 binary만으로 서명된 report tar.gz
// 번들의 무결성·진위를 검증합니다. 본 도구의 존재 의의:
//
//   - GitHub repo가 private 유지되더라도(R30-4 결정) 외부 감사인은 release page에서
//     binary만 다운로드하여 P1 "외부 검증" 요건을 충족할 수 있다.
//   - 도메인 코드(internal/domain/reporting.VerifyBundle)와 같은 module을 공유하므로
//     server와 검증 로직이 byte-identical — drift 위험 0.
//   - stdlib + crypto/ed25519만 사용. 외부 의존 0.
//
// 사용법:
//
//	rosshield-audit-verify --bundle <path.tar.gz> [--format json|table] [--strict]
//
// exit code:
//
//	0  PASS — 모든 단계 통과
//	1  FAIL — 검증 실패 (서명 invalid·entry 부재·tar.gz 손상·anchor malformed)
//	2  ARG  — invalid CLI args (필수 옵션 누락 또는 알 수 없는 --format 값)
//
// --strict는 현 단계에서 결과에 영향 없음 (warning gate가 추가될 미래 호환). 명시 수용.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/ssabro/rosshield/internal/domain/reporting"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// stepResult는 단계별 결과를 사람·기계 모두 읽기 쉽게 노출합니다.
type stepResult struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// verifyOutput은 stdout 와이어 형식 (table·JSON 모두 동일 필드).
type verifyOutput struct {
	OK            bool         `json:"ok"`
	Result        string       `json:"result"` // "PASS" | "FAIL"
	Reason        string       `json:"reason,omitempty"`
	Error         string       `json:"error,omitempty"`
	BundlePath    string       `json:"bundlePath"`
	PDFSize       int64        `json:"pdfSize"`
	PDFSHA256     string       `json:"pdfSha256"`
	SignerKeyID   string       `json:"signerKeyId"`
	ChainHeadSeq  int64        `json:"chainHeadSeq"`
	ChainHeadHash string       `json:"chainHeadHash"`
	Steps         []stepResult `json:"steps"`
}

// run은 args를 받아 exit code를 반환합니다 (테스트 친화 분리).
//
// Stage 5: args[0]가 'rotation'이면 rotation 서브커맨드로 분기. 그 외는 기존 bundle verify.
// rotation 서브커맨드는 cold archive (tar.gz)의 segment_hash + prev_segment_hash chain을
// 검증합니다 — `rotation.go` 참조.
func run(args []string) int {
	if len(args) > 0 && args[0] == "rotation" {
		return runRotation(args[1:])
	}
	fs := flag.NewFlagSet("rosshield-audit-verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // flag 자체 stderr 메시지 억제 — 본 binary 메시지로 통일.
	bundlePath := fs.String("bundle", "", "report tar.gz 번들 경로 (필수)")
	format := fs.String("format", "table", "출력 포맷: table | json")
	strict := fs.Bool("strict", false, "warning을 fail로 처리 (현 단계 no-op)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield-audit-verify: flag parse error: %v\n", err)
		usage()
		return 2
	}
	if *bundlePath == "" {
		fmt.Fprintln(os.Stderr, "rosshield-audit-verify: --bundle <path> is required")
		usage()
		return 2
	}
	if *format != "table" && *format != "json" {
		fmt.Fprintf(os.Stderr, "rosshield-audit-verify: unknown --format %q (allowed: table, json)\n", *format)
		return 2
	}
	_ = *strict // 현 단계 no-op (E30 spec 만족용 placeholder).

	out := verifyOutput{BundlePath: *bundlePath}

	bundleBytes, err := os.ReadFile(*bundlePath)
	if err != nil {
		out.Result = "FAIL"
		out.Reason = fmt.Sprintf("read bundle: %v", err)
		out.Error = err.Error()
		out.Steps = []stepResult{{Name: "read", OK: false, Detail: err.Error()}}
		emitOutput(*format, out)
		return 1
	}
	out.Steps = append(out.Steps, stepResult{Name: "read", OK: true,
		Detail: fmt.Sprintf("%d bytes", len(bundleBytes))})

	res, verr := reporting.VerifyBundle(bundleBytes, nil) // 번들 내 public-key.pem 신뢰
	out.PDFSize = res.PDFSize
	out.PDFSHA256 = res.PDFSHA256
	out.SignerKeyID = res.SignerKeyID
	out.ChainHeadSeq = res.ChainHeadSeq
	out.ChainHeadHash = res.ChainHeadHash

	if verr != nil {
		out.OK = false
		out.Result = "FAIL"
		out.Reason = classifyReason(verr)
		out.Error = verr.Error()
		out.Steps = append(out.Steps, classifyStep(verr))
		emitOutput(*format, out)
		return 1
	}
	if !res.OK {
		out.OK = false
		out.Result = "FAIL"
		out.Reason = res.Reason
		out.Steps = append(out.Steps, stepResult{Name: "verify", OK: false, Detail: res.Reason})
		emitOutput(*format, out)
		return 1
	}

	// PASS — 단계별 success summary.
	out.OK = true
	out.Result = "PASS"
	out.Steps = append(out.Steps,
		stepResult{Name: "extract", OK: true, Detail: "all 4 entries present"},
		stepResult{Name: "publicKey", OK: true, Detail: "PEM decoded (Ed25519)"},
		stepResult{Name: "signature", OK: true, Detail: "ed25519.Verify OK"},
		stepResult{Name: "anchor", OK: true,
			Detail: fmt.Sprintf("seq=%d hash=%s signer=%s",
				res.ChainHeadSeq, res.ChainHeadHash, res.SignerKeyID)},
		stepResult{Name: "evidence", OK: true,
			Detail: fmt.Sprintf("pdf sha256=%s size=%d",
				res.PDFSHA256, res.PDFSize)},
	)
	emitOutput(*format, out)
	return 0
}

// classifyReason은 verifyBundle 에러를 사람 읽기용 reason으로 변환합니다.
func classifyReason(err error) string {
	switch {
	case errors.Is(err, reporting.ErrBundleSignatureInvalid):
		return "signature verify failed"
	case errors.Is(err, reporting.ErrBundleSignatureSize):
		return "signature size invalid"
	case errors.Is(err, reporting.ErrBundlePubKeyMalformed):
		return "public key malformed"
	case errors.Is(err, reporting.ErrBundlePubKeyMismatch):
		return "public key mismatch"
	case errors.Is(err, reporting.ErrBundleMissingPDF):
		return "bundle missing report.pdf"
	case errors.Is(err, reporting.ErrBundleMissingSignature):
		return "bundle missing report.pdf.sig"
	case errors.Is(err, reporting.ErrBundleMissingAnchor):
		return "bundle missing audit-chain-head.json"
	case errors.Is(err, reporting.ErrBundleMissingPubKey):
		return "bundle missing public-key.pem"
	case errors.Is(err, reporting.ErrBundleAnchorMalformed):
		return "anchor JSON malformed"
	default:
		return err.Error()
	}
}

// classifyStep은 verifyBundle 에러를 어느 단계에서 실패했는지 stepResult로 매핑합니다.
func classifyStep(err error) stepResult {
	switch {
	case errors.Is(err, reporting.ErrBundleMissingPDF),
		errors.Is(err, reporting.ErrBundleMissingSignature),
		errors.Is(err, reporting.ErrBundleMissingAnchor),
		errors.Is(err, reporting.ErrBundleMissingPubKey):
		return stepResult{Name: "extract", OK: false, Detail: classifyReason(err)}
	case errors.Is(err, reporting.ErrBundlePubKeyMalformed):
		return stepResult{Name: "publicKey", OK: false, Detail: classifyReason(err)}
	case errors.Is(err, reporting.ErrBundleSignatureInvalid),
		errors.Is(err, reporting.ErrBundleSignatureSize),
		errors.Is(err, reporting.ErrBundlePubKeyMismatch):
		return stepResult{Name: "signature", OK: false, Detail: classifyReason(err)}
	case errors.Is(err, reporting.ErrBundleAnchorMalformed):
		return stepResult{Name: "anchor", OK: false, Detail: classifyReason(err)}
	default:
		// tar.gz 손상·entry size 초과 등.
		return stepResult{Name: "extract", OK: false, Detail: err.Error()}
	}
}

// emitOutput은 --format에 따라 stdout에 결과를 씁니다.
func emitOutput(format string, out verifyOutput) {
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	emitTable(out)
}

// emitTable은 사람 읽기용 표 + 단계별 체크리스트를 stdout에 씁니다.
func emitTable(out verifyOutput) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "RESULT\t%s\n", out.Result)
	_, _ = fmt.Fprintf(tw, "bundle\t%s\n", out.BundlePath)
	_, _ = fmt.Fprintf(tw, "pdfSize\t%s\n", strconv.FormatInt(out.PDFSize, 10))
	_, _ = fmt.Fprintf(tw, "pdfSha256\t%s\n", out.PDFSHA256)
	_, _ = fmt.Fprintf(tw, "signerKeyId\t%s\n", out.SignerKeyID)
	_, _ = fmt.Fprintf(tw, "chainHeadSeq\t%s\n", strconv.FormatInt(out.ChainHeadSeq, 10))
	_, _ = fmt.Fprintf(tw, "chainHeadHash\t%s\n", out.ChainHeadHash)
	if out.Reason != "" {
		_, _ = fmt.Fprintf(tw, "reason\t%s\n", out.Reason)
	}
	if out.Error != "" {
		_, _ = fmt.Fprintf(tw, "error\t%s\n", out.Error)
	}
	_ = tw.Flush()

	// 단계별 체크리스트 — 외부 감사인이 어느 검증이 통과/실패했는지 시각 확인.
	fmt.Println()
	fmt.Println("STEPS:")
	stw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(stw, "STEP\tOK\tDETAIL")
	for _, s := range out.Steps {
		mark := "FAIL"
		if s.OK {
			mark = "PASS"
		}
		_, _ = fmt.Fprintf(stw, "%s\t%s\t%s\n", s.Name, mark, s.Detail)
	}
	_ = stw.Flush()

	// 마지막 줄에 PASS/FAIL을 한 번 더 — 스크립트 grep 친화.
	if out.OK {
		fmt.Println("\nPASS — bundle verification successful.")
	} else {
		fmt.Println("\nFAIL — bundle verification failed.")
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `rosshield-audit-verify — 외부 감사인용 standalone report bundle 검증 도구

사용법:
  rosshield-audit-verify --bundle <path.tar.gz> [--format json|table] [--strict]

옵션:
  --bundle  검증할 report tar.gz 번들 경로 (필수)
  --format  출력 포맷 (table | json, 기본 table)
  --strict  warning을 fail로 처리 (현 단계 no-op, 미래 확장)

exit code:
  0  PASS — 모든 단계 통과
  1  FAIL — 검증 실패 (서명·entry·tar.gz·anchor)
  2  ARG  — invalid CLI args`)
}
