package benchmark_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/benchmark"
)

// packBuilder는 in-memory tar.gz 팩을 만드는 테스트 헬퍼입니다.
type packBuilder struct {
	files map[string][]byte
}

func newPackBuilder() *packBuilder {
	return &packBuilder{files: map[string][]byte{}}
}

func (b *packBuilder) Add(path string, body []byte) *packBuilder {
	b.files[path] = body
	return b
}

// Build는 MANIFEST.json + SIGNATURE를 자동 생성하고 tar.gz 바이트를 반환합니다.
func (b *packBuilder) Build(t *testing.T, packKey string, priv ed25519.PrivateKey) []byte {
	t.Helper()

	entries := make([]benchmark.ManifestEntry, 0, len(b.files))
	for path, body := range b.files {
		sum := sha256.Sum256(body)
		entries = append(entries, benchmark.ManifestEntry{
			Path:   path,
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(body)),
		})
	}
	manifestBytes, err := benchmark.CanonicalManifest(benchmark.Manifest{
		SchemaVersion: 1,
		PackKey:       packKey,
		Files:         entries,
	})
	if err != nil {
		t.Fatalf("CanonicalManifest: %v", err)
	}
	signature := ed25519.Sign(priv, manifestBytes)

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)

	writeFile := func(name string, body []byte) {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %q: %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("write body %q: %v", name, err)
		}
	}
	writeFile("MANIFEST.json", manifestBytes)
	writeFile("SIGNATURE", signature)
	for path, body := range b.files {
		writeFile(path, body)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}
	return gzBuf.Bytes()
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

// E4 보조 — 정상 archive 라운드트립.
func TestVerifyArchiveAcceptsValidSignedPack(t *testing.T) {
	t.Parallel()
	pub, priv := newKey(t)

	tarGz := newPackBuilder().
		Add("pack.yaml", []byte(validPackYAML)).
		Add("checks/CIS-1.1.1.1.yaml", []byte(validCheckYAML)).
		Build(t, "cis-cis-ubuntu-2404-v1.0.0", priv)

	_, manifest, err := benchmark.VerifyArchive(tarGz, pub)
	if err != nil {
		t.Fatalf("VerifyArchive: %v", err)
	}
	if manifest.PackKey != "cis-cis-ubuntu-2404-v1.0.0" {
		t.Errorf("PackKey = %q", manifest.PackKey)
	}
	if len(manifest.Files) != 2 {
		t.Errorf("Files = %d, want 2", len(manifest.Files))
	}
}

// E4.T2 본체 — SIGNATURE가 잘못된 키로 서명됐으면 거부.
func TestPackRejectsInvalidSignature(t *testing.T) {
	t.Parallel()
	wrongPub, _ := newKey(t)
	_, signedPriv := newKey(t) // 다른 키로 서명

	tarGz := newPackBuilder().
		Add("pack.yaml", []byte(validPackYAML)).
		Build(t, "cis-cis-ubuntu-2404-v1.0.0", signedPriv)

	_, _, err := benchmark.VerifyArchive(tarGz, wrongPub)
	if !errors.Is(err, benchmark.ErrSignatureInvalid) {
		t.Errorf("err = %v, want ErrSignatureInvalid", err)
	}
}

// E4.T3 본체 — manifest hash와 실제 파일 hash가 다르면 거부.
// 정상 archive를 만든 후 raw byte 변조 — 어렵기 때문에, 직접 잘못된 manifest를 넣어서 테스트.
func TestPackRejectsHashMismatch(t *testing.T) {
	t.Parallel()
	pub, priv := newKey(t)

	// 의도적으로 manifest의 sha256을 잘못 적은 archive 만들기.
	body := []byte(validPackYAML)
	wrongHash := sha256.Sum256([]byte("different content"))
	manifest := benchmark.Manifest{
		SchemaVersion: 1,
		PackKey:       "cis-cis-ubuntu-2404-v1.0.0",
		Files: []benchmark.ManifestEntry{
			{
				Path:   "pack.yaml",
				SHA256: hex.EncodeToString(wrongHash[:]), // ← 잘못된 hash
				Size:   int64(len(body)),
			},
		},
	}
	manifestBytes, _ := benchmark.CanonicalManifest(manifest)
	signature := ed25519.Sign(priv, manifestBytes)

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{
		{"MANIFEST.json", manifestBytes},
		{"SIGNATURE", signature},
		{"pack.yaml", body},
	} {
		_ = tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.body))})
		_, _ = tw.Write(f.body)
	}
	_ = tw.Close()
	_ = gz.Close()

	_, _, err := benchmark.VerifyArchive(gzBuf.Bytes(), pub)
	if !errors.Is(err, benchmark.ErrManifestHashMismatch) {
		t.Errorf("err = %v, want ErrManifestHashMismatch", err)
	}
}

// MANIFEST 누락.
func TestVerifyArchiveRequiresManifest(t *testing.T) {
	t.Parallel()
	pub, _ := newKey(t)

	// pack.yaml만 있고 MANIFEST/SIGNATURE 없는 archive.
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "pack.yaml", Size: int64(len(validPackYAML))})
	_, _ = tw.Write([]byte(validPackYAML))
	_ = tw.Close()
	_ = gz.Close()

	_, _, err := benchmark.VerifyArchive(gzBuf.Bytes(), pub)
	if !errors.Is(err, benchmark.ErrManifestMissing) {
		t.Errorf("err = %v, want ErrManifestMissing", err)
	}
}

