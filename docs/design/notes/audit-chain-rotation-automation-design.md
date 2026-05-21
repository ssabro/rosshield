# Audit Chain Key Rotation 자동화 — Phase 10 옵션 D Design

> **상태**: Design (Stage 10.D-1) — 코드 0줄 / 마이그레이션 0건 / pack 변경 0.
> **작성일**: 2026-05-21
> **범위**: Phase 6 carryover로 이월된 audit chain **서명 키 rotation** 자동화. Phase 10 backlog §4.4(`phase10-backlog-design.md`)의 2순위 권장. 본 round는 design doc만, 코드 진입은 D-P10D-1~3 사용자 확정 후 별 PR(Stage 10.D-2~7).
> **참조**:
> - `docs/design/notes/audit-chain-rotation-design.md` (Phase 6 결선 design doc — **audit entry segment rotation**. 본 doc과 별 layer).
> - `docs/design/notes/phase10-backlog-design.md` §4.4 (옵션 D 권장 진입 — 본 doc의 직접 부모).
> - `docs/design/notes/auto-failover-research.md` · `multi-region-ha-design.md` (직전 design doc 패턴).
> - 코드: `internal/domain/audit/` · `internal/platform/signer/` · `internal/platform/keystore/` · `cmd/rosshield-audit-verify/`.
> - 마이그레이션: `internal/platform/storage/postgres/migrations/0002_audit.up.sql`(audit_checkpoints.signer_key_id) · 0032/0035/0036(entry segment rotation 결선).
> **비목표** (§10에서 명시):
> - TPM-bound rotation (옵션 D는 별 epic — TPM hardware 의존).
> - multi-tenant 환경에서 tenant별 rotation 분리 — 현 단일 system tenant 전제.
> - 외부 KMS(AWS KMS · GCP KMS) 통합 — customer 환경 의존.
> - audit entry **segment** rotation 자체 변경 — 별 결선 (E32 0032~0036).

---

## 1. 상태 / 배경

### 1.1 Phase 6 carryover 사실

Phase 6 시점에서 작성된 design doc `audit-chain-rotation-design.md`(2026-05-19, ~458줄)는 **audit entry segment rotation**(hot DB → cold archive 분할)에 한해 결선되었습니다. 동 doc은 12 결정 항목(D-AR-1~10) + 6 Stage 분해 + 옵션 A(시간 기반 checkpoint 분할 + S3/NFS) 권장 default를 채택했습니다. 실 구현은 다음과 같이 결선되어 있습니다:

- `internal/domain/audit/rotation/` — archiver · backend(file + s3 + s3-enterprise) · cosign · gc · policy · builder + 16 file + 단위/통합 테스트.
- migrations 0032 `audit_rotation_segments` · 0035 `prev_segment_hash` 컬럼 · 0036 `audit_gc_marker` + 0034 `audit_gc_guc` 결선.
- `cmd/rosshield-audit-verify rotation` 서브커맨드 — single archive verify + chain mode batch verify (segment 간 prev_segment_hash 자동 forward).

본 carryover에서 **미진행 사실**(`phase10-backlog-design.md` §4.4 인용):
> 본 doc은 결선이나 **자동 rotation 코드 미진행**. opt B(정기 + 운영자 승인 + audit emit) — scheduler + admin UI 승인 + `audit_chain.key_rotated` event + `fg-verify` rotation aware + 마이그레이션 1건(audit_chain_keys epoch별 public key 보존).

본 round의 대상은 audit entry segment rotation이 아닌 **서명 키(Ed25519 signer key) rotation**입니다 — 명칭 충돌은 doc 인용 패턴 그대로 유지하나 §2의 fact-check에서 두 layer 분리를 분명히 합니다.

### 1.2 본 round 진입 가치

- **compliance baseline**: ISMS-P SC-12(암호 키 관리) · NIST 800-53 SC-12(Cryptographic Key Establishment and Management) 통제 명시 요구 — 정기 rotation + 키 폐기 + history 보존.
- **enterprise 영업 자산**: SOC2 CC6.6(암호 키 정기 rotation) · 첫 paying customer 진입 *전* baseline 강화.
- **외부 감사 호환**: 외부 감사인이 epoch 1년 후의 audit checkpoint 검증 시 epoch별 public key 부재면 검증 불가 → audit chain의 **검증 가능성 손상** 위험.
- **Phase 6 carryover 자연 마감**: Phase 10 backlog §4.4에서 2순위 권장 — 옵션 A(multi-region UI) 마감 후 자연 진입.

