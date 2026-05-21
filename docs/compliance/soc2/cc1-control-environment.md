# SOC2 CC1 — Control Environment

> **Trust Services Criteria**: Common Criteria (필수, 모든 SOC2 audit 적용)
> **Sub-controls**: CC1.1 ~ CC1.5 (5)
> **Status**: Lodestar 결선 자산 ~3.5/5 매핑 (~70% cover) — 외부 감사인 검증 대기. CC1.4(competence/training)는 외부 트랙 ★ 의존이 큼.

CC1은 조직의 통제 환경 — 정직성 · 거버넌스 · 책임 구조 · competence · accountability를 다룹니다. SOC2 audit의 foundation 군으로, 다른 8 통제군의 전제가 됩니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| CC1.1 Integrity and Ethical Values | `CODE_OF_CONDUCT.md` · `CONTRIBUTING.md` · `SECURITY.md` · Apache-2.0 license header | 정기 ethics review docs 0 | 외부 firm ethics audit ★ |
| CC1.2 Board Independence and Oversight | `CLAUDE.md` governance · `SESSION_HANDOFF.md` 결정 로그 · design doc D-* 식별자 | 공식 board charter docs 0 (small team) | board structure 외부 firm 검증 ★ |
| CC1.3 Organizational Structure, Authority and Responsibilities | RBAC fine-grained(`internal/api/handlers/rbac_middleware.go`) · tenant scope · CODEOWNERS(권장) | 공식 org chart docs 0 | HR org structure docs ★ |
| CC1.4 Commitment to Competence | `docs/onboarding/` · `docs/design/` 패턴 · TDD 강제 (CLAUDE.md) | **security awareness training 콘텐츠 부재** | ★ internal HR 또는 외부 트레이닝 벤더 (KnowBe4/SANS) |
| CC1.5 Accountability | Audit chain immutability(`internal/domain/audit/`) · trunk-based git history · CHANGELOG | individual KPI/performance review docs 0 | HR performance review ★ |

---

## Sub-control 상세

### CC1.1 Demonstrates Commitment to Integrity and Ethical Values

**Trust Services Criteria 본문 의역**: 조직은 정직성과 윤리적 가치에 대한 약속을 시연합니다. 행동 강령(code of conduct), 윤리 표준, 위반 보고 체계를 통해 모든 구성원이 조직의 가치를 이해하고 준수합니다.

**Lodestar 매핑**:

- **Code of Conduct**: `CODE_OF_CONDUCT.md` (top-level) — Contributor Covenant 기반. 모든 contributor에 적용.
- **Security policy**: `SECURITY.md` — 취약점 신고 채널, response SLA(72h initial · 90d fix), coordinated disclosure 절차.
- **Contributing guide**: `CONTRIBUTING.md` — PR 절차, 코드 스타일, DCO sign-off (해당 시).
- **License header**: 모든 source file Apache-2.0 헤더 적용(open-core 코어 부분).
- **CLAUDE.md governance**: `CLAUDE.md` — 작업 컨벤션 · trunk-based · TDD 강제 · 도메인 경계 규칙 등 개발 윤리 명문화.

**gap**: 정기 ethics review 라운드 docs 부재. 위반 사례 reporting log 부재(현재 GitHub Issues + private security advisory만).

**외부 트랙 ★**: 실 SOC2 firm 진입 시 외부 ethics audit 또는 internal whistleblower hotline 별도 트랙 검토 필요.

---

### CC1.2 Board of Directors Demonstrates Independence from Management and Exercises Oversight

**Trust Services Criteria 본문 의역**: 이사회 또는 거버넌스 기구가 경영진으로부터 독립적이며, 내부 통제의 개발 · 운영을 감독합니다. 책임 구조와 의사결정 권한을 명문화합니다.

**Lodestar 매핑**:

- **`CLAUDE.md` governance**: 작업 컨벤션 · 도메인 경계 규칙 · 비목표 명시. 현 단계 Phase 0~11에서 의사결정 기구 역할.
- **`SESSION_HANDOFF.md` 결정 로그**: D-P0-* · D-P5-* · D-P11B-* 형식 — 모든 중요 결정을 날짜 + 근거와 함께 기록. 추적 가능성 보장.
- **design doc 결정 식별자**: `docs/design/notes/*.md` 의 D-* 식별자(예: D-P11B-1 = 옵션 A) — 옵션 비교 + 트레이드오프 + 사용자 합의 명문화.
- **trunk-based git history**: 모든 변경이 main 브랜치에 trunk-based로 적용 — 누가 무엇을 왜 변경했는지 immutable 기록.

**gap**: small team(현 1인 founder) 단계로 공식 board of directors 없음. 외부 advisor 또는 별도 governance committee 부재.

**외부 트랙 ★**: 회사 성장 + funding round 진입 시 board structure docs + 정기 review minutes 외부 firm 검증 별도 트랙.

---

### CC1.3 Establishes Structure, Authority and Responsibilities

**Trust Services Criteria 본문 의역**: 경영진은 적절한 권한 · 책임 · reporting line을 수립합니다. 역할별 책임이 명확하고, 권한 위임이 문서화됩니다.

**Lodestar 매핑**:

- **RBAC fine-grained**: `internal/api/handlers/rbac_middleware.go` — `RequireRole(allowed ...string)` + `RequirePermission(resource, action)` dual layer. `admin` 와일드카드 `"*"` 패턴 명시 + `auditor`/`operator`/`custom` role 명시 포함 필요(주석 §lines 12-19 참조).
- **Fleet scope**: tenant × fleet × user 3-tier scope binding (Phase 5 결선). `internal/platform/authz/` 정밀 인가 엔진.
- **Tenant 격리**: 모든 테이블 `tenant_id` 컬럼 필수 (CLAUDE.md §하지 말 것 항목 일관).
- **CODEOWNERS** (권장): GitHub CODEOWNERS 파일로 코드 영역별 reviewer 자동 할당 — 별도 검토 권장.
- **CONTRIBUTING.md**: PR review 절차 · maintainer 정의.

**gap**: 공식 org chart docs(인적 reporting line) 0. 현 단계 small team으로 RBAC 모델이 곧 org structure 역할.

**외부 트랙 ★**: HR org structure docs · job description · reporting line 외부 firm 인증 시 별도 트랙.

---

### CC1.4 Demonstrates Commitment to Competence

**Trust Services Criteria 본문 의역**: 조직은 객관적 표준에 부합하는 competence를 채용 · 개발 · 유지합니다. 직무 수행에 필요한 지식 · 기술 · 능력을 확보하기 위한 교육 프로그램을 운영합니다.

**Lodestar 매핑**:

- **Onboarding docs**: `docs/onboarding/` — 신규 contributor 진입 가이드.
- **Design doc 패턴**: `docs/design/notes/*.md` — 모든 큰 작업이 design doc 우선(memory `feedback_design_doc_first.md`). 옵션 비교 + 트레이드오프 + Stage 분해 강제로 의사결정 competence 보장.
- **TDD 강제**: CLAUDE.md §TDD 강제 — Red → Green → Refactor, 테스트 없이 도메인 코드 commit 금지.
- **코드 리뷰 패턴**: PR 검토(`/review` skill) · security review(`/security-review` skill).

**gap**: **security awareness training 콘텐츠 부재 — CC1.4 cover 가장 약함**. 현재 contributor가 SOC2/보안 표준에 대한 사전 교육을 받았는지 검증 0.

**외부 트랙 ★**: 
- ★ Internal HR 트랙 또는 외부 트레이닝 벤더(KnowBe4 · SANS · Coursera 보안 코스 등) 도입.
- ★ Annual security training 의무화 + 이수 증명서 보관.
- ★ Phishing simulation 정기 라운드.

---

### CC1.5 Enforces Accountability

**Trust Services Criteria 본문 의역**: 개인은 통제 관련 책임에 대해 정직성과 정확성을 갖춰 accountable합니다. 통제 위반 시 적절한 corrective action이 시행됩니다.

**Lodestar 매핑**:

- **Audit chain immutability**: `internal/domain/audit/hash.go` + `internal/domain/audit/audit.go` — append-only hash chain(`hash_i = sha256(prevHash ‖ payloadDigest ‖ canonicalMetaJSON)`), 모든 system action을 immutable로 기록. UPDATE/DELETE 불가능(CLAUDE.md §하지 말 것).
- **trunk-based git history**: 모든 코드 변경이 main에 직접 commit + 작성자/시각/이유 기록(commit message 본문 `## 추가/변경` · `## 결정·근거` · `## 테스트` 구조 강제).
- **CHANGELOG**: `CHANGELOG.md` — 모든 release의 변경 사항 + 영향 범위 명시.
- **Audit signer key rotation**: `internal/domain/audit/keyrotation/rotator.go` — 90일 quarterly rotation으로 long-term integrity 강화. epoch별 public key 보존(0037 마이그레이션)으로 backward verification 보장.
- **fg-verify CLI**: `cmd/rosshield-audit-verify/` — 외부 감사인이 audit chain 자체 검증 가능.

**gap**: individual KPI/performance review docs 0(small team). 통제 위반 시 corrective action 절차 명문화 부재.

**외부 트랙 ★**: HR performance review · disciplinary action policy 외부 firm 인증 시 별도 트랙.

---

## 참조

- AICPA Trust Services Criteria 2017 — CC1 Control Environment (COSO Framework 2013 기반).
- Lodestar 결선 자산:
  - `CODE_OF_CONDUCT.md` · `SECURITY.md` · `CONTRIBUTING.md` (repo top-level)
  - `CLAUDE.md` (작업 컨벤션)
  - `internal/api/handlers/rbac_middleware.go` (RBAC fine-grained)
  - `internal/platform/authz/` (authz 엔진)
  - `internal/domain/audit/hash.go` · `audit.go` · `keyrotation/rotator.go` (audit chain immutability)
  - `cmd/rosshield-audit-verify/` (fg-verify v2)
  - `docs/onboarding/` · `docs/design/notes/` (competence docs)
- 관련 design doc:
  - `docs/design/notes/rbac-fine-grained-design.md` (RBAC)
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain rotation)
  - `docs/design/notes/soc2-readiness-design.md` §2.3 · §2.5 · §2.8 (fact-check)
- 다음 단계: CC2 Communication and Information → [`cc2-communication-information.md`](./cc2-communication-information.md)

---

*Last updated: 2026-05-21 — Stage 11.B-2 CC1 mapping round.*
