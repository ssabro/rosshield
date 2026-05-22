# Lodestar Compliance — SOC2 Type II Readiness Baseline

> **상태**: **Phase 11 옵션 B 완전 마감 (v0.12.0)** — `docs/compliance/` 14 매트릭스 docs (CC1~CC9 + A1~A5, 47 sub-controls) + `auditor` read-only role 활성 + audit log export wizard (`POST /api/v1/compliance/export` + `/compliance/export` UI) + effectiveness dashboard (`GET /api/v1/compliance/effectiveness` + `/compliance/effectiveness` UI, 14 카테고리 × 40 sub-control 매핑) + `soc2-controls` 자동 검증 pack (61 check, CC1~CC9 + A1~A5 8 카테고리 cover) 결선.
> **버전**: v0.12.0 (Phase 11 첫 minor, Top 3 순차 B → C → A 진행 중).
> **외부 트랙 ★**: 실 SOC2 Type II 감사 firm 계약(Deloitte/KPMG/PwC/BDO/A-LIGN 등)은 사용자 외부 트랙. 본 트리는 외부 firm 진입 baseline을 제공합니다.
> **설계 근거**: `docs/design/notes/soc2-readiness-design.md` §13 — D-P11B-1 = 옵션 A · D-P11B-2 = 전체 5(A1~A5, A6 미적용) · D-P11B-3 = auditor role 신규 · D-P11B-4 = 자동 검증 pack.

---

## §1 목적

Lodestar의 SOC2 Type II readiness baseline을 정착합니다. 본 트리는 다음을 제공합니다.

- **SOC2 Trust Services Criteria(TSC) 통제 매핑** — Common Criteria CC1~CC9 + Additional Criteria A1~A5 각 sub-control에 대한 Lodestar 결선 자산 cross-reference.
- **gap analysis** — Lodestar에 cover되지 않은 영역 정직 명시 + 외부 트랙 ★ 분리.
- **외부 감사인 진입 baseline** — 실 SOC2 Type II 감사 firm이 90일 운영 효과성 측정을 시작할 수 있는 통제 설계 정합성 docs.

본 트리는 **자체 인증 발급을 의미하지 않습니다**. SOC2 Type II 인증서는 AICPA 회원 외부 감사 firm만 발급할 수 있습니다(★). 본 트리는 외부 firm 진입 *전* "통제 설계가 SOC2 TSC에 매핑된다"를 입증하는 baseline입니다.

---

## §2 범위

SOC2(AICPA Trust Services Criteria 2017, Description Criteria 2018 갱신) 두 축 cover.

### §2.1 Common Criteria (필수, 모든 SOC2 audit 적용)

| TSC 군 | sub-controls | docs |
|---|---|---|
| CC1 Control Environment | CC1.1~CC1.5 (5) | [`soc2/cc1-control-environment.md`](./soc2/cc1-control-environment.md) |
| CC2 Communication and Information | CC2.1~CC2.3 (3) | [`soc2/cc2-communication-information.md`](./soc2/cc2-communication-information.md) |
| CC3 Risk Assessment | CC3.1~CC3.4 (4) | [`soc2/cc3-risk-assessment.md`](./soc2/cc3-risk-assessment.md) |
| CC4 Monitoring Activities | CC4.1~CC4.2 (2) | [`soc2/cc4-monitoring-activities.md`](./soc2/cc4-monitoring-activities.md) |
| CC5 Control Activities | CC5.1~CC5.3 (3) | [`soc2/cc5-control-activities.md`](./soc2/cc5-control-activities.md) |
| CC6 Logical and Physical Access | CC6.1~CC6.8 (8) | [`soc2/cc6-logical-physical-access.md`](./soc2/cc6-logical-physical-access.md) |
| CC7 System Operations | CC7.1~CC7.5 (5) | [`soc2/cc7-system-operations.md`](./soc2/cc7-system-operations.md) |
| CC8 Change Management | CC8.1 (1) | [`soc2/cc8-change-management.md`](./soc2/cc8-change-management.md) |
| CC9 Risk Mitigation | CC9.1~CC9.2 (2) | [`soc2/cc9-risk-mitigation.md`](./soc2/cc9-risk-mitigation.md) |

### §2.2 Additional Criteria (D-P11B-2 결정: 전체 5)

