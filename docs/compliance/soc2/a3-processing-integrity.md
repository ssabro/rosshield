# SOC2 A3 — Processing Integrity

> **Trust Services Criteria**: Additional Categories (처리 무결성)
> **Sub-controls**: A3.1 ~ A3.4 (4)
> **Status**: Lodestar 결선 자산 ~3.5/4 매핑 (~88% cover) — 외부 감사인 검증 대기. 결선 자산(scan engine determinism · audit chain hash chain · PDF cosign · fg-verify v2)이 자연 cover. Lodestar의 핵심 가치 명제(audit-grade evidence)와 가장 강하게 정렬되는 통제군.

A3는 처리 무결성 — 처리 무결성 목표 명시, 입력의 완전성/정확성/유효성/인가, 처리의 완전성/정확성/유효성/적시성, 출력의 완전성/정확성/유효성/배포 4단계를 다룹니다. 결정론적 처리 + audit chain + 서명 출력이 핵심.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| A3.1 Specifies Processing Integrity Objectives | `docs/design/01-principles.md` §원칙 1·9 · scan engine determinism · audit chain immutability · 설계 원칙 12개 | formal processing integrity SLA 명문화 부족 | 외부 firm objective audit ★ |
| A3.2 Inputs are Complete, Accurate, Valid, and Authorized | Scan engine determinism · audit chain `payloadDigest` · RBAC fine-grained · API schema validation · input sanitization | input validation 정책 단일 docs 0 | 외부 firm input audit ★ |
| A3.3 Processing is Complete, Accurate, Valid, Timely | Scan/evidence/audit chain hash chain · `canonicalMetaJSON` 알파벳순 직렬화 · 결정론적 fallback (원칙 6) · check-health hook · TDD 강제 | 처리 timing SLA 명문화 부족 (스캔 timeout 등) | 외부 firm processing audit ★ |
| A3.4 System Output is Complete, Accurate, Valid, Distributed | PDF signature(`internal/domain/reporting/pdf/`) · cosign keyless signed releases · fg-verify v2 · Ed25519 checkpoint · audit export bundle 서명(Stage 11.B-5) | report 배포 추적 audit event 부분 cover | 외부 firm output audit ★ |

---

## Sub-control 상세

### A3.1 Obtains or Generates, Uses, and Communicates Relevant, Quality Information Regarding the Objectives Related to Processing, Including Definitions of Data Processed and Product and Service Specifications

**Trust Services Criteria 본문 의역**: 조직은 처리 목표(완전성 · 정확성 · 유효성 · 적시성)를 정의하고, 데이터 처리 사양 및 제품 사양과 연관시킵니다. 처리 무결성 목표가 명문화 · 통신됩니다.

**Lodestar 매핑**:

- **설계 원칙 (processing integrity objectives)**:
  - **`docs/design/01-principles.md` §원칙 1** — "감사인이 받아들이는 증거" = 결정론적 + 해시 체인 + 외부 검증.
  - **§원칙 6** — "결정론적 fallback" = AI는 규칙 기반의 보조.
  - **§원칙 9** — "데이터 불변성" = append-only.
  - **§원칙 11** — "설명 가능성" = 모든 AI 판단에 reasoning trace.

- **Audit chain hash chain (정확성 + 적시성)**:
  - `internal/domain/audit/hash.go` — `hash_i = sha256(prevHash ‖ payloadDigest ‖ canonicalMetaJSON)` 결정론적 hash chain.
  - 모든 처리 단계가 audit event로 immutable 기록.

- **Scan engine determinism**:
  - `internal/domain/scan/` — 결정론적 스캔 엔진. 동일 입력 → 동일 출력 보장.
  - 비결정 영역(예: timestamp · UUID)은 별도 분리 + 명문화.

- **Data processed 정의**:
  - **Domain model**: `internal/domain/*/` 도메인별 엔터티 명문화.
  - **OpenAPI** (Stage 0.3-β 또는 E9 진입 예정): API schema 명세.

- **사용자 합의 패턴 (objective communication)**:
  - **D-* 결정 식별자**: 모든 중요 결정이 사용자 합의 후 docs 기록.
  - **SESSION_HANDOFF.md**: 옵션 선택 + 근거.

**gap**: 
- **Formal processing integrity SLA 명문화 부족** — "스캔 결과 결정론적 0 변형" 등 정량 SLA 명문화 0.
- **OpenAPI spec 미완성** — Stage 0.3-β 또는 E9에서 진입 예정.

**외부 트랙 ★**: 외부 firm processing integrity objective audit.

---

### A3.2 Implements Policies and Procedures Over System Inputs, Including Controls Over Completeness, Accuracy, and Validity, to Result in Products, Services, and Reporting to Meet the Entity's Objectives

**Trust Services Criteria 본문 의역**: 조직은 시스템 입력에 대한 정책과 절차를 시행합니다. 완전성 · 정확성 · 유효성 · 인가에 대한 통제가 적용되어 제품 · 서비스 · 보고가 목표를 달성합니다.

