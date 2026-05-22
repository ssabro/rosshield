# OpenTelemetry tracing 전면 — Phase 11 옵션 A Design (Stage 11.A-1)

> **상태**: Design (Stage 11.A-1) — 코드 0줄 / 마이그레이션 0건 / pack 변경 0. 본 round 는 design doc 만, 코드 진입은 D-P11A-1~5 사용자 확정 후 별 PR(Stage 11.A-2~7).
> **작성일**: 2026-05-22
> **범위**: Phase 11 옵션 A 진입 첫 stage. Phase 11 마지막 옵션 — 옵션 B(SOC2 readiness, v0.12.0 마감) + 옵션 C(audit hash key_epoch input, v0.13.0 마감) 후속. Prometheus metrics + Grafana + structured slog + audit chain 결선 위에 OpenTelemetry SDK 전면 통합 — scan flow trace + multi-region request trace + advisor LLM call trace + Patroni failover trace + log aggregation pipeline. 본 doc 자체는 Stage 분해 + D-P11A 결정 항목까지만 마감.
> **참조**:
> - `docs/design/notes/phase11-backlog-design.md` §4.1 + §12.1 — 본 doc 직접 부모, D-P11-1 = Top 3 순차 B → C → A 확정. 본 doc 은 3순위 옵션 A 본체.
> - `docs/design/notes/audit-chain-rotation-automation-design.md` — Phase 10 옵션 D 본체. fact-check + Stage 분해 패턴 직접 모방 대상.
> - `docs/design/notes/soc2-readiness-design.md` — Phase 11 옵션 B 본체(직전 stage). 패턴 모방.
> - `docs/design/notes/audit-hash-key-epoch-input-design.md` — Phase 11 옵션 C 본체(직전 stage). 패턴 모방.
> - 코드: `internal/platform/metrics/metrics.go`(Prometheus collectors) · `internal/platform/logger/logger.go`(slog contextHandler + RequestID/TraceID ctx key 결선) · `internal/platform/llm/{noop,anthropic,ollama,vllm}/`(4 provider) · `internal/app/scanrun/scanrun.go`(SSH executor + check evaluator + evidence + audit emit hot path) · `internal/api/handlers/replication.go`(multi-region 4 endpoint) · `internal/platform/replication/middleware.go`(standby read-only middleware) · `cmd/rosshield-server/main.go`(http.Server bootstrap + chi router).
> - 마이그레이션: 본 epic 은 마이그레이션 신규 0(trace 는 별 channel, audit chain 영향 0).
> **R 식별자**: R-PHASE11-A(본 stage 전체) — 결정 항목은 D-P11A-1~5.
> **본 문서 작성 위치**: main(head `3ffcbcd` 직후), 단독 sub-agent.
> **비목표** (§10 에서 명시):
> - vendor-specific APM 직접 통합(Datadog · NewRelic · AppDynamics SDK 직접) — ★ 외부 vendor 의존, otel-collector 경유 권장(§10.1).
> - 자체 trace backend 구현 — Jaeger/Tempo customer 위탁(§10.2).
> - audit chain → trace 통합 / audit 대체 — audit 은 별 source of truth 유지(§10.3).
> - profiling(pprof flame graph · CPU/heap 시각화) — 별 epic.
> - production tracing 의 강제 적용 — R14-1 옵트인 원칙 일관, `--otel-disabled` flag default off.

---

## 1. 상태 / 배경

### 1.1 Phase 11.B + Phase 11.C 마감 + Phase 11.A 진입 사실

`docs/design/notes/phase11-backlog-design.md` §12.1 확정(2026-05-21):

| 진입 순서 | 옵션 | 추정 | minor release | 상태 |
|---|---|---|---|---|
| 1순위 (Phase 11.B) | SOC2 Type II readiness | ~6~9주 | v0.12.0 | ✅ 마감 |
| 2순위 (Phase 11.C) | audit hash chain key_epoch input + fg-verify v3 | ~2~3주 | v0.13.0 | ✅ 마감 |
| **3순위 (본 stage)** | **A OpenTelemetry tracing 전면** | **~5~7주** | **v0.14.0** | **본 doc** |

Phase 11.B(SOC2 readiness, v0.12.0) + Phase 11.C(audit key_epoch input, v0.13.0) 두 minor 마감 후 자연 진입. 옵션 A 는 Phase 10 backlog §4.4(옵션 F)에서 권장되었으나 옵션 A·D·E 마감으로 carryover → Phase 11.1 진입 시점에 3순위로 자연 재진입.

### 1.2 본 round 진입 가치

- **multi-region 운영 표면 가시화**: Phase 8~10 에서 multi-region replication + Patroni 자동 failover + `/regions` UI + 5 alert rule 결선. cross-region request 가 다수 hop(LB → primary → standby) 을 거치는 상황에서 단일 process scope 의 slog 만으로는 root cause 추적 한계. distributed tracing 으로 hop 별 latency·error attribution 명확.
- **scan flow 부분 latency 분석**: scanrun orchestrator(`internal/app/scanrun/scanrun.go`) 는 SSH connect(`sshpool` 재사용) → check exec(`SSHExecutor.Exec`) → check evaluate(`CheckEvaluator.Evaluate`) → evidence write(`evidence.Service`) → audit emit(`scan.Service.RecordResult` 직후 publish "scan.progress") 의 5 단계 hot path. 현재 metric 은 ScansStartedTotal/ScansCompletedTotal/ScanFailedChecksTotal counter 만 — **단계별 latency histogram 없음**. 부하 환경(50~100 robot fan-out) 에서 어느 단계가 병목인지 trace 없이 추정 불가능.
- **advisor LLM call trace**: LLM 4 provider(noop · anthropic · ollama · vllm) 결선 + LlmTrace struct(Provider · Model · DurationMs · InputTokens · OutputTokens · Cost · Error) 결선. 그러나 LlmTrace 는 audit emit 만 — http hop 의 trace 와 분리되어 있어 "user 가 advisor 질문 → LLM 호출 → DB tool query → final response" 의 end-to-end trace 부재. token usage 사후 분석은 가능하나 실시간 분산 trace 부재.
- **enterprise customer otel-compatible backend 지원**: customer 가 Jaeger · Tempo · Honeycomb · Datadog · NewRelic · AWS X-Ray · Azure Monitor 등 OpenTelemetry-compatible backend 를 사용 중이면 Lodestar trace 를 자연 흡수 가능. otel 표준 채택 시 vendor lock-in 0.
- **log aggregation pipeline**: 현 `internal/platform/logger/logger.go` 는 slog JSON handler + ctx 기반 tenantId/requestId/traceId attr 자동 부착 결선. 그러나 customer 환경에서 log shipping(Loki · ELK · Splunk · Datadog Logs) 자동화 docs 부재 — trace_id 를 log line 과 cross-reference 하는 wiring 명시 필요.
- **Phase 11 마감 timeline 안정성**: 옵션 A 는 Phase 11 마지막 옵션. 본 doc 마감 + Stage 11.A-2~7 진행 후 Phase 11 전체 마감 → Phase 12 진입 자연 timing.

### 1.3 본 round 범위 · 비범위

