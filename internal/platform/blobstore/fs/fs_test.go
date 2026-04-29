package fs_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/blobstore"
	"github.com/ssabro/rosshield/internal/platform/blobstore/fs"
)

func newStore(t *testing.T) (blobstore.Store, string) {
	t.Helper()
	root := t.TempDir()
	s, err := fs.New(root)
	if err != nil {
		t.Fatalf("fs.New: %v", err)
	}
	return s, root
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestNewCreatesRootIfMissing(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "nested", "blobs")
	s, err := fs.New(root)
	if err != nil {
		t.Fatalf("fs.New: %v", err)
	}
	if s == nil {
		t.Fatal("store is nil")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root not created: %v", err)
	}
	for _, sub := range []string{".staging", ".quarantine"} {
		if _, err := os.Stat(filepath.Join(root, sub)); err != nil {
			t.Fatalf("%s not created: %v", sub, err)
		}
	}
}

func TestPutThenGetReturnsSameBytes(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	in := []byte("hello blob world")
	sha, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	out, err := s.Get(ctx, sha)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(out) != string(in) {
		t.Fatalf("mismatch: got %q want %q", string(out), string(in))
	}
}

func TestPutAssignsCorrectSHA256(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	in := []byte("audit chain payload")
	sha, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	want := sha256Hex(in)
	if sha != want {
		t.Fatalf("sha mismatch: got %s want %s", sha, want)
	}
	if len(sha) != 64 {
		t.Fatalf("sha length not 64: %d", len(sha))
	}
}

func TestPutDeduplicatesIdempotent(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	in := []byte("dedup payload")
	sha1, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	sha2, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	if sha1 != sha2 {
		t.Fatalf("sha differs across idempotent puts: %s vs %s", sha1, sha2)
	}
	// 파일이 정확히 한 개인지 확인 (shard 경로 기준).
	shard := filepath.Join(root, sha1[0:2], sha1[2:4])
	entries, err := os.ReadDir(shard)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 file in shard, got %d", len(entries))
	}
}

