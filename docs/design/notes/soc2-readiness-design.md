# SOC2 Type II readiness — Phase 11 옵션 B Design (Stage 11.B-1)

> **상태**: Design (Stage 11.B-1) — 코드 0줄 / 마이그레이션 0건 / pack 변경 0. 본 round는 design doc만, 코드 진입은 D-P11B-1~4 사용자 확정 후 별 PR(Stage 11.B-2~8).
> **작성일**: 2026-05-21
> **범위**: Phase 11 옵션 B 진입 첫 stage. SOC2 Type II 인증 readiness baseline — `docs/compliance/` 트리 신규 + Trust Services Criteria(TSC) control mapping + 외부 감사인 access wizard + 통제 effectiveness dashboard + `soc2-controls` benchmark pack. 본 doc 자체는 Stage 분해 + D-P11B 결정 항목까지만 마감.
> **참조**:
> - `docs/design/notes/phase11-backlog-design.md` §4.2 + §12 — 본 doc의 직접 부모, D-P11-1 = Top 3 순차 B→C→A 확정.
> - `docs/design/notes/audit-chain-rotation-automation-design.md` — Phase 10 옵션 D 본체, design doc 패턴 직접 모방 대상.
> - `docs/design/notes/ros2-humble-dds-sros2-design.md` — Phase 10 옵션 E 본체, fact-check 패턴 직접 모방 대상.
> - 코드: `internal/domain/audit/` · `internal/domain/audit/keyrotation/` · `internal/domain/compliance/` · `internal/api/handlers/rbac_middleware.go` · `internal/api/handlers/sso.go` · `internal/platform/keystore/tpm/` · `internal/platform/replication/`.
> - 마이그레이션: `0037_audit_chain_keys` (Phase 10.D 결선 — epoch별 public key 보존) · 0036 audit_gc_marker.
> **R 식별자**: R-PHASE11-B(본 stage 전체) — 결정 항목은 D-P11B-1~4.
> **본 문서 작성 위치**: main(head `85b6bb1` 직후), 단독 sub-agent.
> **비목표** (§11에서 명시):
> - ISO 27001 / GDPR / HIPAA / PCI-DSS — 본 doc은 SOC2 Type II에 한정. (ISO 27001 통합은 옵션 D 후보로 §5.4 비교만.)
> - 외부 SOC2 감사 firm 직접 계약(Deloitte/KPMG/PwC 등) — 사용자 외부 트랙 ★ (memory `feedback_user_tracks.md` 일관).
> - security awareness training 콘텐츠 제작 — internal HR 트랙 ★.
> - penetration testing(CC9.4) 실행 — third-party 외부 위임 ★.
> - 90일 운영 효과성 측정 자체 — SOC2 Type II 정의상 외부 감사 firm이 측정. 본 epic은 자체 측정 도구만 cover.

---

## 1. 상태 / 배경

### 1.1 Phase 10 마감 + Phase 11 진입 사실

`docs/design/notes/phase11-backlog-design.md` §12.1 확정(2026-05-21):

| 진입 순서 | 옵션 | 추정 | minor release |
|---|---|---|---|
| **1순위 (본 stage)** | **B SOC2 Type II readiness** | ~6~9주 | v0.12.0 |
| 2순위 | C audit hash chain key_epoch input | ~2~3주 | v0.13.0 |
| 3순위 | A OpenTelemetry tracing 전면 | ~5~7주 | v0.14.0 |

Phase 10 (v0.9.0 + v0.10.0~v0.10.2 + v0.11.0) 마감 후 자연 진입. 옵션 B는 Phase 10 backlog §4.2에서 권장되었으나 옵션 A·D·E 마감으로 carryover → Phase 11에서 1순위 재진입.

### 1.2 SOC2 Type I vs Type II 차이

| 차이 | Type I | Type II |
|---|---|---|
| 측정 시점 | 특정 시점(point-in-time) — 통제 설계 적정성만 | 운영 기간(typical 90일~12개월) — 통제 운영 효과성 |
| 외부 감사 firm | 1라운드 | 2라운드(설계 + 효과성) |
| 영업 가치 | 초기 enterprise gate 통과 | enterprise 영업 critical gate(금융 · 의료 · 정부 시장) |
| 본 epic 범위 | docs + 매핑 cover | docs + 매핑 + effectiveness 자체 측정 도구 cover. **실 90일 측정은 외부 firm 트랙 ★** |

본 epic은 **Type II readiness** = "외부 firm이 90일 측정을 시작할 수 있는 baseline"까지. 실 인증은 외부 firm 컨설팅(★) 후 추진.

### 1.3 본 round 진입 가치

- **enterprise 영업 critical gate**: SOC2 통과 없이는 금융 · 의료 · 정부 enterprise customer 진입 불가능한 시장이 큼. paying customer 진입 *전* baseline 강화로 영업 사이클 단축.
- **Lodestar 결선 자산 자연 매핑**: audit chain(P1+) · RBAC fine-grained(P5) · SSO/SAML/OIDC(P5) · cosign signed releases(P7) · TPM/Secure Boot(P5 E34) · audit key rotation(P10.D 자동화 + epoch별 public key 보존 0037) · multi-region HA(P8+9) 등 Phase 1~10에서 결선된 자산 대부분이 SOC2 TSC 통제에 직접 매핑 가능 → 신규 코드 비중 작음, docs 비중 큼.
- **외부 트랙 의존 0 (docs/매핑/dashboard/pack)**: 실 SOC2 firm 계약은 별 외부 트랙 ★이나 본 epic의 docs + access wizard + effectiveness dashboard + soc2-controls pack은 사내 진행 가능.
- **회귀 위험 작음**: 신규 docs 트리 + 신규 pack + 신규 handler/페이지 — 기존 audit chain · multi-region 코어 변경 0.

### 1.4 본 round 범위 · 비범위

- **범위** (Stage 11.B-2~8):
  - `docs/compliance/` 신규 트리 — TSC CC1~CC9 + A1~A5 control mapping 매트릭스 + Lodestar 결선 자산 cross-reference.
  - 외부 감사인 access wizard — `auditor` role 신규 + audit log/checkpoint/evidence export bundle.
  - 통제 effectiveness dashboard — 통제별 audit event 집계 query + Grafana panel + web `/compliance` 페이지(admin + auditor 권한).
  - `soc2-controls` benchmark pack — 통제 단위 yaml check ~50~80건, Lodestar 자체를 스캔하여 통제 충족 자동 평가.
  - testcontainers + Playwright e2e + ops docs + v0.12.0 minor release.
- **비범위** (§11 명시): ISO 27001 / GDPR / HIPAA / PCI-DSS · 외부 firm 직접 계약 · awareness training 콘텐츠 · pen test 실행 · 90일 측정 자체.

---

## 2. 현재 상태 fact-check (코드/디렉터리 직접 grep)

본 §은 추측 0, fact만 명시. 8 영역.

### 2.1 `docs/compliance/` 디렉터리 부재 사실

`docs/` 트리 직접 검사(2026-05-21 head `85b6bb1` 직후):

