# E1 EventBus Deep Dive — 인프로세스 pub/sub 설계 노트

> **상태**: Draft (2026-04-23). Phase 1 E1.T6/T7 구현 전 참고용.
> **범위**: `internal/platform/eventbus/` 패키지의 설계 결정. 분리 모드(NATS/Redis) 교체 가능성을 유지하는 인터페이스 경계를 포함.
> **목표 독자**: 본 세션(메인)과 향후 E2~E8 도메인이 EventBus를 "어떻게 쓰는가"를 결정할 때.
> **비목표**: 실제 NATS/Redis 어댑터 구현 (§3.3 분리 모드, Phase 3+).

## 0. 전제 요약

- 모노리스(§3.3)가 Phase 1 유일 토폴로지. 인프로세스 channel 기반 pub/sub로 충분합니다.
- 도메인 서비스는 다른 도메인의 저장소를 직접 호출하지 않습니다(P5). **EventBus는 도메인 간 통합의 1순위 경로**입니다(§3.6, §3.1).
- 이벤트·Evidence·Audit은 append-only(P9). EventBus도 이 불변성에 정합해야 합니다.
- 감사 도메인(E2)은 **모든 WRITE의 감사 엔트리를 append**할 책임이 있습니다(§10.2).

---

## 1. In-process 아키텍처

**후보**:

| 방식 | 설명 | 장 | 단 |
|---|---|---|---|
| A. channel-per-topic | topic별로 하나의 `chan Event`. 구독자들은 같은 channel에서 수신 | 구현 단순, 메모리 적음 | 구독자 다중 시 fan-out 불가(채널은 단일 소비자) |
| B. channel-per-subscriber (fan-out) | Publish 시 bus가 각 구독의 개별 channel로 복사 전송 | 구독자 간 독립(백프레셔·속도 격리) | Publish 코스트가 N배 |
| C. copy-on-publish (직접 호출) | Publish 호출 스레드에서 핸들러 목록을 순회하며 바로 실행 | 최단 레이턴시, 디버깅 쉬움 | publisher 스레드 블록·panic 전파 위험 |

**추천**: **B (channel-per-subscriber fan-out)**. 이유:

1. 구독자별 백프레셔·panic 격리가 **자연스럽게 분리**됩니다(§3.10 DLQ 전제).
2. 각 구독자가 "자신만의 goroutine에서 순서대로 소비"한다는 **강한 순서 보장**을 줄 수 있습니다(§10 감사 순서에 유용).
3. 핸들러 실행 시간이 길어도 publisher가 블록되지 않습니다.
4. 테스트에서 "drain까지 대기"를 구현하기 쉽습니다(§9).

C는 테스트 모드용 옵션(아래 §9)으로만 남깁니다. A는 구독자 1개 보장이 불가능한 본 프로젝트 구조에 부적합합니다.

---

## 2. 구독 lifecycle

### Register

```go
sub := bus.Subscribe(ctx, topic, handler, opts...)
// sub.Cancel() 호출 전까지 수신
```

- `Subscribe`는 내부적으로 `subscription{id, topic, ch, handler, done}`을 생성하고 topic의 구독자 목록에 append.
- 구독자 목록은 `sync.RWMutex`로 보호. Publish는 RLock, Subscribe/Cancel은 Lock.
- 반환되는 `Subscription`은 **불변 참조**. 내부 상태는 구조체가 숨깁니다.

### Unregister (Cancel)

- `Subscription.Cancel()`은 **idempotent**. 두 번째 호출은 no-op(`sync.Once` 또는 `atomic.Bool` + CompareAndSwap 사용).
- Cancel 효과:
  1. 구독자 목록에서 제거(Lock 하에).
  2. 전용 channel close → worker goroutine의 `for evt := range ch` 루프 종료.
  3. `Done()` channel close → 호출자가 완전 종료를 대기 가능.

### 핸들러 실행 중 unsubscribe (race 처리)