func TestGetReturnsErrNotFoundForUnknownSHA(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	missing := sha256Hex([]byte("never stored"))
	_, err := s.Get(ctx, missing)
	if !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestGetVerifiesHashAndReturnsErrCorruptedOnMismatch(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	in := []byte("trust but verify")
	sha, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	// 파일 직접 변조.
	target := filepath.Join(root, sha[0:2], sha[2:4], sha+".blob")
	if err := os.WriteFile(target, []byte("corrupted bytes !!"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = s.Get(ctx, sha)
	if !errors.Is(err, blobstore.ErrCorrupted) {
		t.Fatalf("want ErrCorrupted, got %v", err)
	}
	// 원본은 quarantine으로 이동되어야 함 → target은 사라짐.
	if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("corrupted file still at original path: %v", statErr)
	}
	// quarantine dir에 적어도 하나의 파일이 있어야 함.
	qDir := filepath.Join(root, ".quarantine")
	entries, err := os.ReadDir(qDir)
	if err != nil {
		t.Fatalf("ReadDir quarantine: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("quarantine dir empty")
	}
}

func TestVerifyDetectsCorruption(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	in := []byte("verify me")
	sha, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Verify(ctx, sha); err != nil {
		t.Fatalf("Verify ok case: %v", err)
	}
	target := filepath.Join(root, sha[0:2], sha[2:4], sha+".blob")
	if err := os.WriteFile(target, []byte("xxxxxxxxx"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := s.Verify(ctx, sha); !errors.Is(err, blobstore.ErrCorrupted) {
		t.Fatalf("want ErrCorrupted, got %v", err)
	}
}

func TestVerifyReturnsNotFoundForMissing(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	missing := sha256Hex([]byte("ghost"))
	if err := s.Verify(ctx, missing); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestExistsReturnsTrueForStored(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	in := []byte("exists me")
	sha, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	ok, err := s.Exists(ctx, sha)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Fatal("Exists returned false for stored blob")
	}
	missing := sha256Hex([]byte("not stored"))
	ok, err = s.Exists(ctx, missing)
	if err != nil {
		t.Fatalf("Exists missing: %v", err)
	}
	if ok {
		t.Fatal("Exists returned true for missing blob")
	}
}

func TestOpenStreamsLargeBlob(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	buf := make([]byte, 1<<20) // 1 MiB
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	sha, err := s.Put(ctx, buf)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, err := s.Open(ctx, sha)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close() //nolint:errcheck
	chunk := make([]byte, 8192)
	total := 0
	hh := sha256.New()
	for {
		n, err := rc.Read(chunk)
		if n > 0 {
			hh.Write(chunk[:n])
			total += n
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if total != len(buf) {
		t.Fatalf("read total %d want %d", total, len(buf))
	}
	if hex.EncodeToString(hh.Sum(nil)) != sha {
		t.Fatal("streamed bytes differ from original sha")
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close clean: %v", err)
	}
}

func TestOpenCloseDetectsCorruption(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	in := []byte("close detects corruption")
	sha, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := filepath.Join(root, sha[0:2], sha[2:4], sha+".blob")
	if err := os.WriteFile(target, []byte("tampered tampered tampered"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	rc, err := s.Open(ctx, sha)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := io.Copy(io.Discard, rc); err != nil {
		t.Fatalf("Read: %v", err)
	}
	closeErr := rc.Close()
	if !errors.Is(closeErr, blobstore.ErrCorrupted) {
		t.Fatalf("want Close to return ErrCorrupted, got %v", closeErr)
	}
}

func TestRejectsInvalidSHAFormat(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	cases := map[string]string{
		"too short":      strings.Repeat("a", 63),
		"too long":       strings.Repeat("a", 65),
		"uppercase":      strings.Repeat("A", 64),
		"non-hex":        strings.Repeat("g", 64),
		"empty":          "",
		"path traversal": "../../../etc/passwd0000000000000000000000000000000000000000000000000",
	}
	for name, sha := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Get(ctx, sha); !errors.Is(err, blobstore.ErrInvalidSHA) {
				t.Fatalf("Get: want ErrInvalidSHA, got %v", err)
			}
			if err := s.Verify(ctx, sha); !errors.Is(err, blobstore.ErrInvalidSHA) {
				t.Fatalf("Verify: want ErrInvalidSHA, got %v", err)
			}
			if _, err := s.Exists(ctx, sha); !errors.Is(err, blobstore.ErrInvalidSHA) {
				t.Fatalf("Exists: want ErrInvalidSHA, got %v", err)
			}
			if _, err := s.Open(ctx, sha); !errors.Is(err, blobstore.ErrInvalidSHA) {
				t.Fatalf("Open: want ErrInvalidSHA, got %v", err)
			}
		})
	}
}

func TestPutEmptyBytesReturnsSHAOfEmpty(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	sha, err := s.Put(ctx, []byte{})
	if err != nil {
		t.Fatalf("Put empty: %v", err)
	}
	const wantEmptySHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if sha != wantEmptySHA {
		t.Fatalf("sha mismatch for empty: got %s want %s", sha, wantEmptySHA)
	}
	out, err := s.Get(ctx, sha)
	if err != nil {
		t.Fatalf("Get empty: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("want empty bytes, got %d", len(out))
	}
}

func TestPutConcurrentSameSHAOK(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	in := []byte("concurrent same payload")
	const n = 10
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sha, err := s.Put(ctx, in)
			results[idx] = sha
			errs[idx] = err
		}(i)
	}
	wg.Wait()
	want := sha256Hex(in)
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if results[i] != want {
			t.Fatalf("worker %d sha %s != %s", i, results[i], want)
		}
	}
	shard := filepath.Join(root, want[0:2], want[2:4])
	entries, err := os.ReadDir(shard)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 file, got %d", len(entries))
	}
}

func TestPutConcurrentDifferentSHAOK(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	const n = 10
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			payload := []byte("payload-" + string(rune('A'+idx)))
			sha, err := s.Put(ctx, payload)
			results[idx] = sha
			errs[idx] = err
		}(i)
	}
	wg.Wait()
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		seen[results[i]] = struct{}{}
		ok, err := s.Exists(ctx, results[i])
		if err != nil {
			t.Fatalf("Exists worker %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("worker %d blob missing", i)
		}
	}
	if len(seen) != n {
		t.Fatalf("want %d distinct sha, got %d", n, len(seen))
	}
}

func TestQuarantinePreservesOriginalForensics(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	in := []byte("forensic original")
	sha, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	target := filepath.Join(root, sha[0:2], sha[2:4], sha+".blob")
	tampered := []byte("tampered evidence preserved as-is for forensic")
	if err := os.WriteFile(target, tampered, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := s.Get(ctx, sha); !errors.Is(err, blobstore.ErrCorrupted) {
		t.Fatalf("want ErrCorrupted, got %v", err)
	}
	qDir := filepath.Join(root, ".quarantine")
	entries, err := os.ReadDir(qDir)
	if err != nil {
		t.Fatalf("ReadDir quarantine: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("quarantine empty")
	}
	// 격리된 파일은 변조된 bytes 그대로 보존(자동 삭제 X).
	body, err := os.ReadFile(filepath.Join(qDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile quarantine: %v", err)
	}
	if string(body) != string(tampered) {
		t.Fatalf("quarantine body mismatch: got %q want %q", string(body), string(tampered))
	}
	// 파일명에 sha가 포함되어야 한다 (forensic trace).
	if !strings.Contains(entries[0].Name(), sha) {
		t.Fatalf("quarantine name should contain sha %s, got %s", sha, entries[0].Name())
	}
}

func TestShardLayoutMatchesExpected(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	in := []byte("shard layout check")
	sha, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	expected := filepath.Join(root, sha[0:2], sha[2:4], sha+".blob")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected shard path missing: %v (path=%s)", err, expected)
	}
}

func TestStagingDirAlwaysCleanedAfterPut(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := s.Put(ctx, []byte("payload-"+string(rune('a'+i)))); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	stagingDir := filepath.Join(root, ".staging")
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		t.Fatalf("ReadDir staging: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("staging dir not clean: %v", names)
	}
}

func TestDeleteUnsupportedInPhase1(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	in := []byte("delete me later")
	sha, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	err = s.Delete(ctx, sha)
	if !errors.Is(err, blobstore.ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func TestNewRejectsExcessivelyLongRoot(t *testing.T) {
	// Windows long path 함정 방어 — root가 240자를 넘으면 New가 거부.
	parent := t.TempDir()
	long := strings.Repeat("x", 250)
	root := filepath.Join(parent, long)
	if _, err := fs.New(root); err == nil {
		t.Fatal("want error for excessively long root, got nil")
	}
}
