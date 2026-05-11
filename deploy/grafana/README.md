# Grafana dashboard — rosshield operational view

본 디렉토리는 rosshield `/metrics` 엔드포인트(E27 Phase 4)를 Prometheus로 scrape하여 Grafana로 시각화하기 위한 자산을 담습니다.

- `rosshield-dashboard.json` — Grafana 11.x 호환 dashboard export (5 row · 12 panel)
- 본 `README.md` — 운영자 setup·import·alerting·트러블슈팅 가이드

운영 docs 일관성: [`docs/operations/snap-deployment.md`](../../docs/operations/snap-deployment.md), [`docs/operations/ha-deployment.md`](../../docs/operations/ha-deployment.md).

---

## 1. 사전 준비

| 항목 | 최소 버전 | 비고 |
|---|---|---|
| Prometheus | 2.45+ | 본 dashboard PromQL은 `clamp_min`, `increase`, `rate` 등 표준 함수만 사용 |
| Grafana | 11.x | `schemaVersion: 39` 기준 export. 10.x 이하에서는 일부 panel 옵션 무시될 수 있음 |
| rosshield-server | E27 포함 | `--metrics-addr` CLI flag 지원 빌드 |

### 1.1 /metrics 활성화

**rosshield는 기본적으로 metrics endpoint를 노출하지 않습니다.** 옵트인이 원칙입니다. 부팅 시 `--metrics-addr` 플래그를 명시해야 mount됩니다.

```bash
# 로컬 노출 (권장 — Prometheus가 같은 host에서 scrape)
rosshield-server --metrics-addr 127.0.0.1:9090

# 클러스터 노출 (전용 NIC·방화벽 룰 전제)
rosshield-server --metrics-addr 0.0.0.0:9090
```

응답 확인:

```bash
curl http://localhost:9090/metrics | head -30
# 출력 예:
# # HELP rosshield_scan_started_total Number of scan sessions started, partitioned by tenant.
# # TYPE rosshield_scan_started_total counter
# rosshield_scan_started_total{tenant="tn_T"} 1
# ...
```