**Lodestar 매핑**:

- **입력 검증 (input validation)**:
  - **API schema validation**: HTTP handler 진입 시 JSON schema 검증 (`encoding/json` + struct tag 또는 validator).
  - **Input sanitization**: SQL injection 방어 (parameterized query 강제, `database/sql` 또는 sqlx).
  - **HTML escape**: XSS 방어 (Go template auto-escape).

- **인가 (authorized inputs)**:
  - **RBAC fine-grained**: `internal/api/handlers/rbac_middleware.go` — `RequirePermission(resource, action)` — 인가된 사용자만 입력 가능.
  - **Tenant 격리**: tenant_id 필수 — 다른 tenant 데이터 read/write 차단.
  - **API key scope binding**: machine-to-machine 입력 scope 제한.

- **완전성 (completeness)**:
  - **Required fields**: API schema에 필수 필드 명시.
  - **Default values**: optional 필드 default 명문화.
  - **Audit chain**: 입력 audit event로 기록 — 누락 사후 추적 가능.

- **정확성 (accuracy)**:
  - **Type safety**: Go strong typing.
  - **Validation rules**: domain entity 생성 시 validation 강제.
  - **TDD 강제**: 입력 validation 테스트 우선.

- **payloadDigest (입력 무결성)**:
  - `internal/domain/audit/hash.go` — 입력 payload의 결정론적 hash digest. 입력 변조 사후 탐지.

- **Idempotency** (해당 시):
  - 멱등 키(`Idempotency-Key` 헤더) 패턴 — 중복 처리 방어.

- **Webhook delivery HMAC (외부 입력 인증)**:
  - `internal/api/handlers/webhook.go` — outbound 시 HMAC signature. 외부 incoming webhook도 HMAC 검증 패턴.

**gap**: 
- **Input validation 정책 단일 docs 0** — 모든 endpoint별 입력 검증 규칙이 코드에 분산. 단일 정책 docs 부재.
- **Rate limiting 정책 명문화 부족** (해당 시).

**외부 트랙 ★**: 외부 firm input validation audit + 펜테스트 ★.

---

### A3.3 Implements Policies and Procedures Over System Processing to Result in Products, Services, and Reporting to Meet the Entity's Objectives

**Trust Services Criteria 본문 의역**: 조직은 시스템 처리에 대한 정책과 절차를 시행합니다. 완전성 · 정확성 · 유효성 · 적시성에 대한 통제가 적용됩니다.

**Lodestar 매핑**:

- **결정론적 처리 (deterministic processing)**:
  - **Scan engine**: `internal/domain/scan/` — 결정론적 스캔. 동일 입력 → 동일 출력.
  - **canonicalMetaJSON 알파벳순 직렬화**: `internal/domain/audit/hash.go` — JSON key 알파벳순 직렬화 강제 → hash 결정론.
  - **결정론적 fallback (원칙 6)**: AI는 규칙 기반의 보조. AI 결과 비결정 시 규칙 fallback.

- **처리 무결성 (processing integrity)**:
  - **Hash chain**: `internal/domain/audit/hash.go` — `hash_i = sha256(prevHash ‖ payloadDigest ‖ canonicalMetaJSON)`. 처리 단계가 chain으로 연결.
  - **Ed25519 서명 checkpoint**: `internal/domain/audit/checkpoint.go` — 처리 결과 외부 검증 가능.
  - **`signer_key_id` + `key_epoch` 컬럼**: 0037 마이그레이션 — 처리 시점의 키 epoch 기록.

- **완전성 (processing completeness)**:
  - **Transaction**: PostgreSQL transaction — 처리 atomicity 보장.
  - **Audit event emit 일관성**: 처리 결과 + audit event를 동일 transaction에서 emit.

- **정확성 (processing accuracy)**:
  - **TDD 강제**: 도메인 로직 테스트 우선.
  - **race detector**: `make test-race` (Linux/CGO).
  - **Go vet**: `make vet`.

- **적시성 (processing timeliness)**:
  - **Post-refresh check-health hook**: 120s timeout (commit `9c6bf04`).
  - **Patroni leader election**: 단일 leader로 처리 순서 보장.
  - **Replication lag metric**: `RosshieldReplicationLagWarning` (30s) · `Critical` (60s) — 처리 적시성 SLA 모니터링.

- **운영 처리 통제**:
  - **5 alert rule**: 처리 이상 자동 탐지.
  - **`/healthz` endpoint**: 처리 가용성 외부 확인.

**gap**: 
- **처리 timing SLA 명문화 부족** — 스캔 timeout · report 생성 timeout · LLM 호출 timeout 등 명문화 분산.
- **Idempotency 정책 미명문화** (해당 시).

**외부 트랙 ★**: 외부 firm processing audit + 정기 처리 효과성 측정.

---