| TSC | 명칭 | 본 epic cover | docs |
|---|---|---|---|
| A1 Availability | 가용성 — 운영 · 모니터링 · 인시던트 · 복구 | ✅ | [`soc2/a1-availability.md`](./soc2/a1-availability.md) |
| A2 Confidentiality | 기밀성 — 식별 · 저장 · 폐기 · 전송 보호 | ✅ | [`soc2/a2-confidentiality.md`](./soc2/a2-confidentiality.md) |
| A3 Processing Integrity | 처리 무결성 — 정합성 · 완전성 · 적시성 | ✅ | [`soc2/a3-processing-integrity.md`](./soc2/a3-processing-integrity.md) |
| A4 Privacy | 프라이버시 — 개인정보 (SOC2 baseline만; GDPR · CCPA · 한국 PIPA 상세는 별 epic ★) | ✅ | [`soc2/a4-privacy.md`](./soc2/a4-privacy.md) |
| A5 Security | 보안 — 통제 자산 보호 (CC 안에 cover, cross-reference 위주) | ✅ | [`soc2/a5-security.md`](./soc2/a5-security.md) |
| A6 Custodial | 금융 custody 전용 | ❌ (Lodestar 적용 0) | — |

---

## §3 Lodestar 결선 자산 매핑 개요

Phase 1~10에서 결선된 자산이 SOC2 TSC ~95% 자연 cover. 각 통제 docs에 코드 path + docs path 인용. 핵심 결선 자산:

| 자산 | 코드 / docs | 매핑되는 SOC2 통제 |
|---|---|---|
| **Audit chain immutability + hash chain** | `internal/domain/audit/hash.go` · `audit.go` · `checkpoint.go` · `export.go` | CC6.6 · CC7.2 · CC8.1 · A3 |
| **Audit signer key rotation 자동 (90일 quarterly)** | `internal/domain/audit/keyrotation/rotator.go` + 0037 마이그레이션 | CC6.6 · CC7.2 |
| **fg-verify v2 (epoch별 검증, backward compat)** | `cmd/rosshield-audit-verify/` | CC6.6 · CC8.1 · A3 |
| **RBAC fine-grained (role × permission × fleet scope dual layer)** | `internal/api/handlers/rbac_middleware.go` + `internal/platform/authz/` | CC6.1 · CC6.3 · CC5.2 |
| **SSO/SAML/OIDC + group→role 자동 매핑** | `internal/api/handlers/sso.go` + `internal/domain/tenant/sso/` | CC6.1 · CC6.2 |
| **cosign keyless signed releases + Sigstore Rekor** | `.github/workflows/release-pipeline.yml` + `internal/domain/audit/rotation/cosign.go` | CC8.1 · CC9.2 |
| **TPM 2.0 PCR-sealed keystore + Secure Boot** | `internal/platform/keystore/tpm/` + `internal/enterprise/robotid/` | CC6.6 · CC6.7 |
| **Multi-region HA + Patroni 자동 failover (RTO ≤ 60s)** | `internal/platform/replication/` + `internal/platform/ha/patroni/` | A1.1 · A1.2 · A1.3 · CC7.5 |
| **Prometheus + Grafana + 5 alert rule + multi-region dashboard** | `internal/platform/metrics/` + `deploy/grafana/dashboards/` + `deploy/prometheus/alerts/multi-region.yml` | CC4.1 · CC7.1 · CC7.2 |
| **Compliance 도메인 + 3 framework yaml(isms-p/iso27001/nist-800-53)** | `internal/domain/compliance/` | docs/매핑 자체 cover |
| **Code of Conduct + Security Policy + Contributing guide** | `CODE_OF_CONDUCT.md` · `SECURITY.md` · `CONTRIBUTING.md` | CC1.1 · CC2.3 |
| **Webhook delivery + alertmanager 외부 통보** | `internal/api/handlers/webhook.go` + `deploy/prometheus/alertmanager-sample.yml` | CC2.3 · CC4.2 |

---

## §4 외부 감사인 access — `auditor` role (Stage 11.B-5 ✅ 마감, v0.12.0)

본 트리는 외부 감사인이 자체적으로 접근할 read-only role 및 export wizard를 제공합니다.

