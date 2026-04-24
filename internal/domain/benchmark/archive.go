package benchmark

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// 보안 한계 (C8 결정):
//   - 파일당 최대 16MiB — zip bomb 방지
//   - 총 압축 해제 크기 256MiB
//   - path traversal 차단 ("..", 절대 경로 거부)
const (
	MaxFileSize   = 16 * 1024 * 1024
	MaxTotalSize  = 256 * 1024 * 1024
	manifestFile  = "MANIFEST.json"
	signatureFile = "SIGNATURE"
	packYAMLFile  = "pack.yaml"
	checksDir     = "checks/"
	selftestDir   = "selftest/"
)

// ManifestEntry는 MANIFEST.json의 한 항목입니다.
type ManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"` // hex 64자
	Size   int64  `json:"size"`
}

// Manifest는 팩의 MANIFEST.json 구조입니다.
//
// JSON canonical form: 키 알파벳순, files는 path 알파벳순. 외부 검증 도구가
// 동일 입력으로 sha256을 재계산할 수 있도록 결정적.
type Manifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	PackKey       string          `json:"packKey"`
	Files         []ManifestEntry `json:"files"`
}

// archiveContent는 tar.gz 해체 결과입니다 (메모리).
type archiveContent struct {
	files map[string][]byte // path → bytes (만이 16MiB)
}

var (
	ErrManifestMissing      = errors.New("benchmark: MANIFEST.json not found in archive")
	ErrSignatureMissing     = errors.New("benchmark: SIGNATURE not found in archive")
	ErrManifestInvalid      = errors.New("benchmark: MANIFEST.json malformed")
	ErrManifestHashMismatch = errors.New("benchmark: file hash does not match manifest")
	ErrManifestExtraFile    = errors.New("benchmark: archive contains files not listed in manifest")
	ErrSignatureInvalid     = errors.New("benchmark: SIGNATURE Ed25519 verify failed")
	ErrInvalidSignatureSize = errors.New("benchmark: SIGNATURE must be 64 bytes (Ed25519)")
	ErrPathTraversal        = errors.New("benchmark: archive entry has unsafe path")
	ErrFileTooBig           = errors.New("benchmark: file exceeds size limit")
	ErrArchiveTooBig        = errors.New("benchmark: archive total size exceeds limit")
)

// extractTarGz는 tar.gz 바이트를 검증하면서 메모리에 풀어 archiveContent를 반환합니다.
//
// 안전성:
//   - path traversal 차단 (`..`, 절대 경로)
//   - 파일당 16MiB / 총 256MiB 제한 (zip bomb 방지)
//   - tar streaming reader는 entry마다 read 완료 후 다음 entry로 넘어가야 함 (리서치 함정)
func extractTarGz(data []byte) (*archiveContent, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("benchmark: gzip open: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	out := &archiveContent{files: make(map[string][]byte)}
	var totalSize int64

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("benchmark: tar next: %w", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}

		clean := path.Clean(hdr.Name)
		if strings.HasPrefix(clean, "/") || strings.Contains(clean, "..") {
			return nil, fmt.Errorf("%w: %q", ErrPathTraversal, hdr.Name)
		}
		if hdr.Size > MaxFileSize {
			return nil, fmt.Errorf("%w: %q (%d bytes)", ErrFileTooBig, clean, hdr.Size)
		}
		if totalSize+hdr.Size > MaxTotalSize {
			return nil, fmt.Errorf("%w: %d > %d", ErrArchiveTooBig, totalSize+hdr.Size, MaxTotalSize)
		}

		buf := make([]byte, 0, hdr.Size)
		w := bytes.NewBuffer(buf)
		// 읽기 한도 초과 차단 (헤더 거짓말 방어).
		n, err := io.CopyN(w, tr, MaxFileSize+1)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("benchmark: read entry %q: %w", clean, err)
		}
		if n > MaxFileSize {
			return nil, fmt.Errorf("%w: %q exceeded during read", ErrFileTooBig, clean)
		}
		out.files[clean] = w.Bytes()
		totalSize += n
	}
	return out, nil
}

