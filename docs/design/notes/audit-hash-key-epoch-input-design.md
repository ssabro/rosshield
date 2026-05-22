# Audit hash chain key_epoch input + fg-verify v3 — Phase 11 옵션 C Design (Stage 11.C-1)

> **상태**: Design (Stage 11.C-1) — 코드 0줄 / 마이그레이션 0건 / pack 변경 0. 본 round 는 design doc 만, 코드 진입은 D-P11C-1~4 사용자 확정 후 별 PR(Stage 11.C-2~6).
> **작성일**: 2026-05-22
> **범위**: Phase 11 옵션 C 진입 첫 stage. v0.10.0 carryover 마감 — audit hash chain `canonicalMetaJSON` input 에 `key_epoch` + `leader_epoch` 미포함이라는 사실을 마감하고 `fg-verify` v3 도입(v1/v2/v3 3-tier backward compat). 본 doc 자체는 Stage 분해 + D-P11C 결정 항목까지만 마감.
> **참조**:
> - `docs/design/notes/phase11-backlog-design.md` §4.3 + §12.1 — 본 doc 직접 부모, D-P11-1 = Top 3 순차 B → C → A 확정. 본 doc 은 2순위 옵션 C 본체.
> - `docs/design/notes/audit-chain-rotation-automation-design.md` — Phase 10 옵션 D 본체. fact-check 패턴 + Stage 분해 패턴 직접 모방 대상.
> - `docs/design/notes/soc2-readiness-design.md` — Phase 11 옵션 B 본체(직전 stage). 패턴 모방.
> - 코드: `internal/domain/audit/hash.go`(canonicalMetaJSON) · `internal/domain/audit/audit.go`(Entry struct) · `internal/domain/audit/export.go`(v1/v2 bundle) · `internal/domain/audit/sqliterepo/repo.go`(Append/Export/ExportV2) · `internal/domain/audit/keyrotation/rotator.go`(Phase 10.D) · `cmd/rosshield-audit-verify/export_verify.go`(fg-verify v1+v2).
> - 마이그레이션: 0037 `audit_chain_keys` · 0038 (Phase 10.D 결선 — epoch 별 public key 보존). 본 epic 은 마이그레이션 신규 0.
> **R 식별자**: R-PHASE11-C(본 stage 전체) — 결정 항목은 D-P11C-1~4.
> **본 문서 작성 위치**: main(head `85b6bb1` 직후), 단독 sub-agent.
> **비목표** (§10 에서 명시):
> - audit chain 일괄 re-compute(기존 entry hash 변경) — append-only 원칙(설계서 §1.9) 위반. 신규 entry 부터만 v3 hash.
> - v0.9.0 이하 customer 강제 upgrade — backward compat 유지(v1/v2/v3 자동 감지).
> - multi-tenant 분리 hash chain — 별 epic(Phase 10.D 비목표 일관).
> - 외부 KMS 통합 / TPM-bound hash chain — 별 epic.
> - audit entry segment rotation(0032~0036) 변경 — 별 layer 결선.

---

## 1. 상태 / 배경

### 1.1 Phase 11.B 마감 + Phase 11.C 진입 사실

`docs/design/notes/phase11-backlog-design.md` §12.1 확정(2026-05-21):

| 진입 순서 | 옵션 | 추정 | minor release |
|---|---|---|---|
| 1순위 (Phase 11.B) | SOC2 Type II readiness | ~6~9주 | v0.12.0 |
| **2순위 (본 stage)** | **C audit hash chain key_epoch input + fg-verify v3** | **~2~3주** | **v0.13.0** |
| 3순위 (Phase 11.A) | OpenTelemetry tracing 전면 | ~5~7주 | v0.14.0 |

Phase 11.B(SOC2 readiness) 마감 후 자연 진입. 옵션 C 는 v0.10.0(Phase 10.D) carryover 명시 — `CHANGELOG.md [Unreleased]` entry "audit hash chain key_epoch+leader_epoch input 포함" 항목을 본 round 에서 마감합니다.

### 1.2 v0.10.0 carryover 사실

Phase 10.D 결선 시 다음이 활성화 되었습니다(`audit-chain-rotation-automation-design.md` §13):
- `Entry.KeyEpoch *int64` + `Entry.LeaderEpoch *int64` 컬럼 + INSERT/SELECT propagation.
- `audit_chain_keys` 테이블(0037) + `KeyEpochProvider` interface(`internal/domain/audit/key_epoch.go`).
- `KeyRotator` 90일 quarterly cron + emergency override CLI(`internal/domain/audit/keyrotation/rotator.go`).
- `Repo.ExportV2` + `ExportEntryLine.KeyEpoch` + `ExportSignatureLine.BundleVersion="v2"` + `_chainKeyEpochs[]`.
- `fg-verify` v2(`cmd/rosshield-audit-verify/export_verify.go`) — v1/v2 자동 감지 + `verifyEpochTransitions` + epoch 별 public key cross-reference.

**미진행 사실**(`phase11-backlog-design.md` §4.3 + §2.5 인용):
> audit chain `canonicalMetaJSON` 은 알파벳순 7 키(action · actor · occurredAt · outcome · seq · target · tenantId)를 직렬화. **`keyEpoch` + `leaderEpoch` 미포함**. fg-verify v2 는 `chainKeyEpochs` 로 epoch 별 public key 를 검증하지만 hash chain input 자체는 epoch 없이 계산.

본 epic 의 cover 영역은 (a) `canonicalMetaJSON` 에 `keyEpoch` + `leaderEpoch` 추가 + (b) `fg-verify` v3 도입(v1/v2/v3 3-tier).

### 1.3 본 round 진입 가치