- **role**: `auditor` — `internal/platform/authz/permission_matrix.go` 매트릭스에 결선 (RBAC fine-grained 재사용, 신규 role 정의 코드 0). audit.read/verify/export + compliance.read/export + report.read/verify/export + system.read 보유, write 권한 일체 0 (read-only 엄격).
- **export wizard**: `POST /api/v1/compliance/export` (admin + auditor 권한, `RequirePermission(ResourceAudit, ActionExport)` 게이트). body `{"format":"v1|v2","period":"<duration>","tenantId":"<id>"}` (optional defaults). 응답은 audit bundle tar.gz + 응답 헤더 `X-Audit-Entry-Seq` (export event entry seq).
- **export bundle**: 기존 `sqliterepo.Repo.ExportV2` (Stage 10.D-5) 재사용 — `chainKeyEpochs` 메타 포함, fg-verify v2 backward compat. keyRepo nil 시 v1 fallback byte-identical 호환.
- **UI**: `/compliance/export` 페이지 (admin + auditor visible, system.read 게이트). i18n `compliance.export.*` (ko + en).
- **emit**: `audit.compliance.export` event 동일 Tx emit (외부 감사인 추적 가능).
- **RBAC boundary test**: `rbac_integration_test.go` endpoint count 29 → 30, 6 페르소나 × 30 endpoint = 180 case 회귀 0.

---

## §5 통제 effectiveness dashboard (Stage 11.B-6 ✅ 마감, v0.12.0)

본 트리는 통제 충족도를 자동 집계하여 운영자/감사인에게 가시화합니다.

- **endpoint**: `GET /api/v1/compliance/effectiveness` (admin + auditor 권한, audit.export 게이트). read-only handler — audit emit 0.
- **매핑**: `internal/domain/compliance/soc2_mapping.go` (416줄, Go map). **14 카테고리 × 40 sub-control × audit action**. CC1~CC9 + A1·A2·A5 cover (D-P11B-2 default 일관).
- **aggregation**: `audit.EffectivenessAggregator` interface + `audit/sqliterepo.CountActionsByWindows`. read-heavy 단일 query (`audit_entries action IN (...) + occurred_at >= ?`, tenant scope index 활용). 1d / 7d / 30d window 동시 집계.
- **UI**: `/compliance/effectiveness` 페이지 — cover% hero · 매트릭스 표 · gaps 카드 + drill-down. cover% rollup = covered=true sub-control / total × 100. i18n `compliance.dashboard.*` ko + en (40 신규 키).

---

## §6 자동 검증 — `soc2-controls` benchmark pack (Stage 11.B-7 ✅ 마감, v0.12.0)

본 트리는 docs only에 그치지 않고 **자동 검증 pack**으로 통제 충족을 측정합니다.

- **pack 경로**: `packs/soc2-controls/` (v0.1.0, **61 check** CIS-style yaml).
- **카테고리 cover (8/8)**: CC1 2 · CC2 3 · CC3 3 · CC4 2 · CC5 3 · CC6 15 · CC7 10 · CC8 5 · CC9 2 + A1 5 · A2 3 · A3 2 · A4 1 · A5 5.
- **예시 check**:
  - `CC6.6-audit-key-rotation.yaml` → audit chain key rotation 90일 이내 검증 (Lodestar API curl).
  - `CC6.1-rbac-enabled.yaml` → RBAC 매트릭스 활성 검증.
  - `CC7.1-alert-rules.yaml` → Prometheus 5 alert rule 활성 검증.
  - `A1.1-multi-region-ha.yaml` → Patroni primary + standby 활성 검증.
- **audit cmd 패턴 3종 mix**: host-side bash (config grep / file 존재 / perms / systemctl) + Lodestar API curl (`ROSSHIELD_API_URL` + `ROSSHIELD_API_TOKEN` env) + docs 존재 (runbook/policy).
- **selftest 61 fixture**: PASS + FAIL + site policy skip 3 case 표준. env 미설정 시 PASS skip 패턴 일관 (false positive 0, site policy 유연성).
- **회귀 검증**: `internal/domain/benchmark/soc2_controls_fixture_test.go` (round-trip + count [50,80] guard).
- **effectiveness dashboard 통합**: `/compliance/effectiveness` 페이지에서 cross-reference.

---

## §7 디렉터리 구조

