# SOC2 CC2 — Communication and Information

> **Trust Services Criteria**: Common Criteria (필수, 모든 SOC2 audit 적용)
> **Sub-controls**: CC2.1 ~ CC2.3 (3)
> **Status**: Lodestar 결선 자산 ~3/3 매핑 (~100% cover, docs 보강 잔여) — 외부 감사인 검증 대기. 결선 자산(audit chain · Prometheus · webhook · release notes · CHANGELOG)이 자연 cover.

CC2는 통제 정보의 통신 — 정보 생성 · 내부 통신 · 외부 통신을 다룹니다. 통제 환경의 작동을 지원하는 정보 흐름의 품질과 신뢰성에 대한 통제군입니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| CC2.1 Obtains or Generates Relevant Quality Information | Audit chain(append-only) · Prometheus metrics · Grafana dashboard · `/healthz` endpoint · structured logging | 정보 품질 SLA docs 0 | 외부 firm 데이터 품질 audit ★ |
| CC2.2 Internally Communicates Information | `docs/operations/` runbook · `docs/onboarding/` · CLAUDE.md · SESSION_HANDOFF.md · CHANGELOG · design doc 패턴 | 정기 staff communication 라운드 docs 0 (small team) | internal HR comms 트랙 ★ |
| CC2.3 Communicates with External Parties | `docs/releases/` notes · CHANGELOG · GitHub Releases + cosign 서명 · Webhook delivery · alertmanager-sample · `SECURITY.md` 신고 채널 | customer communication wizard 부재 | 외부 customer notification SLA ★ |

---

## Sub-control 상세

### CC2.1 Obtains or Generates and Uses Relevant Quality Information

**Trust Services Criteria 본문 의역**: 조직은 내부 통제의 작동을 지원하기 위해 관련성 있고 품질 높은 정보를 획득 · 생성 · 사용합니다. 정보는 정확성 · 적시성 · 완전성 · 접근성을 갖춰야 합니다.

**Lodestar 매핑**:

- **Audit chain (append-only quality information)**:
  - `internal/domain/audit/audit.go` — `Entry.SignerKeyID` + `KeyEpoch *int64` 필드 (Phase 10.D-2 결선).
  - `internal/domain/audit/hash.go` — `canonicalMetaJSON` 알파벳순 7 키 직렬화 + `hash_i = sha256(prevHash ‖ payloadDigest ‖ canonicalMetaJSON)` 결정론적 hash chain.
  - `internal/domain/audit/checkpoint.go` — Ed25519 서명 + `audit_checkpoints` 테이블 `signer_key_id` 컬럼.
  - 정확성 + 적시성 + 완전성 + immutability 보장.

- **Prometheus metrics (real-time quality information)**:
  - `internal/platform/metrics/metrics.go` — `rosshield_audit_chain_head_seq{tenant}` · `rosshield_ha_role` · `rosshield_ha_leader_epoch` · `rosshield_ha_failover_total` · `rosshield_replication_lag_seconds` 등.
  - 적시성: scrape interval 15s/30s.
  - 접근성: Prometheus + Grafana dashboard.

- **Grafana dashboard**:
  - `deploy/grafana/rosshield-dashboard.json` — multi-region status + audit chain head + replication lag 시각화 (Phase 10.A 결선).

- **`/healthz` endpoint**:
  - `internal/api/handlers/handlers.go` — health check endpoint, 시스템 가용성 정보 외부 공개.

- **Structured logging**:
  - Go `log/slog` 또는 동등 — JSON structured log + tenant_id + trace_id (관련 필드).

**gap**: 정보 품질 SLA(latency · accuracy threshold) 명문화 docs 0. 정보 retention policy 명문화는 audit chain rotation docs(`docs/operations/audit-chain-key-rotation.md`)가 부분 cover.

**외부 트랙 ★**: 실 SOC2 firm 진입 시 정보 품질 audit + SLA dashboard 별도 검증 트랙.

---

### CC2.2 Internally Communicates Information, including Objectives and Responsibilities

**Trust Services Criteria 본문 의역**: 조직은 내부 통제의 작동에 필요한 정보(목표 · 책임 포함)를 내부적으로 통신합니다. 조직 구성원이 자신의 통제 책임을 이해할 수 있도록 적절한 채널로 정보를 전달합니다.

**Lodestar 매핑**:

- **Operations runbooks**:
  - `docs/operations/audit-chain-key-rotation.md` — audit signer key 90일 rotation 절차.
  - `docs/operations/audit-verify-cli.md` — fg-verify v2 사용 가이드.
  - `docs/operations/multi-region-failover-runbook.md` — Patroni failover 절차 (§13 5 alert 대응).
  - `docs/operations/ha-deployment.md` · `patroni-deployment.md` · `multi-region-dns.md` — HA 배포 가이드.
  - `docs/operations/cis-ubuntu-2404-degraded.md` · `cis-ubuntu-2404-manual.md` — CIS 검증 runbook.
  - `docs/operations/snap-deployment.md` · `release-pack-signer-setup.md` · `secure-boot-enrollment.md` · `ros2-humble-deployment.md` · `pwa-offline.md` · `pwa-persist.md` — 운영 docs 14건 결선.

- **Onboarding docs**:
  - `docs/onboarding/` — 신규 contributor 진입 가이드.

