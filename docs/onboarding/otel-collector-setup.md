# OpenTelemetry collector + backend setup (customer onboarding)

> **상태**: GA — Phase 11.A v0.14.0 마감 자산 기준. customer 측 otel-collector + backend(Jaeger/Tempo/Datadog/NewRelic) 설치 가이드.
> **대상**: customer 운영자 (Lodestar v0.14.0+ 도입 + 분산 trace backend 사전 준비).
> **참조**: `docs/operations/opentelemetry-setup.md` (Lodestar 측 활성 절차), `docs/design/notes/opentelemetry-tracing-design.md`.

본 문서는 Lodestar 가 emit 하는 OTLP trace 를 흡수하기 위한 **customer 환경 측 otel-collector + backend stack** 의 설치·구성 예제를 정리합니다. Lodestar 측 활성은 `docs/operations/opentelemetry-setup.md` 를 참조하세요.

> **핵심 원칙**: Lodestar 측은 vendor-neutral OTLP emit 만. backend (Jaeger/Tempo/Datadog 등) 의 선택·운영·sizing 은 customer 결정. otel-collector 가 단일 buffering 지점 — backend 교체 시 Lodestar 재시작 0 (collector config 만 갱신).

---

## 1. 사전 요구사항

- Docker Engine 24.0+ 또는 Kubernetes 1.28+.
- Lodestar v0.14.0+ binary 또는 snap.
- 네트워크 — Lodestar host → otel-collector 의 4317 (gRPC) 또는 4318 (HTTP) 접근 가능.
- (Datadog/NewRelic 등 SaaS) — API key 발급 + outbound HTTPS 접근.

---

## 2. otel-collector + Jaeger (docker-compose)

### 2.1 stack 개요

```
Lodestar  --OTLP 4317-->  otel-collector  --OTLP 4317-->  Jaeger all-in-one
                              |                              |
                              +--Prometheus 8889 ----------> (선택) Prometheus
                                                             |
                              UI 16686 -----------------> 운영자 browser
```

### 2.2 docker-compose.yml

```yaml
# docker-compose.yml
version: "3.9"

services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.115.0
    container_name: otel-collector
    command: ["--config=/etc/otelcol/config.yaml"]
    volumes:
      - ./otel-collector-config.yaml:/etc/otelcol/config.yaml:ro
    ports:
      - "4317:4317"   # OTLP gRPC (Lodestar 측)
      - "4318:4318"   # OTLP HTTP (Lodestar 측 alternative)
      - "8889:8889"   # Prometheus metrics (collector 자체 telemetry)
    depends_on:
      - jaeger

  jaeger:
    image: jaegertracing/all-in-one:1.62
    container_name: jaeger
    environment:
      - COLLECTOR_OTLP_ENABLED=true
    ports:
      - "16686:16686"  # Jaeger UI
      - "14250:14250"  # Jaeger model.proto (internal)
```

### 2.3 otel-collector-config.yaml

```yaml
# otel-collector-config.yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:
    timeout: 5s
    send_batch_size: 1024
  memory_limiter:
    check_interval: 1s
    limit_mib: 512
    spike_limit_mib: 128
  # (선택) tenant_id 누락 span drop — multi-tenant 환경 안전.
  filter/require_tenant:
    error_mode: ignore
    traces:
      span:
        - 'attributes["rosshield.tenant_id"] == nil'

exporters:
  otlp/jaeger:
    endpoint: jaeger:4317
    tls:
      insecure: true
  debug:
    verbosity: basic

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlp/jaeger, debug]
  telemetry:
    metrics:
      address: 0.0.0.0:8889
```

### 2.4 활성

```bash
docker compose up -d
# otel-collector + jaeger 두 컨테이너 기동.

# Lodestar 측 활성.
./rosshield-server \
  --otel-enabled=true \
  --otel-endpoint=localhost:4317 \
  --otel-insecure=true

# 검증 — HTTP 요청 1건 emit 후 Jaeger UI 에서 확인.
curl http://localhost:8080/api/v1/healthz
open http://localhost:16686    # Service: rosshield 선택 → Find Traces
```

---

## 3. otel-collector + Tempo + Grafana stack

Grafana 와 Loki 가 이미 운영 중인 customer 권장. trace_id 클릭 → Tempo trace view 자연 점프.

### 3.1 stack 개요

