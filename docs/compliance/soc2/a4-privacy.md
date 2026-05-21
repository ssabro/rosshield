# SOC2 A4 — Privacy

> **Trust Services Criteria**: Additional Categories (프라이버시 / Privacy)
> **Sub-controls**: A4.1 ~ A4.8 (8)
> **Status**: Lodestar 결선 자산 ~3.5/8 매핑 (~44% cover) — 외부 감사인 검증 대기. A4는 외부 트랙 ★ 의존이 가장 큰 통제군 (A4.1·A4.2·A4.6 = privacy notice · consent · breach notification는 외부 정책 영역). Lodestar는 A4.3·A4.4·A4.5·A4.7·A4.8(수집 · 보존 · 접근 · 품질 · 모니터링)에 기술 통제 cover. **A4 Privacy 상세 GDPR 매핑은 별 epic ★ — 본 doc은 SOC2 baseline만**.

A4는 프라이버시 — 통지, 동의, 수집, 사용/보존/폐기, 접근, 공개/통보, 품질, 모니터링/시행 8단계를 다룹니다. AICPA Generally Accepted Privacy Principles(GAPP) 기반. GDPR · CCPA · 한국 PIPA 등 별 framework와 부분 중복하나 **본 doc은 SOC2 baseline만, GDPR/CCPA 상세는 별 epic ★**.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| A4.1 Privacy Notice | (★ 외부) — Lodestar 자체 cover 0 | privacy policy docs 0 | ★ Privacy policy 별 epic 또는 외부 법무 위탁 |
| A4.2 Choice and Consent | (★ 외부) — LLM 옵트인은 결선이나 광범위 consent management 0 | consent management 자체 0 | ★ Consent management 별 epic |
| A4.3 Collection | Data minimization(설계 원칙 10 "프라이버시 기본값") · evidence redaction · pseudonymization(부분) · tenant 격리 | formal data inventory 0 (PII 분류 부재) | 외부 firm PII inventory audit ★ |
| A4.4 Use, Retention and Disposal | Retention policy(`docs/operations/audit-chain-key-rotation.md`) · audit chain append-only · evidence redaction · LLM 옵트인 · audit signer key epoch 보존 | formal data retention SLA per data class 부족 | 외부 firm retention audit ★ |
| A4.5 Access | RBAC fine-grained · auditor role 신규(Stage 11.B-5) · audit export bundle · tenant 격리 | data subject access request(DSAR) 자동화 0 | ★ DSAR workflow 별 epic |
| A4.6 Disclosure and Notification | (★ 외부) — Webhook delivery는 결선이나 breach notification 절차 0 | breach notification 절차 명문화 0 | ★ Breach notification 절차 별 epic 또는 외부 법무 |
| A4.7 Quality | Scan engine determinism · audit chain hash chain · data validation · `payloadDigest` · TDD 강제 | data quality SLA 명문화 부족 | 외부 firm data quality audit ★ |
| A4.8 Monitoring and Enforcement | Audit chain immutability · Prometheus + Grafana · 5 alert rule · webhook delivery + HMAC · SECURITY.md 신고 채널 | formal privacy violation tracking 0 | 외부 firm privacy monitoring audit ★ |

---

## Sub-control 상세

### A4.1 Provides Notice About Its Privacy Practices

**Trust Services Criteria 본문 의역**: 조직은 프라이버시 관행에 대한 통지를 제공합니다. 통지는 데이터 수집 · 사용 · 공개 · 보존 등 모든 영역을 cover하며, 데이터 주체가 접근 가능합니다.

**Lodestar 매핑**:

- **★ Lodestar 자체 cover 0** — Lodestar 제품 자체는 customer의 privacy policy 발행을 지원하지 않음. Lodestar 호스팅 서비스 시 운영자(=Lodestar 회사) 책임.

**gap**: 
- **Privacy policy docs 0** — 회사 차원의 privacy policy 미명문화.
- **Notice 갱신 절차 0**.

**외부 트랙 ★**: 
- ★ Privacy policy 별 epic 또는 외부 법무(GDPR · CCPA · 한국 PIPA 적합 정책).
- ★ Annual privacy notice review 라운드.

---

### A4.2 Communicates Choices Available Regarding the Collection, Use, Retention, Disclosure, and Disposal of Personal Information

**Trust Services Criteria 본문 의역**: 조직은 개인정보의 수집 · 사용 · 보존 · 공개 · 폐기에 관한 선택지를 통신합니다. 데이터 주체의 명시적 동의(opt-in) 또는 명시적 거부(opt-out)가 cover됩니다.

**Lodestar 매핑**:

- **LLM 옵트인 (limited consent)**:
  - **설계 원칙 2 "옵트인 지능화"** — AI 기능은 기본 비활성. customer 명시적 활성화 필요.
  - `docs/design/01-principles.md` §원칙 2.
  - `internal/platform/llm/` — LLM 4 provider 추상화. customer 선택 가능.

- **CONTRIBUTING.md DCO sign-off** (해당 시) — contributor 동의 패턴.

**gap**: 
- **Consent management 자체 0** — opt-in/opt-out toggle · consent banner · consent log 미구현. 
- **Granular consent 부재** — 데이터 카테고리별 consent 분리 0.

**외부 트랙 ★**: 
- ★ Consent management 별 epic (예: OneTrust · Cookiebot · TrustArc 통합).
- ★ Cookie consent (web frontend) 별 트랙.

---

### A4.3 Collects Personal Information Consistent with the Entity's Objectives Related to Privacy

**Trust Services Criteria 본문 의역**: 조직은 프라이버시 목표에 부합하는 개인정보만 수집합니다. 최소 수집 원칙(data minimization) + 정당한 목적이 적용됩니다.

**Lodestar 매핑**:

- **Data minimization (설계 원칙 10)**:
  - `docs/design/01-principles.md` §원칙 10 "프라이버시 기본값" — 로컬 우선.
  - 최소 수집 원칙 — 스캔에 필요한 데이터만 수집.

- **Evidence redaction (자동 minimization)**:
  - `internal/domain/evidence/redaction.go` — 외부 disclosure 전 민감 정보 자동 redaction.
  - `internal/domain/evidence/redaction_test.go` — redaction 규칙 테스트.

- **Pseudonymization (부분 cover)**:
  - User ID는 UUID · email은 SSO IdP 위탁 가능 → 일부 pseudonymization 가능.

- **Tenant 격리**:
  - tenant_id 필수 컬럼 — 다른 tenant 데이터 수집 불가능.

- **Audit chain (수집 추적)**:
  - 데이터 수집 audit event로 immutable 기록.

**gap**: 
- **Formal data inventory 부재** — PII 카테고리 분류 + 수집 경로 매핑 docs 0.
- **수집 목적 명문화 부족** — 데이터별 수집 목적 정당화 문서 0.

**외부 트랙 ★**: 외부 firm PII inventory audit + data flow mapping.

---

### A4.4 Limits the Use, Retention, and Disposal of Personal Information

**Trust Services Criteria 본문 의역**: 조직은 명시된 목적에 한정된 사용 · 보존 · 폐기를 보장합니다. 보존 기간 만료 시 자동 폐기되거나 anonymize됩니다.

**Lodestar 매핑**:

- **Audit chain append-only (immutable 보존)**:
  - `internal/domain/audit/hash.go` — UPDATE/DELETE 불가능. audit event 영구 보존.

- **Audit signer key rotation epoch 보존**:
  - 0037 마이그레이션 `audit_chain_keys` 테이블 — epoch별 public key 보존, 신규 key는 90일 quarterly rotation.

- **Evidence redaction (selective use limit)**:
  - 외부 disclosure 전 자동 redaction.

- **LLM 옵트인 (limited use)**:
  - 설계 원칙 2 — AI는 기본 비활성. customer가 활성화한 영역에 한해 사용.

- **Retention policy docs**:
  - `docs/operations/audit-chain-key-rotation.md` — audit chain key rotation 절차 + 보존.

- **사용자 deprovisioning (logical disposal)**:
  - SSO group 동기 + API key revoke + session 무효화.

- **Soft delete + tenant_id filter (data isolation)**:
  - 다른 tenant 데이터 read 불가능.

**gap**: 
- **Formal data retention SLA per data class 부족** — audit chain은 영구 보존이나 비-audit PII(예: user email · IP log · session token) retention SLA 명문화 부재.
- **자동 폐기 cron 부재** — 보존 기간 만료 데이터 자동 폐기 job 0.

**외부 트랙 ★**: 외부 firm retention audit + per-data-class retention policy 명문화.

---

### A4.5 Provides Individuals with Access to Their Personal Information for Review and, Upon Request, Provides Physical or Electronic Copies of That Information

**Trust Services Criteria 본문 의역**: 조직은 개인이 자신의 개인정보에 접근하여 review할 수 있도록 합니다. 요청 시 물리적 또는 전자적 복사본을 제공합니다(데이터 portability).

**Lodestar 매핑**:

- **RBAC fine-grained (사용자 자기 데이터 접근)**:
  - `internal/api/handlers/rbac_middleware.go` — RBAC로 본인 데이터 read 가능.

- **Auditor role (Stage 11.B-5 진입 예정)**:
  - `auditor` role — read-only audit log/checkpoint/evidence export. 외부 감사인 자체 검증 cover.

