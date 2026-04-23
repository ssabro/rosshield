// Package eventbus는 도메인 간 통합의 1순위 경로(P5)인 인프로세스 pub/sub의 공개 표면을 정의합니다.
//
// 인터페이스는 분리 모드(NATS/Redis, Phase 3+) 교체를 염두에 두고 설계됩니다(R2 §10).
// inproc 어댑터는 `internal/platform/eventbus/inproc`에서 구현됩니다.
//
// R2 결정 요약:
//   - R2-1 Outbox: Phase 1은 outbox 없이 tx.Commit() 후 단순 publish
//   - R2-2 Publish 의미: 모든 구독자 channel에 enqueue 완료 = 수용 보장
//   - R2-3 Topic: 2-segment "<domain>.<EventName>" 고정
//   - R2-5 Event 영속: EventBus는 전달만, audit 도메인이 자체 영속
//   - R2-6 Wildcard: Phase 1 exact match만
//   - R2-7 Correlation ID: ctx에 없으면 EventBus가 자동 생성 (cor_<ULID>)
package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Event는 bus를 통과하는 envelope입니다. 불변. publish 이후에는 수정하지 않습니다.
type Event struct {
	ID            string          // "evt_<ULID>". 빈 값이면 Bus가 자동 생성.
	Type          string          // "<domain>.<EventName>" (R2-3). 비어 있으면 Publish 거부.
	Version       int             // type별 스키마 버전. 필드 추가는 동일 버전, 제거·의미 변경은 증가.
	TenantID      string          // P4 멀티테넌시.
	OccurredAt    time.Time       // UTC. 빈 값이면 Bus가 Clock.Now()로 자동 채움.
	Aggregate     AggregateRef    // 도메인 aggregate 참조.
	Payload       json.RawMessage // type별 payload 스키마.
	CausationID   string          // 직전 이벤트 ID (handler ctx에서 자동 전파됨).
	CorrelationID string          // 요청 단위 식별자. 빈 값이면 Bus가 자동 생성 (R2-7).
}

// AggregateRef는 이벤트가 속한 도메인 aggregate를 가리킵니다.
type AggregateRef struct {
	Type string // "ScanSession", "Robot" 등
	ID   string // "ss_...", "ro_..."
}

// Handler는 구독자 로직입니다. 반환 error는 기본 정책상 "경고 로그".
// 재시도·DLQ는 어댑터 구현이 관장합니다 (R2-4).
type Handler func(ctx context.Context, evt Event) error

// Subscription은 구독 제어 핸들입니다. 모든 메서드는 goroutine-safe.
type Subscription interface {
	Topic() string
	Cancel()               // idempotent — 두 번째 호출은 no-op.
	Done() <-chan struct{} // worker가 완전 종료된 후 close.
}

// Bus는 공개 표면입니다. 구현은 어댑터(inproc·NATS·Redis)에 위임.
type Bus interface {
	// Publish는 모든 구독자가 channel에 enqueue까지 완료된 후 nil을 반환합니다 (R2-2 수용 보장).
	// Bus가 closed면 ErrBusClosed.
	Publish(ctx context.Context, evt Event) error

	// Subscribe는 topic의 새 구독을 등록합니다. ctx는 등록용 (handler 호출 ctx와 별개).
	Subscribe(ctx context.Context, topic string, h Handler, opts ...SubscribeOption) Subscription

	// Close는 새 publish를 거부하고 모든 구독을 cancel한 뒤 worker 종료를 대기합니다.
	Close(ctx context.Context) error
}

// OverflowPolicy는 구독자 channel이 가득 찼을 때의 정책입니다 (R2 §4).
type OverflowPolicy int

const (
	// OverflowBlock은 channel에 자리가 날 때까지 publisher를 blocking합니다 (publish timeout 적용).
	OverflowBlock OverflowPolicy = iota

	// OverflowDropOldest는 가장 오래된 이벤트를 버리고 새 이벤트를 push합니다.
	OverflowDropOldest
)

// SubscribeConfig는 SubscribeOption들이 누적된 결과입니다.
type SubscribeConfig struct {
	Buffer         int            // channel 용량. 기본 256.
	Overflow       OverflowPolicy // 가득 찼을 때 정책. 기본 OverflowDropOldest.
	PublishTimeout time.Duration  // 단일 enqueue 최대 대기. 기본 100ms.
}

// SubscribeOption은 함수형 옵션 패턴입니다.
type SubscribeOption func(*SubscribeConfig)

// 기본값 (R2 §4 결정).
const (
	DefaultBuffer         = 256
	DefaultPublishTimeout = 100 * time.Millisecond
)

// DefaultSubscribeConfig는 R2 §4 권장 기본값으로 채워진 Config를 반환합니다.
func DefaultSubscribeConfig() SubscribeConfig {
	return SubscribeConfig{
		Buffer:         DefaultBuffer,
		Overflow:       OverflowDropOldest,
		PublishTimeout: DefaultPublishTimeout,
	}
}

// ApplyOptions는 기본값에 옵션들을 누적 적용한 SubscribeConfig를 반환합니다.
// 어댑터 구현이 호출합니다.
func ApplyOptions(opts []SubscribeOption) SubscribeConfig {
	cfg := DefaultSubscribeConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithBuffer는 구독 channel 용량을 지정합니다.
func WithBuffer(n int) SubscribeOption {
	return func(c *SubscribeConfig) { c.Buffer = n }
}

// WithOverflow는 channel이 가득 찼을 때의 정책을 지정합니다.
func WithOverflow(p OverflowPolicy) SubscribeOption {
	return func(c *SubscribeConfig) { c.Overflow = p }
}

// WithPublishTimeout는 단일 enqueue 최대 대기 시간을 지정합니다.
func WithPublishTimeout(d time.Duration) SubscribeOption {
	return func(c *SubscribeConfig) { c.PublishTimeout = d }
}

// 공통 에러.
var (
	ErrBusClosed = errors.New("eventbus: bus is closed")
	ErrNoType    = errors.New("eventbus: event Type is required")
)
