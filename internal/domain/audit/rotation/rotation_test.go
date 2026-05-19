package rotation_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	"github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

const testTenant storage.TenantID = "tn_rot"

// --- policy ---

func TestDefaultPolicy(t *testing.T) {
	t.Parallel()

	p := rotation.DefaultPolicy()
	if p.Frequency != rotation.DefaultFrequency {
		t.Errorf("Frequency = %s, want %s", p.Frequency, rotation.DefaultFrequency)
	}
	if p.HotRetention != rotation.DefaultHotRetention {
		t.Errorf("HotRetention = %s, want %s", p.HotRetention, rotation.DefaultHotRetention)
	}
	if p.ColdBackend != rotation.ColdBackendFile {
		t.Errorf("ColdBackend = %q, want %q", p.ColdBackend, rotation.ColdBackendFile)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate(default): %v", err)
	}
}

func TestLoadPolicyFromEnv_Overrides(t *testing.T) {
	// 본 테스트는 env mutate → 직렬화 필요 (parallel 금지).
	t.Setenv(rotation.EnvFrequency, "weekly")
	t.Setenv(rotation.EnvHotRetentionDay, "90")
	t.Setenv(rotation.EnvColdBackend, "s3")

	p, err := rotation.LoadPolicyFromEnv()
	if err != nil {
		t.Fatalf("LoadPolicyFromEnv: %v", err)
	}
	if p.Frequency != 7*24*time.Hour {
		t.Errorf("Frequency = %s, want 168h (weekly)", p.Frequency)
	}
	if p.HotRetention != 90*24*time.Hour {
		t.Errorf("HotRetention = %s, want 2160h (90 days)", p.HotRetention)
	}
	if p.ColdBackend != rotation.ColdBackendS3 {
		t.Errorf("ColdBackend = %q, want %q", p.ColdBackend, rotation.ColdBackendS3)
	}
}

func TestLoadPolicyFromEnv_InvalidValues(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"bad frequency", map[string]string{rotation.EnvFrequency: "notaduration"}, "FREQUENCY"},
		{"zero retention", map[string]string{rotation.EnvHotRetentionDay: "0"}, "HOT_RETENTION"},
		{"unknown backend", map[string]string{rotation.EnvColdBackend: "azure"}, "COLD_BACKEND"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			_, err := rotation.LoadPolicyFromEnv()
			if err == nil {
				t.Fatalf("expected error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

// --- file backend ---

func TestFileBackend_PutGetExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	be, err := rotation.NewFileBackend(root)
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}
	ctx := context.Background()

	uri, err := be.Put(ctx, "tn_x/seg-000001.tar.gz", []byte("hello"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !strings.HasPrefix(uri, "file://") {
		t.Errorf("uri %q does not start with file://", uri)
	}

	exists, err := be.Exists(ctx, uri)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("Exists returned false after Put")
	}

	got, err := be.Get(ctx, uri)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("Get = %q, want hello", got)
	}
}

func TestFileBackend_NotExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	be, err := rotation.NewFileBackend(root)
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}

	// 존재 안 함.
	exists, err := be.Exists(context.Background(),
		"file:///"+filepath.ToSlash(filepath.Join(root, "missing.tar.gz")))
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("Exists returned true for missing file")
	}

	// Get → ErrNotExist.
	_, err = be.Get(context.Background(),
		"file:///"+filepath.ToSlash(filepath.Join(root, "missing.tar.gz")))
	if !errors.Is(err, rotation.ErrNotExist) {
		t.Errorf("Get error = %v, want ErrNotExist", err)
	}
}

func TestFileBackend_RejectsAbsoluteKey(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	be, _ := rotation.NewFileBackend(root)

	_, err := be.Put(context.Background(), "/abs/escape.tar.gz", []byte("x"))
	if err == nil {
		t.Error("expected error for absolute key")
	}

	_, err = be.Put(context.Background(), "../escape.tar.gz", []byte("x"))
	if err == nil {
		t.Error("expected error for escape key")
	}
}

// --- S3 backend stub (core build) ---

func TestS3Backend_StubReturnsNotAvailable(t *testing.T) {
	t.Parallel()

	_, err := rotation.NewS3Backend(rotation.S3Config{Region: "us-east-1", Bucket: "x"})
	if !errors.Is(err, rotation.ErrS3BackendNotAvailable) {
		t.Errorf("NewS3Backend error = %v, want ErrS3BackendNotAvailable", err)
	}
}

// --- segment builder + archiver ---