- **범위** (Stage 11.A-2~7):
  - OpenTelemetry Go SDK 도입 + tracer provider scaffold (`internal/platform/otel/` 신규 패키지) + bootstrap 결선(`cmd/rosshield-server/main.go` 시작 시 provider init + shutdown hook).
  - HTTP server middleware — request trace + W3C `traceparent` header propagation(`internal/api/handlers/otel_middleware.go`) — chi router Use 결선.
  - scan flow instrument — `internal/app/scanrun/scanrun.go` 의 5 단계(ssh.connect · check.exec · check.evaluate · evidence.write · audit.emit) 각각 span emit.
  - multi-region request trace — `internal/api/handlers/replication.go` 4 endpoint(heartbeat · failover · status · health) + `internal/platform/replication/middleware.go` standby read-only + Patroni REST adapter 의 cross-region call propagation.
  - LLM advisor call trace — `internal/app/advisorrun/llm_client.go` + `internal/platform/llm/{anthropic,ollama,vllm}/*.go` 의 Complete/CompleteStream span emit(LlmTrace → span attribute 매핑).
  - testcontainers integration test(`test/integration/otel_e2e_test.go`) + ops docs(`docs/operations/opentelemetry-setup.md`) + customer 가이드(`docs/onboarding/otel-collector-setup.md` — Jaeger/Tempo backend customer 위탁 패턴) + v0.14.0 minor release.
- **비범위** (§10 명시): vendor-specific APM SDK 직접 통합 · 자체 trace backend · audit chain 통합 · pprof profiling · production tracing 강제 적용.

---

## 2. 현재 상태 fact-check (코드/디렉터리 직접 grep)

본 §은 추측 0, fact 만 명시. 8 영역.

### 2.1 OpenTelemetry import 부재 사실

repo-wide `opentelemetry|otel|OpenTelemetry|traceparent|trace\.Tracer` grep 결과(head `3ffcbcd` 직후, 2026-05-22):

| 영역 | fact |
|---|---|
| `go.mod` 자체 import | **0**. `require` block 의 directive 는 모두 indirect — `go.opentelemetry.io/auto/sdk v1.2.1 // indirect` · `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.65.0 // indirect` · `go.opentelemetry.io/otel v1.41.0 // indirect` · `go.opentelemetry.io/otel/metric v1.41.0 // indirect` · `go.opentelemetry.io/otel/trace v1.41.0 // indirect` 5 패키지가 transitive(다른 dep 이 가져옴) 로만 존재. |
| `internal/**/*.go` import | **0**. 자체 import 0. `internal/platform/metrics/metrics.go` 는 Prometheus client 만 사용. |
| `cmd/**/*.go` import | **0**. `cmd/rosshield-server/main.go` 는 otel 미사용. |
| docs 언급 | `docs/design/notes/phase10-backlog-design.md`(옵션 F) · `docs/design/notes/phase11-backlog-design.md`(옵션 A) · `docs/design/10-audit-and-observability.md`(roadmap 언급) · `docs/design/11-tech-stack-and-roadmap.md`(stack roadmap 언급) · `docs/releases/v0.12.0.md` · `docs/releases/v0.13.0.md`(Phase 11 마감 release) · `CHANGELOG.md`. 모두 **계획 단계**. |
| `web/pnpm-lock.yaml` | front transitive 일부 — front 자체 import 0(별 epic). |
| 비-otel `traceability` 키워드 | `internal/enterprise/robotid/quote_linux_test.go`(TPM attestation traceability 맥락, otel 무관) · `packs/cis-ubuntu-2404/checks/5.1.4.yaml`·`docs/operations/cis-ubuntu-2404-degraded.md`(auditd audit log path 맥락, otel 무관). |

→ **확정**: OpenTelemetry SDK 자체 통합 0. transitive 5 패키지가 `go.mod` 에 indirect 로 등록되어 있어 본 epic 진입 시 `require` block 에 직접 명시로 승격 + 신규 SDK 패키지 추가.

### 2.2 `internal/platform/metrics/metrics.go` Prometheus collectors (Phase 4 E27 + Phase 8~11)

`metrics.go` Read 결과(line 31~120):

| collector | label | 결선 시점 |
|---|---|---|
| `ScansStartedTotal` | tenant | Phase 4 E27 |
| `ScansCompletedTotal` | tenant, status(completed/failed/cancelled) | Phase 4 E27 |
| `ScanFailedChecksTotal` | tenant | Phase 4 E27 |
| `WebhookDeliveriesTotal` | status(success/failed/dead) | Phase 4 E27 |
| `InvitationsSentTotal` · `InvitationsAcceptedTotal` | (none) | Phase 5 |
| `AuditChainHeadSeq` | tenant | Phase 4 E27 |
| `AuditRotationTotal` | status(success/failed/skipped) | Phase 10.D-3+4 |
| `AuditKeyEpoch` | tenant | Phase 10.D-3+4 |
| `AuditChainHashVersion` · `AuditChainHashVersionTransitionTotal` | tenant | Phase 11.C-3 |
| `EventPublishDuration` | topic | Phase 4 E27 |
| `HARole` · `HALeaderEpoch` · `HAFailoverTotal` | (none) | Phase 4 E25 |
| `ReplicationLagSeconds` | application_name | Phase 8 MR.T8 |

→ **확정**: Prometheus collectors 결선 완비 — counter/gauge/histogram 3 종 cover. **distributed tracing 0**. metric label 에 tenant scope 일관 — trace attribute 에도 동일 일관 필요. `--metrics-addr` flag 빈 시 endpoint mount X(opt-in 일관) — otel exporter 도 동일 flag 패턴 적용 권장.

### 2.3 `internal/platform/logger/logger.go` slog contextHandler (TraceID ctx key 결선 사실)

`logger.go` 전문 Read 결과(76 lines):

| 항목 | fact |
|---|---|
| ctxKey | `keyTenantID` · `keyRequestID` · `keyTraceID` 3 키 결선. |
| Setter | `WithTenantID(ctx, id)` · `WithRequestID(ctx, id)` · `WithTraceID(ctx, id)`. |
| Getter | `TenantID(ctx)` · `RequestID(ctx)` · `TraceID(ctx)`. |
| `contextHandler.Handle` | 매 log record 에 tenantId / requestId / traceId attr 자동 부착(빈 문자열이면 skip). |
| handler 구성 | `slog.NewJSONHandler` wrap — JSON-format log line 에 ctx attr 자동 inject. |
| 실 사용 fact | `internal/platform/logger/logger_test.go` 만 `WithTraceID` 호출. **production code path 에서 `WithTraceID` 호출 0** — TraceID ctx key 는 결선되어 있으나 활용 0. |

→ **확정**: trace ID slot 은 logger 단에 이미 결선 — otel propagation 시 span context 의 trace ID 를 자동으로 `WithTraceID(ctx, span.SpanContext().TraceID().String())` 로 채우면 log line 과 trace 가 자연 cross-reference. **신규 컬럼/구조체 없음**. 기존 slog JSON output 호환성 보존(traceId field 만 추가됨).

### 2.4 HTTP handler 트리 + middleware chain (`internal/api/handlers/` + `cmd/rosshield-server/main.go::newMux`)

`main.go` line 141~232 Read 결과:

| 영역 | fact |
|---|---|
| router | `chi.NewRouter()` — chi v5 router. `mux.HandleFunc("GET /healthz", ...)` stdlib mux + `mux.Handle("/api/v1/", apiRouter)` chi sub-router. |
| 결선된 middleware | `handlers.RequireLeaderForWrites(p.HA)` (E25 Stage 3 — HA leader gate, opt-in) · `replication.StandbyReadOnlyMiddleware(p.ReplicationConfig)` (Phase 8 Stage 2 — standby write 차단) · `h.AuthMiddleware` (E9 Stage B — bearer auth) · `h.RequireRole("admin","auditor")` (RBAC Stage 2). |
| **otel middleware** | **부재**. tracing middleware 0. request span emit 0. |
| W3C `traceparent` header | grep 결과 0 — 본 epic 에서 신규 도입. |
| RequestID 활용 | `internal/api/gen/openapi.gen.go:641 RequestId string` — OpenAPI 스펙에 노출 가능한 response field 만. handler 자체에서 `WithRequestID` 호출 0(2.3 일관). |
| HTTP server | `&http.Server{Handler: mux, ...}` — Handler interface 호환. otel `otelhttp.NewHandler(handler, "rosshield")` wrap 으로 자연 통합 가능. |
| metrics 별 mux | `metricsAddr != ""` 일 때 별 `http.Server{Addr: *metricsAddr}` (line 562~582) — `/metrics` 별 mount(R14-1 opt-in). otel exporter 도 동일 패턴 권장(`--otel-endpoint` flag 빈 시 disable). |

→ **확정**: chi router 기반 middleware chain 결선 — otel middleware 는 chi `apiRouter.Use(otelhttp.NewMiddleware("rosshield"))` 1 line 으로 mount 가능. 단, `RequireLeaderForWrites` · `StandbyReadOnlyMiddleware` 보다 **앞**에 등록해야 span 이 모든 request 를 cover.

### 2.5 `internal/app/scanrun/scanrun.go` hot path 5 단계 (E6 Orchestrator)

`scanrun.go` line 1~60 Read 결과:

| 단계 | 결선 코드 | span 후보 |
|---|---|---|
| 1. SSH connect | `Deps.Executor scan.SSHExecutor` injected — `cmd/rosshield-server/scanexec.go` 에서 `sshpool.Pool.Get(ctx, target)` 호출(Pool idle 재사용 + keepalive). | `ssh.connect` span — attribute: robot.id, robot.host, pool.reused. |
| 2. check exec | `Executor.Exec(ctx, robot, check)` — SSH session run + stdout/stderr capture. | `check.exec` span — attribute: check.id, check.kind, robot.id. |
| 3. check evaluate | `Deps.Evaluator scan.CheckEvaluator` — rule-based outcome 판정. | `check.evaluate` span — attribute: check.id, outcome(pass/fail/error/skipped/manual). |
| 4. evidence write | `Deps.Evidence evidence.Service` — SSH stdout/stderr redact + 해시 + blob 영속(N:M ref). | `evidence.write` span — attribute: blob.size, redact.count. |
| 5. audit emit (publish) | `scan.Service.RecordResult` + `Bus.Publish("scan.progress")`. | `scan.publish` span — attribute: scan.id, seq. terminal 전이 시 `scan.completed` span — attribute: status. |

→ **확정**: scan flow 5 단계 span 식별 명확 — 단일 fan-out 단위(robots × checks 카티전 곱) 마다 child span 5 개 emit. parent span 은 HTTP middleware 의 `POST /api/v1/scans/{id}:start` request span. worker pool(semaphore, default 10) 은 동일 ctx 를 worker goroutine 으로 전달하므로 ctx 의 span context propagation 자연.

### 2.6 multi-region 4 endpoint + replication middleware (Phase 8+9 결선)

`internal/api/handlers/replication.go` + `internal/platform/replication/middleware.go` Read 결과:

| 영역 | fact |
|---|---|
| replication endpoint | `GET /api/v1/replication/status` · `POST /api/v1/replication/heartbeat`(standby → primary ping) · `POST /api/v1/replication/failover`(admin trigger) · `GET /api/v1/replication/health`(LB probe). 4 endpoint cover. |
| `StandbyReadOnlyMiddleware` | line 18~24 — exempt path 5 개(`/health`·`/healthz`·`/readyz`·`/api/v1/replication/heartbeat`·`/api/v1/replication/failover`) 외 모든 write method 409 Conflict. trace 도입 시 standby exempt path 의 span 도 emit(특히 failover trigger 는 cross-region distributed transaction 의 핵심). |
| cross-region call | Patroni REST adapter(`internal/platform/replication/patroni/` 추정 — phase 9 결선) 에서 outbound HTTP call 발생. otel client middleware(`otelhttp.NewTransport(http.DefaultTransport)`) wrap 필수 — `traceparent` header outgoing 전파. |
| `HARole` · `HALeaderEpoch` metric | Phase 4 E25 결선 — trace attribute 에도 동일 차원 일관 표기 권장(`region`, `role`, `leader_epoch`). |

→ **확정**: multi-region request hop 별 trace 위해 (a) standby 의 outbound Patroni call → otel client 적용 (b) primary 의 inbound endpoint → otel server middleware 적용 두 측 모두 cover 필요. failover 시 span 의 attribute 에 `region.from` · `region.to` · `failover.trigger` 명시 권장.

### 2.7 LLM 4 provider Complete 호출 지점 (`internal/platform/llm/` + `internal/app/advisorrun/`)

`internal/platform/llm/{noop,anthropic,ollama,vllm}/*.go` + `internal/app/advisorrun/llm_client.go` Read 결과:

| 영역 | fact |
|---|---|
| Adapter interface | `internal/platform/llm/llm.go:77~87` — `Complete(ctx, req) (CompleteResponse, error)` · `CompleteStream(ctx, req) (<-chan StreamChunk, error)`. ctx propagation 자연. |
| `LlmTrace` struct | line 60~69 — Provider · Model · StartedAt · DurationMs · InputTokens · OutputTokens · Cost · Error. 모든 어댑터 동일 형식. **span attribute 매핑 자연** — `llm.provider` · `llm.model` · `llm.tokens.input` · `llm.tokens.output` · `llm.cost.usd` · `llm.duration.ms`. |
| anthropic | `internal/platform/llm/anthropic/anthropic.go` — stdlib `net/http` + SSE 파싱. `http.Client` 의 `Transport` 를 `otelhttp.NewTransport` 로 wrap → outbound HTTPS call 의 trace 자동. |
| ollama · vllm | 동일 패턴 — stdlib `net/http` 만. otel transport wrap 으로 자동 cover. |
| noop | `ErrLLMDisabled` 즉시 반환 — span 은 root advisor span 만, child llm.call span 도 short-lived 로 emit(disabled outcome 명시). |
| `LLMClientAdapter.CompleteWithTools` | `internal/app/advisorrun/llm_client.go:36` — 모든 LLM 호출은 본 메서드 경유. **단일 instrument point** — span emit + LlmTrace 매핑을 본 메서드 내부에 1회 cover하면 4 provider 동시 적용. |

→ **확정**: advisor LLM call instrument 는 (a) `LLMClientAdapter.CompleteWithTools` 내부의 1 span(`llm.complete`) (b) 각 provider 의 `http.Client.Transport` 를 `otelhttp.NewTransport` wrap 두 layer 로 cover. token usage · cost 는 span attribute 로 자연 매핑.

### 2.8 docs/operations + onboarding 결선 사실 (otel 가이드 부재)

`docs/operations/` 트리(2026-05-22):
- audit-chain-key-rotation.md · audit-rotation-cosign.md · audit-rotation-s3.md · audit-rotation-verify.md · audit-verify-cli.md · cis-ubuntu-2404-degraded.md · multi-region-ha-setup.md · 기타 — **otel/jaeger/tempo/loki/elk/observability 관련 docs 0**.

