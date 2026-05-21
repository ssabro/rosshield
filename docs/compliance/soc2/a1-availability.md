# SOC2 A1 — Availability

> **Trust Services Criteria**: Additional Categories (가용성)
> **Sub-controls**: A1.1 ~ A1.3 (3)
> **Status**: Lodestar 결선 자산 ~2/3 매핑 (~67% cover) — 외부 감사인 검증 대기. A1.1·A1.2는 multi-region HA + Patroni + audit chain immutability + replication 결선 자산이 자연 cover, A1.3(environmental protections)은 DC 위탁 외부 트랙 ★ 의존.

A1은 가용성 — 용량 계획, 백업/복구, 환경 보호 3단계를 다룹니다. SOC2 Type II에서 SaaS · 엔터프라이즈 영업의 critical gate가 되는 통제군으로, RTO/RPO SLA 입증이 핵심입니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| A1.1 Capacity Planning | Multi-region HA · Patroni leader election · `replication_lag_seconds` metric · Prometheus + Grafana · scaling docs(`ha-deployment.md` · `patroni-deployment.md`) | formal capacity planning round docs 0 · auto-scaling 정책 명문화 부족 | 외부 firm capacity planning audit ★ |
| A1.2 Backup and Recovery | Audit chain immutability · backup endpoint · streaming replication(RPO 1m) · `multi-region-failover-runbook.md` · audit signer key rotation epoch 보존 · RPO/RTO docs | formal RPO/RTO customer-facing SLA 명문화 부족 · DR test 정기 라운드 0 | 외부 firm DR test 외부 검증 ★ |
| A1.3 Environmental Protections | (★ 외부) — Lodestar 자체 cover 0 | DC/클라우드 provider 위탁 | ★ AWS/GCP/Azure SOC2 Type II 인증서 또는 customer 자체 DC |

---

## Sub-control 상세

### A1.1 Maintains, Monitors, and Evaluates Current Processing Capacity and Use of System Components to Manage Capacity Demand and to Enable the Implementation of Additional Capacity to Help Meet Its Objectives

**Trust Services Criteria 본문 의역**: 조직은 처리 용량과 시스템 구성 요소 사용량을 유지 · 모니터링 · 평가하여 용량 수요를 관리하고 추가 용량 도입을 가능하게 합니다. 용량 부족이 가용성 SLA 위협 전에 식별됩니다.

**Lodestar 매핑**:

- **용량 모니터링 (capacity monitoring)**:
  - **Prometheus + Grafana**: `internal/platform/metrics/metrics.go` + `deploy/grafana/rosshield-dashboard.json` — 핵심 metric:
    - `rosshield_audit_chain_head_seq{tenant}` — audit chain 진행 속도.
    - `rosshield_replication_lag_seconds{application_name}` — replication 부하 (Phase 8 MR.T8).
    - `rosshield_ha_role` · `rosshield_ha_leader_epoch` · `rosshield_ha_failover_total` (Phase 9 E25).
  - scrape interval 15s/30s — near real-time capacity view.

- **5 alert rule (capacity threshold)**:
  - `deploy/prometheus/alerts/multi-region.yml`:
    - `RosshieldReplicationLagWarning` (lag > 30s for 2m) — replication 부하 임계 도달.
    - `RosshieldReplicationLagCritical` (> 60s for 1m, RPO 1m SLA 위협).
  - 용량 부족 사전 탐지 가능.

- **수평 확장 (horizontal scaling)**:
  - **Patroni 다중 replica**: `internal/platform/replication/setup/` — 추가 read replica 도입 가능.
  - **Multi-region 확장**: `internal/platform/ha/patroni/` — 신규 region 추가 시 자동 leader election 통합.

- **수직 확장 (vertical scaling)**:
  - **PostgreSQL 튜닝**: `docs/operations/patroni-deployment.md` — PostgreSQL 구성 가이드.

- **운영 docs**:
  - **`docs/operations/ha-deployment.md`** — HA 배포 가이드.
  - **`docs/operations/patroni-deployment.md`** — Patroni 구성 가이드.
  - **`docs/operations/multi-region-failover-runbook.md`** — 다중 region 운영 절차.

