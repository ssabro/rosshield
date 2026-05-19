package rotation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// fileScheme은 file backend URI 식별자입니다.
const fileScheme = "file"

// FileBackend는 로컬 filesystem에 archive를 저장합니다 (Apache-2.0, default).
//
// URI 형식: file://<absolute-path>
// 예: file:///var/lib/rosshield/audit-archives/tn_x/seg-0001.tar.gz
//
// 동시성: Put은 동일 key 재호출 시 덮어쓰기 (write+rename atomic on POSIX·NTFS).
type FileBackend struct {
	root string
}

// NewFileBackend는 root 디렉토리 (절대 경로) 하위에 archive를 저장하는 FileBackend를 만듭니다.
// root는 호출 시 mkdir -p 됩니다. 권한 0755.
func NewFileBackend(root string) (*FileBackend, error) {
	if root == "" {
		return nil, errors.New("rotation: FileBackend root must be non-empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("rotation: FileBackend abs path: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("rotation: FileBackend mkdir %s: %w", abs, err)
	}
	return &FileBackend{root: abs}, nil
}

// Scheme는 "file"을 반환합니다.
func (f *FileBackend) Scheme() string { return fileScheme }

// Put은 root/key 경로에 data를 저장하고 file:// URI를 반환합니다.
//
// key는 상대 경로 (예: "tn_x/seg-0001.tar.gz"). 절대 경로·상위 디렉토리 escape (../) 거부.
func (f *FileBackend) Put(ctx context.Context, key string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateRelKey(key); err != nil {
		return "", err
	}

	target := filepath.Join(f.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("rotation: file backend mkdir: %w", err)
	}

	// write+rename atomic.
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", fmt.Errorf("rotation: file backend write tmp: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rotation: file backend rename: %w", err)
	}

	return toFileURI(target), nil
}

// Get은 file:// URI에서 본문을 읽습니다. 없으면 ErrNotExist.
func (f *FileBackend) Get(ctx context.Context, uri string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := f.uriToPath(uri)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("rotation: file backend read: %w", err)
	}
	return data, nil
}

// Exists는 file:// URI 객체 존재 여부를 반환합니다.
func (f *FileBackend) Exists(ctx context.Context, uri string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	path, err := f.uriToPath(uri)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("rotation: file backend stat: %w", err)
	}
	return true, nil
}

// validateRelKey는 key가 상대 경로 + escape 없는지 검사합니다.
func validateRelKey(key string) error {
	if key == "" {
		return errors.New("rotation: key must be non-empty")
	}
	if filepath.IsAbs(key) || strings.HasPrefix(key, "/") {
		return fmt.Errorf("rotation: key must be relative, got %q", key)
	}
	clean := filepath.ToSlash(filepath.Clean(key))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("rotation: key escapes root, got %q", key)
	}
	return nil
}

// toFileURI는 절대 경로를 file:// URI로 변환합니다.
//
// Windows: file://C:/path → file:///C:/path (RFC 8089) — net/url 호환 위해 3 slash.
// POSIX:   file:///abs/path
func toFileURI(absPath string) string {
	p := filepath.ToSlash(absPath)
	if !strings.HasPrefix(p, "/") {
		// Windows drive letter — prepend `/` so URI becomes `file:///C:/...`.
		p = "/" + p
	}
	return fileScheme + "://" + p
}

// uriToPath는 file:// URI에서 OS native 경로를 추출합니다.
func (f *FileBackend) uriToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("rotation: parse uri %q: %w", uri, err)
	}
	if u.Scheme != fileScheme {
		return "", fmt.Errorf("rotation: file backend got scheme %q, want %q", u.Scheme, fileScheme)
	}
	p := u.Path
	// Windows path normalize: /C:/... → C:/...
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	abs := filepath.FromSlash(p)
	// root 하위 강제 — escape 차단.
	rel, err := filepath.Rel(f.root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("rotation: uri %q outside backend root %q", uri, f.root)
	}
	return abs, nil
}
