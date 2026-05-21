# SOC2 CC6 — Logical and Physical Access Controls

> **Trust Services Criteria**: Common Criteria (필수, 모든 SOC2 audit 적용)
> **Sub-controls**: CC6.1 ~ CC6.8 (8)
> **Status**: Lodestar 결선 자산 ~7/8 매핑 (~88% cover) — 외부 감사인 검증 대기. CC6.4(physical access)는 DC 위탁 외부 트랙 ★ 의존, 나머지 7건은 RBAC + SSO + audit chain + TPM + TLS + cosign 결선 자산이 자연 cover. SOC2 cover 폭이 가장 넓은 통제군.

CC6는 논리·물리 접근 통제 — 보안 소프트웨어/인프라/아키텍처, 논리적 접근 제한, 접근 지점 관리, 물리적 접근, 보호의 종료, 보안 조치 구현, 전송/이동, 위협 방지/탐지 8 단계를 다룹니다. SOC2 audit에서 가장 광범위하게 평가되는 통제군입니다.

---

## 매핑 매트릭스

| Sub-control | Lodestar 결선 자산 | gap | 외부 트랙 |
|---|---|---|---|
| CC6.1 Implements Logical Access Security Software, Infrastructure, and Architectures | RBAC fine-grained · SSO/SAML/OIDC · JWT · API key · tenant 격리 · TLS · audit chain | session timeout 정책 명문화 부족 | 외부 firm logical access audit ★ |
| CC6.2 Restricts Logical Access | `rbac_middleware.go` `RequirePermission` · `permission_matrix.go` · fleet scope dual layer · 최소권한 | least-privilege 자동 검증 pack 부재 (Stage 11.B-7 진입 예정) | 외부 firm RBAC audit ★ |
| CC6.3 Manages Points of Access | SSO/SAML/OIDC + group→role 자동 매핑 · JWT(`jwt.go`) · API key(`apikey.go`) · invitation flow | MFA 강제 정책 미명문화 (SSO IdP 위탁) | ★ IdP MFA 정책 (외부 SSO provider) |
| CC6.4 Restricts Physical Access | (★ 외부) — Lodestar 자체 cover 0 | physical access 통제 0 (소프트웨어 제품) | ★ DC/클라우드 provider 위탁 (AWS/GCP/Azure SOC2 Type II 인증서) |
| CC6.5 Discontinues Logical and Physical Protections | 사용자 deprovisioning(SSO group 동기) · API key revoke · session 무효화 · audit chain immutability(폐기 사후 추적) | 자동 deprovisioning timeline SLA 미명문화 | 외부 firm offboarding audit ★ |
| CC6.6 Implements Logical Access Security Measures | Audit signer key rotation 90일 자동 · 0037 마이그레이션 epoch별 key · cosign keyless · TPM PCR-sealed · password bcrypt | password rotation 정책 명문화 0 (SSO 위탁 시 N/A) | 외부 firm key management audit ★ |
| CC6.7 Restricts the Transmission, Movement | TLS 강제 · mTLS(replication) · cosign 서명 binary · TPM-sealed key 미이동 · audit export bundle 서명 | data loss prevention(DLP) 도구 부재 | ★ DLP 별 epic 또는 외부 보안 firm |
| CC6.8 Implements Controls to Prevent or Detect Unauthorized or Malicious Software | Audit chain immutability · cosign Sigstore Rekor · check-health hook · TPM Secure Boot · 의존성 lockfile(go.sum) | EDR/AV 통합 부재 (host OS 책임) | ★ host EDR/AV (외부 보안 벤더) |

---

## Sub-control 상세

### CC6.1 Implements Logical Access Security Software, Infrastructure, and Architectures

**Trust Services Criteria 본문 의역**: 조직은 보호 대상 자산에 대한 논리적 접근을 제한하기 위해 보안 소프트웨어 · 인프라 · 아키텍처를 구현합니다. 인증 · 인가 · 격리 메커니즘이 적절히 설계되고 작동합니다.

**Lodestar 매핑**:

