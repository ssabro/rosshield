# 10. 감사 로그 및 관측성

## 10.1 목표 구분

이 문서는 **세 가지 다른 관심사**를 다룹니다. 혼동하면 제품 신뢰도를 잃습니다.

| 축 | 누가 보는가 | 수명 | 보장 |
|---|---|---|---|
| **Audit** (감사) | 감사인·규제기관·고객 보안팀 | **영구** | 무결성(해시 체인), 외부 검증 가능 |
| **Logs** (로그) | 운영자·엔지니어 | 단기(주·월) | 디버깅 가독성 |
| **Metrics/Tracing** | 엔지니어·SRE | 단기 | 성능·장애 분석 |

**Audit ≠ Log**. Audit는 "누가 언제 무엇을 했는가"의 **비즈니스 사실**. Log는 "시스템에 어떤 일이 있었는가"의 **기술적 관찰**.

---

## Part A — 감사 (Audit)

## 10.2 감사 대상 액션

### WRITE 계열은 모두 감사

| 카테고리 | 예시 |
|---|---|
| 테넌트·사용자 | 초대, 역할 부여, 2FA 변경, 로그인·로그아웃 |
| 로봇·자격증명 | 등록, 수정, 삭제, 자격증명 로테이션 |
| 벤치마크 팩 | 설치, 활성화, 비활성화, 삭제, 매핑 변경 |
| 스캔 | 시작, 취소, 차분 재사용, 수동 override |
| 리포트 | 생성, 서명, 다운로드, 공유 링크 생성 |
| 조치 | remediation 스크립트 생성, 실행, 승인 |
| Insight | dismiss, escalate |
| 설정 | LLM provider 변경, intelligence 토글, retention 조정 |
| API Key | 발급, revoke |
| Audit 자체 | export, verify (who verified what) |

### READ 계열 (선택적 기록)

- 기본: off (볼륨이 너무 큼).
- 특수 리소스(Audit export·특정 민감 증거)는 항상 on.
- 테넌트별로 "READ 감사 강화 모드" 토글 제공 (규제 요건 충족용).

## 10.3 엔트리 구조 (재정리)

```
AuditEntry {
  tenantId
  seq                    // tenant 내 단조 증가 (1, 2, 3, ...)
  occurredAt
  actor {
    type: 'user' | 'api' | 'system' | 'anonymous'
    id                   // us_... | ak_... | 'system' | '0.0.0.0'
    ip?                  // 가능한 경우
    userAgent?
  }
  action                 // 'robot.create' | 'scan.execute' | ...
  target {
    type                 // 'robot' | 'scan' | 'tenant' | ...
    id
  }
  payloadDigest          // sha256(canonical JSON of relevant payload)
  outcome                // 'success' | 'failure' | 'partial'
  error?                 // { code, message }
  prevHash
  hash                   // sha256(prevHash || payloadDigest || occurredAt || actor || action || target || outcome)
}
```

- `payloadDigest`는 **내용의 요약**. 원문 자체를 감사 테이블에 저장하지 않음(크기·개인정보 이유). 필요 시 다른 테이블에서 같은 sha256으로 조회.

## 10.4 해시 체인 구성

```
ε (genesis) → e1 → e2 → e3 → ... → eN
       hash₀      hash₁    hash₂    hash₃       hashN

hash₀ = sha256(0x00..00 || entry₁.payloadDigest || meta₁)
hash_i = sha256(hash_{i-1} || entry_i.payloadDigest || meta_i)
```

- 각 테넌트는 자체 체인.
- `ChainHead` 테이블이 현재 (`tenantId`, `seq`, `hash`, `updatedAt`)를 관리.
- 삽입은 **append 전용 쓰기 경로**(유일한 코드 path)에서만.

## 10.5 Checkpoint 서명

일정 주기(예: 매시간) 또는 세션 완료 등 중요 이벤트마다:

```
CheckpointSignature {
  tenantId
  seq
  hash
  signedAt
  signerKeyId
  signature             // Ed25519(hash || seq || tenantId)
}
```

- 이 서명이 있으면 "이 시점에 이런 체인 상태였음"이 제3자에게 증명됨.
- 서명 키는 **기기/조직 키**. 어플라이언스는 TPM-봉인.

## 10.6 외부 검증 API

### 검증 토큰

감사인에게 발급하는 1회성 검증 토큰 — "이 테넌트의 이 기간 감사를 볼 수 있다".

```
POST /api/v1/audit/verification-tokens
  body: { scope: { from, to }, expiresIn: '7d' }
  response: { token: 'vt_...' (원문 최초 1회), url: 'https://.../verify/vt_...' }
```

