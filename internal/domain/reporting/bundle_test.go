package reporting_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/reporting"
)

func newSignedReport(t *testing.T, pdfBody []byte) (reporting.Report, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sig := ed25519.Sign(priv, pdfBody)
	return reporting.Report{
		ID:           "rep_x",
		TenantID:     "tnt_a",
		ScopeType:    reporting.ScopeSession,
		SessionID:    "scan_x",
		Format:       reporting.FormatPDF,
		PDFSHA256:    "deadbeef",
		PDFSizeBytes: int64(len(pdfBody)),
		PDF:          pdfBody,
		GeneratedAt:  time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
		GeneratedBy:  "system",
		Signature: reporting.ReportSignature{
			Algorithm:     reporting.SignatureAlgorithmEd25519,
			SignerKeyID:   "key_test",
			Signature:     sig,
			SignedAt:      time.Date(2026, 4, 29, 12, 0, 1, 0, time.UTC),
			ChainHeadSeq:  42,
			ChainHeadHash: "abc123",
		},
	}, pub, priv
}

func TestBuildBundleProducesAllFourEntries(t *testing.T) {
	report, pub, _ := newSignedReport(t, []byte("%PDF-1.4 fixture\n%%EOF"))
	data, err := reporting.BuildBundle(report, pub)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	res, err := reporting.VerifyBundle(data, pub)
	if err != nil {
		t.Fatalf("VerifyBundle: %v", err)
	}
	if !res.OK {
		t.Fatalf("verify result.OK=false: %s", res.Reason)
	}
	if res.PDFSize != int64(len(report.PDF)) {
		t.Fatalf("PDFSize=%d, want %d", res.PDFSize, len(report.PDF))
	}
	if res.SignerKeyID != "key_test" {
		t.Fatalf("SignerKeyID=%q", res.SignerKeyID)
	}
	if res.ChainHeadSeq != 42 || res.ChainHeadHash != "abc123" {
		t.Fatalf("anchor mismatch: %+v", res)
	}
}

func TestBuildBundleByteForByteStable(t *testing.T) {
	report, pub, _ := newSignedReport(t, []byte("body"))
	a, err := reporting.BuildBundle(report, pub)
	if err != nil {
		t.Fatalf("BuildBundle#1: %v", err)
	}
	b, err := reporting.BuildBundle(report, pub)
	if err != nil {
		t.Fatalf("BuildBundle#2: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("bundle bytes not deterministic")
	}
}

func TestVerifyBundleRejectsTamperedPDF(t *testing.T) {
	report, pub, priv := newSignedReport(t, []byte("original body"))
	data, err := reporting.BuildBundle(report, pub)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	// 번들 풀어서 PDF만 변조 후 재포장 — sig는 원본 PDF에 대한 것이라 검증 실패.
	tampered := tamperBundleEntry(t, data, reporting.BundleFilePDF, []byte("tampered body"))
	_, err = reporting.VerifyBundle(tampered, pub)
	if !errors.Is(err, reporting.ErrBundleSignatureInvalid) {
		t.Fatalf("err=%v, want ErrBundleSignatureInvalid", err)
	}

	// 정상 PDF + 새로 서명된 sig는 통과 — 검증 알고리즘 자체 sanity check.
	freshSig := ed25519.Sign(priv, []byte("tampered body"))
	tamperedFresh := tamperBundleEntry(t, tampered, reporting.BundleFileSignature, freshSig)
	res, err := reporting.VerifyBundle(tamperedFresh, pub)
	if err != nil || !res.OK {
		t.Fatalf("re-signed fresh bundle should verify: %v %+v", err, res)
	}
}

func TestVerifyBundleRejectsWrongPublicKey(t *testing.T) {
	report, pub, _ := newSignedReport(t, []byte("x"))
	data, err := reporting.BuildBundle(report, pub)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	_, err = reporting.VerifyBundle(data, otherPub)
	if !errors.Is(err, reporting.ErrBundlePubKeyMismatch) {
		t.Fatalf("err=%v, want ErrBundlePubKeyMismatch", err)
	}
}

func TestVerifyBundleAcceptsNilExpectedKey(t *testing.T) {
	report, pub, _ := newSignedReport(t, []byte("y"))
	data, err := reporting.BuildBundle(report, pub)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	res, err := reporting.VerifyBundle(data, nil) // 번들 내 public-key.pem 신뢰
	if err != nil {
		t.Fatalf("VerifyBundle nil key: %v", err)
	}
	if !res.OK {
		t.Fatalf("OK=false: %s", res.Reason)
	}
}

func TestBuildBundleRejectsUnsignedReport(t *testing.T) {
	report, pub, _ := newSignedReport(t, []byte("z"))
	report.Signature = reporting.ReportSignature{} // unsign
	_, err := reporting.BuildBundle(report, pub)
	if err == nil {
		t.Fatalf("expected error for unsigned report")
	}
}

func TestBuildBundleRejectsInvalidSignatureSize(t *testing.T) {
	report, pub, _ := newSignedReport(t, []byte("z"))
	report.Signature.Signature = []byte{1, 2, 3}
	_, err := reporting.BuildBundle(report, pub)
	if !errors.Is(err, reporting.ErrBundleSignatureSize) {
		t.Fatalf("err=%v, want ErrBundleSignatureSize", err)
	}
}

func TestBuildBundleRejectsEmptyPDF(t *testing.T) {
	report, pub, _ := newSignedReport(t, []byte("x"))
	report.PDF = nil
	_, err := reporting.BuildBundle(report, pub)
	if err == nil {
		t.Fatalf("expected error for empty PDF")
	}
}

func TestBuildBundleRejectsWrongPubKeySize(t *testing.T) {
	report, _, _ := newSignedReport(t, []byte("z"))
	_, err := reporting.BuildBundle(report, []byte{1, 2, 3})
	if err == nil {
		t.Fatalf("expected error for short pub key")
	}
}

// === helpers ===

// tamperBundleEntry는 tar.gz 번들 안의 한 entry를 다른 bytes로 교체한 새 번들을 반환합니다.
func tamperBundleEntry(t *testing.T, tarGz []byte, target string, newBody []byte) []byte {
	t.Helper()
	files := readTarGz(t, tarGz)
	files[target] = newBody
	return writeTarGz(t, files)
}

func readTarGz(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip open: %v", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read entry %q: %v", hdr.Name, err)
		}
		out[hdr.Name] = body
	}
	return out
}

func writeTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)
	// 정렬된 순서로 직렬화 — 결정성.
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		body := files[name]
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(body)), Format: tar.FormatPAX,
		}); err != nil {
			t.Fatalf("tar header %q: %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("tar write %q: %v", name, err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return gzBuf.Bytes()
}