- **chain integrity 강화**: key_epoch 또는 leader_epoch 위변조 시 hash chain 자체가 mismatch → 위변조 즉시 감지. 현 v1/v2 는 epoch 위변조를 `audit_chain_keys` snapshot + `verifyEpochTransitions` 로만 cover 하므로 snapshot 자체 위변조 시 회피 가능성 잔존.
- **외부 감사인 호환성 강화**: v3 bundle 은 entry 단위에서 epoch 가 hash 에 포함된 사실을 외부 검증 도구로 단독 검증 가능 → ISMS-P SC-12 / SOC2 CC6.6(cryptographic key management) baseline 강도 강화. Phase 11.B(SOC2 readiness) 의 자연 후속.
- **v0.10.0 carryover 마감**: Phase 10.D 결선 시 명시 carryover — fg-verify v3 도입 시점에 마감 권장으로 합의됨. 다음 분기 도래 전 마감.
- **회귀 위험 작음**: DB 스키마 변경 0(컬럼 이미 결선) + 신규 entry 부터만 v3 hash 적용(기존 entry 변경 0, append-only 원칙 일관).
- **추정 짧음**: 2~3주 — Phase 11 어느 옵션보다 ROI 회수 빠름.

### 1.4 본 round 범위 · 비범위

- **범위** (Stage 11.C-2~6):
  - `canonicalMetaJSON` 에 `keyEpoch` + `leaderEpoch` field 추가(알파벳순 정렬, nil 처리 명시).
  - `ComputeEntryHash` chain transition 마킹 entry(`audit.chain.epoch_input_activated`) — 신규 entry 부터 v3 hash 적용 시점 명시.
  - `Repo.ExportV3` + `ExportSignatureLine.BundleVersion="v3"` + chain transition entry 노출.
  - `fg-verify` v3 verify 로직 + v1/v2/v3 자동 감지 + testdata fixture.
  - testcontainers e2e + ops docs + v0.13.0 minor release.
- **비범위** (§10 명시): 일괄 re-compute · v0.9.0 이하 강제 upgrade · multi-tenant 분리 hash chain · 외부 KMS / TPM-bound · entry segment rotation 변경.

---

## 2. 현재 상태 fact-check (코드/디렉터리 직접 grep)

본 §은 추측 0, fact 만 명시. 5 영역.

### 2.1 `internal/domain/audit/hash.go::canonicalMetaJSON` 현재 fields

`hash.go:45~85` Read 결과(head `85b6bb1` 직후):

| 항목 | fact |
|---|---|
| `ComputeEntryHash` signature | `(prevHash, payloadDigest Hash, e Entry) (Hash, error)` — Entry 전체 수신하나 meta 직렬화는 7 키만 사용. |
| hash input 공식 | `hash_i = sha256(prevHash[32] ‖ payloadDigest[32] ‖ canonicalMetaJSON)`. |
| `metaJSON` struct fields | **알파벳순 7 키**: `action`, `actor`(중첩 4 키), `occurredAt`(RFC3339Nano UTC), `outcome`, `seq`, `target`(중첩 2 키), `tenantId`. |
| **`keyEpoch` 노출** | **0 — 미포함**. |
| **`leaderEpoch` 노출** | **0 — 미포함**. |
| `error` 노출 | 의도적 제외 주석 명시(line 17): "error 텍스트 변경이 체인을 깨면 안 됨". |

→ **확정**: 현 hash 는 epoch 정보 0. v3 도입 시 `keyEpoch` + `leaderEpoch` 알파벳순 위치 결정 필요(둘 다 `k` + `l` 시작이라 `outcome` 앞).

### 2.2 `internal/domain/audit/audit.go::Entry` 구조체

`audit.go:78~99` Read 결과:

| 필드 | 결선 시점 | 타입 | nullable | fact |
|---|---|---|---|---|
| `LeaderEpoch *int64` | Phase 4 E25 HA | nullable | ✅ | `audit_entries.leader_epoch` 컬럼 nullable — HA 비활성 시 nil. |
| `KeyEpoch *int64` | Phase 10.D-2 | nullable | ✅ | `audit_entries.key_epoch` 컬럼 nullable — SwappableSigner 미주입 시 또는 v0.10.0 이전 INSERT 시 nil. |

→ Entry 단위 epoch 정보는 이미 결선되어 활용 가능. hash input 만 갱신 필요.

### 2.3 `internal/domain/audit/export.go` v1/v2 format

`export.go:46~103` Read 결과:

| 영역 | fact |
|---|---|
| `ExportEntryLine.KeyEpoch *int64` | Phase 10.D-5 추가. `json:"keyEpoch,omitempty"` — nil 이면 line 에서 미노출(v1 호환). |
| `ExportEntryLine.LeaderEpoch` | **부재** — entry line 에 leader_epoch 노출 0(현 v2 bundle 도 cover 안 함). |
| `ExportSignatureLine.BundleVersion` | `omitempty` — 빈 문자열이면 v1, `"v2"` 이면 v2. |
| `ExportSignatureLine.ChainKeyEpochs[]` | v2 만 — `[{epoch, keyId, publicKeyHex, createdAt, revokedAt?}]`. |
| `BundleVersionV2` const | `"v2"` (line 220). v3 const 부재. |

→ v3 도입 시 `ExportEntryLine.LeaderEpoch *int64` 신규 + `BundleVersionV3 = "v3"` 신규 + signature line 에 chain transition entry seq 표시 검토.

### 2.4 `cmd/rosshield-audit-verify/export_verify.go` v2 verify 로직

`export_verify.go:60~205` Read 결과:

| 영역 | fact |
|---|---|
| `exportOutput.BundleVersion` | `"v1" | "v2"` 두 값만. |
| `bundleVersionLabel(wire)` | 빈 문자열 → `"v1"`, 그 외 → wire 그대로. |
| `buildEpochMap` | `sig.BundleVersion != audit.BundleVersionV2` 분기 → v1 fallback(epoch=1 default). v3 분기 부재. |
| `lookupSigningPublicKey` | 동일 분기 — `!= BundleVersionV2` 면 v1 epoch=1 fallback. |
| `verifyEpochTransitions` | v2 만 호출(line 189). v3 분기 부재. |
| `verifyHashChain` | `ComputeEntryHash(e.PrevHash, e.PayloadDigest, e)` 호출 — 도메인 `ComputeEntryHash` 와 동일 signature. canonicalMetaJSON 갱신 시 자동 cover. |

→ v3 도입 시 (a) `BundleVersionV3` const + (b) `buildEpochMap`/`lookupSigningPublicKey` 의 분기 확장 + (c) v3 entry 의 `keyEpoch`/`leaderEpoch` 가 hash 에 포함되었는지 검증 step 추가 + (d) chain transition entry 검증.

### 2.5 `internal/domain/audit/sqliterepo/repo.go::Append` INSERT 시점

grep 결과 요약(line 35~169 + 294 + 393 + 711):

| 영역 | fact |
|---|---|
| `Deps.KeyEpoch audit.KeyEpochProvider` | optional. nil 이면 entry.KeyEpoch = nil. |
| Append INSERT 시점 | `r.deps.KeyEpoch != nil` 면 `r.deps.KeyEpoch.CurrentEpoch()` 호출하여 entry.KeyEpoch 채움. |
| INSERT SQL 컬럼 | `prev_hash, hash, leader_epoch, key_epoch` 4 컬럼 nullable INSERT(line 169). |
| `ExportV2` | `keyRepo.ListChainKeyEpochs(...)` 호출 → `ToExportChainKeyEpochs` 로 변환 → `signatureLine.ChainKeyEpochs` 에 채움(line 422~435). |
| SELECT 시 KeyEpoch/LeaderEpoch 복원 | line 631~635 — DB row 가 NULL 이 아니면 `&ep` 로 채움. |

→ INSERT 경로는 변경 0(컬럼 이미 활용). `ComputeEntryHash` 만 갱신하면 신규 entry 부터 자동 v3 hash. 기존 entry 는 v1 hash 그대로 보존(append-only).

---

## 3. 위협 모델 / 요구사항

### 3.1 신규/잔여 위협

| 위협 | 가능성 | 영향 | 본 epic cover |
|---|---|---|---|
| audit chain key_epoch 위변조 — `audit_chain_keys` snapshot 위변조 후 chain hash 재계산 가능성(epoch 정보 hash 외부) | 낮음(공격자 DB write 권한 + audit chain head 권한 모두 필요) | chain integrity 손상 — 외부 감사인 검증 회의 | §6 옵션 A(canonicalMetaJSON keyEpoch+leaderEpoch input 포함) |
| audit chain leader_epoch 위변조 — multi-region failover 시 epoch 변경의 chain integrity 검증 부재 | 낮음(HA 활성 시 공격자 leader_epoch row write 권한 필요) | split-brain 분석 회피 가능성 | 옵션 A — leaderEpoch 도 input 포함 |
| 외부 감사인 fg-verify 호환 — v3 미지원 시 false negative(v3 bundle 을 fg-verify v2 가 검증 시 epoch input 미포함으로 hash mismatch 보고) | 중(외부 감사인 v3 빌드 분배 필요) | 외부 감사인 검증 차단 | 옵션 A — fg-verify v3 자동 감지 + v1/v2/v3 3-tier backward compat |
| chain transition 시점 모호 — v1/v2 hash 와 v3 hash 가 같은 chain 안에 혼재 시 외부 도구 혼란 | 중(transition entry 미노출 시) | 외부 도구 검증 오류 | 옵션 A — `audit.chain.epoch_input_activated` entry 명시 + signature line 에 transition seq 노출 |
| v3 bundle 외부 검증 회귀 — v3 도입 후 v1/v2 fixture 검증 회귀 | 중(testdata fixture 충분히 cover 안 되면) | 외부 감사인 v1/v2 bundle 검증 차단 | Stage 11.C-5 — v1/v2/v3 fixture 3 종 회귀 test |

### 3.2 요구사항

| ID | 요구 | 측정 |
|---|---|---|
| R11C-1 | v3 도입 후 v1/v2 bundle backward compat 100% 유지 | 기존 fg-verify v1/v2 fixture PASS + 회귀 0 |
| R11C-2 | 신규 entry 부터만 v3 hash 적용(기존 entry append-only) | 마이그레이션 0 + 기존 audit_entries.hash 컬럼 변경 0 |
| R11C-3 | chain transition 시점 외부 도구가 명확히 인식 가능 | `audit.chain.epoch_input_activated` entry seq + signature line 에 transition_seq 노출 |
| R11C-4 | hash input 변경 후 외부 결정론적 재현 가능 | canonicalMetaJSON 필드 알파벳순 일관 + RFC3339Nano UTC 일관 |
| R11C-5 | Phase 0~10 baseline 회귀 0 | 기존 audit/audit_test.go + verify_test.go + e2e PASS |
| R11C-6 | 보수적 추정 일관 | memory `feedback_design_doc_conservative.md` |

---

## 4. 옵션 비교

각 옵션마다 (a) 설계 요약 (b) 가치 (c) 노력 추정(보수적) (d) 전제·의존 (e) 리스크.

### 4.1 옵션 A — canonicalMetaJSON 에 key_epoch + leader_epoch input 포함 + fg-verify v3 (권장)

