# Audit Chain Rotation — Design Doc

> Phase 8 carryover · Lodestar v0.6.2 후속
> 상태: **설계 (Stage 0)** — 코드 0줄
> 참조: `docs/design/10-audit-and-observability.md` §10.4 ~ §10.7 · `internal/audit/` · `internal/enterprise/crosswitness/` (A-1 cross-witness fold-in) · `migrations/0022_leader_epoch.up.sql`

---

## 1. 상태 / 배경

### 1.1 현재 구조

- `audit_entries` 테이블은 **단일 chain, append-only**.
- entry 1행 = `(tenant_id, seq, occurred_at, actor, action, target, payload_digest, outcome, prev_hash, hash, leader_epoch)`.
- 테넌트 단위 chain: `ε (genesis) → e1 → e2 → ... → eN`.
- `ChainHead` 테이블이 테넌트별 현재 `(seq, hash)`를 관리.
- `CheckpointSignature`(Ed25519)이 시간/이벤트 단위로 체인 상태를 외부에 증명.
- A-1 (R-D8 v1+v2): cross-witness가 N개 인스턴스 chain head를 fold-in — 분산 무결성.

### 1.2 운영 가정

- 단일 tenant 일평균 audit entry: 10k (스캔 + UI 작업 + READ 강화 모드 일부).
- 1년: `10k × 365 = 3.65M`/tenant.
- 100 tenant 운영 환경: `365M row/year`.
- entry 평균 크기 ~1KB (digest + JSON) → **~365GB hot DB/year** (인덱스 포함 시 1.5×~2× 추가).

### 1.3 통증 (1년+ paying customer 시점)

| 차원 | 1개월 | 1년 | 3년 |
|---|---|---|---|
| row 수 (100 tenant) | 30M | 365M | 1.1B |
| hot DB 디스크 | 30GB | 365GB | 1.1TB (인덱스 제외) |
| chain verify latency (sequential O(n)) | <10s | **수 분~수십 분** | **시간 단위** |
| `seq` BIGINT 한도 | 안전 | 안전 | 안전 (이론) |
| backup 시간 | 분 | **시간** | **반나절** |

**핵심 통증** — chain verify는 `prev_hash` 누적 sha256 재계산이라 **strict sequential**. parallel 화 불가능 (cross-witness fold-in 전까지). 1년 chain 1회 재검증 = pg_dump 후 single-thread sha256 365M 회 ≈ **수십 분**. 감사인 SLA "1분 내 검증"을 깨지게 됨.

---

## 2. 위협 모델 / 요구사항

### 2.1 functional

- **F1** chain verify (전체 history) latency ≤ **1분** (감사인 요구; 부분 chain 결합).
- **F2** rotation 자체가 무결성 보장. rotation 시점 entry가 chain의 다음 link로 들어가야 함 (rotation entry는 cold reference + cold archive sha256 포함).
- **F3** old (rotated) entry 외부 검증 가능 — cold storage download + verify CLI standalone.
- **F4** hot DB 디스크 ≤ **100GB / tenant 100 / 1년 retention** (목표 cap).
- **F5** rotation은 무중단 (online) — 신규 entry 처리 멈추지 않음.

### 2.2 non-functional / 보안

- **N1** cold archive는 **변조 불가** — sigstore cosign 또는 자체 Ed25519 서명 + sha256 sealing.
- **N2** cold storage가 deleted/lost 되어도 hot chain은 `archive_hash` reference만 들고 있어 **변조는 탐지**됨 (가용성 ≠ 무결성).
- **N3** cross-witness (A-1)는 rotation entry도 fold-in — 분산 증명 유지.
- **N4** rotation 작업 자체가 audit entry로 기록 (`audit.rotate.start` / `audit.rotate.complete`).
- **N5** tenant 격리 — rotation은 tenant 단위 (혹은 cluster-wide 옵션).

### 2.3 비기능 (운영)

- **O1** S3 endpoint 부재 site (airgap) 지원 — NFS / 로컬 디렉토리 backend도 동일 인터페이스.
- **O2** verify CLI는 cold archive를 stream 검증 가능해야 함 (전체 download가 부담될 때).
- **O3** rotation 실패 시 idempotent retry — partial state 없음.

