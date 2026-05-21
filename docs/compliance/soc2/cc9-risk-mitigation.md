# SOC2 CC9 — Risk Mitigation

> **Trust Services Criteria**: Common Criteria (필수, 모든 SOC2 audit 적용)
> **Sub-controls**: CC9.1 ~ CC9.2 (2)
> **Status**: Lodestar 결선 자산 ~1.5/2 매핑 (~75% cover) — 외부 감사인 검증 대기. CC9.1(리스크 완화 활동)은 multi-region HA + audit chain + cosign 결선 자산이 자연 cover, CC9.2(vendor risk)는 LLM 4 provider + snap store + cosign 의존이 있어 vendor inventory docs는 외부 트랙 ★.

CC9는 리스크 완화 — 리스크 완화 활동 식별·개발, 벤더 관련 리스크 평가·관리 2단계를 다룹니다. CC3(리스크 평가)의 결과를 받아 실 완화 메커니즘을 운영하는 통제군입니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| CC9.1 Identifies, Selects, and Develops Risk Mitigation Activities | Multi-region HA + Patroni auto failover(RTO ≤ 60s) · audit chain immutability · audit signer key rotation 자동 · cosign keyless · TPM Secure Boot · CI/CD · 의존성 lockfile · post-refresh hook · backup endpoint | business continuity plan(BCP) 명문화 부족 · 정기 risk mitigation 라운드 docs 0 | 외부 firm BCP 외부 검증 ★ |
| CC9.2 Assesses and Manages Risks Associated with Vendors and Business Partners | cosign Sigstore Rekor(공급망 투명성) · 의존성 lockfile(go.sum) · LLM 4 provider 추상화(`internal/platform/llm/`) | **formal vendor inventory · vendor risk register 부재** | ★ vendor risk assessment 별 epic 또는 외부 컨설팅 |

---

## Sub-control 상세

### CC9.1 Identifies, Selects, and Develops Risk Mitigation Activities for Risks Arising from Potential Business Disruptions

**Trust Services Criteria 본문 의역**: 조직은 잠재적 사업 중단으로부터 발생하는 리스크에 대한 완화 활동을 식별 · 선정 · 개발합니다. 가용성 · 무결성 · 기밀성 측면의 리스크가 완화 대상에 포함됩니다.

**Lodestar 매핑**:

- **가용성 완화 (availability mitigation)**:
  - **Multi-region HA + Patroni 자동 failover**: `internal/platform/ha/patroni/` — RTO ≤ 60s 자동 복구 (Phase 9 E25).
  - **Patroni leader election**: `internal/platform/ha/ha.go` + `pglock.go` — PostgreSQL advisory lock 기반 단일 leader.
  - **Streaming replication**: `internal/platform/replication/setup/` — RPO 1m SLA.
  - **`multi-region-failover-runbook.md` §13**: 5 alert 대응 절차.
  - **`/healthz` endpoint**: 외부 모니터링 통합 가능.

- **무결성 완화 (integrity mitigation)**:
  - **Audit chain immutability**: `internal/domain/audit/hash.go` — 결정론적 hash chain. tamper 시 chain break 검출.
  - **Audit signer key rotation 자동 (90일)**: `internal/domain/audit/keyrotation/rotator.go` — long-term integrity 강화. epoch별 public key 보존(0037).
  - **Ed25519 서명 checkpoint**: `internal/domain/audit/checkpoint.go` — 외부 감사인 자체 검증 가능.
  - **fg-verify v2 CLI**: `cmd/rosshield-audit-verify/` — backward compat verify.

- **기밀성 완화 (confidentiality mitigation)**:
  - **TLS 강제 (transit)**.
  - **TPM PCR-sealed keystore**: `internal/platform/keystore/tpm/` — Secure Boot + measured boot.
  - **RBAC fine-grained + tenant 격리**: `internal/api/handlers/rbac_middleware.go`.

- **공급망 리스크 완화 (supply chain mitigation)**:
  - **cosign keyless signed releases + Sigstore Rekor**: 공급망 변조 방어.
  - **의존성 lockfile (go.sum · package-lock.json)**: 무단 의존성 변경 차단.
  - **CI/CD 자동 검증**: `.github/workflows/ci.yml` — `make ci`.

- **운영 리스크 완화 (operational mitigation)**:
  - **Post-refresh check-health hook**: refresh 후 자동 검증 + 실패 시 rollback (commit `9c6bf04` timeout 120s).
  - **Snap channel별 점진 배포**: edge · beta · candidate · stable.
  - **Audit event family**: `audit.chain.key_rotated` · `audit.pack.signed` · `audit.checkpoint.created` · `audit.scan.completed` 등 — 운영 활동 audit trail.

- **회복 리스크 완화 (recovery mitigation)**:
  - **Multi-region failover runbook**: 수동 failover 절차 + 자동 failover 실패 시 fallback.
  - **Backup endpoint** (별 epic 후보): point-in-time recovery.

**gap**: 
- **Business Continuity Plan (BCP) 명문화 부족** — multi-region HA + runbook은 명문화 OK이나 BCP scope(인력 · 시설 · 통신 · vendor 의존 등 포괄적 BCP) 명문화 0.
- **정기 risk mitigation 라운드 docs 0** — 분기 또는 연간 라운드 명문화 부재.
- **RTO/RPO customer-facing SLA docs 0**.

**외부 트랙 ★**: 
- ★ BCP 외부 컨설팅 firm (예: Marsh · Aon) 위탁.
- ★ 정기 DR test 외부 검증 (분기 또는 연간).
- ★ 외부 firm BCP audit.