**설계 요약**:
- `canonicalMetaJSON` 의 `metaJSON` struct 에 `keyEpoch *int64 json:"keyEpoch,omitempty"` + `leaderEpoch *int64 json:"leaderEpoch,omitempty"` 추가. 알파벳순 위치: `action` < `actor` < `keyEpoch` < `leaderEpoch` < `occurredAt` < `outcome` < `seq` < `target` < `tenantId` (총 9 키).
- nil 처리 명시: `omitempty` 일관 — v1/v2 chain 의 nil epoch entry 는 hash 에 epoch 노출 0(byte-identical with v1/v2 hash).
- chain transition entry(`audit.chain.epoch_input_activated`) 신규 — chain 활성 시점 명시. 본 entry 자체는 v3 hash 로 INSERT 되어 신규 hash 결정성 첫 anchor.
- `Repo.ExportV3` 신규 + `ExportSignatureLine.BundleVersion="v3"` + signature line 에 `_chainTransitionSeq` 신규 필드.
- `fg-verify` v3 자동 감지 — `BundleVersion="v3"` → v3 분기. v3 분기는 (a) v2 의 모든 step 통과 + (b) chain transition seq 검증 + (c) transition 이후 entry 의 hash 에 epoch 가 포함되었는지 재계산.

**가치**:
- chain integrity ★★★★★ — epoch 위변조 즉시 감지.
- 외부 감사인 호환성 ★★★★★ — v1/v2/v3 3-tier 자동 감지.
- compliance ★★★★ — SOC2 CC6.6 + ISMS-P SC-12 강도 강화.
- 기술 부채 ★★★★★ — Phase 10.D carryover 마감.
- 회귀 위험 ★★★★ — 신규 entry 부터만 적용, 기존 entry 변경 0.

**노력 추정(보수적)**: **2~3주**. Stage 11.C-2(canonicalMetaJSON 갱신 + 단위 test) 0.5주 + Stage 11.C-3(chain transition entry + 마이그레이션 0) 0.5~1주 + Stage 11.C-4(ExportV3 + BundleVersionV3) 0.5주 + Stage 11.C-5(fg-verify v3 + v1/v2/v3 fixture 3 종) 0.5~1주 + Stage 11.C-6(testcontainers + ops docs + v0.13.0 minor) 0.5주.

**전제·의존**: 없음. Phase 10.D 결선(KeyEpoch + LeaderEpoch + ChainKeyRepo) 활용. 외부 fg-verify 사용 customer 는 v3 자동 감지로 회귀 0(v1/v2 bundle 변경 0).

**리스크**: **중**. hash input 변경은 critical — chain transition entry 의 명확한 노출 필요. 외부 감사인이 v3 bundle 검증 시 fg-verify v3 binary 필요(★ 분배 절차 = Stage 11.C-6 후 외부 트랙). v3 fixture 회귀 충분히 cover 필수.

### 4.2 옵션 B — key_epoch 만 input 포함(leader_epoch 보류)

**설계 요약**: 옵션 A 와 동일하나 `leaderEpoch` 는 hash input 에서 제외. multi-region failover 미사용 customer cover 만 진행. `leaderEpoch` 는 별 epic 으로 이월.

**가치**:
- chain integrity ★★★★ — key_epoch 위변조 감지(leader_epoch 위변조 미감지).
- 외부 감사인 호환성 ★★★★★ — v1/v2/v3 3-tier 동일.
- compliance ★★★ — SOC2 CC6.6 cover(leader_epoch 는 별 epic).
- 기술 부채 ★★★ — Phase 10.D carryover 일부 마감(leader_epoch carryover 잔존).

**노력 추정(보수적)**: **1.5~2주**. canonicalMetaJSON 갱신 + ExportV3 + fg-verify v3 — leader_epoch 미포함이라 절반 감소.

**전제·의존**: 없음.

**리스크**: **낮음**. 옵션 A 의 부분집합. 단점은 leader_epoch carryover 가 별 epic 으로 이월되어 Phase 12+ 에 다시 design doc + Stage 분해 부담 발생.

### 4.3 옵션 C — 별 hash chain 신규(legacy + new 병행)

**설계 요약**: 기존 hash chain(v1+v2) 그대로 유지 + 신규 v3 hash chain(epoch input 포함) 병행 계산 + entry 마다 두 hash 컬럼 보존(`hash` + `hash_v3`). 외부 도구는 양쪽 검증 가능.

**가치**:
- chain integrity ★★★★ — epoch 위변조 감지 + 기존 hash 회귀 0.
- 외부 감사인 호환성 ★★★ — v1/v2 도구로 검증 가능 + v3 도구로 추가 검증.
- compliance ★★★ — 추가 검증 가능.
- 기술 부채 ★ — 추가 컬럼 + 추가 계산 부담.

**노력 추정(보수적)**: **3~4주**. 마이그레이션 1건(`hash_v3` 컬럼 추가) + 양쪽 hash 계산 + 외부 fg-verify 2 모드 모두 신규 + 부하 측정.

**전제·의존**: 마이그레이션 1건 — DB 스키마 변경.

**리스크**: **높음**. 두 hash 병행은 복잡성 큼 + 외부 감사인 혼란 가능 + 두 chain 의 정합성 검증 부담 + DB 스키마 변경. append-only 원칙(설계서 §1.9) 부분 위반 우려(hash_v3 컬럼이 새로 추가되더라도 기존 row 에 backfill 필요 vs nullable 처리).

### 4.4 옵션 D — input 포함 안 함, fg-verify 에서 chain 외 epoch 별도 검증

**설계 요약**: hash chain 변경 0. `fg-verify` 가 `audit_chain_keys` snapshot 의 무결성(예: cosign 서명, Merkle root)을 별도 검증. epoch 정보는 hash 외부에서만 검증.