---

## 3. 옵션 비교 (≥3)

| 옵션 | rotation trigger | cold backend | chain segmentation | verify 효율 | 강점 | 약점 |
|---|---|---|---|---|---|---|
| **A — 시간 기반 checkpoint 분할** | cron 월 1회 (혹은 hot retention=1년 도달) | S3 + (NFS fallback) | hot = 최근 1년, cold = 월별 segment | hot O(checkpoint개수=12) + cold O(1 per segment) | 단순 / 예측 가능 / 표준 / audit 친화 | manual rotation tuning |
| **B — row count threshold 동적 rotation** | row count > 10M 또는 disk > 80% | S3 + audit_archive 테이블 | dynamic segment (가변 길이) | adaptive | latency 자동 조정 | 복잡 / segment 경계 비결정적 / 감사 reproducibility 약화 |
| **C — tenant 별 fully isolated chain + rotation** | tenant 단위 cron | S3 prefix `s3://bucket/<tenantId>/segments/` | tenant당 chain 완전 분리 | per-tenant parallel verify | 멀티테넌시 자연 / 격리 강 | tenant 적은 site overhead (수십 chain 관리) / cross-witness fold-in 복잡 |
| **D — event sourcing → Kafka log compaction** | 항상 streaming | Kafka log + S3 sink | log topic 분할 | 분산 / parallel scalable | infra 부담 / 운영 부담 / airgap 부적합 / open-core 일관성 깨짐 |
| **E — Merkle tree 변형 (per-segment root)** | A와 동일 trigger + segment root tree | A와 동일 | hot = Merkle root 리스트 / cold = entry + Merkle proof | O(log n) per entry lookup | proof-of-inclusion 매우 강 | hash 계산 변경 (chain 호환성 깨짐) / Phase 8 범위 초과 |

### 3.1 평가 기준

- **(W1) 단순성** — paying customer 1년+ 운영 가능, 빠르게 구현.
- **(W2) airgap 친화** — 원칙 #3.
- **(W3) 외부 검증 표준성** — 감사인이 받아들이는 도구.
- **(W4) cross-witness (A-1) fold-in 일관성**.
- **(W5) 추후 Merkle tree (E) 마이그레이션 여지**.

| 옵션 | W1 | W2 | W3 | W4 | W5 |
|---|---|---|---|---|---|
| A | ◎ | ◎ (NFS fallback) | ◎ | ◎ | ○ |
| B | ○ | ○ | △ | ○ | △ |
| C | △ | ○ | ○ | △ | △ |
| D | × | × | △ | × | × |
| E | △ | ◎ | ◎ | ◎ | — (이미 Merkle) |

### 3.2 권장 default

**옵션 A** — 시간 기반 checkpoint 분할 + S3 (NFS fallback) export.

근거:

1. paying customer 1년+ 시점의 최우선 통증 (verify latency) 해소 + 단순.
2. airgap profile에서 S3 driver 미사용 → NFS / 로컬 디렉토리 backend로 동일 동작.
3. 감사인이 받아들이는 표준 (NDJSON segment + sha256 + signature).
4. cross-witness (A-1)이 rotation entry를 fold-in — 분산 증명 유지.
5. 추후 E (Merkle) 마이그레이션 시 segment 구조가 그대로 leaf가 됨.

옵션 B/C/E는 paying customer N=2~3 도달 시 재평가 항목으로 backlog.

---

## 4. 아키텍처

### 4.1 데이터 모델 변경

신규 테이블:

```sql
-- 0028_audit_rotation.up.sql
CREATE TABLE audit_rotations (
  rotation_id        UUID PRIMARY KEY,
  tenant_id          UUID NOT NULL,
  segment_no         BIGINT NOT NULL,        -- tenant 내 단조 증가
  seq_from           BIGINT NOT NULL,        -- inclusive
  seq_to             BIGINT NOT NULL,        -- inclusive
  occurred_at_from   TIMESTAMPTZ NOT NULL,
  occurred_at_to     TIMESTAMPTZ NOT NULL,
  entry_count        BIGINT NOT NULL,
  archive_sha256     BYTEA NOT NULL,         -- segment NDJSON sha256
  archive_size_bytes BIGINT NOT NULL,
  archive_uri        TEXT NOT NULL,          -- 's3://...' or 'file:///...'
  signature_alg      TEXT NOT NULL,          -- 'cosign-keyless-v1' or 'ed25519-v1'
  signature          BYTEA NOT NULL,
  signer_key_id      TEXT NOT NULL,
  leader_epoch       BIGINT NOT NULL,
  prev_segment_hash  BYTEA,                  -- 직전 segment archive_sha256 (chain 분할 link)
  rotated_at         TIMESTAMPTZ NOT NULL,
  rotated_by         TEXT NOT NULL,          -- 'system:cron' or 'user:us_...'
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, segment_no),
  CHECK (seq_to >= seq_from),
  CHECK (occurred_at_to >= occurred_at_from)
);

-- audit_entries에 외래 reference 컬럼 추가 (옵션, hot에 있는 행만)
ALTER TABLE audit_entries
  ADD COLUMN rotated_into_segment UUID REFERENCES audit_rotations(rotation_id);
-- rotated_into_segment IS NULL 이면 hot, IS NOT NULL 이면 곧 삭제될 (Stage 2 GC) 행
```

`audit_rotations`은 audit table 자체와 같은 불변성 (`UPDATE/DELETE` 차단 트리거). hot DB에 영구 보관 (segment 메타데이터; segment 본문은 cold).

### 4.2 rotation 절차 (online, idempotent)

```
1. (lock) tenant 단위 rotation advisory lock (pg_advisory_lock)
2. (cut) hot chain head 확인 → cut point = (seq_to = head.seq, occurred_at_to = head.occurred_at)
3. (snapshot) seq_from = 직전 rotation.seq_to + 1 (혹은 0)
   대상 행: WHERE tenant_id = $1 AND seq BETWEEN seq_from AND seq_to AND rotated_into_segment IS NULL
4. (serialize) NDJSON으로 직렬화 (canonical JSON, sort keys; entry 한 줄)
5. (hash) sha256 streaming 계산 → archive_sha256
6. (sign) cosign keyless OR Ed25519 서명 (D-AR-4 결정)
7. (upload) S3 PUT (혹은 NFS write) — multipart, retry
8. (verify) S3 GET head + sha256 재계산 검증 (round-trip)
9. (record) audit_rotations INSERT (signature, archive_uri, archive_sha256, prev_segment_hash)
10. (chain entry) audit_entries에 'audit.rotate.complete' entry 추가 — prev_hash = head.hash,
    payload_digest = sha256(rotation_id || archive_sha256 || segment_no)
11. (mark) WHERE seq BETWEEN seq_from AND seq_to UPDATE rotated_into_segment = rotation_id
12. (gc, deferred) hot retention 정책에 따라 별도 GC job이 rotated_into_segment IS NOT NULL 행을
    DELETE (단 audit_rotations의 sha256과 archive_uri는 그대로 보존)
13. (unlock)
```

**중요**: step 10의 `audit.rotate.complete` entry는 chain의 정상 link로 들어감 (genesis → ... → rotate.complete → 신규 entry). 따라서 chain은 끊기지 않고, rotation 자체가 외부 검증의 일부.

### 4.3 chain verify (hot + cold 결합)

```
verify(tenant, from_seq, to_seq):
  1. segments = audit_rotations WHERE tenant=$1 AND [seq overlap]
  2. for each segment:
       download archive_uri (또는 NFS read)
       sha256 검증 == archive_sha256
       signature 검증 (cosign / Ed25519)
       NDJSON parse → entry stream
       chain reconstruction (segment 내 prev_hash → hash)
  3. segment 간 link 검증: segment[i].first.prev_hash == previous-cold-or-rotation.hash
  4. hot range (마지막 segment.seq_to+1 ~ current.head): hot DB에서 직접 traversal
  5. chain head signature (Ed25519) 검증
  6. cross-witness fold-in (A-1) 확인
```

### 4.4 verify CLI 확장

`cmd/rosshield-audit-verify` 기존 `--bundle audit-export.tar.gz` 외에:

