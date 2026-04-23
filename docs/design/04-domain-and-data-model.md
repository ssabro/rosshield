# 04. 도메인 및 데이터 모델

## 4.1 모델 개요 (ERD 요약)

```
Tenant ──┬─ User ── Role ── Permission
         ├─ ApiKey
         ├─ Fleet ── FleetPolicy
         │            └── Robot ── Credential
         │                      └── PeerGroup
         ├─ BenchmarkPack ── CheckDefinition ── ControlMapping
         ├─ ScanSession ── ScanResult ── EvidenceRef
         │                            └── Insight
         ├─ ComplianceProfile ── FrameworkSnapshot ── ControlStatus
         ├─ Report ── ReportSignature
         └─ AuditEntry (해시 체인)
```

모든 루트 엔터티는 `tenant_id`를 가집니다 (P4 원칙).

## 4.2 핵심 타입 정의

> 이 문서에서는 언어 중립적으로 표기합니다. 실제 구현은 `11-tech-stack-and-roadmap.md`에서 선택된 언어(Go 또는 TypeScript)의 타입 시스템으로 표현됩니다.

### Tenant

```
Tenant {
  id: TenantId
  name: string
  plan: 'desktop_free' | 'desktop_pro' | 'enterprise' | 'appliance'
  createdAt: timestamp
  settings: TenantSettings
  features: FeatureFlags       // 플랜·라이선스별 기능 활성 상태
  retentionPolicy: RetentionPolicy
}
```

### User · Role · Permission

```
User {
  id: UserId
  tenantId: TenantId
  email: string
  displayName: string
  authProvider: 'local' | 'oidc' | 'saml' | 'os'
  externalSubject?: string      // SSO에서의 subject ID
  status: 'active' | 'invited' | 'suspended'
  roles: RoleId[]
  createdAt, updatedAt: timestamp
}

Role {
  id: RoleId
  tenantId: TenantId
  name: string                   // 'admin' | 'auditor' | 'operator' | custom
  permissions: Permission[]
  system: boolean                // 시스템 기본 역할 여부
}

Permission = 
  | 'robot.read'   | 'robot.write'
  | 'scan.read'    | 'scan.execute' | 'scan.cancel'
  | 'bench.read'   | 'bench.install'
  | 'report.read'  | 'report.sign'
  | 'compliance.read'
  | 'audit.read'   | 'audit.export'
  | 'admin.user'   | 'admin.tenant'
  | 'plugin.install'
```

### ApiKey

```
ApiKey {
  id: ApiKeyId
  tenantId: TenantId
  name: string
  prefix: string           // 토큰 앞 8자 (표시용)
  hashed: string           // bcrypt/argon2
  scopes: Permission[]
  expiresAt?: timestamp
  lastUsedAt?: timestamp
  createdBy: UserId
  revokedAt?: timestamp
}
```

### Fleet · Robot · Credential

```
Fleet {
  id: FleetId
  tenantId: TenantId
  name: string
  description?: string
  policy: FleetPolicy          // 기본 baseline·criticality·스캔 주기 등
}

Robot {
  id: RobotId
  tenantId: TenantId
  fleetId: FleetId
  name: string
  host: string
  port: number
  authType: 'password' | 'privateKey' | 'agent'
  credentialId: CredentialId
  osDistro: 'ubuntu-24.04' | 'ubuntu-22.04' | string
  rosDistro: 'jazzy' | 'humble' | string
  peerGroupId?: PeerGroupId
  tags: string[]
  role?: string              // 'mobile' | 'manipulator' | custom
  criticality: 'low' | 'medium' | 'high' | 'critical'
  addedAt, updatedAt: timestamp
  lastScanAt?: timestamp
}

Credential {
  id: CredentialId
  tenantId: TenantId
  type: 'password' | 'privateKey'
  // 암호화된 material (KEK/DEK로 wrap)
  encryptedPayload: bytes
  encryptionMeta: EncryptionMeta
  rotationDueAt?: timestamp
  createdAt: timestamp
}
```

### Benchmark Pack