- Cancel이 list에서 제거하더라도 **이미 worker가 실행 중인 handler 한 건은 끝까지 실행**합니다. 중단 가능한 handler는 `ctx`로 협조해야 합니다(bus가 강제 interrupt하지 않음).
- Publish 도중에 Cancel이 호출되어도 **그 시점에 이미 채널에 push된 이벤트**는 소비됩니다(채널이 closed되기 전까지). Publish는 `select { case ch <- evt: case <-sub.done: }`로 취소된 구독자에게는 더 이상 보내지 않습니다.
- **Dropped count metric**: Cancel 이후 도착한 이벤트는 "sub_cancelled_drops" 카운터로 기록합니다.

---

## 3. Goroutine 모델

**후보**:

| 모델 | 스레드 구조 | 장 | 단 |
|---|---|---|---|
| M1. Publisher 스레드 직접 실행 (sync) | Publish 호출자가 핸들러 직접 실행 | 단순, 순서 명확 | 느린 handler가 publisher 블록, panic이 upstream에 전파 |
| M2. Subscriber당 전용 goroutine | Subscribe마다 1 goroutine 소비 루프 | 격리, 구독자별 순서 보장 | 구독자 많으면 goroutine 수가 선형 증가 (허용 가능 수준: 수십~수백) |
| M3. Worker pool | 공유 worker pool N개, 이벤트 work item 큐 | 자원 상한 분명 | 구독자별 순서 보장을 잃음, 핸들러가 오래 걸리면 head-of-line blocking |

**추천**: **M2 (subscriber당 goroutine)**. 이유:

- 도메인 경계에서 "내 이벤트 순서"가 보존되는 편이 **추론하기 쉽고** 감사·리포트 로직에 안전합니다(§3.6).
- Phase 1 예상 구독자 수는 10~30개 수준(§3.2, 11 도메인). goroutine 수가 문제되지 않습니다.
- M3는 Phase 3 분리 모드에서 NATS consumer로 교체될 때 재검토합니다.

### 트레이드오프 매트릭스

| 항목 | M1 | M2 | M3 |
|---|---|---|---|
| 구현 복잡도 | 낮음 | 중 | 높음 |
| 구독자 순서 보장 | 강함(동기) | 강함(구독자 내) | 약함 |
| 핸들러 지연 대한 내성 | 나쁨 | 좋음 | 중간 |
| panic 격리 | 수동 `recover` 필수 | goroutine별 `recover` | pool worker에서 recover |
| 감사 통합 난이도 | 낮음(같은 트랜잭션 가능) | 중간(분리 트랜잭션) | 중간 |
| 테스트 synchronous drain | 쉬움 | `Drain()` API 필요 | `Drain()` + 풀 배수 |

M1은 **테스트 모드 전용 옵션**으로 제공(§9).

---

## 4. Backpressure

### 설계

- 구독자별 channel은 **bounded**(기본 `cap=256`). 이유: 메모리 폭주 방지, 지연 구독자 탐지.
- Publish의 동작은 **per-subscriber 정책**으로 선택:
  - `Block` (기본): `ctx.Done()`이 닫히기 전까지 `ch <- evt` 대기.
  - `DropNewest`: channel이 가득 차면 이 이벤트를 드롭 + 카운터·로그.
  - `DropOldest`: 가득 차면 가장 오래된 이벤트 한 건을 버리고 새 이벤트를 밀어넣음 (구현: `select`로 시도 후 실패 시 non-blocking receive로 앞을 비우고 다시 push).
- Publish는 구독자 전체에 대해 fan-out하므로, 느린 구독자 1명이 publisher를 잡지 않도록 **timeout 포함 select**를 기본값으로 합니다. timeout 초과 시 설정된 정책을 적용.

### 기본값 & Override

- **기본**: `DropOldest` + 용량 256 + publish timeout 100ms.
- **감사 구독자(Audit Subscriber)는 `Block` + 용량 1024**. 근거: P1/P9 — 감사는 유실 불가, 다른 구독자가 뒤처져도 audit이 먼저 durably commit 되어야 함. Publish 호출자는 audit 수용 한도를 넘지 않도록 배치 처리를 기대.
- Override는 `SubscribeOption`으로: `WithBuffer(n)`, `WithOverflow(policy)`, `WithTimeout(d)`.