```
rosshield-audit-verify \
  --hot-endpoint https://...:8443 \
  --token vt_... \
  --tenant tn_... \
  --from 2025-01-01 --to 2026-05-19 \
  --cold-cache ./cache/ \           # download 캐시
  --signing-bundle ./trusted-keys/  # cosign root / Ed25519 pubkey
```

- hot range: API endpoint에서 `/verify/vt_*/entries` 받기.
- cold range: `audit_rotations.archive_uri` 받기 → S3 / file URI 직접 GET.
- streaming hash & link 검증 → exit 0 (OK) / non-zero + reason.

### 4.5 ChainHead·CheckpointSignature 영향

- `ChainHead`는 **항상 hot의 현재 head**를 가리킴 (변경 없음).
- 별도 `RotationHead` (per tenant: last segment_no + last archive_sha256) 추가 → cross-witness fold-in 시 hot head + rotation head 두 가지 fold.
- `CheckpointSignature`는 hot DB에 그대로 — rotation이 일어나도 과거 checkpoint signature는 audit segment 내에 포함되어 cold로 옮겨감.

### 4.6 cross-witness (A-1) 통합

- 기존 cross-witness는 `(tenantId, hash, seq)` fold-in.
- rotation 발생 시 추가 fold-in payload: `(tenantId, segment_no, archive_sha256, rotated_at)`.
- A-1 검증 단계에 "rotation segment 일치"가 추가됨 → tenant의 cold archive가 변조되어도 다수 witness가 원본 sha256 합의를 보유.

---

## 5. TDD 진입

### Red 단계 (가장 먼저 추가될 실패 테스트들)

1. `TestRotation_Plan_EmptyTenant_ReturnsNoOp` — 신규 tenant rotation no-op.
2. `TestRotation_Execute_100Entries_ArchiveSha256Match` — 100 entry → NDJSON → sha256 일치.
3. `TestRotation_S3Upload_RoundTripVerify` — mock S3 (minio testcontainer) upload + download + sha256.
4. `TestRotation_RecordsAuditEntry` — rotation 완료 후 `audit.rotate.complete` entry가 chain head에 있음.
5. `TestRotation_HotEntries_MarkedRotated` — `rotated_into_segment` 값 일치.
6. `TestVerify_HotPlusCold_Combined` — hot 50 entry + cold 50 entry segment 결합 verify PASS.
7. `TestVerify_TamperedArchive_Fails` — cold archive 1 byte 변조 → verify FAIL with reason.
8. `TestRotation_Idempotent_RetryAfterPartialFail` — S3 upload 후 DB record 직전 fail → 재시도 시 archive_uri 동일.
9. `TestRotation_LeaderEpoch_Honored` — non-leader는 rotation 거부.
10. `TestRotation_AdvisoryLock_PreventsConcurrent` — 동시 rotation 1개만 진행.
11. `TestVerify_SignatureMismatch_Fails` — cosign 서명 wrong key → FAIL.
12. `TestCrossWitness_RotationFoldIn` — rotation 발생 → witness payload에 segment hash 포함.

각 테스트는 표준 Go test + testcontainers (PG + minio).

---

## 6. Stage 분해 (6 stage)

### Stage 1 — 정책 + 마이그레이션 (≈1.5일)

- `migrations/0028_audit_rotation.up.sql` (+ down).
- `internal/audit/rotation_policy.go` — RotationPolicy struct (period / row_threshold / retention).
- 정책 검증 unit test.
- **출력**: 테이블 + 정책 type만. 아직 rotation 미수행.

### Stage 2 — cold archive 생성 + sha256 검증 (≈2일)

- `internal/audit/rotation.go` — `Plan()` / `Execute()` / `Verify()`.
- NDJSON canonical serializer.
- in-memory sha256 round-trip 테스트 (S3 없이).
- `audit.rotate.start` / `audit.rotate.complete` entry 기록.

### Stage 3 — S3 upload + 서명 (≈2일)

- `internal/audit/cold_storage.go` — interface ColdStorage { Put, Get, Head, Delete }.
- S3 backend (aws-sdk-go-v2) + File backend (NFS / 로컬).
- cosign keyless 서명 (Sigstore) — release pipeline의 기존 cosign 재사용.
- testcontainers minio + sigstore mock.

### Stage 4 — verify CLI 확장 (≈1.5일)