- **Project governance**:
  - `CLAUDE.md` — 작업 컨벤션 · TDD 강제 · 도메인 경계 규칙 · 비목표 명시. 모든 contributor의 책임 명확화.
  - `SESSION_HANDOFF.md` — Phase 진행 상태 + 결정 로그 + 진행 중 선택지 (현 phase에서 임시 결정 communication 채널).

- **Change communication**:
  - `CHANGELOG.md` — 모든 release 변경 사항 + breaking change 표기.
  - `docs/releases/v*.md` — release notes (현재 v0.6.9 ~ v0.11.0 38건).
  - design doc 패턴 — 큰 작업은 `docs/design/notes/*.md` 우선, 의사결정 추적 가능성 보장.

**gap**: 정기 staff communication 라운드(monthly all-hands, quarterly review 등) docs 0 — small team(현 1인 founder)으로 자연 부재.

**외부 트랙 ★**: 회사 성장 + team 확장 시 internal HR communication 트랙(Slack channel · all-hands · staff townhall) 별도 cover.

---

### CC2.3 Communicates with External Parties

**Trust Services Criteria 본문 의역**: 조직은 외부 당사자(customer · vendor · regulator · auditor 등)에 통제의 작동에 영향을 미치는 정보를 통신합니다. 외부 채널은 신뢰할 수 있고, incident 발생 시 적시 통보 절차를 갖춥니다.

**Lodestar 매핑**:

- **GitHub Releases + cosign keyless signed binary** (CC8.1 · CC9.2 dual mapping):
  - `.github/workflows/release-pipeline.yml` — GitHub Releases automation.
  - `internal/domain/audit/rotation/cosign.go` — audit segment cosign keyless 서명 + Sigstore Rekor 등록.
  - 38건 release(v0.3.0 ~ v0.11.0) — 외부 customer가 binary 진위 자체 검증 가능.

- **Release notes**:
  - `docs/releases/v*.md` — 각 release의 변경 · 영향 · upgrade 절차.
  - `CHANGELOG.md` — Keep a Changelog 형식 + semantic versioning.

- **Webhook delivery** (외부 customer 통보):
  - `internal/api/handlers/webhook.go` — outbound webhook delivery + signature(HMAC) + retry. External system 통합 채널.

- **Alertmanager (운영 통보)**:
  - `deploy/prometheus/alertmanager-sample.yml` — Prometheus alert → alertmanager → webhook/email/slack 외부 통보 채널.
  - 5 alert rule: `RosshieldReplicationLagWarning` · `RosshieldReplicationLagCritical` 등 (`deploy/prometheus/alerts/multi-region.yml` 5건 결선).

- **Security disclosure channel**:
  - `SECURITY.md` §Reporting a Vulnerability — GitHub Private Vulnerability Reporting + email `ssabro_k@naver.com` + PGP 옵션.
  - response SLA: initial ack 72h · severity triage 7d · fix 90d (Critical 30d · High 60d).

- **External docs accessibility**:
  - repo public(D6 = GitHub private 유지이나 release binary + report verify CLI(E30) 외부 검증 baseline 결선 — 설계서 D6 §결정 항목 참조). enterprise customer 진입 시 trial license 또는 별 channel.

- **fg-verify CLI 외부 감사인 호환**:
  - `cmd/rosshield-audit-verify/` — 외부 감사인이 audit bundle을 binary 형태로 받아 자체 검증 가능. v2 bundle `_bundleVersion: "v2"` + `_chainKeyEpochs` 호환 (Phase 10.D-5).

**gap**: customer-facing notification wizard 부재. enterprise customer가 정기 통제 status report를 받는 자동화 0 (Stage 11.B-6 effectiveness dashboard에서 부분 cover 예정). breach notification SLA 명문화 부족.

**외부 트랙 ★**: 
- ★ 실 customer notification SLA(GDPR Art 33 72h notification 등 별 framework) 외부 firm 트랙.
- ★ Regulator notification 절차(국가별 상이) 별 트랙.

---

## 참조

- AICPA Trust Services Criteria 2017 — CC2 Communication and Information.
- Lodestar 결선 자산:
  - `internal/domain/audit/` (audit chain — `audit.go` · `hash.go` · `checkpoint.go` · `export.go`)
  - `internal/platform/metrics/metrics.go` (Prometheus)
  - `internal/api/handlers/webhook.go` (webhook delivery)
  - `internal/api/handlers/handlers.go` (`/healthz`)
  - `deploy/grafana/rosshield-dashboard.json` (Grafana)
  - `deploy/prometheus/alerts/multi-region.yml` (5 alert rule)
  - `deploy/prometheus/alertmanager-sample.yml` (alertmanager)
  - `docs/operations/*.md` (14건 runbook)
  - `docs/releases/v*.md` (38건 release notes)
  - `CHANGELOG.md`
  - `SECURITY.md` (외부 신고 채널)
  - `CLAUDE.md` · `SESSION_HANDOFF.md` (internal communication)
  - `cmd/rosshield-audit-verify/` (외부 감사인 호환)
- 관련 design doc:
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain)
  - `docs/design/notes/multi-region-ha-design.md` (multi-region)
  - `docs/design/notes/soc2-readiness-design.md` §2.2 · §2.7 (fact-check)
- 다음 단계: CC3 Risk Assessment → [`cc3-risk-assessment.md`](./cc3-risk-assessment.md)

---

*Last updated: 2026-05-21 — Stage 11.B-2 CC2 mapping round.*
