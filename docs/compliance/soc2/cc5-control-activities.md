# SOC2 CC5 — Control Activities

> **Trust Services Criteria**: Common Criteria (필수, 모든 SOC2 audit 적용)
> **Sub-controls**: CC5.1 ~ CC5.3 (3)
> **Status**: Lodestar 결선 자산 ~2.5/3 매핑 (~85% cover) — 외부 감사인 검증 대기. CC5.1(통제 활동 선정)과 CC5.2(기술 일반 통제)는 RBAC fine-grained + audit chain + cosign 자산이 자연 cover, CC5.3(정책·절차 배포)은 CLAUDE.md + design doc 패턴이 부분 cover하나 정식 정책 문서 카탈로그 부재.

CC5는 통제 활동 — 통제 활동의 선정·개발, 기술 일반 통제의 선정·개발, 정책과 절차를 통한 배포 3단계를 다룹니다. CC3(리스크 평가)의 결과를 받아 실 통제 메커니즘으로 전환하는 통제군입니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| CC5.1 Selects and Develops Control Activities | RBAC fine-grained(role × permission × fleet scope) · audit chain immutability · cosign signed releases · TPM PCR-sealed · trunk-based git · TDD 강제 | 통제 활동 카탈로그 단일 docs 0 (CC1~CC9·A1~A5 trees가 곧 카탈로그) | 외부 firm 통제 활동 cover audit ★ |
| CC5.2 Selects and Develops General Controls Over Technology | RBAC `permission_matrix.go` · `policy.go` · 0037 마이그레이션 · CI/CD pipeline · cosign keyless · 의존성 lockfile(go.sum) · `make ci` | SBOM 자동 발행 부재(권장 진입 예정) · 의존성 취약점 정기 스캔 docs 0 | 외부 firm 기술 통제 audit ★ |
| CC5.3 Deploys Through Policies and Procedures | CLAUDE.md(작업 컨벤션) · CONTRIBUTING.md · SECURITY.md · design doc 패턴 · runbook(`docs/operations/`) | **정식 정책 카탈로그(InfoSec policy · acceptable use · data classification 등) 부재** | ★ 정책 문서 카탈로그 별 epic 또는 외부 컨설팅 |

---

## Sub-control 상세

### CC5.1 Selects and Develops Control Activities

**Trust Services Criteria 본문 의역**: 조직은 리스크를 수용 가능한 수준으로 완화하기 위한 통제 활동을 선정하고 개발합니다. 통제 활동은 식별된 리스크에 대응하며, 다양한 통제 유형(예방·탐지·정정)이 적절히 조합됩니다.

**Lodestar 매핑**:

- **예방 통제 (preventive controls)**:
  - **RBAC fine-grained**: `internal/api/handlers/rbac_middleware.go` — `RequireRole(allowed ...string)` + `RequirePermission(resource, action)` dual layer. admin write 차단 + auditor write 차단 + custom role 위임으로 권한 분리.
  - **TPM PCR-sealed keystore**: `internal/platform/keystore/tpm/` — Secure Boot + measured boot. 변조된 부팅 환경에서 키 unsealing 불가능.
  - **cosign keyless signed releases**: `.github/workflows/release-pipeline.yml` + `internal/domain/audit/rotation/cosign.go` — Sigstore Rekor 투명 로그로 미서명 binary 배포 차단.
  - **TLS · mTLS**: HTTP/WS 전송 보호. 모든 API endpoint TLS 강제.

- **탐지 통제 (detective controls)**:
  - **Audit chain immutability**: `internal/domain/audit/hash.go` — `hash_i = sha256(prevHash ‖ payloadDigest ‖ canonicalMetaJSON)` 결정론적 chain. tamper 시 chain break 검출 가능.
  - **Prometheus + 5 alert rule**: `deploy/prometheus/alerts/multi-region.yml` — replication lag · HA failover · audit chain 이상 자동 탐지.
  - **fg-verify v2 CLI**: `cmd/rosshield-audit-verify/` — 외부 감사인 자체 검증으로 통제 위반 사후 탐지.

