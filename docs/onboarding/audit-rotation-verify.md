# Audit Rotation 외부 검증 가이드

> **대상**: 외부 감사인 또는 customer 보안팀. rotated audit segment archive의 무결성 검증.
> **도구**: `rosshield-audit-verify` (E30, Stage 5 확장). stdlib + 도메인 hash 함수만 의존 — release page에서 단일 binary로 받아 사용.
> **선행**: customer 환경에서 `audit_rotation_segments` 테이블 + cold archive (tar.gz)가 만들어진 상태.

---

## 1. 배경

`audit_entries` 테이블은 append-only chain이지만 1년+ 운영 시 row 수가 수억에 도달합니다. Lodestar는 segment 단위 rotation을 통해 오래된 entry를 cold archive (tar.gz)로 분리하고, hot DB에는 segment 메타데이터(hash·archive sha256·prev_segment_hash)만 유지합니다.

**Stage 5 chain link** — segment N+1의 `prev_segment_hash` column에 segment N의 `segment_hash`를 기록함으로써 cold 영역까지 chain 무결성을 확장합니다. 외부 감사인은 archive 들을 순서대로 다운로드 받아 chain 일관성을 단독 검증할 수 있습니다.

---

## 2. archive 구조

`seg-NNNNNN.tar.gz`:

```
manifest.json     ← 메타 (version "2", segment_hash, prev_segment_hash, ranges, ...)
entries.ndjson    ← entry 한 줄씩 (audit.MarshalEntryLine 형식)
```

`manifest.json` 예 (v2):

```json
{
  "version": "2",
  "tenantId": "tn_acme",
  "firstEntryId": 1001,
  "lastEntryId": 2000,
  "entryCount": 1000,
  "startedAt": "2026-04-01T00:00:00Z",
  "endedAt": "2026-04-30T23:59:59.999999999Z",
  "segmentHash": "abc...64hex",
  "prevSegmentHash": "def...64hex",
  "entriesFile": "entries.ndjson",
  "createdAt": "2026-05-01T00:00:00Z"
}
```

`prevSegmentHash`는 segment_number=1인 첫 segment에서는 생략(omitempty)됩니다.

---

## 3. 단일 archive 검증

```bash
rosshield-audit-verify rotation \
  --archive-uri file:///var/rosshield/audit-archives/tn_acme/seg-000005.tar.gz \
  --expected-sha256       abc123...  \  # 외부 채널로 받은 archive sha256 (옵션)
  --expected-segment-hash def456...  \  # 외부 채널로 받은 segment hash (옵션)
  --prev-segment-hash     xyz789...     # 직전 segment_hash (옵션, segment 1 검증 시 생략)
```

검증 절차:

1. **fetch** — `file://` URI에서 archive bytes 로딩.
2. **archiveSha256** — `sha256(body)` == `--expected-sha256` (옵션).
3. **extract** — tar.gz 풀어서 `manifest.json` + `entries.ndjson` 추출.
4. **segmentHash** — `entries.ndjson` 파싱 후 `ComputeSegmentHash`로 재계산 → `manifest.segmentHash`와 비교 (필수). 추가로 `--expected-segment-hash`와도 비교 (옵션).
5. **prevChain** — `manifest.prevSegmentHash` == `--prev-segment-hash` (옵션).

`--expected-*` 미지정 시 해당 step은 자동 PASS 처리되지만 step 결과는 그대로 노출됩니다. 외부 채널의 expected 값과 비교하면 변조 탐지 강도가 올라갑니다.

### exit code

| code | 의미 |
|------|------|
| 0 | PASS — 모든 단계 통과 |
| 1 | FAIL — sha256·segment_hash·prev_chain mismatch 또는 archive 파싱 실패 |
| 2 | ARG  — invalid CLI args |

### JSON 출력

```bash
rosshield-audit-verify rotation \
  --archive-uri file:///path/seg-000005.tar.gz \
  --format json
```

