# SOC2 CC8 — Change Management

> **Trust Services Criteria**: Common Criteria (필수, 모든 SOC2 audit 적용)
> **Sub-controls**: CC8.1 (1)
> **Status**: Lodestar 결선 자산 ~1/1 매핑 (~100% cover) — 외부 감사인 검증 대기. 결선 자산(trunk-based git + signed commits + cosign + CI/CD + audit chain)이 자연 cover.

CC8은 변경 관리 — 변경의 승인 · 설계 · 개발 · 테스트 · 승인 1단계를 다룹니다. 변경 자체와 변경의 안전한 도입 절차를 cover합니다. CC5(통제 활동)와 함께 가장 핵심적인 운영 통제군입니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| CC8.1 Authorizes, Designs, Develops, Tests, and Approves Changes | trunk-based git + signed commits · design doc 패턴 · TDD 강제 · CI/CD pipeline · cosign keyless signed releases · CHANGELOG · semver · audit chain immutability | emergency change 명문화 0 · 정기 change advisory board(CAB) 부재 (small team) | 외부 firm change management audit ★ |

---

## Sub-control 상세

### CC8.1 Authorizes, Designs, Develops or Acquires, Configures, Documents, Tests, Approves, and Implements Changes to Infrastructure, Data, Software, and Procedures to Meet Its Objectives

**Trust Services Criteria 본문 의역**: 조직은 인프라 · 데이터 · 소프트웨어 · 절차의 변경을 승인 · 설계 · 개발/획득 · 구성 · 문서화 · 테스트 · 승인 · 구현합니다. 모든 변경은 승인된 절차에 따라 도입되며 추적 가능합니다.

**Lodestar 매핑**:

- **변경 승인 (authorization)**:
  - **Design doc 패턴**: 1일+ 작업은 design doc 우선 (memory `feedback_design_doc_first.md`). 옵션 비교 + 트레이드오프 + 결정 항목 명문화.
  - **D-* 결정 식별자**: `docs/design/notes/*.md`의 D-P0-* · D-P11B-* 등 — 모든 중요 결정이 사용자 합의 후 명문화. 합의 없이 코드 선결정 금지 (CLAUDE.md §결정 현황).
  - **SESSION_HANDOFF.md 결정 로그**: 옵션 선택 + 근거 + 날짜 추적.

- **변경 설계 (design)**:
  - **Stage 분해**: 큰 작업은 Stage 단위 분해 + 보수적 추정 (memory `feedback_design_doc_conservative.md`).
  - **옵션 비교**: 3+ 옵션 비교 + 트레이드오프 + 권장 + 근거.
  - **R-* 식별자**: 작업 ID + Stage 명시.

- **변경 개발 (development)**:
  - **TDD 강제**: CLAUDE.md §TDD 강제 — Red → Green → Refactor. 테스트 우선 + 도메인 코드 테스트 없이 commit 금지.
  - **Trunk-based git**: 모든 변경이 main에 직접 commit. 분기 0 (CLAUDE.md §하지 말 것).
  - **Domain boundary 강제**: 도메인 서비스는 다른 도메인 저장소 직접 호출 금지. 위반 시 린트로 차단.
  - **파일/함수 크기 한도**: 파일 ≤ 400줄 권장 · 800줄 최대, 함수 ≤ 50줄, 순환 복잡도 ≤ 10.

- **변경 구성 (configuration)**:
  - **Database migration**: append-only 마이그레이션 패턴 (예: 0037 audit_chain_keys). 데이터 변경은 이전 마이그레이션 rollback 불가능 — 신규 마이그레이션으로만 진입.
  - **환경변수 분리**: 코드와 구성 분리 (`12-factor app` 패턴).
  - **테스트 환경 분리**: testcontainers + `make test`.

- **변경 문서화 (documentation)**:
  - **CHANGELOG.md**: 모든 release 변경 사항 + 영향 범위 명시 (Keep a Changelog 패턴).
  - **Commit message 본문**: `## 추가/변경` · `## 결정·근거` · `## 테스트` 구조 강제 (CLAUDE.md).
  - **Release notes**: `docs/releases/*.md` — major/minor release별 release notes.
  - **Design doc 갱신**: 설계 변경 시 design doc 갱신 + commit message §결정·근거 기록.

