# SOC2 CC7 — System Operations

> **Trust Services Criteria**: Common Criteria (필수, 모든 SOC2 audit 적용)
> **Sub-controls**: CC7.1 ~ CC7.5 (5)
> **Status**: Lodestar 결선 자산 ~3.5/5 매핑 (~70% cover) — 외부 감사인 검증 대기. CC7.1·CC7.2·CC7.3·CC7.5는 Prometheus + Grafana + multi-region HA + audit chain 결선 자산이 자연 cover, CC7.4(incident response)는 SECURITY.md + alertmanager가 부분 cover하나 정식 incident response 절차 docs 0 → 외부 트랙 ★ 의존.

CC7은 시스템 운영 — 탐지/모니터링, 시스템 구성 요소 모니터링, 보안 이벤트 평가, 인시던트 대응, 인시던트 복구 5단계를 다룹니다. 통제 시스템의 일상 운영과 인시던트 발생 시 대응 절차를 cover합니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| CC7.1 Detects and Monitors | Prometheus + Grafana dashboard · 5 alert rule · scrape interval 15s/30s · structured logging | SIEM(Splunk/Sumo/ELK) 통합 부재 | 외부 SIEM 별 epic 또는 SaaS 위탁 ★ |
| CC7.2 Monitors System Components | Patroni leader election · `/healthz` endpoint · check-health hook · replication lag metric · audit chain head metric | 통제별 effectiveness dashboard 부재 (Stage 11.B-6 진입 예정) | 외부 firm 운영 효과성 90일 측정 ★ |
| CC7.3 Evaluates Security Events | Audit chain immutability · webhook delivery + HMAC · alertmanager severity routing · `SECURITY.md` triage SLA | formal security event triage workflow docs 0 | 외부 firm security event audit ★ |
| CC7.4 Responds to Identified Security Incidents | `SECURITY.md` response SLA · alertmanager routing · runbook(`docs/operations/`) · post-refresh hook | **정식 incident response 절차 docs 0** | ★ incident response plan 별 epic 또는 외부 컨설팅 |
| CC7.5 Recovers from Identified Security Incidents | Multi-region HA + Patroni auto failover(RTO ≤ 60s) · `multi-region-failover-runbook.md` · audit chain immutability · backup endpoint | DR test 정기 라운드 docs 0 | 외부 firm DR test 외부 검증 ★ |

---

## Sub-control 상세

### CC7.1 Uses Detection and Monitoring Procedures to Identify (1) Changes to Configurations That Result in the Introduction of New Vulnerabilities, and (2) Susceptibilities to Newly Discovered Vulnerabilities

**Trust Services Criteria 본문 의역**: 조직은 구성 변경 또는 신규 취약점에 의해 발생하는 보안 노출을 식별하기 위해 탐지·모니터링 절차를 사용합니다. 시스템 활동 · 구성 · 외부 위협 정보를 실시간 모니터링합니다.

**Lodestar 매핑**:

- **Prometheus + Grafana (실시간 모니터링)**:
  - `internal/platform/metrics/metrics.go` — Lodestar core metric emit. scrape interval 15s/30s near real-time.
  - `deploy/grafana/rosshield-dashboard.json` — multi-region status + audit chain + replication lag 시각화 (Phase 10.A).
  - 핵심 metric:
    - `rosshield_audit_chain_head_seq{tenant}` — audit chain head sequence.
    - `rosshield_ha_role` · `rosshield_ha_leader_epoch` · `rosshield_ha_failover_total` (Phase 9 E25).
    - `rosshield_replication_lag_seconds{application_name}` (Phase 8 MR.T8).

- **5 alert rule (자동 탐지 trigger)**:
  - `deploy/prometheus/alerts/multi-region.yml` — 5 alert:
    1. `RosshieldReplicationLagWarning` (lag > 30s for 2m).
    2. `RosshieldReplicationLagCritical` (> 60s for 1m, RPO 1m SLA 위협).
    3. 그 외 multi-region 관련 alert 3건.
  - alert annotation `runbook_url`이 `docs/operations/multi-region-failover-runbook.md` §13 대응 절차로 연결.

