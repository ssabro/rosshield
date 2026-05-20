package main

// rotation_test.go — Stage 5: `rosshield-audit-verify rotation` 서브커맨드 검증.
//
// fixture는 실 rotation.Rotator로 만듭니다 — 외부 감사인이 받게 될 archive와
// byte-identical 한 본문을 검증.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	"github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// rotationFixture는 N개의 연속 segment를 만들어 file backend root + segment records를 반환합니다.
type rotationFixture struct {
	backendRoot string
	tenantID    storage.TenantID
	segments    []*rotation.SegmentRecord
}

func buildRotationFixture(t *testing.T, segCount int, entriesPerSeg int) rotationFixture {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "rot.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System()})

	backendRoot := t.TempDir()
	be, err := rotation.NewFileBackend(backendRoot)
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}
	rot, err := rotation.New(rotation.Deps{Clock: clock.System(), Backend: be, Appender: repo})
	if err != nil {
		t.Fatalf("rotation.New: %v", err)
	}

	const tenantID storage.TenantID = "tn_verify"
	ctx := storage.WithTenantID(context.Background(), tenantID)

	// seed entries — segCount * entriesPerSeg.
	totalEntries := segCount * entriesPerSeg
	for i := 0; i < totalEntries; i++ {
		i := i
		err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: tenantID,
				Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
				Action:   "fixture.event",
				Target:   audit.Target{Type: "x", ID: fmt.Sprintf("%d", i)},
				Payload:  []byte(fmt.Sprintf(`{"n":%d}`, i)),
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		})
		if err != nil {
			t.Fatalf("seed entry %d: %v", i, err)
		}
	}

	// rotate segments. 매 rotation 후 audit.rotate.complete entry가 chain head로 link되지만,
	// 신규 rotation은 명시 seq 만 archive — rotate.complete entry는 별 segment에 포함되지 않음.
	// fromSeq/toSeq를 명시적으로 계산: 각 rotation은 entriesPerSeg 만큼.
	// 그러나 rotate.complete entry가 매번 추가되므로 seq 점프가 발생함.
	// 단순화: 각 segment의 fromSeq = (i-1)*entriesPerSeg + (i-1) + 1, toSeq = fromSeq + entriesPerSeg - 1.
	// (직전 rotate.complete entry는 i-1개 추가됨.)
	var segs []*rotation.SegmentRecord
	for i := 1; i <= segCount; i++ {
		// 직전 rotation.complete entry 수 = (i - 1).
		fromSeq := int64((i-1)*entriesPerSeg + (i - 1) + 1)
		toSeq := fromSeq + int64(entriesPerSeg) - 1
		i := i
		err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			r, err := rot.Rotate(ctx, tx, tenantID, int64(i), fromSeq, toSeq)
			if err != nil {
				return err
			}
			segs = append(segs, r)
			return nil
		})
		if err != nil {
			t.Fatalf("rotate seg=%d: %v", i, err)
		}
	}
	return rotationFixture{
		backendRoot: backendRoot,
		tenantID:    tenantID,
		segments:    segs,
	}
}

// segmentArchivePath는 fixture에서 segment N의 file path를 반환합니다.
func segmentArchivePath(fix rotationFixture, segNum int64) string {
	return filepath.Join(fix.backendRoot, string(fix.tenantID),
		fmt.Sprintf("seg-%06d.tar.gz", segNum))
}

// pathToFileURI는 OS path → file:// URI (Windows drive letter 호환).
func pathToFileURI(p string) string {
	s := filepath.ToSlash(p)
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	// query/path escape는 안 함 — 테스트 path는 ASCII 가정.
	return "file://" + s
}

// === single archive verify ===

// T1 — 정상 archive verify (seg 1, prev 없음) → PASS, exit 0.
func TestVerifyRotation_GoldenSegmentPASS(t *testing.T) {
	fix := buildRotationFixture(t, 1, 3)
	uri := pathToFileURI(segmentArchivePath(fix, 1))

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", uri,
		}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(stdout, "PASS") {
		t.Fatalf("missing PASS: %q", stdout)
	}
}

