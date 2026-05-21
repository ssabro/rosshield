# SOC2 A2 — Confidentiality

> **Trust Services Criteria**: Additional Categories (기밀성)
> **Sub-controls**: A2.1 ~ A2.2 (2)
> **Status**: Lodestar 결선 자산 ~1.5/2 매핑 (~75% cover) — 외부 감사인 검증 대기. A2.1(분류)은 evidence redaction + tenant 격리 + TLS 결선 자산이 부분 cover, A2.2(폐기)는 audit chain immutability + retention policy 결선 자산이 자연 cover이나 formal data classification 정책 docs 부재.

A2는 기밀성 — 기밀 정보 식별 · 분류, 폐기 2단계를 다룹니다. 보안(security) 통제와는 다르게 데이터 자체의 기밀성 등급에 따른 별도 통제군입니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| A2.1 Confidential Information Classification | Evidence redaction(`internal/domain/evidence/redaction.go`) · tenant 격리 · TLS · TPM PCR-sealed · RBAC fine-grained · 보호된 데이터 격리 패턴 | **formal data classification 정책 docs 0 (public/internal/confidential/restricted 등급 미명문화)** | ★ data classification policy 별 epic 또는 외부 컨설팅 |
| A2.2 Disposal of Confidential Information | Audit chain append-only · retention policy(`docs/operations/audit-chain-key-rotation.md`) · 사용자 deprovisioning · soft delete + tenant_id filter · audit signer key rotation epoch 보존 | formal retention SLA 명문화 부족 · 정기 데이터 disposal 라운드 docs 0 | 외부 firm disposal audit ★ |

---

## Sub-control 상세

### A2.1 Identifies and Maintains Confidential Information to Meet the Entity's Objectives Related to Confidentiality

**Trust Services Criteria 본문 의역**: 조직은 기밀 정보를 식별하고 분류하여 기밀성 목표를 달성합니다. 식별된 기밀 정보는 적절한 보호 조치(분류 라벨 · 격리 · 암호화 · 접근 통제)를 받습니다.

**Lodestar 매핑**:

- **Evidence redaction (민감 정보 자동 식별)**:
  - `internal/domain/evidence/redaction.go` — 스캔 evidence 내 민감 정보(예: password · API key · 개인정보) 자동 redaction.
  - `internal/domain/evidence/redaction_test.go` — redaction 규칙 테스트.
  - 외부 disclosure(report PDF 등) 전 자동 적용.

- **Tenant 격리 (multi-tenant confidentiality)**:
  - 모든 테이블 `tenant_id` 컬럼 필수 (CLAUDE.md §하지 말 것).
  - SQL row-level tenant filter 강제 — 다른 tenant 데이터 read 불가능.
  - `internal/platform/authz/` — tenant scope 강제.

- **RBAC fine-grained (접근 통제)**:
  - `internal/api/handlers/rbac_middleware.go` — `RequirePermission(resource, action)` 정밀 인가.
  - admin · operator · auditor · custom role 차별화. auditor는 read-only(Stage 11.B-5 진입 예정).

- **전송 보호 (transit)**:
  - **TLS 강제**: 모든 HTTP/WS API endpoint TLS 강제.
  - **mTLS**: Patroni streaming replication 양방향 인증.
  - **HMAC**: webhook delivery (`internal/api/handlers/webhook.go`) HMAC signature.

- **저장 보호 (at rest)**:
  - **TPM PCR-sealed keystore**: `internal/platform/keystore/tpm/` — Secure Boot + measured boot. 변조된 부팅 환경에서 키 unsealing 불가능.
  - **bcrypt password hash**: `internal/domain/tenant/password.go` — plain text 저장 0.
  - **암호화 컬럼** (해당 시): 민감 컬럼 application-level 암호화 가능.

- **컴파일타임 통제**:
  - **CODEOWNERS** (권장): 민감 영역 reviewer 강제.

**gap**: 
- **Formal data classification 정책 docs 0** — public · internal · confidential · restricted 4-tier 분류 정책 명문화 부재. 분류 라벨 자체가 코드 0건.
- **암호화 컬럼 표준 미명문화** — 어떤 컬럼이 암호화되어야 하는지 정책 docs 0.
- **데이터 흐름 도 (data flow diagram) 부재** — confidential 데이터의 전송 · 저장 · 처리 흐름 시각화 docs 0.

**외부 트랙 ★**: 
- ★ Data classification policy 별 epic — 4-tier 분류 + 분류 라벨 + 분류별 통제 매트릭스.
- ★ 외부 컨설팅 (예: BigID · Varonis · Microsoft Purview Information Protection).
- ★ Data flow diagram 외부 검증.

---

### A2.2 Disposes of Confidential Information to Meet the Entity's Objectives Related to Confidentiality

**Trust Services Criteria 본문 의역**: 조직은 기밀 정보를 폐기하여 기밀성 목표를 달성합니다. 폐기 후 데이터 재구성이 불가능하며, 폐기 절차는 명문화 · 추적됩니다.

**Lodestar 매핑**:

- **Append-only audit chain (immutable 기록)**:
  - `internal/domain/audit/hash.go` — 결정론적 hash chain.
  - UPDATE/DELETE 불가능 (CLAUDE.md §하지 말 것).
  - 폐기 작업 자체가 audit event로 immutable 기록 → 폐기 누락 감사 가능.

- **사용자 deprovisioning (logical disposal)**:
  - **SSO group 동기**: IdP에서 사용자 비활성 시 다음 로그인 차단.
  - **API key revoke**: `internal/domain/tenant/apikey.go` — 즉시 무효화 + `audit.apikey.revoked` event emit.
  - **Session 무효화**: JWT refresh token revoke + blacklist.

- **Soft delete + tenant_id filter (data isolation disposal)**:
  - 사용자 폐기 후에도 audit event 보존을 위해 hard delete 회피.
  - tenant_id filter로 다른 tenant 데이터 read 불가능.
  - tenant 자체 폐기 시 cascade delete 또는 archive 절차 (별 epic 후보).

- **Retention policy**:
  - **`docs/operations/audit-chain-key-rotation.md`**: audit chain key rotation 절차 — epoch별 public key 보존(0037 마이그레이션), 신규 key는 90일 quarterly rotation.
  - **Audit signer key rotation epoch 보존**: 0037 마이그레이션 `audit_chain_keys` 테이블 — 폐기된 epoch의 public key는 backward verification 위해 보존.

- **물리 폐기 (physical disposal)**:
  - **★ host OS 책임** — Lodestar 소프트웨어는 logical layer만 책임. 물리 디스크 폐기는 host OS 절차(NIST SP 800-88 등) 위탁.
  - 클라우드 배포 시 cloud provider의 disposal 절차 위탁 (AWS Storage decommissioning · GCP Data deletion 등).

- **Evidence redaction (selective disposal)**:
  - `internal/domain/evidence/redaction.go` — 외부 disclosure 전 민감 정보 redaction (전체 폐기 회피하면서 기밀성 보호).

- **fg-verify v2 backward compat (폐기 후 검증)**:
  - `cmd/rosshield-audit-verify/` — 폐기된 epoch에 대해서도 backward verification 가능. 폐기 사후 무결성 검증 cover.

**gap**: 
- **Formal retention SLA 명문화 부족** — audit chain은 영구 보존(append-only)이나 비-audit 데이터(예: scan evidence · report PDF · session log) retention SLA 명문화 부재.
- **정기 데이터 disposal 라운드 docs 0** — 보존 기간 만료 데이터 정기 폐기 라운드 명문화 부재.
- **Tenant 자체 폐기 절차 명문화 부재** — tenant 계약 종료 시 cascade delete 또는 archive 절차 docs 0.
- **Right to be forgotten (GDPR)**: 본 항목은 A4 Privacy 별 cover. A2.2는 기밀성 폐기 중심.

**외부 트랙 ★**: 
- ★ 외부 firm disposal audit + 정기 disposal 라운드 외부 검증.
- ★ 물리 디스크 폐기 인증서 (NIST SP 800-88) — host OS 책임.
- ★ Tenant 종료 절차 별 epic (data export + cascade delete).

---

## 참조

- AICPA Trust Services Criteria 2017 — A2 Confidentiality (Additional Category).
- Lodestar 결선 자산:
  - `internal/domain/evidence/redaction.go` · `redaction_test.go` (민감 정보 redaction)
  - `internal/domain/evidence/evidence.go` (evidence 도메인)
  - `internal/api/handlers/rbac_middleware.go` (RBAC fine-grained)
  - `internal/platform/authz/permission_matrix.go` · `policy.go` (authz 엔진)
  - `internal/platform/keystore/tpm/` (TPM PCR-sealed)
  - `internal/domain/tenant/password.go` (bcrypt)
  - `internal/domain/tenant/apikey.go` (API key revoke)
  - `internal/api/handlers/webhook.go` (HMAC delivery)
  - `internal/platform/replication/setup/` (mTLS replication)
  - `internal/domain/audit/hash.go` · `audit.go` · `keyrotation/rotator.go` (audit chain immutability + key rotation epoch 보존)
  - `cmd/rosshield-audit-verify/` (fg-verify v2 backward compat)
  - `docs/operations/audit-chain-key-rotation.md` (retention policy)
- 0037 마이그레이션: `audit_chain_keys` 테이블 (epoch별 public key 보존 = retention 보장).
- 관련 design doc:
  - `docs/design/notes/e7-redaction-research.md` (evidence redaction)
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain rotation + retention)
  - `docs/design/notes/e34-tpm-design.md` (TPM PCR-sealed)
  - `docs/design/notes/rbac-fine-grained-design.md` (RBAC)
  - `docs/design/notes/soc2-readiness-design.md` §2 · §3.3
- cross-reference: A2.1 ↔ CC6.1·CC6.7 (logical access · transmission), A2.1 ↔ A4.3 (privacy collection/minimization), A2.2 ↔ CC6.5 (logical/physical disposal), A2.2 ↔ A4.4 (privacy retention/disposal).
- 다음 단계: A3 Processing Integrity → [`a3-processing-integrity.md`](./a3-processing-integrity.md)

---

*Last updated: 2026-05-21 — Stage 11.B-4 A2 mapping round.*