- `cmd/rosshield-audit-verify` `--cold-cache` / `--signing-bundle` flag.
- streaming download + sha256 + signature 검증.
- hot range + cold range 결합 chain reconstruction.
- exit code 표준화.

### Stage 5 — cron 자동 rotation + cross-witness fold-in + 운영 docs (≈2일)

- `internal/scheduler/audit_rotation_job.go` — leader_epoch 확인 → 월 1회.
- cross-witness (A-1) payload 확장.
- `docs/operations/audit-rotation.md` — 운영자용 (rotation 모니터링, S3 cost, 복구 절차).

### Stage 6 — customer pilot + tuning (≈2~3주 운영 데이터 후)

- 첫 paying customer 1년+ 운영 시 row 분포 측정.
- rotation 주기 조정, hot retention 조정.
- failure mode 보고서.

**총 추정**: 9~10 영업일 (Stage 1~5) + Stage 6 pilot.

---

## 7. 결정 항목 (권장 default)

| ID | 결정 | 옵션 | 권장 default | 근거 |
|---|---|---|---|---|
| **D-AR-1** | rotation 주기 trigger | (a) 월 1회 cron / (b) 분기 1회 / (c) row count 10M / (d) hybrid (a)+(c) | **(d) hybrid** — 월 1회 cron + safety net (row count 10M 도달 시 즉시) | 예측 가능 + 폭증 방어 |
| **D-AR-2** | cold storage backend | (a) S3 (AWS) / (b) GCS / (c) Azure Blob / (d) S3 API 호환 (MinIO·Wasabi) / (e) NFS·로컬 디렉토리 | **(a) S3 + (e) NFS fallback** — driver interface 동일 | 표준 + airgap 친화 |
| **D-AR-3** | hot retention (rotation 후 GC) | (a) 1년 / (b) 3개월 / (c) 1개월 / (d) tenant 설정 | **(a) 1년 default + (d) tenant override** | 감사인 즉시 access 기간 |
| **D-AR-4** | cold archive 서명 | (a) cosign keyless (Sigstore) / (b) 자체 Ed25519 / (c) 둘 다 | **(a) cosign keyless** — release pipeline 일관성 + airgap은 자체 Ed25519 fallback | 외부 검증 표준 |
| **D-AR-5** | verify CLI 모드 | (a) download → in-process verify / (b) streaming verify / (c) 둘 다 (사용자 선택) | **(c) 둘 다** — 기본 (b) streaming, `--cache` flag로 (a) | 대용량 segment 대응 |
| **D-AR-6** | segment 단위 | (a) 시간(월) / (b) row count / (c) seq range / (d) (a)+(c) tuple | **(d) (occurred_at_to, seq_to) tuple** — 시간 기반 cut + seq 명시 | 양쪽 쿼리 효율 |
| **D-AR-7** | tenant 격리 vs cluster-wide rotation | (a) 테넌트별 독립 cron / (b) 클러스터 1회 + 테넌트 순회 | **(b) 클러스터 1회 + 테넌트 순회 (advisory lock per tenant)** | 운영 단순 |
| **D-AR-8** | rotation 작업의 cross-witness fold-in | (a) hot head만 fold-in (기존) / (b) hot head + rotation head 둘 다 | **(b) 둘 다** | A-1 정합성 |
| **D-AR-9** | S3 dependency 라이선스 영향 | (a) 코어 Apache-2.0 포함 / (b) cold storage 전체 BSL 1.1 enterprise / (c) S3 backend만 BSL | **(c) S3 backend만 BSL 1.1, file backend는 Apache-2.0** | open-core 정합 |
| **D-AR-10** | rotation 실패 시 alerting | (a) audit chain verify 실패 경보 재활용 / (b) 신규 경보 트리거 | **(b) 신규 경보 `audit_rotation_failed`** | 트리거 분리 명확 |

**권장 default 수 = 10개**.

사용자 합의 항목 (Phase 8 진입 시 확정):
- D-AR-1, D-AR-3, D-AR-4 는 customer 요구에 따라 달라질 수 있음 → backlog로.

---

## 8. 변경 사항 outline (~1200줄 추정)