### A3.4 Implements Policies and Procedures to Make Available or Deliver Output Completely, Accurately, and Timely in Accordance with Specifications to Meet the Entity's Objectives

**Trust Services Criteria 본문 의역**: 조직은 출력을 완전 · 정확 · 적시 전달하는 정책과 절차를 시행합니다. 출력 사양에 부합하며, 출력 배포가 추적됩니다.

**Lodestar 매핑**:

- **PDF signature (서명 출력)**:
  - `internal/domain/reporting/pdf/` — report PDF 생성.
  - PAdES (PDF Advanced Electronic Signatures) 또는 Ed25519 서명 — report 무결성 검증 가능.
  - `internal/domain/reporting/bundle.go` · `framework_bundle.go` — bundle 생성.

- **cosign keyless signed releases (출력 무결성)**:
  - `.github/workflows/release-pipeline.yml` + `internal/domain/audit/rotation/cosign.go` — 모든 release artifact cosign 서명.
  - Sigstore Rekor 투명 로그 — 외부 검증 가능.

- **fg-verify v2 CLI (출력 검증)**:
  - `cmd/rosshield-audit-verify/` — 외부 감사인이 audit bundle 자체 검증.
  - v2 bundle `_bundleVersion: "v2"` + `_chainKeyEpochs`(epoch별 public key) backward compat.

- **Ed25519 서명 checkpoint (audit output)**:
  - `internal/domain/audit/checkpoint.go` — Ed25519 서명 + `audit_checkpoints` 테이블 `signer_key_id` 컬럼.
  - 외부 감사인 자체 검증 가능.

- **Audit export bundle 서명 (Stage 11.B-5 진입 예정)**:
  - `GET /api/v1/compliance/auditor-bundle?period=90d` — audit_entries + cosign signatures + chain_keys(epoch별 public key) + 통제별 evidence index + README.
  - cosign 서명 + 외부 검증 가능.

- **Report 배포 추적**:
  - `internal/api/handlers/report.go` — report download endpoint + audit event `audit.report.downloaded` (해당 시).
  - Webhook delivery: `internal/api/handlers/webhook.go` — 외부 통보 + HMAC.

- **CHANGELOG (release 출력 명세)**:
  - 모든 release 변경 사항 명시 (Keep a Changelog 패턴).

- **Snap channel별 점진 배포**:
  - edge · beta · candidate · stable channel — 출력 배포 순서 통제.

**gap**: 
- **Report 배포 추적 audit event 부분 cover** — download event는 있으나 외부 disclosure recipient 추적 부재.
- **Customer notification SLA 명문화 부족** (해당 시).

**외부 트랙 ★**: 외부 firm output audit + customer-facing output SLA 외부 검증.

---

## 참조

- AICPA Trust Services Criteria 2017 — A3 Processing Integrity (Additional Category).
- Lodestar 결선 자산:
  - `internal/domain/audit/hash.go` · `audit.go` · `checkpoint.go` · `key_epoch.go` (audit chain hash chain + epoch)
  - `internal/domain/audit/keyrotation/rotator.go` (key rotation)
  - `internal/domain/audit/rotation/cosign.go` (cosign keyless)
  - `internal/domain/scan/` (scan engine determinism)
  - `internal/domain/evidence/` (evidence)
  - `internal/domain/reporting/pdf/` · `bundle.go` · `framework_bundle.go` (report bundle)
  - `internal/api/handlers/report.go` · `webhook.go` (output delivery)
  - `internal/api/handlers/rbac_middleware.go` (input authz)
  - `cmd/rosshield-audit-verify/` (fg-verify v2)
  - `.github/workflows/release-pipeline.yml` (cosign signed release)
  - `docs/design/01-principles.md` (설계 원칙 12개)
  - `CHANGELOG.md` (release output 명세)
- 0037 마이그레이션: `audit_chain_keys` 테이블 (epoch별 public key 보존, output verification).
- 관련 design doc:
  - `docs/design/01-principles.md` §원칙 1 · 6 · 9 · 11
  - `docs/design/notes/audit-chain-rotation-automation-design.md` (audit chain rotation)
  - `docs/design/notes/e8-pdf-signature-research.md` (PDF signature)
  - `docs/design/notes/e8-reporting-deepdive.md` (reporting)
  - `docs/design/notes/scanrun-ssh-integration-design.md` (scan)
  - `docs/design/notes/soc2-readiness-design.md` §2 · §3.3
- cross-reference: A3.1 ↔ CC3.1 (suitable objectives), A3.2 ↔ CC6.2 (logical access input authz), A3.3 ↔ CC7.2 (system component monitoring), A3.4 ↔ CC8.1 (change management output), A3 전체 ↔ CC6.7 (transmission integrity).
- 다음 단계: A4 Privacy → [`a4-privacy.md`](./a4-privacy.md)

---

*Last updated: 2026-05-21 — Stage 11.B-4 A3 mapping round.*