// T2 — expected sha256 일치 + segment_hash 일치 → PASS.
func TestVerifyRotation_ExpectedHashesMatchPASS(t *testing.T) {
	fix := buildRotationFixture(t, 1, 3)
	uri := pathToFileURI(segmentArchivePath(fix, 1))
	rec := fix.segments[0]

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", uri,
			"--expected-sha256", hex.EncodeToString(rec.ArchiveSHA256),
			"--expected-segment-hash", hex.EncodeToString(rec.SegmentHash[:]),
		}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(stdout, "PASS") {
		t.Fatalf("missing PASS: %q", stdout)
	}
}

// T3 — 변조 archive (bytes 1 byte 변경) → archive sha256 mismatch → FAIL, exit 1.
func TestVerifyRotation_TamperedArchiveFAIL(t *testing.T) {
	fix := buildRotationFixture(t, 1, 3)
	path := segmentArchivePath(fix, 1)

	// 본문 1 byte 변조.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	tampered := bytes.Clone(body)
	tampered[len(tampered)-5] ^= 0xff
	tamperedPath := filepath.Join(t.TempDir(), "tampered.tar.gz")
	if err := os.WriteFile(tamperedPath, tampered, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", pathToFileURI(tamperedPath),
			"--expected-sha256", hex.EncodeToString(fix.segments[0].ArchiveSHA256),
		}); code != 1 {
			t.Errorf("exit=%d, want 1", code)
		}
	})
	if !strings.Contains(stdout, "FAIL") {
		t.Fatalf("missing FAIL: %q", stdout)
	}
}

// T4 — expected_segment_hash mismatch → FAIL.
func TestVerifyRotation_ExpectedSegmentHashMismatchFAIL(t *testing.T) {
	fix := buildRotationFixture(t, 1, 3)
	uri := pathToFileURI(segmentArchivePath(fix, 1))

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", uri,
			"--expected-segment-hash", strings.Repeat("00", 32),
		}); code != 1 {
			t.Errorf("exit=%d, want 1", code)
		}
	})
	if !strings.Contains(stdout, "FAIL") {
		t.Fatalf("missing FAIL: %q", stdout)
	}
	if !strings.Contains(stdout, "segment_hash") {
		t.Errorf("reason missing 'segment_hash': %q", stdout)
	}
}

// T5 — prev_segment_hash mismatch (segment 2의 prev를 잘못 받음) → FAIL.
func TestVerifyRotation_PrevHashMismatchFAIL(t *testing.T) {
	fix := buildRotationFixture(t, 2, 3)
	uri := pathToFileURI(segmentArchivePath(fix, 2))

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", uri,
			"--prev-segment-hash", strings.Repeat("aa", 32),
		}); code != 1 {
			t.Errorf("exit=%d, want 1", code)
		}
	})
	if !strings.Contains(stdout, "FAIL") {
		t.Fatalf("missing FAIL: %q", stdout)
	}
	if !strings.Contains(stdout, "prev_segment_hash") {
		t.Errorf("reason missing 'prev_segment_hash': %q", stdout)
	}
}

// T6 — segment 2의 prev_segment_hash가 segment 1의 segment_hash와 일치 → PASS.
func TestVerifyRotation_PrevHashChainPASS(t *testing.T) {
	fix := buildRotationFixture(t, 2, 3)
	uri := pathToFileURI(segmentArchivePath(fix, 2))
	prevHex := hex.EncodeToString(fix.segments[0].SegmentHash[:])

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", uri,
			"--prev-segment-hash", prevHex,
		}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(stdout, "PASS") {
		t.Fatalf("missing PASS: %q", stdout)
	}
}

// T7 — JSON 출력 유효 + 필수 필드 노출.
func TestVerifyRotation_JSONOutput(t *testing.T) {
	fix := buildRotationFixture(t, 1, 2)
	uri := pathToFileURI(segmentArchivePath(fix, 1))

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", uri,
			"--format", "json",
		}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nraw=%q", err, stdout)
	}
	for _, k := range []string{"ok", "result", "archiveSha256", "segmentHash",
		"entryCount", "manifestVersion", "steps", "archiveSha256Match",
		"segmentHashMatch", "prevChainMatch"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("JSON missing key %q in %q", k, stdout)
		}
	}
	if got, _ := parsed["result"].(string); got != "PASS" {
		t.Errorf("result=%q, want PASS", got)
	}
	if got, _ := parsed["manifestVersion"].(string); got != "2" {
		t.Errorf("manifestVersion=%q, want 2", got)
	}
}

