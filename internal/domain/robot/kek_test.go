package robot_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/robot"
)

func TestKEKLoadOrCreateGeneratesNewKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credential.kek")

	kek, err := robot.LoadOrCreateKEK(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKEK: %v", err)
	}
	if kek.KeyID() == "" {
		t.Error("KeyID should not be empty")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != int64(robot.KEKSizeBytes) {
		t.Errorf("file size = %d, want %d", info.Size(), robot.KEKSizeBytes)
	}
	if runtime.GOOS != "windows" {
		mode := info.Mode().Perm()
		if mode&0o077 != 0 {
			t.Errorf("file perm = %o, want no group/other access", mode)
		}
	}
}

func TestKEKLoadOrCreateReturnsSameKeyIDOnReload(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credential.kek")

	first, err := robot.LoadOrCreateKEK(path)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := robot.LoadOrCreateKEK(path)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.KeyID() != second.KeyID() {
		t.Errorf("KeyID mismatch: first=%q second=%q", first.KeyID(), second.KeyID())
	}
}

func TestKEKKeyIDFormat(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credential.kek")
	kek, err := robot.LoadOrCreateKEK(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKEK: %v", err)
	}
	id := kek.KeyID()
	// "kek_" 접두사 + 8 hex chars
	if len(id) != 12 {
		t.Errorf("KeyID length = %d, want 12 (kek_<8 hex>): got %q", len(id), id)
	}
	if id[:4] != "kek_" {
		t.Errorf("KeyID prefix = %q, want kek_", id[:4])
	}
}

func TestKEKLoadRejectsInvalidLength(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credential.kek")

	// 잘못된 길이의 파일 작성 (16 bytes — AES-128).
	if err := os.WriteFile(path, make([]byte, 16), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := robot.LoadOrCreateKEK(path)
	if !errors.Is(err, robot.ErrKEKInvalidLength) {
		t.Errorf("err = %v, want ErrKEKInvalidLength", err)
	}
}

func TestKEKLoadRejectsLooseFilePerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACL — Phase 2+에 별도 검증")
	}
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credential.kek")

	if err := os.WriteFile(path, make([]byte, robot.KEKSizeBytes), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := robot.LoadOrCreateKEK(path)
	if !errors.Is(err, robot.ErrKEKFilePermissions) {
		t.Errorf("err = %v, want ErrKEKFilePermissions", err)
	}
}

func TestKEKEmptyPathRejected(t *testing.T) {
	t.Parallel()
	_, err := robot.LoadOrCreateKEK("")
	if err == nil {
		t.Error("err = nil, want non-nil")
	}
}
