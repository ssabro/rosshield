package converter

// archive.go — 변환된 pack 디렉터리를 MANIFEST + SIGNATURE 포함 tar.gz로 묶습니다 (E12 Stage D · T6).
//
// 결과 archive는 production `internal/domain/benchmark.LoadPackFromTar`가 그대로 검증·로드.
// MANIFEST.json은 canonical(키 알파벳순, files path 정렬, 공백 0)이며 SIGNATURE는
// raw 64-byte Ed25519 서명(MANIFEST.json bytes에 대해).
//
// 결정성(reproducible build):
//   - tar header의 ModTime/AccessTime/ChangeTime 모두 zero (1970-01-01)
//   - tar entry 작성 순서는 path 알파벳순
//   - MANIFEST.json은 canonical 직렬화로 결정적
//   - gzip writer는 ModTime을 zero로 명시
//
// 같은 pack dir + 같은 private key ⇒ byte-for-byte 동일 archive.

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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	manifestFileName  = "MANIFEST.json"
	signatureFileName = "SIGNATURE"
	packYAMLFileName  = "pack.yaml"
)

// 에러 sentinels.
var (
	ErrArchivePackYAMLMissing = errors.New("converter: pack.yaml not found in pack dir")
	ErrArchiveInvalidKey      = errors.New("converter: invalid Ed25519 private key (must be 64 bytes)")
	ErrArchivePackYAMLInvalid = errors.New("converter: pack.yaml malformed — cannot derive packKey")
)

// manifestFileEntry는 MANIFEST.json의 한 항목입니다 (production benchmark.ManifestEntry와 1:1).
type manifestFileEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// canonicalManifestDoc은 직렬화 시 키 순서가 files→packKey→schemaVersion으로 알파벳순.
type canonicalManifestDoc struct {
	Files         []manifestFileEntry `json:"files"`
	PackKey       string              `json:"packKey"`
	SchemaVersion int                 `json:"schemaVersion"`
}

// BuildArchive는 packDir의 모든 regular file을 tar.gz로 묶고 MANIFEST + SIGNATURE를 포함합니다.
//
// privateKey는 raw 64-byte Ed25519 (`crypto/ed25519`. 표준). 본 함수는 외부 신뢰 경계
// 안에서만 호출(오프라인 도구) — KEK/HSM 흐름은 production audit 도메인에서.
//
// packDir에는 최소 `pack.yaml`이 있어야 하며 packKey는 `<vendor>-<name>-<version>` 규칙에 따라
// 자동 도출 (lower-case vendor·name + version).
//
// 기존 MANIFEST.json·SIGNATURE 파일이 있으면 무시(새로 생성한 것으로 덮어씀) — `archive`를
// 멱등하게 재실행 가능.
func BuildArchive(packDir string, privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrArchiveInvalidKey, len(privateKey))
	}

	files, err := collectPackFiles(packDir)
	if err != nil {
		return nil, err
	}
	if _, ok := files[packYAMLFileName]; !ok {
		return nil, ErrArchivePackYAMLMissing
	}

	packKey, err := derivePackKey(files[packYAMLFileName])
	if err != nil {
		return nil, err
	}

	manifestEntries := make([]manifestFileEntry, 0, len(files))
	for path, body := range files {
		sum := sha256.Sum256(body)
		manifestEntries = append(manifestEntries, manifestFileEntry{
			Path:   path,
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(body)),
		})
	}
	sort.Slice(manifestEntries, func(i, j int) bool {
		return manifestEntries[i].Path < manifestEntries[j].Path
	})

	manifestBytes, err := json.Marshal(canonicalManifestDoc{
		Files:         manifestEntries,
		PackKey:       packKey,
		SchemaVersion: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("converter: marshal manifest: %w", err)
	}
	signature := ed25519.Sign(privateKey, manifestBytes)

	// tar 안의 entry 순서: MANIFEST → SIGNATURE → 그 외 path 알파벳순.
	// MANIFEST/SIGNATURE를 가장 먼저 두면 production loader가 tar streaming 중 빠른
	// 검증 가능하나 현재 loader는 전체를 메모리에 적재하므로 결정성 목적만.
	allPaths := []string{manifestFileName, signatureFileName}
	regularPaths := make([]string, 0, len(files))
	for p := range files {
		regularPaths = append(regularPaths, p)
	}
	sort.Strings(regularPaths)
	allPaths = append(allPaths, regularPaths...)

	var gzBuf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("converter: gzip writer: %w", err)
	}
	gz.ModTime = time.Time{} // gzip header timestamp 결정적

	tw := tar.NewWriter(gz)
	for _, p := range allPaths {
		var body []byte
		switch p {
		case manifestFileName:
			body = manifestBytes
		case signatureFileName:
			body = signature
		default:
			body = files[p]
		}
		hdr := &tar.Header{
			Name:    p,
			Mode:    0o644,
			Size:    int64(len(body)),
			ModTime: time.Time{},
			Format:  tar.FormatPAX,
		}
		// PAX header도 timestamp 0으로 — tar.Writer는 PAXRecords가 비면 USTAR fallback.
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("converter: tar header %q: %w", p, err)
		}
		if _, err := tw.Write(body); err != nil {
			return nil, fmt.Errorf("converter: tar write %q: %w", p, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("converter: tar close: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("converter: gzip close: %w", err)
	}
	return gzBuf.Bytes(), nil
}

// collectPackFiles는 packDir 아래의 모든 regular file을 path → bytes로 수집합니다.
//
// path는 packDir 기준 relative + forward slash(`/`) 정규화 — tar 표준 + Windows/Unix 호환.
// MANIFEST.json·SIGNATURE는 결과에 포함하지 않음(BuildArchive가 새로 생성).
// 심볼릭 링크 등 비-regular file은 무시 — 신뢰할 수 없는 entry를 archive에 들이지 않음.
func collectPackFiles(packDir string) (map[string][]byte, error) {
	out := make(map[string][]byte)
	err := filepath.WalkDir(packDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // 심볼릭 링크·디바이스 등은 skip
		}
		rel, err := filepath.Rel(packDir, path)
		if err != nil {
			return fmt.Errorf("converter: relative path %q: %w", path, err)
		}
		rel = filepath.ToSlash(rel)
		if rel == manifestFileName || rel == signatureFileName {
			return nil // 기존 산출물은 skip — BuildArchive가 새로 생성
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("converter: read %q: %w", rel, err)
		}
		out[rel] = body
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// derivePackKey는 pack.yaml 바이트에서 `<vendor>-<name>-<version>` 키를 추출합니다.
//
// production benchmark.buildPackKey와 동일 규칙 — vendor·name은 lower-case, version은 그대로.
// schema drift 시 라운드트립 테스트가 즉시 fail.
func derivePackKey(packYAMLBytes []byte) (string, error) {
	var doc struct {
		Metadata struct {
			Name    string `yaml:"name"`
			Version string `yaml:"version"`
			Vendor  string `yaml:"vendor"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(packYAMLBytes, &doc); err != nil {
		return "", fmt.Errorf("%w: %v", ErrArchivePackYAMLInvalid, err)
	}
	v, n, ver := strings.TrimSpace(doc.Metadata.Vendor),
		strings.TrimSpace(doc.Metadata.Name),
		strings.TrimSpace(doc.Metadata.Version)
	if v == "" || n == "" || ver == "" {
		return "", fmt.Errorf("%w: vendor/name/version required", ErrArchivePackYAMLInvalid)
	}
	return fmt.Sprintf("%s-%s-%s", strings.ToLower(v), strings.ToLower(n), ver), nil
}