### 1.3 본 round 범위·비범위

- **범위**: signer key rotation 자동화 — scheduler + 운영자 승인 + signer hot-swap + `audit_chain_keys` 신규 테이블(epoch별 public key 보존) + audit emit + `fg-verify` rotation aware.
- **비범위**: §10 명시 — TPM-bound auto-rotation(별 epic) · tenant별 rotation 분리 · 외부 KMS 통합 · entry segment rotation 변경.

---

## 2. 현재 상태 fact-check (코드 직접 grep)

본 §은 추측 0, fact만 명시. 영역별 grep/Read 결과를 표 또는 bullet로 정리합니다.

### 2.1 `internal/domain/audit/` (audit Service + checkpoint)

`audit.go` + `checkpoint.go` + `export.go` + `hash.go` + `sqliterepo/` 결선:

| 파일·라인 | fact |
|---|---|
| `audit.go:125` | `Entry.SignerKeyID string` — entry 단위 서명 key id 보존 (활성 키만). |
| `checkpoint.go:21~30` | `SerializeCheckpointPayload(tenantID, seq, hash)` — Ed25519 서명 input은 `hash[32] ‖ uint64BE(seq) ‖ utf8(tenantId)`. **epoch 없음**. |
| `checkpoint.go:48` | `RegisterCheckpointJob(sch, store, svc, logger, jobID, spec, tenantID, sgn)` — scheduler에 정기 checkpoint job 등록. **rotation 호출 지점 부재**. |
| `checkpoint.go:73` | log에 `"keyId", cp.SignerKeyID` 노출 — key id가 audit checkpoint에 함께 기록됨. |
| `sqliterepo/repo.go:316,376,381~384` | `INSERT INTO audit_checkpoints (..., signer_key_id, signature)` — 매 checkpoint에 사용된 key id 컬럼 보존. |
| `export.go:50` | export bundle JSON에 `_keyId` 필드 노출 — 외부 검증 도구가 받음. |

### 2.2 `internal/platform/signer/` (Ed25519 signer)

| 파일·라인 | fact |
|---|---|
| `signer.go:13~27` | `Signer` interface — `Sign(payload)→(sig, keyID, err)` · `Verify(payload, sig)→err` · `PublicKey()→[]byte` · `KeyID()→string`. **단일 활성 key만**. |
| `soft/signer.go:25~29` | `softSigner{private, public, keyID}` — 단일 키 wrap. **history 보존 없음**. |
| `soft/signer.go:115~118` | `WrapPrivateKey(priv)` — keystore가 unseal한 raw key를 wrap. |
| `soft/signer.go:122~125` | `makeKeyID(pub)` = `"key_" + hex(sha256(pub)[:8])` — 키마다 안정적 식별자, **다른 키는 다른 keyID**. |

핵심: 현재 signer는 **단일 활성 key**. 키 교체 시 새 KeyID가 생기지만, **이전 key 객체·public key는 보존되지 않습니다**.

### 2.3 `internal/platform/keystore/` (E34 KeyStore)

| 파일·라인 | fact |
|---|---|
| `keystore.go:25~34` | `KeyStore` interface — `LoadOrCreatePrivateKey(handle)→ed25519.PrivateKey` · `Close()`. |
| `file/store.go` | disk path 기반 — 0700 dir + 0600 file. |
| `tpm/store_linux.go` + `store_other.go` | TPM 2.0 PCR-sealed (E34 Stage 1 결선) + non-Linux no-op fallback. |

핵심: keystore는 **단일 handle = 단일 key**. rotation 시 새 handle 또는 새 path 필요 + 이전 handle은 keystore 외부에 archive (file backup or DB record).

### 2.4 `cmd/rosshield-audit-verify/` (fg-verify CLI)

| 파일·라인 | fact |
|---|---|
| `main.go:66~70` | args[0]가 `rotation`이면 rotation 서브커맨드 — **entry segment** rotation 검증. |
| `rotation.go:1~33` | `rosshield-audit-verify rotation` + `rotation chain` — segment archive verify. **signer key rotation aware는 부재**. |

핵심: fg-verify는 segment rotation(0032~0036)에 한해 cold archive를 verify하나, **signer key epoch 전환은 인식하지 못함**. checkpoint signature 검증은 단일 public key 가정.

### 2.5 마이그레이션 디렉터리