- **Structured logging (탐지 보조)**:
  - Go `log/slog` 또는 동등 — JSON structured log + tenant_id + trace_id.
  - 향후 OpenTelemetry tracing 전면 적용 시 분산 trace 자동 상관 (Phase 11 옵션 A 후보).

- **Audit chain (구성 변경 탐지)**:
  - 모든 구성 변경 audit event로 immutable 기록. `audit.tenant.config_updated` · `audit.chain.key_rotated` 등.

- **CI/CD 통합 변경 탐지**:
  - `.github/workflows/ci.yml` — 모든 PR 자동 lint + test + build 검증. 회귀 자동 탐지.

**gap**: 
- **SIEM (Security Information and Event Management) 통합 부재** — Prometheus는 metric 중심, security event 상관 분석은 별 도구(Splunk/Sumo Logic/ELK Stack/Datadog) 필요.
- **Threat intelligence feed 통합 0** — 신규 CVE 알림 자동화 부재.

**외부 트랙 ★**: 
- ★ 외부 SIEM 별 epic 또는 SaaS 위탁.
- ★ Threat intelligence feed 구독 (CrowdStrike/Recorded Future 등).

---

### CC7.2 Monitors System Components and the Operation of Those Components for Anomalies That Are Indicative of Malicious Acts, Natural Disasters, and Errors

**Trust Services Criteria 본문 의역**: 조직은 시스템 구성 요소 및 운영을 모니터링하여 악의적 행위 · 자연 재해 · 오류를 시사하는 이상을 식별합니다. 모니터링은 통제 시스템 작동에 통합됩니다.

**Lodestar 매핑**:

- **Patroni leader election + failover monitoring**:
  - `internal/platform/ha/patroni/` — Patroni 자동 failover (Phase 9 E25). leader 변경 audit event emit.
  - metric: `rosshield_ha_role` · `rosshield_ha_leader_epoch` · `rosshield_ha_failover_total`.

- **`/healthz` endpoint**:
  - `internal/api/handlers/handlers.go` — health check endpoint. 외부 모니터링 도구 통합 가능.

- **Post-refresh check-health hook**:
  - Snap refresh 후 check-health hook (timeout 120s, commit `9c6bf04`) — refresh 후 시스템 통합 evaluation. CI에서 자동 검증.

- **Replication lag metric**:
  - `internal/platform/replication/lagmetric/collector.go` — `rosshield_replication_lag_seconds{application_name}` (Phase 8 MR.T8). RPO 1m SLA 자동 모니터링.

- **Audit chain head metric**:
  - `rosshield_audit_chain_head_seq{tenant}` — chain 진행 중단 자동 탐지 가능.

- **Audit signer key rotation 자동 (90일)**:
  - `internal/domain/audit/keyrotation/rotator.go` — rotation 실패 시 audit event emit + Prometheus alert 가능.

- **5 alert rule severity routing**:
  - warning vs critical 차별화. critical alert는 즉시 webhook + email + slack 라우팅.

**gap**: 
- **통제별 effectiveness dashboard 부재** — Stage 11.B-6 진입 예정. 현재는 인프라 metric 중심, 통제 단위 effectiveness drill-down 부재.

**외부 트랙 ★**: 
- ★ 외부 firm 운영 효과성 90일 측정 (SOC2 Type II 정의상 외부 firm 책임).

---

### CC7.3 Evaluates Security Events to Determine Whether They Could or Have Resulted in a Failure of the Entity to Meet Its Objectives

**Trust Services Criteria 본문 의역**: 조직은 식별된 보안 이벤트를 평가하여 조직 목표 달성 실패 가능성 또는 실제 실패 여부를 판단합니다. 평가 결과는 인시던트 대응 절차로 escalation됩니다.

**Lodestar 매핑**:

- **Alertmanager severity routing (자동 평가)**:
  - `deploy/prometheus/alertmanager-sample.yml` — severity 라벨(warning · critical) 차별화 + service/component 라벨 라우팅.
  - 5 alert rule(`deploy/prometheus/alerts/multi-region.yml`) 모두 severity + service: rosshield + component: replication/ha/audit 라벨 명시.

- **Webhook delivery (외부 escalation)**:
  - `internal/api/handlers/webhook.go` — outbound webhook + HMAC signature + retry. PagerDuty · Opsgenie 등 incident management 통합.

