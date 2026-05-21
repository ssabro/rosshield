# SOC2 A5 — Security (Additional Category cross-reference)

> **Trust Services Criteria**: Additional Categories (보안 / Security)
> **Sub-controls**: A5.1 ~ A5.2 (2)
> **Status**: Lodestar 결선 자산 ~2/2 매핑 (~100% cover, CC 안에 광범위 cover) — 외부 감사인 검증 대기. **A5 Security는 Common Criteria(CC1~CC9) 안에 광범위 cover되므로 본 doc은 cross-reference 위주**.

A5는 보안 — 입력 인가 + 중요 보안 인프라 2단계를 다룹니다. 단, SOC2 Common Criteria(CC1~CC9) 자체가 "security"를 cover하므로 A5는 Common Criteria의 보강 카테고리 역할 — **상세 통제는 CC에 분산**되어 있고 본 doc은 cross-reference 매트릭스로 cover.

> **주의**: A5는 AICPA TSC에서 "security" 단독 보강 카테고리이나 실제 SOC2 audit에서는 Common Criteria가 security를 광범위 cover. 본 doc은 그 cross-reference를 명문화합니다.

---

## 매핑 매트릭스 (cross-reference 위주)

| Sub-control | Lodestar 결선 자산 | Common Criteria cross-reference | gap | 외부 트랙 |
|---|---|---|---|---|
| A5.1 Inputs are Complete, Accurate, Valid, and Authorized | 인증(SSO/SAML/OIDC · JWT · API key) · RBAC fine-grained · audit chain · API schema validation | **CC6.1**(logical access) · **CC6.2**(restricts logical access) · **CC6.3**(points of access) · **CC8.1**(change authorization) · **A3.2**(inputs complete) | (CC에 cover) | (CC와 동일) |
| A5.2 Critical Security Infrastructure | TPM PCR-sealed · Secure Boot · cosign keyless · Sigstore Rekor · audit chain immutability · audit signer key rotation 자동 · multi-region HA | **CC6.6**(logical access security measures) · **CC6.7**(transmission) · **CC6.8**(prevent malicious software) · **CC9.1**(risk mitigation) | (CC에 cover) | (CC와 동일) |

---

## Sub-control 상세

### A5.1 Inputs are Complete, Accurate, Valid, and Authorized

**Trust Services Criteria 본문 의역**: 시스템 입력은 완전 · 정확 · 유효 · 인가됩니다. 인증 · 인가 · 입력 검증 통제가 적용됩니다. (Common Criteria의 CC6 + A3.2에 광범위 cover됨.)

**Lodestar 매핑 (cross-reference)**:

- **인증 (authentication)** — **CC6.1 · CC6.3 cover**:
  - `internal/api/handlers/auth.go` — 로그인 + JWT.
  - `internal/domain/tenant/password.go` — bcrypt password hash.
  - `internal/domain/tenant/jwt.go` — JWT 발행 + 검증.
  - `internal/domain/tenant/apikey.go` — API key 인증.
  - `internal/api/handlers/sso.go` + `internal/domain/tenant/sso/` — SSO/SAML/OIDC.
  - **상세**: [`cc6-logical-physical-access.md` §CC6.1 · §CC6.3](./cc6-logical-physical-access.md).

- **인가 (authorization)** — **CC6.2 · CC1.3 cover**:
  - `internal/api/handlers/rbac_middleware.go` — `RequireRole` + `RequirePermission` dual layer.
  - `internal/platform/authz/permission_matrix.go` · `policy.go` · `decision.go` — authz 엔진.
  - tenant × fleet × user 3-tier scope.
  - **상세**: [`cc6-logical-physical-access.md` §CC6.2](./cc6-logical-physical-access.md) · [`cc1-control-environment.md` §CC1.3](./cc1-control-environment.md).

- **입력 검증 (input validation)** — **A3.2 cover**:
  - API schema validation + struct tag validator + parameterized query + HTML escape.
  - TDD 강제로 input validation 테스트 우선.
  - **상세**: [`a3-processing-integrity.md` §A3.2](./a3-processing-integrity.md).

- **변경 인가 (change authorization)** — **CC8.1 cover**:
  - Design doc 패턴 + D-* 식별자 + 사용자 합의 + trunk-based git.
  - **상세**: [`cc8-change-management.md` §CC8.1](./cc8-change-management.md).

- **Audit chain (입력 추적)** — **CC1.5 · A3.2 cover**:
  - `internal/domain/audit/hash.go` — `payloadDigest` + audit event.
  - **상세**: [`cc1-control-environment.md` §CC1.5](./cc1-control-environment.md).

**gap**: CC + A3에 cover. 본 doc은 cross-reference 매트릭스로 cover.

**외부 트랙 ★**: CC + A3와 동일.

---

### A5.2 Critical Security Infrastructure