```
Lodestar  --OTLP 4317-->  otel-collector  --OTLP 4317-->  Tempo
                                                            |
slog log  --promtail--->  Loki  <----- Grafana datasource --+
                                          (correlation 자동 점프)
```

### 3.2 docker-compose.yml

```yaml
# docker-compose.yml
version: "3.9"

services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.115.0
    volumes:
      - ./otel-collector-config.yaml:/etc/otelcol/config.yaml:ro
    command: ["--config=/etc/otelcol/config.yaml"]
    ports:
      - "4317:4317"
      - "4318:4318"
    depends_on: [tempo]

  tempo:
    image: grafana/tempo:2.6.0
    command: ["-config.file=/etc/tempo/tempo.yaml"]
    volumes:
      - ./tempo.yaml:/etc/tempo/tempo.yaml:ro
      - tempo-data:/var/tempo
    ports:
      - "3200:3200"   # Tempo HTTP API
      - "4327:4317"   # OTLP gRPC (internal)

  loki:
    image: grafana/loki:3.2.0
    command: ["-config.file=/etc/loki/local-config.yaml"]
    ports: ["3100:3100"]

  promtail:
    image: grafana/promtail:3.2.0
    volumes:
      - /var/log/rosshield:/var/log/rosshield:ro
      - ./promtail-config.yaml:/etc/promtail/config.yaml:ro
    command: ["-config.file=/etc/promtail/config.yaml"]
    depends_on: [loki]

  grafana:
    image: grafana/grafana:11.3.0
    ports: ["3000:3000"]
    environment:
      - GF_AUTH_ANONYMOUS_ENABLED=true
      - GF_AUTH_ANONYMOUS_ORG_ROLE=Admin
    volumes:
      - ./grafana-datasources.yaml:/etc/grafana/provisioning/datasources/datasources.yaml:ro
    depends_on: [tempo, loki]

volumes:
  tempo-data: {}
```

### 3.3 tempo.yaml

```yaml
# tempo.yaml — Tempo single-binary.
server:
  http_listen_port: 3200

distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317

ingester:
  trace_idle_period: 10s
  max_block_duration: 5m

storage:
  trace:
    backend: local
    local:
      path: /var/tempo/blocks
    wal:
      path: /var/tempo/wal
```

### 3.4 grafana-datasources.yaml (correlation 자동)

```yaml
# grafana-datasources.yaml — Loki ↔ Tempo trace_id 자동 점프.
apiVersion: 1

datasources:
  - name: Tempo
    type: tempo
    uid: tempo
    url: http://tempo:3200
    jsonData:
      tracesToLogsV2:
        datasourceUid: loki
        spanStartTimeShift: -1m
        spanEndTimeShift: 1m
        tags: ["rosshield.tenant_id"]
        filterByTraceID: true
  - name: Loki
    type: loki
    uid: loki
    url: http://loki:3100
    jsonData:
      derivedFields:
        - datasourceUid: tempo
          matcherRegex: '"traceId":"(\w+)"'
          name: TraceID
          url: '${__value.raw}'
```

### 3.5 활성

```bash
docker compose up -d

./rosshield-server \
  --otel-enabled=true \
  --otel-endpoint=localhost:4317 \
  --otel-insecure=true \
  > /var/log/rosshield/server.log 2>&1

open http://localhost:3000  # Grafana UI — Explore → Loki → trace_id 클릭 → Tempo 점프
```

---

## 4. otel-collector + Datadog APM

SaaS Datadog APM customer 권장. otel-collector 의 `datadog` exporter 경유 — Lodestar 는 Datadog SDK 직접 의존 0 (vendor lock-in 회피).

### 4.1 otel-collector-config.yaml

```yaml
# otel-collector-config.yaml — Datadog exporter.
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:
    timeout: 5s

exporters:
  datadog:
    api:
      site: datadoghq.com           # 또는 datadoghq.eu / us3.datadoghq.com 등
      key: ${DD_API_KEY}            # env 또는 secret 주입
    traces:
      compute_stats_by_span_kind: true

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [datadog]
```

### 4.2 docker-compose 예

```yaml
services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.115.0
    environment:
      - DD_API_KEY=${DD_API_KEY}
    volumes:
      - ./otel-collector-config.yaml:/etc/otelcol/config.yaml:ro
    command: ["--config=/etc/otelcol/config.yaml"]
    ports: ["4317:4317", "4318:4318"]
```