### 검증 엔드포인트 (무인증 + 토큰)

```
GET /verify/vt_<token>/entries?from=&to=
GET /verify/vt_<token>/head
GET /verify/vt_<token>/checkpoints
GET /verify/vt_<token>/public-key
```

- 감사인은 별도 OSS 도구(`fg-verify`)로 체인 재계산·공개키 서명 확인.
- 토큰은 읽기 전용, 스코프 제한, 만료.

### 검증 도구 (별도 OSS)

- 단일 바이너리, 인터넷 없이 번들만으로 검증 가능.
- 입력: audit-export.tar.gz
- 출력: "무결성 OK / 체인 위반 at seq N / 서명 불일치"

## 10.7 감사 내보내기

- 포맷: NDJSON (엔트리) + SIGNATURE + 공개키 번들.
- 규모: 테넌트 × 100만 엔트리까지 단일 번들.
- 분할 내보내기: 연/분기별.

## 10.8 금지된 조작 (방어)

1. **UPDATE/DELETE**: DB 트리거·RULE로 차단.
2. **seq 재사용**: `(tenantId, seq)` UNIQUE 제약.
3. **백업 복원 후 추가 쓰기 → 체인 단절**: 복원 이벤트 자체를 감사 엔트리로(`audit.restore`). 이후 이어서 쓰기 시작.
4. **관리자 권한으로 감사 테이블 직접 수정**: 데이터베이스 역할 분리 — 애플리케이션 사용자는 audit에 INSERT만. DBA 계정 접근도 별도 감사(DB 감사 플러그인).

## 10.9 다중 노드 시 순번

분리 모드(API 노드 N개)에서는 `seq` 증가를 직렬화해야 합니다.

- 옵션 A: **단일 writer**. 모든 감사 INSERT는 한 노드로 라우팅.
- 옵션 B: **분산 시퀀스**. DB 시퀀스(PostgreSQL `SEQUENCE`)로 원자적 할당.

초기 출시는 옵션 B.

---

## Part B — 로그 (Logs)

## 10.10 로그 레벨

- `trace`: 개발 전용, 기본 off.
- `debug`: 기본 off, 트러블슈팅 시 on.
- `info`: 주요 이벤트(세션 시작, 팩 로드).
- `warn`: 회복된 실패.
- `error`: 처리되지 않은 에러.
- `fatal`: 프로세스 종료 트리거.

## 10.11 구조화 로그 포맷

```json
{
  "ts": "2026-04-23T03:14:15.123Z",
  "lvl": "info",
  "comp": "scan.executor",
  "msg": "check completed",
  "tenantId": "tn_...",
  "sessionId": "ss_...",
  "robotId": "ro_...",
  "checkId": "CIS-1.1.1.1",
  "outcome": "pass",
  "durationMs": 412,
  "requestId": "req_..."
}
```

- **JSON 한 줄씩** stdout에 기록.
- 파일 로테이션: 10MB × 20 백업 (배포 타깃에 따라 조정).
- 샘플링: `trace/debug`는 고볼륨 구간에서 확률 샘플링.

## 10.12 Redaction

로그 자체에 비밀이 들어가는 사고 방지:

- 로거 미들웨어가 알려진 필드(`password`, `privateKey`, `apiKey`, `authorization`, `cookie`)를 `[REDACTED]`로.
- Evidence redactor가 공유하는 패턴 엔진을 사용.
- CI에 "비밀로 의심되는 고엔트로피 문자열이 로그에 찍히는 코드" 린트.

## 10.13 상관 관계 (Correlation)

- 요청 단위: `requestId` (미들웨어가 생성·전파).
- 이벤트 단위: `eventId` + `causationId` (이벤트 계보).
- 작업 단위: `jobId` (스캔 세션·리포트 생성).
- 로그 필터·검색의 일등 필드.

## 10.14 로그 수집

- 기본: stdout + rotating file.
- 옵션: syslog, Fluentbit sidecar, OTLP log exporter.
- 엔터프라이즈: 고객 SIEM(Splunk·QRadar·Sentinel)에 forwarder 설정 가이드 제공.

---

## Part C — 메트릭 · 추적

## 10.15 메트릭

OpenTelemetry + Prometheus scrape 엔드포인트 `/metrics` (옵트인).

### 핵심 메트릭