### 지연 탐지

- 구독자 channel 깊이가 **용량의 80% 초과 상태로 5초 지속** 시 `bus.slow_subscriber` 메트릭을 증가시키고 경고 로그 (§10.17).

---

## 5. 핸들러 panic 격리

- 구독자 goroutine의 최상단에 `defer func() { if r := recover(); r != nil { ... } }()`.
- recover 결과:
  1. 구조화 로그 `lvl=error comp=eventbus msg="handler panic" topic=... subId=... recovered=...` 기록(§10.11).
  2. `eventbus_handler_panics_total` 카운터 증가(§10.15).
  3. 기본 정책은 **해당 이벤트만 실패 처리 후 루프 계속**. 여러 번 panic하는 구독자는 후속 버전에서 circuit-breaker로 분리 가능(Phase 3).
- Publisher 측은 핸들러 panic을 **관측하지 못합니다** (fan-out이므로). publish는 이미 리턴된 뒤 비동기로 소비되기 때문입니다. 이는 P11(설명 가능성)과 상충하지 않도록, panic은 감사 로그가 아닌 **운영 로그**에만 남깁니다. 감사 실패는 별도 경로(§8 아래)로 처리합니다.

---

## 6. 이벤트 envelope

§3.6의 JSON 구조를 Go struct로 고정합니다.

```go
package eventbus

// Event는 bus를 통과하는 envelope. 불변. publish 이후에는 수정하지 않습니다.
type Event struct {
    ID          string          `json:"id"`           // evt_<ULID>
    Type        string          `json:"type"`         // "scan.ScanCompleted"
    Version     int             `json:"version"`      // 스키마 진화
    TenantID    string          `json:"tenantId"`     // P4
    OccurredAt  time.Time       `json:"occurredAt"`   // UTC
    Aggregate   AggregateRef    `json:"aggregate"`
    Payload     json.RawMessage `json:"payload"`      // type별 스키마
    CausationID string          `json:"causationId,omitempty"`
    CorrelationID string        `json:"correlationId,omitempty"` // §7
}

type AggregateRef struct {
    Type string `json:"type"` // "ScanSession"
    ID   string `json:"id"`   // "ss_..."
}
```

### 설계 노트

- `Payload`는 `json.RawMessage`로 느슨하게 보관. 도메인별 타입은 해당 도메인 `event/` 패키지에서 `Marshal/Unmarshal` 헬퍼로 처리합니다.
- `Version`은 **type 스키마별 정수**. 증가 규칙: 필드 **추가는 non-breaking → 동일 버전 유지**, 제거·의미 변경은 **버전 증가**. 구독자는 `switch version`으로 분기 처리.
- JSON 직렬화는 canonical(키 정렬·escape 고정)을 사용합니다. 이는 audit payloadDigest 계산(§10.3)과 동일한 canonical 규칙이어야 합니다. 공용 `internal/platform/canonicaljson` 패키지를 쓰도록 합의합니다.
- 시간은 `time.Time`을 RFC3339Nano로 직렬화 (§10.11과 일치).
- 이벤트 타입 문자열은 **`<domain>.<EventName>`** 규격. 도메인이 같은 이름 쓰는 것을 방지.

---

## 7. Correlation / causation 전파

- `context.Context`에 `requestId`·`correlationId`·`causationId`를 주입합니다. 키는 private type을 사용해 충돌 방지.
- Publish 호출 시 bus가 자동으로 ctx에서 값을 읽어 envelope에 채웁니다. 호출자가 직접 env을 채우지 않아도 됨.
  - `ctx`에 `causationEventId`가 있으면 `event.CausationID`로 설정.
  - `ctx`에 `correlationId`가 없으면 발행 시점에 새로 생성하여 ctx에 re-inject (그 goroutine 이후 체인에서 유지).
- 구독자 goroutine은 수신한 이벤트의 `CausationID = event.ID`, `CorrelationID = event.CorrelationID`를 **새 ctx**에 심고 handler를 호출합니다. handler 안에서 다시 `Publish`할 때 자동으로 계보가 연결됩니다.
- 로거 미들웨어와 동일한 ctx 키를 공유하므로, handler가 로그를 찍으면 §10.13 correlation 필드가 자동 포함됩니다.

