# OpenTelemetry tracing setup (Phase 11.A)

> **상태**: GA — Stage 11.A-2~6 자산 결선 (provider scaffold · HTTP middleware · scan flow 5 span · multi-region + Patroni REST 결선 · LLM 4 provider span · `httpclient.WrapClient` helper). 운영 default = **opt-in disabled** (`Enabled=false`, noop tracer, overhead 0).
> **대상**: 운영자 (분산 trace backend 활성 진행 중).
> **참조**: `docs/design/notes/opentelemetry-tracing-design.md` (D-P11A-1~5 결정 + Stage 11.A-2~7 분해), `docs/releases/v0.14.0.md`.

본 문서는 Lodestar v0.14.0 에서 결선된 OpenTelemetry tracing 자산의 활성·운영·해석·한계를 정리합니다. customer 측 otel-collector + backend(Jaeger · Tempo · Datadog · NewRelic 등) 설치 가이드는 별도 `docs/onboarding/otel-collector-setup.md` 를 참조하세요.

---

## 1. 개요

### 1.1 도입 가치

Lodestar v0.14.0 에서 OpenTelemetry SDK 전면 통합. 이전 v0.11.x~v0.13.x 까지의 Prometheus metrics + Grafana + structured slog + audit chain 결선 위에 **분산 trace** 채널을 추가해 다음 표면을 가시화:

| 영역 | trace 가치 | 결선 stage |
|---|---|---|
| HTTP 요청 root trace + W3C `traceparent` propagation | cross-service 요청 흐름 추적 | Stage 11.A-3 |
| scan flow 5 단계 (ssh.connect · check.exec · check.evaluate · evidence.write · scan.publish) | 부하 환경에서 어느 단계가 병목인지 hop-level 분석 | Stage 11.A-4 |
| multi-region replication + Patroni REST 자동 failover | cross-region hop 별 latency·error attribution | Stage 11.A-5 |
| LLM 4 provider (noop · anthropic · ollama · vllm) 호출 | advisor end-to-end trace + token/cost attribute | Stage 11.A-6 |
| outbound HTTP (`httpclient.WrapClient`) | patroni · webhook · LLM 의 outbound `traceparent` 자동 inject | Stage 11.A-5 |

### 1.2 opt-in default 원칙

설계서 §R14-1 옵트인 원칙 일관 — `--otel-enabled=false` (default) 시 **noop tracer** 반환, exporter 미연결, span emit 0, transport wrap 0. 호출자(scanrun · advisorrun · patroni adapter · httpclient.WrapClient)는 동일 인터페이스 사용 (nil 체크 불필요).

> air-gap customer 는 `--otel-enabled=false` 그대로 두면 noop. trace 흡수 backend 가 air-gap 망 안에 있으면 `--otel-endpoint=internal-collector:4317 --otel-insecure=true` 로 활성 가능.

### 1.3 cross-channel 통합

- **slog ↔ trace_id**: `internal/platform/logger/logger.go` 의 `contextHandler` 가 span context 에서 `trace_id` 를 자동 추출해 JSON log line 의 `traceId` field 에 inject. Loki/ELK/Splunk 등 log aggregation backend 에서 단일 trace_id 로 log + trace cross-reference 가능 (D-P11A-4).
- **Prometheus federation + otel metric**: 기존 Prometheus collectors 14종은 그대로 유지 (backward compat, D-P11A-5 권장 default). 향후 otel metric exporter plug 시 동시 emit 가능. customer Grafana dashboard 영향 0.
- **audit chain 영향 0**: trace 는 별 channel — append-only audit hash (v3) 변경 0. R11A-4 일관.

---

## 2. 빠른 시작

### 2.1 단일 instance + Jaeger all-in-one

가장 단순한 setup. dev/staging 환경에 적합.

```bash
# 1. Jaeger all-in-one (OTLP 수신 + UI 한 묶음, 단일 컨테이너).
docker run -d --rm --name jaeger \
  -p 4317:4317 \      # OTLP gRPC receiver
  -p 4318:4318 \      # OTLP HTTP receiver
  -p 16686:16686 \    # Jaeger UI
  jaegertracing/all-in-one:latest

# 2. Lodestar 활성 — gRPC exporter + 5% root sampling default.
./rosshield-server \
  --otel-enabled=true \
  --otel-endpoint=localhost:4317 \
  --otel-insecure=true

# 3. 검증.
curl http://localhost:8080/api/v1/healthz   # trace emit 1건 (HTTP root span)
open http://localhost:16686                  # Jaeger UI 에서 service.name=rosshield 확인
```

