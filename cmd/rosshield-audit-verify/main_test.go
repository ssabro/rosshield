package main

// main_test.go — 외부 감사인용 standalone audit-verify 도구 테스트 (E30 T1·T2·T3).
//
// 본 테스트는 rosshield-server·rosshield-CLI 없이 binary가 단독으로
// tar.gz 번들 검증을 수행하는지 확인합니다. fixture 생성은 reporting.BuildBundle
// (도메인 표면)만 사용 — P5 격리 유지.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/reporting"
)

// captureStdio는 fn 실행 동안의 stdout·stderr를 string으로 반환합니다 (cmd/rosshield 동일 패턴).
func captureStdio(t *testing.T, fn func()) (string, string) {
	t.Helper()
	origOut := os.Stdout
	origErr := os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	done := make(chan struct {
		out, err string
	}, 1)
	go func() {
		var bufOut, bufErr bytes.Buffer
		_, _ = io.Copy(&bufOut, rOut)
		_, _ = io.Copy(&bufErr, rErr)
		done <- struct{ out, err string }{bufOut.String(), bufErr.String()}
	}()

	fn()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = origOut
	os.Stderr = origErr

	res := <-done
	return res.out, res.err
}

// writeGoldenBundle은 ed25519로 서명된 합성 PDF 번들을 임시 파일로 작성합니다.
func writeGoldenBundle(t *testing.T) (path string, pub ed25519.PublicKey, priv ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pdfBody := []byte("%PDF-golden\n%%EOF")
	sig := ed25519.Sign(priv, pdfBody)

	report := reporting.Report{
		ID:           "rep_audit",
		TenantID:     "tnt_audit",
		ScopeType:    reporting.ScopeSession,
		SessionID:    "scan_audit",
		Format:       reporting.FormatPDF,
		PDFSHA256:    "deadbeef",
		PDFSizeBytes: int64(len(pdfBody)),
		PDF:          pdfBody,
		GeneratedAt:  time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC),
		GeneratedBy:  "system",
		Signature: reporting.ReportSignature{
			Algorithm:     reporting.SignatureAlgorithmEd25519,
			SignerKeyID:   "key_audit_golden",
			Signature:     sig,
			SignedAt:      time.Date(2026, 5, 8, 0, 0, 1, 0, time.UTC),
			ChainHeadSeq:  101,
			ChainHeadHash: "cafebabe",
		},
	}
	bundleBytes, err := reporting.BuildBundle(report, pub)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	bundlePath := filepath.Join(t.TempDir(), "golden.tar.gz")
	if err := os.WriteFile(bundlePath, bundleBytes, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	return bundlePath, pub, priv
}

// E30.T1 — golden bundle → PASS, exit 0.
func TestAuditVerifyToolValidatesGoldenBundle(t *testing.T) {
	bundlePath, _, _ := writeGoldenBundle(t)
	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"--bundle", bundlePath}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(stdout, "PASS") {
		t.Fatalf("stdout missing 'PASS': %q", stdout)
	}
	if !strings.Contains(stdout, "key_audit_golden") {
		t.Fatalf("stdout missing signer keyId: %q", stdout)
	}
}

// E30.T2 — sig 변조 → FAIL with reason, exit 1.
func TestAuditVerifyDetectsSignatureTamper(t *testing.T) {
	bundlePath, _, _ := writeGoldenBundle(t)

	// 번들 풀어서 PDF만 변조 후 재포장 — sig는 원본 PDF에 대한 것이라 검증 실패.
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	tampered := tamperBundleEntry(t, data, reporting.BundleFilePDF, []byte("tampered body"))
	tamperedPath := filepath.Join(t.TempDir(), "tampered.tar.gz")
	if err := os.WriteFile(tamperedPath, tampered, 0o644); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"--bundle", tamperedPath}); code != 1 {
			t.Errorf("exit=%d, want 1 (signature invalid)", code)
		}
	})
	if !strings.Contains(stdout, "FAIL") {
		t.Fatalf("stdout missing 'FAIL': %q", stdout)
	}
	// reason은 사람 읽기용이므로 'signature' 또는 'verify' 키워드 포함.
	low := strings.ToLower(stdout)
	if !strings.Contains(low, "signature") && !strings.Contains(low, "verify") {
		t.Fatalf("stdout missing reason about signature/verify: %q", stdout)
	}
}

// E30.T3 — JSON 출력이 valid JSON이고 필수 필드 노출.
func TestAuditVerifyJSONOutputIsValid(t *testing.T) {
	bundlePath, _, _ := writeGoldenBundle(t)
	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"--bundle", bundlePath, "--format", "json"}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, stdout)
	}
	for _, key := range []string{"ok", "result", "pdfSize", "pdfSha256",
		"signerKeyId", "chainHeadSeq", "chainHeadHash", "steps"} {
		if _, ok := parsed[key]; !ok {
			t.Fatalf("JSON missing key %q: %s", key, stdout)
		}
	}
	if got, _ := parsed["result"].(string); got != "PASS" {
		t.Fatalf("result=%q, want PASS", got)
	}
	if got, _ := parsed["signerKeyId"].(string); got != "key_audit_golden" {
		t.Fatalf("signerKeyId=%q, want key_audit_golden", got)
	}
	steps, ok := parsed["steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Fatalf("steps not a non-empty array: %v", parsed["steps"])
	}
}

// 추가 sanity: --bundle 누락 시 exit 2 (arg error).
func TestAuditVerifyMissingBundleFlagExitsTwo(t *testing.T) {
	captureStdio(t, func() {
		if code := run([]string{}); code != 2 {
			t.Errorf("exit=%d, want 2", code)
		}
	})
}

// 추가 sanity: 존재하지 않는 파일 → exit 1 (read 실패).
func TestAuditVerifyMissingFileExitsOne(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.tar.gz")
	captureStdio(t, func() {
		if code := run([]string{"--bundle", missing}); code != 1 {
			t.Errorf("exit=%d, want 1", code)
		}
	})
}

// 추가 sanity: --strict 플래그가 받아들여진다 (지금은 동일 결과 — 향후 warning gate용).
func TestAuditVerifyStrictFlagAccepted(t *testing.T) {
	bundlePath, _, _ := writeGoldenBundle(t)
	captureStdio(t, func() {
		if code := run([]string{"--bundle", bundlePath, "--strict"}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
}

// 추가 sanity: 알 수 없는 --format → exit 2.
func TestAuditVerifyUnknownFormatExitsTwo(t *testing.T) {
	bundlePath, _, _ := writeGoldenBundle(t)
	captureStdio(t, func() {
		if code := run([]string{"--bundle", bundlePath, "--format", "yaml"}); code != 2 {
			t.Errorf("exit=%d, want 2", code)
		}
	})
}

// === helpers ===

// tamperBundleEntry는 tar.gz 번들 안의 한 entry를 다른 bytes로 교체한 새 번들을 반환합니다.
// (reporting/bundle_test.go 동일 패턴 — 외부 감사인 binary는 reporting helper에 접근 불가.)
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
