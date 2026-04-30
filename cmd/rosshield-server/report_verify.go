package main

// report_verify.go — `rosshield-server report verify <bundle.tar.gz>` 서브커맨드 (E8 Stage E).
//
// Phase 1 Exit "외부 검증 성공" 흐름의 진입점. tar.gz 번들을 받아 ed25519 서명·anchor
// 메타를 검증하고 결과를 사람·기계 모두 읽기 쉬운 JSON으로 stdout에 출력.
//
// exit code:
//   0 — OK (signature valid, anchor present)
//   1 — read/parse error (file 부재·tar.gz 손상·번들 entry 부재)
//   2 — signature invalid 또는 public key mismatch
//   3 — anchor malformed
//
// 사용처: 외부 감사인이 `rosshield-server report verify report-2026.tar.gz`로 호출 →
// JSON 결과로 OK/실패 판단. 별도 audit DB·CSV 없이 번들 자체의 무결성만 확인.

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ssabro/rosshield/internal/domain/reporting"
)

// reportSubcommand는 `report ...` 서브커맨드를 처리합니다.
//
//	verify             — session 리포트 번들 검증 (E8)
//	verify-framework   — framework 리포트 번들 검증 (E18 후속, Phase 2 Exit)
func reportSubcommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rosshield-server report verify|verify-framework <bundle.tar.gz>")
		return 2
	}
	switch args[0] {
	case "verify":
		return runReportVerify(args[1:])
	case "verify-framework":
		return runFrameworkVerify(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, `report 서브커맨드 — 서명된 PDF 리포트 번들 검증

사용법:
  rosshield-server report verify <bundle.tar.gz> [-public-key <key.pem>]
  rosshield-server report verify-framework <bundle.tar.gz> [-public-key <key.pem>]

옵션:
  -public-key  외부에서 받은 expected ed25519 PublicKey PEM 파일 (옵션).
               지정 시 번들 내 public-key.pem과 byte-equal 비교 — mismatch면 실패.
               미지정 시 번들 내 public-key.pem만으로 검증(self-attesting).

exit code:
  0  OK
  1  read/parse 실패
  2  서명 또는 public key 검증 실패
  3  anchor 메타 오류`)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "report: unknown sub-command %q\n", args[0])
		return 2
	}
}

// runFrameworkVerify는 framework 번들 검증 (E18 후속).
func runFrameworkVerify(args []string) int {
	fs := flag.NewFlagSet("report verify-framework", flag.ContinueOnError)
	publicKeyPath := fs.String("public-key", "", "expected ed25519 PublicKey PEM 파일 (옵션)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: rosshield-server report verify-framework <bundle.tar.gz>")
		return 1
	}
	bundlePath := rest[0]

	bundleBytes, err := os.ReadFile(bundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read bundle %q: %v\n", bundlePath, err)
		return 1
	}

	var expectedPub ed25519.PublicKey
	if *publicKeyPath != "" {
		pemBytes, err := os.ReadFile(*publicKeyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read public key %q: %v\n", *publicKeyPath, err)
			return 1
		}
		pub, err := decodePEMForExternal(pemBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse public key: %v\n", err)
			return 1
		}
		expectedPub = pub
	}

	res, err := reporting.VerifyFrameworkBundle(bundleBytes, expectedPub)
	if err != nil {
		switch {
		case errors.Is(err, reporting.ErrBundleSignatureInvalid),
			errors.Is(err, reporting.ErrBundleSignatureSize),
			errors.Is(err, reporting.ErrBundlePubKeyMismatch),
			errors.Is(err, reporting.ErrBundlePubKeyMalformed):
			emitFrameworkVerifyJSON(os.Stdout, res, err)
			return 2
		case errors.Is(err, reporting.ErrBundleAnchorMalformed):
			emitFrameworkVerifyJSON(os.Stdout, res, err)
			return 3
		default:
			emitFrameworkVerifyJSON(os.Stdout, res, err)
			return 1
		}
	}
	emitFrameworkVerifyJSON(os.Stdout, res, nil)
	return 0
}

type frameworkVerifyJSONOutput struct {
	OK               bool   `json:"ok"`
	Reason           string `json:"reason,omitempty"`
	Error            string `json:"error,omitempty"`
	PDFSize          int64  `json:"pdfSize"`
	PDFSHA256        string `json:"pdfSha256"`
	SignerKeyID      string `json:"signerKeyId"`
	ChainHeadSeq     int64  `json:"chainHeadSeq"`
	ChainHeadHash    string `json:"chainHeadHash"`
	ProfileID        string `json:"profileId"`
	SnapshotID       string `json:"snapshotId"`
	Framework        string `json:"framework"`
	FrameworkVersion string `json:"frameworkVersion"`
}