```
BenchmarkPack {
  id: PackId
  tenantId: TenantId | 'system'   // 시스템 팩은 전 테넌트 공유
  name: string                     // 'cis-ubuntu-24.04'
  version: semver
  vendor: string                   // 'CIS' | 'NIST' | 'Fleet내부' ...
  signature: PackSignature
  installedAt: timestamp
  checks: CheckDefinition[]
  metadata: PackMetadata
}

CheckDefinition {
  id: CheckId
  packId: PackId
  code: string                     // 'CIS-1.1.1.1'
  title: { ko: string; en: string }
  description: { ko: string; en: string }
  severity: 'low' | 'medium' | 'high' | 'critical'
  required: boolean
  automated: boolean
  levels: ('L1' | 'L2')[]
  auditCommand?: AuditCommand      // SSH로 실행할 명령
  evaluationRule: EvaluationRule   // stdout/exitcode → pass/fail
  remediationCommand?: RemediationCommand
  remediationDescription?: { ko: string; en: string }
  references: Reference[]
  controlMappings: ControlMapping[]
}
```

### Scan · Result · Evidence

```
ScanSession {
  id: ScanSessionId
  tenantId: TenantId
  fleetId?: FleetId            // 플릿 단위 or 단일 로봇
  robotIds: RobotId[]
  packId: PackId
  packVersion: semver
  level: 'L1' | 'L2'
  trigger: 'manual' | 'schedule' | 'api'
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  startedAt, completedAt?: timestamp
  summary: ScanSummary
  createdBy: UserId | 'system'
}

ScanResult {
  id: ScanResultId
  sessionId: ScanSessionId
  robotId: RobotId
  checkId: CheckId
  outcome: 'pass' | 'fail' | 'error' | 'not_applicable' | 'manual'
  evidenceRefs: EvidenceRecordId[]
  evaluationTrace: EvaluationTrace  // 어떤 규칙·조건이 적용되었는지
  startedAt, completedAt: timestamp
  durationMs: number
}

EvidenceRecord {
  id: EvidenceRecordId
  tenantId: TenantId
  sha256: string                   // 컨텐츠 해시(중복제거)
  contentType: 'stdout' | 'stderr' | 'file' | 'config-snapshot' | 'screenshot'
  sizeBytes: number
  blobLocator: BlobLocator         // 저장 위치
  redactions: RedactionMark[]      // 민감 정보 마스킹 기록
  createdAt: timestamp
  // 누가·어떤 세션에서 생성했는지는 역참조 테이블로 관리
}
```

### Insight

```
Insight {
  id: InsightId
  tenantId: TenantId
  kind: 'drift' | 'anomaly' | 'root_cause' | 'attack_path' | 'prediction'
  scope: { robotId?: RobotId; fleetId?: FleetId; checkId?: CheckId }
  severity: 'info' | 'low' | 'medium' | 'high' | 'critical'
  summary: string
  reasoning: string                // P11: 설명 가능성
  evidenceRefs: EvidenceRecordId[]
  rulesApplied: string[]
  confidence: number               // 0.0 - 1.0
  producedBy: 'rules' | 'llm' | 'hybrid'
  llmTrace?: LlmTrace
  createdAt: timestamp
  dismissedAt?: timestamp
  dismissedBy?: UserId
}
```

### Compliance

```
ComplianceProfile {
  id: ProfileId
  tenantId: TenantId
  framework: 'iso27001' | 'nist-800-53' | 'cis' | 'iec-62443' | 'isms-p' | string
  version: string                   // 프레임워크 버전
  enabled: boolean
  customizations: ControlCustomization[]
}

FrameworkSnapshot {
  id: SnapshotId
  tenantId: TenantId
  profileId: ProfileId
  sessionId: ScanSessionId
  controlStatuses: ControlStatus[]
  overallScore: number             // 0.0 - 1.0
  createdAt: timestamp
}

ControlStatus {
  controlId: string                // 'ISO27001:A.5.1'
  status: 'pass' | 'fail' | 'partial' | 'not_applicable' | 'unmapped'
  evidenceRefs: EvidenceRecordId[]
  mappedChecks: CheckId[]
  notes?: string
}
```

### Report

```
Report {
  id: ReportId
  tenantId: TenantId
  templateId: TemplateId
  scope: ReportScope              // session · fleet · tenant
  format: 'markdown' | 'pdf' | 'html' | 'xlsx'
  contentBlob: BlobLocator
  signature: ReportSignature       // 서명된 PDF의 경우
  generatedAt: timestamp
  generatedBy: UserId | 'system'
}

ReportSignature {
  algorithm: 'ed25519' | 'ecdsa-p256'
  signerKeyId: string             // 기기/조직 키 ID
  signature: bytes
  signedAt: timestamp
  chainHeadHash: string           // 서명 당시 audit chain의 head (교차 검증)
}
```