**가치**:
- chain integrity ★★ — epoch 위변조 감지는 snapshot 서명 의존.
- 외부 감사인 호환성 ★★★ — fg-verify v2 그대로.
- compliance ★★ — SOC2 CC6.6 부분 cover.
- 기술 부채 ★★ — carryover 미마감.

**노력 추정(보수적)**: **1~1.5주**. audit_chain_keys snapshot 서명 + fg-verify v2 갱신.

**전제·의존**: 없음.

**리스크**: **중**. chain hash 가 epoch 정보를 cover 하지 않는 사실은 잔존 → 외부 감사인이 "왜 chain hash 에 epoch 가 없는지" 회의 가능성. snapshot 서명도 보호 layer 추가나 root-of-trust 추가에 의존.

### 4.5 매트릭스 종합

| 옵션 | 설계 | 가치 | 시간 | 위험 | 즉시 진입 | 외부 트랙 |
|---|---|---|---|---|---|---|
| **A** key+leader input + fg-verify v3 | canonicalMetaJSON 확장 + chain transition entry + ExportV3 | ★★★★★ | 2~3주 | 중 | ✅ | ★ fg-verify v3 분배 |
| B key 만 input + fg-verify v3 | leader_epoch carryover 잔존 | ★★★★ | 1.5~2주 | 낮음 | ✅ | ★ fg-verify v3 분배 |
| C 별 hash chain 병행 | hash_v3 컬럼 신규 + 양쪽 계산 | ★★★ | 3~4주 | 높음 | ⚠️ 마이그레이션 부담 | ★ fg-verify v3 분배 |
| D snapshot 서명만 | chain hash 변경 0 + snapshot 서명 layer | ★★ | 1~1.5주 | 중 | ✅ | — |

---

## 5. Top 1 권장 + 근거

### 5.1 권장 — 옵션 A (canonicalMetaJSON 에 key_epoch + leader_epoch input 포함 + fg-verify v3)

**근거**:
- 가장 깊은 chain integrity 강화 — epoch 위변조 즉시 hash mismatch 로 감지.
- backward compat 보장 — v1(omitempty 로 nil epoch entry 는 byte-identical) + v2(현 결선) + v3(신규) 3-tier 자동 감지.
- 외부 감사인 호환성 명확 — `audit_chain_keys` snapshot + entry 단위 `keyEpoch`/`leaderEpoch` + canonicalMetaJSON 안 epoch 까지 3 layer cross-reference.
- Phase 10.D carryover(key_epoch + leader_epoch 모두) 한 round 로 마감 — 별 epic 잔존 0.
- DB 스키마 변경 0 — 컬럼 이미 결선, hash input 만 갱신.
- Phase 11.B(SOC2 readiness) 의 자연 후속 — SOC2 CC6.6 + ISMS-P SC-12 강도 강화.
- 회귀 위험 작음 — 신규 entry 부터만 적용, 기존 entry append-only 일관.

**추정**: 2~3주 — Phase 11 어느 옵션보다 짧음. R11C-1~6 모두 충족.

### 5.2 보류 옵션

- **옵션 B**: leader_epoch carryover 잔존. Phase 12+ 재진입 부담 발생. 권장 default 에서 제외.
- **옵션 C**: 복잡성 + 마이그레이션 + 외부 감사인 혼란 위험. 권장 default 에서 제외.
- **옵션 D**: chain hash 자체 강화 부재. 외부 감사인 회의 잔존. 권장 default 에서 제외.

---

## 6. Stage 분해 (옵션 A 채택 가정)

memory `feedback_design_doc_first.md` 일관 — Stage 분해는 본 doc 에서 마감, 코드 진입은 D-P11C-1 확정 후.

### 6.1 Stage 11.C-1 — design doc 채택 (본 round)

본 round (docs only, 코드 0). 추정 0.

### 6.2 Stage 11.C-2 — canonicalMetaJSON 갱신 + 단위 test

추정 **0.5주**.
- `internal/domain/audit/hash.go::canonicalMetaJSON` — `metaJSON` struct 에 `KeyEpoch *int64 json:"keyEpoch,omitempty"` + `LeaderEpoch *int64 json:"leaderEpoch,omitempty"` 알파벳순 추가.
- 알파벳순 결정: `action` < `actor` < `keyEpoch` < `leaderEpoch` < `occurredAt` < `outcome` < `seq` < `target` < `tenantId` (9 키).
- nil 처리: `omitempty` — v1/v2 chain 의 nil epoch entry 는 byte-identical with v1/v2 hash. 즉 v3 hash 함수가 v1/v2 entry 에 대해서도 backward-compat 한 결과 산출.
- `ComputeEntryHash` signature 변경 0 — Entry 전체 수신 그대로.
- 단위 test: (a) v1 entry(KeyEpoch=nil, LeaderEpoch=nil) → v3 hash function 결과가 기존 v1 hash 와 byte-identical. (b) v3 entry(KeyEpoch=&3, LeaderEpoch=&7) → hash 가 epoch 포함 직렬화 반영. (c) epoch 값 변경 시 hash mismatch. (d) canonicalMetaJSON byte stream 결정성 검증(같은 entry 두 번 직렬화 시 byte-identical).
- Red → Green → Refactor.

### 6.3 Stage 11.C-3 — chain transition entry + Repo Append 시점 분기