```
docs/compliance/
├─ README.md                            # 본 파일 — 인덱스 + Lodestar 매핑 개요
└─ soc2/                                # SOC2 Trust Services Criteria 매핑 (14 docs · 47 sub-controls)
   ├─ cc1-control-environment.md        # CC1.1~CC1.5 (Stage 11.B-2)
   ├─ cc2-communication-information.md  # CC2.1~CC2.3 (Stage 11.B-2)
   ├─ cc3-risk-assessment.md            # CC3.1~CC3.4 (Stage 11.B-2)
   ├─ cc4-monitoring-activities.md      # CC4.1~CC4.2 (Stage 11.B-2)
   ├─ cc5-control-activities.md         # CC5.1~CC5.3 (Stage 11.B-3)
   ├─ cc6-logical-physical-access.md    # CC6.1~CC6.8 (Stage 11.B-3)
   ├─ cc7-system-operations.md          # CC7.1~CC7.5 (Stage 11.B-3)
   ├─ cc8-change-management.md          # CC8.1 (Stage 11.B-3)
   ├─ cc9-risk-mitigation.md            # CC9.1~CC9.2 (Stage 11.B-3)
   ├─ a1-availability.md                # A1.1~A1.3 (Stage 11.B-4)
   ├─ a2-confidentiality.md             # A2.1~A2.2 (Stage 11.B-4)
   ├─ a3-processing-integrity.md        # A3.1~A3.4 (Stage 11.B-4)
   ├─ a4-privacy.md                     # A4.1~A4.8 (Stage 11.B-4, SOC2 baseline만)
   └─ a5-security.md                    # A5.1~A5.2 (Stage 11.B-4, cross-reference)
```

관련 결선 자산 (별 트리):

- `internal/api/handlers/compliance_export.go` — audit log export wizard backend (Stage 11.B-5).
- `internal/api/handlers/compliance_effectiveness.go` — effectiveness dashboard backend (Stage 11.B-6).
- `internal/domain/compliance/soc2_mapping.go` — 14 카테고리 × 40 sub-control × audit action 매핑 (Stage 11.B-6).
- `web/src/routes/_authenticated/compliance.export.tsx` — export wizard UI (Stage 11.B-5).
- `web/src/routes/_authenticated/compliance.effectiveness.tsx` — effectiveness dashboard UI (Stage 11.B-6).
- `packs/soc2-controls/` — 자동 검증 pack 61 check + 61 selftest (Stage 11.B-7).

---

## §8 외부 트랙 ★

본 트리에 cover되지 않는 영역 — 사용자 외부 트랙(memory `feedback_user_tracks.md` 일관).

- **★ 실 SOC2 Type II 감사 firm 계약** — Deloitte · KPMG · PwC · BDO · A-LIGN · Coalfire 등 AICPA 회원 firm. 본 트리 마감 후 외부 firm 진입 baseline 완비, 사용자 외부 결정.
- **★ 90일 운영 효과성 측정** — SOC2 Type II 정의상 외부 firm이 측정. 본 트리는 자체 측정 도구(effectiveness dashboard + soc2-controls pack)만 cover.
- **★ Security awareness training 콘텐츠 (CC1.4)** — internal HR 트랙 또는 외부 트레이닝 벤더(KnowBe4/SANS 등).
- **★ Penetration testing (CC9.4)** — third-party 보안 firm(PortSwigger/Bishop Fox/Trail of Bits 등) 위탁.
- **★ Fraud risk assessment 라운드 (CC3.3)** — 외부 컨설팅 또는 internal 라운드 docs.
- **★ Business continuity plan 외부 검증 (CC9.1)** — 외부 firm 컨설팅.

---

## §9 참조

- 설계 doc: [`docs/design/notes/soc2-readiness-design.md`](../design/notes/soc2-readiness-design.md)
- AICPA Trust Services Criteria 2017 (with revised points of focus 2022)
- AICPA Description Criteria 2018 (Type II 90일 운영 측정 정의)
- Lodestar Phase 1~10 결선 자산 — 각 통제 docs §매핑 매트릭스 참조.

---

*Last updated: 2026-05-22 — Phase 11 옵션 B Stage 11.B-8 v0.12.0 release 마감 (14 매트릭스 docs · 47 sub-controls · auditor role · export wizard · effectiveness dashboard · soc2-controls pack 61 check 결선).*
