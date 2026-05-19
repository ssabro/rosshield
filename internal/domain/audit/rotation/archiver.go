package rotation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
)

// archiveManifestVersion은 manifest.json 형식 호환 버전입니다.
const archiveManifestVersion = "1"

// archiveManifest는 tar.gz 내 manifest.json의 직렬화 형태입니다 (외부 검증 도구 contract).
type archiveManifest struct {
	Version      string `json:"version"`
	TenantID     string `json:"tenantId"`
	FirstEntryID int64  `json:"firstEntryId"`
	LastEntryID  int64  `json:"lastEntryId"`
	EntryCount   int64  `json:"entryCount"`
	StartedAt    string `json:"startedAt"`   // RFC3339Nano UTC
	EndedAt      string `json:"endedAt"`     // RFC3339Nano UTC
	SegmentHash  string `json:"segmentHash"` // hex (32B → 64 chars)
	EntriesFile  string `json:"entriesFile"` // "entries.ndjson"
	CreatedAt    string `json:"createdAt"`   // RFC3339Nano UTC (archive 생성 시각)
}

// Archive는 segment를 tar.gz로 직렬화하고 backend에 업로드합니다.
//
// 반환값:
//   - uri:    backend가 발행한 URI (file://... 또는 s3://...)
//   - sha256: archive bytes의 sha256 (32B).
//
// archive 본문 구조 (tar.gz):
//
//	manifest.json     — 메타 (version, segment_hash, ranges, ...)
//	entries.ndjson    — entry 한 줄씩 (audit.MarshalEntryLine 동일 형식)
//
// 외부 검증 도구는 tar.gz unwrap → entries.ndjson sha256 검증 (불필요 — manifest segment_hash로 충분) →
// manifest.json의 segment_hash와 ComputeSegmentHash 재계산 결과 비교 → cosign signature 검증
// (cosign_bundle은 본 round 미구현, Stage 5 별 epic).
func Archive(ctx context.Context, segment *Segment, backend Backend, key string, now time.Time) (uri string, sha256sum []byte, err error) {
	if segment == nil {
		return "", nil, fmt.Errorf("rotation: Archive: segment required")
	}
	if backend == nil {
		return "", nil, fmt.Errorf("rotation: Archive: backend required")
	}
	if key == "" {
		return "", nil, fmt.Errorf("rotation: Archive: key required")
	}

	body, err := buildTarGz(segment, now)
	if err != nil {
		return "", nil, err
	}

	sum := sha256.Sum256(body)

	uri, err = backend.Put(ctx, key, body)
	if err != nil {
		return "", nil, fmt.Errorf("rotation: Archive: backend put: %w", err)
	}
	return uri, sum[:], nil
}

// buildTarGz는 segment를 tar.gz bytes로 직렬화합니다 (in-memory).
//
// 대용량 segment에서는 streaming + tempfile이 더 적절하지만 본 round는 단순화.
// 추정 1M entry × 1KB = 1GB, in-memory OK한 site 가정.
// Stage 5에서 streaming 옵션 추가 가능.
func buildTarGz(segment *Segment, now time.Time) ([]byte, error) {
	var entriesBuf bytes.Buffer
	for _, e := range segment.Entries {
		line, err := audit.MarshalEntryLine(e)
		if err != nil {
			return nil, fmt.Errorf("rotation: marshal entry seq=%d: %w", e.Seq, err)
		}
		entriesBuf.Write(line)
		entriesBuf.WriteByte('\n')
	}

	manifest := archiveManifest{
		Version:      archiveManifestVersion,
		TenantID:     string(segment.TenantID),
		FirstEntryID: segment.FirstEntryID,
		LastEntryID:  segment.LastEntryID,
		EntryCount:   segment.EntryCount,
		StartedAt:    segment.StartedAt.UTC().Format(time.RFC3339Nano),
		EndedAt:      segment.EndedAt.UTC().Format(time.RFC3339Nano),
		SegmentHash:  hex.EncodeToString(segment.Hash[:]),
		EntriesFile:  "entries.ndjson",
		CreatedAt:    now.UTC().Format(time.RFC3339Nano),
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("rotation: marshal manifest: %w", err)
	}

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)

	if err := writeTarFile(tw, "manifest.json", manifestJSON, now); err != nil {
		return nil, err
	}
	if err := writeTarFile(tw, "entries.ndjson", entriesBuf.Bytes(), now); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("rotation: tar close: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("rotation: gzip close: %w", err)
	}
	return gzBuf.Bytes(), nil
}

func writeTarFile(tw *tar.Writer, name string, data []byte, now time.Time) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: now.UTC(),
		Format:  tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("rotation: tar header %q: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("rotation: tar write %q: %w", name, err)
	}
	return nil
}
