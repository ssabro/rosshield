# `internal/app/webhookrun`

> E23-B Phase 3 — Webhook delivery dispatcher (재시도 Process worker).

## 책임

- 주기적 `tick`(default 30s)으로 `webhook.Service.ListDueDeliveries`로 dispatch 대상을 회수.
- 각 delivery마다 endpoint(URL/secret/format)를 회수하고 HTTP POST를 1회 수행.
- 응답 2xx → `MarkDeliverySucceeded` (succeeded=1·last_response_status·attempt_count·last_attempted_at).
- 응답 non-2xx 또는 transport error → `MarkDeliveryFailed`
  (attempt_count++·last_response_status·last_error·next_attempt_at = `now + NextRetryDelay(attempt)`).
- `attempt_count >= MaxRetryAttempts(=5)`이면 dead-letter — `next_attempt_at`은 그대로 두고
  `ListDueDeliveries`가 attempt_count 조건으로 자동 제외 (재시도 종료).
- endpoint가 사라졌거나 disabled면 즉시 dead-letter (재시도하지 않음).

## 알고리즘

```
loop {
  select {
    <-ctx.Done():    exit
    <-stop:           exit
    <-ticker.C:
      now = clock.Now().UTC()
      due = Bootstrap(): ListDueDeliveries(now, BatchLimit=50)
      for d in due {
        if ctx.Err(): return
        ep = Tx(tenant=d.TenantID): GetEndpoint(d.EndpointID)
        if !ep.Enabled or ep missing:
          markFailed(MaxRetryAttempts, ...)  // dead-letter
          continue
        sig = HMAC-SHA256(ep.Secret, d.Payload)  → "sha256=<hex>"
        status, err = http.Post(ep.URL, body=d.Payload, headers={
          Content-Type, X-Rosshield-Signature, X-Rosshield-Event, X-Rosshield-Delivery
        })
        attempt = d.AttemptCount + 1
        if 200 <= status < 300:
          markSucceeded(attempt, status, now)
        else:
          delay, _ = NextRetryDelay(attempt)  // 1m·5m·15m·1h·24h, attempt 5+ → no retry
          next = now + delay  (Max 도달이면 next=now → ListDueDeliveries에서 제외)
          markFailed(attempt, next, status, errMsg, now)
      }
  }
}
```

부팅 직후 즉시 1회 `dispatchOnce`를 호출 — backlog 빠른 소진.

## HTTP 헤더

| 헤더 | 값 |
|---|---|
| `Content-Type` | `application/json` (json/ecs) · `text/plain; charset=utf-8` (cef) · `application/octet-stream` (그 외) |
| `X-Rosshield-Signature` | `sha256=<hex>` — HMAC-SHA256(endpoint.Secret, body) |
| `X-Rosshield-Event` | EventType (예: `scan.completed`) — 수신자 라우팅 키 |
| `X-Rosshield-Delivery` | `whd_<ULID>` — 수신자 idempotency 키 |
| `User-Agent` | `rosshield-webhook/1.0` |

수신자 측은 `webhook.VerifySignature(headerValue, secret, body)` 보조 함수로 const-time 비교.

## 트랜잭션 모델

- `ListDueDeliveries` / `MarkDeliverySucceeded` / `MarkDeliveryFailed`는 cross-tenant write이므로
  `Storage.Bootstrap` 진입점만 사용 (worker는 system 잡).
- `GetEndpoint`만 tenant-scoped — delivery row의 `tenant_id`로 `WithTenantID` 주입한 후 `Storage.Tx`.

## 외부 의존성 0

stdlib `net/http` + `crypto/hmac`(`webhook.SignPayload` 경유)만. 외부 webhook 라이브러리 도입 X.

## bootstrap 결선

`cmd/rosshield-server/bootstrap.go`:

```go
webhookSvc := webhookrepo.New(webhookrepo.Deps{Clock: clk, IDGen: ids})

webhookDispatcher := webhookrun.New(webhookrun.Deps{
  Logger: logger, Storage: store, Clock: clk,
  Webhook: webhookSvc,
  TickInterval: cfg.WebhookTickInterval,  // 0 → DefaultTickInterval (30s)
})
go webhookDispatcher.Run(context.Background())
```

Shutdown:

```go
p.WebhookDispatcher.Stop()
<-p.WebhookDispatcher.Done()
```

main.go에 `--webhook-tick-interval=10s` 등으로 조정 가능.

## 후속 stage

본 stage는 **Process worker + bootstrap 결선**까지. 다음 stage들이 표면을 추가합니다:

- **E23-C HTTP 표면**: `internal/api/handlers/webhook.go` —
  `POST /v1/webhooks` (CRUD), `GET /v1/webhooks/{id}/deliveries` 등.
- **E23-D EventBus 구독**: bootstrap에 `scan.completed`·`insight.created`·`audit.checkpoint`
  구독자를 결선하여 `webhook.Service.Enqueue`로 라우팅.
- **E23-E AuditEmitter**: webhook endpoint CRUD 시 audit chain에 기록
  (`webhook.endpoint.created`/`updated`/`deleted`).
- **B3 `/integrations` Web UI**: backlog 참조.

## 테스트

`dispatcher_test.go` — 7건:

- `TestDispatcher200MarksSucceeded`: 2xx 응답 → `Succeeded=1`.
- `TestDispatcher500RetriesWithBackoff`: 500 응답 → `attempt_count=1` + `next_attempt_at ≈ now+1m`.
- `TestDispatcherNetworkFailureRetries`: closed listener → transport error + `last_error` 채움.
- `TestDispatcherSignsRequestWithHMAC`: HMAC 헤더 + Event/Delivery 헤더 검증.
- `TestDispatcherDeadLetterNotRedispatched`: `attempt_count = MaxRetryAttempts`이면 POST 발생 X.
- `TestDispatcherDisabledEndpointDeadLetters`: disabled endpoint의 delivery는 즉시 dead-letter.
- `TestDispatcherStopExits`: `Stop()` 후 `Done()` 채널 닫힘.

```bash
go test ./internal/app/webhookrun/...
```