```
docs/
├─ appliance/
├─ design/
├─ ip/
├─ onboarding/
├─ operations/
├─ releases/
├─ PHASE2_EXIT_DEMO.md
└─ PHASE3_EXIT_DEMO.md
```

→ **`docs/compliance/` 디렉터리 없음**. SOC2 키워드 grep 결과는 기존 design doc(phase10-backlog · phase11-backlog · audit-chain-rotation-automation · rbac-fine-grained · ui-review-a11y-security) + ops docs(audit-chain-key-rotation · audit-verify-cli) + 설계서 06-security-and-tenancy의 인용 맥락만 — control mapping/매트릭스 docs 0.

### 2.2 audit chain · key rotation 결선 사실 (CC6.6 · CC7.2 cover)

`internal/domain/audit/` 트리:

| 파일 | fact |
|---|---|
| `audit.go` | `Entry.SignerKeyID` + `KeyEpoch *int64` (Phase 10.D-2 결선) |
| `hash.go` | `canonicalMetaJSON` — 알파벳순 7 키 직렬화 + `hash_i = sha256(prevHash ‖ payloadDigest ‖ canonicalMetaJSON)`. **`keyEpoch` 미포함**(2순위 옵션 C 진입 시 cover). |
| `checkpoint.go` | Ed25519 서명 + `audit_checkpoints` 테이블 `signer_key_id` 컬럼. |
| `export.go` | v2 bundle `_bundleVersion: "v2"` + `_chainKeyEpochs` (Phase 10.D-5 결선) — 외부 감사인 호환. |
| `key_epoch.go` | epoch 발급/조회 핵심 로직 (Phase 10.D-4). |
| `keyrotation/rotator.go` | 90일 quarterly cron + emergency override CLI (Phase 10.D-3). |
| `rotation/` 18 file | entry segment rotation + cosign 서명 + S3/file backend (P6 결선). |

→ **audit chain 결선 + signer key rotation 자동화 + epoch별 public key 보존(0037 마이그레이션) + cosign 외부 검증 모두 결선**. SOC2 CC6.6(Cryptographic Key Management) + CC7.2(System Monitoring) + CC8.1(Change Management) 자연 cover. fg-verify v2(epoch별 검증) 결선으로 외부 감사인 호환 baseline 충족.

### 2.3 RBAC fine-grained 결선 사실 (CC6.1 · CC6.3 cover)

`internal/api/handlers/rbac_middleware.go` Read 결과:

- `RequireRole(allowed ...string)` — role 단위 차단(`admin` 와일드카드 `"*"` 패턴, `auditor`·`operator`·`custom` 명시 포함 필요).
- `RequirePermission` — 세분 RBAC Stage 3 결선(resource × action + fleetID scope + Bindings → authz.Subject 변환 + claims.Roles fallback for D-RBAC-7 호환).

`internal/platform/authz/` — 정밀 인가 엔진 결선 (rbac-fine-grained-design.md Phase 5 마감). `rbac_integration_test.go` · `rbac_fleet_integration_test.go` 통합 테스트 결선.

→ **RBAC fine-grained + fleet scope + role 기반 + permission 기반 dual layer 결선**. SOC2 CC6.1(Logical Access — Authorization) + CC6.3(Role-based Access Provisioning) 자연 cover. 단, **`auditor` 신규 role은 미존재**(Stage 11.B-5에서 신규 도입).

### 2.4 SSO/SAML/OIDC 결선 사실 (CC6.1 · CC6.2 cover)

`internal/api/handlers/sso.go` Read 결과:

- `GET /api/v1/auth/sso/{providerId}/login` → StartSSOLogin
- `GET /api/v1/auth/sso/{providerId}/callback` → CompleteSSOLoginOIDC (OIDC code + state)
- `POST /api/v1/auth/sso/{providerId}/saml/acs` → CompleteSSOLoginSAML (SAML POST binding)
- `internal/domain/tenant/sso/` 도메인 결선 + `sso_handlers_test.go` 통합 결선.
- group → role 자동 매핑(Phase 5 마감).

→ **SSO/SAML/OIDC + group→role 자동 매핑 결선**. SOC2 CC6.1(Identity Provisioning) + CC6.2(Authentication) 자연 cover.

### 2.5 cosign signed releases 결선 사실 (CC8.1 · CC9.2 cover)

`.github/workflows/release-pipeline.yml` + `.github/workflows/snap-build.yml` + `.github/workflows/ci.yml` 모두 cosign 키워드 cover. `internal/domain/audit/rotation/cosign.go` + `cosign_e2e_test.go` — audit segment cosign keyless 서명(Sigstore Rekor 등록). `docs/releases/v0.6.9.md` ~ `v0.11.0.md` 38 release에 cosign 서명 + Rekor transparency log entry. 

→ **GitHub release binary + audit segment archive 모두 cosign keyless 서명 + Sigstore Rekor 등록 결선**. SOC2 CC8.1(Change Management — Authenticity) + CC9.2(Vendor/Supply Chain) 자연 cover. `fg-verify` SDK가 외부 cosign verify 절차 cover.

### 2.6 TPM 봉인 결선 사실 (CC6.6 · CC6.7 cover)

`internal/platform/keystore/`:

| 파일 | fact |
|---|---|
| `keystore.go` | `KeyStore` interface — `LoadOrCreatePrivateKey(handle)→ed25519.PrivateKey` |
| `file/store.go` | 0700 dir + 0600 file (development/cross-platform fallback) |
| `tpm/store_linux.go` | TPM 2.0 PCR-sealed (E34 Stage 1 결선) |
| `tpm/store_other.go` | non-Linux no-op fallback |

추가 — `internal/enterprise/robotid/quote_linux.go` · `quote_attestation.go` · `tpm_linux.go` — TPM quote attestation(robot identity binding, R-D8 D-3 cover).

→ **TPM 2.0 PCR-sealed signer key + Secure Boot + robot identity TPM attestation 모두 결선**. SOC2 CC6.6(Cryptographic Key Management — Hardware) + CC6.7(Restriction of Data Transmission) 자연 cover.

### 2.7 multi-region HA + Patroni 자동 failover 결선 사실 (A1.1 · A1.2 cover)

- `internal/platform/replication/` — PG logical replication (Phase 8 결선).
- `internal/platform/ha/patroni/role_provider.go` — Patroni REST API + `--ha-rp` flag (Phase 9 결선).
- `internal/api/handlers/replication.go` — 4 endpoint(region status + audit consistency + region timeline + failover).
- `deploy/grafana/dashboards/` — multi-region dashboard + 5 alert rule (Phase 10.A).
- `docs/operations/multi-region-ha-runbook.md` — failover runbook §13 결선.

→ **multi-region 결선 + Patroni 자동 failover RTO ≤ 60초 + 운영자 카드/alert/runbook 모두 결선**. SOC2 A1.1(Availability — Capacity Planning) + A1.2(Recovery) + A1.3(Backup) 자연 cover. backup은 `internal/domain/audit/rotation/backend_s3.go` + `docs/operations/audit-chain-key-rotation.md` cover.