| fact | 값 |
|---|---|
| 마지막 마이그레이션 번호 | **0036** (`audit_gc_marker`). |
| audit_chain_keys 테이블 존재 여부 | **부재**. grep 결과 0. |
| audit_checkpoints.signer_key_id 컬럼 | **존재**(`0002_audit.up.sql:48`) — 매 checkpoint가 사용한 key id 보존. |
| pack_manifest.signer_key_id 컬럼 | **존재**(`0007_packs.up.sql:19`) — pack 서명 key id 보존(별 도메인). |

본 round 신규 마이그레이션 번호: **0037** (audit_chain_keys 테이블).

### 2.6 scheduler · admin UI · audit event emit

| 영역 | fact |
|---|---|
| `internal/platform/scheduler/` | cronsched 결선 — `audit.RegisterCheckpointJob` 패턴 활용 가능. |
| admin UI rotation 페이지 | **부재**. `/regions` 등 page와 동등 패턴으로 신규. |
| audit event `audit.chain.key_rotated` | **부재**. domain audit Service.Append에 새 action 추가 필요. |
| `audit.chain.key_rotation_proposed` event | **부재** — 본 doc에서 신규. |

---

## 3. 위협 모델 / 요구사항

### 3.1 신규 위협

| 위협 | 발생 | 영향 | 본 epic 대응 |
|---|---|---|---|
| 서명 키 장기 노출(누출·암호분석) | 가능성 낮으나 enterprise compliance 명시 요구 | 외부 검증 무효화 | 정기 rotation(D-P10D-2) |
| rotation 실패 시 audit emit 차단 | 실 운영 시 가능 | 모든 WRITE 차단 → 데이터 손실 | fail-safe(검증 후 swap, in-flight 처리 D-P10D-3) |
| 외부 감사인이 epoch=1 public key 부재 시 과거 checkpoint 검증 불가 | 1년+ 운영 후 상시 | audit chain 외부 검증 무효 | audit_chain_keys epoch별 public key append-only 보존 |
| multi-region 환경에서 rotation race | Phase 9 Patroni + cross-region 시점 | epoch 충돌 + chain head 불일치 | Patroni leader가 단일 trigger(R5) |
| 운영자 부재 시 rotation 실패 | 분기 1~3회 | rotation 미수행 → compliance 통제 위반 | Prometheus alert + check-health hook + ops runbook |
| rotation entry 자체가 chain link 깨짐 | 설계 결함 시 | 외부 검증 fail | rotation은 정상 `audit.chain.key_rotated` entry로 chain link 유지(원래 keyID 서명 + 새 epoch metadata) |

### 3.2 요구사항

| ID | 요구 | 측정 |
|---|---|---|
| R1 | rotation은 audit chain 정상 link(`audit.chain.key_rotated` entry로 emit) | sqliterepo 통합 테스트 PASS |
| R2 | epoch별 public key 영구 보존(append-only) | audit_chain_keys.revoked_at NULL 또는 timestamp 보존, 행 삭제 0 |
| R3 | fg-verify가 epoch별 public key를 자동 선택해 과거 checkpoint signature 검증 가능 | bundle 내 epoch metadata + audit_chain_keys 조회 |
| R4 | rotation은 운영자 승인 후에만 실행(fail-safe) | scheduler가 후보 알람 emit만, swap은 admin approve API 호출 시 |
| R5 | multi-region 환경에서 단일 trigger | `ha.Manager.IsLeader()` gate 추가 |
| R6 | rotation 실패 시 audit emit 차단 방지 | 새 key가 검증 통과(self-sign + verify)된 후에만 swap |
| R7 | air-gap customer는 자동 rotation 비활성 옵션 유지 | feature flag `audit_chain_rotation_enabled` default false 또는 manual mode 옵션 |
| R8 | rotation 후 첫 checkpoint signature까지 ≤ 5초 | scheduler가 swap 직후 즉시 WriteCheckpoint 호출 |

---

## 4. 옵션 비교 (4 옵션)

### 4.1 옵션 A — 수동 only (현재 상태 유지)

**설계 요약**: 운영자가 명시 CLI 또는 admin UI 버튼으로 rotation 1회 trigger. scheduler 없음. epoch metadata는 기록되나 자동 trigger 0.

**가치**: ★ — compliance 통제는 운영자 절차로 cover, 자동화 부재.

**노력 추정**: 1~1.5주 (수동 CLI + 기본 audit_chain_keys 테이블 + fg-verify rotation aware).

