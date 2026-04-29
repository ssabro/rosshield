package converter_test

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

	"github.com/ssabro/rosshield/cmd/pack-tools/converter"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

// E12 Stage D · T6 — archive: MANIFEST + SIGNATURE + tar.gz.
//
// 변환 결과 디렉터리를 한 tar.gz로 묶고 Ed25519 서명을 포함시켜 production
// `benchmark.LoadPackFromTar`가 그대로 검증·로드하도록 한다.

func writeFixturePackDir(t *testing.T) string {
	t.Helper()
	pack := converter.Pack{
		Name: "cis-ubuntu-2404", Version: "1.0.0", Vendor: "rosshield",
		Description: "fixture pack",
		Checks: []converter.Check{
			{
				ID: "AUTO-1", Title: "auto", Severity: "high",
				AuditCommand:   "bash -c 'true'",
				EvaluationRule: json.RawMessage(`{"op":"contains","value":"** PASS **"}`),
				Rationale:      "rationale", FixGuidance: "fix",
			},
			{
				ID: "DEGR-1", Title: "deg", Severity: "medium",
				AuditCommand: "true",
				EvaluationRule: json.RawMessage(
					`{"op":"contains","value":"<degraded — Phase 2 fixture required>"}`),
			},
		},
	}
	dir := filepath.Join(t.TempDir(), "pack")
	if err := converter.WriteToDir(pack, dir); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}
	return dir
}