**Trust Services Criteria 본문 의역**: 조직은 통제 자산을 보호하는 중요 보안 인프라를 운영합니다. 키 관리 · 무결성 검증 · 공급망 보안 · 인프라 보안이 포함됩니다. (Common Criteria의 CC6.6~CC6.8 + CC9.1에 광범위 cover됨.)

**Lodestar 매핑 (cross-reference)**:

- **TPM PCR-sealed keystore** — **CC6.6 · CC6.7 cover**:
  - `internal/platform/keystore/tpm/` — Secure Boot + measured boot.
  - `docs/operations/secure-boot-enrollment.md` — 운영 절차.
  - **상세**: [`cc6-logical-physical-access.md` §CC6.6 · §CC6.7](./cc6-logical-physical-access.md).

- **cosign keyless signed releases + Sigstore Rekor** — **CC6.8 · CC8.1 cover**:
  - `.github/workflows/release-pipeline.yml` + `internal/domain/audit/rotation/cosign.go`.
  - 모든 release artifact 서명 + 외부 검증 가능.
  - **상세**: [`cc6-logical-physical-access.md` §CC6.8](./cc6-logical-physical-access.md) · [`cc8-change-management.md` §CC8.1](./cc8-change-management.md).

- **Audit chain immutability + Ed25519 서명 checkpoint** — **CC6.6 · A3.3 · A3.4 cover**:
  - `internal/domain/audit/hash.go` · `audit.go` · `checkpoint.go` · `key_epoch.go`.
  - UPDATE/DELETE 불가능. 외부 감사인 자체 검증 가능.
  - **상세**: [`cc6-logical-physical-access.md` §CC6.6](./cc6-logical-physical-access.md) · [`a3-processing-integrity.md` §A3.3 · §A3.4](./a3-processing-integrity.md).

- **Audit signer key rotation 자동 (90일 quarterly)** — **CC6.6 · CC8.1 cover**:
  - `internal/domain/audit/keyrotation/rotator.go` — 자동 rotation.
  - 0037 마이그레이션 `audit_chain_keys` 테이블 — epoch별 public key 보존, backward verification 보장.
  - **상세**: [`cc6-logical-physical-access.md` §CC6.6](./cc6-logical-physical-access.md).

- **Multi-region HA + Patroni 자동 failover** — **CC7.5 · CC9.1 · A1.1 · A1.2 cover**:
  - `internal/platform/ha/patroni/` — RTO ≤ 60s.
  - **상세**: [`cc7-system-operations.md` §CC7.5](./cc7-system-operations.md) · [`cc9-risk-mitigation.md` §CC9.1](./cc9-risk-mitigation.md) · [`a1-availability.md`](./a1-availability.md).

- **fg-verify v2 CLI** — **CC4.1 · A3.4 cover**:
  - `cmd/rosshield-audit-verify/` — 외부 검증 backward compat.
  - **상세**: [`cc4-monitoring-activities.md` §CC4.1](./cc4-monitoring-activities.md) · [`a3-processing-integrity.md` §A3.4](./a3-processing-integrity.md).

- **TLS · mTLS** — **CC6.7 cover**:
  - 모든 HTTP/WS API TLS + replication mTLS.
  - **상세**: [`cc6-logical-physical-access.md` §CC6.7](./cc6-logical-physical-access.md).

- **CI/CD + 의존성 lockfile** — **CC5.2 · CC8.1 cover**:
  - `.github/workflows/ci.yml` + `release-pipeline.yml` + `go.sum` + `package-lock.json`.
  - **상세**: [`cc5-control-activities.md` §CC5.2](./cc5-control-activities.md) · [`cc8-change-management.md` §CC8.1](./cc8-change-management.md).

- **Prometheus + Grafana + 5 alert rule** — **CC4.1 · CC7.1 · CC7.2 cover**:
  - 보안 인프라 모니터링.
  - **상세**: [`cc4-monitoring-activities.md` §CC4.1](./cc4-monitoring-activities.md) · [`cc7-system-operations.md` §CC7.1 · §CC7.2](./cc7-system-operations.md).

**gap**: CC + A1 + A3에 cover. 본 doc은 cross-reference 매트릭스로 cover.

**외부 트랙 ★**: CC + A1 + A3와 동일.

---

## A5 ↔ Common Criteria cross-reference 종합 매트릭스

A5 통제 자산은 Common Criteria에 광범위 cover됩니다. 본 종합 매트릭스로 한눈에 확인:

| A5 자산 | CC1 | CC2 | CC3 | CC4 | CC5 | CC6 | CC7 | CC8 | CC9 | A1 | A2 | A3 | A4 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| RBAC fine-grained | CC1.3 | | | | CC5.1·CC5.2 | CC6.1·CC6.2·CC6.3 | | | | | A2.1 | A3.2 | A4.5 |
| SSO/SAML/OIDC | | | | | CC5.1 | CC6.1·CC6.3 | | | | | | | A4.7 |
| JWT · API key · password | | | | | CC5.1 | CC6.1·CC6.3·CC6.6 | | | | | A2.1 | A3.2 | |
| Audit chain immutability | CC1.5 | CC2.1 | CC3.2 | CC4.1 | CC5.1 | CC6.5·CC6.6·CC6.8 | CC7.2·CC7.3 | CC8.1 | CC9.1 | A1.2 | A2.2 | A3.1·A3.2·A3.3·A3.4 | A4.4·A4.8 |
| Ed25519 checkpoint | CC1.5 | CC2.1 | | CC4.1 | | CC6.6 | | CC8.1 | | A1.2 | | A3.4 | |
| Audit signer key rotation | | | | CC4.1 | CC5.2 | CC6.6 | CC7.2 | CC8.1 | CC9.1 | A1.2 | A2.2 | A3.3 | A4.4 |
| TPM PCR-sealed + Secure Boot | | | | | CC5.1 | CC6.6·CC6.7·CC6.8 | | | CC9.1 | | A2.1 | | |
| cosign keyless + Rekor | | CC2.3 | | | CC5.1·CC5.2 | CC6.7·CC6.8 | | CC8.1 | CC9.1·CC9.2 | A1.2 | | A3.4 | |
| TLS · mTLS | | CC2.1 | | | CC5.1 | CC6.1·CC6.7 | | | | | A2.1 | | |
| Multi-region HA + Patroni | | | | CC4.1 | | | CC7.2·CC7.5 | | CC9.1 | A1.1·A1.2 | | A3.3 | |
| Prometheus + Grafana + alert | | CC2.1 | | CC4.1 | | | CC7.1·CC7.2·CC7.3 | | | A1.1 | | | A4.8 |
| Webhook delivery + HMAC | | CC2.3 | | CC4.2 | | CC6.7 | CC7.3·CC7.4 | | | | A2.1 | | A4.6·A4.8 |
| fg-verify v2 + audit export | CC1.5 | | | CC4.1 | | | | CC8.1 | | A1.2 | | A3.4 | A4.5 |
| RBAC `auditor` role (Stage 11.B-5) | | | | | | CC6.2 | | | | | | A3.4 | A4.5 |
| trunk-based + signed commits + CI/CD | | | CC3.4 | | CC5.2 | | | CC8.1 | | | | | |
| Evidence redaction | | | | | | CC6.7 | | | | | A2.1 | | A4.3·A4.4 |
| Tenant 격리 (tenant_id 필수) | CC1.3 | | | | CC5.1 | CC6.1·CC6.2·CC6.5 | | | | | A2.1·A2.2 | A3.2 | A4.3·A4.5 |
| Design doc + D-* 식별자 + handoff | CC1.2 | CC2.2 | CC3.1·CC3.4 | | CC5.3 | | | CC8.1 | | | | A3.1 | |
| CLAUDE.md + TDD 강제 + 도메인 경계 | CC1.4 | CC2.2 | | | CC5.2·CC5.3 | | | CC8.1 | | | | A3.3 | |
| CHANGELOG + SECURITY.md + CoC | CC1.1 | CC2.2·CC2.3 | | | CC5.3 | | | CC8.1 | | | | | A4.6 |
| `multi-region-failover-runbook.md` | | CC2.2 | | CC4.1·CC4.2 | CC5.3 | | CC7.4·CC7.5 | | CC9.1 | A1.1·A1.2 | | | |

---

## 참조

- AICPA Trust Services Criteria 2017 — A5 Security (Additional Category, 단, Common Criteria가 광범위 cover).
- Lodestar 결선 자산: A5는 cross-reference 위주 — 상세 자산은 CC1~CC9 + A1~A4 docs 참조.
- 관련 design doc:
  - `docs/design/01-principles.md` §원칙 1·6·8·9·11 (감사인 증거 · 결정론 · 컨텐츠/코드 분리 · 데이터 불변성 · 설명 가능성)
  - `docs/design/notes/audit-chain-rotation-automation-design.md`
  - `docs/design/notes/rbac-fine-grained-design.md`
  - `docs/design/notes/e34-tpm-design.md`
  - `docs/design/notes/multi-region-ha-design.md` · `e25-ha-design.md`
  - `docs/design/notes/soc2-readiness-design.md` §2 · §3.3
- cross-reference: A5.1 → CC6.1·CC6.2·CC6.3·CC8.1·A3.2 / A5.2 → CC6.6·CC6.7·CC6.8·CC9.1·A1.1·A1.2.
- **A5는 cross-reference 위주** — 상세 매핑은 Common Criteria + A1~A4 docs 참조.
- 본 round 마감 — Stage 11.B-3 + 11.B-4 완료. 다음 단계: Stage 11.B-5 (`auditor` role 신규 + audit export bundle 진입 예정).

---

*Last updated: 2026-05-21 — Stage 11.B-4 A5 mapping round (cross-reference 위주, CC + A1~A4가 광범위 cover).*