---

## 8. Audit 통합

§10.2 요구사항: **모든 WRITE가 감사 엔트리를 남긴다**. EventBus와 어떻게 엮을지 후보 평가.

### 후보 A — Audit wildcard 구독

> `audit` 도메인이 `*`(또는 prefix) wildcard 구독으로 모든 토픽의 이벤트를 받아 자동 append.

- **장점**: 도메인 서비스는 `bus.Publish`만 하면 감사가 자동. 추가 배선 없음.
- **단점**:
  1. **행위 ≠ 이벤트**. §10.13은 "Audit = 비즈니스 사실". 어떤 WRITE는 이벤트가 아니라 단순 상태 변경일 수 있어 누락 위험.
  2. 이벤트 payload의 **민감 정보(자격증명 등)** 를 그대로 audit.payloadDigest로 쓰면 사고 가능.
  3. 이벤트 publish 실패 시 감사도 함께 실패 — 감사 무결성(P1)이 이벤트 인프라에 결속됨.
  4. P5 경계는 위반하지 않지만, audit이 모든 도메인 이벤트 스키마를 알게 됩니다(반대 방향의 coupling).
- **테스트 난이도**: 중. 통합 테스트에서 wildcard 구독자 drain 필요.

### 후보 B — 서비스가 명시적 `audit.Append()` 호출 + 별도 `bus.Publish()`

> 각 도메인 서비스 메서드가 (1) 도메인 변경, (2) `auditPort.Append(...)`, (3) `bus.Publish(event)`를 같은 트랜잭션에서 수행.

- **장점**:
  1. 감사 엔트리 필드(actor, action, target, outcome)를 **도메인이 의도적으로 선택**합니다. §10.3에 정확히 대응.
  2. 이벤트 실패와 감사 실패가 **분리**됩니다. 감사는 DB 트랜잭션 안쪽, 이벤트는 커밋 후 publish.
  3. P5를 위반하지 않습니다. `auditPort`는 `audit` 도메인이 노출한 **포트 인터페이스**이며, audit 저장소를 직접 호출하지 않습니다.
- **단점**:
  1. 모든 WRITE 경로에 2줄씩 추가 코드 → 누락 위험. 완화: depguard 린트로 "write 메서드에서 `audit.Append` 호출 없음" 경고(정적 분석 한계 있음).
  2. 도메인이 `auditPort`를 생성자 주입 받아야 하므로 배선 증가.
- **테스트 난이도**: 쉬움. 각 도메인 서비스 테스트에서 audit mock 호출 검증.

### 후보 C — L3 Application Service가 orchestration

> 도메인 서비스는 순수 비즈니스 로직만, L3 유스케이스가 `domain.Do` → `audit.Append` → `bus.Publish`를 순서대로 호출.

- **장점**: 도메인이 audit·bus를 몰라도 됨. 가장 깨끗한 경계.
- **단점**:
  1. 모든 WRITE가 L3를 반드시 경유해야 함 — 도메인 내부 이벤트(scan이 Result 저장 후 자체 Insight 트리거, §3.6)가 어색.
  2. L3 서비스 수가 도메인 WRITE 수 × 2~3배로 폭증.
  3. 도메인 이벤트가 저장되기 **전에** L3에서 publish하므로, 소비자가 "이미 저장된 상태"를 조회할 수 없는 레이스.
- **테스트 난이도**: 중~높음. L3 통합 테스트가 비대.

### 추천: **후보 B**

추가 가드레일:

1. **커밋-후-퍼블리시 패턴**: 도메인 서비스는 트랜잭션 안에서 (i) 도메인 write, (ii) `audit.Append`, (iii) **outbox row insert**. 커밋 성공 후에야 bus에 publish. 재시작 시 outbox 재구동으로 at-least-once 보장(§3.10 DLQ 전제와 정합).
   - Phase 1은 outbox를 **메모리 내 리스트**로 간략화하고, Phase 2에서 DB outbox 테이블로 승격(§12 점진).