**전제·의존**: 없음.

**리스크**: **낮음**. 회귀 표면 작음. 단, ISMS-P/SOC2 정기 rotation 통제는 운영자 수동 절차에 의존 — 인증 시 effectiveness 측정 추가 필요.

### 4.2 옵션 B — 정기 + 운영자 승인 + audit emit (권장, Phase 10 backlog §4.4)

**설계 요약**:

1. scheduler가 D-P10D-2 frequency(default quarterly)마다 rotation **후보 알람** emit — `audit.chain.key_rotation_proposed` event + Prometheus metric `rosshield_audit_rotation_proposed_total{tenant,epoch}` increment.
2. 운영자가 admin UI `/audit/chain/rotations` 페이지에서 후보 확인 + **승인 버튼** 클릭(또는 거부).
3. 승인 시 POST `/api/v1/audit/chain/rotations/approve` — handler가 ha.Manager leader 확인 → 새 Ed25519 key 생성 + keystore에 새 handle로 영속 + verify round-trip(self-sign + verify) → signer hot-swap → 즉시 `WriteCheckpoint` 호출(R8) → `audit.chain.key_rotated` event emit + `audit_chain_keys` 테이블에 새 epoch row append.
4. fg-verify가 audit_chain_keys epoch별 public key 사용 — bundle 내 epoch metadata 또는 audit_checkpoints.signer_key_id 매칭으로 자동 선택.

**가치**: ★★★★ — compliance 통제 + 운영자 통제(false rotation 방어) + audit emit fail-safe.

**노력 추정 (보수적)**: **3~4주**. Stage 분해 §6 참조.

**전제·의존**: Phase 9 ha.Manager 결선(IsLeader 활용) + scheduler 결선 + admin UI 패턴(`/regions` 등) + keystore handle 추가 가능.

**리스크**: **중**. signer hot-swap 시 in-flight audit emit 처리(D-P10D-3) 결정 필요 — block · retry · queue 중 fail-safe 선택. fg-verify SDK 호환성은 epoch=1 default로 v0.9.0 이하 bundle도 정상 검증.

### 4.3 옵션 C — fully automatic + override 가능

**설계 요약**: scheduler가 승인 없이 자동 rotation. 운영자는 emergency override(rotation 일시 정지 또는 즉시 reverse)만. 운영 단순하나 false positive(잘못된 rotation 시점) 시 외부 감사인 호환성 영향.

**가치**: ★★★ — 자동화 강하나 운영자 통제 약함.

**노력 추정**: 2~3주 (옵션 B의 admin UI 승인 layer 제거 + override API만).

**전제·의존**: ha.Manager + scheduler + keystore.

**리스크**: **높음**. 운영자 부재 시 잘못된 rotation 자동 진입 위험. compliance 감사인이 "auto without approval" 패턴 거부 사례 있음 — SOC2 CC6.6은 통제(=사람)의 명시적 책임 요구. 외부 감사 호환성 영향.

### 4.4 옵션 D — TPM-bound rotation

**설계 요약**: TPM 봉인 key를 PCR change 또는 epoch trigger로 자동 unseal + 새 key 생성. E34 Stage 2+ 영역 — TPM hardware 필수.

**가치**: ★★★★ — 봉인 + 자동화 강력. 단 hardware 의존.

**노력 추정**: 6~8주 (TPM 봉인 PCR policy + tpm2-tools 통합 + simulator 테스트).

**전제·의존**: ★ E36 reference HW + TPM 2.0. air-gap customer 일부만 cover.

**리스크**: **높음**. TPM hardware customer 환경 의존 + audit chain의 software-only 일관성 깨짐. 본 epic 비범위(§10.1 비목표 명시).

### 4.5 옵션 비교 매트릭스

| 옵션 | 가치 | 노력 | 리스크 | 외부 트랙 의존 | 즉시 진입 |
|---|---|---|---|---|---|
| **A** 수동 only | ★ | 1~1.5주 | 낮음 | 0 | ✅ |
| **B** 정기 + 승인 + audit emit | ★★★★ | 3~4주 | 중 | 0 | ✅(권장) |
| **C** fully automatic + override | ★★★ | 2~3주 | 높음 | 0 | ⚠️(감사 호환성 risk) |
| **D** TPM-bound | ★★★★ | 6~8주 | 높음 | ★ E36 HW | ❌(비목표) |

---

## 5. Top 1 권장 + 근거