- **정정 통제 (corrective controls)**:
  - **Multi-region HA + Patroni 자동 failover**: `internal/platform/ha/patroni/` — RTO ≤ 60s 자동 정정 (Phase 9 E25).
  - **Audit key rotation 자동 (90일 quarterly)**: `internal/domain/audit/keyrotation/rotator.go` + 0037 마이그레이션 — long-term integrity 정정.
  - **Snap refresh + check-health hook**: post-refresh 자동 검증 (commit `9c6bf04`).

- **개발 프로세스 통제**:
  - **TDD 강제**: CLAUDE.md §TDD 강제 — Red → Green → Refactor. 도메인 코드 commit 전 테스트 우선.
  - **Trunk-based git**: 모든 변경이 main에 직접 commit, 분기 0 → 변경 사후 추적 가능.

**gap**: 통제 활동을 단일 docs로 카탈로그화한 자료 부재 (본 `docs/compliance/` 트리 전체가 곧 카탈로그 역할이나 통제 ID → 통제 활동 매트릭스 단일 docs는 별 epic 후보).

**외부 트랙 ★**: 실 SOC2 firm 진입 시 통제 활동 cover audit + 통제 활동 효과성 90일 운영 측정.

---

### CC5.2 Selects and Develops General Controls Over Technology

**Trust Services Criteria 본문 의역**: 조직은 목표 달성을 지원하기 위해 기술 일반 통제(general controls over technology)를 선정하고 개발합니다. 인프라 · 보안 관리 · 소프트웨어 개발 수명주기 · 시스템 운영에 걸친 통제군입니다.

**Lodestar 매핑**:

- **권한 통제 (access management)**:
  - **RBAC matrix**: `internal/platform/authz/permission_matrix.go` + `policy.go` — 최소권한 원칙(least privilege) 적용. tenant × fleet × user 3-tier scope binding (Phase 5).
  - **SSO/SAML/OIDC**: `internal/api/handlers/sso.go` + `internal/domain/tenant/sso/` — group → role 자동 매핑.

- **변경 통제 (change management)**:
  - **Trunk-based + signed commits**: CLAUDE.md §작업 컨벤션 — 모든 변경이 main 직접 commit + commit message §결정·근거 강제 + signed commits 가능.
  - **Database migration**: 0037 마이그레이션 등 — append-only 마이그레이션 패턴.
  - **CI/CD pipeline**: `.github/workflows/ci.yml` + `release-pipeline.yml` + `snap-build.yml` — `make ci` (vet + test + build) 자동 검증.

- **소프트웨어 개발 수명주기 (SDLC)**:
  - **Design doc 패턴**: `docs/design/notes/*.md` — 1일+ 작업은 design doc 우선 (memory `feedback_design_doc_first.md`). 옵션 비교 + 트레이드오프 + Stage 분해.
  - **TDD 강제**: CLAUDE.md §TDD 강제.
  - **PR review 패턴**: CONTRIBUTING.md + `/review` skill + `/security-review` skill.

- **인프라 통제 (infrastructure controls)**:
  - **Multi-region replication**: `internal/platform/replication/setup/` — Patroni streaming replication.
  - **HA leader election**: `internal/platform/ha/ha.go` + `pglock.go` (PostgreSQL advisory lock 기반 단일 leader).
  - **Audit signer key rotation**: `internal/domain/audit/keyrotation/rotator.go` — 90일 자동 rotation.

- **공급망 통제 (supply chain)**:
  - **cosign keyless signed releases + Sigstore Rekor**: tamper 방지.
  - **의존성 lockfile**: `go.sum` · `package-lock.json` — 의존성 고정 + 변경 시 PR review.
  - **컨테이너 이미지 서명**: cosign으로 OCI image 서명 (Phase 7 cosign 결선).

**gap**: 
- **SBOM (Software Bill of Materials) 자동 발행 부재** — `release-pipeline.yml`에 SBOM 단계 진입 권장 (별 epic 후보).
- **의존성 취약점 정기 스캔 docs 0** — Dependabot · Trivy · Snyk 등 통합 권장.

**외부 트랙 ★**: 실 SOC2 firm 진입 시 기술 일반 통제 audit + 공급망 보안 별 트랙.