func TestComputeSegmentHash_Deterministic(t *testing.T) {
	t.Parallel()

	e1 := audit.Entry{Hash: audit.Hash{1, 2, 3}}
	e2 := audit.Entry{Hash: audit.Hash{4, 5, 6}}

	a := rotation.ComputeSegmentHash([]audit.Entry{e1, e2})
	b := rotation.ComputeSegmentHash([]audit.Entry{e1, e2})
	if a != b {
		t.Error("ComputeSegmentHash not deterministic")
	}

	// 다른 순서 → 다른 hash.
	c := rotation.ComputeSegmentHash([]audit.Entry{e2, e1})
	if a == c {
		t.Error("ComputeSegmentHash should depend on order")
	}
}

func TestBuildSegment_ReadsRange(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	// 5 entry seed.
	seedEntries(t, store, repo, 5)

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var seg *rotation.Segment
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		s, err := rotation.BuildSegment(ctx, tx, testTenant, 1, 5)
		if err != nil {
			return err
		}
		seg = s
		return nil
	})
	if err != nil {
		t.Fatalf("Tx/BuildSegment: %v", err)
	}

	if seg.EntryCount != 5 {
		t.Errorf("EntryCount = %d, want 5", seg.EntryCount)
	}
	if seg.FirstEntryID != 1 || seg.LastEntryID != 5 {
		t.Errorf("range = [%d, %d], want [1, 5]", seg.FirstEntryID, seg.LastEntryID)
	}
	if len(seg.Entries) != 5 {
		t.Errorf("len(Entries) = %d, want 5", len(seg.Entries))
	}
	// hash 일관성: BuildSegment가 같은 entries로 같은 hash를 만드는지.
	got := rotation.ComputeSegmentHash(seg.Entries)
	if got != seg.Hash {
		t.Errorf("Segment.Hash mismatch with ComputeSegmentHash(Entries)")
	}
}

func TestBuildSegment_EmptyRange(t *testing.T) {
	t.Parallel()

	store, _ := newTestStorage(t)
	ctx := storage.WithTenantID(context.Background(), testTenant)

	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rotation.BuildSegment(ctx, tx, testTenant, 1, 5)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "no entries") {
		t.Errorf("expected 'no entries' error, got %v", err)
	}
}

func TestArchive_TarGzManifestSegmentHash(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 3)

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var seg *rotation.Segment
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		s, err := rotation.BuildSegment(ctx, tx, testTenant, 1, 3)
		if err != nil {
			return err
		}
		seg = s
		return nil
	}); err != nil {
		t.Fatalf("BuildSegment: %v", err)
	}

	root := t.TempDir()
	be, _ := rotation.NewFileBackend(root)
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)

	uri, sum, err := rotation.Archive(ctx, seg, be, "tn_rot/seg-1.tar.gz", now)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if len(sum) != 32 {
		t.Errorf("sha256 len = %d, want 32", len(sum))
	}

	body, err := be.Get(ctx, uri)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// archive sha256 = Get 본문 sha256와 일치.
	actual := sha256.Sum256(body)
	if !bytes.Equal(actual[:], sum) {
		t.Errorf("archive sha256 mismatch")
	}

	// tar.gz 내 manifest.json 검증.
	manifest, entriesNDJSON, err := readArchive(body)
	if err != nil {
		t.Fatalf("readArchive: %v", err)
	}
	if manifest["version"] != "1" {
		t.Errorf("manifest.version = %v, want \"1\"", manifest["version"])
	}
	wantHash := hex.EncodeToString(seg.Hash[:])
	if manifest["segmentHash"] != wantHash {
		t.Errorf("manifest.segmentHash = %v, want %s", manifest["segmentHash"], wantHash)
	}
	// entries.ndjson 라인 3개.
	lines := bytes.Split(bytes.TrimRight(entriesNDJSON, "\n"), []byte{'\n'})
	if len(lines) != 3 {
		t.Errorf("entries.ndjson lines = %d, want 3", len(lines))
	}
}

// --- Rotate (integration: archive + DB INSERT + audit.rotate.complete entry) ---