추정 **0.5~1주**.
- 신규 audit event action: `audit.chain.epoch_input_activated`.
- 운영자 admin endpoint 또는 startup hook 에서 1회 emit — chain transition 시점 명시.
- `audit_chain` 또는 별 row 없음 — entry 자체가 transition marker. signature line 의 `_chainTransitionSeq` 필드로 외부 도구가 자동 인식.
- 마이그레이션 0(기존 컬럼 활용).
- 정책 결정 항목(D-P11C-2): 기존 entry hash 일괄 re-compute X(append-only 일관) vs Y(re-compute). **권장 default = X(기존 entry 변경 0)**.
- 단위 test: (a) transition entry emit 후 다음 entry 의 hash 가 v3 형식. (b) transition entry 이전 entry 의 hash 는 변경 0. (c) transition entry 자체의 hash 는 v3 형식. (d) idempotency(중복 emit 차단 또는 idempotent).

### 6.4 Stage 11.C-4 — ExportV3 + BundleVersionV3

추정 **0.5주**.
- `internal/domain/audit/export.go`:
  - `BundleVersionV3 = "v3"` const 신규.
  - `ExportEntryLine.LeaderEpoch *int64 json:"leaderEpoch,omitempty"` 신규 — v2 fixture 비호환 risk 검증(v2 fixture 의 entry 에 leaderEpoch 가 없으므로 unmarshal 시 nil → omitempty 로 byte-identical 유지).
  - `ExportSignatureLine.ChainTransitionSeq *int64 json:"_chainTransitionSeq,omitempty"` 신규.
- `internal/domain/audit/sqliterepo/repo.go`:
  - `Repo.ExportV3(ctx, tx, tenantID, fromSeq, toSeq, sgn, keyRepo) (io.ReadCloser, error)` 신규.
  - 내부 구현 = `ExportV2` + `BundleVersion="v3"` + `LeaderEpoch` 노출 + `ChainTransitionSeq` 조회.
- API handler — `GET /api/v1/audit/export?version=v3` (기본 v2 유지, opt-in v3).
- 단위 test: (a) v3 bundle export 후 unmarshal → signature line BundleVersion="v3" + leaderEpoch 노출. (b) v2 fixture 회귀 byte-identical.

### 6.5 Stage 11.C-5 — fg-verify v3 verify 로직 + v1/v2/v3 fixture 3 종

추정 **0.5~1주**.
- `cmd/rosshield-audit-verify/export_verify.go`:
  - `exportOutput.BundleVersion` enum 확장 — `"v1" | "v2" | "v3"`.
  - `bundleVersionLabel(wire)` — `"v3"` 매핑 추가.
  - `buildEpochMap`/`lookupSigningPublicKey` 분기 확장 — v3 도 v2 와 동일한 `_chainKeyEpochs` 사용(v3 는 epoch 가 hash 안에 있다는 사실 보장).
  - `verifyEpochTransitions` v3 분기 — chain transition entry 검증 추가 + transition 이후 entry 는 hash recompute 시 keyEpoch+leaderEpoch 포함 보장.
  - 신규 step: `chainTransition` — `_chainTransitionSeq` 와 `audit.chain.epoch_input_activated` entry seq 일치 검증.
- testdata fixture 3 종(`testdata/v1-bundle.ndjson.gz` · `v2-bundle.ndjson.gz` · `v3-bundle.ndjson.gz`) — 각 fixture 의 chain 정합 검증 PASS.
- 단위 test: (a) v1 fixture PASS. (b) v2 fixture PASS. (c) v3 fixture PASS. (d) v3 fixture 에서 entry hash 1 byte tamper 시 FAIL. (e) v3 fixture 에서 chain transition seq mismatch 시 FAIL.
- 외부 빌드 일관 — stdlib + crypto/ed25519 + audit 도메인 only. P5 일관.

### 6.6 Stage 11.C-6 — testcontainers integration + ops docs + v0.13.0 minor release

추정 **0.5주**.
- `test/integration/audit_chain_v3_e2e_test.go` 신규 — Postgres + audit chain 활성 + transition entry emit + ExportV3 + fg-verify v3 PASS.
- `docs/operations/audit-chain-v3-migration.md` 신규 — chain transition 시점 + fg-verify v3 빌드 + 외부 감사인 분배 절차.
- `docs/operations/audit-verify-cli.md` 갱신 — v1/v2/v3 backward compat 표.
- `docs/releases/v0.13.0.md` + `CHANGELOG.md [0.13.0]` entry.
- v0.13.0 minor release tag(★ remote push 는 사용자 명시 요청 시).

### 6.7 Stage 11.C-2~11.C-6 합계

**2~3주** (보수적). 마감 시 audit hash chain 의 key_epoch + leader_epoch input 완전 결합 + fg-verify v3 + 첫 v0.13.0 minor release.

---

## 7. 결정 항목 (D-P11C-1~4)

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시.

### 7.1 D-P11C-1 — 옵션 채택

- (1) **옵션 A 채택** — canonicalMetaJSON key_epoch + leader_epoch input + fg-verify v3 (**권장 default**).
- (2) 옵션 B — key_epoch 만 input(leader_epoch 보류).
- (3) 옵션 C — 별 hash chain 병행.
- (4) 옵션 D — snapshot 서명만(hash chain 변경 0).
- (5) 거부 — Phase 11.C 비채택, Phase 11.A(OpenTelemetry) 또는 다른 옵션으로 우회.

**근거**: 옵션 A 는 v0.10.0 carryover(key_epoch + leader_epoch 모두) 한 round 로 마감 + DB 스키마 변경 0 + backward compat 강력 + 추정 짧음(2~3주). 옵션 B 는 leader_epoch 잔존이 Phase 12+ 재진입 부담. 옵션 C 는 복잡성 + 마이그레이션 부담. 옵션 D 는 chain hash 자체 강화 부재.

### 7.2 D-P11C-2 — chain transition 처리