`docs/onboarding/` 트리:
- customer-info-template.md · walkthrough.md · quickstart.md · demo-script.md · llm-private-deployment.md · multi-region-ha-setup.md · audit-rotation-cosign.md · 기타 — **otel collector setup 가이드 0**.

→ **확정**: 본 epic Stage 11.A-7 에서 `docs/operations/opentelemetry-setup.md`(자체 운영 가이드) + `docs/onboarding/otel-collector-setup.md`(customer 위탁 패턴 — Jaeger/Tempo/Datadog backend 별 collector config 샘플) 신규 작성.

---

## 3. 위협 모델 / 요구사항

### 3.1 신규/잔여 위협

| 위협 | 가능성 | 영향 | 본 epic cover |
|---|---|---|---|
| multi-region 운영자가 cross-region request latency 원인 파악 지연 → customer churn | 중(고부하 customer 진입 시) | 운영자 대응 지연 + churn | Stage 11.A-3 + 11.A-5 — HTTP middleware + Patroni REST cross-region trace propagation. |
| scan flow 5 단계 중 어느 단계가 병목인지 trace 부재 → tuning blind spot | 중(fan-out 50~100 robot 시) | 운영 ROI 모호 | Stage 11.A-4 — scan flow 5 span instrument. |
| advisor LLM token cost spike 시 어느 prompt/tool 가 원인인지 trace 부재 | 중(advisor 옵트인 customer 시) | cost guardrail 효과성 약화 | Stage 11.A-6 — LLM provider 4 종 span + token attribute 매핑. |
| customer 가 자체 otel-compatible backend(Jaeger/Tempo/Datadog) 통합 의지 → 표준 미지원 시 lock-out | 높음(enterprise 영업 시점) | enterprise 영업 기회 손실 | Stage 11.A-2~7 전 stage — otel 표준 SDK 일관. |
| trace 추가로 inline overhead → high-throughput customer latency 영향 | 중(sample rate 부적절 시) | latency p99 spike | D-P11A-3 — sampling policy 권장 default ratio_based 0.1 + parent-based. |
| log line 과 trace 의 cross-reference 부재 → 디버깅 시 trace ID 수동 grep | 중(현 slog 만 활용 시) | 디버깅 효율 저하 | Stage 11.A-2 — slog contextHandler 의 traceId attr 를 span context 에서 자동 inject. |
| air-gap customer 의 otel exporter outbound 차단 → endpoint 무한 timeout | 중(air-gap 시) | 운영 장애 위험 | `--otel-disabled` flag default off + air-gap docs 명시. |
| otel SDK transitive dep 큼 → go.mod size + supply chain attack surface 증가 | 낮음(SDK 자체는 CNCF graduated, 검증된 dep) | 보안 표면 증가 | dep 직접 명시(transitive 승격) + cosign verify 일관. |

### 3.2 요구사항

| ID | 요구 | 측정 |
|---|---|---|
| R11A-1 | otel SDK 통합은 vendor-neutral — Jaeger/Tempo/Datadog/Honeycomb/AWS X-Ray 등 OpenTelemetry-compatible backend 모두 호환 | otel-collector 경유 standard otlp-grpc/otlp-http exporter 만 사용, vendor SDK 0 |
| R11A-2 | R14-1 옵트인 일관 — `--otel-disabled` flag default off + air-gap customer 호환 | flag 빈 시 provider noop, span emit 0 |
| R11A-3 | tenant scope 격리 일관(설계서 §1.4) — 모든 span attribute 에 `tenant.id` 필수 | span emit helper 가 ctx 의 TenantID 자동 부착 |
| R11A-4 | audit chain 영향 0 — trace 는 별 channel, append-only audit hash 변경 0 | 본 epic 마이그레이션 0, audit hash version 변경 0 |
| R11A-5 | Prometheus metrics backward compat — 기존 metric label/name 변경 0(parallel emit) | metric backward compat test (Phase 4 E27 일관) |
| R11A-6 | sampling 부적절로 인한 latency 영향 측정 — 부하 시나리오에서 p99 측정 | benchmark test (50 robot fan-out 시 trace on/off p99 delta < 5%) |
| R11A-7 | 보수적 추정 일관 | memory `feedback_design_doc_conservative.md` |
| R11A-8 | design doc 우선 | 1일+ 임계 작업 (memory `feedback_design_doc_first.md`) |

---

## 4. 옵션 비교

각 옵션마다 (a) 설계 요약 (b) 가치 (c) 노력 추정(보수적) (d) 전제·의존 (e) 리스크.

### 4.1 옵션 A — OpenTelemetry SDK 전면 + Jaeger/Tempo backend 권장 (권장 default)

**설계 요약**:
- `internal/platform/otel/` 신규 패키지 — TracerProvider scaffold + sampler config(parent-based + ratio_based) + exporter wiring(otlp-grpc + otlp-http 양 옵션).
- 4 hot path 모두 instrument: (1) HTTP server middleware(chi router Use, `otelhttp.NewMiddleware`) (2) scan flow 5 span(`internal/app/scanrun/`) (3) multi-region request trace(replication endpoint + Patroni REST outbound) (4) LLM advisor call(`LLMClientAdapter.CompleteWithTools` + 4 provider 의 `http.Client.Transport` otelhttp wrap).
- Prometheus federation 옵션 — `--prometheus-federation-endpoint` flag 로 otel exporter 가 customer Prometheus 와 동시 export 가능(metric 도 otel collector 경유).
- log aggregation pipeline — slog contextHandler 의 traceId attr 자동 채움(2.3 결선 활용) + customer 가이드(Loki · ELK · Splunk 별 sample config).
- v0.14.0 minor release(Phase 11 마감).

**가치**:
- paying customer ★★ / enterprise ★★★★(otel-compatible backend 영업 적합성) / compliance ★★(distributed system audit trace) / operational ★★★★(multi-region · scan flow 디버깅 효율) / 기술 부채 ★★★(distributed tracing 부재 마감)

**노력 추정 (보수적)**: **5~7주** — Stage 분해 §6 참조. otel SDK 도입 + provider scaffold 1주 + HTTP middleware + scan flow + multi-region + LLM 4 layer instrument 3주 + testcontainers + ops docs + customer 가이드 + release 1~1.5주 + sampling 정책 검증 + 부하 benchmark 0.5주.

**전제·의존**: 없음. Phase 11.B + Phase 11.C 마감 baseline 활용. customer 환경의 otel-collector 또는 backend(Jaeger/Tempo) 설치는 customer 위탁 트랙.

**리스크**: **중**. otel SDK transitive dep 증가(go.mod size — 현 indirect 5 패키지 → direct 10+ 패키지 추정). sampling 정책 부적절 시 production latency 영향. multi-region cross-boundary propagation 일관(W3C `traceparent` standard 준수 + Patroni REST 의 vendor 응답 호환).

### 4.2 옵션 B — OpenTelemetry SDK 부분 적용 (scan flow 만)

**설계 요약**: 가장 큰 ROI 영역(scan flow 5 단계 latency 분석)만 우선. HTTP middleware + scan flow span 만 instrument. multi-region request trace + LLM call trace 는 별 epic 보류.

**가치**:
- paying customer ★ / enterprise ★★(부분 otel 지원 — backend 통합 적합성 일부) / compliance ★ / operational ★★★(scan tuning 가능, multi-region · advisor 디버깅은 여전히 blind) / 기술 부채 ★★

**노력 추정 (보수적)**: **2~3주** — otel SDK 도입 1주 + HTTP middleware + scan flow span 1주 + testcontainers + ops docs + release 0.5~1주.