// === chain batch verify ===

// T8 — 3 segment chain batch verify PASS (1~3 순서대로).
func TestVerifyRotation_ChainBatchPASS(t *testing.T) {
	fix := buildRotationFixture(t, 3, 2)
	backend := pathToFileURI(filepath.Join(fix.backendRoot, string(fix.tenantID)))

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation", "chain",
			"--backend", backend,
			"--from-segment", "1",
			"--to-segment", "3",
		}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(stdout, "PASS") {
		t.Fatalf("missing PASS: %q", stdout)
	}
	if !strings.Contains(stdout, "chain of 3 segments") {
		t.Errorf("missing chain summary: %q", stdout)
	}
}

// T9 — chain 중간 segment archive 변조 → chain FAIL with firstFailure.
func TestVerifyRotation_ChainBatchFAILWithFirstFailure(t *testing.T) {
	fix := buildRotationFixture(t, 3, 2)
	backend := pathToFileURI(filepath.Join(fix.backendRoot, string(fix.tenantID)))

	// segment 2를 변조 — chain check가 seg2에서 fail해야 함 (seg1의 hash는 seg2의 manifest와
	// 일치해야 하는데 manifest 자체가 깨짐).
	path := segmentArchivePath(fix, 2)
	body, _ := os.ReadFile(path)
	tampered := bytes.Clone(body)
	tampered[100] ^= 0xff
	_ = os.WriteFile(path, tampered, 0o644)

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation", "chain",
			"--backend", backend,
			"--from-segment", "1",
			"--to-segment", "3",
			"--format", "json",
		}); code != 1 {
			t.Errorf("exit=%d, want 1", code)
		}
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nraw=%q", err, stdout)
	}
	if got, _ := parsed["result"].(string); got != "FAIL" {
		t.Errorf("result=%q, want FAIL", got)
	}
	if ff, _ := parsed["firstFailure"].(float64); int64(ff) != 2 {
		t.Errorf("firstFailure=%v, want 2", parsed["firstFailure"])
	}
}

// === arg validation ===

// T10 — `rotation` without --archive-uri → exit 2.
func TestVerifyRotation_MissingArchiveURIExitsTwo(t *testing.T) {
	captureStdio(t, func() {
		if code := run([]string{"rotation"}); code != 2 {
			t.Errorf("exit=%d, want 2", code)
		}
	})
}

// T11 — `rotation chain` without --backend → exit 2.
func TestVerifyRotation_ChainMissingBackendExitsTwo(t *testing.T) {
	captureStdio(t, func() {
		if code := run([]string{"rotation", "chain"}); code != 2 {
			t.Errorf("exit=%d, want 2", code)
		}
	})
}

// T12 — chain from > to → exit 2.
func TestVerifyRotation_ChainFromGreaterThanToExitsTwo(t *testing.T) {
	captureStdio(t, func() {
		if code := run([]string{"rotation", "chain",
			"--backend", "file:///tmp/xx",
			"--from-segment", "5",
			"--to-segment", "3",
		}); code != 2 {
			t.Errorf("exit=%d, want 2", code)
		}
	})
}

// T13 — unsupported scheme (s3) → fetch error → FAIL.
func TestVerifyRotation_UnsupportedSchemeFAIL(t *testing.T) {
	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", "s3://bucket/key.tar.gz",
		}); code != 1 {
			t.Errorf("exit=%d, want 1", code)
		}
	})
	if !strings.Contains(stdout, "FAIL") {
		t.Fatalf("missing FAIL: %q", stdout)
	}
}

// === unit test for URI helpers ===