- **tenant 격리 (multi-tenant capacity)**:
  - 모든 테이블 `tenant_id` 컬럼 — tenant별 사용량 측정 가능.
  - `rosshield_audit_chain_head_seq{tenant}` — tenant 단위 metric.

**gap**: 
- **Formal capacity planning round docs 0** — 분기 또는 연간 capacity review 라운드 명문화 부재.
- **Auto-scaling 정책 명문화 부족** — Kubernetes HPA 가이드는 `deploy/k8s/` 부분 cover하나 정책 docs 0.
- **Capacity threshold customer-facing SLA docs 0**.

**외부 트랙 ★**: 외부 firm capacity planning audit + 정기 capacity review 외부 검증.

---

### A1.2 Authorizes, Designs, Develops or Acquires, Implements, Operates, Approves, Maintains, and Monitors Environmental Protections, Software, Data Backup Processes, and Recovery Infrastructure to Meet Its Objectives

**Trust Services Criteria 본문 의역**: 조직은 환경 보호 · 소프트웨어 · 데이터 백업 · 복구 인프라를 승인 · 설계 · 개발 · 구현 · 운영 · 모니터링하여 가용성 목표를 달성합니다. 백업의 빈도 · 보존 · 검증이 cover됩니다.

**Lodestar 매핑**:

- **Streaming replication (real-time backup)**:
  - **Patroni streaming replication**: `internal/platform/replication/setup/` — RPO 1m SLA.
  - **Multi-region replica**: 다른 region에 자동 replica 보유 — region 장애 시 자동 failover.

- **Audit chain immutability (data integrity)**:
  - **append-only**: `internal/domain/audit/hash.go` — 결정론적 hash chain. UPDATE/DELETE 불가능. 복구 후에도 데이터 무결성 보장.
  - **Audit signer key rotation epoch 보존**: 0037 마이그레이션 `audit_chain_keys` 테이블 — epoch별 public key 보존으로 backward verification 보장.

- **자동 복구 (automated recovery)**:
  - **Patroni 자동 failover**: RTO ≤ 60s (Phase 9 E25).
  - **Snap refresh + post-refresh check-health hook**: refresh 실패 시 자동 rollback (commit `9c6bf04` timeout 120s).
  - **CI/CD rollback**: cosign keyless + Sigstore Rekor → 이전 release 검증 후 rollback 가능.

- **수동 복구 (manual recovery)**:
  - **`multi-region-failover-runbook.md` §13**: 5 alert 대응 절차 + 수동 failover 절차.
  - **`docs/operations/audit-verify-cli.md`**: fg-verify v2 CLI 사용법.

- **백업 endpoint (point-in-time recovery)**:
  - audit chain은 append-only이므로 별도 백업 없이 chain 자체가 백업.
  - 향후 point-in-time backup endpoint (별 epic 후보).

- **백업 검증 (backup verification)**:
  - **fg-verify v2 CLI**: `cmd/rosshield-audit-verify/` — backup 무결성 외부 검증 가능. v2 bundle `_bundleVersion: "v2"` + `_chainKeyEpochs` backward compat.
  - **외부 감사인 자체 검증**: Ed25519 서명 checkpoint + epoch별 public key 보존으로 audit chain 외부 검증 가능.

- **RPO/RTO docs (서비스 수준)**:
  - **RPO 1m**: replication lag SLA (Phase 8 MR.T8). `RosshieldReplicationLagCritical` alert로 자동 모니터링.
  - **RTO ≤ 60s**: Patroni 자동 failover (Phase 9 E25). metric `rosshield_ha_failover_total` 측정.

**gap**: 
- **Formal RPO/RTO customer-facing SLA docs 0** — 내부 metric으로는 측정 OK이나 customer-facing SLA docs (예: 99.9% uptime · RPO 1m · RTO 60s) 명문화 부재.
- **DR test 정기 라운드 docs 0** — failover runbook은 명문화 OK이나 정기 DR test(예: 분기 1회) 라운드 명문화 부재.
- **백업 retention policy 명문화 부족** — audit chain은 append-only로 사실상 영구 보존이나 비-audit 데이터 retention policy 0.

**외부 트랙 ★**: 
- ★ 외부 firm DR test 외부 검증 (분기 또는 연간).
- ★ Customer-facing uptime SLA 별 epic.
- ★ Business Continuity Plan (BCP) 외부 검증.

---