```json
{
  "ok": true,
  "result": "PASS",
  "archiveUri": "file:///path/seg-000005.tar.gz",
  "archiveSha256Match": true,
  "archiveSha256": "abc...",
  "segmentHashMatch": true,
  "segmentHash": "def...",
  "prevChainMatch": true,
  "prevSegmentHash": "xyz...",
  "entryCount": 1000,
  "manifestVersion": "2",
  "steps": [
    {"name":"fetch","ok":true,"detail":"5421 bytes"},
    {"name":"archiveSha256","ok":true,"detail":"matches expected"},
    {"name":"extract","ok":true,"detail":"manifest v2, 1000 entries"},
    {"name":"segmentHash","ok":true,"detail":"recomputed == manifest"},
    {"name":"prevChain","ok":true,"detail":"matches expected prev_segment_hash"}
  ]
}
```

---

## 4. Chain batch 검증

여러 segment의 chain 일관성을 한 번에 검증:

```bash
rosshield-audit-verify rotation chain \
  --backend file:///var/rosshield/audit-archives/tn_acme/ \
  --from-segment 1 \
  --to-segment   12
```

검증 절차 (segment 1 → N 순서):

1. segment N의 archive fetch + sha256 + segment_hash 재계산.
2. segment N의 `manifest.prevSegmentHash` == 직전 segment(N-1)의 `manifest.segmentHash` (자동 forward).
3. 어느 한 step이라도 실패하면 즉시 FAIL + `firstFailure: <N>`.

`--from-segment > 1` 로 시작할 경우 시작 segment의 `prev_segment_hash`는 self-consistent check만 수행 (외부 expected 비교는 single mode로).

### 출력 (JSON)

```json
{
  "ok": true,
  "result": "PASS",
  "fromSegment": 1,
  "toSegment": 12,
  "backend": "file:///var/rosshield/audit-archives/tn_acme/",
  "verified": 12,
  "segments": [
    {"segmentNumber":1,"ok":true,"segmentHash":"...","prevSegmentHash":""},
    {"segmentNumber":2,"ok":true,"segmentHash":"...","prevSegmentHash":"..."},
    ...
  ]
}
```

---

## 5. 외부 감사 시나리오

### 5.1 customer 환경 외부 검증 (가장 흔한 케이스)

1. 감사인이 release page에서 `rosshield-audit-verify` binary 다운로드.
2. customer 보안팀이 cold archive (tar.gz) 들을 zip으로 전달 (sneakernet 또는 SFTP).
3. customer가 별도 channel(e.g. PDF 리포트)로 각 segment의 `expected-sha256` + `expected-segment-hash` 공유.
4. 감사인이 위 `chain` 모드로 chain 일관성 검증 + 핵심 segment 들을 single 모드로 expected 값 cross-check.

### 5.2 chain 일관성만 검증 (in-house audit)

customer 본인 환경에서 cron 또는 release 게이트에 chain batch verify를 통합 — `expected-*` 없이도 self-consistent chain check 만으로 archive 변조 탐지 가능 (chain link가 깨지면 즉시 FAIL).

---

## 6. 한계

- **scheme**: 현재 `file://` 만 지원. `s3://` 등 cloud backend는 BSL backend module 의존이라 별 epic (rotation backend interface는 stub 상태).
- **expected-sha256 부재 시**: archive sha256 자체는 manifest에 포함되지 않으므로 expected 미지정 시 단순 self-computation만 수행. 외부 채널 expected 값이 변조 탐지에 핵심.
- **segment_hash 자체 변조**: archive 안 manifest와 entries를 함께 변조하면 segment_hash 재계산 일관성이 통과될 수 있음 — 그러나 `prev_segment_hash` chain이 다음 segment에서 깨지므로 chain batch verify로 탐지.
- **archive 가용성**: cold storage가 삭제·손실되면 verify 불가 (가용성 ≠ 무결성). hot DB의 `archive_uri` reference는 유지되어 archive 누락 사실 자체는 audit chain에 기록됨.

---

## 7. 참조

- design: [`docs/design/notes/audit-chain-rotation-design.md`](../design/notes/audit-chain-rotation-design.md) Stage 5
- core code: `internal/domain/audit/rotation/` (`builder.go` · `archiver.go` · `rotation.go`)
- CLI: `cmd/rosshield-audit-verify/rotation.go`
- migration: `internal/platform/storage/postgres/migrations/0035_audit_rotation_chain.up.sql` + sqlite 동등