func TestRotate_EmitsCompleteEntryAndPersistsSegment(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 3)

	be, _ := rotation.NewFileBackend(t.TempDir())
	clk := clock.System()
	rot, err := rotation.New(rotation.Deps{
		Clock:    clk,
		Backend:  be,
		Appender: repo,
	})
	if err != nil {
		t.Fatalf("rotation.New: %v", err)
	}

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var rec *rotation.SegmentRecord
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		// 첫 rotation → segmentNumber = 1.
		latest, err := rotation.LatestSegmentNumber(ctx, tx, testTenant)
		if err != nil {
			return err
		}
		if latest != 0 {
			return errors.New("expected LatestSegmentNumber=0 before first rotation")
		}
		r, err := rot.Rotate(ctx, tx, testTenant, 1, 1, 3)
		if err != nil {
			return err
		}
		rec = r
		return nil
	}); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if rec.ID <= 0 {
		t.Errorf("rec.ID = %d, want > 0", rec.ID)
	}
	if rec.EntryCount != 3 {
		t.Errorf("EntryCount = %d, want 3", rec.EntryCount)
	}
	if !strings.HasPrefix(rec.ArchiveURI, "file://") {
		t.Errorf("ArchiveURI prefix mismatch: %s", rec.ArchiveURI)
	}
	if len(rec.ArchiveSHA256) != 32 {
		t.Errorf("ArchiveSHA256 len = %d, want 32", len(rec.ArchiveSHA256))
	}

	// rotation.complete entry가 chain의 head로 link됐는지.
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		head, err := repo.Head(ctx, tx, testTenant)
		if err != nil {
			return err
		}
		// 원래 3 entry + rotate.complete → head.Seq = 4.
		if head.Seq != 4 {
			t.Errorf("head.Seq = %d, want 4", head.Seq)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify head: %v", err)
	}

	// GetSegment 일관성.
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		got, err := rotation.GetSegment(ctx, tx, testTenant, 1)
		if err != nil {
			return err
		}
		if got.SegmentHash != rec.SegmentHash {
			t.Errorf("GetSegment SegmentHash mismatch")
		}
		if got.ArchiveURI != rec.ArchiveURI {
			t.Errorf("GetSegment ArchiveURI = %q, want %q", got.ArchiveURI, rec.ArchiveURI)
		}
		return nil
	}); err != nil {
		t.Fatalf("GetSegment: %v", err)
	}
}

func TestRotate_LatestSegmentNumber_Increment(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 4)

	be, _ := rotation.NewFileBackend(t.TempDir())
	rot, _ := rotation.New(rotation.Deps{Clock: clock.System(), Backend: be, Appender: repo})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	// 1 회차: seq 1~2 rotation.
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rot.Rotate(ctx, tx, testTenant, 1, 1, 2)
		return err
	}); err != nil {
		t.Fatalf("first rotate: %v", err)
	}
	// 2 회차: seq 3~4 rotation (직전 rotate.complete entry는 seq 3, 즉 5번째 entry 됨 — 본 round
	// 단순화: 명시 seq 만 archive 대상으로 함. 실제 적용 시 직전 segment.last_entry_id + 1 자동).
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		latest, err := rotation.LatestSegmentNumber(ctx, tx, testTenant)
		if err != nil {
			return err
		}
		if latest != 1 {
			t.Errorf("LatestSegmentNumber = %d, want 1", latest)
		}
		_, err = rot.Rotate(ctx, tx, testTenant, 2, 3, 4)
		return err
	}); err != nil {
		t.Fatalf("second rotate: %v", err)
	}

	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		latest, err := rotation.LatestSegmentNumber(ctx, tx, testTenant)
		if err != nil {
			return err
		}
		if latest != 2 {
			t.Errorf("LatestSegmentNumber = %d, want 2", latest)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify latest: %v", err)
	}
}

func TestRotate_RejectsDuplicateSegmentNumber(t *testing.T) {
	t.Parallel()

	store, repo := newTestStorage(t)
	seedEntries(t, store, repo, 2)

	be, _ := rotation.NewFileBackend(t.TempDir())
	rot, _ := rotation.New(rotation.Deps{Clock: clock.System(), Backend: be, Appender: repo})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rot.Rotate(ctx, tx, testTenant, 1, 1, 2)
		return err
	}); err != nil {
		t.Fatalf("first rotate: %v", err)
	}

	// 동일 segmentNumber 재시도 → UNIQUE 위반.
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := rot.Rotate(ctx, tx, testTenant, 1, 1, 2)
		return err
	})
	if err == nil {
		t.Error("expected error for duplicate segment_number")
	}
}

// --- helpers ---

func newTestStorage(t *testing.T) (storage.Storage, *sqliterepo.Repo) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "rotation.db")
	s, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System()})
	return s, repo
}

func seedEntries(t *testing.T, s storage.Storage, repo *sqliterepo.Repo, n int) {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), testTenant)
	for i := 0; i < n; i++ {
		i := i
		err := s.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: testTenant,
				Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
				Action:   "test.event",
				Target:   audit.Target{Type: "robot", ID: "ro_test"},
				Payload:  []byte(`{"n":` + itoa(i) + `}`),
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		})
		if err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func readArchive(body []byte) (map[string]any, []byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	var manifest map[string]any
	var entries []byte
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, err
		}
		switch hdr.Name {
		case "manifest.json":
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, nil, err
			}
		case "entries.ndjson":
			entries = data
		}
	}
	if manifest == nil {
		return nil, nil, errors.New("manifest.json not found in tar")
	}
	return manifest, entries, nil
}