**전제·의존**: 없음.

**리스크**: **낮음~중**. 부분 적용으로 instrument 일관성 부족 — customer 가 advisor trace 도 요구 시 별 epic 추가 부담. otel 표준 부분 지원으로 enterprise 영업 적합성 모호.

### 4.3 옵션 C — 표준 logging 강화 + trace ID propagation 수동 (otel 미도입)

**설계 요약**: OpenTelemetry SDK 미도입. 현 slog contextHandler 의 traceId ctx key(2.3 결선) 활용 + HTTP middleware 로 `X-Trace-Id` header 받기/생성 + outbound HTTP call 에 동일 header 전파(수동). customer 는 ELK/Splunk 에서 traceId field grep + cross-reference.

**가치**:
- paying customer ★ / enterprise ★(표준 미지원, vendor backend 통합 부재) / compliance ★ / operational ★★(traceId grep 가능, span tree 없음) / 기술 부채 ★

**노력 추정 (보수적)**: **1~2주** — slog → trace_id auto propagation 1주 + HTTP middleware(traceId 생성·전파) 0.5주 + outbound HTTP client wrapping(수동) 0.5주.

**전제·의존**: 없음. otel transitive 5 indirect dep 그대로 유지(직접 import 0).

**리스크**: **중**. (a) gRPC/HTTP cross-boundary instrumentation 부담 — 모든 outbound call 에 header 수동 inject 필요(otelhttp.NewTransport 없음). (b) span tree 구조(parent-child span 관계) 부재 — root cause attribution 한계. (c) vendor backend 통합 부재 — Jaeger/Tempo/Datadog 모두 자체 trace format 으로 변환 필요. customer 영업 약점.

### 4.4 옵션 D — Datadog/NewRelic APM 직접 통합 (★ 외부 vendor 의존)

**설계 요약**: otel-collector 경유 0 — vendor SDK 직접 통합. Datadog APM Go tracer 또는 NewRelic Go agent 를 직접 import. scan flow + LLM call + multi-region 모두 vendor SDK 의 trace API 로 instrument.

**가치**:
- paying customer ★(vendor 사용 customer 만 적합) / enterprise ★(특정 vendor 한정) / compliance ★ / operational ★★★(vendor backend 풀 활용) / 기술 부채 ★★(특정 vendor lock-in)

**노력 추정 (보수적)**: **4~5주** — vendor SDK 도입 + 4 hot path instrument + vendor backend 통합 + docs. 옵션 A 대비 약간 짧으나 lock-in 위험 큼.

**전제·의존**: ★ **customer 의 vendor 계약 의존** — Datadog/NewRelic 라이선스 의존. air-gap customer 비호환.

**리스크**: **높음**. (a) vendor lock-in — 추후 customer 가 Jaeger/Tempo 전환 시 재구현 필요. (b) air-gap 비호환. (c) 본 doc 의 비목표 §10.1 명시 — 거부.

### 4.5 매트릭스 종합

| 옵션 | 설계 요약 | 가치 종합 | 시간 | 위험 | vendor neutral | 권장 |
|---|---|---|---|---|---|---|
| **A** otel SDK 전면 + Jaeger/Tempo 권장 | 4 hot path 모두 | ★★★★ | 5~7주 | 중 | ✅ | **권장 default** |
| **B** otel SDK 부분 (scan flow 만) | scan flow 만 | ★★ | 2~3주 | 낮음~중 | ✅(부분) | 차선 |
| **C** logging 강화 + 수동 trace_id | otel 미도입 | ★★ | 1~2주 | 중 | ✅ | 거부(span tree 부재) |
| **D** Datadog/NewRelic APM 직접 | vendor SDK | ★★ | 4~5주 | 높음 | ✗ | 거부(비목표 §10.1) |

---

## 5. Top 1 권장 — 옵션 A (otel SDK 전면)

memory `feedback_design_doc_conservative.md` 일관 — 잠재 효과/시간 보수적.

### 5.1 근거

- **vendor-neutral 표준**: OpenTelemetry 는 CNCF graduated project — Jaeger · Tempo · Honeycomb · Datadog · NewRelic · Lightstep · AWS X-Ray · Azure Monitor · Google Cloud Trace 등 거의 모든 APM backend 가 otlp 표준 흡수. customer 가 자유롭게 backend 선택 → vendor lock-in 0.
- **기존 Prometheus metric + slog 자연 통합**: 옵션 A 의 Stage 11.A-2 는 slog contextHandler 의 traceId ctx key(2.3 결선) 활용 + Prometheus metric 그대로 parallel emit. backward compat 0 회귀. 옵션 C 는 span tree 부재로 multi-hop 디버깅 한계.
- **enterprise 영업 가치**: 옵션 A 에서 customer 가 자체 backend(Jaeger/Tempo) 운영 시 Lodestar trace 가 자연 흡수 — Phase 11.B SOC2 readiness 의 자연 후속(SOC2 CC7.2 "monitoring of system performance" 통제 cover 강화).
- **Phase 11 마감 timeline 적합**: 5~7주 추정 — Phase 11.B(6~9주) · Phase 11.C(2~3주) 후 자연 진입. Phase 12 진입 전 Phase 11 전체 마감.
- **옵션 D 는 §10.1 비목표 명시** — vendor-specific APM 직접 통합 거부, otel-collector 경유 패턴 권장.
- **옵션 B(부분) 는 instrument 일관성 약화** — 4 hot path 중 1 개만 cover 시 customer 가 LLM cost · multi-region debugging 시 다시 별 epic 필요 → 누적 비용 증가.
- **옵션 C(otel 미도입) 는 span tree 부재** — multi-hop 분산 trace 의 핵심 가치(parent-child 관계 + service map) 부재. 옵션 A 의 ROI 와 비교 시 long-term 약점 큼.

### 5.2 추정

**5~7주** (Stage 11.A-2~7 합계) — R11A-7 보수적 일관. Stage 분해 §6 참조.

---

## 6. Stage 분해 (옵션 A 채택 가정)

memory `feedback_design_doc_first.md` 일관 — 본 doc 에서 옵션 A 만 Stage 분해, 옵션 B/C/D 진입은 D-P11A-1 변경 시 별 design doc 위임.

### 6.1 Stage 11.A-1 — design doc (본 round)

본 doc. 코드 0줄 / 마이그레이션 0건 / pack 변경 0. D-P11A-1~5 결정 항목까지만.

### 6.2 Stage 11.A-2 — OpenTelemetry SDK 도입 + tracer provider scaffold + bootstrap 결선

추정 **1주**.
- `go.mod` 의 5 transitive otel 패키지를 direct 로 승격 + 신규 추가(`go.opentelemetry.io/otel/exporters/otlp/otlptrace` · `go.opentelemetry.io/otel/sdk` · `go.opentelemetry.io/otel/sdk/trace` · `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`).
- `internal/platform/otel/` 신규 패키지 — `provider.go`(TracerProvider scaffold + sampler config + exporter wiring) · `propagator.go`(W3C tracecontext + baggage propagator) · `attribute.go`(tenant.id 등 공통 attr helper) · `noop.go`(disabled 시 short-circuit) + 단위 테스트.
- `cmd/rosshield-server/main.go` bootstrap — `--otel-endpoint` flag(빈 시 disabled, R14-1 일관) + `--otel-exporter` flag(otlp-grpc|otlp-http) + `--otel-sample-rate` flag(0.0~1.0, default 0.1) + provider init + graceful shutdown hook.
- slog contextHandler 의 traceId attr 자동 inject — span context 의 trace ID 를 ctx 에 자동 부착(`otel.GetTextMapPropagator().Inject` + `logger.WithTraceID` 래퍼).
- testcontainers Jaeger/otel-collector 기반 smoke test.