### 2.2 환경 변수 fallback

CLI flag 가 default 일 때만 env 가 우선 (CLI 명시 값 우선 보호 패턴).

| flag | env | default | 의미 |
|---|---|---|---|
| `--otel-enabled` | `ROSSHIELD_OTEL_ENABLED` | `false` | tracer provider 활성 여부 |
| `--otel-endpoint` | `ROSSHIELD_OTEL_ENDPOINT` | `""` | OTLP collector host:port (Enabled=true 시 필수) |
| `--otel-exporter` | `ROSSHIELD_OTEL_EXPORTER` | `grpc` | `grpc` (4317) 또는 `http` (4318) |
| `--otel-sampling` | `ROSSHIELD_OTEL_SAMPLING` | `0.05` | root sampling ratio (0=never, 1.0=always, 그 외=parent_based) |
| `--otel-insecure` | `ROSSHIELD_OTEL_INSECURE` | `false` | TLS 미사용 (dev/air-gap 만 권장) |

env 활성 예:

```bash
export ROSSHIELD_OTEL_ENABLED=true
export ROSSHIELD_OTEL_ENDPOINT=otel-collector.observability.svc:4317
export ROSSHIELD_OTEL_EXPORTER=grpc
export ROSSHIELD_OTEL_SAMPLING=0.05
./rosshield-server
```

### 2.3 production 권장 setup

```bash
./rosshield-server \
  --otel-enabled=true \
  --otel-endpoint=otel-collector.observability.svc.cluster.local:4317 \
  --otel-exporter=grpc \
  --otel-sampling=0.05 \
  # --otel-insecure 미지정 → TLS 활성 (default)
```

production 은 반드시 TLS 활성 (`--otel-insecure=false` default). air-gap 환경에서 자체 collector 가 TLS 미지원 시에만 `--otel-insecure=true`.

---

## 3. Backend 선택지 (D-P11A-1 vendor-neutral)

Lodestar 는 OTLP 표준 emit 만 담당 — 어떤 backend 를 사용할지는 customer 결정. vendor lock-in 0.

| backend | OTLP 호환 | 자체 호스팅 | 권장 환경 |
|---|---|---|---|
| **Jaeger** | gRPC + HTTP | 가능 (all-in-one 또는 cluster) | dev/staging, on-prem small |
| **Grafana Tempo** | gRPC + HTTP | 가능 (Loki/Mimir 와 동일 stack) | on-prem + Grafana 사용자 |
| **Honeycomb** | gRPC + HTTP | SaaS | high-cardinality query, debugging |
| **Datadog APM** | otel-collector 경유 | SaaS | full-stack observability |
| **NewRelic** | otel-collector 경유 | SaaS | enterprise APM |
| **AWS X-Ray** | otel-collector + adot exporter | AWS managed | AWS-native |
| **Azure Monitor** | otel-collector + azuremonitor exporter | Azure managed | Azure-native |
| **GCP Cloud Trace** | otel-collector + googlecloud exporter | GCP managed | GCP-native |

vendor-specific APM SDK 의 **직접 통합은 거부** (`docs/design/notes/opentelemetry-tracing-design.md` §10.1) — 모든 vendor backend 는 **otel-collector 경유** 권장. lock-in 회피 + 표준 일관.

---

## 4. Trace 분석

### 4.1 scan flow 5 span (Stage 11.A-4)

`scanrun.Service.Run` 1회 호출 시 emit 되는 span tree:

```
scan.run                                       (root, application scope)
├── attribute: rosshield.tenant_id = <tenant>
├── attribute: rosshield.scan_id = <scan_id>
├── attribute: rosshield.fleet_id = <fleet_id>
├── attribute: rosshield.pack_id = <pack_id>
│
├── ssh.connect (span name: "scan.ssh.connect")
│   └── attribute: rosshield.robot_id, ssh.host
├── check.exec (span name: "scan.check.exec")
│   └── attribute: check.id, check.category, ssh.exit_code
├── check.evaluate (span name: "scan.check.evaluate")
│   └── attribute: check.id, check.severity, check.outcome
├── evidence.write (span name: "scan.evidence.write")
│   └── attribute: evidence.byte_count, evidence.format
└── scan.publish (span name: "scan.publish")
    └── attribute: audit.event.action, eventbus.topic
```

병목 분석 예시:

- **ssh.connect 가 p99 > 5s** — sshpool 재사용 cache miss · DNS resolution slow · MTU 문제. Patroni REST 트래픽과 cross-correlate 가능.
- **check.exec 가 p99 > 30s** — 특정 check 의 host-side bash heavy. check.id attribute 로 drill-down 후 pack 의 timeout 조정 또는 check 분할.
- **evidence.write 가 p99 > 1s** — DB I/O 또는 blob store 지연. multi-region 배포에서는 standby 의 replication lag 도 확인.

### 4.2 multi-region + Patroni REST trace (Stage 11.A-5)

```
HTTP POST /api/v1/replication/heartbeat        (root, region=primary)
├── attribute: rosshield.region = "primary"
├── attribute: http.method = POST
├── attribute: http.route = "/api/v1/replication/heartbeat"
│
├── HTTP GET patroni                           (outbound, child span)
│   ├── attribute: rosshield.outbound.target = "patroni"
│   ├── attribute: http.url = "https://patroni-1.svc:8008/cluster"
│   └── attribute: http.status_code = 200
│
└── audit.emit (replication.heartbeat.received)
```

cross-region failover 시 standby region 의 incoming HTTP 가 primary 의 outbound `traceparent` 와 동일 trace_id 로 묶임 → cross-region distributed trace 완성.

### 4.3 LLM advisor call trace (Stage 11.A-6)

```
advisor.complete                               (parent, application scope)
└── llm.complete (scope: rosshield/advisorrun/llm)
    ├── attribute: llm.provider = "anthropic" | "ollama" | "vllm" | "noop"
    ├── attribute: llm.model = "claude-sonnet-4.5" 등
    ├── attribute: llm.tool_count = 3                 (size only, PII 회피)
    ├── attribute: llm.tokens.input = 1024            (size only)
    ├── attribute: llm.tokens.output = 256
    ├── attribute: llm.cost.usd = 0.0042
    ├── attribute: llm.duration.ms = 1830
    ├── attribute: llm.stop_reason = "end_turn"
    └── attribute: llm.error = ""                     (성공 시 빈 값)
```

prompt/response content · tool args 는 **attribute 에 포함 0** (D-LLM-3 PII 회피 엄격, design §6.6).

---

## 5. Sampling 정책

### 5.1 parent_based default

D-P11A-3 권장 default = `parent_based(traceidratiobased(0.05))`:

- **root span** — `0.05` (5%) 확률로 sampling. 부하 환경 p99 latency 영향 < 5% (R11A-6 가드).
- **child span** — parent 의 sampling decision 상속. distributed system 의 일관성 보장 (parent 가 sampling 안 했으면 child 도 emit 안 함).

### 5.2 customer 조정 가이드

```bash
# low-volume customer (< 100 scan/day) — 모두 emit 유효.
--otel-sampling=1.0

# 일반 (default) — 5%.
--otel-sampling=0.05

# high-volume customer (> 10k scan/day) — 1%.
--otel-sampling=0.01

# 완전 비활성 (provider 활성하되 span emit 0, 진단용).
--otel-sampling=0.0
```

### 5.3 tail-based sampling

collector 단에서 latency · error · attribute 기준 sampling 정책 적용 가능. Lodestar 측은 `--otel-sampling=1.0` + collector tail-based 결합 권장 (high-fidelity but cost-controlled).

```yaml
# otel-collector tail-based 정책 예 (Stage 11.A-7 onboarding 참조).
processors:
  tail_sampling:
    decision_wait: 10s
    policies:
      - name: errors
        type: status_code
        status_code: { status_codes: [ERROR] }
      - name: slow
        type: latency
        latency: { threshold_ms: 5000 }
      - name: baseline
        type: probabilistic
        probabilistic: { sampling_percentage: 5 }
```

---

## 6. Log aggregation 옵션 (D-P11A-4)

`internal/platform/logger/logger.go` 의 `contextHandler` 가 span context 에서 `trace_id` 자동 추출 → JSON log line 의 `traceId` field 에 inject. customer 환경의 collector 가 흡수.

### 6.1 권장 default — Loki + Grafana Tempo

```
slog JSON output → promtail → Loki → Grafana
                                      ↓
                              trace_id click → Tempo
```

- Grafana 단에서 log line 클릭 → trace_id 추출 → Tempo trace view 자연 점프 (Grafana datasource correlation).
- Lodestar 측 추가 wiring 0 — slog JSON output 표준 그대로.

### 6.2 ELK stack

```
slog JSON output → filebeat → Logstash → Elasticsearch → Kibana
```