**옵션 B (정기 + 운영자 승인 + audit emit)** — Phase 10 backlog §4.4 권장 default와 일치.

### 5.1 근거

1. **운영자 통제 보존** — 보안 + 사람 판단 우선. SOC2 CC6.6 통제(명시적 책임자) + ISMS-P SC-12 호환.
2. **audit emit fail-safe 단순** — 승인 단계가 검증 자동화의 자연 gate. 잘못된 rotation 시점은 운영자가 거부.
3. **D-P10D-2 결정 항목으로 frequency 조정 가능** — quarterly default + customer 요구 시 monthly 또는 운영자 trigger 시만 옵션.
4. **외부 트랙 의존 0** — 기존 ha.Manager + scheduler + keystore + admin UI 패턴 결선 baseline 활용.
5. **fg-verify SDK 호환** — epoch=1 default로 v0.9.0 이하 bundle 정상 검증 + 신규 bundle은 epoch metadata 추가.
6. **회귀 위험 중** — signer hot-swap이 가장 critical. 검증 round-trip(R6) + advisory lock + 단위 테스트로 cover.

### 5.2 보류 옵션 사유

- **옵션 A**(수동 only): compliance effectiveness 측정 부담 + 자동화 가치 0.
- **옵션 C**(fully automatic): 외부 감사 호환성 risk + SOC2 CC6.6 통제 책임 요구 위반 가능.
- **옵션 D**(TPM-bound): §10 비목표 명시 + ★ E36 hardware 외부 트랙 의존.

---

## 6. Stage 분해 (옵션 B 채택 가정)

memory `feedback_design_doc_conservative.md` 일관 — 보수적 추정.

### 6.1 Stage 10.D-1 — 본 design doc (마감 단계)

본 round (docs only, 코드 0). D-P10D-1~3 결정 + 사용자 합의.

### 6.2 Stage 10.D-2 — 마이그레이션 + audit_chain_keys 테이블

추정 **1주**.

- `migrations/0037_audit_chain_keys.up.sql` + `.down.sql`:
  - 컬럼: `epoch BIGINT PK` · `key_id TEXT NOT NULL UNIQUE` · `public_key_hex TEXT NOT NULL` · `keystore_handle TEXT NOT NULL` · `created_at TEXT NOT NULL` · `revoked_at TEXT` · `created_by TEXT NOT NULL` · `audit_entry_seq BIGINT REFERENCES audit_entries(seq)`.
  - 단일 system tenant 가정 — tenant_id 컬럼은 향후 D-P10D 확장 옵션으로 명시(현 round 부재 + R-D8 정책).
  - 불변성: UPDATE/DELETE 차단 트리거(P9 일관) + revoked_at만 update 허용(별 트리거).
- `internal/domain/audit/chain_keys.go` — `ChainKey{Epoch, KeyID, PublicKey, KeystoreHandle, CreatedAt, RevokedAt}` 도메인 type + repository interface.
- sqliterepo + postgres 양 driver 어댑터 + 단위 test ~80줄.

### 6.3 Stage 10.D-3 — scheduler + rotation 후보 알람 + admin approve API

추정 **1주**.

- `internal/platform/scheduler/audit_chain_rotation_job.go` — Patroni leader 확인 + D-P10D-2 spec cron + rotation 후보 평가(마지막 epoch created_at + frequency >= now면 propose).
- Prometheus metric `rosshield_audit_rotation_proposed_total{epoch}` increment.
- 신규 audit action `audit.chain.key_rotation_proposed` event emit — domain.audit.Service.Append.
- API handler: GET `/api/v1/audit/chain/rotations` (현재 epoch + 후보 status) · POST `/api/v1/audit/chain/rotations/approve` (운영자 승인) · POST `/api/v1/audit/chain/rotations/reject` (거부).
- 권한: `auditor` role 이상(또는 신규 `audit_rotation` 권한 — §8 결정).

### 6.4 Stage 10.D-4 — signer hot-swap + audit.chain.key_rotated event emit + 단위 test

추정 **1주**.

- `internal/platform/signer/swap.go` — `SwappableSigner` wrapper:
  - 내부 `atomic.Value[Signer]` + RWMutex.
  - `SwapTo(newSigner) error` — verify round-trip(new sign + new verify) PASS 후 swap.
  - D-P10D-3 in-flight 처리: **queue** 권장 default (sign 호출 시 RWMutex.RLock → swap 시 Lock으로 동기화).