### 6.3 Stage 11.A-3 — HTTP server middleware (request trace + W3C propagation)

추정 **1주**.
- `internal/api/handlers/otel_middleware.go` 신규 — `otelhttp.NewMiddleware("rosshield")` wrap + tenant attribute 자동 부착 + RequireLeaderForWrites/StandbyReadOnlyMiddleware 보다 앞에 등록.
- chi router 적용 — `apiRouter.Use(otelMiddleware)`. healthz path 는 sampler exclusion 적용(low-value high-volume probe noise 회피).
- W3C `traceparent` header propagation 검증 — incoming 시 parent span context 추출, outgoing 시 inject.
- request span attribute: `http.method` · `http.route` · `http.status_code` · `tenant.id` · `user.id`(인증 후).
- 단위 테스트(span attribute 검증) + chi integration test.

### 6.4 Stage 11.A-4 — scan flow instrument (5 span)

추정 **1주**.
- `internal/app/scanrun/scanrun.go` — 5 단계 child span emit(2.5 fact-check 참조):
  - `ssh.connect` — attribute: `robot.id` · `robot.host` · `pool.reused`(bool, sshpool 재사용 여부).
  - `check.exec` — attribute: `check.id` · `check.kind` · `robot.id`.
  - `check.evaluate` — attribute: `check.id` · `outcome`.
  - `evidence.write` — attribute: `blob.size` · `redact.count`.
  - `scan.publish` — attribute: `scan.id` · `seq` · terminal 시 `status`.
- worker pool ctx 전파 — semaphore 의 goroutine 으로 parent span 의 ctx 정확히 propagate.
- HealthFailureThreshold 발동 시 span 에 `robot.offline` event 추가.
- benchmark — 50 robot fan-out 시 p99 latency on/off delta 측정.

### 6.5 Stage 11.A-5 — multi-region request trace (Patroni REST + replication endpoint)

추정 **1주**.
- `internal/api/handlers/replication.go` 4 endpoint — Stage 11.A-3 의 HTTP middleware 자동 cover, span attribute 보강(`region.from` · `region.to` · `replication.role` · `failover.trigger.actor`).
- Patroni REST adapter(`internal/platform/replication/patroni/*.go`) — outbound `http.Client.Transport` 를 `otelhttp.NewTransport(http.DefaultTransport)` 로 wrap → `traceparent` 자동 inject.
- standby read-only middleware exempt path(/health · /healthz · /readyz · heartbeat · failover) span sampling 정책 — heartbeat 는 high-volume 이라 ratio 0.01 권장.
- testcontainers 2-region(primary + standby) e2e — `POST /api/v1/replication/failover` 시 cross-region span tree 검증.
- failover audit emit 의 span 식별자(trace_id) attribute 부착 — audit ↔ trace cross-reference 강화.

### 6.6 Stage 11.A-6 — LLM advisor call trace (4 provider)

추정 **0.5주**.
- `internal/app/advisorrun/llm_client.go::LLMClientAdapter.CompleteWithTools` — 단일 instrument point(2.7 fact-check). span: `llm.complete` · attribute: `llm.provider` · `llm.model` · `llm.tokens.input` · `llm.tokens.output` · `llm.cost.usd` · `llm.duration.ms` · `llm.stop_reason` · `llm.error`.
- `internal/platform/llm/anthropic/anthropic.go` · `ollama/ollama.go` · `vllm/vllm.go` — 각 provider 의 `http.Client.Transport` 를 `otelhttp.NewTransport` wrap. noop 는 short-circuit span(disabled outcome attribute).
- LlmTrace ↔ span attribute 매핑 helper(`internal/platform/otel/llm.go`).
- token usage spike 시 span event 추가(token > threshold).
- 단위 테스트 + advisor integration test 의 span 검증.

### 6.7 Stage 11.A-7 — testcontainers integration + ops docs + customer 가이드 + v0.14.0 release

추정 **1~1.5주**.
- `test/integration/otel_e2e_test.go` 신규 — testcontainers Jaeger 또는 otel-collector container + 4 hot path span 전수 검증(HTTP + scan flow + multi-region + LLM).
- Playwright e2e — `/regions` 페이지에서 region 별 trace_id 표시(옵션). UI 변경 최소(별 micro epic 분할 가능).
- `docs/operations/opentelemetry-setup.md` 신규 — 자체 운영 가이드. exporter 옵션 · sampling 정책 권장 · air-gap 비활성 패턴 · troubleshooting.
- `docs/onboarding/otel-collector-setup.md` 신규 — customer 위탁 패턴. Jaeger/Tempo/Datadog/NewRelic backend 별 collector config 샘플(otlp receiver + 각 backend exporter).
- Grafana dashboard 갱신 — trace 통계 panel 추가(span count by service/region · LLM duration histogram). 기존 metric panel 변경 0.
- CHANGELOG entry [0.14.0] + `docs/releases/v0.14.0.md` 작성 — Phase 11 마감 release.
- v0.14.0 tag(사용자 명시 요청 시 push).

**Stage 11.A-2~7 합계 = ~5~6주** (보수적). R11A-7 일관.

---

## 7. 결정 항목 (D-P11A-1·2·3·4·5)

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시.

### 7.1 D-P11A-1 — 옵션 채택

- (1) **옵션 A 채택 — otel SDK 전면 + Jaeger/Tempo 권장** (**권장 default**).
- (2) 옵션 B 채택 — scan flow 만 부분 적용(2~3주 단축, multi-region/LLM 별 epic).
- (3) 옵션 C 채택 — otel 미도입, logging + 수동 trace_id(span tree 부재).
- (4) 옵션 D 거부 — vendor-specific APM(§10.1 비목표 일관).
- (5) Phase 11 마감 후 별 Phase 12 옵션으로 이월.

**근거**: §5 권장 분석 일관. vendor-neutral 표준 + Jaeger/Tempo/Datadog 모든 backend 호환 + 기존 Prometheus + slog + audit chain 자연 통합 + Phase 11 마감 timeline 적합. 옵션 B 는 instrument 일관성 약화, 옵션 C 는 span tree 부재(multi-hop 디버깅 한계), 옵션 D 는 §10.1 비목표.

### 7.2 D-P11A-2 — trace exporter (otlp-grpc vs otlp-http vs both)

- (1) **otlp-grpc 권장 default + otlp-http 옵션 plug** (**권장 default**) — gRPC 는 streaming 효율 + 표준 backend(Jaeger/Tempo/Datadog) 모두 지원. air-gap 또는 HTTP only 환경은 http exporter 옵션.
- (2) otlp-http 만 — HTTP 만, 모든 환경 호환 + dep 감소(gRPC client 제외).
- (3) both — `--otel-exporter` flag 로 customer 선택. SDK dep 부담 약간 증가.
- (4) stdout exporter 만 — debug 용도(production 비추천).

**근거**: gRPC 가 streaming 효율 우수하나 일부 corporate proxy 환경 호환 모호 → http exporter 옵션 plug 으로 cover. 옵션 (3) both 도 합리, dep 부담 작음.

### 7.3 D-P11A-3 — sampling policy

