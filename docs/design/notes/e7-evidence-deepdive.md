# E7 Evidence Store 설계 심화 - nrobotcheck v2 패턴 분석

_작성일: 2026-04-29_
_대상: rosshield E7 Evidence Store 구현 설계_

---

## 1. 결론 (한 줄)

**nrobotcheck의 Evidence Store는 완성된 패턴 제공**: content-addressed blob deduplication (SHA256) + ref counting 기반 GC + redaction-at-write + N:M evidence_refs 매핑을 모두 구현했으므로, 전신 패턴 **직접 차용 권장** (단, 멀티테넌트·blob 저장소 확장 필요).

---

## 2. 발견된 자산

### 2.1 핵심 구현 파일

#### EvidenceRepository (content-addressed blob store)
**경로**: /d/robot/dev/nrobotcheck/src/domains/evidence/repository/EvidenceRepository.ts

**핵심**: insert()는 (Evidence, rawContent)을 받아서 SHA256 계산 → 중복 확인 → blob 재사용 또는 신규 저장 → metadata 입력

`	ypescript
insert(evidence: Evidence, rawContent: string): Evidence {
  const contentHash = sha256Hex(rawContent)
  const full: Evidence = { ...evidence, contentHash }
  
  this.db.transaction(() => {
    const existing = this.db.prepare(
      'SELECT ref_count FROM evidence_blobs WHERE content_hash = ?'
    ).get(contentHash)
    
    if (existing) {
      this.db.prepare('UPDATE evidence_blobs SET ref_count = ref_count + 1 ...')
    } else {
      const buf = Buffer.from(rawContent, 'utf8')
      this.db.prepare(
        'INSERT INTO evidence_blobs (content_hash, content, compressed, size_bytes, first_seen_at, ref_count) VALUES (...)'
      ).run(contentHash, buf, buf.byteLength, now)
    }
    
    this.db.prepare('INSERT INTO evidence (...) VALUES (...)')
  })
  return full
}
`

**교훈**: atomicity (blob+metadata 동시 갱신), deduplication (sha256 기반), ref counting GC.

#### Evidence Type 정의
**경로**: /d/robot/dev/nrobotcheck/src/domains/evidence/types/Evidence.ts

`	ypescript
export interface EvidenceBase {
  readonly id: string                    // UUID (메타 row)
  readonly kind: EvidenceKind            // command_output|file_snapshot|log|...
  readonly robotId: string
  readonly sessionId?: string
  readonly checkId?: string
  readonly contentHash: string           // SHA256 (blob 참조)
  readonly redacted: boolean
  readonly redactionPolicy?: string      // 적용된 정책 이름 (쉼표 분리)
  readonly collectedAt: string
  readonly expiresAt?: string            // retention 만료
}
`

**패턴**: 메타 row (id)와 콘텐츠 (contentHash)를 분리, redaction 추적 필드 포함.

#### RedactionService (regex 기반 정책 엔진)
**경로**: /d/robot/dev/nrobotcheck/src/domains/evidence/services/RedactionService.ts

**내장 정책**:
- PEM private key: -----BEGIN.*PRIVATE KEY-----...-----END.*PRIVATE KEY----- (multiline, priority 100)
- AWS key: AKIA[0-9A-Z]{16} (priority 50)
- Bearer token: (?i)Bearer\s+[A-Za-z0-9._~+/=-]{16,} (priority 40)
- password=VALUE: (?i)\b(?:password|passwd|...) \s*=\s*\S+ (priority 30)

**특징**:
- 우선순위 정렬 (높은 것부터 실행)
- multiline flag로 regex 성능 제어
- applied[] 배열로 적용된 정책 추적
- Inline (?i) flag 지원

#### EvidenceCollector (파이프라인)
**경로**: /d/robot/dev/nrobotcheck/src/domains/evidence/services/EvidenceCollector.ts

Flow: raw input → 즉시 redaction → metadata 붙이기→ 저장 → 이벤트 발행

`	ypescript
async collectCommandOutput(input: CommandOutputInput) {
  const combined = \\\n---STDERR---\n\\
  const { content, applied } = this.opts.redactor.redact(combined)
  
  const evidence: CommandOutputEvidence = {
    id: crypto.randomUUID(),
    kind: 'command_output',
    redacted: applied.length > 0,
    redactionPolicy: applied.length > 0 ? applied.join(',') : undefined,
    expiresAt: this.computeExpiry(),
    ...
  }
  const saved = this.opts.repo.insert(evidence, content)
  await this.announce(saved)
  return saved
}
`

#### ScanEvidenceSink (N:M 매핑)
**경로**: /d/robot/dev/nrobotcheck/src/domains/scan/services/ScanEvidenceSink.ts

`	ypescript
async recordConditionOutput(scanResultId: string, input: CommandOutputInput) {
  const evidence = await this.opts.collector.collectCommandOutput(input)
  this.opts.repo.addRef(evidence.id, 'scan_result', scanResultId)
  return evidence.id
}
`

**설계**: scan_result ↔ evidence 양방향 링크, UNIQUE(evidence_id, ref_type, ref_id) 제약.

