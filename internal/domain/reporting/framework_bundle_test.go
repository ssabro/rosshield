package reporting_test

// framework_bundle_test.go — E18 후속, framework 번들 결정성·검증 테스트.

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/reporting"
)

func newSignedFrameworkReport(t *testing.T, pdfBody []byte) (reporting.FrameworkReport, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sig := ed25519.Sign(priv, pdfBody)
	return reporting.FrameworkReport{
		ID:           "frep_x",
		TenantID:     "tnt_a",
		ProfileID:    "cp_X",
		SnapshotID:   "fs_X",
		PDFSHA256:    "deadbeef",
		PDFSizeBytes: int64(len(pdfBody)),
		PDF:          pdfBody,
		GeneratedAt:  time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		GeneratedBy:  "system",
		Signature: reporting.ReportSignature{
			Algorithm:     reporting.SignatureAlgorithmEd25519,
			SignerKeyID:   "key_test",
			Signature:     sig,
			SignedAt:      time.Date(2026, 4, 30, 12, 0, 1, 0, time.UTC),
			ChainHeadSeq:  99,
			ChainHeadHash: "cafebabe",
		},
	}, pub, priv
}

func TestBuildAndVerifyFrameworkBundleRoundTrip(t *testing.T) {
	t.Parallel()
	report, pub, _ := newSignedFrameworkReport(t, []byte("%PDF-1.4 framework fixture\n%%EOF"))
	data, err := reporting.BuildFrameworkBundle(report, "isms-p", "2024", pub)
	if err != nil {
		t.Fatalf("BuildFrameworkBundle: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("bundle is empty")
	}

	result, err := reporting.VerifyFrameworkBundle(data, pub)
	if err != nil {
		t.Fatalf("VerifyFrameworkBundle: %v", err)
	}
	if !result.OK {
		t.Errorf("OK = false, reason=%s", result.Reason)
	}
	if result.ProfileID != "cp_X" || result.SnapshotID != "fs_X" {
		t.Errorf("anchor profile/snapshot mismatch: %+v", result)
	}
	if result.Framework != "isms-p" || result.FrameworkVersion != "2024" {
		t.Errorf("anchor framework/version mismatch: %+v", result)
	}
	if result.SignerKeyID != "key_test" || result.ChainHeadSeq != 99 {
		t.Errorf("anchor sig meta mismatch: %+v", result)
	}
}

func TestVerifyFrameworkBundleRejectsTamperedBundle(t *testing.T) {
	t.Parallel()
	report, pub, _ := newSignedFrameworkReport(t, []byte("%PDF-original\n%%EOF"))
	data, err := reporting.BuildFrameworkBundle(report, "isms-p", "2024", pub)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// 중간 byte를 비트 flip — gzip CRC 또는 tar/anchor/sig 변조 → 어떤 단계든 verify 실패.
	tampered := append([]byte(nil), data...)
	tampered[len(tampered)/2] ^= 0xFF
	_, err = reporting.VerifyFrameworkBundle(tampered, pub)
	if err == nil {
		t.Errorf("tampered bundle should fail verification, got nil")
	}
}

func TestVerifyFrameworkBundleRejectsWrongPublicKey(t *testing.T) {
	t.Parallel()
	report, pub, _ := newSignedFrameworkReport(t, []byte("%PDF-1.4"))
	data, err := reporting.BuildFrameworkBundle(report, "isms-p", "2024", pub)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// 다른 pubKey로 검증 시도.
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	_, err = reporting.VerifyFrameworkBundle(data, otherPub)
	if !errors.Is(err, reporting.ErrBundlePubKeyMismatch) {
		t.Errorf("err = %v, want ErrBundlePubKeyMismatch", err)
	}
}

func TestVerifyFrameworkBundleAcceptsNilExpectedKey(t *testing.T) {
	t.Parallel()
	report, pub, _ := newSignedFrameworkReport(t, []byte("%PDF-1.4"))
	data, err := reporting.BuildFrameworkBundle(report, "isms-p", "2024", pub)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// expectedPublicKey nil → 번들 안 PEM을 신뢰.
	result, err := reporting.VerifyFrameworkBundle(data, nil)
	if err != nil {
		t.Fatalf("Verify(nil): %v", err)
	}
	if !result.OK {
		t.Errorf("OK = false")
	}
}

func TestBuildFrameworkBundleRejectsUnsignedReport(t *testing.T) {
	t.Parallel()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	report := reporting.FrameworkReport{
		ID:  "frep_x",
		PDF: []byte("%PDF"),
		// Signature.Signature는 zero (Generate 직후 placeholder)
		Signature: reporting.ReportSignature{
			Signature: make([]byte, 64), // 64B all-zero
		},
	}
	_, err = reporting.BuildFrameworkBundle(report, "isms-p", "2024", pub)
	if err == nil {
		t.Error("BuildFrameworkBundle should error on unsigned report")
	}
	if !strings.Contains(err.Error(), "not signed") {
		t.Errorf("err = %v, want 'not signed' message", err)
	}
}

func TestBuildFrameworkBundleIsDeterministic(t *testing.T) {
	t.Parallel()
	report, pub, _ := newSignedFrameworkReport(t, []byte("%PDF-stable"))
	d1, err := reporting.BuildFrameworkBundle(report, "isms-p", "2024", pub)
	if err != nil {
		t.Fatalf("Build1: %v", err)
	}
	d2, err := reporting.BuildFrameworkBundle(report, "isms-p", "2024", pub)
	if err != nil {
		t.Fatalf("Build2: %v", err)
	}
	if len(d1) != len(d2) {
		t.Fatalf("len mismatch: %d vs %d", len(d1), len(d2))
	}
	for i := range d1 {
		if d1[i] != d2[i] {
			t.Fatalf("byte mismatch at %d", i)
		}
	}
}
