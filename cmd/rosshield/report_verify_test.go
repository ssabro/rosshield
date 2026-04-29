package main

// report_verify_test.go — `report verify` CLI 단위 테스트 (E9 Stage A).
//
// rosshield-server의 동명 테스트는 Bootstrap → seed → GenerateAndSignReport로 fixture를
// 만들지만, 본 패키지는 그 헬퍼에 접근 불가(다른 main 패키지). 대신 reporting.BuildBundle을
// 직접 사용해 minimal Report + Sign 흐름으로 합성 — 도메인 표면만 사용해 P5 격리.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/reporting"
)

// writeMinimalBundle은 ed25519 키로 서명된 합성 PDF 번들을 임시 파일에 저장합니다.
//
// 실제 PDF가 아닌 임의 bytes("%PDF-fake")를 서명·번들링 — VerifyBundle은 PDF 파싱을
// 수행하지 않으므로 ed25519.Verify 통과만 보장하면 OK 결과 유효.
func writeMinimalBundle(t *testing.T) (string, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pdfBody := []byte("%PDF-fake\n%%EOF")
	sig := ed25519.Sign(priv, pdfBody)

	report := reporting.Report{
		ID:           "rep_cli",
		TenantID:     "tnt_cli",
		ScopeType:    reporting.ScopeSession,
		SessionID:    "scan_cli",
		Format:       reporting.FormatPDF,
		PDFSHA256:    "deadbeef",
		PDFSizeBytes: int64(len(pdfBody)),
		PDF:          pdfBody,
		GeneratedAt:  time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC),
		GeneratedBy:  "system",
		Signature: reporting.ReportSignature{
			Algorithm:     reporting.SignatureAlgorithmEd25519,
			SignerKeyID:   "key_cli_test",
			Signature:     sig,
			SignedAt:      time.Date(2026, 4, 29, 0, 0, 1, 0, time.UTC),
			ChainHeadSeq:  7,
			ChainHeadHash: "feedface",
		},
	}
	bundleBytes, err := reporting.BuildBundle(report, pub)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	bundlePath := filepath.Join(t.TempDir(), "report.tar.gz")
	if err := os.WriteFile(bundlePath, bundleBytes, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	return bundlePath, pub
}

func TestReportVerifyExitsZeroOnValidBundle(t *testing.T) {
	bundlePath, _ := writeMinimalBundle(t)
	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"report", "verify", bundlePath}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(stdout, "ok") {
		t.Fatalf("stdout missing 'ok': %q", stdout)
	}
	if !strings.Contains(stdout, "true") {
		t.Fatalf("stdout missing 'true': %q", stdout)
	}
}

func TestReportVerifyExitsThreeOnTamperedBundle(t *testing.T) {
	bundlePath, _ := writeMinimalBundle(t)
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) < 100 {
		t.Fatalf("bundle too small: %d bytes", len(data))
	}
	// 가운데 byte 한 개 뒤집기 — gzip CRC 실패는 exit 1, sig 불일치는 exit 3.
	// 본 테스트는 어느 쪽이든 받아들이지만 sig 변조를 직접 만들려면 entry 단위 변조가 필요.
	// 단순 byte flip은 보통 gzip layer에서 거부되어 exit 1 — 따라서 entry 단위 테스트를 별도.
	data[len(data)/2] ^= 0xFF
	tampered := filepath.Join(t.TempDir(), "tampered.tar.gz")
	if err := os.WriteFile(tampered, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	captureStdio(t, func() {
		code := run([]string{"report", "verify", tampered})
		if code == 0 {
			t.Errorf("exit=0, want non-zero (tampered)")
		}
		// 1(gzip 손상) 또는 3(sig 실패) 둘 중 하나여야 함.
		if code != 1 && code != 3 {
			t.Errorf("exit=%d, want 1 or 3", code)
		}
	})
}

func TestReportVerifyExitsThreeOnWrongPublicKey(t *testing.T) {
	bundlePath, _ := writeMinimalBundle(t)

	// 다른 ed25519 키 PEM을 임시 파일로 작성 후 -public-key로 전달.
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pemPath := filepath.Join(t.TempDir(), "other.pem")
	if err := os.WriteFile(pemPath, encodePubPEMForTest(t, otherPub), 0o644); err != nil {
		t.Fatalf("write pem: %v", err)
	}
	captureStdio(t, func() {
		if code := run([]string{"report", "verify",
			"-public-key", pemPath, bundlePath}); code != 3 {
			t.Errorf("exit=%d, want 3 (pub key mismatch)", code)
		}
	})
}

func TestReportVerifyExitsOneOnMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.tar.gz")
	captureStdio(t, func() {
		if code := run([]string{"report", "verify", missing}); code != 1 {
			t.Errorf("exit=%d, want 1", code)
		}
	})
}

func TestReportVerifyJSONOutput(t *testing.T) {
	bundlePath, _ := writeMinimalBundle(t)
	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"report", "verify", "-o", "json", bundlePath}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal stdout: %v\nraw: %s", err, stdout)
	}
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("ok != true: %v", parsed)
	}
	for _, key := range []string{"pdfSize", "pdfSha256", "signerKeyId",
		"chainHeadSeq", "chainHeadHash"} {
		if _, ok := parsed[key]; !ok {
			t.Fatalf("missing key %q in JSON: %s", key, stdout)
		}
	}
	if got, _ := parsed["signerKeyId"].(string); got != "key_cli_test" {
		t.Fatalf("signerKeyId=%q, want key_cli_test", got)
	}
}

func TestReportVerifyTableOutput(t *testing.T) {
	bundlePath, _ := writeMinimalBundle(t)
	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"report", "verify", bundlePath}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	for _, key := range []string{"ok", "pdfSize", "pdfSha256",
		"signerKeyId", "chainHeadSeq", "chainHeadHash"} {
		if !strings.Contains(stdout, key) {
			t.Fatalf("table missing %q: %s", key, stdout)
		}
	}
	if !strings.Contains(stdout, "key_cli_test") {
		t.Fatalf("table missing signer key value: %s", stdout)
	}
}

func TestReportVerifyMissingArgsExitsTwo(t *testing.T) {
	captureStdio(t, func() {
		if code := run([]string{"report", "verify"}); code != 2 {
			t.Errorf("exit=%d, want 2", code)
		}
	})
}

func TestReportVerifyUnknownOutputFormatExitsTwo(t *testing.T) {
	bundlePath, _ := writeMinimalBundle(t)
	captureStdio(t, func() {
		if code := run([]string{"report", "verify", "-o", "yaml", bundlePath}); code != 2 {
			t.Errorf("exit=%d, want 2", code)
		}
	})
}

// encodePubPEMForTest는 PKIX SubjectPublicKeyInfo PEM을 생성합니다.
func encodePubPEMForTest(t *testing.T, pub ed25519.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("MarshalPKIX: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}
