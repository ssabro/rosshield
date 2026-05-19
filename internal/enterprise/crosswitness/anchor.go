//go:build rosshield_enterprise

// anchor.go — A-1 cross-witness 외부 anchoring (enterprise edition).
//
// 본 파일은 spec-candidate-A-draft.md [0020-3] / phase7-public-transition-design.md §6.2
// "운영자가 모든 테넌트 동시 위조 시 외부 anchoring으로 보강" 요구를 구현합니다:
//
//   - WebhookAnchor: 외부 transparency log·webhook으로 fold-in hash를 HTTP POST.
//     실패 시 exponential backoff로 MaxRetries회 재시도. 외부 시스템이 idempotent하게
//     수신해야 하며, retry는 같은 body를 그대로 재전송.
//   - FilesystemDumpAnchor: append-only JSONL 파일에 (timestamp, hash, meta)를 dump.
//     로컬 secondary storage·외부 sync 대상으로 활용 가능.
//
// 두 anchor 모두 결정론 (같은 input + 같은 success/failure path → 같은 effect).

package crosswitness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// ErrWebhookAnchorFailed는 MaxRetries+1회 시도 모두 실패했을 때 반환됩니다.
var ErrWebhookAnchorFailed = errors.New("crosswitness: webhook anchor failed after retries")

// Anchor는 외부 anchoring 한 단위를 추상화합니다.
//
// 구현체는 다음 계약을 따릅니다:
//   - 동일 (hash, meta) 입력에 대해 retry가 idempotent해야 합니다 (외부 수신자가
//     중복 entry를 거부하지 않는 한).
//   - ctx 취소 시 가능한 빨리 반환합니다.
//   - 멀티 goroutine 호출에 안전해야 합니다 (Scheduler가 callback에서 호출).
type Anchor interface {
	Anchor(ctx context.Context, hash Hash, meta map[string]string) error
}

// anchorPayload는 두 anchor가 공통으로 사용하는 JSON 페이로드 구조입니다.
type anchorPayload struct {
	Hash     string            `json:"hash"`
	SignedAt string            `json:"signedAt"`
	Meta     map[string]string `json:"meta,omitempty"`
}

func newAnchorPayload(h Hash, meta map[string]string, now time.Time) anchorPayload {
	return anchorPayload{
		Hash:     hashToHex(h),
		SignedAt: now.UTC().Format(time.RFC3339Nano),
		Meta:     meta,
	}
}

// WebhookAnchor는 HTTP POST로 외부 webhook에 fold-in hash를 전달합니다.
type WebhookAnchor struct {
	// URL은 POST 대상 endpoint (필수).
	URL string

	// HTTPClient는 호출에 사용할 client. nil이면 http.DefaultClient.
	HTTPClient *http.Client

	// MaxRetries는 5xx/네트워크 실패 시 추가 retry 횟수 (총 시도 = 1 + MaxRetries).
	MaxRetries int

	// BackoffBase는 exponential backoff 1차 대기 시간 (i회차 = BackoffBase * 2^(i-1)).
	BackoffBase time.Duration
}

// Anchor는 hash + meta를 JSON으로 POST합니다.
// 2xx 응답이면 success, 그 외(5xx 또는 transport error)면 exponential backoff
// 후 재시도. MaxRetries 초과 시 ErrWebhookAnchorFailed를 wrap해 반환.
func (a *WebhookAnchor) Anchor(ctx context.Context, h Hash, meta map[string]string) error {
	payload := newAnchorPayload(h, meta, time.Now())
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("crosswitness: webhook marshal: %w", err)
	}

	client := a.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	var lastErr error
	totalAttempts := a.MaxRetries + 1
	for attempt := 0; attempt < totalAttempts; attempt++ {
		if attempt > 0 {
			// exponential backoff: BackoffBase * 2^(attempt-1).
			wait := a.BackoffBase * (1 << (attempt - 1))
			select {
			case <-ctx.Done():
				return fmt.Errorf("crosswitness: webhook ctx cancel during backoff: %w", ctx.Err())
			case <-time.After(wait):
			}
		}

		err := a.doRequest(ctx, client, body)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("%w: %v", ErrWebhookAnchorFailed, lastErr)
}

// doRequest는 단일 HTTP POST를 수행하고 결과를 분류합니다.
func (a *WebhookAnchor) doRequest(ctx context.Context, client *http.Client, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("crosswitness: webhook new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("crosswitness: webhook transport: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("crosswitness: webhook status %d", resp.StatusCode)
}

// FilesystemDumpAnchor는 append-only JSONL 파일에 (timestamp, hash, meta) line을 추가합니다.
//
// 내부 mutex로 동시 쓰기를 serialize하므로 멀티 goroutine 호출에 안전합니다.
// 파일이 없으면 생성, 부모 디렉터리가 없으면 호출자가 미리 만들어야 합니다.
type FilesystemDumpAnchor struct {
	// Path는 JSONL 파일 경로 (필수).
	Path string

	mu sync.Mutex
}

// Anchor는 JSONL line 1개를 append합니다.
//
// line 포맷: {"timestamp": "RFC3339Nano UTC", "hash": "hex", "meta": {...}}
func (a *FilesystemDumpAnchor) Anchor(_ context.Context, h Hash, meta map[string]string) error {
	payload := newAnchorPayload(h, meta, time.Now())

	// 파일 포맷의 일관성을 위해 anchorPayload의 SignedAt을 timestamp 필드명으로 옮긴 line struct를 사용.
	line := struct {
		Timestamp string            `json:"timestamp"`
		Hash      string            `json:"hash"`
		Meta      map[string]string `json:"meta,omitempty"`
	}{
		Timestamp: payload.SignedAt,
		Hash:      payload.Hash,
		Meta:      meta,
	}

	encoded, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("crosswitness: dump marshal: %w", err)
	}
	encoded = append(encoded, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()

	f, err := os.OpenFile(a.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("crosswitness: dump open: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(encoded); err != nil {
		return fmt.Errorf("crosswitness: dump write: %w", err)
	}
	return nil
}
