# `internal/domain/integration/webhook`

> E23 Phase 3 — 외부 SIEM·통합 시스템으로 도메인 이벤트를 송출하는 도메인.

## 도메인 책임

- **WebhookEndpoint CRUD**: 테넌트별 등록 가능한 송출 대상 (URL + secret + event 필터).
- **WebhookDelivery 영속**: 모든 송출 시도가 append-only 큐에 기록됨 (P9 불변성).
- **HMAC-SHA256 payload 서명**: 수신자가 공유 secret으로 위·변조 검증.
- **SIEM 형식 변환**: CEF (ArcSight·Splunk OOTB) + ECS (Elastic Stack) + raw JSON.
- **재시도 정책**: 1m·5m·15m·1h·24h (5회) — `NextRetryDelay()`.

## 결합 규칙 (P5)

webhook 도메인은 **audit·scan·insight 도메인을 직접 import하지 않습니다**.

후속 stage(E23-B)에서 bootstrap이 EventBus 구독자를 결선하여 도메인 이벤트(`scan.completed`·`insight.created`·`audit.checkpoint`)를 `webhook.DomainEvent` DTO로 매핑한 뒤 `Service.Enqueue`로 라우팅합니다. 본 도메인은 그 DTO만 알고 있습니다.

## 외부 의존성

**0** — stdlib `crypto/hmac`/`crypto/sha256`/`encoding/hex`/`net/url`/`encoding/json`만. SIEM 라이브러리 도입 X.

## 모델

### `WebhookEndpoint`

| 필드 | 설명 |
|---|---|
| `ID` | `wh_<ULID>` |
| `TenantID` | 멀티테넌시 |
| `URL` | absolute http/https URL (`ValidateURL`) |
| `Secret` | HMAC-SHA256 키 (평문 보관 — KMS 통합은 후속) |
| `Events` | 구독할 EventType 배열. 빈 배열은 모든 known event 구독 |
| `Format` | payload 직렬화 형식 (`PayloadFormatJSON`·`PayloadFormatCEF`·`PayloadFormatECS`) |
| `Enabled` | false면 Enqueue가 skip — 기존 delivery 보존 |

### `WebhookDelivery` (append-only)

| 필드 | 설명 |
|---|---|
| `ID` | `whd_<ULID>` |
| `EndpointID` | FK |
| `TenantID` | denormalized for cross-tenant 격리 |
| `EventType` | `scan.completed` 등 |
| `EventID` | 원천 `EventBus.Event.ID` (cross-reference) |
| `Payload` | 직렬화된 본문 (raw bytes) |
| `AttemptCount` | 0=대기, 1~5=시도 |
| `LastAttemptedAt` / `NextAttemptAt` | 재시도 worker 스케줄링 |
| `Succeeded` | true면 더 이상 시도 안 함 |
| `LastResponseStatus` / `LastError` | 마지막 시도 결과 |

INSERT는 `Enqueue` 시점 1회. UPDATE는 후속 stage(Process worker)가 attempt 갱신만. **DELETE 절대 안 함** (P9).

## HMAC 서명 형식

수신자는 `X-Rosshield-Signature` 헤더로 검증:

```
X-Rosshield-Signature: sha256=<hex>
X-Rosshield-Event: scan.completed
X-Rosshield-Delivery: whd_01H8...
```

`SignPayload(secret, body)` → `"sha256=<hex>"` 반환. 수신자 측은 `VerifySignature()` 보조 함수 사용 가능 (const-time 비교).

## SIEM 형식

### CEF (Common Event Format)

```
CEF:0|rosshield|rosshield|1.0|insight.created|insight.created|7|tenant=tn_acme event_id=evt_X rt=1746619200000 aggregate_type=Insight aggregate_id=ins_Y
```

severity 매핑: info=2, low=3, medium=5, high=7, critical=10.

### ECS (Elastic Common Schema)

```json
{
  "@timestamp": "2026-05-07T12:00:00Z",
  "ecs.version": "8.11",
  "event": {
    "dataset": "rosshield.scan.completed",
    "kind": "event",
    "action": "scan.completed",
    "severity": 0,
    "id": "evt_X"
  },
  "organization": {"id": "tn_acme"},
  "observer": {"vendor": "rosshield", "product": "rosshield"},
  "rosshield": {"payload": {...}}
}
```

severity 매핑: info=0, low=1, medium=2, high/critical=3.

## 재시도 정책 (R23-1)

| Attempt | 다음 재시도 대기 |
|---|---|
| 1 (첫 시도 실패) | 1분 |
| 2 | 5분 |
| 3 | 15분 |
| 4 | 1시간 |
| 5 | 24시간 |
| 6+ | dead-letter (시도 종료) |

`NextRetryDelay(attemptCount)` → `(time.Duration, bool)`.

## 마이그레이션

`internal/platform/storage/migrations/sqlite/0019_webhooks.sql` — `webhook_endpoints` + `webhook_deliveries`.

> backlog는 0020을 가정했으나 실제 next available은 0019. E22 PostgreSQL 어댑터(0019 후보)는 본 stage 시작 시점에 미존재 — 0019로 진입.

## 후속 stage

본 stage는 **도메인 모델 + sqliterepo + 마이그레이션 + 단위 테스트**까지. 다음 stage들이 결선·HTTP 표면을 추가:

- **E23-B Process worker**: `next_attempt_at <= now AND succeeded = 0` 쿼리로 dispatch, `net/http` POST + HMAC 헤더, response status 기준 재시도 또는 dead-letter.
- **E23-C HTTP 표면**: `internal/api/handlers/webhook.go` — `POST /v1/webhooks`/`PUT`/`DELETE`/`GET`, `GET /v1/webhooks/{id}/deliveries`.
- **E23-D bootstrap 결선**: `cmd/rosshield-server/bootstrap.go` — EventBus 구독 어댑터 (`scan.completed` → `Service.Enqueue`).
- **E23-E AuditEmitter**: webhook endpoint CRUD 시 audit chain에 기록 (`webhook.endpoint.created`/`updated`/`deleted`/`enqueued`).
- **B3 `/integrations` Web UI**: backlog 참조.

## 테스트

- `webhook_test.go`: HMAC 서명·재시도 backoff·CEF/ECS 형식·검증 helper (12 테스트).
- `sqliterepo/repo_test.go`: CRUD + tenant scope 격리 + Enqueue 필터링 (10 테스트).

```bash
go test ./internal/domain/integration/webhook/...
```