2. **감사 전용 이벤트 타입 별도 관리**: `audit.Appended` 이벤트는 audit 도메인이 자체 publish. 외부 노드가 감사 스트림만 구독하고 싶을 때 사용.
3. 후보 A의 wildcard 구독은 **관측/디버깅용 옵션**으로 남기되, 감사 경로에는 쓰지 않습니다.

---

## 9. 테스트 전략

### Synchronous drain mode

- 테스트용 `Bus` 생성 시 `WithSyncDrain()` 옵션 제공.
  - 구현 A: M1(직접 실행) 모드로 전환. 가장 단순.
  - 구현 B: 기본 M2이되, `Drain(ctx)` 메서드가 모든 구독 channel이 비고 worker가 idle이 될 때까지 대기.
- 추천: **둘 다 제공**. 단순 유닛 테스트는 A, 순서·panic 검증이 필요한 테스트는 B를 씁니다.

### 시나리오 카탈로그

1. **Happy path**: publish 1건 → 구독자 2명 모두 1회씩 handler 실행. `Drain` 후 counter 검증.
2. **순서 assert**: 한 구독자에게 10건 publish → handler 호출 순서가 publish 순서와 동일(M2 모델 보장).
3. **Panic isolation**: 구독자 A handler가 panic → 구독자 B는 정상 수신(E1.T7).
4. **Cancel during publish**: publish 직후 `sub.Cancel()` → handler가 더 이상 수신하지 않음, 이미 큐에 들어간 이벤트는 수신되거나 드롭.
5. **Causation propagation**: ctx에 causationId 주입 → handler에서 재발행 → 새 이벤트의 causationId가 첫 이벤트 ID와 같음.
6. **Backpressure (Block policy)**: 용량 1, 수신 지연 → publisher가 block → ctx cancel 시 `Publish`가 에러 반환.
7. **Backpressure (DropOldest)**: 용량 1, 연속 3건 → 가장 최근 1~2건만 수신.

### Property-based

- 감사 체인과 이벤트 순서 상관 관계는 `rapid`로 속성 검증 권장(§리스크 테이블과 정합).

---

## 10. Future compatibility (NATS/Redis 교체)

### 교체 대상 (분리 모드 §3.3)

- NATS JetStream: consumer durable, 순서 보장(per-subject), at-least-once.
- Redis Streams: consumer group, XADD/XREADGROUP, 순서 보장(per-stream).

### Interface 뒤로 숨길 것 (leakable 금지)

- channel, goroutine, sync.RWMutex 같은 **내부 동시성 원시타입**. 호출자에게 `chan Event` 노출하지 않습니다.
- backpressure 정책 enum은 인터페이스에 포함 가능(`OverflowPolicy`). NATS/Redis는 같은 의미를 다른 메커니즘으로 제공.
- subscription 취소 방식: `Cancel()`·`Done()` 메서드만 인터페이스. 내부 구현이 channel close든 NATS `Unsubscribe()`든 상관 없음.

### 흘려도 되는 detail (구현별)

- 재시도·DLQ 설정은 **어댑터 생성자 옵션**으로 노출. NATS는 consumer config, Redis는 XACK 정책, inproc는 in-memory retry count. 인터페이스는 공통 개념만 정의합니다.
- 메시지 durability는 publisher에는 보이지 않지만 **ack 시점**이 다릅니다. `Publish`는 "수용됨(수신자들에게 도달 예정)"까지만 보장. 분리 모드에서는 `PublishSync`(스토리지 커밋까지 대기)가 별도 메서드로 추가될 수 있습니다.
- wildcard 구독 문법: inproc은 `tenant.*`·`*.ScanCompleted`, NATS는 `tenant.>`. Phase 1에서는 **exact match만 지원**하고 wildcard는 Phase 3에서 규격화.

### 동일 interface로 swap 가능한가