### 2.2 스키마 설계

**경로**: /d/robot/dev/nrobotcheck/src/main/platform/storage/migrations/003_evidence.ts

3-level 구조:
- evidence_blobs: 실제 콘텐츠 (BLOB), ref_count로 GC
- evidence: 메타 (UUID, kind, robot_id, ..., content_hash FK)
- evidence_refs: 역참조 (evidence_id, ref_type='scan_result'|'insight'|..., ref_id)
- redaction_policies: 정책 저장소

**인덱스**:
- idx_evidence_blobs_unreferenced: WHERE ref_count=0 (GC 빠르게)
- idx_evidence_* : robot, session, check, kind, collected_at, expires_at
- idx_evidence_refs_* : evidence_id, (ref_type, ref_id)

### 2.3 테스트 커버리지

**EvidenceRepository.test.ts**:
- blob deduplication (same content → 1 blob, ref_count++)
- different content → separate blobs
- delete() decrements ref_count, GC removes at 0
- purgeExpired() + gcUnreferencedBlobs() coordination

**RedactionService.test.ts**:
- PEM key blocking (multiline)
- AWS key, Bearer token, password=VALUE
- Priority ordering
- Disabled policies skipped
- applied[] list (no duplicates)

---

## 3. 함정 및 교훈

### 3.1 발견된 이슈

**(1) blob 저장소 제약**
- SQLite BLOB은 한 파일에 모든 blob 저장 → DB 비대화 위험
- 스케일: 수천 로봇 × 월별 스캔 → GB~TB 수준
- 압축 필드(compressed=0) 예약하나 미사용

**rosshield**: 멀티테넌트 SaaS → S3/MinIO + 해시 인덱스 필요.

**(2) Redaction 신뢰도**
- password=VALUE 규칙: \S+ → "password=foo bar" (공백 있는 값) 누락 위험
- YAML/JSON multiline 값, Base64 인코딩 비밀 미처리

**대책**: 커스텀 정책 추가 UI, 테스트 자동화, 2차 LLM 검증.

**(3) Retention 정책 단순**
- retentionDays 전역만 지원 (GDPR/PCI-DSS별 다름, kind별 다름)

**fleetguard**: per-tenant + per-kind 확장 계획.

**(4) N:M 매핑의 "왜"가 부족**
- evidence_refs는 "무엇이 무엇에 쓰였는가"만 기록
- confidence, reasonType 등 추가 메타 필요

### 3.2 아키텍처 강점

**(1) Event-driven**
`
evidence:collected → Insight 파이프라인 / LLM 분석 / Compliance 감시 (독립적)
`
느슨한 결합, 유지보수 용이.

**(2) Idempotency**
INSERT OR IGNORE + UNIQUE → 재입력 안전 (멀티 리트라이).

**(3) 트랜잭션 원자성**
blob 신규/재사용, evidence 메타, ref_count 갱신이 all-or-nothing.

---

## 4. rosshield 권장

### 4.1 직접 차용

| 항목 | 이유 |
|---|---|
| SHA256 content addressing | 증명된 중복제거 |
| ref counting GC | 간단하고 효과적 |
| Evidence type enum | 확장 가능한 설계 |
| Redaction service 아키텍처 | 정책 엔진 + 우선순위 |
| RedactionPolicy.multiline | regex 대재앙 회피 |
| EvidenceCollector 파이프라인 | redact → store → announce |
| Event-driven evidence_refs | 도메인 간 느슨한 결합 |

### 4.2 재설계

| 항목 | 변경 | 사유 |
|---|---|---|
| blob 저장소 | S3/MinIO + local | 멀티테넌트, 대규모 |
| Retention | per-tenant + per-kind | 규제, 테넌트 SLA |
| 정책 저장 | 테넌트별 + 버전 관리 | 감사, 롤백 |
| evidence_refs | confidence, reasonType | 설명 가능성 (P11) |
| Compression | zstd 또는 gzip | bandwidth 절감 |

### 4.3 구현 로드맵

- **E7a (2주)**: 코어 (EvidenceRecord, SHA256, GC, RedactionService)
- **E7b (2주)**: 멀티테넌트 (BlobStore IF, per-tenant retention)
- **E7c (1주)**: 설명 가능성 (evidence_refs 확장, Audit trail)

**총 5주 예상** (test 포함).

---

## 5. 핵심 결론 5줄

1. **nrobotcheck의 Evidence Store는 완성형**: SHA256 + ref counting GC + redaction-at-write + N:M refs.

2. **blob 저장소는 재설계 필수**: SQLite BLOB → S3/MinIO + local fallback (멀티테넌트).

3. **Redaction은 정책에 의존**: regex 한계 (multiline 값), 커스텀 정책 + 2차 LLM 검증 필수.

4. **Event-driven 아키텍처 차용 권장**: evidence:collected로 느슨한 결합, 추후 도메인 통합 용이.

5. **E7 기간**: 코어 2주 + 멀티테넌트 2주 + 설명 가능성 1주 = **5주**.