// VerifyArchive는 tar.gz 바이트를 받아 MANIFEST + SIGNATURE까지 검증한 archiveContent를 반환합니다.
//
// 검증 순서:
//  1. tar.gz 안전 해체
//  2. SIGNATURE = Ed25519(MANIFEST.json bytes) 검증
//  3. MANIFEST.json 파싱 + 각 파일 sha256 재계산 + 비교
//  4. 매니페스트 외 추가 파일 거부 (SIGNATURE/MANIFEST.json 자체는 예외)
//
// 반환된 archiveContent로 ParsePackYAML/ParseCheckYAML 호출.
func VerifyArchive(data []byte, publicKey ed25519.PublicKey) (*archiveContent, *Manifest, error) {
	arc, err := extractTarGz(data)
	if err != nil {
		return nil, nil, err
	}

	manifestBytes, ok := arc.files[manifestFile]
	if !ok {
		return nil, nil, ErrManifestMissing
	}
	signature, ok := arc.files[signatureFile]
	if !ok {
		return nil, nil, ErrSignatureMissing
	}
	if len(signature) != ed25519.SignatureSize {
		return nil, nil, fmt.Errorf("%w: got %d", ErrInvalidSignatureSize, len(signature))
	}

	// SIGNATURE 검증을 가장 먼저 — 이후 단계는 검증된 manifest를 신뢰.
	if !ed25519.Verify(publicKey, manifestBytes, signature) {
		return nil, nil, ErrSignatureInvalid
	}

	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}

	listed := make(map[string]struct{}, len(manifest.Files))
	for _, entry := range manifest.Files {
		body, ok := arc.files[entry.Path]
		if !ok {
			return nil, nil, fmt.Errorf("%w: file %q listed in manifest but missing in archive",
				ErrManifestInvalid, entry.Path)
		}
		if int64(len(body)) != entry.Size {
			return nil, nil, fmt.Errorf("%w: %q size mismatch (archive=%d, manifest=%d)",
				ErrManifestHashMismatch, entry.Path, len(body), entry.Size)
		}
		sum := sha256.Sum256(body)
		got := hex.EncodeToString(sum[:])
		if got != entry.SHA256 {
			return nil, nil, fmt.Errorf("%w: %q (archive=%s, manifest=%s)",
				ErrManifestHashMismatch, entry.Path, got, entry.SHA256)
		}
		listed[entry.Path] = struct{}{}
	}

	// 매니페스트 외 추가 파일 거부 (MANIFEST·SIGNATURE 자체는 예외).
	for p := range arc.files {
		if p == manifestFile || p == signatureFile {
			continue
		}
		if _, ok := listed[p]; !ok {
			return nil, nil, fmt.Errorf("%w: %q", ErrManifestExtraFile, p)
		}
	}

	return arc, &manifest, nil
}

// CanonicalManifest는 결정적 JSON 직렬화 — 외부 검증 도구가 같은 형식으로 sha256 재계산 가능.
//
// 형식: {"files": [...sorted by path...], "packKey": "...", "schemaVersion": N}
// 키 알파벳순, files 정렬, 공백 없음.
func CanonicalManifest(m Manifest) ([]byte, error) {
	sorted := make([]ManifestEntry, len(m.Files))
	copy(sorted, m.Files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	canonical := struct {
		Files         []ManifestEntry `json:"files"`
		PackKey       string          `json:"packKey"`
		SchemaVersion int             `json:"schemaVersion"`
	}{
		Files:         sorted,
		PackKey:       m.PackKey,
		SchemaVersion: m.SchemaVersion,
	}
	return json.Marshal(canonical)
}

// ManifestHashOf는 canonical manifest bytes의 sha256을 반환합니다 (DB packs.manifest_hash 저장용).
func ManifestHashOf(manifestBytes []byte) [32]byte {
	return sha256.Sum256(manifestBytes)
}