- approve handler:
  1. ha.Manager.IsLeader() 확인.
  2. 새 ed25519 key 생성 + keystore에 새 handle로 영속(`audit-chain-{epoch}`).
  3. verify round-trip self-test.
  4. SwappableSigner.SwapTo(new) — in-flight audit emit은 queue로 흡수.
  5. `audit_chain_keys` row append (epoch++, revoked_at NULL).
  6. 이전 epoch의 revoked_at = now() (별 UPDATE, trigger 허용).
  7. `audit.chain.key_rotated` event emit + Prometheus metric `rosshield_audit_rotation_total{result=success}` increment.
  8. 즉시 `WriteCheckpoint` 호출 (R8).
- 단위 test ~150줄 (테이블 + signer swap + audit emit + verify round-trip).

### 6.5 Stage 10.D-5 — fg-verify rotation aware + epoch별 검증 + 단위 test

추정 **0.5주**.

- `cmd/rosshield-audit-verify/main.go` — bundle 안 `_keyId` 또는 `_epoch` metadata 파싱.
- audit_chain_keys 조회 옵션:
  - (a) **embedded bundle**: export bundle에 audit_chain_keys epoch별 public key 동시 포함(현 권장 default — fg-verify standalone 검증 보장).
  - (b) **trusted-keys dir**: `--trusted-keys ./keys/` 디렉터리에서 `epoch_<N>_pub.hex` 파일 로드.
- checkpoint verify 시 signer_key_id로 epoch lookup → 해당 public key로 Ed25519.Verify.
- 호환성: v0.9.0 이하 bundle은 `_epoch` 부재 → epoch=1 default 처리.
- 단위 test ~80줄 (multi-epoch bundle + verify PASS + tampered epoch FAIL).

### 6.6 Stage 10.D-6 — admin UI rotation 승인 페이지 + i18n + 단위 test

추정 **0.5주**.

- web `/audit/chain/rotations` route + RBAC gate(`auditor` 이상).
- React Query hook `useChainRotations` — GET `/api/v1/audit/chain/rotations` 60s polling.
- ChainRotationCard 컴포넌트 — 현재 epoch + 후보 상태(proposed/approved/none) + approve/reject 버튼.
- i18n 키 ~12건 ko+en.
- 단위 test(vitest) + Playwright e2e 1 scenario.

### 6.7 Stage 10.D-7 — testcontainers integration + ops docs + release notes + v0.10.0 minor

추정 **0.5주**.

- testcontainers integration: PG + rosshield-server boot + scheduler trigger → propose → API approve → audit_chain_keys epoch=2 row 확인 + fg-verify bundle 검증 PASS.
- `docs/operations/audit-chain-key-rotation.md` 신규 — 운영자 가이드(approve 절차 + rollback + air-gap customer).
- v0.10.0 minor — Phase 10 옵션 D 마감 첫 minor.
- release notes + CHANGELOG entry.

### 6.8 Stage 10.D-2~7 합계 추정

**3~4주** (보수적). Phase 10 backlog §4.4 추정 2~4주와 정합.

---

## 7. 결정 항목

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시.

### 7.1 D-P10D-1 — 옵션 채택

- (A) 수동 only — 자동화 0.
- **(B)** 정기 + 운영자 승인 + audit emit (**권장 default**) — Phase 10 backlog §4.4 일관.
- (C) fully automatic + override — 외부 감사 호환성 risk.
- (D) TPM-bound — §10 비목표.

**근거**: 운영자 통제 + audit emit fail-safe + 외부 트랙 의존 0 + ROI 가장 빠름.

### 7.2 D-P10D-2 — rotation frequency

- (1) **quarterly** (3개월) (**권장 default**) — ISMS-P/SOC2 baseline + 운영자 부담 작음.
- (2) monthly (1개월) — compliance 강도 강화, 운영자 부담 증가.
- (3) 운영자 trigger 시만 — 자동 propose 없음, scheduler 비활성. 옵션 A에 근접.
- (4) yearly (1년) — 통제 빈도 약함, 일부 통제 기준 미달.

**근거**: SOC2 CC6.6 + NIST 800-53 SC-12 baseline은 quarterly~yearly 범위 — quarterly가 enterprise 일반 baseline. customer 요구 시 monthly로 조정 가능.

### 7.3 D-P10D-3 — signer hot-swap 시 in-flight audit emit 처리