- **Audit chain immutability (이벤트 기록)**:
  - `internal/domain/audit/hash.go` — 보안 이벤트가 audit event로 immutable 기록. 사후 평가 가능.
  - `audit.checkpoint.created` · `audit.chain.key_rotated` · `audit.export.created` 등 보안 관련 audit event family.

- **Security disclosure triage**:
  - `SECURITY.md` §Response SLA:
    - Initial acknowledgement: 72시간.
    - Severity triage (CVSS 산정): 7일.
    - Fix 또는 mitigation plan: 90일 (Critical 30일 · High 60일).

- **Runbook §13 (5 alert 대응)**:
  - `docs/operations/multi-region-failover-runbook.md` §13 — 각 alert에 대응 절차 명시.

- **fg-verify v2 (사후 검증)**:
  - `cmd/rosshield-audit-verify/` — 외부 감사인 자체 검증으로 audit chain 무결성 확인.

**gap**: 
- **Formal security event triage workflow docs 0** — `SECURITY.md`는 외부 신고 위주, 내부 운영 보안 event triage 명문화 0.
- **Threat severity classification matrix 부재** — CVSS는 SECURITY.md에 명시이나 내부 운영 event 자체 분류 0.

**외부 트랙 ★**: 외부 firm security event triage process audit.

---

### CC7.4 Responds to Identified Security Incidents by Executing a Defined Incident Response Program to Understand, Contain, Remediate, and Communicate Security Incidents, as Appropriate

**Trust Services Criteria 본문 의역**: 조직은 식별된 보안 인시던트에 대해 정의된 인시던트 대응 프로그램을 실행합니다. 프로그램은 이해 · 봉쇄 · 시정 · 의사소통 단계를 cover합니다.

**Lodestar 매핑**:

- **외부 신고 대응 (external incident)**:
  - **SECURITY.md**:
    - Response SLA: Initial 72h · triage 7d · fix 90d (Critical 30d · High 60d).
    - GitHub Private Vulnerability Reporting + email 신고 채널.
    - Coordinated disclosure: Fix 배포 후 14~30일 (advisory + CVE 발행).

- **운영 인시던트 대응 (operational incident)**:
  - **Alertmanager routing**: severity warning · critical 분리 → webhook/email/slack 자동 escalation.
  - **5 alert runbook**: `multi-region-failover-runbook.md` §13 — 각 alert 대응 절차.
  - **Patroni 자동 failover (RTO ≤ 60s)**: 자동 봉쇄 (Phase 9 E25).
  - **Snap refresh rollback**: post-refresh hook fail 시 자동 rollback (commit `9c6bf04`).

- **의사소통 (communication)**:
  - **Webhook delivery (real-time customer notification)**: `internal/api/handlers/webhook.go` — 외부 customer 자동 통보 (HMAC signature + retry).
  - **CHANGELOG**: 인시던트 사후 변경 사항 명시.

- **시정 (remediation)**:
  - **trunk-based + signed commits**: 시정 변경이 main에 직접 commit + audit chain immutable 기록.
  - **CI/CD 자동 검증**: 시정 변경이 회귀 0 검증 (`make ci`).

**gap**: 
- **정식 incident response 절차 docs 0** — 운영 incident 분류(P0~P3) · 단계별 절차(detection → triage → containment → eradication → recovery → post-mortem) · escalation matrix · communication template 명문화 부재.
- **Post-mortem template 부재** — 현재는 design doc §리스크 + commit message §결정·근거 + CHANGELOG `### Fixed`가 informal post-mortem 패턴 cover하나 정식 template 0.
- **On-call rotation 정책 0** (small team).

**외부 트랙 ★**: 
- ★ Incident response plan 별 epic 또는 외부 컨설팅 (예: Mandiant · CrowdStrike · TrustedSec).
- ★ On-call rotation 정책 (회사 성장 후).
- ★ Tabletop exercise 정기 라운드.

---

### CC7.5 Identifies, Develops, and Implements Activities to Recover from Identified Security Incidents

**Trust Services Criteria 본문 의역**: 조직은 식별된 보안 인시던트로부터 복구하기 위한 활동을 식별 · 개발 · 구현합니다. 복구는 정상 운영 회복까지 cover합니다.

**Lodestar 매핑**:

- **Multi-region HA + Patroni 자동 failover**:
  - `internal/platform/ha/patroni/` — RTO ≤ 60s 자동 복구 (Phase 9 E25).
  - `internal/platform/ha/pglock.go` — PostgreSQL advisory lock 기반 단일 leader 보장.
  - 복구 audit event emit (`audit.ha.failover_completed` 등).

- **`docs/operations/multi-region-failover-runbook.md`**:
  - 수동 failover 절차 + 자동 failover 실패 시 fallback 절차 명문화.
  - §13에 5 alert 대응 절차 명시.

- **Backup endpoint + audit chain immutability (RPO 보장)**:
  - audit chain은 append-only — 인시던트 후에도 데이터 무결성 보장.
  - replication RPO 1m SLA (Patroni streaming).
  - 향후 backup endpoint (별 epic 후보) — point-in-time recovery.

- **fg-verify v2 (복구 후 무결성 검증)**:
  - `cmd/rosshield-audit-verify/` — 복구 후 audit chain 자체 검증 가능. v2 bundle backward compat.

- **Snap refresh rollback (deployment recovery)**:
  - post-refresh check-health hook fail 시 자동 rollback (commit `9c6bf04`).

- **Audit signer key rotation 자동**:
  - key compromise 시 rotation으로 신규 epoch 진입 + epoch별 public key 보존(0037)으로 backward verification 보장.

- **CI/CD rollback**:
  - cosign keyless signed releases + Sigstore Rekor 투명 로그 → 이전 release 검증 후 rollback 가능.

**gap**: 
- **DR (Disaster Recovery) test 정기 라운드 docs 0** — failover runbook은 명문화 OK이나 정기 DR test(예: 분기 1회) 라운드 명문화 부재.
- **RTO/RPO SLA 명문화** — Patroni RTO ≤ 60s · replication RPO 1m은 metric으로 측정 가능하나 customer-facing SLA docs 0.

**외부 트랙 ★**: 
- ★ 외부 firm DR test 외부 검증 (분기 또는 연간 라운드).
- ★ Business continuity plan (BCP) 외부 검증.

---

## 참조

- AICPA Trust Services Criteria 2017 — CC7 System Operations.
- Lodestar 결선 자산:
  - `internal/platform/metrics/metrics.go` (Prometheus emit)
  - `internal/platform/replication/lagmetric/collector.go` (replication lag)
  - `deploy/grafana/rosshield-dashboard.json` (Grafana dashboard)
  - `deploy/prometheus/alerts/multi-region.yml` (5 alert rule)
  - `deploy/prometheus/alertmanager-sample.yml` (alertmanager routing)
  - `internal/api/handlers/handlers.go` (`/healthz`)
  - `internal/api/handlers/webhook.go` (outbound webhook)
  - `internal/domain/audit/hash.go` · `audit.go` · `checkpoint.go` · `keyrotation/rotator.go` (audit chain)
  - `cmd/rosshield-audit-verify/` (fg-verify v2)
  - `internal/platform/ha/patroni/` · `ha.go` · `pglock.go` (HA leader election)
  - `internal/platform/replication/setup/` (Patroni replication)
  - `docs/operations/multi-region-failover-runbook.md` §13 (5 alert 대응)
  - `SECURITY.md` (보안 결손 SLA + coordinated disclosure)
- 관련 design doc:
  - `docs/design/notes/multi-region-ha-design.md` · `multi-region-ha-stage4-design.md` (Phase 8 · 9)
  - `docs/design/notes/e25-ha-design.md` (Patroni)
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain)
  - `docs/design/notes/e35-refresh-rollback-redesign.md` (post-refresh hook)
  - `docs/design/notes/soc2-readiness-design.md` §2 · §3.3
- cross-reference: CC7.1 ↔ CC4.1 (ongoing monitoring), CC7.2 ↔ A1.1 (availability monitoring), CC7.3 ↔ CC4.2 (deficiency communication), CC7.4 ↔ CC9.1 (risk mitigation activities), CC7.5 ↔ A1.2 (backup and recovery).
- 다음 단계: CC8 Change Management → [`cc8-change-management.md`](./cc8-change-management.md)

---

*Last updated: 2026-05-21 — Stage 11.B-3 CC7 mapping round.*
