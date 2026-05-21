# Prometheus alert rules + alertmanager — rosshield multi-region

본 디렉토리는 Lodestar `/metrics` 엔드포인트(E27)를 scrape하는 Prometheus 환경에서
multi-region 운영자 알람을 자동화하기 위한 자산을 담습니다 (Phase 10 Stage 10.A-5).

- `alerts/multi-region.yml` — Prometheus alert rules (5 rules, group `rosshield-multi-region`)
- `alertmanager-sample.yml` — Alertmanager routing/receiver sample
- 본 `README.md` — import·검증·운영 가이드

운영 docs 일관성: [`deploy/grafana/README.md`](../grafana/README.md),
[`docs/operations/multi-region-failover-runbook.md`](../../docs/operations/multi-region-failover-runbook.md) §13.

---

## 1. 사전 준비

| 항목 | 최소 버전 | 비고 |
|---|---|---|
| Prometheus | 2.45+ | `promtool` 동봉 — rule syntax 검증용 |
| Alertmanager | 0.26+ | `amtool` 동봉 — config syntax 검증용 |
| rosshield-server | E27 포함 (`--metrics-addr` flag) | `deploy/grafana/README.md` §1.1 참조 |

multi-region rule은 Phase 8 MR.T8 (`rosshield_replication_lag_seconds`) +
Phase 9 E25 (`rosshield_ha_*`) + E27 (`rosshield_audit_chain_head_seq`) 메트릭을
활용합니다 — 모두 5a55da5 시점에 결선.

---

## 2. Alert rule import

### 2.1 prometheus.yml 갱신

```yaml
# prometheus.yml 일부
rule_files:
  - /etc/prometheus/alerts/*.yml

alerting:
  alertmanagers:
    - static_configs:
        - targets: ['alertmanager:9093']
```

본 디렉토리를 `/etc/prometheus/alerts/`로 복사 (또는 mount):

```bash
sudo mkdir -p /etc/prometheus/alerts
sudo cp deploy/prometheus/alerts/multi-region.yml /etc/prometheus/alerts/
```

### 2.2 Rule syntax 검증

```bash
promtool check rules deploy/prometheus/alerts/multi-region.yml
# Expected:
#   Checking deploy/prometheus/alerts/multi-region.yml
#     SUCCESS: 5 rules found
```

### 2.3 Prometheus reload

```bash
curl -X POST http://prometheus.internal:9090/-/reload
# 또는
systemctl reload prometheus
```

Prometheus UI → Alerts 탭에서 5개 rule이 inactive/firing 상태로 표시되면 정상.

---

## 3. Alertmanager 설정

### 3.1 Sample copy

```bash
cp deploy/prometheus/alertmanager-sample.yml /etc/alertmanager/alertmanager.yml
```

`REPLACE_WITH_*` 토큰을 customer 환경 값으로 교체:
- `REPLACE_WITH_PAGERDUTY_SERVICE_KEY` — PagerDuty Integration Key
- `https://hooks.slack.com/services/REPLACE/WITH/WEBHOOK` — Slack Incoming Webhook URL
- `https://webhook.example.com/lodestar/alerts` — 자체 webhook receiver (선택)

### 3.2 Config syntax 검증

```bash
amtool check-config /etc/alertmanager/alertmanager.yml
# Expected: Checking '/etc/alertmanager/alertmanager.yml'  SUCCESS
```

### 3.3 Routing 테스트 (firing alert 없이)

```bash
amtool config routes test \
  --config.file=/etc/alertmanager/alertmanager.yml \
  service=rosshield severity=critical component=replication
# Expected: rosshield-critical receiver로 routing
```

### 3.4 첫 firing 검증

배포 직후 정상 동작은 inactive (메트릭 임계 미달). 임계 강제 검증은:

```bash
# Prometheus 임시 rule로 always-fire 추가 → reload → Slack/PagerDuty 수신 확인 → rule 제거
```

또는 staging에서 secondary PG의 subscription을 stop하여 lag을 인위적으로 60s 이상
증가시켜 검증 (drill 절차는 runbook §1.3 참조).

---

## 4. Alert rule 매트릭스

| Alert name | 트리거 | severity | runbook |
|---|---|---|---|
| `RosshieldReplicationLagWarning` | `replication_lag_seconds > 30` for 2m | warning | §13.1 |
| `RosshieldReplicationLagCritical` | `replication_lag_seconds > 60` for 1m | critical | §13.2 |
| `RosshieldAuditChainHeadSeqMismatch` | tenant별 head seq cross-instance divergence > 0 for 5m | critical | §13.3 |
| `RosshieldHARoleSwap` | `rate(ha_failover_total[5m]) > 0` | info | §13.4 |
| `RosshieldHAFailoverStorm` | `increase(ha_failover_total[1h]) >= 3` | critical | §13.5 |

각 rule에 `runbook_url` annotation이 포함되어 있어 Alertmanager · PagerDuty notification에
runbook 링크가 자동 전파됩니다.

---

## 5. Grafana dashboard 연계

[`deploy/grafana/rosshield-dashboard.json`](../grafana/rosshield-dashboard.json) HA row(8행)에서
`rosshield_ha_role` · `rosshield_ha_leader_epoch` · `rosshield_ha_failover_total` panel을
이미 노출합니다. multi-region replication lag panel은 후속 dashboard 갱신에서 추가 권장
(carryover — Stage 10.A-6 또는 별 epic).

---

## 6. 한계

| 한계 | 상태 |
|---|---|
| `rosshield_audit_chain_head_seq` 만 노출 (head sha 직접 비교 metric 부재) | seq divergence를 head sha mismatch proxy로 활용. seq 일치 + sha 불일치 케이스(이론상 replication corruption)는 본 alert로 잡지 못함 — Stage 10.A-6 또는 별 epic에서 background collector + `rosshield_audit_chain_head_sha_match` gauge 추가 권장. |
| Alertmanager → Lodestar 자체 webhook endpoint | Lodestar는 inbound alertmanager webhook 수신 endpoint 미보유. customer 환경별로 webhook 수신처를 직접 결선 (Slack/PagerDuty/자체 ticketing). 본 sample은 outbound 패턴만 cover. |
| Multi-cluster scrape label | 본 rule은 `instance` label 기준 비교. cross-region 환경에서 `cluster` 또는 `region` label을 추가 사용한다면 PromQL 일부 조정 필요. |

---

## 참고

- 메트릭 정의: [`internal/platform/metrics/metrics.go`](../../internal/platform/metrics/metrics.go)
- Replication lag collector: [`internal/platform/replication/lagmetric/collector.go`](../../internal/platform/replication/lagmetric/collector.go)
- Phase 10 design doc: [`docs/design/notes/phase10-backlog-design.md`](../../docs/design/notes/phase10-backlog-design.md) §6.5
- Prometheus alerting docs: https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/
- Alertmanager docs: https://prometheus.io/docs/alerting/latest/configuration/