- **Audit export bundle (Stage 11.B-5 진입 예정)**:
  - `GET /api/v1/compliance/auditor-bundle?period=90d` — audit_entries + cosign signatures + chain_keys + 통제별 evidence index.

- **Tenant 격리 (개인 데이터 접근 제한)**:
  - tenant_id 필수 — 다른 tenant 데이터 접근 불가능. 본인 tenant 데이터 read 가능.

- **API endpoint (data access)**:
  - `internal/api/handlers/` — `/api/v1/scans` · `/api/v1/reports` · `/api/v1/audit-entries` 등 본인 데이터 access endpoint.

**gap**: 
- **Data Subject Access Request (DSAR) 자동화 0** — GDPR Article 15(right of access) 자동 워크플로 0.
- **데이터 portability export 자동화 부족** — JSON export endpoint는 있으나 GDPR Article 20(data portability) 표준 형식 export 0.

**외부 트랙 ★**: 
- ★ DSAR workflow 별 epic (GDPR · CCPA 적합).
- ★ Data portability export 표준화.

---

### A4.6 Provides Notification of Breaches and Incidents

**Trust Services Criteria 본문 의역**: 조직은 개인정보 침해 또는 인시던트를 적시 통보합니다. 데이터 주체 · 규제 기관 · 영향받는 당사자에 대한 통보 절차가 명문화됩니다.

**Lodestar 매핑**:

- **Webhook delivery (외부 통보 채널)**:
  - `internal/api/handlers/webhook.go` — outbound webhook + HMAC + retry. PagerDuty · Opsgenie · 외부 incident management 통합.

- **Alertmanager severity routing**:
  - `deploy/prometheus/alertmanager-sample.yml` — severity 라벨 분리 + webhook/email/slack 라우팅.

- **SECURITY.md (외부 신고 채널)**:
  - 보안 결손 신고 channel + response SLA.
  - Coordinated disclosure 절차 (Fix 배포 후 14~30일).

- **Audit chain immutability (침해 사후 추적)**:
  - 침해 event 자체가 audit event로 immutable 기록.

**gap**: 
- **★ Breach notification 절차 명문화 0** — GDPR Article 33/34(72시간 통보) · CCPA · 한국 PIPA(72시간 통보) 절차 명문화 부재.
- **Regulatory notification workflow 0** — 규제 기관 자동 통보 0.
- **Data subject notification template 0** — 영향받는 사용자 통보 template 0.

**외부 트랙 ★**: 
- ★ Breach notification 절차 별 epic 또는 외부 법무 (GDPR · CCPA · PIPA 적합).
- ★ Cyber insurance + breach response retainer.

---

### A4.7 Collects and Maintains Accurate, Up-to-Date, Complete, and Relevant Personal Information

**Trust Services Criteria 본문 의역**: 조직은 정확 · 최신 · 완전 · 관련성 있는 개인정보를 수집 · 유지합니다. 데이터 품질 통제가 적용됩니다.

**Lodestar 매핑**:

- **Scan engine determinism (data accuracy)**:
  - `internal/domain/scan/` — 결정론적 스캔. 동일 입력 → 동일 출력.

- **Audit chain hash chain (data integrity)**:
  - `internal/domain/audit/hash.go` — `payloadDigest` + `canonicalMetaJSON` — 데이터 변경 사후 탐지 가능.

- **Input validation (data accuracy)**:
  - HTTP handler JSON schema validation + domain entity validation.

- **TDD 강제**:
  - CLAUDE.md §TDD 강제 — domain logic 테스트 우선으로 data quality 보장.

- **SSO group sync (up-to-date)**:
  - `internal/domain/tenant/sso/` — IdP group 변경 시 자동 role 동기.

- **Replication lag metric**:
  - `rosshield_replication_lag_seconds` — replica 데이터 최신성 모니터링.

**gap**: 
- **Data quality SLA 명문화 부족** — 데이터 정확성 SLA(예: 99.9% accuracy) 명문화 0.
- **Data update tracking 부족** — PII 갱신 audit event 분류 부재.

**외부 트랙 ★**: 외부 firm data quality audit + 정기 data quality review.

---

### A4.8 Monitors Compliance with Its Privacy Policies and Procedures and Takes Action to Address Noncompliance

**Trust Services Criteria 본문 의역**: 조직은 프라이버시 정책과 절차의 준수를 모니터링하고, 미준수에 대해 시정 조치를 시행합니다. 위반 추적 · escalation · 정정이 cover됩니다.

**Lodestar 매핑**:

- **Audit chain immutability (privacy violation tracking)**:
  - `internal/domain/audit/hash.go` — 모든 데이터 access audit event로 immutable 기록.
  - 미인가 접근 시도 사후 탐지 가능.

- **Prometheus + Grafana (monitoring)**:
  - 데이터 access metric + alert.

- **5 alert rule (자동 탐지)**:
  - `deploy/prometheus/alerts/multi-region.yml` — 운영 이상 자동 탐지.

- **Webhook delivery + HMAC (escalation)**:
  - 미준수 시 외부 incident management 자동 통보.

- **SECURITY.md (신고 채널)**:
  - 보안/프라이버시 위반 신고 channel.

- **RBAC + audit chain (enforcement)**:
  - 미인가 접근 차단 + 시도 audit 기록.

**gap**: 
- **Formal privacy violation tracking 0** — 통합 privacy violation register · 시정 조치 추적 0.
- **Privacy compliance dashboard 부재** — 통제별 privacy compliance status drill-down 0 (Stage 11.B-6 effectiveness dashboard 진입 시 부분 cover 예정).

**외부 트랙 ★**: 외부 firm privacy monitoring audit + DPO(Data Protection Officer) 정책 (GDPR Article 37 해당 시).

---

## A4 ↔ GDPR · CCPA · 한국 PIPA 상세 매핑

**★ A4 Privacy는 GDPR · CCPA · 한국 PIPA 등 별 framework와 광범위 중복**. 본 doc은 SOC2 A4 baseline만 cover하며, 상세 매핑은 별 epic ★:

- **GDPR (EU) 별 epic ★**: Article 5(원칙) · Article 13/14(통지) · Article 15(접근) · Article 17(삭제) · Article 20(portability) · Article 33/34(72h breach notification) · Article 37(DPO) 등 상세 매핑.
- **CCPA (California) 별 epic ★**: 1798.100(right to know) · 1798.105(right to delete) · 1798.120(right to opt-out of sale) 등.
- **한국 PIPA 별 epic ★**: 개인정보보호법 적합성 + ISMS-P 통제 통합 매핑.

본 doc은 SOC2 readiness baseline만 cover — 외부 firm 진입 *전* "통제 설계가 SOC2 A4에 매핑된다"를 입증.

---

## 참조

- AICPA Trust Services Criteria 2017 — A4 Privacy (Additional Category, GAPP 기반).
- Lodestar 결선 자산:
  - `internal/domain/evidence/redaction.go` · `redaction_test.go` (민감 정보 redaction)
  - `internal/api/handlers/rbac_middleware.go` (RBAC fine-grained)
  - `internal/platform/authz/permission_matrix.go` · `policy.go` (authz 엔진)
  - `internal/domain/tenant/sso/` (SSO group sync)
  - `internal/domain/audit/hash.go` · `audit.go` · `checkpoint.go` · `keyrotation/rotator.go` (audit chain immutability)
  - `internal/api/handlers/webhook.go` (HMAC delivery)
  - `internal/platform/llm/` (LLM 4 provider — opt-in)
  - `internal/platform/metrics/metrics.go` · `deploy/prometheus/alerts/multi-region.yml`
  - `docs/design/01-principles.md` §원칙 2(옵트인) · §원칙 10(프라이버시 기본값)
  - `docs/operations/audit-chain-key-rotation.md` (retention)
  - `SECURITY.md` (신고 채널)
- 0037 마이그레이션: `audit_chain_keys` 테이블 (epoch별 public key 보존).
- 관련 design doc:
  - `docs/design/01-principles.md` §원칙 2 · 10 · 11
  - `docs/design/notes/e7-redaction-research.md` (evidence redaction)
  - `docs/design/notes/llm-private-deployment-design.md` (LLM 4 provider opt-in)
  - `docs/design/notes/audit-chain-rotation-automation-design.md`
  - `docs/design/notes/soc2-readiness-design.md` §2 · §3.3
- cross-reference: A4.3 ↔ A2.1 (confidentiality classification), A4.4 ↔ A2.2 (confidentiality disposal), A4.5 ↔ CC6.1·CC6.2 (logical access), A4.6 ↔ CC7.4 (incident response), A4.7 ↔ A3.2·A3.3 (processing integrity), A4.8 ↔ CC4.1·CC4.2 (monitoring + deficiency comm).
- **★ A4 Privacy 상세 GDPR · CCPA · 한국 PIPA 매핑은 별 epic** — 본 doc은 SOC2 baseline만.
- 다음 단계: A5 Security → [`a5-security.md`](./a5-security.md)

---

*Last updated: 2026-05-21 — Stage 11.B-4 A4 mapping round (SOC2 baseline만, GDPR 상세는 별 epic ★).*