### 2.8 compliance 도메인 결선 사실 (다른 framework 기반)

`internal/domain/compliance/`:

| 파일 | fact |
|---|---|
| `compliance.go` · `mapping.go` · `mapping_test.go` | E17 결선 — 통제 매핑 도메인 결선 |
| `frameworks/isms-p.yaml` | ISMS-P (Korean info security mgmt) framework |
| `frameworks/iso27001-2022.yaml` | ISO 27001:2022 framework |
| `frameworks/nist-800-53-rev5.yaml` | NIST SP 800-53 rev 5 framework |
| `sqliterepo/repo.go` · `suggestions_repo.go` | compliance suggestions persistence |
| `internal/api/handlers/compliance.go` | 4 endpoint(profiles + snapshots) |

→ **compliance 도메인 + 3 framework yaml + handler + persistence 결선**. **SOC2 framework yaml 부재** → Stage 11.B-3에서 `frameworks/soc2-type2-2017.yaml` 신규 추가 자연 진입 (기존 패턴 재사용, 신규 도메인 0).

### 2.9 가설 8 종합

| 영역 | 결선 자산 | SOC2 통제 매핑 | gap |
|---|---|---|---|
| audit chain | ✅ P1~P10.D | CC6.6 · CC7.2 · CC8.1 | epoch in hash (옵션 C carryover) |
| RBAC | ✅ P5 | CC6.1 · CC6.3 | `auditor` role 미존재 |
| SSO | ✅ P5 | CC6.1 · CC6.2 | 0 |
| cosign | ✅ P6~P10 | CC8.1 · CC9.2 | 0 |
| TPM | ✅ P5 E34 | CC6.6 · CC6.7 | 0 |
| multi-region | ✅ P8~P10.A | A1.1 · A1.2 · A1.3 | 0 |
| compliance 도메인 | ✅ E17 (3 framework) | docs/매핑 cover | **SOC2 framework yaml 부재** |
| docs/compliance/ | ❌ | docs 트리 부재 | **신규 영역** |

본 epic은 docs 신규 + `auditor` role 신규 + SOC2 framework yaml 신규 + effectiveness dashboard 신규 + `soc2-controls` pack 신규에 집중. 기존 audit/RBAC/SSO/cosign/TPM/multi-region 코어 변경 0.

---

## 3. SOC2 Trust Services Criteria 매트릭스

SOC2(AICPA Trust Services Criteria 2017, Description Criteria 2018 갱신)는 다음 두 축:

### 3.1 Common Criteria (CC1~CC9, 모든 SOC2 audit 필수)