func emitFrameworkVerifyJSON(w io.Writer, res reporting.FrameworkVerifyResult, err error) {
	out := frameworkVerifyJSONOutput{
		OK:               res.OK && err == nil,
		Reason:           res.Reason,
		PDFSize:          res.PDFSize,
		PDFSHA256:        res.PDFSHA256,
		SignerKeyID:      res.SignerKeyID,
		ChainHeadSeq:     res.ChainHeadSeq,
		ChainHeadHash:    res.ChainHeadHash,
		ProfileID:        res.ProfileID,
		SnapshotID:       res.SnapshotID,
		Framework:        res.Framework,
		FrameworkVersion: res.FrameworkVersion,
	}
	if err != nil {
		out.Error = err.Error()
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// runReportVerify는 `report verify` 본 흐름입니다.
//
// 결과 stdout JSON:
//
//	{
//	  "ok": true,
//	  "pdfSize": 123456,
//	  "pdfSha256": "abc...",
//	  "signerKeyId": "key_...",
//	  "chainHeadSeq": 42,
//	  "chainHeadHash": "abc...",
//	  "publicKey": "-----BEGIN PUBLIC KEY-----\n..."   (옵션 -with-key 지정 시)
//	}
func runReportVerify(args []string) int {
	fs := flag.NewFlagSet("report verify", flag.ContinueOnError)
	publicKeyPath := fs.String("public-key", "", "expected ed25519 PublicKey PEM 파일 (옵션)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: rosshield-server report verify <bundle.tar.gz>")
		return 1
	}
	bundlePath := rest[0]

	bundleBytes, err := os.ReadFile(bundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read bundle %q: %v\n", bundlePath, err)
		return 1
	}

	var expectedPub ed25519.PublicKey
	if *publicKeyPath != "" {
		pem, err := os.ReadFile(*publicKeyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read public key %q: %v\n", *publicKeyPath, err)
			return 1
		}
		pub, err := decodePEMForExternal(pem)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse public key: %v\n", err)
			return 1
		}
		expectedPub = pub
	}

	res, err := reporting.VerifyBundle(bundleBytes, expectedPub)
	if err != nil {
		// 실패 분류는 sentinel 비교.
		switch {
		case errors.Is(err, reporting.ErrBundleSignatureInvalid),
			errors.Is(err, reporting.ErrBundleSignatureSize),
			errors.Is(err, reporting.ErrBundlePubKeyMismatch),
			errors.Is(err, reporting.ErrBundlePubKeyMalformed):
			emitVerifyJSON(os.Stdout, res, err)
			return 2
		case errors.Is(err, reporting.ErrBundleAnchorMalformed):
			emitVerifyJSON(os.Stdout, res, err)
			return 3
		default:
			// missing entry 등은 read/parse 영역.
			emitVerifyJSON(os.Stdout, res, err)
			return 1
		}
	}
	emitVerifyJSON(os.Stdout, res, nil)
	return 0
}

// verifyJSONOutput은 stdout에 출력되는 결과 형식입니다.
type verifyJSONOutput struct {
	OK            bool   `json:"ok"`
	Reason        string `json:"reason,omitempty"`
	Error         string `json:"error,omitempty"`
	PDFSize       int64  `json:"pdfSize"`
	PDFSHA256     string `json:"pdfSha256"`
	SignerKeyID   string `json:"signerKeyId"`
	ChainHeadSeq  int64  `json:"chainHeadSeq"`
	ChainHeadHash string `json:"chainHeadHash"`
}

func emitVerifyJSON(w io.Writer, res reporting.VerifyResult, err error) {
	out := verifyJSONOutput{
		OK:            res.OK && err == nil,
		Reason:        res.Reason,
		PDFSize:       res.PDFSize,
		PDFSHA256:     res.PDFSHA256,
		SignerKeyID:   res.SignerKeyID,
		ChainHeadSeq:  res.ChainHeadSeq,
		ChainHeadHash: res.ChainHeadHash,
	}
	if err != nil {
		out.Error = err.Error()
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// decodePEMForExternal은 외부에서 받은 expected public key PEM을 파싱합니다.
//
// reporting/bundle.go의 decodePublicKeyPEM은 unexported이므로 동일 흐름을 cmd/* 안에서
// 재구현 — 도메인 표면을 늘리지 않고 단순화. 양쪽 모두 PKIX SubjectPublicKeyInfo 표준.
func decodePEMForExternal(data []byte) (ed25519.PublicKey, error) {
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