### Audit (해시 체인)

```
AuditEntry {
  seq: number                      // 테넌트 내 단조 증가
  tenantId: TenantId
  occurredAt: timestamp
  actor: { type: 'user'|'api'|'system'; id: string }
  action: string                   // 'scan.execute'·'robot.create' 등
  target: { type: string; id: string }
  payloadDigest: string            // 요청·변경의 해시 요약
  prevHash: string                 // 이전 엔트리 hash (genesis는 zeros)
  hash: string                     // sha256(prevHash || payloadDigest || meta)
}

ChainHead {
  tenantId: TenantId
  seq: number
  hash: string
  signedHash?: bytes               // 기기 키로 주기 서명 (checkpoint)
  updatedAt: timestamp
}
```

**외부 검증**: 누구나 `/api/v1/audit/verify` 엔드포인트로 `seq` 구간을 가져와 체인을 재계산하여 `ChainHead`와 대조 가능합니다.

## 4.3 SQL 스키마 (요약)

> 아래는 PostgreSQL 문법 기준. SQLite용 변형은 `ARRAY`·`JSONB`·`GENERATED` 대응만 차이.

```sql
CREATE TABLE tenants (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  plan          TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  settings      JSONB NOT NULL DEFAULT '{}',
  features      JSONB NOT NULL DEFAULT '{}',
  retention     JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE users (
  id              TEXT PRIMARY KEY,
  tenant_id       TEXT NOT NULL REFERENCES tenants(id),
  email           TEXT NOT NULL,
  display_name    TEXT,
  auth_provider   TEXT NOT NULL,
  external_subject TEXT,
  status          TEXT NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, email),
  UNIQUE (auth_provider, external_subject)
);

CREATE TABLE robots (
  id              TEXT PRIMARY KEY,
  tenant_id       TEXT NOT NULL REFERENCES tenants(id),
  fleet_id        TEXT NOT NULL,
  name            TEXT NOT NULL,
  host            TEXT NOT NULL,
  port            INTEGER NOT NULL DEFAULT 22,
  auth_type       TEXT NOT NULL,
  credential_id   TEXT NOT NULL,
  os_distro       TEXT,
  ros_distro      TEXT,
  peer_group_id   TEXT,
  tags            TEXT[] NOT NULL DEFAULT '{}',
  role            TEXT,
  criticality     TEXT NOT NULL DEFAULT 'medium',
  added_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_scan_at    TIMESTAMPTZ
);
CREATE INDEX robots_tenant_fleet ON robots(tenant_id, fleet_id);

-- ... (benchmark_packs, check_definitions, scan_sessions, scan_results,
--      evidence_records, insights, compliance_*, reports, audit_entries)
```

### 감사 체인 테이블

```sql
CREATE TABLE audit_entries (
  tenant_id        TEXT NOT NULL REFERENCES tenants(id),
  seq              BIGINT NOT NULL,
  occurred_at      TIMESTAMPTZ NOT NULL,
  actor_type       TEXT NOT NULL,
  actor_id         TEXT NOT NULL,
  action           TEXT NOT NULL,
  target_type      TEXT NOT NULL,
  target_id        TEXT NOT NULL,
  payload_digest   TEXT NOT NULL,
  prev_hash        TEXT NOT NULL,
  hash             TEXT NOT NULL,
  PRIMARY KEY (tenant_id, seq)
);

-- 절대 UPDATE/DELETE 금지를 DB 레벨로 강제
CREATE RULE audit_entries_no_update AS ON UPDATE TO audit_entries DO INSTEAD NOTHING;
CREATE RULE audit_entries_no_delete AS ON DELETE TO audit_entries DO INSTEAD NOTHING;
```

SQLite에는 `RULE`이 없으므로 트리거로 동일 효과:

```sql
CREATE TRIGGER audit_entries_no_update BEFORE UPDATE ON audit_entries
  BEGIN SELECT RAISE(ABORT, 'audit log is immutable'); END;
CREATE TRIGGER audit_entries_no_delete BEFORE DELETE ON audit_entries
  BEGIN SELECT RAISE(ABORT, 'audit log is immutable'); END;
```

## 4.4 ID 규칙

- **접두사 + base32 ulid/nanoid**: `tn_01H...`, `ro_01H...`, `ss_01H...`
- 접두사로 타입을 즉시 구분 (로그·UI 디버깅 이점).
- 테넌트 내 단조 증가(audit seq)는 별도 sequence 관리.