Kibana 에서 trace_id 로 search filter → Jaeger UI URL 수동 점프 (Kibana 7.x+ Trace integration 또는 manual link).

### 6.3 Splunk

```
slog JSON output → splunk forwarder → Splunk → Splunk APM (OTLP)
```

Splunk APM 의 OTLP receiver 활성 시 Lodestar 측은 그냥 splunk-otel-collector 로 OTLP emit.

### 6.4 Datadog Logs

```
slog JSON output → datadog-agent → Datadog Logs → Datadog APM 자동 correlation
```

Datadog agent 가 trace_id 자동 인식 (otel-collector 단의 `datadog` exporter 와 일관).

---

## 7. PII 회피 정책

설계서 §1.10 프라이버시 기본값 일관 — span attribute 에 **민감 정보 노출 0**:

| 영역 | 노출 | 회피 |
|---|---|---|
| **LLM prompt content** | ❌ 절대 노출 0 | `llm.tokens.input` (size only) |
| **LLM response content** | ❌ 절대 노출 0 | `llm.tokens.output` (size only) |
| **LLM tool args** | ❌ 절대 노출 0 | `llm.tool_count` (size only) |
| **SSH credentials** | ❌ 절대 노출 0 | `ssh.host` (식별자만) |
| **check evidence content** | ❌ 절대 노출 0 | `evidence.byte_count` (size only) |
| **audit event metadata** | ❌ 절대 노출 0 | `audit.event.action` (action name 만) |
| **HTTP request body** | ❌ 절대 노출 0 | `http.route` + `http.status_code` |
| **tenant_id** | ✅ 노출 (필수) | `rosshield.tenant_id` (멀티테넌시 cross-tenant 분리, R11A-3) |

cross-tenant trace 노출 금지 — 모든 span 에 `rosshield.tenant_id` 필수, downstream backend 에서 tenant scope filter 권장.

---

## 8. 한계 + carryover

### 8.1 customer 위탁 영역

- **otel-collector 또는 backend 자체 설치** — Lodestar 측은 OTLP emit 만. Jaeger/Tempo/Datadog/NewRelic 등 backend 의 설치·운영·sizing 은 customer 결정 (★ 환경 의존).
- **log aggregation backend** — Loki/ELK/Splunk/Datadog Logs 중 customer 선택. Lodestar 는 slog JSON output 표준만 보장.
- **trace_id ↔ audit chain cross-reference UI** — Lodestar UI 에 trace_id 표시 별 epic. 본 release 는 slog log line 의 traceId field 까지.

### 8.2 미지원

- **vendor-specific APM SDK 직접 통합** (Datadog APM Go tracer · NewRelic Go agent · AppDynamics SDK 직접 import) — 거부. otel-collector 경유 권장.
- **자체 trace backend** — Jaeger/Tempo customer 위탁.
- **profiling (pprof flame graph)** — 별 epic. distributed tracing 과 직교.
- **audit chain → trace 통합** — audit 은 별 source of truth 유지 (R11A-4).

### 8.3 carryover (v0.14.0 신규)

- **testcontainers e2e Jaeger smoke** — CI docker pull 비용 회피. 본 release 는 in-memory `tracetest.SpanRecorder` 기반 단위 test 만. e2e 는 별 epic.
- **otel metric exporter plug** — D-P11A-5 권장 default 의 양쪽 emit 중 Prometheus federation 만 결선. otel metric exporter (`prometheusexporter` 가 아닌 `otlpmetricgrpc`) 는 별 round.
- **Grafana dashboard panel (trace volume + sampling effective rate)** — 별 dashboard 통합 epic.

### 8.4 cross-reference

- Phase 11 옵션 A 완전 마감 → Phase 11 Top 3 (B SOC2 + C audit key_epoch + A otel) 모두 마감.
- 다음 Phase 12 backlog draft 또는 enterprise plugin 잔여 (fleetxval · rostopo · selectdisclose) 진입 가능.

---

## 9. 참조

- [opentelemetry-tracing-design.md](../design/notes/opentelemetry-tracing-design.md) — Phase 11.A design (12 섹션 + D-P11A-1~5 결정 항목 + Stage 11.A-2~7 분해).
- [otel-collector-setup.md](../onboarding/otel-collector-setup.md) — customer 측 otel-collector + backend 설치 가이드.
- [docs/releases/v0.14.0.md](../releases/v0.14.0.md) — Phase 11.A 마감 release notes.
- OpenTelemetry CNCF spec — https://opentelemetry.io
- W3C trace context — https://www.w3.org/TR/trace-context/
