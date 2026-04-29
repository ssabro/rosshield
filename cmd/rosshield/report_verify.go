package main

// report_verify.go — `rosshield report verify <bundle.tar.gz>` 서브커맨드 (E9 Stage A, R11-8).
//
// rosshield-server 동명 서브커맨드와 동일 흐름(reporting.VerifyBundle 호출)이지만 exit code
// 매핑은 R11-8 spec을 따름:
//
//	0  OK
//	1  read/parse 실패 (file 부재·tar.gz 손상·번들 entry 부재·anchor JSON malformed)
//	3  signature/verify 실패 (sig invalid·sig size mismatch·pub key mismatch·pub key malformed)
//
// rosshield-server는 anchor malformed에 exit 3을 사용하지만, 본 CLI는 R11-8의 의도(3=서명/검증
// 실패)에 맞춰 anchor malformed를 read/parse(1)로 분류 — Stage A 결정.
//
// 출력은 -o table(기본) 또는 -o json — 둘 다 동일한 필드(ok/pdfSize/pdfSha256/signerKeyId/
// chainHeadSeq/chainHeadHash/error)를 노출.

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/ssabro/rosshield/internal/domain/reporting"
)

// runReport는 `report ...` 서브커맨드를 분기합니다.
func runReport(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rosshield report verify <bundle.tar.gz>  |  rosshield report list [--session ID]")
		return 2
	}
	switch args[0] {
	case "verify":
		return runReportVerify(args[1:])
	case "list":
		return runReportList(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, `report 서브커맨드 — 서명된 PDF 리포트 번들 검증 (offline)

사용법:
  rosshield report verify <bundle.tar.gz> [-public-key <key.pem>] [-o table|json]

옵션:
  -public-key  외부에서 받은 expected ed25519 PublicKey PEM 파일 (옵션).
               지정 시 번들 내 public-key.pem과 byte-equal 비교 — mismatch면 exit 3.
  -o           출력 포맷 (table | json, 기본 table)

exit code:
  0  OK
  1  read/parse 실패 (file 부재·번들 손상·anchor 형식 오류)
  3  서명·public key 검증 실패`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "report: unknown sub-command %q\n", args[0])
		return 2
	}
}

// verifyOutput은 stdout 출력 row의 와이어 형식입니다.
//
// table·JSON 모두 동일 필드를 노출 — 사용자가 -o 변경 시 필드 이름 학습 비용 0.
type verifyOutput struct {
	OK            bool   `json:"ok"`
	Reason        string `json:"reason,omitempty"`
	Error         string `json:"error,omitempty"`
	PDFSize       int64  `json:"pdfSize"`
	PDFSHA256     string `json:"pdfSha256"`
	SignerKeyID   string `json:"signerKeyId"`
	ChainHeadSeq  int64  `json:"chainHeadSeq"`
	ChainHeadHash string `json:"chainHeadHash"`
}

// runReportVerify는 `report verify` 본 흐름입니다.
func runReportVerify(args []string) int {
	fs := flag.NewFlagSet("report verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // flag 자체 stderr 메시지 억제 — 본 CLI 메시지로 통일.
	publicKeyPath := fs.String("public-key", "", "expected ed25519 PublicKey PEM 파일 (옵션)")
	outFmt := fs.String("o", "table", "output format: table | json")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "report verify: flag parse error: %v\n", err)
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: rosshield report verify <bundle.tar.gz>")
		return 2
	}
	format, err := ParseOutputFormat(*outFmt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "report verify: %v\n", err)
		return 2
	}
	bundlePath := rest[0]

	bundleBytes, err := os.ReadFile(bundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read bundle %q: %v\n", bundlePath, err)
		return 1
	}

	var expectedPub ed25519.PublicKey
	if *publicKeyPath != "" {
		pemData, err := os.ReadFile(*publicKeyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read public key %q: %v\n", *publicKeyPath, err)
			return 1
		}
		pub, err := decodeExternalPEM(pemData)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse public key: %v\n", err)
			return 1
		}
		expectedPub = pub
	}

	res, verr := reporting.VerifyBundle(bundleBytes, expectedPub)
	out := verifyOutput{
		OK:            res.OK && verr == nil,
		Reason:        res.Reason,
		PDFSize:       res.PDFSize,
		PDFSHA256:     res.PDFSHA256,
		SignerKeyID:   res.SignerKeyID,
		ChainHeadSeq:  res.ChainHeadSeq,
		ChainHeadHash: res.ChainHeadHash,
	}
	if verr != nil {
		out.Error = verr.Error()
	}
	emitVerifyOutput(format, out)

	if verr == nil {
		return 0
	}
	switch {
	case errors.Is(verr, reporting.ErrBundleSignatureInvalid),
		errors.Is(verr, reporting.ErrBundleSignatureSize),
		errors.Is(verr, reporting.ErrBundlePubKeyMismatch),
		errors.Is(verr, reporting.ErrBundlePubKeyMalformed):
		return 3
	default:
		// missing entry·tar.gz 손상·anchor malformed 등은 read/parse 영역.
		return 1
	}
}

// emitVerifyOutput은 -o 플래그에 따라 stdout에 결과를 씁니다.
func emitVerifyOutput(format OutputFormat, out verifyOutput) {
	if format == OutputJSON {
		_ = PrintJSON(out)
		return
	}
	rows := [][]string{
		{"ok", strconv.FormatBool(out.OK)},
		{"pdfSize", strconv.FormatInt(out.PDFSize, 10)},
		{"pdfSha256", out.PDFSHA256},
		{"signerKeyId", out.SignerKeyID},
		{"chainHeadSeq", strconv.FormatInt(out.ChainHeadSeq, 10)},
		{"chainHeadHash", out.ChainHeadHash},
	}
	if out.Reason != "" {
		rows = append(rows, []string{"reason", out.Reason})
	}
	if out.Error != "" {
		rows = append(rows, []string{"error", out.Error})
	}
	PrintTable([]string{"KEY", "VALUE"}, rows)
}

// decodeExternalPEM은 외부 expected public key PEM을 파싱합니다.
//
// reporting.decodePublicKeyPEM은 unexported이므로 동일 흐름을 본 패키지에서 재구현.
// rosshield-server/report_verify.go의 decodePEMForExternal과 동일 패턴.
func decodeExternalPEM(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("PEM decode failed")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX: %w", err)
	}
	edPub, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("PEM is not Ed25519")
	}
	return edPub, nil
}