### A1.3 Tests Recovery Plan Procedures Supporting System Recovery to Meet Its Objectives

**Trust Services Criteria 본문 의역**: 조직은 시스템 복구를 지원하는 복구 계획 절차를 정기 테스트하여 가용성 목표 달성을 검증합니다. 환경 보호(전력 · 냉각 · 화재 진압 등 물리)와 facility 통제가 포함됩니다.

**Lodestar 매핑**:

- **★ Lodestar 자체 cover 0** — Lodestar는 소프트웨어 제품 (no own DC).
- **클라우드 provider 위탁**: AWS/GCP/Azure 등 SOC2 Type II 인증 cloud provider 위탁 시 provider의 SOC2 audit이 A1.3 cover.
  - AWS: SOC2 Type II 인증서 + multi-AZ + multi-region availability zones.
  - GCP: SOC2 Type II 인증서 + multi-zone deployment.
  - Azure: SOC2 Type II 인증서 + availability sets/zones.
- **on-prem 배포**: customer 자체 DC 책임 — customer의 SOC2 audit 범위.
- **Appliance(Snap)**: customer 사이트 환경 보호 책임 — customer 책임.

- **Multi-region 운영 (recovery plan testing 보조)**:
  - Patroni 다중 region 운영으로 region 장애 자동 failover — DR test 자체는 multi-region 운영으로 부분 cover.
  - `multi-region-failover-runbook.md` §13 — 5 alert 대응 절차 + 수동 failover 절차 명문화.
  - 정기 DR test 라운드는 외부 트랙 ★.

**gap**: 
- Lodestar 자체 cover 영역 0 (소프트웨어 제품 제약).
- 정기 DR test 라운드 명문화 0.

**외부 트랙 ★**: 
- ★ AWS/GCP/Azure SOC2 Type II 인증서 (배포 환경 호스팅 시).
- ★ on-prem 배포 시 customer 자체 DC 환경 보호 책임.
- ★ 정기 DR test 외부 검증.

---

## 참조

- AICPA Trust Services Criteria 2017 — A1 Availability (Additional Category).
- Lodestar 결선 자산:
  - `internal/platform/ha/patroni/` · `ha.go` · `pglock.go` (multi-region HA · auto failover)
  - `internal/platform/replication/setup/` (Patroni streaming replication, RPO 1m)
  - `internal/platform/replication/lagmetric/collector.go` (replication lag metric)
  - `internal/platform/metrics/metrics.go` (Prometheus emit)
  - `deploy/grafana/rosshield-dashboard.json` (Grafana dashboard)
  - `deploy/prometheus/alerts/multi-region.yml` (5 alert rule)
  - `internal/domain/audit/hash.go` · `audit.go` · `checkpoint.go` · `keyrotation/rotator.go` (audit chain immutability)
  - `cmd/rosshield-audit-verify/` (fg-verify v2 backup verify)
  - `docs/operations/ha-deployment.md` · `patroni-deployment.md` · `multi-region-failover-runbook.md` (운영 가이드)
  - `docs/operations/audit-verify-cli.md` · `audit-chain-key-rotation.md`
  - `.github/workflows/release-pipeline.yml` (cosign keyless rollback baseline)
  - `deploy/k8s/` · `deploy/terraform/` (IaC scaling)
- 0037 마이그레이션: `audit_chain_keys` 테이블 (epoch별 public key 보존, backward verification).
- 관련 design doc:
  - `docs/design/notes/multi-region-ha-design.md` · `multi-region-ha-stage4-design.md` (Phase 8 · 9)
  - `docs/design/notes/e25-ha-design.md` (Patroni)
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain rotation)
  - `docs/design/notes/auto-failover-research.md` (failover research)
  - `docs/design/notes/soc2-readiness-design.md` §2 · §3.3
- cross-reference: A1.1 ↔ CC7.2 (system component monitoring), A1.2 ↔ CC7.5 (recovery from incidents), A1.2 ↔ CC9.1 (risk mitigation BCP), A1.3 ↔ CC6.4 (physical access), A1 전체 ↔ A2.1 (confidentiality during recovery).
- 다음 단계: A2 Confidentiality → [`a2-confidentiality.md`](./a2-confidentiality.md)

---

*Last updated: 2026-05-21 — Stage 11.B-4 A1 mapping round.*