// path traversal 차단.
func TestExtractTarGzRejectsPathTraversal(t *testing.T) {
	t.Parallel()
	pub, priv := newKey(t)

	// 정상 파일 + 절대 경로 파일을 같이 넣되, manifest는 정상 파일만 listing.
	// extractTarGz 단계에서 path traversal 차단 — verifyArchive까지 못 감.
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)
	body := []byte("malicious")
	_ = tw.WriteHeader(&tar.Header{Name: "../../etc/passwd", Size: int64(len(body))})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()

	_, _, err := benchmark.VerifyArchive(gzBuf.Bytes(), pub)
	if !errors.Is(err, benchmark.ErrPathTraversal) {
		t.Errorf("err = %v, want ErrPathTraversal", err)
	}
	_ = priv
}

// MANIFEST 외 추가 파일 거부.
func TestVerifyArchiveRejectsUnlistedFiles(t *testing.T) {
	t.Parallel()
	pub, priv := newKey(t)

	// pack.yaml만 manifest에 listed, extra.txt는 listing 없이 추가.
	body := []byte(validPackYAML)
	manifest := benchmark.Manifest{
		SchemaVersion: 1,
		PackKey:       "cis-cis-ubuntu-2404-v1.0.0",
		Files: []benchmark.ManifestEntry{
			{Path: "pack.yaml", SHA256: hexSum(body), Size: int64(len(body))},
		},
	}
	manifestBytes, _ := benchmark.CanonicalManifest(manifest)
	signature := ed25519.Sign(priv, manifestBytes)

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{
		{"MANIFEST.json", manifestBytes},
		{"SIGNATURE", signature},
		{"pack.yaml", body},
		{"extra.txt", []byte("not in manifest")}, // ← 목록 외
	} {
		_ = tw.WriteHeader(&tar.Header{Name: f.name, Size: int64(len(f.body))})
		_, _ = tw.Write(f.body)
	}
	_ = tw.Close()
	_ = gz.Close()

	_, _, err := benchmark.VerifyArchive(gzBuf.Bytes(), pub)
	if !errors.Is(err, benchmark.ErrManifestExtraFile) {
		t.Errorf("err = %v, want ErrManifestExtraFile", err)
	}
}

func hexSum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// E4 — full LoadPackFromTar 라운드트립 (Stage A·B 통합).
func TestLoadPackFromTarValidEndToEnd(t *testing.T) {
	t.Parallel()
	pub, priv := newKey(t)

	tarGz := newPackBuilder().
		Add("pack.yaml", []byte(validPackYAML)).
		Add("checks/CIS-1.1.1.1.yaml", []byte(validCheckYAML)).
		Build(t, "cis-cis-ubuntu-2404-v1.0.0", priv)

	pack, err := benchmark.LoadPackFromTar(tarGz, pub)
	if err != nil {
		t.Fatalf("LoadPackFromTar: %v", err)
	}
	if pack.Name != "cis-ubuntu-2404" {
		t.Errorf("Name = %q", pack.Name)
	}
	if pack.PackKey != "cis-cis-ubuntu-2404-v1.0.0" {
		t.Errorf("PackKey = %q", pack.PackKey)
	}
	if len(pack.Checks) != 1 {
		t.Fatalf("Checks = %d, want 1", len(pack.Checks))
	}
	if pack.Checks[0].CheckID != "CIS-1.1.1.1" {
		t.Errorf("Check.CheckID = %q", pack.Checks[0].CheckID)
	}
	if pack.ManifestHash == ([32]byte{}) {
		t.Error("ManifestHash should be set")
	}
}

// pack.yaml의 packKey와 MANIFEST의 packKey 불일치 → ErrSchemaViolation.
func TestLoadPackFromTarRejectsManifestMismatch(t *testing.T) {
	t.Parallel()
	pub, priv := newKey(t)

	tarGz := newPackBuilder().
		Add("pack.yaml", []byte(validPackYAML)).
		Build(t, "wrong-pack-key-v9.9.9", priv) // ← pack.yaml과 다른 PackKey

	_, err := benchmark.LoadPackFromTar(tarGz, pub)
	if err == nil {
		t.Fatal("expected ErrSchemaViolation for mismatched packKey")
	}
}

// 같은 check ID 두 개 — 중복 거부.
func TestLoadPackFromTarRejectsDuplicateCheckID(t *testing.T) {
	t.Parallel()
	pub, priv := newKey(t)

	tarGz := newPackBuilder().
		Add("pack.yaml", []byte(validPackYAML)).
		Add("checks/A.yaml", []byte(validCheckYAML)).
		Add("checks/B.yaml", []byte(validCheckYAML)). // 같은 metadata.id
		Build(t, "cis-cis-ubuntu-2404-v1.0.0", priv)

	_, err := benchmark.LoadPackFromTar(tarGz, pub)
	if !errors.Is(err, benchmark.ErrDuplicateCheckID) {
		t.Errorf("err = %v, want ErrDuplicateCheckID", err)
	}
}