| 이름 | 타입 | 설명 |
|---|---|---|
| `fg_http_requests_total` | counter | method·route·status별 |
| `fg_http_request_duration_seconds` | histogram | 응답 시간 |
| `fg_scan_sessions_total` | counter | outcome별 |
| `fg_scan_checks_total` | counter | outcome·packId별 |
| `fg_ssh_connections_active` | gauge | 풀 상태 |
| `fg_ssh_connect_errors_total` | counter | 이유별 |
| `fg_evidence_bytes_stored_total` | counter | 누적 저장량 |
| `fg_llm_calls_total` | counter | provider·outcome별 |
| `fg_llm_tokens_total` | counter | provider·direction별 |
| `fg_audit_entries_total` | counter | tenant별 |
| `fg_audit_chain_verify_seconds` | histogram | 검증 시간 |
| `fg_pack_installed_total` | counter | 결과별 |
| `fg_compliance_score` | gauge | tenant·framework별 |

### 대시보드

- 운영자용 Grafana 대시보드 JSON을 릴리스에 포함.
- 주 탭: 스캔 처리량 / SSH 풀 건강 / LLM 비용 / 감사 체인 상태 / 에러율.

## 10.16 추적 (Tracing)

OpenTelemetry OTLP exporter (옵션).

- 루트 스팬: HTTP 요청 / 스캔 세션 / 리포트 생성.
- 중요 자식: SSH 연결·LLM 호출·DB 쿼리 (이름은 sanitize).
- 샘플링: 기본 1%, 에러 100%, 요청당 강제 샘플 플래그(`?trace=1`).

## 10.17 경보 (Alerting)

내장 경보 경로:

1. **내부 알림**: 알림 센터 (UI 벨).
2. **외부 Webhook**: 등록된 URL로 POST + HMAC.
3. **이메일**: SMTP 설정된 경우.
4. **SIEM 연동**: syslog/OTLP로 forwarder.

경보 트리거 예:

- 감사 체인 검증 실패
- 팩 서명 검증 실패 (설치 시도)
- SSH 연결 실패율 임계 초과
- LLM 토큰 쿼터 도달
- 디스크/blob 스토리지 80% 이상
- Scheduler 낙오 (마지막 실행 시간이 주기보다 오래됨)
- TPM/키체인 접근 실패 (어플라이언스)

## 10.18 자가 진단 (Self-Diagnostics)

UI: Settings → System → Health.

- 각 서브시스템(DB·Blob·SSH 풀·LLM·Pack 미러·스케줄러) 상태.
- 최근 실패·재시도.
- 진단 번들 다운로드 (로그·메트릭 스냅샷·설정 마스킹) — 지원 티켓 첨부용.

## 10.19 텔레메트리 (사용 통계)

- **기본 off**, 사용자가 활성화 시에만 수집.
- 수집 항목(익명): 제품 버전·OS·플랜·기능 사용 빈도·에러 발생률.
- **감사·증거·로봇 데이터는 절대 전송 안 함**.
- 에어갭 프로필에서는 코드 경로 자체가 비활성 (전송 함수가 `noop`).

## 10.20 규제·감사 준비 체크리스트

| 질문 | 답변 |
|---|---|
| "모든 행위를 누가·언제·무엇을 했는지 기록합니까?" | Audit 엔트리로 예 |
| "기록이 변조되지 않았음을 증명할 수 있습니까?" | 해시 체인 + checkpoint 서명으로 예 |
| "기록을 제3자가 독립 검증할 수 있습니까?" | 외부 검증 API + OSS 도구로 예 |
| "기록은 얼마나 보관됩니까?" | 영구 (retention policy에서 변경 불가) |
| "접근 통제·분리·최소 권한 원칙이 적용됩니까?" | RBAC + 테넌시 격리로 예 |
| "장애·사고 시 복원 절차가 있습니까?" | 운영 가이드에 문서화 |
| "개인정보가 로그에 들어갈 가능성을 통제합니까?" | 로거·Evidence 레덕션 파이프라인 |
| "SIEM 연동이 가능합니까?" | syslog·OTLP·webhook |

## 10.21 이 문서의 핵심 결정

1. **Audit는 비즈니스 사실, Log는 기술 관찰** — 섞지 않는다.
2. **해시 체인 + checkpoint 서명 + 외부 검증 API**가 제품의 핵심 해자.
3. **감사는 영구, 로그는 단기** — retention 정책이 다르다.
4. **메트릭·추적은 옵트인**, 프라이버시 우선.
5. **텔레메트리는 기본 off + 에어갭에서는 코드 비활성**.

다음 문서: [11-tech-stack-and-roadmap.md](./11-tech-stack-and-roadmap.md)
