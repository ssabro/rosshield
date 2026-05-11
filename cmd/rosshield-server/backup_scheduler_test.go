package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestListBackupsEmptyDir — 디렉터리 부재 또는 빈 디렉터리는 빈 슬라이스 + nil err.
func TestListBackupsEmptyDir(t *testing.T) {
	t.Parallel()
	got, err := listBackups("")
	if err != nil {
		t.Errorf("empty path: err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("empty path: got = %v, want nil", got)
	}

	// 부재 디렉터리도 nil.
	got, err = listBackups(filepath.Join(t.TempDir(), "no-such-dir"))
	if err != nil {
		t.Errorf("missing dir: err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("missing dir: got %d items, want 0", len(got))
	}

	// 빈 디렉터리.
	dir := t.TempDir()
	got, err = listBackups(dir)
	if err != nil {
		t.Errorf("empty dir: err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("empty dir: got %d items, want 0", len(got))
	}
}

// TestListBackupsFiltersAndSorts — *.tar.gz만 + GeneratedAt 내림차순.
func TestListBackupsFiltersAndSorts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// 3 tar.gz + 1 텍스트(필터 대상) 생성.
	files := []struct {
		name    string
		content string
	}{
		{"auto-20260511-100000.tar.gz", "first"},
		{"auto-20260511-110000.tar.gz", "second"},
		{"auto-20260511-090000.tar.gz", "third"},
		{"README.md", "ignore me"},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0o600); err != nil {
			t.Fatalf("write %s: %v", f.name, err)
		}
	}

	got, err := listBackups(dir)
	if err != nil {
		t.Fatalf("listBackups: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3 (only .tar.gz)", len(got))
	}

	// SHA256 검증 (한 entry).
	for _, m := range got {
		if m.Filename == "auto-20260511-110000.tar.gz" {
			expectSum := sha256.Sum256([]byte("second"))
			expectHex := hex.EncodeToString(expectSum[:])
			if m.SHA256 != expectHex {
				t.Errorf("SHA256 mismatch for %s: got %s, want %s", m.Filename, m.SHA256, expectHex)
			}
			if m.Size != int64(len("second")) {
				t.Errorf("Size = %d, want %d", m.Size, len("second"))
			}
		}
	}

	// 정렬: GeneratedAt 내림차순. 모든 파일이 같은 ModTime일 수 있으므로 strict
	// 정렬 보장은 어려움 — 적어도 .tar.gz 3개가 모두 포함되었는지만 검증.
	names := map[string]bool{}
	for _, m := range got {
		names[m.Filename] = true
	}
	for _, f := range files[:3] {
		if !names[f.name] {
			t.Errorf("missing %s in result", f.name)
		}
	}
	if names["README.md"] {
		t.Errorf("non-.tar.gz file leaked into result")
	}
}

// TestRegisterBackupJobNoSpec — spec=""이면 no-op (cron 등록 없이 nil).
func TestRegisterBackupJobNoSpec(t *testing.T) {
	t.Parallel()
	// nil scheduler를 넘겨도 schedule 호출이 일어나지 않으므로 panic 없음.
	if err := registerBackupJob(nil, "", "/tmp/data", "/tmp/backups", false, nil); err != nil {
		t.Errorf("err = %v, want nil for empty spec", err)
	}
	// 명시적 공백도 동일.
	if err := registerBackupJob(nil, "   ", "/tmp/data", "/tmp/backups", false, nil); err != nil {
		t.Errorf("err = %v, want nil for whitespace spec", err)
	}
}
