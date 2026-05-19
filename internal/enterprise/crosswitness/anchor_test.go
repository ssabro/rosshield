//go:build rosshield_enterprise

// anchor_test.go — A-1 외부 anchoring 단위 테스트.
//
// 본 테스트는 phase7-public-transition-design.md §6.2 spec을 검증합니다:
//   - WebhookAnchor: HTTP POST JSON body(hash hex + meta + signedAt RFC3339Nano)
//     + retry 3회 exponential backoff.
//   - FilesystemDumpAnchor: append-only JSONL line 추가 + concurrent 안전.
//   - 두 anchor 모두 결정론 (같은 input → 같은 effect).

package crosswitness

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhookAnchor_정상_POST_성공(t *testing.T) {
	var (
		got       map[string]any
		recvCount int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&recvCount, 1)
		if r.Method != http.MethodPost {
			t.Errorf("HTTP method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("body decode 에러: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	anchor := &WebhookAnchor{
		URL:         server.URL,
		HTTPClient:  server.Client(),
		MaxRetries:  3,
		BackoffBase: 10 * time.Millisecond,
	}

	h := mkHash(0xAB)
	meta := map[string]string{"tenant": "t1", "seq": "42"}
	if err := anchor.Anchor(context.Background(), h, meta); err != nil {
		t.Fatalf("Anchor 에러: %v", err)
	}

	if atomic.LoadInt32(&recvCount) != 1 {
		t.Errorf("server recv count = %d, want 1", recvCount)
	}
	if got["hash"] != hashToHex(h) {
		t.Errorf("hash field = %v, want %s", got["hash"], hashToHex(h))
	}
	if got["signedAt"] == "" || got["signedAt"] == nil {
		t.Errorf("signedAt 필드 누락")
	}
	gotMeta, ok := got["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta 필드 타입 잘못: %T", got["meta"])
	}
	if gotMeta["tenant"] != "t1" {
		t.Errorf("meta.tenant = %v, want t1", gotMeta["tenant"])
	}
}

func TestWebhookAnchor_retry_3회_backoff(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	anchor := &WebhookAnchor{
		URL:         server.URL,
		HTTPClient:  server.Client(),
		MaxRetries:  3,
		BackoffBase: 10 * time.Millisecond,
	}

	start := time.Now()
	err := anchor.Anchor(context.Background(), mkHash(0x01), nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("3회째 성공해야 하는데 에러: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	// exponential backoff: base=10ms, 2회 retry → 10 + 20 = 30ms 이상 경과해야.
	if elapsed < 25*time.Millisecond {
		t.Errorf("elapsed = %v, want ≥ 25ms (backoff 검증)", elapsed)
	}
}

func TestWebhookAnchor_MaxRetries_초과시_에러(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	anchor := &WebhookAnchor{
		URL:         server.URL,
		HTTPClient:  server.Client(),
		MaxRetries:  2,
		BackoffBase: 5 * time.Millisecond,
	}

	err := anchor.Anchor(context.Background(), mkHash(0x01), nil)
	if !errors.Is(err, ErrWebhookAnchorFailed) {
		t.Errorf("err = %v, want ErrWebhookAnchorFailed", err)
	}
	// MaxRetries=2 → 총 3회 시도(initial + 2 retry).
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("attempts = %d, want 3 (initial + 2 retries)", attempts)
	}
}

func TestWebhookAnchor_ctx_cancel_중단(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	anchor := &WebhookAnchor{
		URL:         server.URL,
		HTTPClient:  server.Client(),
		MaxRetries:  5,
		BackoffBase: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := anchor.Anchor(ctx, mkHash(0x01), nil)
	if err == nil {
		t.Errorf("ctx cancel 에러 기대했지만 nil")
	}
}

func TestFilesystemDumpAnchor_JSONL_라인_추가(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anchor.jsonl")
	anchor := &FilesystemDumpAnchor{Path: path}

	h1 := mkHash(0x01)
	h2 := mkHash(0x02)

	if err := anchor.Anchor(context.Background(), h1, map[string]string{"k": "v1"}); err != nil {
		t.Fatalf("1차 Anchor 에러: %v", err)
	}
	if err := anchor.Anchor(context.Background(), h2, map[string]string{"k": "v2"}); err != nil {
		t.Fatalf("2차 Anchor 에러: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("파일 열기: %v", err)
	}
	defer func() { _ = f.Close() }()

	var lines []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("line unmarshal 에러: %v", err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 2 {
		t.Fatalf("lines len = %d, want 2", len(lines))
	}
	if lines[0]["hash"] != hashToHex(h1) {
		t.Errorf("line[0].hash = %v, want %s", lines[0]["hash"], hashToHex(h1))
	}
	if lines[1]["hash"] != hashToHex(h2) {
		t.Errorf("line[1].hash = %v, want %s", lines[1]["hash"], hashToHex(h2))
	}
	if lines[0]["timestamp"] == nil {
		t.Errorf("timestamp 필드 누락")
	}
}

func TestFilesystemDumpAnchor_concurrent_안전(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anchor.jsonl")
	anchor := &FilesystemDumpAnchor{Path: path}

	const N = 20
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			h := mkHash(byte(i))
			_ = anchor.Anchor(context.Background(), h, map[string]string{"i": "x"})
		}(i)
	}
	wg.Wait()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("파일 열기: %v", err)
	}
	defer func() { _ = f.Close() }()

	count := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Errorf("concurrent write가 line 손상: %v (line=%q)", err, sc.Text())
		}
		count++
	}
	if count != N {
		t.Errorf("동시 anchor count = %d, want %d", count, N)
	}
}

func TestFilesystemDumpAnchor_파일_없으면_생성(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "anchor.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	anchor := &FilesystemDumpAnchor{Path: path}
	if err := anchor.Anchor(context.Background(), mkHash(0x01), nil); err != nil {
		t.Fatalf("Anchor 에러: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("파일이 생성되지 않음: %v", err)
	}
}

func TestAnchor_interface_compliance(t *testing.T) {
	// 컴파일 타임 인터페이스 준수 확인 — 변경 시 빌드 깨짐으로 검출.
	var _ Anchor = (*WebhookAnchor)(nil)
	var _ Anchor = (*FilesystemDumpAnchor)(nil)
}