- **인증 (authentication)**:
  - `internal/api/handlers/auth.go` — 로그인 + JWT 발급. `internal/domain/tenant/password.go` bcrypt password hash.
  - `internal/domain/tenant/jwt.go` — JWT 토큰 발행 + 검증. 만료 시간 + signing key 분리.
  - `internal/domain/tenant/apikey.go` — API key 인증 (machine-to-machine).
  - `internal/api/handlers/sso.go` + `internal/domain/tenant/sso/` — SSO/SAML/OIDC 진입 (Phase 5 결선).

- **인가 (authorization)**:
  - `internal/api/handlers/rbac_middleware.go` — `RequireRole(allowed ...string)` + `RequirePermission(resource, action)` dual layer.
  - `internal/platform/authz/permission_matrix.go` — role × permission 매트릭스.
  - `internal/platform/authz/decision.go` — 인가 결정 엔진.

- **격리 (isolation)**:
  - **Tenant 격리**: 모든 테이블 `tenant_id` 컬럼 필수 (CLAUDE.md §하지 말 것 항목 일관). SQL row-level tenant filter 강제.
  - **Fleet scope**: tenant × fleet × user 3-tier scope binding.

- **전송 보호**:
  - **TLS**: 모든 HTTP/WS API endpoint TLS 강제.
  - **mTLS**: replication 채널(Patroni streaming) 양방향 인증.

**gap**: session timeout · idle timeout · concurrent session 한도 등 정책 명문화 부족.

**외부 트랙 ★**: 외부 firm logical access architecture audit.

---

### CC6.2 Restricts Logical Access to Information and System Resources

**Trust Services Criteria 본문 의역**: 조직은 인가된 사용자만 정보와 시스템 자원에 접근하도록 논리적 접근을 제한합니다. 최소 권한 원칙(least privilege) + 직무 분리(segregation of duties)가 적용됩니다.

**Lodestar 매핑**:

- **최소 권한 (least privilege)**:
  - `internal/api/handlers/rbac_middleware.go` — `RequirePermission(resource, action)` resource-action grain 정밀 인가.
  - `internal/platform/authz/permission_matrix.go` — admin · operator · auditor · custom role 분리. admin 와일드카드 `"*"`는 admin 전용.
  - Custom role 위임(Phase 5 fine-grained) — tenant 관리자가 자체 role 정의 가능, 최소 권한 보장.

- **직무 분리 (segregation of duties)**:
  - admin(write) vs auditor(read-only, Stage 11.B-5 진입 예정) 분리.
  - scan 실행자(operator) vs report 발행자(custom role) 분리 가능.
  - audit chain은 모든 role에 immutable (append-only) — 통제 위반자도 audit log 변조 불가능.

- **Fleet scope (시각 격리)**:
  - `internal/platform/authz/policy.go` — fleet × user binding으로 다른 fleet 데이터 접근 차단.

- **Tenant 격리**:
  - tenant_id 필수 컬럼 + repository 계층 tenant filter 강제.

**gap**: 
- **least-privilege 자동 검증 pack 부재** — Stage 11.B-7 `soc2-controls` pack에서 `CC5.2-rbac-least-privilege.yaml` 진입 예정. admin 권한 사용자 수 임계 검증 + 미사용 role 탐지.
- 정기 access review 라운드 docs 0.

**외부 트랙 ★**: 외부 firm RBAC matrix audit + 정기 access review.

---

### CC6.3 Manages Points of Access (Authentication and Provisioning)

**Trust Services Criteria 본문 의역**: 조직은 접근 지점을 관리합니다. 사용자 식별 · 인증 · 등록 · 변경 · 폐기의 수명주기가 통제됩니다.

**Lodestar 매핑**:

- **사용자 등록 (provisioning)**:
  - **SSO group → role 자동 매핑**: `internal/domain/tenant/sso/` — IdP group 변경 시 자동 role 매핑. 사용자 등록 자동화.
  - **Invitation flow**: `internal/api/handlers/invitation.go` — admin invite + email verification + 첫 로그인 시 password 설정.

- **인증 메커니즘**:
  - **SSO/SAML/OIDC**: Phase 5 결선. 외부 IdP(Okta · Auth0 · Azure AD · Google Workspace) 위탁.
  - **JWT**: short-lived(예: 1h) access token + refresh token. signing key rotation 가능.
  - **API key**: 장기 machine credentials, scope binding.