- **변경 테스트 (testing)**:
  - **`make ci`**: vet + test + build. `make ci` 통과 없이 main commit 0.
  - **`make test-race`**: race detector (Linux/CGO 필요).
  - **CI/CD pipeline**: `.github/workflows/ci.yml` — 모든 PR 자동 lint + test + build.
  - **`make lint`**: golangci-lint run.
  - **e2e 테스트** (해당 시): testcontainers · Playwright.
  - **migration round-trip**: 0037 등 마이그레이션 round-trip CI 검증.

- **변경 승인 (approval)**:
  - **사용자 합의 패턴**: 중요 결정은 사용자와 합의 후 docs에 기록 (CLAUDE.md §기본 방침).
  - **trunk-based + signed commits**: signed commits 가능. main 브랜치 protection rule.
  - **post-refresh hook**: refresh 후 시스템 통합 evaluation (commit `9c6bf04` timeout 120s).

- **변경 구현 (implementation)**:
  - **Trunk-based git history**: 모든 변경이 main에 immutable commit history로 기록.
  - **Semver**: semantic versioning. v0.x.x → v1.0.0 진입 정책 명문화 (`CLAUDE.md` 또는 design doc).
  - **cosign keyless signed releases**: `.github/workflows/release-pipeline.yml` + `internal/domain/audit/rotation/cosign.go` — 모든 release artifact cosign 서명 + Sigstore Rekor 투명 로그.
  - **Snap refresh + check-health hook**: snap channel별 점진 배포 (edge · beta · candidate · stable).
  - **Audit chain immutability (변경 사후 추적)**: 변경 자체가 audit event로 기록 (예: `audit.tenant.config_updated`).

- **공급망 변경 통제 (supply chain change)**:
  - **의존성 lockfile**: `go.sum` · `package-lock.json` — 의존성 hash 고정. 무단 의존성 변경 시 빌드 fail.
  - **cosign 컨테이너 이미지 서명**: OCI image 서명.
  - **Sigstore Rekor 투명 로그**: 모든 cosign 서명이 외부 검증 가능.

**gap**: 
- **Emergency change 명문화 0** — 정상 절차 우회 emergency hotfix 시 사후 review 절차 명문화 부재. 현재는 trunk-based + audit chain으로 사후 추적은 cover하나 emergency 분류 자체 명문화 0.
- **정식 Change Advisory Board (CAB) 부재** — small team 단계로 사용자 단독 의사결정. 회사 성장 시 CAB 정책 별 epic.
- **Configuration drift 자동 탐지 부재** — IaC(Terraform)는 `deploy/terraform/` 결선이나 drift detection 자동화 0.

**외부 트랙 ★**: 
- ★ 외부 firm change management audit + 정기 review.
- ★ Emergency change response 정책 별 epic.
- ★ CAB 정책 (회사 성장 후).

---

## 참조

- AICPA Trust Services Criteria 2017 — CC8 Change Management.
- Lodestar 결선 자산:
  - `CLAUDE.md` (작업 컨벤션 · TDD 강제 · 도메인 경계 규칙)
  - `docs/design/notes/*.md` (design doc 패턴)
  - `SESSION_HANDOFF.md` (결정 로그)
  - `CHANGELOG.md` (release 변경 사항)
  - `.github/workflows/ci.yml` · `release-pipeline.yml` · `snap-build.yml` · `snap-smoke.yml` (CI/CD)
  - `internal/domain/audit/rotation/cosign.go` (cosign keyless)
  - `internal/domain/audit/hash.go` · `audit.go` (audit chain immutability)
  - `cmd/rosshield-audit-verify/` (fg-verify v2)
  - `go.sum` · `package-lock.json` (의존성 lockfile)
  - `deploy/terraform/` (IaC)
  - `docs/operations/audit-chain-key-rotation.md` · `secure-boot-enrollment.md` · `snap-deployment.md` (operational runbook)
- 0037 마이그레이션: `audit_chain_keys` 테이블 (Phase 10.D-2 결선 — epoch별 public key 보존, change 사후 backward verification).
- 관련 design doc:
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain rotation)
  - `docs/design/notes/e35-refresh-rollback-redesign.md` (snap refresh rollback)
  - `docs/design/notes/soc2-readiness-design.md` §2 · §3.3
- cross-reference: CC8.1 ↔ CC5.2 (general controls technology), CC8.1 ↔ CC6.6 (key rotation as change), CC8.1 ↔ A3.3 (processing integrity through change), CC8.1 ↔ CC9.2 (vendor change supply chain).
- 다음 단계: CC9 Risk Mitigation → [`cc9-risk-mitigation.md`](./cc9-risk-mitigation.md)

---

*Last updated: 2026-05-21 — Stage 11.B-3 CC8 mapping round.*