- (1) **parent-based + ratio_based 0.1 default** (**권장 default**) — parent span 의 sampling decision 우선 + root span 은 10% sampling. high-throughput customer 에도 latency 영향 최소(< 5% p99 delta 예상).
- (2) always_on — 모든 span emit. production 비추천(latency 영향 + storage 비용 증가).
- (3) ratio_based 0.01 — 1% sampling. low-volume customer 또는 cost-sensitive.
- (4) ratio_based 1.0 + tail-based at collector — 모두 emit 후 collector 단에서 sampling. 정밀하나 collector 부담 큼.
- (5) customer 별 flag(`--otel-sample-rate` 0.0~1.0).

**근거**: parent-based 는 distributed system 의 일관성 보장(parent 가 sampling 안 했으면 child 도 emit 안 함). ratio 0.1 default 는 production 부하 영향 최소 + 디버깅 가치 보존. R11A-6 일관(부하 시 p99 delta < 5%).

### 7.4 D-P11A-4 — log aggregation backend 권장

- (1) **customer 선택 — Loki · ELK · Splunk · Datadog Logs 모두 docs 가이드 제공** (**권장 default**) — Lodestar 측은 slog JSON output 표준 일관, customer 가 자체 backend 의 collector(promtail · filebeat · fluentd) 로 흡수.
- (2) Loki 권장 default — Grafana Loki 가 Tempo 와 자연 통합(trace_id 클릭 → log line) + cosign 가능.
- (3) ELK 권장 default — enterprise 시장 점유 큰 ELK 우선 docs.
- (4) Lodestar 자체 log aggregation 결선 — 별 epic(범위 외).

**근거**: customer 환경 다양 — 특정 backend 권장 default 시 customer lock-in. otel 의 vendor-neutral 정신 일관. 옵션 (2) Loki 도 합리 — Grafana 통합 customer 다수.

### 7.5 D-P11A-5 — customer-facing metrics export (Prometheus federation vs otel exporter)

- (1) **두 옵션 plug — Prometheus federation default + otel metric exporter plug** (**권장 default**) — 기존 Prometheus collectors 그대로 유지(backward compat) + customer 가 otel metric exporter 활성 시 동일 metric 을 otlp 로도 동시 emit.
- (2) Prometheus federation 만 — 현 상태 유지, otel metric 미통합.
- (3) otel exporter 만(Prometheus deprecate) — backward compat 회귀 위험.
- (4) Prometheus deprecate + otel 만(future) — long-term plan, Phase 12+ 이후.

**근거**: Phase 4 E27 결선 Prometheus collectors 의 backward compat 일관(R11A-5). otel metric 은 옵션 plug 으로 customer 선택. 옵션 (3)·(4) 는 backward compat 회귀 위험으로 거부.

---

## 8. 마이그레이션 / 호환성 영향

### 8.1 코드 변경 큰 영역 (4 hot path 모두 instrument)

| 영역 | 변경 표면 | 위험 |
|---|---|---|
| HTTP middleware | `chi.Router.Use` 1 line 추가 + 기존 middleware 와 순서 정렬. | 낮음 — handler 자체 코드 변경 0, middleware 만 추가. |
| scan flow | `internal/app/scanrun/scanrun.go` worker 함수 내부 5 span emit 코드 추가. | 중 — ctx propagation 정확성 검증 필요(worker goroutine 의 ctx 누락 시 span 미연결). |
| multi-region | Patroni REST adapter 의 `http.Client.Transport` wrap + replication endpoint span attribute 추가. | 중 — outbound call 의 `traceparent` header inject 가 Patroni REST 호환성 영향 0 확인. |
| LLM call | `LLMClientAdapter.CompleteWithTools` 내부 1 span + 4 provider 의 transport wrap. | 낮음 — adapter pattern 자연. |
| bootstrap | `cmd/rosshield-server/main.go` 의 flag 추가 + provider init/shutdown. | 낮음 — graceful shutdown hook 정확성. |
| slog | `internal/platform/logger/logger.go` 의 traceId 자동 채움(2.3 결선 활용). | 낮음 — 기존 traceId ctx key 활용. |

### 8.2 backward compat

- **Prometheus metric backward compat**: 기존 metric label/name 변경 0 — parallel emit(R11A-5 일관). customer 측 Grafana dashboard 영향 0.
- **audit chain 영향 0**: trace 는 별 channel — append-only audit hash 변경 0(R11A-4 일관). Phase 11.C-3 결선 v3 hash 그대로.
- **logger JSON output 호환**: 기존 traceId field 가 빈 시 미노출(slog 의 ctx key 가 빈 문자열이면 attr inject 안 함, 2.3 결선 동작 유지). production log parser(Loki · ELK) backward compat.
- **disabled 시 회귀 0**: `--otel-endpoint` 빈 시 provider noop — span emit 0 + transport wrap 도 noop. R11A-2(R14-1 일관).

### 8.3 customer 환경 영향

- **otel-collector 또는 backend 설치는 customer 위탁**: Lodestar 측은 otlp endpoint URL 만 제공 → customer 가 자체 backend(Jaeger/Tempo/Datadog) 운영. `docs/onboarding/otel-collector-setup.md` 가이드(Stage 11.A-7).
- **air-gap customer**: `--otel-endpoint` 빈 시 자동 disabled. air-gap 호환 R11A-2 일관.

---

## 9. 리스크

- **otel SDK transitive dep 큼** — go.mod size 증가(현 indirect 5 → direct 10+ 예상). 빌드 시간 영향 미세. supply chain attack surface 증가 — cosign verify 일관 + CNCF graduated project 표준 신뢰 baseline.
- **sampling 정책 부적절 시 production 영향** — always_on 은 production 비추천(D-P11A-3 (1) 권장 default 일관). 부하 benchmark 필수(R11A-6 — 50 robot fan-out 시 p99 delta < 5%).
- **multi-region trace context propagation 복잡** — Patroni REST 의 vendor 응답 + DNS routing + application-level fence 의 cross-boundary 일관(W3C `traceparent` standard 준수). Patroni REST 호환성 검증 필수.
- **LLM provider 4종 instrument 일관** — anthropic SSE + ollama HTTP + vllm OpenAI-compatible + noop short-circuit 각각 transport wrap 패턴 일관 필요. LlmTrace ↔ span attribute 매핑 helper(`internal/platform/otel/llm.go`) 가 단일 진실 source.
- **slog contextHandler traceId 자동 inject 시점** — span context 의 trace ID 가 valid 한지 확인(noop tracer 또는 disabled 시 빈 trace ID 처리). 2.3 결선 ctx key 호환 일관.
- **chi middleware 순서** — `otelMiddleware` 가 `RequireLeaderForWrites` · `StandbyReadOnlyMiddleware` 보다 앞 위치 필수 — 그렇지 않으면 401/409 응답 span 이 emit 안 됨. Stage 11.A-3 의 정렬 검증.
- **Prometheus federation + otel metric 동시 emit 시 중복** — D-P11A-5 (1) 권장 default 일관. metric 동일성 일관(label/value).
- **testcontainers Jaeger/otel-collector 의존성** — CI 시 docker pull 비용 증가. Stage 11.A-2 의 smoke test 는 가벼운 stdout exporter 로 우선 cover, Stage 11.A-7 의 e2e 만 Jaeger container 활용.

---

## 10. 비목표 / 거부

### 10.1 vendor-specific APM 직접 통합 거부