- **변경 관리**:
  - **SSO group 동기**: IdP group 변경 시 다음 로그인 시 role 자동 갱신.
  - **Audit chain entries**: 권한 변경 audit event `audit.tenant.role_assigned` 등 immutable 기록.

- **폐기 (deprovisioning)**:
  - **SSO 비활성**: IdP에서 사용자 비활성 시 다음 로그인 차단.
  - **API key revoke**: `apikey.go` revoke endpoint — 즉시 무효화.
  - **JWT 무효화**: refresh token revoke + JWT blacklist (해당 시).

**gap**: MFA 강제 정책 명문화 부족 — SSO IdP에 위탁이나 Lodestar 자체 MFA 강제 옵션 미존재.

**외부 트랙 ★**: 
- ★ IdP MFA 정책 (외부 SSO provider Okta/Auth0/Azure AD 등 위탁).
- ★ 정기 access review 외부 검증.

---

### CC6.4 Restricts Physical Access to Facilities and Protected Information Assets

**Trust Services Criteria 본문 의역**: 조직은 물리적 facility · 시스템 · 자산에 대한 물리적 접근을 제한합니다. badge · biometric · escort · log 등 물리 통제가 적용됩니다.

**Lodestar 매핑**:

- **★ Lodestar 자체 cover 0** — Lodestar는 소프트웨어 제품 (no own DC).
- **클라우드 provider 위탁**: AWS/GCP/Azure 등 SOC2 Type II 인증 cloud provider 위탁 시 provider의 SOC2 audit이 CC6.4 cover.
- **on-prem 배포**: customer 자체 DC 책임 — customer의 SOC2 audit 범위.
- **Appliance(Snap)**: customer 사이트 물리 통제 책임 — customer 책임.

**gap**: Lodestar 자체 cover 영역 0. 본 항목은 배포 환경 host의 책임으로 위임.

**외부 트랙 ★**: 
- ★ 클라우드 provider SOC2 Type II 인증서 (AWS/GCP/Azure 등) 첨부.
- ★ on-prem 배포 시 customer의 DC physical security 책임.

---

### CC6.5 Discontinues Logical and Physical Protections Over Physical Assets Only After the Ability to Read or Recover Data and Software From Those Assets Has Been Diminished and Is No Longer Required

**Trust Services Criteria 본문 의역**: 조직은 정보 자산의 폐기 또는 재활용 전에 데이터를 복구 불가능하게 처리합니다. 폐기 후 데이터 재구성이 불가능해야 합니다.

**Lodestar 매핑**:

- **사용자 deprovisioning**:
  - SSO group 동기로 자동 비활성.
  - audit chain은 사용자 폐기 후에도 immutable — 폐기 자체가 audit event로 기록.

- **API key revoke**:
  - `internal/domain/tenant/apikey.go` — revoke 시 즉시 무효화 + `audit.apikey.revoked` event emit.

- **Session 무효화**:
  - JWT refresh token revoke + blacklist.

- **Tenant 격리 (data residency)**:
  - 다른 tenant 데이터 read 불가능 — soft delete + tenant_id filter로 격리.

- **Audit chain immutability (폐기 사후 추적)**:
  - 폐기 작업 자체가 audit event로 immutable 기록 → 폐기 누락 감사 가능.

- **물리 폐기 (physical disposal)**:
  - **★ host OS 책임** — Lodestar 소프트웨어는 logical layer만 책임. 물리 디스크 폐기는 host OS 절차(NIST SP 800-88 등) 위탁.

**gap**: 
- **자동 deprovisioning timeline SLA 미명문화** — IdP group 변경 → Lodestar 반영까지 SLA(예: 24h) 명문화 0.
- 데이터 retention policy 명문화는 audit chain rotation docs(`docs/operations/audit-chain-key-rotation.md`)가 부분 cover.

**외부 트랙 ★**: 외부 firm offboarding audit + 물리 폐기 절차 위탁.

---

### CC6.6 Implements Logical Access Security Measures to Protect Against Threats from Sources Outside Its System Boundaries

**Trust Services Criteria 본문 의역**: 조직은 시스템 경계 외부 위협으로부터 보호하기 위해 논리적 접근 보안 조치를 구현합니다. 키 관리 · 암호화 · 인증 강화 · 외부 위협 모니터링이 포함됩니다.

**Lodestar 매핑**:

- **Audit signer key rotation (Phase 10.D 자동화)**:
  - `internal/domain/audit/keyrotation/rotator.go` — 90일 quarterly 자동 rotation. epoch별 public key 보존(0037 마이그레이션 `audit_chain_keys` 테이블).
  - `docs/operations/audit-chain-key-rotation.md` — rotation 절차 + 외부 감사인 검증 절차.

- **Audit chain hash chain**:
  - `internal/domain/audit/hash.go` — `hash_i = sha256(prevHash ‖ payloadDigest ‖ canonicalMetaJSON)` 결정론적 chain.
  - Ed25519 서명 checkpoint (`internal/domain/audit/checkpoint.go`) — 외부 감사인 자체 검증 가능.

- **cosign keyless signed releases**:
  - `.github/workflows/release-pipeline.yml` + `internal/domain/audit/rotation/cosign.go` — Sigstore Rekor 투명 로그. 외부 위협(공급망 공격) 방어.

- **TPM PCR-sealed keystore**:
  - `internal/platform/keystore/tpm/` — Secure Boot + measured boot. 변조된 부팅 환경에서 키 unsealing 불가능.

- **Password 보호**:
  - `internal/domain/tenant/password.go` — bcrypt cost factor 적용. plain text 저장 0.

- **JWT signing key 분리**:
  - JWT signing key는 환경변수 또는 keystore (`internal/platform/keystore/`)에서 주입, 코드 0건.

- **Audit chain immutability**:
  - UPDATE/DELETE 불가능 (CLAUDE.md §하지 말 것). 외부 공격자가 audit log 변조 불가능.

**gap**: 
- **Password rotation 정책 명문화 0** — SSO 위탁 시 N/A, password 사용자는 IdP rotation 정책 위탁.

**외부 트랙 ★**: 외부 firm key management audit + 외부 위협 모니터링(SIEM 등) 별 트랙.

---

### CC6.7 Restricts the Transmission, Movement, and Removal of Information

**Trust Services Criteria 본문 의역**: 조직은 정보의 전송 · 이동 · 제거를 인가된 행위로 제한합니다. 전송 중 암호화 · 이동 권한 통제 · 외부 반출 통제가 적용됩니다.

**Lodestar 매핑**:

- **TLS 강제 (transit)**:
  - 모든 HTTP/WS API endpoint TLS 강제.
  - 외부 webhook(`internal/api/handlers/webhook.go`)도 HTTPS 강제 + HMAC signature.

- **mTLS (replication)**:
  - Patroni streaming replication + mTLS 양방향 인증 (`internal/platform/replication/setup/`).

- **cosign 서명 binary (movement)**:
  - 모든 release artifact cosign 서명 — 이동 중 변조 탐지 가능.
  - Sigstore Rekor 투명 로그로 모든 서명 외부 검증.

- **TPM-sealed key 미이동 (PCR seal)**:
  - `internal/platform/keystore/tpm/` — PCR-sealed key는 측정된 부팅 환경에서만 unsealing. 다른 host로 이동 시 unsealing 불가능.

- **Audit export bundle 서명 (removal)**:
  - audit export bundle (Stage 11.B-5 진입 예정) — `auditor` role read-only export + cosign 서명 + audit event `audit.export.created`.

- **Customer-owned encryption (BYOK 옵션)**:
  - 향후 enterprise 고객용 BYOK 옵션 (별 epic 후보).

**gap**: 
- **DLP (Data Loss Prevention) 도구 부재** — 외부 반출 자동 차단 도구 0. 본 항목은 access control + audit log로 detective control만 cover.

**외부 트랙 ★**: 
- ★ DLP 별 epic 또는 외부 보안 firm(예: Symantec/Microsoft Purview/Forcepoint) 위탁.
- ★ data residency 정책 (EU GDPR / 한국 PIPA) 별 외부 트랙.

---

### CC6.8 Implements Controls to Prevent or Detect and Act Upon the Introduction of Unauthorized or Malicious Software

**Trust Services Criteria 본문 의역**: 조직은 인가되지 않거나 악성인 소프트웨어의 도입을 방지·탐지·대응합니다. 코드 서명 · 무결성 검증 · EDR/AV · 외부 모니터링이 포함됩니다.