func TestSegmentArchiveURI(t *testing.T) {
	cases := []struct {
		root string
		n    int64
		want string
	}{
		{"file:///tmp/audit/tn_x", 5, "file:///tmp/audit/tn_x/seg-000005.tar.gz"},
		{"file:///tmp/audit/tn_x/", 1, "file:///tmp/audit/tn_x/seg-000001.tar.gz"},
		{"file:///C:/data/audit/tn_x", 42, "file:///C:/data/audit/tn_x/seg-000042.tar.gz"},
	}
	for _, c := range cases {
		got, err := segmentArchiveURI(c.root, c.n)
		if err != nil {
			t.Errorf("segmentArchiveURI(%q, %d) error: %v", c.root, c.n, err)
			continue
		}
		if got != c.want {
			t.Errorf("segmentArchiveURI(%q, %d) = %q, want %q", c.root, c.n, got, c.want)
		}
	}
	// scheme error.
	if _, err := segmentArchiveURI("s3://bucket/x", 1); err == nil {
		t.Error("expected scheme error for s3://")
	}
}

// 보조: file:// URI escape이 fileURIToPath 와 안전한지.
func TestFileURIRoundTrip(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.tar.gz")
	_ = os.WriteFile(tmp, []byte("hi"), 0o644)

	uri := pathToFileURI(tmp)
	body, err := fetchArchive(uri)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(body) != "hi" {
		t.Errorf("body=%q", body)
	}
	// url.Parse가 path를 그대로 반환하는지 sanity.
	if _, err := url.Parse(uri); err != nil {
		t.Errorf("url.Parse: %v", err)
	}
}

// === cosign verify 통합 (Phase 8 audit rotation 마무리) ===

// swapCosignRunner는 test 동안 runCosignVerify를 fake로 교체합니다.
//
// 외부 cosign binary 의존을 피하기 위해 function var를 swap하고 Cleanup으로 복원.
// 본 함수의 signature는 cosignRunner 정의와 정확히 일치해야 합니다.
func swapCosignRunner(t *testing.T, fn cosignRunner) {
	t.Helper()
	orig := runCosignVerify
	runCosignVerify = fn
	t.Cleanup(func() { runCosignVerify = orig })
}

// T_Cosign1 — cosign-bundle 비활성(둘 다 빈 값) → cosignVerify step "skipped" + PASS.
func TestVerifyRotation_CosignSkipWhenInactive(t *testing.T) {
	fix := buildRotationFixture(t, 1, 2)
	uri := pathToFileURI(segmentArchivePath(fix, 1))

	called := false
	swapCosignRunner(t, func(_ cosignRunArgs) error {
		called = true
		return nil
	})

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", uri,
		}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if called {
		t.Error("runCosignVerify called when bundle/identity not set")
	}
	if !strings.Contains(stdout, "PASS") {
		t.Fatalf("missing PASS: %q", stdout)
	}
}

// T_Cosign2 — bundle + identity 설정 + runner exit 0 → cosignVerify step PASS + 전체 PASS.
func TestVerifyRotation_CosignVerifyPASS(t *testing.T) {
	fix := buildRotationFixture(t, 1, 2)
	uri := pathToFileURI(segmentArchivePath(fix, 1))
	bundlePath := filepath.Join(t.TempDir(), "seg-000001.cosign.bundle")
	_ = os.WriteFile(bundlePath, []byte("fake-bundle"), 0o644)

	var gotArgs cosignRunArgs
	swapCosignRunner(t, func(a cosignRunArgs) error {
		gotArgs = a
		return nil
	})

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", uri,
			"--cosign-bundle", bundlePath,
			"--cosign-identity", "ci@example.com",
			"--cosign-oidc-issuer", "https://accounts.google.com",
			"--format", "json",
		}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if gotArgs.BundlePath != bundlePath {
		t.Errorf("BundlePath=%q, want %q", gotArgs.BundlePath, bundlePath)
	}
	if gotArgs.Identity != "ci@example.com" {
		t.Errorf("Identity=%q", gotArgs.Identity)
	}
	if gotArgs.OIDCIssuer != "https://accounts.google.com" {
		t.Errorf("OIDCIssuer=%q", gotArgs.OIDCIssuer)
	}
	if len(gotArgs.Archive) == 0 {
		t.Error("Archive bytes empty (stdin)")
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nraw=%q", err, stdout)
	}
	if got, _ := parsed["cosignVerifyMatch"].(bool); !got {
		t.Errorf("cosignVerifyMatch=%v, want true", parsed["cosignVerifyMatch"])
	}
	if got, _ := parsed["result"].(string); got != "PASS" {
		t.Errorf("result=%q, want PASS", got)
	}
}

