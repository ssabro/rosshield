// Package fs는 blobstore.Store의 filesystem 구현체입니다.
//
// 레이아웃: <root>/<aa>/<bb>/<sha>.blob (sha[0:2]/sha[2:4] 2-level shard, R9-2)
// 보조 디렉터리:
//   - <root>/.staging/    : atomic write용 temp 파일 (rename 직전까지)
//   - <root>/.quarantine/ : hash mismatch 격리 — 자동 삭제 X (R9-3)
//
// 외부 dep 0 (stdlib만). Cross-platform 함정 11종 노트(`docs/design/notes/e7-blobstore-research.md`) 따름.
package fs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/ssabro/rosshield/internal/platform/blobstore"
)

const (
	stagingSubdir    = ".staging"
	quarantineSubdir = ".quarantine"
	blobExt          = ".blob"

	// Windows MAX_PATH(260)에서 sha hex(64) + ext(.blob, 5) + shard(aa/bb/, 6) =
	// 75자 prefix. root는 약 185자까지 안전. 여유 두고 240자에서 거부.
	maxRootPathLen = 240

	// SHARING_VIOLATION·OneDrive·AV 잠금 retry.
	retryAttempts = 3
	retryDelay    = 100 * time.Millisecond
)

// shaPattern은 lowercase 64-hex만 허용 (R9-5).
var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type store struct {
	root          string
	stagingDir    string
	quarantineDir string
}

// New는 root 디렉터리 기반 filesystem Store를 만듭니다.
// root는 자동 생성. <root>/.staging/, <root>/.quarantine/ 하위 디렉터리도 자동 생성.
//
// root는 절대경로 권장. Windows long-path 함정(F9) 방어 — 240자 초과 시 거부.
func New(root string) (blobstore.Store, error) {
	if root == "" {
		return nil, errors.New("blobstore/fs: empty root")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("blobstore/fs: abs root: %w", err)
	}
	if len(abs) > maxRootPathLen {
		return nil, fmt.Errorf("blobstore/fs: root path too long (%d > %d): %s",
			len(abs), maxRootPathLen, abs)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("blobstore/fs: mkdir root: %w", err)
	}
	staging := filepath.Join(abs, stagingSubdir)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return nil, fmt.Errorf("blobstore/fs: mkdir staging: %w", err)
	}
	quarantine := filepath.Join(abs, quarantineSubdir)
	if err := os.MkdirAll(quarantine, 0o755); err != nil {
		return nil, fmt.Errorf("blobstore/fs: mkdir quarantine: %w", err)
	}
	return &store{
		root:          abs,
		stagingDir:    staging,
		quarantineDir: quarantine,
	}, nil
}

// blobPath는 sha의 최종 저장 경로를 돌려줍니다 (검증 가정).
func (s *store) blobPath(sha string) string {
	return filepath.Join(s.root, sha[0:2], sha[2:4], sha+blobExt)
}

// validateSHA는 sha hex가 lowercase 64-hex인지 확인합니다.
func validateSHA(sha string) error {
	if !shaPattern.MatchString(sha) {
		return blobstore.ErrInvalidSHA
	}
	return nil
}