```bash
export DD_API_KEY=<your-datadog-api-key>
docker compose up -d
./rosshield-server --otel-enabled=true --otel-endpoint=localhost:4317 --otel-insecure=true
```

Datadog APM 대시보드에서 `service:rosshield` 선택 → trace 자동 노출.

---

## 5. otel-collector + NewRelic

```yaml
# otel-collector-config.yaml — NewRelic OTLP endpoint.
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:
    timeout: 5s

exporters:
  otlp/newrelic:
    endpoint: otlp.nr-data.net:4317
    headers:
      api-key: ${NEW_RELIC_LICENSE_KEY}

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp/newrelic]
```

```bash
export NEW_RELIC_LICENSE_KEY=<your-license-key>
docker compose up -d
./rosshield-server --otel-enabled=true --otel-endpoint=localhost:4317 --otel-insecure=true
```

---

## 6. Snap deployment 활성

Lodestar snap (canonical channel `rosshield-server`) 도 동일 flag/env 패턴.

### 6.1 snap config

```bash
# snap config 로 env 주입 (snap config 권장 패턴).
sudo snap set rosshield-server \
  otel.enabled=true \
  otel.endpoint=otel-collector.observability.svc:4317 \
  otel.exporter=grpc \
  otel.sampling=0.05 \
  otel.insecure=false

# snap 재시작.
sudo snap restart rosshield-server

# 또는 env 방식 (snap connect 후):
sudo snap set rosshield-server env.ROSSHIELD_OTEL_ENABLED=true
sudo snap set rosshield-server env.ROSSHIELD_OTEL_ENDPOINT=otel-collector.svc:4317
```

> snap config 의 dot notation 은 internal env mapping — Lodestar snap wrapper 가 `ROSSHIELD_OTEL_*` env 로 자동 변환 (Phase 7 snap wiring).

### 6.2 air-gap snap customer

air-gap 환경에 자체 otel-collector 가 있으면 동일 활성. backend 가 없으면 `--otel-enabled=false` (default) 유지 — noop tracer, overhead 0.

---

## 7. Kubernetes sidecar 패턴

Lodestar pod 안에 otel-collector sidecar 1개를 함께 배치하는 패턴. cluster-wide collector deployment 가 없는 small cluster 또는 multi-tenant 격리가 필요한 환경 권장.

### 7.1 Deployment 예

```yaml
# rosshield-with-otel.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rosshield-server
  namespace: rosshield
spec:
  replicas: 2
  selector:
    matchLabels: { app: rosshield-server }
  template:
    metadata:
      labels: { app: rosshield-server }
    spec:
      containers:
        - name: rosshield-server
          image: ghcr.io/ssabro/rosshield-server:v0.14.0
          env:
            - name: ROSSHIELD_OTEL_ENABLED
              value: "true"
            - name: ROSSHIELD_OTEL_ENDPOINT
              value: "localhost:4317"          # sidecar
            - name: ROSSHIELD_OTEL_INSECURE
              value: "true"                    # localhost 내부 망
            - name: ROSSHIELD_OTEL_SAMPLING
              value: "0.05"
          ports:
            - containerPort: 8080
              name: http
        - name: otel-collector
          image: otel/opentelemetry-collector-contrib:0.115.0
          args: ["--config=/etc/otelcol/config.yaml"]
          volumeMounts:
            - name: collector-config
              mountPath: /etc/otelcol
              readOnly: true
          ports:
            - containerPort: 4317
              name: otlp-grpc
      volumes:
        - name: collector-config
          configMap:
            name: rosshield-otel-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: rosshield-otel-config
  namespace: rosshield
data:
  config.yaml: |
    receivers:
      otlp:
        protocols:
          grpc:
            endpoint: 0.0.0.0:4317
    processors:
      batch: { timeout: 5s }
    exporters:
      otlp/tempo:
        endpoint: tempo.observability.svc.cluster.local:4317
        tls: { insecure: true }
    service:
      pipelines:
        traces:
          receivers: [otlp]
          processors: [batch]
          exporters: [otlp/tempo]
```

### 7.2 cluster-wide collector (DaemonSet) 대안

대규모 cluster 는 DaemonSet 으로 노드당 1개 collector 운영 권장 (resource overhead 최소화):