// T_Cosign3 — runner error → cosignVerify step FAIL + 전체 FAIL + exit 1.
func TestVerifyRotation_CosignVerifyFAIL(t *testing.T) {
	fix := buildRotationFixture(t, 1, 2)
	uri := pathToFileURI(segmentArchivePath(fix, 1))
	bundlePath := filepath.Join(t.TempDir(), "seg-000001.cosign.bundle")
	_ = os.WriteFile(bundlePath, []byte("fake-bundle"), 0o644)

	swapCosignRunner(t, func(_ cosignRunArgs) error {
		return fmt.Errorf("cosign exit 1: certificate identity mismatch")
	})

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", uri,
			"--cosign-bundle", bundlePath,
			"--cosign-identity", "ci@example.com",
		}); code != 1 {
			t.Errorf("exit=%d, want 1", code)
		}
	})
	if !strings.Contains(stdout, "FAIL") {
		t.Fatalf("missing FAIL: %q", stdout)
	}
	if !strings.Contains(stdout, "cosign") {
		t.Errorf("FAIL reason missing 'cosign': %q", stdout)
	}
}

// T_Cosign4 — bundle 파일 부재 + required(default true when identity set) → FAIL.
func TestVerifyRotation_CosignBundleMissingFAIL(t *testing.T) {
	fix := buildRotationFixture(t, 1, 2)
	uri := pathToFileURI(segmentArchivePath(fix, 1))
	missingPath := filepath.Join(t.TempDir(), "does-not-exist.bundle")

	called := false
	swapCosignRunner(t, func(_ cosignRunArgs) error {
		called = true
		return nil
	})

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation",
			"--archive-uri", uri,
			"--cosign-bundle", missingPath,
			"--cosign-identity", "ci@example.com",
		}); code != 1 {
			t.Errorf("exit=%d, want 1", code)
		}
	})
	if called {
		t.Error("runCosignVerify called even though bundle file missing")
	}
	if !strings.Contains(stdout, "FAIL") {
		t.Fatalf("missing FAIL: %q", stdout)
	}
}

// T_Cosign5 — chain mode + cosign-bundle-dir + 각 segment 옆 bundle 자동 검색 PASS.
func TestVerifyRotation_ChainCosignBundleDirPASS(t *testing.T) {
	fix := buildRotationFixture(t, 2, 2)
	backend := pathToFileURI(filepath.Join(fix.backendRoot, string(fix.tenantID)))
	bundleDir := t.TempDir()
	// 각 segment 옆 bundle (seg-NNNNNN.cosign.bundle) 작성.
	for n := 1; n <= 2; n++ {
		p := filepath.Join(bundleDir, fmt.Sprintf("seg-%06d.cosign.bundle", n))
		_ = os.WriteFile(p, []byte(fmt.Sprintf("bundle-%d", n)), 0o644)
	}

	var bundlePaths []string
	swapCosignRunner(t, func(a cosignRunArgs) error {
		bundlePaths = append(bundlePaths, a.BundlePath)
		return nil
	})

	stdout, _ := captureStdio(t, func() {
		if code := run([]string{"rotation", "chain",
			"--backend", backend,
			"--from-segment", "1",
			"--to-segment", "2",
			"--cosign-bundle-dir", bundleDir,
			"--cosign-identity", "ci@example.com",
		}); code != 0 {
			t.Errorf("exit=%d, want 0", code)
		}
	})
	if len(bundlePaths) != 2 {
		t.Errorf("bundle path count=%d, want 2 (got %v)", len(bundlePaths), bundlePaths)
	}
	if !strings.Contains(stdout, "PASS") {
		t.Fatalf("missing PASS: %q", stdout)
	}
}