---

### CC9.2 Assesses and Manages Risks Associated with Vendors and Business Partners

**Trust Services Criteria 본문 의역**: 조직은 벤더 및 사업 파트너와 관련된 리스크를 평가하고 관리합니다. 벤더 inventory · due diligence · 계약상 보안 요건 · 모니터링이 포함됩니다.

**Lodestar 매핑**:

- **공급망 투명성 (supply chain transparency)**:
  - **cosign keyless signed releases + Sigstore Rekor**: 모든 release artifact 외부 검증 가능. 공급망 변조 사후 탐지 가능.
  - **의존성 lockfile (go.sum)**: Go 의존성 hash 고정. 무단 의존성 변경 시 빌드 fail.
  - **`package-lock.json` (web)**: JavaScript 의존성 고정.

- **벤더 격리 (vendor isolation)**:
  - **LLM 4 provider 추상화**: `internal/platform/llm/` — OpenAI · Anthropic · Google · 자체 호스팅 4 provider 추상화. 단일 provider 의존 회피.
  - **SSO IdP 추상화**: SAML/OIDC 표준 — 특정 IdP 잠금 없음.
  - **S3 backend 추상화**: `internal/domain/audit/rotation/backend_s3.go` · `backend_s3_enterprise.go` · `backend_file.go` — S3-compatible backend(MinIO · AWS S3 · GCS · R2) + 파일 backend 추상화.

- **vendor risk 식별 영역 (현재 가시 vendor)**:
  - **Cloud provider**: AWS/GCP/Azure (배포 환경 — customer 또는 Lodestar 호스팅 시).
  - **Snap store**: Canonical (snap binary 배포).
  - **GitHub**: code hosting · CI/CD · release artifact.
  - **Sigstore**: cosign keyless + Rekor 투명 로그 (오픈소스 + Linux Foundation 운영).
  - **LLM provider**: OpenAI · Anthropic · Google (옵트인 시).
  - **SSO IdP**: Okta · Auth0 · Azure AD · Google Workspace (customer 선택).
  - **DC/클라우드 provider**: customer 배포 환경 책임 (★ CC6.4 위탁).

- **운영 모니터링 (vendor monitoring)**:
  - **CI/CD 통합 검증**: 의존성 변경 시 자동 lint + test + build.
  - **Dependabot/Renovate** (권장 진입 예정): 의존성 취약점 자동 알림.

**gap**: 
- **Formal vendor inventory 부재** — 위 vendor 목록은 design doc + CLAUDE.md + 코드 분산. 단일 vendor inventory 카탈로그 docs 0.
- **Vendor risk register 부재** — 각 vendor의 리스크 등급 · 대체 계획 · 모니터링 SLA 명문화 0.
- **Vendor contract 보안 요건 명문화 0** — Cloud provider · LLM provider 등과의 계약상 보안 요건 명문화 부재.
- **Vendor SOC2 인증서 수집 절차 0** — AWS/GCP/Azure SOC2 인증서 자동 수집 + 갱신 추적 부재.
- **Annual vendor review 라운드 0**.

**외부 트랙 ★**: 
- ★ Vendor risk assessment 별 epic — vendor inventory · risk register · due diligence checklist · vendor contract template 명문화.
- ★ 외부 컨설팅 (예: BitSight · SecurityScorecard · Whistic) 위탁.
- ★ 외부 firm vendor management audit.

---

## 참조

- AICPA Trust Services Criteria 2017 — CC9 Risk Mitigation.
- Lodestar 결선 자산:
  - `internal/platform/ha/patroni/` · `ha.go` · `pglock.go` (multi-region HA)
  - `internal/platform/replication/setup/` (Patroni streaming replication)
  - `internal/domain/audit/hash.go` · `audit.go` · `checkpoint.go` · `keyrotation/rotator.go` (audit chain immutability + key rotation)
  - `internal/domain/audit/rotation/cosign.go` · `backend_s3.go` · `backend_s3_enterprise.go` (cosign + S3 추상화)
  - `internal/platform/keystore/tpm/` (TPM PCR-sealed)
  - `internal/platform/llm/` (LLM 4 provider 추상화)
  - `internal/api/handlers/rbac_middleware.go` (RBAC fine-grained)
  - `go.sum` · `package-lock.json` (의존성 lockfile)
  - `.github/workflows/ci.yml` · `release-pipeline.yml` (CI/CD 통합)
  - `docs/operations/multi-region-failover-runbook.md` §13 (5 alert 대응)
  - `cmd/rosshield-audit-verify/` (fg-verify v2)
- 관련 design doc:
  - `docs/design/notes/multi-region-ha-design.md` · `multi-region-ha-stage4-design.md` (Phase 8 · 9)
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain rotation)
  - `docs/design/notes/llm-private-deployment-design.md` (LLM 4 provider)
  - `docs/design/notes/e34-tpm-design.md` (TPM)
  - `docs/design/notes/soc2-readiness-design.md` §2 · §3.3
- cross-reference: CC9.1 ↔ CC7.5 (recovery from incidents), CC9.1 ↔ A1.1 (availability capacity planning), CC9.2 ↔ CC5.2 (general controls supply chain), CC9.2 ↔ CC8.1 (vendor change management).
- 다음 단계: A1 Availability → [`a1-availability.md`](./a1-availability.md)

---

*Last updated: 2026-05-21 — Stage 11.B-3 CC9 mapping round.*
