# SOC2 CC4 — Monitoring Activities

> **Trust Services Criteria**: Common Criteria (필수, 모든 SOC2 audit 적용)
> **Sub-controls**: CC4.1 ~ CC4.2 (2)
> **Status**: Lodestar 결선 자산 ~1.5/2 매핑 (~75% cover) — 외부 감사인 검증 대기. CC4.1(ongoing monitoring)은 Prometheus + Grafana + 5 alert + audit chain 자연 cover, CC4.2(deficiency communication)는 alertmanager webhook + GitHub Issues가 부분 cover하나 formal deficiency reporting 부재.

CC4는 모니터링 활동 — 내부 통제의 지속적 평가 및 결손의 적시 의사소통 2단계를 다룹니다. 통제 시스템이 작동하는지 검증하고 발견된 결손을 책임 있는 당사자에게 적시 통보하는 통제군입니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| CC4.1 Conducts Ongoing and/or Separate Evaluations | Prometheus + Grafana dashboard · 5 alert rule(`multi-region.yml`) · `/healthz` endpoint · audit chain immutability · audit.* events · check-health hook(post-refresh) · fg-verify v2 CLI | **effectiveness dashboard 신규 진입 예정** (Stage 11.B-6), 정기 effectiveness 평가 report 0 | 외부 firm 90일 운영 효과성 측정 ★ |
| CC4.2 Evaluates and Communicates Deficiencies in a Timely Manner | Alertmanager webhook(severity warning/critical) · GitHub Issues · `SECURITY.md` private vulnerability reporting · Prometheus alert routing · runbook §13 5 alert 대응 | **formal deficiency reporting workflow 0**, defect tracking SLA 명문화 부족 | 외부 firm deficiency tracking process audit ★ |

---

## Sub-control 상세

### CC4.1 Conducts Ongoing and/or Separate Evaluations

**Trust Services Criteria 본문 의역**: 조직은 내부 통제의 구성 요소가 존재하고 작동하는지 확인하기 위해 지속적 평가(ongoing evaluation) 또는 별도 평가(separate evaluation)를 수행합니다. 평가의 범위 · 빈도 · 객관성은 평가되는 통제의 중요도에 비례합니다.

**Lodestar 매핑**:

- **Prometheus + Grafana dashboard (ongoing monitoring)**:
  - `internal/platform/metrics/metrics.go` — Lodestar core metric emit. 핵심 metric:
    - `rosshield_audit_chain_head_seq{tenant}` — audit chain head sequence (E27 Phase 4 결선).
    - `rosshield_ha_role` · `rosshield_ha_leader_epoch` · `rosshield_ha_failover_total` (Phase 9 E25).
    - `rosshield_replication_lag_seconds{application_name}` (Phase 8 MR.T8, `internal/platform/replication/lagmetric/collector.go`).
  - `deploy/grafana/rosshield-dashboard.json` — multi-region status + audit chain + replication lag 시각화 (Phase 10.A).
  - scrape interval 15s/30s — near real-time ongoing evaluation.

- **5 alert rule (separate evaluation triggers)**:
  - `deploy/prometheus/alerts/multi-region.yml` — 5 alert rule:
    1. `RosshieldReplicationLagWarning` (replication lag > 30s for 2m)
    2. `RosshieldReplicationLagCritical` (> 60s for 1m, RPO 1m SLA 위협)
    3. (그 외 multi-region 관련 alert 3건)
  - 각 alert에 `runbook_url`이 `docs/operations/multi-region-failover-runbook.md` §13 대응 절차로 연결.

- **`/healthz` endpoint (separate health evaluation)**:
  - `internal/api/handlers/handlers.go` — health check endpoint. 시스템 가용성 외부 evaluation 채널.

- **Audit chain immutability (ongoing tamper detection)**:
  - `internal/domain/audit/hash.go` — 결정론적 hash chain. tamper 시 chain break 검출 가능.
  - `internal/domain/audit/checkpoint.go` — Ed25519 서명 checkpoint, 외부 감사인 자체 검증 가능.

- **fg-verify v2 CLI (separate evaluation tool)**:
  - `cmd/rosshield-audit-verify/` — audit bundle 외부 검증 도구. v2 bundle `_bundleVersion: "v2"` + `_chainKeyEpochs`(epoch별 public key) backward compat.
  - 외부 감사인이 audit chain integrity 자체 evaluation 가능.

- **Post-refresh check-health hook**:
  - Snap refresh 후 check-health hook (timeout 120s, 최근 commit `9c6bf04`) — refresh round-trip 후 시스템 통합 evaluation. CI에서 자동 검증.

- **Audit events (audit.* event family)**:
  - audit.scan.completed · audit.chain.key_rotated · audit.pack.signed · audit.checkpoint.created 등 — 통제 활동 자체가 audit event로 emit, ongoing trail 형성.

**gap**: 
- **effectiveness dashboard 신규 진입 예정 (Stage 11.B-6)** — 통제별 audit event 집계 + 통제별 status panel + `/compliance` 페이지. 현 round에서는 placeholder만, 실 구현은 후속 Stage.
- 정기 effectiveness 평가 report 0 (90일 운영 측정은 외부 firm 트랙 ★).
- soc2-controls 자동 검증 pack 미존재 (Stage 11.B-7 진입 예정).

