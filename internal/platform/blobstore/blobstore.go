// Package blobstore는 sha256 hex 주소 기반의 content-addressed blob 저장소를 정의합니다.
//
// E7 evidence 도메인이 redact된 raw bytes를 영속화할 때 사용합니다. 같은 bytes는
// 항상 같은 sha256으로 매핑되므로 Put은 자연스럽게 idempotent하며 dedup이 보장됩니다.
//
// Phase 1은 filesystem 어댑터(`fs` sub-package)만 제공합니다. S3·MinIO 어댑터는
// Phase 3에서 동일 인터페이스로 추가됩니다(원칙 7 — 단일 바이너리 다중 껍질).
//
// 표면 설계 근거:
//   - Open/Get은 EOF 시점에 lazy hash 검증 (R9-3)
//   - Verify는 명시적 fsck/audit 경로 (R9-3)
//   - Delete는 GC 전용 — 외부 호출 금지(원칙 9 불변성)
//   - sha256 hex는 lowercase 64자만 허용 (R9-5, Windows·macOS case-insensitive FS 방어)
package blobstore

import (
	"context"
	"errors"
	"io"
)

// Store는 content-addressed blob 저장소입니다 (sha256 hex 주소).
type Store interface {
	// Put은 raw bytes를 저장하고 sha256 hex(64자 lowercase)를 반환합니다.
	// 같은 sha가 이미 있으면 no-op (idempotent).
	Put(ctx context.Context, raw []byte) (sha256Hex string, err error)

	// Get은 sha의 평문 bytes를 반환합니다. 읽기 EOF 시 자동 hash 검증 — mismatch면 ErrCorrupted.
	Get(ctx context.Context, sha256Hex string) ([]byte, error)

	// Open은 streaming 읽기용 ReadCloser를 반환합니다(거대 blob 메모리 spike 회피).
	// Close 시점에 hash 검증 — mismatch면 ErrCorrupted (Close 에러로 통보).
	Open(ctx context.Context, sha256Hex string) (io.ReadCloser, error)

	// Verify는 명시적 hash 재계산 — 반환은 nil 또는 ErrCorrupted/ErrNotFound.
	Verify(ctx context.Context, sha256Hex string) error

	// Exists는 blob 존재 여부 — 검증 없이 빠른 조회.
	Exists(ctx context.Context, sha256Hex string) (bool, error)

	// Delete는 GC 전용 — 외부 호출 금지(Phase 1은 unimplemented).
	Delete(ctx context.Context, sha256Hex string) error
}

// 공통 에러.
var (
	ErrNotFound    = errors.New("blobstore: not found")
	ErrCorrupted   = errors.New("blobstore: hash mismatch (file quarantined)")
	ErrInvalidSHA  = errors.New("blobstore: invalid sha256 hex (must be 64 lowercase hex)")
	ErrUnsupported = errors.New("blobstore: operation not supported in this phase")
)
