# Lodestar Compliance — SOC2 Type II Readiness Baseline

> **상태**: Phase 11 옵션 B Stage 11.B-2 진입 — `docs/compliance/` 신규 트리 + CC1~CC4 mapping 1차 round.
> **버전**: v0.12.0 진입 baseline (in progress).
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
| CC5 Control Activities | CC5.1~CC5.3 (3) | Stage 11.B-3 진입 예정 |
| CC6 Logical and Physical Access | CC6.1~CC6.8 (8) | Stage 11.B-3 진입 예정 |
| CC7 System Operations | CC7.1~CC7.5 (5) | Stage 11.B-3 진입 예정 |
| CC8 Change Management | CC8.1 (1) | Stage 11.B-3 진입 예정 |
| CC9 Risk Mitigation | CC9.1~CC9.2 (2) | Stage 11.B-3 진입 예정 |

### §2.2 Additional Criteria (D-P11B-2 결정: 전체 5)

| TSC | 명칭 | 본 epic cover | docs |
|---|---|---|---|
| A1 Availability | 가용성 — 운영 · 모니터링 · 인시던트 · 복구 | ✅ | Stage 11.B-4 진입 예정 |
| A2 Confidentiality | 기밀성 — 식별 · 저장 · 폐기 · 전송 보호 | ✅ | Stage 11.B-4 진입 예정 |
| A3 Processing Integrity | 처리 무결성 — 정합성 · 완전성 · 적시성 | ✅ | Stage 11.B-4 진입 예정 |
| A4 Privacy | 프라이버시 — 개인정보 | ✅ | Stage 11.B-4 진입 예정 |
| A5 Security | 보안 — 통제 자산 보호 | ✅ | Stage 11.B-4 진입 예정 |
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

## §4 외부 감사인 access — `auditor` role (신규, Stage 11.B-5 진입 예정)

본 트리는 외부 감사인이 자체적으로 접근할 read-only role 및 export bundle을 제공합니다.

- **신규 role**: `auditor` — read-only 매트릭스, write 차단.
- **export bundle**: `GET /api/v1/compliance/auditor-bundle?period=90d` — audit_entries + cosign signatures + chain_keys(epoch별 public key) + 통제별 evidence index + README.
- **bundle 형식 docs**: `docs/compliance/auditor-bundle-format.md` (Stage 11.B-5 진입 예정).
- **RBAC boundary test**: auditor write 차단, admin export 허용.

**현 round 상태**: `auditor` role 미존재. Stage 11.B-5 진입 시 신규 도입. 본 round docs는 매핑만 cover.

---

## §5 자동 검증 — `soc2-controls` benchmark pack (Stage 11.B-7 진입 예정)

본 트리는 docs only에 그치지 않고 **자동 검증 pack**으로 통제 충족을 측정합니다.

- **pack 경로**: `packs/soc2-controls/` (신규, Stage 11.B-7 진입 예정).
- **check 수**: ~50~80 yaml check, Lodestar 자체를 스캔하여 통제 충족 자동 평가.
- **예시 check**:
  - `CC6.6-key-rotation-quarterly.yaml` → audit chain key rotation 90일 이내 검증.
  - `CC5.2-rbac-least-privilege.yaml` → admin 권한 사용자 수 임계 검증.
  - `CC7.2-monitoring-anomaly.yaml` → multi-region 5 alert rule 활성 검증.
  - `A1.1-availability-multi-region.yaml` → Patroni primary + standby 활성 검증.
- **effectiveness dashboard 통합** (Stage 11.B-6): `/compliance` 페이지에서 통제별 status + 마지막 evidence + drill-down.

---

## §6 디렉터리 구조

```
docs/compliance/
├─ README.md                            # 본 파일 — 인덱스 + Lodestar 매핑 개요
├─ soc2/                                # SOC2 Trust Services Criteria 매핑
│  ├─ cc1-control-environment.md        # CC1.1~CC1.5 (본 round)
│  ├─ cc2-communication-information.md  # CC2.1~CC2.3 (본 round)
│  ├─ cc3-risk-assessment.md            # CC3.1~CC3.4 (본 round)
│  ├─ cc4-monitoring-activities.md      # CC4.1~CC4.2 (본 round)
│  ├─ cc5-control-activities.md         # Stage 11.B-3 진입 예정
│  ├─ cc6-logical-access.md             # Stage 11.B-3 진입 예정
│  ├─ cc7-system-operations.md          # Stage 11.B-3 진입 예정
│  ├─ cc8-change-management.md          # Stage 11.B-3 진입 예정
│  ├─ cc9-risk-mitigation.md            # Stage 11.B-3 진입 예정
│  ├─ a1-availability.md                # Stage 11.B-4 진입 예정
│  ├─ a2-confidentiality.md             # Stage 11.B-4 진입 예정
│  ├─ a3-processing-integrity.md        # Stage 11.B-4 진입 예정
│  ├─ a4-privacy.md                     # Stage 11.B-4 진입 예정
│  └─ a5-security.md                    # Stage 11.B-4 진입 예정
├─ auditor-bundle-format.md             # Stage 11.B-5 진입 예정
└─ audit-export-guide.md                # Stage 11.B-5 진입 예정
```

---

## §7 외부 트랙 ★

본 트리에 cover되지 않는 영역 — 사용자 외부 트랙(memory `feedback_user_tracks.md` 일관).

- **★ 실 SOC2 Type II 감사 firm 계약** — Deloitte · KPMG · PwC · BDO · A-LIGN · Coalfire 등 AICPA 회원 firm. 본 트리 마감 후 외부 firm 진입 baseline 완비, 사용자 외부 결정.
- **★ 90일 운영 효과성 측정** — SOC2 Type II 정의상 외부 firm이 측정. 본 트리는 자체 측정 도구(effectiveness dashboard + soc2-controls pack)만 cover.
- **★ Security awareness training 콘텐츠 (CC1.4)** — internal HR 트랙 또는 외부 트레이닝 벤더(KnowBe4/SANS 등).
- **★ Penetration testing (CC9.4)** — third-party 보안 firm(PortSwigger/Bishop Fox/Trail of Bits 등) 위탁.
- **★ Fraud risk assessment 라운드 (CC3.3)** — 외부 컨설팅 또는 internal 라운드 docs.
- **★ Business continuity plan 외부 검증 (CC9.1)** — 외부 firm 컨설팅.

---

## §8 참조

- 설계 doc: [`docs/design/notes/soc2-readiness-design.md`](../design/notes/soc2-readiness-design.md)
- AICPA Trust Services Criteria 2017 (with revised points of focus 2022)
- AICPA Description Criteria 2018 (Type II 90일 운영 측정 정의)
- Lodestar Phase 1~10 결선 자산 — 각 통제 docs §매핑 매트릭스 참조.

---

*Last updated: 2026-05-21 — Stage 11.B-2 CC1~CC4 mapping round.*