// randSuffix는 staging temp 파일용 crypto-rand suffix를 만듭니다.
func randSuffix() (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

// Put은 raw bytes를 sha256으로 주소화해 저장합니다 (idempotent).
func (s *store) Put(ctx context.Context, raw []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	final := s.blobPath(sha)

	// dedup: 이미 있으면 no-op.
	if _, err := os.Stat(final); err == nil {
		return sha, nil
	}

	// 1) staging temp 파일 (crypto-rand 이름).
	suffix, err := randSuffix()
	if err != nil {
		return "", fmt.Errorf("blobstore/fs: rand: %w", err)
	}
	tmpPath := filepath.Join(s.stagingDir, "blob-"+suffix+".tmp")
	tmp, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("blobstore/fs: create staging: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	// 2) bytes 쓰기 + fsync.
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("blobstore/fs: write staging: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("blobstore/fs: fsync staging: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("blobstore/fs: close staging: %w", err)
	}

	// 3) shard dir 보장 (race-safe, MkdirAll이 EEXIST 흡수).
	shardDir := filepath.Join(s.root, sha[0:2], sha[2:4])
	if err := os.MkdirAll(shardDir, 0o755); err != nil {
		cleanup()
		return "", fmt.Errorf("blobstore/fs: mkdir shard: %w", err)
	}

	// 4) 다시 dedup 확인 — 동시 worker race 대응.
	if _, err := os.Stat(final); err == nil {
		cleanup()
		return sha, nil
	}

	// 5) atomic rename (Windows SHARING_VIOLATION·OneDrive·AV retry).
	if err := renameWithRetry(tmpPath, final); err != nil {
		// rename 실패 후 동시 worker가 먼저 만든 경우 ok.
		if _, statErr := os.Stat(final); statErr == nil {
			cleanup()
			return sha, nil
		}
		cleanup()
		return "", fmt.Errorf("blobstore/fs: rename: %w", err)
	}

	// 6) Linux 전용 dir fsync (R9-1).
	if runtime.GOOS == "linux" {
		if d, derr := os.Open(shardDir); derr == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
	return sha, nil
}

// renameWithRetry는 Windows의 SHARING_VIOLATION·AV 잠금에 대응합니다.
func renameWithRetry(src, dst string) error {
	var lastErr error
	for i := 0; i < retryAttempts; i++ {
		err := os.Rename(src, dst)
		if err == nil {
			return nil
		}
		lastErr = err
		// 재시도 전 약간 대기.
		time.Sleep(retryDelay)
	}
	return lastErr
}

// Get은 sha의 평문 bytes를 반환하며 EOF 시 hash 검증을 수행합니다.
func (s *store) Get(ctx context.Context, sha string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSHA(sha); err != nil {
		return nil, err
	}
	path := s.blobPath(sha)
	data, err := readWithRetry(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, blobstore.ErrNotFound
		}
		return nil, fmt.Errorf("blobstore/fs: read: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != sha {
		// hash mismatch → quarantine 격리 + ErrCorrupted.
		_ = s.quarantine(sha, path)
		return nil, blobstore.ErrCorrupted
	}
	return data, nil
}

// readWithRetry는 SHARING_VIOLATION·잠금에 대응합니다.
func readWithRetry(path string) ([]byte, error) {
	var lastErr error
	for i := 0; i < retryAttempts; i++ {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
		// 부재는 retry 무의미.
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		lastErr = err
		time.Sleep(retryDelay)
	}
	return nil, lastErr
}

// Open은 streaming reader를 반환하며 Close 시 hash 검증합니다.
func (s *store) Open(ctx context.Context, sha string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSHA(sha); err != nil {
		return nil, err
	}
	path := s.blobPath(sha)
	f, err := openWithRetry(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, blobstore.ErrNotFound
		}
		return nil, fmt.Errorf("blobstore/fs: open: %w", err)
	}
	return &verifyingReader{
		f:     f,
		want:  sha,
		h:     sha256.New(),
		store: s,
		path:  path,
	}, nil
}

func openWithRetry(path string) (*os.File, error) {
	var lastErr error
	for i := 0; i < retryAttempts; i++ {
		f, err := os.Open(path)
		if err == nil {
			return f, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		lastErr = err
		time.Sleep(retryDelay)
	}
	return nil, lastErr
}

// verifyingReader는 Read 시 hash를 누적하고 Close 시 검증합니다.
type verifyingReader struct {
	f      *os.File
	want   string
	h      hash.Hash
	store  *store
	path   string
	closed bool
}

func (r *verifyingReader) Read(p []byte) (int, error) {
	n, err := r.f.Read(p)
	if n > 0 {
		_, _ = r.h.Write(p[:n])
	}
	return n, err
}

func (r *verifyingReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	closeErr := r.f.Close()
	got := hex.EncodeToString(r.h.Sum(nil))
	if got != r.want {
		// hash mismatch → quarantine + ErrCorrupted.
		_ = r.store.quarantine(r.want, r.path)
		return blobstore.ErrCorrupted
	}
	return closeErr
}

// Verify는 명시적으로 hash를 재계산합니다.
func (s *store) Verify(ctx context.Context, sha string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSHA(sha); err != nil {
		return err
	}
	path := s.blobPath(sha)
	data, err := readWithRetry(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return blobstore.ErrNotFound
		}
		return fmt.Errorf("blobstore/fs: verify read: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != sha {
		_ = s.quarantine(sha, path)
		return blobstore.ErrCorrupted
	}
	return nil
}

// Exists는 검증 없이 빠른 존재 조회.
func (s *store) Exists(ctx context.Context, sha string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := validateSHA(sha); err != nil {
		return false, err
	}
	if _, err := os.Stat(s.blobPath(sha)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("blobstore/fs: stat: %w", err)
	}
	return true, nil
}

// Delete는 GC 전용 — Phase 1에서는 미구현.
func (s *store) Delete(ctx context.Context, sha string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSHA(sha); err != nil {
		return err
	}
	return blobstore.ErrUnsupported
}

// quarantine은 corrupted 파일을 .quarantine으로 이동합니다.
// 자동 삭제 X — forensic 보존(R9-3).
func (s *store) quarantine(sha, src string) error {
	name := fmt.Sprintf("%s-%d", sha, time.Now().UnixNano())
	// 충돌 회피 (같은 nano 시각에 두 번 격리되는 경우는 거의 없으나 안전장치).
	dst := filepath.Join(s.quarantineDir, name)
	if _, err := os.Stat(dst); err == nil {
		// 충돌 시 suffix 추가.
		suffix, _ := randSuffix()
		dst = dst + "-" + suffix
	}
	if err := renameWithRetry(src, dst); err != nil {
		// rename 실패 시 fallback — 원본을 강제 삭제하지 않고 에러만 반환.
		// 호출자(Get/Verify)는 ErrCorrupted를 사용자에게 통보.
		return fmt.Errorf("blobstore/fs: quarantine rename: %w", err)
	}
	return nil
}

// 컴파일 타임에 Store 인터페이스 만족 보장.
var _ blobstore.Store = (*store)(nil)