- **예**, 단 조건:
  1. `Publish`의 의미를 "bus에 수용됨"으로 정의(durable 보장은 별도 메서드).
  2. 구독자 순서 보장은 **per-subscription**만 약속. cross-subscription 순서는 inproc에서도 보장하지 않는 걸로 계약.
  3. DLQ·재시도는 어댑터 config의 일부로, 공통 인터페이스에서는 "실패 이벤트는 경고 로그 + 메트릭"까지만 약속.

Phase 1 `Bus` 인터페이스를 이 전제로 짜면 NATS 교체 시 **도메인 코드 수정 없이** 어댑터 추가만으로 전환 가능합니다.

---

## 11. Go 인터페이스 스케치

`internal/platform/eventbus/` 하위 파일 배치(≤400줄 권장, §3.8):

```
internal/platform/eventbus/
  ├─ bus.go              // Bus 인터페이스, Event, Handler, Subscription 인터페이스
  ├─ inproc/
  │   ├─ bus.go          // inproc.Bus 구현(구독자별 goroutine 모델)
  │   ├─ subscription.go // subscription 구조체 + Cancel 로직
  │   └─ options.go      // SubscribeOption, OverflowPolicy
  └─ testing/
      └─ drain.go        // Drain() 헬퍼 + 직접 실행 sync 모드
```

### 공개 타입

```go
package eventbus

import (
    "context"
    "encoding/json"
    "time"
)

// Event — envelope. 불변.
type Event struct {
    ID            string
    Type          string
    Version       int
    TenantID      string
    OccurredAt    time.Time
    Aggregate     AggregateRef
    Payload       json.RawMessage
    CausationID   string
    CorrelationID string
}

type AggregateRef struct {
    Type string
    ID   string
}

// Handler — 구독자 로직. handler 반환 error는 기본 정책상 "경고 로그 + 메트릭".
// 재시도·DLQ는 어댑터 구현이 관장합니다.
type Handler func(ctx context.Context, evt Event) error

// Subscription — 구독 제어.
type Subscription interface {
    Topic() string
    Cancel()              // idempotent
    Done() <-chan struct{} // 완전 종료 시 close
}

// Bus — 공개 표면.
type Bus interface {
    Publish(ctx context.Context, evt Event) error
    Subscribe(ctx context.Context, topic string, h Handler, opts ...SubscribeOption) Subscription
    // Close는 모든 구독 취소 + publish 거부.
    Close(ctx context.Context) error
}

type SubscribeOption interface{ apply(*subscribeConfig) }

// 내부 전용.
type subscribeConfig struct {
    buffer   int
    overflow OverflowPolicy
    timeout  time.Duration
}

type OverflowPolicy int

const (
    OverflowBlock OverflowPolicy = iota
    OverflowDropOldest
    OverflowDropNewest
)

// Option 헬퍼 (함수 옵션 패턴).
func WithBuffer(n int) SubscribeOption          { /* ... */ }
func WithOverflow(p OverflowPolicy) SubscribeOption { /* ... */ }
func WithPublishTimeout(d time.Duration) SubscribeOption { /* ... */ }
```

### inproc 구현 개요

```go
package inproc

// Bus는 Event Bus의 인프로세스 구현입니다.
type Bus struct {
    mu     sync.RWMutex
    topics map[string][]*subscription
    log    platform.Logger
    now    platform.Clock
    idgen  platform.IDGen
    closed atomic.Bool
}

func New(deps Deps) *Bus { /* ... */ }

// 구독 추가는 O(1), publish는 O(N subscribers for topic).
func (b *Bus) Publish(ctx context.Context, evt Event) error { /* fan-out */ }
func (b *Bus) Subscribe(ctx context.Context, topic string, h Handler, opts ...eventbus.SubscribeOption) eventbus.Subscription { /* ... */ }
```

함수 개별 ≤50줄 유지: `fanOutToSubscribers`, `enqueueWithPolicy`, `runWorker`, `recoverHandlerPanic`로 분해.

### 의존성

- 표준 라이브러리만 사용 (`context`·`sync`·`time`·`encoding/json`).
- 이벤트 ID는 `internal/platform/idgen`(E1.T3)를 호출 — `evt_<ULID>`.
- 시간은 `internal/platform/clock`(E1.T2) 주입.
- 로거는 `internal/platform/logger`(E1.T1).

