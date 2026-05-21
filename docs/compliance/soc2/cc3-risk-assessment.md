# SOC2 CC3 — Risk Assessment

> **Trust Services Criteria**: Common Criteria (필수, 모든 SOC2 audit 적용)
> **Sub-controls**: CC3.1 ~ CC3.4 (4)
> **Status**: Lodestar 결선 자산 ~2.5/4 매핑 (~60% cover) — 외부 감사인 검증 대기. CC3.3(fraud risk)는 외부 트랙 ★ 의존이 큼, formal risk register는 design doc §리스크 섹션이 부분 cover.

CC3은 리스크 평가 — 목표 설정 · 리스크 식별 · 부정 가능성 고려 · 변화 식별 4단계를 다룹니다. 통제 활동(CC5) 설계의 입력이 되는 통제군입니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| CC3.1 Specifies Suitable Objectives | `docs/design/00-mission-and-positioning.md` · `01-principles.md` · `11-tech-stack-and-roadmap.md` · design doc §범위/비범위 | 정기 objective review 라운드 0 | 외부 firm objective audit ★ |
| CC3.2 Identifies Risks | design doc §리스크 섹션 (모든 notes/*.md) · `SECURITY.md` threat scope · audit chain immutability · threat model 부분 cover | **formal risk register 부재** | 외부 firm risk assessment ★ |
| CC3.3 Considers Potential for Fraud | RBAC fine-grained · audit chain immutability · cosign signed releases · TPM PCR-sealed · segregation of duties(부분) | **formal fraud risk assessment 라운드 부재** | ★ 외부 컨설팅 또는 internal fraud risk 라운드 |
| CC3.4 Identifies and Assesses Changes | trunk-based git history · CHANGELOG · CI/CD audit · design doc 의 D-* 식별자 · semver · cosign signed binary | 정기 change impact assessment 라운드 docs 0 | 외부 firm change audit ★ |

---

## Sub-control 상세

### CC3.1 Specifies Suitable Objectives

**Trust Services Criteria 본문 의역**: 조직은 리스크 식별과 평가가 가능하도록 충분히 구체적인 목표를 명시합니다. 목표는 운영 · 보고 · 컴플라이언스 카테고리로 분류되며, 측정 가능한 기준을 갖춥니다.

**Lodestar 매핑**:

- **Mission and positioning**:
  - `docs/design/00-mission-and-positioning.md` — Lodestar의 미션, target customer(robotics fleet operator), competition positioning.

- **Design principles**:
  - `docs/design/01-principles.md` — 12 원칙(특히 §1.1 결정론적 증거 · §1.4 멀티테넌시 · §1.9 불변성). 충돌 시 번호 작은 쪽 우선이라는 충돌 해소 룰 명시.

- **Tech stack + roadmap**:
  - `docs/design/11-tech-stack-and-roadmap.md` — Go 백엔드 + Tauri 데스크톱 + 3종 배포 타깃 · Phase 0~12 roadmap.

- **Phase-별 scope/non-goals**:
  - 모든 `docs/design/notes/*.md` design doc이 §범위 + §비범위 명시. 예: `soc2-readiness-design.md` §11에서 ISO 27001 · GDPR · pen test 실행 등 비목표 명시.

- **R 식별자 + D 식별자**:
  - 모든 design doc에 R-* (work identifier) + D-* (decision identifier) — 목표를 측정 가능한 단위로 분해.

**gap**: 정기 objective review 라운드(quarterly OKR 등) docs 0. 운영 SLA · 영업 KPI · 보안 metric을 통합한 single objective dashboard 부재.

**외부 트랙 ★**: 실 SOC2 firm 진입 시 objective 명확성 audit + 정기 review minutes 외부 검증 별도 트랙.

---

### CC3.2 Identifies Risks

**Trust Services Criteria 본문 의역**: 조직은 목표 달성에 영향을 미칠 수 있는 리스크를 조직 전체에 걸쳐 식별하고 분석합니다. 리스크는 가능성과 영향에 따라 평가됩니다.

**Lodestar 매핑**:

- **Design doc §리스크 섹션** (formal risk register 대안):
  - 모든 `docs/design/notes/*.md` 가 §리스크 표 포함. 예: `soc2-readiness-design.md` §10 — 7건 리스크 + 가능성 + 영향 + 완화책 매트릭스.
  - `audit-chain-rotation-automation-design.md` · `ros2-humble-dds-sros2-design.md` · `multi-region-ha-design.md` · `rbac-fine-grained-design.md` 등 결선 design doc 약 ~20건이 각 epic의 리스크 명문화.

- **Security threat scope**:
  - `SECURITY.md` §Scope — Lodestar 보안 정책 범위 + 제외 영역 명시. 외부 신고 채널로 unknown threat 식별 흐름.

- **Audit chain immutability + threat model 부분 cover**:
  - `internal/domain/audit/hash.go` — 결정론적 hash chain (`hash_i = sha256(prevHash ‖ payloadDigest ‖ canonicalMetaJSON)`)로 audit 위변조 리스크 차단.
  - tenant 격리 (모든 테이블 `tenant_id` 필수) — cross-tenant leak 리스크 구조적 차단.
  - RBAC fine-grained — privilege escalation 리스크 차단(role + permission + fleet scope dual layer).

- **CI/CD audit trail**:
  - `.github/workflows/*.yml` — 모든 build/test/release 경로가 GitHub Actions audit log + workflow run history로 immutable 기록.

**gap**: **공식 risk register 부재**. design doc §리스크는 epic 단위로 분산 — 전체 조직 차원의 통합 risk inventory + likelihood × impact matrix + risk owner 명시 docs 0. Phase 12 이후 별 epic 후보.

**외부 트랙 ★**: 실 SOC2 firm 진입 시 risk assessment workshop + risk register template 외부 firm 컨설팅 별도 트랙.

---

### CC3.3 Considers Potential for Fraud

**Trust Services Criteria 본문 의역**: 조직은 리스크 평가 시 부정(fraud)의 가능성을 고려합니다. 부정 동기 · 기회 · 합리화의 3 요소(fraud triangle)를 분석하고, 부정 통제를 설계합니다.

**Lodestar 매핑**:

- **RBAC fine-grained (privilege misuse 차단)**:
  - `internal/api/handlers/rbac_middleware.go` — `RequireRole` + `RequirePermission` dual layer. tenant × fleet × resource × action 4-tier scope.
  - `internal/platform/authz/` — 정밀 인가 엔진 (Phase 5 마감).

- **Audit chain immutability (부정 흔적 immutable)**:
  - `internal/domain/audit/audit.go` + `hash.go` — append-only hash chain. 모든 system action immutable 기록. UPDATE/DELETE 불가능(CLAUDE.md §하지 말 것).
  - `internal/domain/audit/checkpoint.go` — Ed25519 서명 + checkpoint epoch별 서명 분리. signer key compromise 시 epoch별 격리.
  - `internal/domain/audit/keyrotation/rotator.go` — 90일 quarterly rotation + emergency override CLI. signer key long-term compromise 리스크 완화.

- **cosign keyless signed releases (binary 위변조 방지)**:
  - `.github/workflows/release-pipeline.yml` — GitHub release binary cosign keyless 서명.
  - `internal/domain/audit/rotation/cosign.go` — audit segment archive cosign 서명 + Sigstore Rekor 등록 (외부 transparency log).
  - 38 release(v0.3.0~v0.11.0) 모두 cosign + Rekor entry.

- **TPM 2.0 PCR-sealed keystore (key extract 방지)**:
  - `internal/platform/keystore/tpm/store_linux.go` — TPM 2.0 PCR-sealed signer key (E34 Stage 1 결선). Secure Boot 위배 시 key unseal 차단.
  - `internal/enterprise/robotid/quote_attestation.go` — TPM quote attestation으로 robot identity 위변조 방지.

- **Segregation of Duties (부분 cover)**:
  - RBAC role 분리 (admin · operator · auditor 신규 예정) — 한 role이 audit + privileged 모두 수행 불가능.
  - **★ Stage 11.B-5 `auditor` role 신규 도입 시 격리 강화 — 외부 감사인 read-only + admin 변경 권한 분리 완비.**

**gap**: **formal fraud risk assessment 라운드 부재**. fraud triangle(motive · opportunity · rationalization) 분석 워크숍 0. financial fraud · IP theft · insider threat 시나리오별 통제 매핑 부재.

**외부 트랙 ★**:
- ★ 외부 컨설팅 또는 internal fraud risk 워크숍 라운드.
- ★ ACFE(Association of Certified Fraud Examiners) 가이드 기반 fraud risk assessment.
- ★ Insider threat program (CC1.4 awareness training과 연계).

---

### CC3.4 Identifies and Assesses Significant Changes

**Trust Services Criteria 본문 의역**: 조직은 내부 통제 시스템에 중대한 영향을 미칠 수 있는 변화를 식별하고 평가합니다. 변화는 외부 환경 · 비즈니스 모델 · 리더십 · 시스템 · 기술 변화를 포함합니다.

**Lodestar 매핑**:

- **Trunk-based git history**:
  - 모든 코드 변경이 main 브랜치에 trunk-based commit (CLAUDE.md §작업 컨벤션). 누가 무엇을 왜 변경했는지 immutable 기록.
  - commit message 본문 `## 추가/변경` · `## 결정·근거` · `## 테스트` 구조 강제로 변경 의도 추적 가능.

- **CHANGELOG**:
  - `CHANGELOG.md` — Keep a Changelog 형식 + semver. 모든 minor/patch release의 breaking · feature · fix 분류.
  - `docs/releases/v*.md` — 38건 release notes (v0.3.0~v0.11.0).

- **Design doc D-* 결정 식별자**:
  - `SESSION_HANDOFF.md` 결정 로그 + design doc D-P11B-* 등 — 모든 중요 결정에 날짜 + 옵션 비교 + 근거 명문화. CC1.2와 dual mapping.

- **CI/CD audit trail (변화 통제)**:
  - `.github/workflows/release-pipeline.yml` · `ci.yml` · `snap-build.yml` — 모든 build/test/release가 GitHub Actions audit log 기록.
  - workflow run history immutable.
  - cosign keyless 서명으로 release binary 진위 검증.

- **Semantic versioning + cosign signed binary**:
  - semver 강제 — breaking change는 major bump, feature는 minor bump, fix는 patch bump.
  - cosign keyless 서명 + Sigstore Rekor 등록으로 binary 변화 외부 검증 가능.

- **Migration audit**:
  - `internal/platform/db/migrations/*.up.sql` + `*.down.sql` — 모든 schema 변경이 sequential migration으로 immutable. 0037_audit_chain_keys 등.

**gap**: 정기 change impact assessment 라운드(quarterly tech debt review 등) docs 0 — small team(현 1인 founder)에서 design doc 패턴이 자연 대체. 외부 환경 변화(regulatory · market) 모니터링 절차 부재.

**외부 트랙 ★**: 실 SOC2 firm 진입 시 change management process audit + 정기 review minutes 외부 검증 별도 트랙.

---

## 참조

- AICPA Trust Services Criteria 2017 — CC3 Risk Assessment (COSO Framework 2013 Component 2 기반).
- Lodestar 결선 자산:
  - `docs/design/00-mission-and-positioning.md` · `01-principles.md` · `11-tech-stack-and-roadmap.md` (objectives)
  - `docs/design/notes/*.md` §리스크 (~20건 design doc의 리스크 섹션)
  - `SECURITY.md` (threat scope)
  - `internal/domain/audit/` (audit chain immutability)
  - `internal/api/handlers/rbac_middleware.go` · `internal/platform/authz/` (RBAC)
  - `internal/domain/audit/rotation/cosign.go` (cosign + Rekor)
  - `internal/platform/keystore/tpm/store_linux.go` (TPM PCR-sealed)
  - `internal/enterprise/robotid/quote_attestation.go` (TPM quote)
  - `internal/platform/db/migrations/*.sql` (migration audit)
  - `.github/workflows/*.yml` (CI/CD audit)
  - `CHANGELOG.md` · `docs/releases/v*.md` (change tracking)
  - `SESSION_HANDOFF.md` (decision log)
- 관련 design doc:
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain rotation)
  - `docs/design/notes/rbac-fine-grained-design.md` (RBAC)
  - `docs/design/notes/soc2-readiness-design.md` §10 (리스크 매트릭스 예시)
- 다음 단계: CC4 Monitoring Activities → [`cc4-monitoring-activities.md`](./cc4-monitoring-activities.md)

---

*Last updated: 2026-05-21 — Stage 11.B-2 CC3 mapping round.*