```yaml
# DaemonSet pattern — 각 노드에 collector 1개. pod 는 host IP 로 통신.
env:
  - name: ROSSHIELD_OTEL_ENDPOINT
    value: "$(HOST_IP):4317"
  - name: HOST_IP
    valueFrom:
      fieldRef:
        fieldPath: status.hostIP
```

---

## 8. tail-based sampling 고도화 (선택)

high-fidelity + cost-controlled — Lodestar 측 `--otel-sampling=1.0` + collector 단 tail-based 결합.

```yaml
# otel-collector-config.yaml — tail-based sampling 정책.
processors:
  tail_sampling:
    decision_wait: 10s
    num_traces: 100000
    expected_new_traces_per_sec: 100
    policies:
      # 모든 error span 100% sampling.
      - name: errors
        type: status_code
        status_code: { status_codes: [ERROR] }
      # 5s 이상 slow trace 100% sampling.
      - name: slow
        type: latency
        latency: { threshold_ms: 5000 }
      # advisor LLM call 100% sampling (cost analysis).
      - name: llm
        type: string_attribute
        string_attribute:
          key: llm.provider
          values: [anthropic, ollama, vllm]
      # baseline 5%.
      - name: baseline
        type: probabilistic
        probabilistic: { sampling_percentage: 5 }

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [tail_sampling, batch]
      exporters: [otlp/tempo]
```

Lodestar 측은 `--otel-sampling=1.0` 으로 모든 span emit + collector 단에서 정책 분기.

---

## 9. 검증 체크리스트

활성 후 1차 검증 (Lodestar v0.14.0):

```bash
# 1. Lodestar 측 — flag/env 활성 확인.
./rosshield-server --otel-enabled=true --otel-endpoint=... &
# stdout 에 "otel: tracer provider enabled (endpoint=...)" 로그 1회 출력.

# 2. HTTP root span 검증.
curl http://localhost:8080/api/v1/healthz
# backend UI 에서 service.name=rosshield · span name="HTTP GET /api/v1/healthz" 1건.

# 3. scan flow 5 span 검증.
# /api/v1/scans POST 1회 → backend 에서 trace tree 5 span 확인:
# scan.run → ssh.connect / check.exec / check.evaluate / evidence.write / scan.publish

# 4. multi-region trace_id 검증 (multi-region 배포 시).
# primary 의 outgoing patroni 호출 + standby 의 incoming HTTP 가 동일 trace_id 로 묶임.

# 5. log ↔ trace cross-reference (Grafana + Loki + Tempo 사용 시).
# Loki query: {service="rosshield"} | json
# log line 의 traceId field 클릭 → Tempo trace view 자동 점프.

# 6. PII 회피 확인 (security check).
# span attribute 에 prompt content · response content · ssh credential · tool args
# 가 노출되지 않는지 검증. llm.tokens.input / llm.tokens.output (size only) 만 노출.
```

---

## 10. 한계 + cross-reference

- **Lodestar 측 책임 범위**: OTLP emit + propagation + PII 회피 정책까지.
- **customer 측 책임 범위**: otel-collector + backend 설치 · 운영 · sizing · backup · retention.
- **vendor-specific APM 직접 통합**: 거부. otel-collector 경유만 (lock-in 회피).
- **Phase 11 옵션 A 마감** = Lodestar v0.14.0 baseline. 후속 enterprise customer 의 backend 별 fine-tuning 은 별 round.

---

## 11. 참조

- [opentelemetry-setup.md](../operations/opentelemetry-setup.md) — Lodestar 측 활성 절차 (operator 가이드).
- [opentelemetry-tracing-design.md](../design/notes/opentelemetry-tracing-design.md) — Phase 11.A design (12 섹션).
- [docs/releases/v0.14.0.md](../releases/v0.14.0.md) — Phase 11.A 마감 release.
- OpenTelemetry Collector docs — https://opentelemetry.io/docs/collector/
- Jaeger docs — https://www.jaegertracing.io/docs/
- Grafana Tempo docs — https://grafana.com/docs/tempo/
- Datadog OTLP ingestion — https://docs.datadoghq.com/opentelemetry/
- NewRelic OTLP ingestion — https://docs.newrelic.com/docs/more-integrations/open-source-telemetry-integrations/opentelemetry/