func newTestSigner(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func extractTarGz(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
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
			t.Fatalf("tar.Next: %v", err)
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

func TestBuildArchiveProducesValidTarGz(t *testing.T) {
	dir := writeFixturePackDir(t)
	_, priv := newTestSigner(t)

	data, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	files := extractTarGz(t, data)

	wantPaths := []string{
		"MANIFEST.json", "SIGNATURE",
		"pack.yaml",
		"checks/AUTO-1.yaml", "checks/DEGR-1.yaml",
		"selftest/AUTO-1.yaml", // DEGR-1은 fixture 없음
	}
	sort.Strings(wantPaths)
	got := make([]string, 0, len(files))
	for p := range files {
		got = append(got, p)
	}
	sort.Strings(got)
	if !equalStrings(got, wantPaths) {
		t.Fatalf("archive contents mismatch\n want: %v\n got:  %v", wantPaths, got)
	}
}

// 핵심 — 결과 archive는 production loader로 그대로 검증·로드된다 (T4와 함께 가장 중요).
func TestBuildArchiveLoadsInBenchmarkLoader(t *testing.T) {
	dir := writeFixturePackDir(t)
	pub, priv := newTestSigner(t)

	data, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}

	pack, err := benchmark.LoadPackFromTar(data, pub)
	if err != nil {
		t.Fatalf("LoadPackFromTar: %v", err)
	}
	if pack.Vendor != "rosshield" || pack.Name != "cis-ubuntu-2404" || pack.Version != "1.0.0" {
		t.Fatalf("pack meta drift: %+v", pack)
	}
	if len(pack.Checks) != 2 {
		t.Fatalf("checks len=%d, want 2", len(pack.Checks))
	}
	// Check 정렬은 manifest 내 path 알파벳순.
	if pack.Checks[0].CheckID != "AUTO-1" || pack.Checks[1].CheckID != "DEGR-1" {
		t.Fatalf("check IDs: %s, %s", pack.Checks[0].CheckID, pack.Checks[1].CheckID)
	}
	// EvaluationRule도 라운드트립.
	rule, err := benchmark.ParseEvalRule(pack.Checks[0].EvaluationRule)
	if err != nil {
		t.Fatalf("ParseEvalRule: %v", err)
	}
	out, err := rule.Eval(benchmark.EvalInput{Stdout: "** PASS **", ExitCode: 0})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if out.Status != benchmark.StatusPass {
		t.Fatalf("Eval status=%s, want PASS", out.Status)
	}
}

func TestBuildArchiveSignatureRejectsWrongPublicKey(t *testing.T) {
	dir := writeFixturePackDir(t)
	_, priv := newTestSigner(t)
	data, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}

	otherPub, _ := newTestSigner(t)
	if _, err := benchmark.LoadPackFromTar(data, otherPub); err == nil {
		t.Fatalf("expected SIGNATURE verify failure with wrong public key, got nil")
	} else if !errors.Is(err, benchmark.ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestBuildArchiveTamperingDetected(t *testing.T) {
	dir := writeFixturePackDir(t)
	pub, priv := newTestSigner(t)

	// pack.yaml을 archive 후 변조 → manifest hash mismatch 또는 signature 위반.
	data, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}

	// archive 안의 pack.yaml을 직접 변조한 새 archive 만들기.
	files := extractTarGz(t, data)
	files["pack.yaml"] = bytes.ReplaceAll(files["pack.yaml"], []byte("rosshield"), []byte("attacker"))

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()

	if _, err := benchmark.LoadPackFromTar(gzBuf.Bytes(), pub); err == nil {
		t.Fatalf("expected error for tampered pack.yaml, got nil")
	} else if !errors.Is(err, benchmark.ErrManifestHashMismatch) {
		// (또는 ManifestExtraFile 등 — 핵심은 검증이 거부했다는 사실)
		t.Logf("rejected with %v — acceptable as long as not nil", err)
	}
}

func TestBuildArchiveRejectsMissingPackYAML(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(filepath.Join(dir, "checks"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, priv := newTestSigner(t)
	if _, err := converter.BuildArchive(dir, priv); err == nil {
		t.Fatalf("expected error for missing pack.yaml, got nil")
	} else if !errors.Is(err, converter.ErrArchivePackYAMLMissing) {
		t.Fatalf("err=%v, want ErrArchivePackYAMLMissing", err)
	}
}

func TestBuildArchiveRejectsInvalidPrivateKey(t *testing.T) {
	dir := writeFixturePackDir(t)
	if _, err := converter.BuildArchive(dir, nil); err == nil {
		t.Fatalf("expected error for nil private key")
	} else if !errors.Is(err, converter.ErrArchiveInvalidKey) {
		t.Fatalf("err=%v, want ErrArchiveInvalidKey", err)
	}
	short := make([]byte, 16)
	if _, err := converter.BuildArchive(dir, short); err == nil {
		t.Fatalf("expected error for short private key")
	} else if !errors.Is(err, converter.ErrArchiveInvalidKey) {
		t.Fatalf("err=%v, want ErrArchiveInvalidKey", err)
	}
}

// MANIFEST.json은 canonical(키 알파벳순, files path 정렬, 공백 없음) — 외부 도구가
// 같은 형식으로 sha256 재계산할 수 있어야 함.
func TestBuildArchiveManifestIsCanonical(t *testing.T) {
	dir := writeFixturePackDir(t)
	_, priv := newTestSigner(t)
	data, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	files := extractTarGz(t, data)

	manifestBytes := files["MANIFEST.json"]
	if len(manifestBytes) == 0 {
		t.Fatal("MANIFEST.json missing")
	}
	// canonical 형태 확인: 공백 0, 키 순서 files→packKey→schemaVersion.
	if bytes.Contains(manifestBytes, []byte("  ")) || bytes.Contains(manifestBytes, []byte("\n")) {
		t.Fatalf("manifest is not canonical (contains whitespace): %s", manifestBytes)
	}
	if !bytes.Contains(manifestBytes, []byte(`"packKey":"rosshield-cis-ubuntu-2404-1.0.0"`)) {
		t.Fatalf("packKey not as expected: %s", manifestBytes)
	}

	// files 배열은 path 알파벳순.
	var manifest struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	for i := 1; i < len(manifest.Files); i++ {
		if manifest.Files[i-1].Path > manifest.Files[i].Path {
			t.Fatalf("manifest files not sorted: %v", manifest.Files)
		}
	}
}

// archive는 stable — 같은 입력으로 두 번 빌드해도 결과가 동일해야 한다.
// (Phase 2에서 reproducible build 검증에 사용.)
//
// gzip은 timestamp를 포함하지만 archive/tar.Writer 자체도 timestamp 0으로 명시 지정해야 안정.
func TestBuildArchiveByteForByteStable(t *testing.T) {
	dir := writeFixturePackDir(t)
	_, priv := newTestSigner(t)
	a, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive#1: %v", err)
	}
	b, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive#2: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("archive bytes not stable across two builds — gzip timestamp 또는 tar header 비결정성 의심")
	}
}

// 외부 파일·심볼릭 링크·tar header 비표준 entry는 build에서 거르거나 안전 처리.
//
// 본 함수는 Windows/Unix 모두에서 동작해야 하므로 단순한 hidden file 추가만 검증.
func TestBuildArchiveIncludesAllRegularFiles(t *testing.T) {
	dir := writeFixturePackDir(t)
	_, priv := newTestSigner(t)

	// extra 파일 추가 — manifest에 포함되어야 한다 (외부에서 임의 삽입 차단은 호출자 책임 영역,
	// 우리는 변환 흐름에서만 디스크에 쓰므로 모든 regular file은 manifest에 등재).
	extraPath := filepath.Join(dir, "checks", "EXTRA.yaml")
	body := []byte("apiVersion: rosshield.io/v1\nkind: Check\nmetadata:\n  id: EXTRA\n  title: extra\n  severity: low\nspec:\n  auditCommand: 'true'\n  evaluationRule:\n    op: contains\n    value: '** PASS **'\n")
	if err := os.WriteFile(extraPath, body, 0o644); err != nil {
		t.Fatalf("write extra: %v", err)
	}

	data, err := converter.BuildArchive(dir, priv)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	files := extractTarGz(t, data)
	if _, ok := files["checks/EXTRA.yaml"]; !ok {
		t.Fatalf("EXTRA.yaml not in archive: %v", strings.Join(keys(files), ", "))
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