- (1) **기존 entry v1/v2 hash 유지(신규 entry 부터 v3)** — append-only 원칙 일관 (**권장 default**).
- (2) 일괄 re-compute — 모든 기존 entry 의 hash 를 v3 로 갱신.

**근거**: append-only 원칙(설계서 §1.9) 일관 — UPDATE 가능한 audit 테이블 금지. 일괄 re-compute 는 (a) 외부 도구가 보유한 기존 bundle 의 hash 와 mismatch → 외부 감사인 검증 회의 (b) audit 테이블 무결성 자체 손상. chain transition entry 로 시점 명시 충분.

### 7.3 D-P11C-3 — leader_epoch input 포함 여부

- (1) **key_epoch + leader_epoch 둘 다 포함** — 옵션 A (**권장 default**).
- (2) key_epoch 만 포함 — 옵션 B.

**근거**: leader_epoch 도 HA 활성 시 chain integrity 의 중요한 dimension. multi-region failover 시 epoch 변경의 chain hash cover 가 필요. 추정 차이는 0.5주 정도로 한 round 안에 묶어 마감 권장.

### 7.4 D-P11C-4 — v3 bundle 외부 감사인 분배 시점

- (1) **Stage 11.C-6 release 후(v0.13.0 minor) 즉시 분배** — release 시점에 fg-verify v3 binary + docs 동시 분배 (**권장 default**).
- (2) Stage 11.C-5 완료 직후 분배(release 이전) — 외부 감사인 사전 검토.
- (3) 첫 v3 bundle 생성 후 customer 별 분배 — customer trigger.

**근거**: release 시점에 fg-verify v3 binary + migration ops docs 동시 분배가 안정. 사전 검토는 외부 감사인 트랙 의존(★) — 본 epic 의 외부 트랙은 release 시점에 한정.

---

## 8. 마이그레이션 · 호환성 영향

### 8.1 DB 마이그레이션 영향

- **0건**. `audit_entries.key_epoch` + `leader_epoch` 컬럼 이미 Phase 10.D 결선. `audit_chain_keys` 테이블 이미 결선(0037). 신규 마이그레이션 0.

### 8.2 chain hash 호환성

- v1 entry(KeyEpoch=nil, LeaderEpoch=nil): v3 hash function 으로 재계산 시 omitempty 로 byte-identical → 기존 hash 와 일치.
- v2 entry(KeyEpoch=&n, LeaderEpoch=nil): v3 hash function 으로 재계산 시 keyEpoch 추가 노출 → 기존 v2 hash 와 mismatch 가능성.

→ **중요**: v2 entry 는 기존 hash 가 epoch 없이 계산되어 있으나 v3 도입 시 chain transition entry 이전의 모든 entry 는 기존 hash 그대로 보존(re-compute 0). v3 hash 는 chain transition entry 이후의 신규 entry 에만 적용.

→ **검증 단계 fg-verify 처리**: v1/v2 bundle 은 그대로 verify(기존 hash 검증) + v3 bundle 은 transition seq 이전 entry 는 v1/v2 hash function 으로 검증 + transition seq 이후 entry 는 v3 hash function 으로 검증.

### 8.3 fg-verify SDK backward compat

- v1 bundle(v0.9.0 이하): `_bundleVersion` 부재 → v1 분기 그대로.
- v2 bundle(v0.10.0~v0.12.x): `_bundleVersion="v2"` → v2 분기 그대로.
- v3 bundle(v0.13.0+): `_bundleVersion="v3"` → v3 분기 신규.

→ fg-verify v3 binary 는 세 분기 모두 cover. fg-verify v2 binary 는 v1/v2 만 cover — v3 bundle 검증 시 "unknown bundleVersion" error 명시. 외부 감사인은 fg-verify v3 binary 분배 필요(★ Stage 11.C-6 분배).

### 8.4 외부 감사인 분배 절차 (★ 외부 트랙)

- Stage 11.C-6 v0.13.0 minor release 시점에 fg-verify v3 binary + `audit-chain-v3-migration.md` ops docs 동시 분배.
- 외부 감사인은 기존 fg-verify v2 binary 도 v1/v2 bundle 검증 시 그대로 유지 가능. v3 bundle 검증 시점에만 v3 binary 로 업그레이드.

---

## 9. 리스크 / 운영 고려

### 9.1 chain re-compute 시 기존 hash 무결성 보장

- re-compute 는 신규 entry 에만 적용 — 기존 entry 의 hash 변경 0(append-only 원칙).
- chain transition entry 시점에 외부 도구가 분기 인식 — `_chainTransitionSeq` signature line 필드 + `audit.chain.epoch_input_activated` entry 둘 다 노출.

### 9.2 fg-verify v3 회귀

- v1/v2/v3 fixture 3 종 + 각 fixture 의 chain 정합 검증 PASS + tamper 시 FAIL 검증.
- fg-verify v3 binary 는 P5 일관(stdlib + crypto/ed25519 + audit 도메인 only).
- 외부 빌드 가능성 검증(make build-fg-verify 단독).

### 9.3 audit chain head sha 변경 — multi-region replication 영향

- chain transition entry emit 시점에 chain head sha 가 한 번 갱신 — multi-region replication 자연 따라옴(현 결선된 replication 도메인 활용).
- 운영자 dashboard 의 RegionAuditConsistency 카드(Phase 10 옵션 A 결선) 가 자동 cover.

### 9.4 외부 감사인 fg-verify v3 빌드 + 분배 절차 (★ 외부 트랙)

- Stage 11.C-6 release 후 외부 감사인 분배는 사용자 외부 트랙(memory `feedback_user_tracks.md` 일관). 본 doc 권장 default 에서 분배 절차의 진행 자체는 사용자 결정.