Datadog APM Go tracer · NewRelic Go agent · AppDynamics SDK · Dynatrace OneAgent 등 vendor SDK 의 직접 import 거부. 모든 customer backend 통합은 otel-collector 경유 — collector 단에서 vendor exporter(datadog · newrelic · dynatrace) 활성. lock-in 회피 + vendor-neutral 표준 일관 + 옵션 D 거부 일관(§4.4).

### 10.2 자체 trace backend 구현 거부

Lodestar 측에서 trace storage · UI · query engine 자체 구현 0. customer 가 Jaeger · Tempo · Honeycomb · Datadog · NewRelic 등 표준 backend 운영. Lodestar 는 otlp emit 만.

### 10.3 audit chain → trace 통합 거부

audit chain 은 별 source of truth 유지 — Phase 1+ baseline 일관. trace 는 디버깅·관측 용도. audit hash chain(Phase 11.C v3) 영향 0. R11A-4 일관. cross-reference 는 attribute 단(trace_id 를 audit event metadata 에 inject) 으로 가능하나 audit hash input 변경 0.

### 10.4 profiling (pprof flame graph · CPU/heap) 거부

distributed tracing 과 별 epic. pprof 는 process-local profiling — distributed system 의 trace 와 직교. customer 요구 시 별 Phase 12 후보.

### 10.5 production tracing 강제 적용 거부

R14-1 옵트인 원칙 일관 — `--otel-endpoint` flag default off + air-gap customer noop. trace emit 강제 0.

### 10.6 tenant_id 없는 신규 span 거부

설계서 §1.4 멀티테넌시 원칙 일관 — 모든 span attribute 에 `tenant.id` 필수(R11A-3). cross-tenant trace 노출 금지.

### 10.7 audit 테이블 UPDATE/DELETE 거부

설계서 §1.9 불변성 원칙 일관 — trace 는 별 channel, audit 테이블 변경 0.

### 10.8 Remote push 자동화 거부

CLAUDE.md 일관 — local 커밋 OK, remote push 사용자 명시 요청 시에만. v0.14.0 tag push 도 사용자 결정.

---

## 11. 참조

### 11.1 직전 design doc 패턴

- `docs/design/notes/audit-chain-rotation-automation-design.md` — Phase 10 옵션 D 본체. fact-check + Stage 분해 + 결정 항목 패턴 직접 모방.
- `docs/design/notes/soc2-readiness-design.md` — Phase 11 옵션 B 본체(직전 stage). 패턴 모방.
- `docs/design/notes/audit-hash-key-epoch-input-design.md` — Phase 11 옵션 C 본체(직전 stage). 패턴 모방.
- `docs/design/notes/phase11-backlog-design.md` — Phase 11 진입 doc. 본 doc 직접 부모(§4.1 + §12.1).
- `docs/design/notes/multi-region-ha-design.md` — Phase 8 epic 본체. multi-region trace 의 baseline.
- `docs/design/notes/auto-failover-research.md` — Phase 9 epic 본체. Patroni REST 의 baseline.

### 11.2 OpenTelemetry 표준 + Lodestar 결선 자산

- OpenTelemetry CNCF spec — https://opentelemetry.io (Go SDK + W3C tracecontext + otlp protocol).
- `go.opentelemetry.io/otel`(v1.41.0) · `sdk/trace` · `exporters/otlp/otlptrace` · `contrib/instrumentation/net/http/otelhttp` — 본 epic 직접 사용 패키지.
- Lodestar 결선 자산: Prometheus(Phase 4 E27 + Phase 8~11 추가) · Grafana(Phase 4 E27) · slog contextHandler(`internal/platform/logger/logger.go` — TraceID ctx key 결선) · audit chain(Phase 1+) · check-health hook(Phase 10.D E35).

### 11.3 release / CHANGELOG

- `docs/releases/v0.12.0.md` — Phase 11.B SOC2 readiness.
- `docs/releases/v0.13.0.md` — Phase 11.C audit hash key_epoch input + fg-verify v3.
- v0.14.0 — 본 epic 마감 시 신규.
- `CHANGELOG.md` — [0.14.0] entry(Stage 11.A-7 신규).

### 11.4 설계서

- `docs/design/01-principles.md` — 12 원칙(특히 §1.4 멀티테넌시 + §1.9 불변성 + §1.10 프라이버시 + R14-1 옵트인).
- `docs/design/03-architecture.md` — observability layer.
- `docs/design/10-audit-and-observability.md` — observability roadmap.
- `docs/design/11-tech-stack-and-roadmap.md` — stack roadmap.

### 11.5 코드/디렉터리 fact-check 참조

- `go.mod` line 191~195 — 5 otel indirect transitive(자체 import 0 확정).
- `internal/platform/metrics/metrics.go` line 31~120 — Prometheus collectors 14 종.
- `internal/platform/logger/logger.go` line 9~75 — slog contextHandler + TraceID ctx key 결선.
- `internal/platform/llm/{noop,anthropic,ollama,vllm}/` — 4 provider Complete 호출 지점.
- `internal/app/advisorrun/llm_client.go` line 36 — 단일 instrument point(LLMClientAdapter.CompleteWithTools).
- `internal/app/scanrun/scanrun.go` line 1~60 — 5 단계 hot path.
- `internal/api/handlers/replication.go` line 53~186 — 4 endpoint.
- `internal/platform/replication/middleware.go` line 18~24 — standby exempt path 5 개.
- `cmd/rosshield-server/main.go` line 141~232 — chi router + middleware chain.

### 11.6 memory feedback

- `feedback_design_doc_first.md` — 1일+ 임계 design doc 우선.
- `feedback_design_doc_conservative.md` — 잠재 효과/시간 보수적.
- `feedback_parallel_agents.md` — 매 stage 시작 시 병렬 가능성 재평가.
- `feedback_user_tracks.md` — D1·E36·SOC2 감사·customer trigger 등 외부 트랙 제외(★ 표기).
- `feedback_no_rest_recommendation.md` — 휴식 옵션 자동 포함 X.
- `feedback_recommend_next_actions.md` — 다음 추천 작업 3~5건 명시.
- `feedback_skip_handoff.md` — handoff edit/commit/push 생략, CHANGELOG + release notes + commit 메시지로 trace.
- `feedback_go_commit_pipeline.md` — gofmt + go mod tidy + goimports + errcheck 4종 commit 전 검증.

---

## 12. 결정 확정 (2026-05-22)

사용자 D-P11A-1 결정 = **옵션 A** (otel SDK 전면 + Jaeger/Tempo backend 권장). D-P11A-2·3·4·5는 권장 default 채택:

- **D-P11A-1 = 옵션 A** — otel SDK 전면 + vendor-neutral CNCF graduated 표준.
- **D-P11A-2 = otlp-grpc + otlp-http both** — customer 환경 다양성 cover (collector 따라 선택).
- **D-P11A-3 = parent_based sampling** — root span ratio 5% default + child span은 parent 결정 일관. production performance 안전.
- **D-P11A-4 = customer 선택, Loki 권장** — docs에 Loki + Grafana stack 권장 default 명시. Splunk/ELK customer는 자유 선택.
- **D-P11A-5 = otel exporter + Prometheus federation 양쪽** — 기존 Prometheus 자산 보존 + otel metric은 추가 노출.

### 12.1 Stage 11.A-2 진입 가능

Stage 11.A-2: OpenTelemetry SDK 도입 + `internal/platform/otel/` provider scaffold + bootstrap 결선 (~1주). 첫 코드 stage.