---

### CC5.3 Deploys Through Policies and Procedures

**Trust Services Criteria 본문 의역**: 조직은 통제 활동을 정책과 절차를 통해 배포합니다. 정책은 기대치를 명문화하고 절차는 정책 시행을 보장합니다. 정책과 절차는 정기 review · 갱신됩니다.

**Lodestar 매핑**:

- **개발 정책 (development policies)**:
  - **CLAUDE.md**: 작업 컨벤션 · trunk-based · TDD 강제 · 도메인 경계 규칙 · 파일/함수 크기 한도 · 불변성 정책.
  - **CONTRIBUTING.md**: PR 절차 · 코드 스타일 · review 절차.

- **보안 정책 (security policies)**:
  - **SECURITY.md**: 취약점 신고 채널 · response SLA · disclosure 절차.
  - **Coordinated disclosure**: §Response SLA 72h initial + 90d fix.

- **운영 정책 (operational policies)**:
  - **Runbook**: `docs/operations/multi-region-failover-runbook.md` · `audit-chain-key-rotation.md` · `secure-boot-enrollment.md` 등 — 통제 활동 시행 절차 명문화.
  - **CHANGELOG**: `CHANGELOG.md` — 모든 release 변경 사항.

- **거버넌스 정책**:
  - **Design doc D-* 식별자**: `docs/design/notes/*.md`의 D-P0-* · D-P11B-* 등 — 모든 중요 결정 명문화.
  - **SESSION_HANDOFF.md 결정 로그**: 옵션 선택 + 근거 추적 가능.

- **윤리 정책**:
  - **CODE_OF_CONDUCT.md**: Contributor Covenant.

**gap**: 
- **정식 정책 카탈로그 부재** — InfoSec policy · acceptable use policy · data classification policy · incident response policy · access control policy · BYOD policy · data retention policy 등 외부 firm 요구 정책 문서 카탈로그 0. 본 트리는 SOC2 매핑 cover하나 정책 문서 자체는 별 epic 진입 필요.
- 정책 정기 review 라운드 docs 0 (small team).

**외부 트랙 ★**: 
- ★ 정책 문서 카탈로그 별 epic 또는 외부 컨설팅 firm 위탁.
- ★ 정기 정책 review 라운드 외부 검증.

---

## 참조

- AICPA Trust Services Criteria 2017 — CC5 Control Activities (COSO Framework 2013 Component 3 기반).
- Lodestar 결선 자산:
  - `internal/api/handlers/rbac_middleware.go` (RBAC fine-grained)
  - `internal/platform/authz/permission_matrix.go` · `policy.go` · `decision.go` (authz 엔진)
  - `internal/domain/audit/hash.go` · `audit.go` · `keyrotation/rotator.go` (audit chain immutability + key rotation)
  - `internal/domain/audit/rotation/cosign.go` (cosign signed releases)
  - `internal/platform/keystore/tpm/` (TPM PCR-sealed)
  - `.github/workflows/ci.yml` · `release-pipeline.yml` · `snap-build.yml` (CI/CD)
  - `internal/platform/ha/patroni/` (multi-region HA)
  - `internal/platform/replication/setup/` (Patroni streaming replication)
  - `CLAUDE.md` · `CONTRIBUTING.md` · `SECURITY.md` · `CODE_OF_CONDUCT.md` (정책 문서)
  - `docs/operations/*.md` (runbook)
- 관련 design doc:
  - `docs/design/notes/rbac-fine-grained-design.md` (RBAC)
  - `docs/design/notes/rbac-fleet-scope-precision-design.md` (fleet scope)
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain rotation)
  - `docs/design/notes/soc2-readiness-design.md` §2 · §3.3 (fact-check 매트릭스)
- cross-reference: CC5.1 ↔ CC6.1·CC6.2 (logical access), CC5.2 ↔ CC8.1 (change management), CC5.3 ↔ CC2.2 (internal communication).
- 다음 단계: CC6 Logical and Physical Access → [`cc6-logical-physical-access.md`](./cc6-logical-physical-access.md)

---

*Last updated: 2026-05-21 — Stage 11.B-3 CC5 mapping round.*