### 9.5 Phase 11.B(SOC2 readiness) 와의 충돌

- Phase 11.B 의 `soc2-controls` pack 의 CC6.6 check 가 audit chain key rotation 정기성 검증 → 본 epic 의 chain transition entry 추가가 CC6.6 effectiveness 측정 강도 강화에 자연 cover.
- Phase 11.B 마감 후 자연 진입(`phase11-backlog-design.md` §12.1 일관).

---

## 10. 비목표 / 거부

본 Phase 11.C 에서 명시 거부:

### 10.1 audit chain 일괄 re-compute

설계서 §1.9 불변성 원칙 — append-only. 기존 entry 의 hash 변경 0. chain transition entry 로 시점 명시 충분.

### 10.2 v0.9.0 이하 customer 강제 upgrade

backward compat 유지 — v1 bundle 은 fg-verify v3 가 자동 v1 분기로 처리. v0.9.0 이하 customer 는 그대로 유지.

### 10.3 multi-tenant 분리 hash chain

별 epic. tenant 별 chain 분리는 현 단일 system tenant 가정과 별 layer. Phase 12+ 후보.

### 10.4 외부 KMS 통합 / TPM-bound hash chain

별 epic. 외부 KMS / TPM 결합은 customer 환경 의존. Phase 12+ 후보.

### 10.5 audit entry segment rotation 변경

별 layer 결선(0032~0036). 본 epic 은 entry 단위 hash 만 변경.

### 10.6 LLM 필수 경로 도입

설계서 §1.2 옵트인 원칙 일관. 본 epic 은 LLM 0.

### 10.7 tenant_id 없는 신규 테이블

설계서 §1.4 멀티테넌시 원칙 일관. 본 epic 은 신규 테이블 0(기존 audit_entries / audit_chain_keys 활용).

### 10.8 UPDATE/DELETE 가능한 audit 테이블

설계서 §1.9 불변성 원칙. 본 epic 은 신규 entry append 만 — 기존 entry UPDATE/DELETE 0.

### 10.9 Remote push 자동화

CLAUDE.md 일관 — local 커밋 OK, remote push 사용자 명시 요청 시에만.

---

## 11. 참조

### 11.1 직전 design doc 패턴

- `docs/design/notes/phase11-backlog-design.md` §4.3 + §12.1 — 본 doc 직접 부모.
- `docs/design/notes/audit-chain-rotation-automation-design.md` — Phase 10 옵션 D 본체, fact-check + Stage 분해 패턴 1차 모방.
- `docs/design/notes/soc2-readiness-design.md` — Phase 11 옵션 B 본체, 패턴 모방.
- `docs/design/notes/auto-failover-research.md` — Phase 9 진입 doc 패턴.
- `docs/design/notes/multi-region-ha-design.md` — Phase 8 epic 본체.

### 11.2 release / CHANGELOG

- `docs/releases/v0.10.0.md` — Phase 10 옵션 D audit chain signer key rotation 자동화. v2 bundle 결선.
- `docs/releases/v0.10.1.md` · `v0.10.2.md` — v0.10.0 lint hot fix.
- `docs/releases/v0.11.0.md` — Phase 10 옵션 E ros2-humble + DDS/SROS2.
- `CHANGELOG.md [Unreleased]` — "audit hash chain key_epoch+leader_epoch input 포함" 항목(본 epic 마감 대상).

### 11.3 설계서

- `docs/design/01-principles.md` — 12 원칙(특히 §1.9 불변성, §1.4 멀티테넌시, §1.2 옵트인).
- `docs/design/10-audit-and-observability.md` — audit chain + hash 체인 명세.
- `docs/design/11-tech-stack-and-roadmap.md` — 로드맵 + 결정 로그.
- `docs/design/12-migration-and-non-goals.md` — 비목표.

### 11.4 코드/디렉터리 fact-check 참조

- `internal/domain/audit/hash.go` — canonicalMetaJSON 현재 7 키 직렬화.
- `internal/domain/audit/audit.go` — Entry.KeyEpoch + LeaderEpoch nullable field 결선.
- `internal/domain/audit/export.go` — v1/v2 bundle wire format + BundleVersionV2 const.
- `internal/domain/audit/sqliterepo/repo.go` — Append/Export/ExportV2 결선 + key_epoch/leader_epoch INSERT 경로.
- `internal/domain/audit/keyrotation/rotator.go` — Phase 10.D 90일 quarterly cron + emergency override.
- `cmd/rosshield-audit-verify/export_verify.go` — fg-verify v2 verify 로직(v1/v2 자동 감지).
- `internal/domain/audit/key_epoch.go` — KeyEpochProvider interface.

### 11.5 외부 표준

- AICPA SOC2 Trust Services Criteria CC6.6 — Cryptographic key management.
- ISMS-P SC-12 — 암호화 키 관리.
- NIST SP 800-53 SC-12 — Cryptographic Key Establishment and Management.
- NIST SP 800-57 — Key Management Best Practices(epoch + transition 정의).

### 11.6 memory feedback

- `feedback_design_doc_first.md` — 1일+ 임계 design doc 우선.
- `feedback_design_doc_conservative.md` — 잠재 효과/시간 보수적.
- `feedback_parallel_agents.md` — 매 stage 시작 시 병렬 가능성 재평가.
- `feedback_user_tracks.md` — D1·E36·SOC2 감사·customer trigger·fg-verify v3 분배 등 외부 트랙 ★ 표기.
- `feedback_recommend_next_actions.md` — 다음 추천 작업 3~5건 명시.
- `feedback_skip_handoff.md` — handoff edit/commit/push 생략, CHANGELOG + release notes + commit 메시지로 trace.