- (1) **block** — swap 시점 in-flight emit 일시 block, swap 후 자동 재개 (수 ms ~ 수십 ms latency). 단순.
- (2) retry — sign 시 swap 진행 중이면 에러 반환 + caller retry. caller 부담.
- (3) **queue** (**권장 default**) — `SwappableSigner` wrapper가 RWMutex로 동기화 + sign 호출 시 RLock + swap 시 Lock. in-flight는 자연 직렬화. transparent. latency 영향 ≤ 1ms.

**근거**: queue는 latency 영향 가장 작고 caller 변경 0. block은 미세 latency spike 발생 + 일부 미세 race 가능. retry는 caller 부담.

---

## 8. 마이그레이션·호환성 영향

### 8.1 신규 마이그레이션

- **0037_audit_chain_keys** (up/down).
- 컬럼: epoch · key_id · public_key_hex · keystore_handle · created_at · revoked_at · created_by · audit_entry_seq.
- 불변성 트리거: append-only + revoked_at만 update 허용.
- 초기 row: 마이그레이션 적용 시 현재 활성 signer의 KeyID + PublicKey + KeystoreHandle을 **epoch=1**로 자동 insert (`INSERT ... SELECT ...` migration 본문).

### 8.2 fg-verify SDK 호환성

- v0.9.0 이하 export bundle은 `_epoch` 필드 부재 → fg-verify가 **epoch=1 default**로 처리.
- v0.10.0 이상 export bundle은 `_epoch` 필드 포함 + (선택) audit_chain_keys epoch별 public key embedded.
- 단위 test: v0.9.0 fixture bundle + v0.10.0 fixture bundle 둘 다 PASS.

### 8.3 audit chain head sha 영향

- rotation 시점 `audit.chain.key_rotated` entry가 chain link로 들어감 — chain hash 정상 forward.
- 이전 epoch에서 작성된 entry는 이전 keyID로 서명, 새 epoch entry는 새 keyID로 서명 — audit_entries.signer_key_id 컬럼 기존 보존(2.1 fact).
- 외부 감사인 검증: rotation entry 자체는 이전 keyID로 서명(rotation **요청**은 이전 epoch 동안) + 다음 checkpoint부터 새 keyID 서명. fg-verify가 entry별 signer_key_id로 정확 매칭.

### 8.4 multi-region/Patroni 영향

- ha.Manager.IsLeader() gate(R5) — Patroni leader region만 trigger.
- standby region은 logical replication으로 audit_chain_keys row receive — replication lag 후 fg-verify aware 가능.
- 단위 test: 2-region cutover 후 rotation 후보 standby에서 trigger 안 함 확인.

### 8.5 air-gap customer

- feature flag `audit_chain_rotation_enabled` config (default true / air-gap profile default **false** — R7 일관).
- 비활성 시 scheduler 미동작 + admin UI 페이지는 "manual mode" 메시지.
- 수동 rotation CLI는 옵션 A 동등 — 별 epic으로 분리 가능 (현 round 비범위).

---

## 9. 리스크

| # | 리스크 | 영향 | 완화 |
|---|---|---|---|
| R1 | rotation 실패 시 audit emit 차단 | 모든 WRITE 차단 → 데이터 손실 | 새 key verify round-trip(R6) → 검증 통과 후에만 swap + Prometheus alert `rosshield_audit_rotation_failures_total` |
| R2 | multi-region rotation race | epoch 충돌 + chain head 불일치 | ha.Manager.IsLeader() gate(R5) + advisory lock + audit_chain_keys.epoch UNIQUE |
| R3 | old key 영구 보존 누락 | 외부 감사인이 과거 checkpoint 검증 불가 | audit_chain_keys append-only + revoked_at만 update + 단위 test |
| R4 | 운영자 부재 시 rotation 미수행 | compliance 통제 위반 | Prometheus alert `rosshield_audit_rotation_proposed{age > 7d}` + check-health hook + ops runbook |
| R5 | fg-verify v0.9.0 호환성 깨짐 | 기존 customer export bundle 검증 fail | epoch=1 default + 단위 test 양쪽 fixture |
| R6 | air-gap customer scheduler 자동 진입 | unauthorized rotation | feature flag default false on air-gap profile + ops docs |
| R7 | keystore handle 충돌(같은 path 재사용) | 새 key가 이전 file 덮어쓰기 | keystore handle 명명 규칙 `audit-chain-{epoch}` + 파일 존재 확인 + ErrAlreadyExists |
| R8 | rotation entry chain link 깨짐 | 외부 검증 fail | rotation entry는 이전 keyID로 서명 + 정상 chain link + 단위 test |
| R9 | signer hot-swap 동시성 race | 일부 emit이 새 key + 일부가 이전 key로 서명 | SwappableSigner RWMutex (D-P10D-3 queue) + 단위 test |
| R10 | audit_chain_keys 비대 | 거의 0 (quarterly × 100년 = 400 row) | 우려 없음 |