| 영역 | 파일 | 줄 추정 |
|---|---|---|
| migration | `migrations/0028_audit_rotation.up.sql` + `.down.sql` | ~60 |
| domain | `internal/audit/rotation_policy.go` | ~80 |
| domain | `internal/audit/rotation.go` (Plan / Execute / Verify) | ~180 |
| infra | `internal/audit/cold_storage.go` (interface) | ~40 |
| infra | `internal/audit/cold_storage_s3.go` (aws-sdk-go-v2) | ~150 |
| infra | `internal/audit/cold_storage_file.go` | ~70 |
| infra | `internal/audit/signing_cosign.go` | ~100 |
| infra | `internal/audit/signing_ed25519.go` | ~60 |
| scheduler | `internal/scheduler/audit_rotation_job.go` | ~100 |
| cross-witness | `internal/enterprise/crosswitness/rotation_foldin.go` | ~80 |
| CLI | `cmd/rosshield-audit-verify/cold.go` | ~120 |
| CLI | `cmd/rosshield-audit-verify/main.go` flag 통합 | ~30 |
| tests | `internal/audit/rotation_test.go` | ~250 |
| tests | `internal/audit/cold_storage_s3_test.go` (testcontainers minio) | ~180 |
| tests | `cmd/rosshield-audit-verify/cold_test.go` | ~120 |
| docs | `docs/operations/audit-rotation.md` (운영자용) | ~300 |
| docs | `docs/design/10-audit-and-observability.md` §10.22 신규 (rotation 절) | ~80 |
| **합계** | | **~2000줄** |

기존 추정 (~1200줄)은 test/docs 미포함. 실제 commit volume ~2000줄, **개발 9~10 영업일 + pilot 2~3주**.

---

## 9. 검증

### 9.1 단위

- rotation plan/execute/verify (Stage 2/3 테스트).
- chain link 재계산 (segment 내, segment 간).
- signature 검증 (cosign + Ed25519).

### 9.2 통합 (testcontainers)

- PG + minio (S3 호환).
- 100 entry rotation → cold → verify PASS.
- 1000 entry × 10 segment → hot + cold 결합 verify (latency 측정).
- 변조 (segment 1 byte flip / metadata flip / signature swap) → 모두 FAIL.

### 9.3 부하 / latency

- 1M entry × 1 segment → archive 생성 시간 + S3 upload 시간 측정.
- 12 segment × 1M entry verify latency 측정 (목표 ≤ 1분).
- 동시 rotation 5 tenant → advisory lock 정상 작동.

### 9.4 chaos / failure

- S3 upload mid-fail → 재시도 idempotent.
- DB rotation record INSERT 직전 crash → 재시도 시 segment 중복 생성 없음 (sha256 비교).
- leader_epoch 변동 (non-leader가 rotation 시도) → 거부.

---

## 10. 비즈니스 / 라이선스 영향

- **코어 (Apache-2.0)**: file backend, rotation domain logic, Ed25519 서명.
- **enterprise (BSL 1.1)**: S3 backend, cosign keyless 통합, cross-witness fold-in 확장 (이미 enterprise).
- 새 외부 의존:
  - `aws-sdk-go-v2/service/s3` (Apache-2.0) — license 호환.
  - `github.com/sigstore/cosign/v2` (Apache-2.0) — license 호환.
- pricing: cold storage cost는 customer S3 계정 사용 → Lodestar는 storage cost 부담 없음.
- 가치 제안: "1년+ audit 무결성, 검증 1분 내" — paying enterprise customer pitch에 직접 반영.

---

## 11. 리스크