**Lodestar 매핑**:

- **코드 서명 (code signing)**:
  - cosign keyless signed releases + Sigstore Rekor 투명 로그.
  - Snap binary 서명 (snap store 자체 서명).

- **부팅 무결성**:
  - **TPM Secure Boot**: `internal/platform/keystore/tpm/` — measured boot + PCR 검증. 변조된 부팅 환경 탐지.
  - **Secure Boot enrollment**: `docs/operations/secure-boot-enrollment.md` — 운영 절차 명문화.

- **Audit chain immutability**:
  - 악성 소프트웨어 도입 시도 자체가 audit event로 immutable 기록 (해당 시).
  - chain break 검출 시 외부 감사인 자체 검증으로 탐지.

- **CI/CD 통합 검증**:
  - `.github/workflows/ci.yml` — `make ci` (vet + test + build) + lint 자동 검증.
  - `make ci` 통과 없이는 main commit 0 (trunk-based 강제).

- **Post-refresh check-health hook**:
  - Snap refresh 후 check-health hook (timeout 120s, commit `9c6bf04`) — refresh 후 시스템 통합 evaluation. 악성 update 시 hook fail로 자동 차단.

- **의존성 lockfile**:
  - `go.sum` — Go 의존성 hash 고정. 무단 의존성 변경 시 빌드 fail.
  - `package-lock.json` (web) — JavaScript 의존성 고정.

- **컨테이너 이미지 서명**:
  - cosign으로 OCI image 서명. 미서명 image deploy 차단 가능.

**gap**: 
- **EDR/AV (Endpoint Detection and Response / Antivirus) 통합 부재** — host OS 책임으로 위임. Lodestar 자체 EDR 0.

**외부 트랙 ★**: 
- ★ host EDR/AV (외부 보안 벤더 CrowdStrike · SentinelOne · Defender 등).
- ★ 정기 vulnerability scanning 별 epic 또는 외부 펜테스트 firm.

---

## 참조

- AICPA Trust Services Criteria 2017 — CC6 Logical and Physical Access Controls.
- Lodestar 결선 자산:
  - `internal/api/handlers/rbac_middleware.go` (RBAC fine-grained)
  - `internal/api/handlers/sso.go` · `internal/domain/tenant/sso/` (SSO/SAML/OIDC)
  - `internal/api/handlers/auth.go` · `internal/domain/tenant/jwt.go` · `apikey.go` · `password.go` (인증)
  - `internal/api/handlers/invitation.go` (invitation flow)
  - `internal/platform/authz/permission_matrix.go` · `policy.go` · `decision.go` (authz 엔진)
  - `internal/domain/audit/hash.go` · `audit.go` · `checkpoint.go` · `keyrotation/rotator.go` (audit chain + key rotation)
  - `internal/domain/audit/rotation/cosign.go` (cosign keyless)
  - `internal/platform/keystore/tpm/` (TPM PCR-sealed)
  - `internal/platform/replication/setup/` (mTLS replication)
  - `internal/api/handlers/webhook.go` (HMAC webhook)
  - `.github/workflows/release-pipeline.yml` (서명 release)
  - `docs/operations/audit-chain-key-rotation.md` · `secure-boot-enrollment.md`
- 0037 마이그레이션: `audit_chain_keys` 테이블 (epoch별 public key 보존, Phase 10.D-2 결선).
- 관련 design doc:
  - `docs/design/notes/rbac-fine-grained-design.md` · `rbac-fleet-scope-precision-design.md`
  - `docs/design/notes/audit-chain-rotation-automation-design.md`
  - `docs/design/notes/e34-tpm-design.md`
  - `docs/design/notes/soc2-readiness-design.md` §2 · §3.3
- cross-reference: CC6.1 ↔ CC5.1 (preventive controls), CC6.2 ↔ CC1.3 (org structure), CC6.6 ↔ CC8.1 (change management 키 rotation), CC6.7 ↔ A2.1 (confidentiality transmission), CC6.8 ↔ CC7.3 (security event detection), CC6.4 ↔ A1.3 (DC environmental).
- 다음 단계: CC7 System Operations → [`cc7-system-operations.md`](./cc7-system-operations.md)

---

*Last updated: 2026-05-21 — Stage 11.B-3 CC6 mapping round.*