**외부 트랙 ★**: 실 SOC2 Type II 90일 운영 효과성 측정 — 외부 firm(Deloitte/KPMG/PwC/BDO 등)이 측정. 본 epic은 자체 측정 도구만 cover.

---

### CC4.2 Evaluates and Communicates Internal Control Deficiencies in a Timely Manner

**Trust Services Criteria 본문 의역**: 조직은 내부 통제 결손(deficiency)을 평가하고, 책임 있는 corrective action 당사자에게 적시 통보합니다. 결손의 심각도에 따라 reporting line이 정의되며, 시정 조치가 추적됩니다.

**Lodestar 매핑**:

- **Alertmanager webhook routing (real-time deficiency notification)**:
  - `deploy/prometheus/alertmanager-sample.yml` — Prometheus alert → alertmanager → webhook/email/slack 라우팅.
  - severity 라벨 분리: `severity: warning` · `severity: critical` — 적시성 차별화.
  - 5 alert rule(`deploy/prometheus/alerts/multi-region.yml`) 모두 severity 라벨 + `service: rosshield` + `component: replication/ha/audit` 라벨 명시 → 책임 component 분기 가능.

- **Webhook delivery (외부 customer 통보)**:
  - `internal/api/handlers/webhook.go` — outbound webhook + HMAC signature + retry. external incident management system(PagerDuty · Opsgenie 등) 통합 채널.

- **Runbook §13 (5 alert 대응 절차)**:
  - `docs/operations/multi-region-failover-runbook.md` §13 — 각 alert(`RosshieldReplicationLagWarning` 등)에 대응 절차 명시. alert annotation에 `runbook_url` 연결로 alertmanager → 운영자 즉시 lookup 가능.

- **GitHub Issues (defect tracking)**:
  - `https://github.com/ssabro/rosshield/issues` (현재 private) — bug/defect tracking 채널.

- **Security disclosure channel**:
  - `SECURITY.md` §Response SLA — 보안 결손 적시 통보:
    - Initial acknowledgement: 72시간 이내.
    - Severity triage (CVSS 산정): 7일 이내.
    - Fix 또는 mitigation plan: 90일 이내 (Critical 30일 · High 60일 목표).
    - Public disclosure (Coordinated): Fix 배포 후 14~30일 (advisory + CVE 발행).
  - GitHub Private Vulnerability Reporting + email 신고 채널.

- **Post-mortem pattern (informal)**:
  - design doc §리스크 + commit message §결정·근거 + CHANGELOG `### Fixed` 섹션이 informal post-mortem 패턴 cover.

- **Audit chain immutability (deficiency event recording)**:
  - 통제 결손이 발생해도 audit chain 자체는 변조 불가능 — 결손 이벤트 자체가 audit event로 immutable 기록.

**gap**: 
- **formal deficiency reporting workflow 부재** — Prometheus alert는 real-time이나 통합 deficiency register · 시정 조치 SLA 추적 · 정기 review 라운드 docs 0.
- Defect tracking SLA(Critical N days · High N days 등) 명문화 부족 — 보안 결손(SECURITY.md)은 명문화 OK이나 운영 deficiency는 informal.
- Stage 11.B-6 effectiveness dashboard 진입 시 통제별 status drill-down으로 부분 cover 예정 (현 round에서는 placeholder만).

**외부 트랙 ★**: 실 SOC2 firm 진입 시 deficiency tracking process audit + 시정 조치 정기 review 외부 검증 별도 트랙.

---

## 참조

- AICPA Trust Services Criteria 2017 — CC4 Monitoring Activities (COSO Framework 2013 Component 5 기반).
- Lodestar 결선 자산:
  - `internal/platform/metrics/metrics.go` (Prometheus metric emit)
  - `internal/platform/replication/lagmetric/collector.go` (replication lag)
  - `deploy/grafana/rosshield-dashboard.json` (Grafana dashboard)
  - `deploy/prometheus/alerts/multi-region.yml` (5 alert rule)
  - `deploy/prometheus/alertmanager-sample.yml` (alertmanager routing)
  - `internal/api/handlers/handlers.go` (`/healthz`)
  - `internal/api/handlers/webhook.go` (outbound webhook)
  - `internal/domain/audit/hash.go` · `audit.go` · `checkpoint.go` (audit chain immutability)
  - `cmd/rosshield-audit-verify/` (fg-verify v2)
  - `docs/operations/multi-region-failover-runbook.md` §13 (5 alert 대응)
  - `SECURITY.md` (보안 결손 SLA)
- 관련 design doc:
  - `docs/design/notes/multi-region-ha-design.md` (Phase 8 · 9)
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain)
  - `docs/design/notes/soc2-readiness-design.md` §2.2 · §2.7 · Stage 11.B-6 (effectiveness dashboard 진입 예정)
- Stage 11.B-6 진입 예정: 통제 effectiveness dashboard (`internal/api/handlers/compliance_effectiveness.go` 신규 + `deploy/grafana/dashboards/compliance-effectiveness.json` + web `/compliance` 페이지).
- 다음 단계 (Stage 11.B-3 진입 예정): CC5 Control Activities → `cc5-control-activities.md`

---

*Last updated: 2026-05-21 — Stage 11.B-2 CC4 mapping round.*