| # | 리스크 | 영향 | 완화 |
|---|---|---|---|
| R1 | rotation 도중 신규 entry 처리 race | chain 단절 가능 | advisory lock + cut point는 rotation 시작 시 snapshot, 그 이후 entry는 다음 segment로 |
| R2 | S3 endpoint 부재 customer | 기능 사용 불가 | file backend (NFS / 로컬 디렉토리) — Stage 3에 동시 구현 |
| R3 | verify CLI cold download bandwidth | 감사인 검증 시간 증가 | streaming 모드 (D-AR-5) + segment 단위 parallel download |
| R4 | cosign keyless airgap 미지원 | 서명 검증 불가 | airgap profile은 Ed25519 fallback (D-AR-4 자체 키) |
| R5 | rotation 자체 audit entry로 인한 chain 변경 | 기존 verify CLI 호환 깨짐 | rotation entry는 표준 audit entry — 기존 verify 호환 (단 verify CLI가 archive_hash payload 인식 추가) |
| R6 | leader_epoch 변경 중 rotation in-progress | rotation 미완료 + 새 leader가 재시작 시 중복 | advisory lock + audit_rotations UNIQUE(tenant_id, segment_no) + sha256 비교 idempotency |
| R7 | cold archive deleted 후 verify 요청 | 가용성 손실 (무결성은 hot에 archive_sha256 reference로 탐지) | S3 versioning + Object Lock (compliance mode) 권장 운영 가이드 |
| R8 | cross-witness (A-1) rotation fold-in 미동기화 | 분산 증명 일관성 깨짐 | rotation entry는 cross-witness fold-in queue에 동기 push |
| R9 | tenant 폭증 (100→1000) 시 cron 시간 부족 | rotation lag | tenant 단위 parallel rotation worker pool (Stage 6 tuning) |
| R10 | `audit_rotations` 자체가 unbounded growth | 메타 테이블 비대 | segment_no는 BIGINT, 100 tenant × 월 1 × 100년 = 120k row → 충분히 작음 |

---

## 12. 결정 로그

| 일자 | 결정 | 근거 |
|---|---|---|
| 2026-05-19 | 본 design doc 채택 (옵션 A — checkpoint 분할 + S3 + NFS fallback) | 단순 + airgap 친화 + 외부 검증 표준 + cross-witness 정합 |
| 2026-05-19 | hot retention default 1년 | 감사인 즉시 access 기간 + DB cap 100GB/tenant100 |
| 2026-05-19 | cold archive 서명 cosign keyless 우선, airgap Ed25519 fallback | 기존 release pipeline 일관성 |
| 2026-05-19 | rotation은 항상 audit chain의 정상 link (`audit.rotate.complete` entry) | 무결성 보장 + 외부 검증 누락 없음 |
| 2026-05-19 | open-core 분할: file backend Apache-2.0 / S3 backend BSL 1.1 | 라이선스 정합 + airgap customer 무료 동작 |

---

### 부록 A — rotation 절차 ASCII diagram

```
hot chain                                    cold storage (S3 / NFS)
─────────                                    ────────────────────────
  ε ─ e1 ─ e2 ─ ... ─ e(N) ─ ROTATE-CMPLT ─ e(N+1) ─ ...
                                │
                                ▼
                   audit_rotations row #1
                   ├─ segment_no = 1
                   ├─ seq_from = 1, seq_to = N
                   ├─ archive_sha256 = 0xabc...
                   ├─ archive_uri = s3://.../seg-001.ndjson
                   └─ signature (cosign)

       다음 회차:
  ROTATE-CMPLT ─ e(N+1) ─ ... ─ e(M) ─ ROTATE-CMPLT ─ ...
                                          │
                                          ▼
                              audit_rotations row #2
                              ├─ segment_no = 2
                              ├─ seq_from = N+1, seq_to = M
                              ├─ prev_segment_hash = 0xabc...
                              └─ ...
```

### 부록 B — verify CLI 흐름 (hot + cold 결합)

```
$ rosshield-audit-verify \
    --hot-endpoint https://lodestar.example.com:8443 \
    --token vt_XXX --tenant tn_001 \
    --from 2025-01-01 --to 2026-05-19 \
    --signing-bundle ./trusted/

[1/5] Fetching audit_rotations metadata...
      → 12 cold segments + 1 hot range
[2/5] Downloading segment 1/12 (12.4 MB)... OK (sha256 match)
      Signature verify (cosign) ... OK
      Chain reconstruction (3.65M entries) ... OK (0.84s)
[...]
[3/5] Verifying hot range (seq 36500001..36789012)... OK
[4/5] Cross-segment links ... OK
[5/5] Cross-witness fold-in (A-1) ... 3/3 witnesses agree
RESULT: OK (full chain integrity verified in 47.2s)
```

---

**문서 끝**. 코드 진입 전 사용자 합의: D-AR-1, D-AR-3, D-AR-4.