빈 응답·404가 나오면 [§7 트러블슈팅](#7-트러블슈팅) 참조.

---

## 2. Prometheus scrape config

`prometheus.yml`에 다음 job을 추가합니다.

```yaml
# prometheus.yml 일부
scrape_configs:
  - job_name: rosshield
    static_configs:
      - targets: ['rosshield-server.example.internal:9090']
    scrape_interval: 30s
    metrics_path: /metrics
```

### 2.1 HA 환경 (leader + follower)

[`ha-deployment.md`](../../docs/operations/ha-deployment.md) 구성에서는 leader·follower **둘 다** `/metrics`를 노출하며, 둘 다 scrape해야 합니다. 추후 도입될 `rosshield_ha_role` gauge(0=follower, 1=leader)로 dashboard에서 구분합니다.

```yaml
  - job_name: rosshield
    static_configs:
      - targets: ['rosshield-a:9090', 'rosshield-b:9090']
        labels:
          cluster: 'production-1'
    scrape_interval: 30s
    metrics_path: /metrics
```

> Phase 5 E25 stage 4+에서 `rosshield_ha_role` 등 HA 메트릭이 추가되기 전까지는 follower도 leader와 동일한 도메인 counter를 노출합니다. 도메인 트래픽은 leader에서만 처리되므로 follower의 counter는 0이 일반적입니다.

### 2.2 reload

```bash
curl -X POST http://prometheus.internal:9090/-/reload
# 또는
systemctl reload prometheus
```

`up{job="rosshield"}` 시리즈가 1이면 scrape 정상.

---

## 3. Grafana datasource 등록

1. Grafana 좌측 메뉴 → **Connections** → **Data sources** → **Add new data source**
2. **Prometheus** 선택
3. 설정:
   - **Name**: `Prometheus` (또는 기관 표준)
   - **URL**: `http://prometheus.internal:9090`
   - **Access**: Server (default)
4. **Save & test** → "Successfully queried the Prometheus API"

> 본 dashboard JSON은 `${DS_PROMETHEUS}` 변수로 datasource를 참조하므로, import 단계에서 어느 Prometheus를 쓸지 선택할 수 있습니다.

---

## 4. Dashboard import

1. Grafana 좌측 메뉴 → **Dashboards** → **New** → **Import**
2. 다음 중 하나:
   - **Upload JSON file**: `deploy/grafana/rosshield-dashboard.json`
   - **Paste**: 파일 내용을 텍스트박스에 붙여넣기
3. **Datasource** 드롭다운에서 위에서 등록한 Prometheus 선택
4. **Import** 클릭

URL: `https://grafana.example.internal/d/rosshield-ops/rosshield-operational-dashboard`

### 4.1 다중 환경 (staging/production)

`uid: rosshield-ops`는 Grafana 내에서 unique 해야 합니다. 같은 Grafana에 staging·production을 둘 다 import하려면 JSON 사본을 만들고 `uid`/`title`을 변경하거나, Grafana Folder를 환경별로 분리하세요.

---

## 5. Dashboard 구조

총 5 row · 12 panel. 메트릭은 모두 E27에서 노출하는 6종을 활용합니다.

| Row | Panel | Type | 데이터 소스 (PromQL 요지) | 용도 |
|---|---|---|---|---|
| **Summary (24h)** | Scans started (24h) | stat | `sum(increase(rosshield_scan_started_total[24h]))` | 일간 스캔량 |
| | Webhook delivery success rate (24h) | stat | `success / clamp_min(total, 1)` | 외부 연동 건강도 (alert 후보) |
| | Audit chain head (sum) | stat | `sum(rosshield_audit_chain_head_seq)` | 감사 시퀀스 진행 |
| | Invitations accepted (24h) | stat | `sum(increase(rosshield_invitation_accepted_total[24h]))` | onboarding 진행 |
| **Throughput** | Scan rate by tenant | timeseries | `sum by (tenant) (rate(...[5m]))` | tenant별 추이 |
| | Webhook delivery rate by status | timeseries | `sum by (status) (rate(...[5m]))` | success/failed/dead 분리 |
| | Invitation flow — sent vs accepted | timeseries | sent·accepted 2 시리즈 overlay | 초대 funnel |
| **Latency** | EventBus publish latency (heatmap) | heatmap | `sum by (le) (rate(..._bucket[5m]))` | 백프레셔·느린 subscriber 탐지 |
| **Audit chain** | Audit chain head — per tenant | table | `rosshield_audit_chain_head_seq` (instant, table format) | tenant별 head seq, 정체 식별 |
| **HA status (placeholder)** | HA role (placeholder) | gauge | `rosshield_ha_role` (0/1) | **Phase 5 E25 stage 4+ 도입 후 활성** |
| | Leader epoch (placeholder) | stat | `max(rosshield_ha_leader_epoch)` | **동상** |
| | Failover count 24h (placeholder) | stat | `sum(increase(rosshield_ha_failover_total[24h]))` | **동상** — 3건 이상이면 P1 |

### 5.1 Tenant 변수

상단 **Tenant** 드롭다운은 `label_values(rosshield_scan_started_total, tenant)`로 자동 채워지며, Multi + All 옵션. 특정 tenant만 보고 싶을 때 사용하세요.

> 본 dashboard는 단일 인스턴스를 가정합니다. 멀티 클러스터 분리가 필요하면 §2.1과 같이 `cluster` label을 추가하고 dashboard에 동일한 templating variable을 추가하세요.

---

## 6. Alerting 권장

Grafana Alerting 또는 Alertmanager 규칙으로 다음 alert을 권장합니다. (rule 자체는 본 dashboard에 포함되지 않습니다 — Grafana Alert rules는 별도 관리.)

| 우선순위 | 조건 | PromQL 예 | 의미 |
|---|---|---|---|
| **P2** | 5분 연속 webhook 성공률 < 95% | `sum(rate(rosshield_webhook_deliveries_total{status="success"}[5m])) / clamp_min(sum(rate(rosshield_webhook_deliveries_total[5m])), 1) < 0.95` | 외부 수신자 장애·인증서 만료 등 |
| **P3** | audit chain head 1시간 정체 | `increase(rosshield_audit_chain_head_seq[1h]) == 0 and rosshield_audit_chain_head_seq > 0` | audit checkpoint job 정지 의심 |
| **P3** | event publish p99 > 1s | `histogram_quantile(0.99, sum by (le) (rate(rosshield_event_publish_duration_seconds_bucket[5m]))) > 1` | 느린 subscriber·backpressure |
| **P1** (E25 stage 4+ 후) | 1시간 내 failover ≥ 3 | `increase(rosshield_ha_failover_total[1h]) >= 3` | 클러스터 불안정 (네트워크·디스크) |

> Alerting rule은 환경별 SLA에 따라 임계치를 조정하세요. 위 값은 출발점입니다.

---

## 7. 트러블슈팅

| 증상 | 원인 | 해결 |
|---|---|---|
| `/metrics` 404 또는 connection refused | `--metrics-addr` 미설정 | 부팅 시 `--metrics-addr 127.0.0.1:9090` 추가 후 재기동 |
| Prometheus target `down` | 네트워크·방화벽·잘못된 host | `curl http://<host>:9090/metrics`로 직접 확인. 방화벽 룰 점검 |
| Dashboard panel "No data" | Prometheus scrape 미설정 또는 datasource UID mismatch | (1) Prometheus에서 직접 PromQL 실행 → 시리즈 존재 확인, (2) Grafana datasource가 올바른 Prometheus를 가리키는지 확인 |
| Tenant 드롭다운이 비어 있음 | 아직 scan이 시작된 적이 없거나, label cardinality 정책에 의해 누락 | 임의 tenant로 `/v1/scans` 한 번 실행 → 30초 후 새로고침 |
| HA panel 빈 값 | E25 stage 4+ HA 메트릭 미구현 | 정상 동작. "HA status (placeholder)" row는 후속 stage에서 자동 채워집니다 |
| 시리즈 이름 mismatch (`rosshield_invitations_*` 등) | 구버전 빌드 또는 잘못된 외부 분석 자료 | E27 기준 정확한 이름은 `rosshield_invitation_sent_total`·`rosshield_invitation_accepted_total` (subsystem `invitation`, 단수형). `internal/platform/metrics/metrics.go` 참조 |
| Webhook status label에 예상치 못한 값 | 정상. `success`·`failed`·`dead` 3종이 정의됨 (dead = retry 소진 후 dead-letter 이동) | dashboard panel은 3 값 모두에 색을 매핑. 그 외 값이 있다면 어플리케이션 버그 또는 추후 추가 |

### 7.1 디버깅 명령

```bash
# 시리즈가 노출되는지 직접 확인
curl -s http://localhost:9090/metrics | grep '^rosshield_'

# Prometheus 측에서 시리즈 존재 확인
curl -s 'http://prometheus.internal:9090/api/v1/series?match[]=rosshield_scan_started_total'

# tenant label 값 확인
curl -s 'http://prometheus.internal:9090/api/v1/label/tenant/values'
```

---

## 8. 한계 (Phase 5 1차)

본 dashboard는 **현재 E27이 노출하는 6종 메트릭만 활용**합니다. 다음은 후속 epic으로 분리되어 있으며, 도입되면 본 dashboard에 panel을 추가하면 됩니다.

| 한계 | 분류 | 추적 |
|---|---|---|
| HA 메트릭 (`rosshield_ha_role`/`leader_epoch`/`failover_total`) 미구현 | Phase 5 E25 stage 4+ | placeholder panel 3개로 자리만 잡아둠 |
| 라이선스 사용량 메트릭 (`rosshield_license_seats_used` 등) | 별 epic (Phase 5 후반) | 미정 |
| Per-handler HTTP latency (`rosshield_http_request_duration_seconds`) | 별 epic | 미정 |
| Per-tenant rate-limit reject 카운터 | 별 epic | 미정 |
| Distributed tracing (OTel exporter) | Phase 6+ | 본 dashboard 범위 밖 — Grafana Tempo·Jaeger 별도 |

후속 메트릭이 추가되면 dashboard JSON에 panel을 추가하고 `schemaVersion`·`version`을 올리세요.

---

## 참고

- 메트릭 정의 코드: [`internal/platform/metrics/metrics.go`](../../internal/platform/metrics/metrics.go)
- 메트릭 노출 검증 테스트: [`internal/platform/metrics/metrics_test.go`](../../internal/platform/metrics/metrics_test.go)
- E27 epic 결정 기록: `SESSION_HANDOFF.md`
- Prometheus best practice: https://prometheus.io/docs/practices/naming/
- Grafana dashboard JSON 구조: https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/view-dashboard-json-model/