---

## 10. 비목표

본 epic 명시 제외:

### 10.1 TPM-bound rotation (옵션 D)

별 epic — TPM hardware 의존 + E34 Stage 2+ 영역. ★ E36 reference HW 외부 트랙. customer hardware 진입 후 재평가.

### 10.2 multi-tenant 환경에서 tenant별 rotation 분리

현 단일 system tenant 전제. tenant별 rotation은 audit_chain_keys.tenant_id 컬럼 추가 + tenant scope rotation Service 분리 필요 — 별 epic.

### 10.3 외부 KMS(AWS KMS · GCP KMS) 통합

customer 환경 의존(★). AWS KMS는 sign API 호출이 audit emit hot path latency 영향. on-prem customer 비호환. 별 epic.

### 10.4 audit entry segment rotation 변경

E32(0032~0036)로 결선. 본 epic은 **signer key rotation**만 — entry segment rotation은 변경 0.

### 10.5 자동 key revocation broadcast

revoked_at 보존만 + 외부 client에게 즉시 broadcast 없음. fg-verify는 audit_chain_keys 조회로 자연 인식. 별 broadcast 채널 없음.

### 10.6 hardware-bound + multi-region 동기 rotation

Patroni leader 단일 trigger로 충분. multi-region active-active 자동 rotation 동기화는 옵션 D + 별 epic.

---

## 11. 참조

### 11.1 직전 design doc 4건 (Phase 6 결선 영역)

- `docs/design/notes/audit-chain-rotation-design.md` — Phase 6 결선 design doc(audit **entry segment** rotation; 본 doc과 별 layer). 12 결정 항목 D-AR-1~10 + 6 Stage 분해.
- `audit-rotation-cosign.md` · `audit-rotation-s3.md` · `audit-rotation-verify.md` — Phase 10 backlog §4.4 인용 명칭. 본 리포 내 별 file 부재 — entry segment rotation 영역은 위 결선 doc + 0032/0035/0036 + `internal/domain/audit/rotation/`로 결선되어 있음(2.5 fact).

### 11.2 Phase backlog

- `phase6-backlog-design.md` — Phase 6 carryover 기록(이월 시점).
- `phase10-backlog-design.md` §4.4 — 본 doc의 직접 부모(옵션 D 권장 default).

### 11.3 코드 영역

- `internal/domain/audit/` — Service.Append + WriteCheckpoint + checkpoint.go (RegisterCheckpointJob 패턴).
- `internal/platform/signer/` — Ed25519 signer interface + soft adapter + KeyID 규칙.
- `internal/platform/keystore/` — file + tpm 어댑터 (E34 Stage 1).
- `cmd/rosshield-audit-verify/` — fg-verify CLI(rotation 서브커맨드 결선; key rotation aware는 본 round Stage 10.D-5).
- `internal/platform/storage/postgres/migrations/0002_audit.up.sql` — audit_checkpoints.signer_key_id 컬럼.
- `internal/platform/scheduler/` — cronsched.
- `internal/platform/ha/` — Manager.IsLeader() gate.

### 11.4 compliance 기준

- ISMS-P SC-12 — 암호 키 관리(생성·배포·저장·갱신·폐기).
- NIST 800-53 SC-12 — Cryptographic Key Establishment and Management.
- SOC2 CC6.6 — 암호 키 정기 rotation + 명시적 책임자 통제.

### 11.5 memory feedback

- `feedback_design_doc_first.md` — 1일+ 임계 design doc 우선.
- `feedback_design_doc_conservative.md` — 잠재 효과/시간 보수적.
- `feedback_user_tracks.md` — D1·E36·SOC2 감사·customer trigger 외부 트랙 제외(★ 표기).
- `feedback_parallel_agents.md` — 매 stage 시작 시 병렬 가능성 재평가.
- `feedback_skip_handoff.md` — handoff edit/commit/push 생략, CHANGELOG + release notes + commit 메시지로 trace.

---

**문서 끝**. 본 round 마감 — D-P10D-1·2·3 사용자 확정 후 Stage 10.D-2 진입.