| TSC | 명칭 | Lodestar 결선 자산 | gap |
|---|---|---|---|
| **CC1** Control Environment | 조직 통제 환경(정직성 · 거버넌스 · 책임 구조) | repo public + Apache 2.0 + CLAUDE.md governance + SESSION_HANDOFF.md 결정 로그 | awareness training 콘텐츠(★ HR), org chart docs |
| **CC2** Communication and Information | 통제 정보 통신(내부 + 외부) | docs/onboarding/ + docs/operations/ + release notes + CHANGELOG | external customer communication wizard |
| **CC3** Risk Assessment | 리스크 평가(부정 · 변화 · 위협 분석) | design doc(notes/*) 의 §risk 항목 + audit chain immutability + threat model 섹션 | 정기 리스크 평가 라운드 docs, fraud risk 분석 |
| **CC4** Monitoring Activities | 통제 모니터링 활동 + 결손 의사소통 | Prometheus + Grafana(5 alert rule) + audit chain immutability + multi-region dashboard | **effectiveness dashboard 신규**(통제별 audit event 집계) |
| **CC5** Control Activities | 통제 활동 선정·개발·배포 | RBAC fine-grained + segregation of duties + design doc 패턴 | control activity docs 매트릭스 |
| **CC6.1** Logical Access — Authorization | 논리적 접근 통제 | RBAC fine-grained + SSO group→role + fleet scope | `auditor` role 신규 도입 |
| **CC6.2** Authentication | 인증 | SSO/SAML/OIDC + MFA(IdP 위임) | docs 매트릭스 |
| **CC6.3** Role-based Provisioning | 역할 기반 권한 부여 | RBAC + tenant scope + bindings | docs 매트릭스 |
| **CC6.6** Cryptographic Key Management | 암호 키 관리 | audit signer 90일 quarterly rotation(P10.D) + epoch별 public key 보존(0037) + cosign keyless + TPM PCR-sealed | docs 매트릭스 |
| **CC6.7** Transmission Restriction | 전송 제한 | TLS(net/http) + SSH keyscan + TPM-bound | docs 매트릭스 |
| **CC6.8** System Configuration Protection | 시스템 구성 보호 | trunk-based + signed commits(권장) + audit chain | docs 매트릭스 |
| **CC7.1** System Operations Monitoring | 운영 모니터링 | Prometheus + Grafana + 5 alert rule + audit event | docs 매트릭스 |
| **CC7.2** System Monitoring | 시스템 모니터링 + 변경 식별 | audit chain(append-only) + Patroni ha_role metric | docs 매트릭스 |
| **CC7.3** Evaluation of Anomalies | 이상 평가 | alert rule + audit chain immutability | incident response runbook SOC2 형식 통합 |
| **CC7.4** Incident Response | 인시던트 대응 | multi-region failover runbook + ops docs | **incident response runbook SOC2 형식 통합 부재** |
| **CC7.5** Recovery | 복구 | Patroni RTO ≤ 60s + backup S3/file | docs 매트릭스 |
| **CC8.1** Change Management | 변경 관리 | trunk-based git + signed releases(cosign) + audit chain | docs 매트릭스 |
| **CC9.1** Risk Mitigation — Business Disruption | 사업 중단 리스크 완화 | multi-region HA + backup + DR runbook | docs 매트릭스 |
| **CC9.2** Vendor and Business Partner Mgmt | 벤더 관리 | LLM provider 4종(noop/anthropic/ollama/vllm) + snap store + cosign supply chain | **vendor inventory docs 부재** |

### 3.2 Additional Criteria (A1~A5, 서비스에 따라 선택)

| TSC | 명칭 | Lodestar 결선 자산 | 본 epic cover 여부 |
|---|---|---|---|
| **A1** Availability | 가용성 — 운영 · 모니터링 · 인시던트 · 복구 | multi-region HA + Patroni + backup + RTO ≤ 60s | **✅ cover** (권장 default) |
| **A2** Confidentiality | 기밀성 — 식별 · 저장 · 폐기 · 전송 보호 | TLS + redaction + air-gap + LLM private deployment + evidence access control | **✅ cover** (권장 default) |
| **A3** Processing Integrity | 처리 무결성 — 정합성 · 완전성 · 시간 적시성 | audit chain hash chain + cosign + checkpoint signed | 보류 (선택) |
| **A4** Privacy | 프라이버시 — 개인정보 보호 | redaction + local-first + opt-in LLM | 보류 (선택, GDPR/CCPA 별 트랙) |
| **A5** Security | 보안 — 통제 자산 보호(infrequent extra) | RBAC + audit chain + TPM + cosign + multi-region | **✅ cover** (권장 default) |

(A6 Custodial은 financial custody 전용 — Lodestar는 적용 0, 비범위.)

본 epic 권장 cover 범위(D-P11B-2 결정): **CC1~CC9 전체 + A1 · A2 · A5**. A3(Processing Integrity)는 audit chain hash chain이 자연 cover하나 별 통제 명시는 보류. A4(Privacy)는 GDPR/CCPA 별 트랙으로 분리.

### 3.3 매핑 cover 종합

| 범주 | 통제 수 | 결선 자산 cover | 신규 작업 |
|---|---|---|---|
| CC1~CC9 (~9 군 + 세부 ~30 통제) | ~30 | 28/30 (~93%) — 거의 모든 통제가 Phase 1~10 결선 자산으로 자연 cover | 2/30 — incident response SOC2 형식 통합 + vendor inventory docs |
| A1 · A2 · A5 (~9 통제) | ~9 | 9/9 (100%) — Phase 8~10에서 cover | 0 |
| **합계** | ~39 | **~37/39 (~95%)** | **~2 신규 + docs 매트릭스 본체** |

Lodestar 결선 자산이 SOC2 ~95% 자연 cover — docs 비중 절대 큼, 신규 코드 비중 작음.

---

## 4. Gap Analysis

현재 자산으로 cover되지 않는 SOC2 영역 + 본 epic cover 여부.

### 4.1 본 epic cover (Stage 11.B-2~8)

| Gap | TSC | cover Stage |
|---|---|---|
| `docs/compliance/` 트리 + control mapping 매트릭스 부재 | 전 통제 | Stage 11.B-2~4 |
| `auditor` 신규 role 미존재 — 현 RBAC에 `admin`/`operator`/custom만 | CC6.1 · CC6.3 | Stage 11.B-5 |
| 외부 감사인 audit log/checkpoint/evidence export wizard 부재 | CC6.1 · CC8.1 | Stage 11.B-5 |
| 통제 effectiveness 측정 dashboard 부재 — 통제별 audit event 집계 시각화 0 | CC4.1 · CC7.1 | Stage 11.B-6 |
| `soc2-controls` benchmark pack 부재 — Lodestar 자체를 SOC2 통제 단위로 스캔 | 전 통제 자동 검증 | Stage 11.B-7 |
| incident response runbook SOC2 형식 통합 부재 | CC7.4 | Stage 11.B-3(CC7 매핑 docs에 통합) |
| vendor inventory docs 부재 — LLM provider · snap store · cosign supply chain | CC9.2 | Stage 11.B-3(CC9 매핑 docs에 통합) |
| SOC2 framework yaml 부재 — `internal/domain/compliance/frameworks/` | 도메인 매핑 | Stage 11.B-3(soc2-type2-2017.yaml 신규) |

### 4.2 외부 트랙 ★ (본 epic 비범위)

| Gap | 외부 트랙 |
|---|---|
| 실 SOC2 Type II 90일 운영 효과성 측정 | ★ 외부 감사 firm (Deloitte/KPMG/PwC/BDO) 계약 |
| 외부 감사 firm 인증서 발급 | ★ 외부 firm |
| security awareness training 콘텐츠 (CC1.4) | ★ internal HR 또는 외부 트레이닝 벤더 |
| penetration testing (CC9.4) | ★ third-party 보안 firm (PortSwigger/Bishop Fox 등) |
| fraud risk assessment 라운드 (CC3.1) | ★ 외부 컨설팅 또는 내부 라운드 docs |
| business continuity plan 외부 검증 (CC9.1) | ★ 외부 firm |

---

## 5. 옵션 비교

본 epic 진입 시 4 옵션 — 각 옵션에 (a) 설계 요약 (b) 가치 (c) 노력 추정 (d) 전제·의존 (e) 리스크.

### 5.1 옵션 A — docs/compliance/ 매트릭스 + access wizard + dashboard 1차 (권장 default)

**설계 요약**: `docs/compliance/` 신규 트리 + CC1~CC9 + A1·A2·A5 control mapping 매트릭스 + `auditor` role 신규 + audit log/checkpoint/evidence export wizard + 통제 effectiveness dashboard(통제별 audit event 집계 + Grafana panel + web `/compliance` 페이지) + `soc2-controls` benchmark pack(~50~80 check, 통제 단위 yaml 자동 검증). Phase 11 backlog §4.2와 일관.

**가치**:
- paying customer ★★★★★ (enterprise 영업 critical gate, 외부 firm 진입 baseline)
- enterprise ★★★★★ / compliance ★★★★★ / operational ★★★ (effectiveness dashboard 운영자 가시성) / 기술 부채 ★

**노력 추정 (보수적)**: **6~9주**
- Stage 11.B-2: docs/compliance/ scaffold + CC1~CC4 매핑 1.5주
- Stage 11.B-3: CC5~CC9 매핑 + soc2 framework yaml 1.5주
- Stage 11.B-4: A1·A2·A5 매핑 0.5주
- Stage 11.B-5: auditor role + export wizard 1.5주
- Stage 11.B-6: effectiveness dashboard 1.5주
- Stage 11.B-7: soc2-controls pack(~50~80 check) 1.5~2주
- Stage 11.B-8: testcontainers + Playwright e2e + ops docs + v0.12.0 0.5~1주

**전제·의존**: 외부 SOC2 감사 firm 컨설팅(★)은 별 트랙 — docs/매핑/wizard/dashboard/pack은 외부 의존 0. Lodestar 결선 자산(audit chain · RBAC · SSO · cosign · TPM · multi-region) 활용.

**리스크**: **중**. effectiveness dashboard가 audit event 집계로 read-heavy query 추가 → 성능 영향 검증 필요(testcontainers 2-region에서 large dataset benchmark). soc2-controls pack ~50~80 check의 selftest fixture 작성 부담. audit log 형식이 외부 감사인 호환 — fg-verify v2 backward compat 보장 필수.

### 5.2 옵션 B — docs/compliance/ 매트릭스만 + 외부 firm 컨설팅 후 wizard/dashboard/pack 진입

**설계 요약**: Stage 11.B-2~4만 진입 — `docs/compliance/` 트리 + CC1~CC9 + A1·A2·A5 매핑 docs + SOC2 framework yaml. 외부 firm 컨설팅 라운드 후 firm 요구에 맞춰 wizard/dashboard/pack 분리 진입. 진입 노력 작음 + ROI 빠름.

**가치**:
- paying customer ★★★ (docs cover 만으로는 enterprise 영업 gate 부분 cover)
- enterprise ★★★ / compliance ★★★ / operational ★ / 기술 부채 ★

**노력 추정 (보수적)**: **3~4주** — Stage 11.B-2~4 + ops docs + v0.12.0 minor.

**전제·의존**: 외부 firm 컨설팅 라운드 *후* wizard/dashboard 요구 명세 받음. 본 옵션 진입 시 외부 firm 계약 trigger(★) 더 빠른 진입 가능.

**리스크**: **낮음**. 신규 코드 0, 회귀 영향 0. 단, enterprise 영업 demo 시 "docs만, 실 wizard/dashboard 없음"으로 customer 회의 가능.

### 5.3 옵션 C — docs/compliance/ + 외부 SOC2 감사 직접 계약 ★

**설계 요약**: 옵션 A 또는 B 진입 + 외부 SOC2 firm(Deloitte/KPMG/PwC/BDO 등) 직접 계약 + 90일 운영 측정 + 정식 SOC2 Type II 인증서 발급.

**가치**: ★★★★★ — 실 인증 보유로 enterprise 영업 직접 closing 가능.

**노력 추정**: docs/wizard/dashboard 본 epic + **외부 firm 계약 비용(~$30k~$100k+) + 90일 측정 기간 + 인증서 발급 ~3개월~6개월**.

**전제·의존**: ★ **사용자 외부 트랙** — 외부 firm 계약은 사용자 결정. memory `feedback_user_tracks.md` 일관 — D1 변리사 · E36 hands-on · 외부 감사 firm 등 외부/hands-on 트랙은 본 doc 진행 중 선택지에서 제외.

**리스크**: 외부 firm 일정 + 비용 + 90일 운영 측정 결과 변동 — 사용자 외부 결정.

### 5.4 옵션 D — ISO 27001 + SOC2 통합 docs

**설계 요약**: `docs/compliance/` 트리 + SOC2 + ISO 27001:2022 통합 매핑 + 공통 통제 cross-reference + 두 framework yaml 동기. broader scope.

**가치**:
- paying customer ★★★★★ (SOC2 + ISO 27001 양쪽 enterprise gate cover, EU + 미국 양쪽 시장)
- enterprise ★★★★★ / compliance ★★★★★ / operational ★★★ / 기술 부채 ★

**노력 추정 (보수적)**: **10~14주** — SOC2 단독 6~9주 + ISO 27001 추가 매핑 3~5주 + 두 framework yaml 동기 + 통합 docs.

**전제·의존**: ISO 27001 framework yaml은 이미 `internal/domain/compliance/frameworks/iso27001-2022.yaml` 결선 — 통합 매핑 자연 가능.

**리스크**: **중**. broader scope = 본 epic 추정 늘어남 + Phase 11 timeline 13~19주 → 16~22주 누적으로 R11-2(≤ 1.5개월 1순위) 한참 초과. SOC2 단독으로 enterprise 영업 critical gate cover 충분 → ISO 27001은 별 Phase 12 후보 권장.

### 5.5 옵션 비교 매트릭스

| 옵션 | 진입 노력 | 가치 종합 | 외부 트랙 의존 | Phase 11 timeline 적합도 |
|---|---|---|---|---|
| **A** docs + wizard + dashboard + pack | 6~9주 | ★★★★★ | 0 (실 인증은 ★) | ✅ (default) |
| **B** docs only | 3~4주 | ★★★ | 0 | ✅ (작은 진입) |
| **C** docs + 외부 firm 계약 | 6~9주 + ★3~6개월 | ★★★★★ (실 인증) | ★★★★ (외부 firm) | ⚠️ (외부 의존) |
| **D** ISO 27001 + SOC2 통합 | 10~14주 | ★★★★★ | 0 | ⚠️ (timeline 초과) |

---

## 6. Top 1 권장 + 근거

### 6.1 권장: 옵션 A (docs/compliance/ 매트릭스 + access wizard + dashboard + soc2-controls pack)

**근거**:
- **enterprise 영업 critical gate**: SOC2 통과 없이는 금융 · 의료 · 정부 enterprise customer 진입 불가능한 시장이 큼. paying customer 진입 *전* baseline 강화로 영업 사이클 단축. 옵션 B(docs only)는 영업 demo 시 "실 wizard/dashboard 없음" 회의 가능.
- **Lodestar 결선 자산 최대 활용**: §3.3 매트릭스에서 ~37/39 (~95%) 통제가 Phase 1~10 결선 자산으로 자연 cover. 신규 코드 비중 작음(auditor role + export wizard + dashboard + pack), docs 비중 큼 → 회귀 위험 작음 + 추정 안정.
- **외부 트랙 분리 명확**: 실 SOC2 firm 계약(옵션 C)은 사용자 외부 트랙 ★. 본 epic은 외부 firm 진입 baseline까지 — 외부 의존 0.
- **Phase 11 backlog 일관**: §4.2 + §12.1에서 옵션 B(SOC2) 1순위 채택. 옵션 A(docs + wizard + dashboard + pack)는 backlog 권장 default와 일관.
- **추정 6~9주 = R11-2 약간 초과**: backlog §5.1 인용 — "docs/pack 작업 비중이 커서 외부 트랙 의존 부재로 충분히 분할 가능". Stage 11.B-2~8 7 Stage 분해로 분할 cover.

### 6.2 옵션 B(docs only)와의 비교

옵션 B 진입 시 3~4주로 마감 가능하나 영업 demo 시 enterprise customer가 "통제 effectiveness 실 측정 도구 0" 지적 가능 → 결국 옵션 A로 재진입 → 누적 노력 증가. 옵션 A 한 epic으로 한번에 baseline 완비가 효율.

### 6.3 옵션 D(ISO 27001 통합)와의 비교

ISO 27001 통합은 가치 ★★★★★이나 추정 10~14주 → Phase 11 timeline R11-2 한참 초과. ISO 27001은 별 Phase 12 후보 — SOC2 단독 마감 후 자연 진입(공통 통제 cross-reference 자연 가능, ISMS-P 한국 시장 + ISO 27001 EU 시장 추가 cover).

---

## 7. Stage 분해 (옵션 A 채택 가정)

memory `feedback_design_doc_first.md` 일관 — Stage 단위 분해 + 보수적 추정.

### 7.1 Stage 11.B-1 — 본 design doc

본 round. docs only, 코드 0. **마감**.

### 7.2 Stage 11.B-2 — docs/compliance/ scaffold + CC1~CC4 매핑

추정 **1.5주**.
- `docs/compliance/README.md` — SOC2 Type II 개요 + Lodestar 결선 자산 매핑 표 + 외부 firm 진입 절차(★ 외부 트랙 명시).
- `docs/compliance/cc1-control-environment.md` — CC1 governance 매핑(repo public + Apache 2.0 + CLAUDE.md + design doc 패턴).
- `docs/compliance/cc2-communication.md` — CC2 communication(docs/onboarding + docs/operations + release notes + CHANGELOG).
- `docs/compliance/cc3-risk-assessment.md` — CC3 risk(design doc §risk + audit chain immutability + threat model).
- `docs/compliance/cc4-monitoring.md` — CC4 monitoring(Prometheus + Grafana + 5 alert + effectiveness dashboard placeholder).
- 한국어 docs 본문 + 영어 요약(외부 firm 대상).

### 7.3 Stage 11.B-3 — CC5~CC9 매핑 + SOC2 framework yaml

추정 **1.5주**.
- `docs/compliance/cc5-control-activities.md` — CC5 control activities(RBAC + segregation of duties).
- `docs/compliance/cc6-logical-access.md` — CC6.1~CC6.8 통합(RBAC + SSO + audit signer key rotation + TPM + TLS + audit chain immutability).
- `docs/compliance/cc7-system-operations.md` — CC7.1~CC7.5 통합(Prometheus + audit chain + Patroni + multi-region + incident response runbook SOC2 형식 통합).
- `docs/compliance/cc8-change-management.md` — CC8.1(trunk-based + signed releases + audit chain).
- `docs/compliance/cc9-risk-mitigation.md` — CC9.1~CC9.2 통합(multi-region HA + vendor inventory: LLM 4 provider · snap store · cosign supply chain).
- `internal/domain/compliance/frameworks/soc2-type2-2017.yaml` 신규 — 기존 isms-p/iso27001/nist-800-53 패턴 cargo cult. 통제 코드 + 명칭 + 매핑.

### 7.4 Stage 11.B-4 — A1·A2·A5 매핑

추정 **0.5주** (A3·A4·A6 보류).
- `docs/compliance/a1-availability.md` — A1.1~A1.3(multi-region + Patroni + backup S3).
- `docs/compliance/a2-confidentiality.md` — A2.1~A2.3(TLS + redaction + air-gap + LLM private + evidence access).
- `docs/compliance/a5-security.md` — A5.1~A5.3(RBAC + audit chain + TPM + cosign).
- soc2-type2-2017.yaml 갱신 — A1·A2·A5 통제 추가.

### 7.5 Stage 11.B-5 — `auditor` role 신규 + audit export wizard

추정 **1.5주**.
- `internal/domain/tenant/role.go` — `auditor` role 신규(read-only 매트릭스). RBAC fine-grained 결선 활용.
- `internal/api/handlers/compliance_export.go` 신규 — `GET /api/v1/compliance/auditor-bundle?period=90d` endpoint(admin + auditor 권한). 응답: audit_entries + cosign signatures + chain_keys(epoch별 public key) + 통제별 evidence index + README.md.
- bundle 구조 docs — `docs/compliance/auditor-bundle-format.md`.
- 단위 + e2e + boundary test(auditor write 차단, admin export 허용).

### 7.6 Stage 11.B-6 — 통제 effectiveness dashboard

추정 **1.5주**.
- `internal/api/handlers/compliance_effectiveness.go` 신규 — `GET /api/v1/compliance/effectiveness?control=CC6.6` endpoint. tenant scope 필수(설계서 §1.4 일관).
- 통제별 audit event 집계 query — CC6.6 → `audit.chain.key_rotated` count + 정기성(90일 이내 검증) + 마지막 evidence 시각.
- `deploy/grafana/dashboards/compliance-effectiveness.json` 신규 — 통제별 status panel.
- web `/compliance` 페이지 — admin + auditor 권한 카드(통제별 status + 마지막 evidence + drill-down).
- i18n 키(ko + en) + e2e Playwright + boundary test.

### 7.7 Stage 11.B-7 — `soc2-controls` benchmark pack

추정 **1.5~2주**.
- `packs/soc2-controls/pack.yaml` 신규 — apiVersion + compatibility(`platform: any`, `category: compliance`).
- `packs/soc2-controls/checks/` — ~50~80 yaml check, Lodestar 자체를 스캔하여 통제 충족 자동 평가:
  - 예: `CC6.6-key-rotation-quarterly.yaml` → audit chain key rotation 90일 이내 검증(audit endpoint query).
  - 예: `CC5.2-rbac-least-privilege.yaml` → admin 권한 사용자 수 임계 검증.
  - 예: `CC7.2-monitoring-anomaly.yaml` → multi-region 5 alert rule 활성 검증.
  - 예: `A1.1-availability-multi-region.yaml` → Patroni primary + standby 활성 검증.
- selftest fixture 1:1 매칭 — `ValidatePackYAMLBytes` + `ParseCheckYAML` + `ParseSelfTestYAML` + `RunCheckSelfTest` PASS.
- archive(`packs/soc2-controls.tar.gz`) + Makefile PACKS_SOURCE 등록.

### 7.8 Stage 11.B-8 — testcontainers integration + ops docs + v0.12.0 minor release

추정 **0.5~1주**.
- `test/integration/compliance_e2e_test.go` — 90일 simulation + auditor bundle export + effectiveness aggregate(testcontainers 2-region).
- Playwright e2e — `/compliance` 페이지 + auditor 권한 게이트 + bundle export 흐름.
- `docs/operations/soc2-readiness.md` 신규 — 운영자 가이드(외부 firm 진입 절차 + 90일 측정 절차 + bundle export 절차).
- `docs/releases/v0.12.0.md` + CHANGELOG entry.
- v0.12.0 minor tag(사용자 명시 후 push, CLAUDE.md 일관).

### 7.9 Stage 분해 합계

| Stage | 추정 |
|---|---|
| 11.B-1 (본 doc) | **마감** |
| 11.B-2 docs CC1~CC4 | 1.5주 |
| 11.B-3 docs CC5~CC9 + framework yaml | 1.5주 |
| 11.B-4 docs A1·A2·A5 | 0.5주 |
| 11.B-5 auditor role + export wizard | 1.5주 |
| 11.B-6 effectiveness dashboard | 1.5주 |
| 11.B-7 soc2-controls pack | 1.5~2주 |
| 11.B-8 e2e + ops docs + v0.12.0 | 0.5~1주 |
| **합계** | **~8~10주** (보수적, phase11-backlog §4.2 추정 6~9주와 일관, memory `feedback_design_doc_conservative.md` 약간 늘려잡음) |

---

## 8. 결정 항목 (D-P11B-1~4)

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시.

### 8.1 D-P11B-1 — 옵션 채택

- (1) **옵션 A** — docs/compliance/ 매트릭스 + auditor role + export wizard + effectiveness dashboard + soc2-controls pack (**권장 default**). 추정 ~8~10주.
- (2) 옵션 B — docs/compliance/ 매트릭스만(Stage 11.B-2~4 + ops docs + v0.12.0). 추정 ~3~4주. 외부 firm 컨설팅 후 wizard/dashboard/pack 별 epic.
- (3) 옵션 C — 옵션 A + 외부 SOC2 firm 계약(★ 사용자 외부 트랙).
- (4) 옵션 D — ISO 27001 + SOC2 통합 docs. 추정 ~10~14주. Phase 11 timeline 한참 초과 — Phase 12 후보 권장.

**근거**: Lodestar 결선 자산이 SOC2 ~95% 자연 cover + 외부 트랙 의존 0 + enterprise 영업 critical gate. 옵션 A 한 epic으로 baseline 완비가 효율. 옵션 D는 ISO 27001 + SOC2 통합으로 timeline 초과 — Phase 12 권장.

### 8.2 D-P11B-2 — A1~A5 cover 범위

- (1) **A1 Availability + A2 Confidentiality + A5 Security 우선** — A3·A4·A6 보류(**권장 default**). multi-region + redaction + RBAC 결선 자산 자연 cover.
- (2) A1·A2·A3·A5 전체 — A3(Processing Integrity)는 audit chain hash chain이 자연 cover하므로 추가 docs 0.5주 비용으로 cover 가능.
- (3) A1·A2·A4·A5 — A4(Privacy)는 GDPR/CCPA 별 트랙으로 분리 권장.
- (4) A1·A2·A3·A4·A5 전체 — 가장 broad. A4는 GDPR/CCPA 별 epic으로 분리 권장 → 본 epic 피하는 게 timeline 안정.

**근거**: A1·A2·A5는 결선 자산 자연 cover로 ROI 높음. A3는 추가 0.5주 비용으로 cover 가능하나 첫 round에서는 default A1+A2+A5만 권장. A4는 GDPR/CCPA 별 epic.

### 8.3 D-P11B-3 — 감사인 access wizard 범위

- (1) **`auditor` 신규 role + audit log/checkpoint/evidence export bundle endpoint** (**권장 default**). RBAC fine-grained 결선 활용, 신규 코드 작음.
- (2) 기존 `viewer` role 확장 — `viewer`에 export 권한 추가. role 분리 부재로 외부 감사인과 internal viewer 격리 불가능. 권장 X.
- (3) 신규 role 없이 admin role 활용 + IP allowlist — 외부 감사인에 admin role 부여는 보안 원칙 위반. 권장 X.

**근거**: 외부 감사인은 read-only + write 차단 + 감사 사본 export 권한이 필요한 별 role. 기존 viewer 확장은 격리 부재로 거부.

### 8.4 D-P11B-4 — soc2-controls pack 운영 방식

- (1) **CIS-style yaml 자동 검증 pack** (~50~80 check, **권장 default**). Lodestar 자체를 스캔하여 통제 충족 자동 평가. effectiveness dashboard와 통합.
- (2) docs only 매트릭스 — `docs/compliance/` 트리만, pack 0. 자동 검증 0, 운영자 수동 점검 의존.
- (3) yaml pack + 통제별 PROM metric 자동 emit — pack 실행 결과를 Prometheus metric으로 emit. 옵션 (1) + 옵션 A(OpenTelemetry, 3순위) 진입 시 자연 통합.

**근거**: pack 자동 검증은 effectiveness dashboard에 audit event 자연 emit + 운영자 수동 점검 부담 0 + 외부 firm이 자동 검증 결과를 evidence로 인정 가능. (3)은 OpenTelemetry 진입 시 자연 통합 — 본 epic에서는 (1) default.

---

## 9. 외부 트랙 (★ 표기, memory `feedback_user_tracks.md` 일관)

본 epic 권장 default에서 제외 — 사용자 외부 트랙.

- ★ **실 SOC2 Type II 감사 firm 컨설팅 + 90일 운영 측정 + 인증서 발급** — 외부 firm(Deloitte/KPMG/PwC/BDO/A-LIGN 등) 계약. 본 epic 마감 후 외부 firm 진입 baseline 완비, 사용자 외부 결정.
- ★ **security awareness training 콘텐츠 (CC1.4)** — internal HR 트랙 또는 외부 트레이닝 벤더(KnowBe4/SANS 등).
- ★ **penetration testing (CC9.4)** — third-party 보안 firm(PortSwigger/Bishop Fox/Trail of Bits 등) 위탁.
- ★ **fraud risk assessment 라운드 (CC3.1)** — 외부 컨설팅 또는 internal 라운드 docs(별 epic, Phase 12 후보).
- ★ **business continuity plan 외부 검증 (CC9.1)** — 외부 firm 컨설팅.

---

## 10. 리스크

| 리스크 | 가능성 | 영향 | 완화 |
|---|---|---|---|
| 외부 SOC2 firm 의존 — 본 epic은 baseline까지, 실 인증은 외부 ★ | 높음 | enterprise 영업 closing 지연 | 외부 firm 진입 baseline 완비 + 사용자 외부 트랙 명시 |
| effectiveness dashboard read-heavy aggregation query 성능 영향 | 중 | 운영자 페이지 latency 증가 | testcontainers 2-region에서 large dataset benchmark(Stage 11.B-8). tenant scope index 활용. |
| soc2-controls pack ~50~80 check maintenance 부담 | 중 | 통제 변경 시 pack + framework yaml 동기 부담 | selftest fixture 1:1 매칭 + CI 자동 검증 |
| audit log 형식이 외부 firm 호환 — fg-verify v2 backward compat | 낮음 | 외부 firm 검증 실패 위험 | Phase 10.D 결선 fg-verify v2 backward compat 유지 + Stage 11.B-5 bundle 형식 docs 명시 |
| `auditor` role 신규로 RBAC boundary 추가 → boundary test 부담 | 중 | RBAC 회귀 위험 | rbac_integration_test.go 갱신 + boundary test 명시 |
| Stage 11.B-2~4 docs 작성 분량 큼 — 한국어 + 영어 양쪽 | 중 | timeline 초과 위험 | 한국어 본문 + 영어 요약(외부 firm 대상) 분리 — 영어 full translation은 외부 firm 진입 시 별 round |
| Phase 11 backlog §12.1 누적 timeline 13~19주 — 본 epic 6~9주는 ~1.5~2개월 | 중 | 사용자 timeline 인식 차이 | 본 doc §7.9 보수적 8~10주 명시, 사용자 합의 |

---

## 11. 비목표 / 거부

본 epic에서 명시 거부:

### 11.1 ISO 27001 / GDPR / HIPAA / PCI-DSS 매핑

옵션 D 비교만, 본 epic은 SOC2 단독. ISO 27001은 별 Phase 12 후보(`internal/domain/compliance/frameworks/iso27001-2022.yaml` 이미 결선, 통합 매핑 자연 가능). GDPR/CCPA는 A4 Privacy 별 트랙. HIPAA/PCI-DSS는 산업 별 epic.

### 11.2 외부 SOC2 감사 firm 직접 계약 ★

사용자 외부 트랙 — memory `feedback_user_tracks.md` 일관. 본 epic은 외부 firm 진입 baseline까지.

### 11.3 security awareness training 콘텐츠 ★

internal HR 트랙 또는 외부 트레이닝 벤더. 본 epic 비범위.

### 11.4 penetration testing 실행 ★

third-party 외부 위탁. 본 epic은 testcontainers e2e + boundary test까지.

### 11.5 90일 운영 효과성 측정 자체

SOC2 Type II 정의상 외부 firm이 측정. 본 epic은 자체 측정 도구(effectiveness dashboard + soc2-controls pack)만 cover — 90일 측정 결과 자체는 외부 firm 트랙 ★.

### 11.6 LLM 필수 경로 도입

설계서 §1.2 옵트인 원칙 일관 — SOC2 control mapping에서 advisor reasoning trace 옵션 활용 가능하나 LLM 필수 경로 0.

### 11.7 tenant_id 없는 신규 테이블

설계서 §1.4 멀티테넌시 원칙 일관 — effectiveness dashboard 신규 query는 tenant scope 필수. compliance_export bundle도 tenant scope 명시.

### 11.8 UPDATE/DELETE 가능한 audit 테이블

설계서 §1.9 불변성 원칙 일관 — soc2-controls pack 자동 검증 결과는 audit event로 append만, 기존 audit_entries 변경 0.

### 11.9 Remote push 자동화

CLAUDE.md 일관 — local 커밋 OK, remote push 사용자 명시 요청 시에만. v0.12.0 minor release tag push도 사용자 결정.

---

## 12. 참조

### 12.1 직전 design doc 패턴

- `docs/design/notes/phase11-backlog-design.md` §4.2 + §12 — 본 doc 직접 부모, Top 3 순차 B→C→A 확정 + 옵션 B 1순위 권장.
- `docs/design/notes/audit-chain-rotation-automation-design.md` — Phase 10 옵션 D 본체, fact-check 패턴 + Stage 분해 패턴 직접 모방.
- `docs/design/notes/ros2-humble-dds-sros2-design.md` — Phase 10 옵션 E 본체, 옵션 비교 매트릭스 패턴 직접 모방.
- `docs/design/notes/customer-onboarding-design.md` — Phase 6 1순위 doc 패턴.
- `docs/design/notes/rbac-fine-grained-design.md` — RBAC fine-grained 결선 doc, auditor role 도입 시 참조.
- `docs/design/notes/multi-region-ha-design.md` — A1 Availability 매핑 참조.

### 12.2 AICPA SOC2 공식 docs

- AICPA Trust Services Criteria 2017 (with revised points of focus 2022) — CC1~CC9 + A1~A6 정의.
- AICPA Description Criteria 2018 — Type II 90일 운영 측정 정의.
- SOC2 audit firm 비교: A-LIGN · BDO · Coalfire · Deloitte · KPMG · PwC 등 (★ 외부 트랙 참조).

### 12.3 Lodestar 결선 보안 자산 (Phase 1~10)

- audit chain immutability + hash chain — `internal/domain/audit/hash.go` · `audit.go` · `checkpoint.go` · `export.go` (Phase 1+).
- audit signer key rotation 자동 + epoch별 public key 보존 — `internal/domain/audit/keyrotation/rotator.go` + 0037 마이그레이션 (Phase 10.D, v0.10.0~v0.10.2).
- audit entry segment rotation + cosign 서명 — `internal/domain/audit/rotation/` (Phase 6 결선).
- fg-verify v2(epoch별 검증, backward compat) — `cmd/rosshield-audit-verify/` (Phase 10.D-5).
- RBAC fine-grained + fleet scope + role/permission dual layer — `internal/api/handlers/rbac_middleware.go` + `internal/platform/authz/` (Phase 5).
- SSO/SAML/OIDC + group→role — `internal/api/handlers/sso.go` + `internal/domain/tenant/sso/` (Phase 5).
- cosign keyless signed releases + Sigstore Rekor — `.github/workflows/release-pipeline.yml` + 38 release(v0.3.0~v0.11.0) (Phase 7+).
- TPM 2.0 PCR-sealed keystore + Secure Boot + robot identity attestation — `internal/platform/keystore/tpm/` + `internal/enterprise/robotid/` (Phase 5 E34).
- multi-region HA + Patroni 자동 failover + replication — `internal/platform/replication/` + `internal/platform/ha/patroni/` (Phase 8+9).
- Prometheus + Grafana + 5 alert rule + multi-region dashboard — `internal/platform/metrics/` + `deploy/grafana/dashboards/` (Phase 4 + Phase 10.A).
- compliance 도메인 + 3 framework yaml(isms-p/iso27001/nist-800-53) — `internal/domain/compliance/` (E17 결선).

### 12.4 설계서 / 원칙

- `docs/design/01-principles.md` — 12 원칙(특히 §1.1 결정론적 증거 + §1.4 멀티테넌시 + §1.9 불변성).
- `docs/design/06-security-and-tenancy.md` — 보안 · 멀티테넌시 본체.
- `docs/design/10-audit-and-observability.md` — 해시 체인 + 관측성.
- `docs/design/12-migration-and-non-goals.md` — 비목표.

### 12.5 memory feedback

- `feedback_design_doc_first.md` — 1일+ 임계 design doc 우선.
- `feedback_design_doc_conservative.md` — 보수적 추정.
- `feedback_parallel_agents.md` — 매 stage 시작 시 병렬 가능성 재평가(본 epic은 default 1 epic, Stage 진입 시 재평가).
- `feedback_user_tracks.md` — D1 변리사 · E36 hands-on · 외부 SOC2 firm · customer trigger 등 외부 트랙 제외(★ 표기).
- `feedback_recommend_next_actions.md` — 다음 추천 작업 3~5건 명시.
- `feedback_skip_handoff.md` — handoff edit/commit/push 생략, CHANGELOG + release notes + commit 메시지로 trace.

---

## 13. 결정 안내 (사용자 round)

본 doc 마감 시 사용자 D-P11B-1~4 4 결정 round:

- **D-P11B-1 옵션 채택** — 권장 default 옵션 A (docs + auditor + dashboard + pack, ~8~10주).
- **D-P11B-2 A1~A5 cover 범위** — 권장 default A1+A2+A5 (A3·A4·A6 보류).
- **D-P11B-3 감사인 wizard 범위** — 권장 default `auditor` 신규 role + export bundle endpoint.
- **D-P11B-4 soc2-controls pack 운영** — 권장 default CIS-style yaml 자동 검증 (~50~80 check).

사용자 4 결정 round 1회로 합의 후 Stage 11.B-2 진입 가능.

---

## 13. 결정 확정 (2026-05-21)

사용자 D-P11B-1·2·3·4 결정:

- **D-P11B-1 = 옵션 A** (compliance docs + auditor role + dashboard + 자동 검증 pack 일괄, 권장 default).
- **D-P11B-2 = 전체 5 (A1~A5)** — sub-agent 권장(A1·A2·A5 우선)에서 broader cover로 변경. A6 Custodial은 미적용(rare).
- **D-P11B-3 = auditor role 신규** — RBAC 일관, 세분 분리, audit log export wizard 전용 role.
- **D-P11B-4 = 자동 검증 pack** — soc2-controls pack ~50~80 check CIS-style yaml, 최대 ROI.

### 13.1 결정 amendment에 따른 Stage 분해 조정

- Stage 11.B-1: 본 design doc (마감, dac77e5)
- Stage 11.B-2: docs/compliance/ scaffold + CC1~CC4 control mapping (1~2주)
- Stage 11.B-3: CC5~CC9 control mapping (1주)
- Stage 11.B-4: **A1~A5 전체 cover** (A6 제외) — Availability + Confidentiality + Processing Integrity + Privacy + Security (0.5~1주, broader)
- Stage 11.B-5: auditor role 신규 + audit log export wizard (1~1.5주)
- Stage 11.B-6: effectiveness dashboard (`/compliance` 페이지) (1~1.5주)
- Stage 11.B-7: soc2-controls 자동 검증 pack (~50~80 check, 1~2주)
- Stage 11.B-8: testcontainers integration + ops docs + v0.12.0 minor release (0.5주)

총 보수적 ~8~10주.