## 4.5 JSON Schema 예시 (ScanResult)

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://rosshield.dev/schemas/ScanResult.json",
  "type": "object",
  "required": ["id", "sessionId", "robotId", "checkId", "outcome",
               "evidenceRefs", "startedAt", "completedAt"],
  "properties": {
    "id":        { "type": "string", "pattern": "^sr_[A-Z0-9]+$" },
    "sessionId": { "type": "string", "pattern": "^ss_[A-Z0-9]+$" },
    "robotId":   { "type": "string", "pattern": "^ro_[A-Z0-9]+$" },
    "checkId":   { "type": "string" },
    "outcome":   { "enum": ["pass", "fail", "error", "not_applicable", "manual"] },
    "evidenceRefs": {
      "type": "array",
      "items": { "type": "string", "pattern": "^ev_[A-Z0-9]+$" }
    },
    "evaluationTrace": { "$ref": "./EvaluationTrace.json" },
    "startedAt":   { "type": "string", "format": "date-time" },
    "completedAt": { "type": "string", "format": "date-time" },
    "durationMs":  { "type": "integer", "minimum": 0 }
  }
}
```

모든 영속 객체는 **JSON Schema 정의**를 가지며 저장 시·API 응답 직렬화 시 검증됩니다.

## 4.6 마이그레이션 원칙

- 모든 스키마 변경은 **앞으로만**(additive) 나아갑니다.
- 컬럼 삭제는 **두 릴리스에 걸쳐**: (N) deprecate + 이중 쓰기, (N+1) 읽기 제거, (N+2) 삭제.
- 마이그레이션 스크립트는 **up + down 모두 작성**. 테스트는 양방향 전부 통과해야 함.
- 빅-뱅 스키마 재설계 금지(P12).

## 4.7 데이터 보존(Retention) 정책

| 데이터 | 기본 보존 | 조정 가능 | 삭제 방식 |
|---|---|---|---|
| Robot 엔터티 | 무기한 | — | 소프트 삭제 + tombstone |
| ScanSession | 2년 | 테넌트별 | 소프트 삭제 |
| ScanResult | 2년 | 테넌트별 | 소프트 삭제 |
| Evidence (blob) | 1년 | 테넌트별 | 해시만 유지, blob은 tombstone |
| Audit | **영구** | 불가 | 삭제 불가(P9) |
| Report | 5년 | 테넌트별 | 소프트 삭제 + 해시 유지 |
| Insight | 1년 | 테넌트별 | 완전 삭제 허용 |
| LLM 대화 | 30일 | 테넌트별 | 완전 삭제 |

## 4.8 개인정보·민감정보 처리

- Evidence 텍스트에서 **자동 레덕션**: SSH 키·토큰·비밀번호 패턴(정규식 + 엔트로피) → `[REDACTED:type]`.
- 레덕션은 **쓰기 시점**에 적용. 원본은 보관 금지.
- LLM 전송 전 **2차 레덕션**. 사용자 설정에서 추가 패턴 등록 가능.

## 4.9 테넌트 격리 구현

### 로우 레벨 격리 (기본)

- 모든 쿼리에 `WHERE tenant_id = :tenant_id` 필수.
- 저장소 레이어가 tenant 컨텍스트를 생성자 주입으로 받고, 직접 `tenant_id`를 주입. 서비스 코드가 실수로 빼먹을 수 없음.

### 스키마 레벨 격리 (옵션, 대형 고객)

- PostgreSQL의 **schema-per-tenant** 옵션. `tenant_<id>` 스키마 자동 생성.
- 전용 애플라이언스에서는 단일 스키마 기본값.

### 교차 테넌트 작업

- 금지가 기본. 필요한 경우(관리자 콘솔) **명시적 권한 + 감사 로깅**.

## 4.10 이 문서의 핵심 결정

1. **모든 루트 엔터티 `tenant_id` 필수** — 단일 사용자 모드에서도 유지.
2. **append-only 감사 테이블**을 DB 레벨 트리거로 강제.
3. **Evidence는 blob store**에 저장하고 **해시 기반 참조**만 관계형 DB에.
4. **JSON Schema가 영속 계약** — 스키마에서 코드 생성.
5. **마이그레이션은 항상 점진적**, big-bang 금지.

다음 문서: [05-api-and-auth.md](./05-api-and-auth.md)