---

## 12. 결정 사항 (2026-04-23 합의)

> Phase 1 E1.T6/T7 착수 전 7건 미해결 질문에 대한 결정. 모두 본 노트의 "기본 추천" 채택. 본문(§4·§5·§6·§7·§8·§10·§11)은 추천과 정합이라 별도 수정 없음.

1. **R2-1 · Outbox 시점 = Phase 1은 outbox 없이 `tx.Commit()` 후 단순 publish** — 점진성(P12). Phase 2에서 DB outbox 테이블로 승격. 재시작 시 이벤트 누락 가능성은 Phase 1 스케일에서 수용. (R1-2 Bootstrap/Tx 분리와 결합: 도메인 서비스는 `Storage.Tx` 안에서 audit append + outbox-less publish 패턴.)

2. **R2-2 · `Publish` 반환 의미 = 모든 구독자 channel에 enqueue 완료까지 = 수용 보장** — Block 정책 구독자(audit)와 정합. at-most-once 회피. Block 구독자가 있으면 publisher도 ctx 만료까지 block.

3. **R2-3 · Topic 네이밍 = 2-segment `<domain>.<EventName>` 고정** — Phase 1 wildcard 미지원이므로 단순 규격으로 충분. Phase 3 분리 모드(NATS subject) 도입 시 3-segment 또는 hierarchy 재검토.

4. **R2-4 · Handler error 처리 = 기본은 로그+메트릭**, 옵션 `WithCriticalFailure(fn)`으로 도메인이 콜백 등록 — 핵심 구독자(audit)는 이 콜백으로 alert + circuit-break 구현. 기본 정책은 후속 이벤트 처리 계속.

5. **R2-5 · Event 영속 주체 = EventBus는 전달만, audit 도메인이 자체 테이블에 영속** — §3.6 "audit 도메인 또는 별도 테이블" 중 audit 채택. R2-1 outbox-less 결정과 정합 (audit 테이블 자체가 사실상 event log 역할).

6. **R2-6 · Wildcard 구독 = Phase 1 exact match만** — Observability는 도메인별 명시 구독으로 충분. Phase 3 분리 모드 NATS subject wildcard 도입 시 규격화 (`tenant.>` 등).

7. **R2-7 · Correlation ID 생성 주체 = 호출 ctx에 없으면 EventBus가 자동 생성** (`cor_<ULID>`) + debug 로그 — 테스트·Scheduler 내부 잡에서도 correlation 유지. ID는 R1-3와 동일하게 `internal/platform/idgen`(E1.T3 완료) 사용.

---

## 13. 부록 — 검증 체크리스트 (E1.T6/T7 구현 시)

- [ ] `TestEventBusInProcPublishAndSubscribe` — publish → subscriber 수신, causationId 보존 (E1.T6)
- [ ] `TestEventBusHandlerErrorIsolated` — 한 구독자 panic이 다른 구독자·publish에 영향 없음 (E1.T7)
- [ ] `TestEventBusCancelIsIdempotent`
- [ ] `TestEventBusOrderPreservedPerSubscription`
- [ ] `TestEventBusBackpressureBlockPolicy`
- [ ] `TestEventBusBackpressureDropOldest`
- [ ] `TestEventBusCloseRejectsNewPublish`
- [ ] `TestEventBusDrainWaitsForInflight`
- [ ] `go test -race` 녹색
- [ ] 커버리지 ≥ 80% (§E1 Exit)
- [ ] 파일 ≤400줄, 함수 ≤50줄 (CLAUDE.md §파일·함수 크기)
- [ ] depguard — `internal/platform/eventbus`는 다른 도메인을 import하지 않음

---

## 참조 요약

- §3.1 레이어, §3.3 모노리스·분리 모드, §3.6 이벤트 모델, §3.10 실패·복구, §3.12 동시성
- §10.2 감사 대상, §10.3 엔트리 구조, §10.4 해시 체인, §10.13 correlation, §10.15 메트릭
- §01 P1·P5·P9·P11
- `docs/design/phase1-backlog.md` §E1 (T6/T7), §E2 (audit append)
