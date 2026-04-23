# 03. 아키텍처

## 3.1 레이어

```
┌────────────────────────────────────────────────────────────┐
│ L5  Client Shells                                          │
│     Web Console · Desktop Shell · CLI                      │
├────────────────────────────────────────────────────────────┤
│ L4  Interface (API Gateway)                                │
│     HTTP REST · WebSocket Streams · OpenAPI                │
│     AuthN · AuthZ · Rate Limit · Envelope · Versioning     │
├────────────────────────────────────────────────────────────┤
│ L3  Application Services                                   │
│     유스케이스 오케스트레이션 (여러 도메인 조율)            │
├────────────────────────────────────────────────────────────┤
│ L2  Domain Services (Bounded Contexts)                     │
│     tenant · fleet · robot · benchmark · scan · evidence   │
│     · insight · advisor · compliance · reporting · audit   │
├────────────────────────────────────────────────────────────┤
│ L1  Platform Services                                      │
│     Event Bus · LLM Adapter · SSH Pool · Storage           │
│     · Secret Store · Signer · Pack Manager · Scheduler     │
│     · Telemetry · Logger · Feature Flags                   │
└────────────────────────────────────────────────────────────┘
```

### 의존 방향

- **하향식**: 각 레이어는 **자기보다 아래 레이어에만** 의존한다.
- 도메인(L2)끼리는 서로 직접 import하지 않는다. 필요하면 **Event Bus** 또는 L3 application service 경유.
- Platform(L1)은 아무에게도 의존하지 않는다.

### 린트 규칙

이 의존 방향을 어기면 빌드 실패. 구체 규칙은 `11-tech-stack-and-roadmap.md` §린트·테스트 섹션 참조.

## 3.2 바운디드 컨텍스트 목록

| 도메인 | 책임 | 주요 aggregate |
|---|---|---|
| `tenant` | 조직·워크스페이스·사용자·권한 | Tenant, User, Role, ApiKey |
| `fleet` | 로봇 묶음·정책 그룹 | Fleet, FleetPolicy |
| `robot` | 로봇 자산·연결 정보·피어 그룹 | Robot, Credential, PeerGroup |
| `benchmark` | 벤치마크 정의·팩·Self-Test | BenchmarkPack, CheckDefinition |
| `scan` | 스캔 세션·실행 오케스트레이션·차분 | ScanSession, ScanResult |
| `evidence` | 원본 증거(stdout·파일 스냅샷) 저장·해시 | EvidenceRecord |
| `insight` | 드리프트·이상·근본 원인·공격 경로·예측 | Insight |
| `advisor` | 대화형 보조·설명·조치 추천 | Conversation, Recommendation |
| `compliance` | 프레임워크 매핑·점수·통제 상태 | FrameworkProfile, ControlMapping |
| `reporting` | 세션·플릿 보고서 생성·서명 | Report, ReportTemplate |
| `audit` | 감사 로그 해시 체인·외부 검증 API | AuditEntry, ChainHead |

각 도메인의 **모델·스키마·서비스·이벤트·API**는 `04-domain-and-data-model.md`와 `05-api-and-auth.md`에서 상세화.

## 3.3 프로세스 토폴로지

### 모노리스 (1 프로세스)

- 데스크톱·기본 온프렘·어플라이언스에서의 기본 형태.
- 모든 도메인·API Gateway가 한 프로세스 안에서 동작.
- Event Bus는 in-process pub/sub.
- SQLite(데스크톱) 또는 PostgreSQL(서버) 연결.

### 분리 모드 (선택, 대규모)

- API Gateway 노드 N개 + Scan Worker 노드 M개로 수평 분리.
- Event Bus를 Redis Streams 또는 NATS로 전환.
- 공유 DB는 PostgreSQL, 공유 Blob은 S3 호환.
- Kubernetes + Helm 차트로 관리.

> **원칙**: 초기 출시는 **모노리스로만**. 분리 모드는 고객 규모가 실제로 증명된 이후 확장.

## 3.4 프로세스 내 구조 (단일 프로세스)

```
┌──────────────────────────────────────────────┐
│ main()                                       │
│  ├─ loadConfig()                             │
│  ├─ Platform.bootstrap()                     │
│  │    ├─ Storage (SQLite|PG)                 │
│  │    ├─ EventBus (in-proc or external)      │
│  │    ├─ SSHPool                             │
│  │    ├─ LlmProvider                         │
│  │    ├─ Signer (TPM|SoftKey)                │
│  │    ├─ PackManager                         │
│  │    └─ Scheduler                           │
│  ├─ Domain.register(platform)                │
│  │    ├─ TenantService ... AuditService      │
│  ├─ ApiGateway.mount(domains)                │
│  │    ├─ Router                              │
│  │    ├─ AuthMiddleware                      │
│  │    ├─ WebSocketHub                        │
│  │    └─ StaticFileHandler (Web UI 번들)     │
│  └─ httpServer.listen(bindAddr, port)        │
└──────────────────────────────────────────────┘
```

### 시작 시퀀스

1. **Config 로드** — 파일·환경변수·CLI 플래그 병합. 검증 실패 시 exit.
2. **Platform 부트스트랩** — 저장소·이벤트버스·SSH풀·LLM·서명자 초기화. 네이티브 의존성(SQLite/TPM) 확인.
3. **Pack 로드** — 설치된 벤치마크·매핑·리포트 팩 서명 검증 후 메모리 인덱스 구축.
4. **도메인 등록** — 각 도메인 서비스가 Platform에서 필요한 리소스를 **생성자 주입**으로 받음.
5. **API Gateway 마운트** — 도메인 라우트 + 미들웨어 + 웹 UI 정적 서빙.
6. **Scheduler 시작** — 주기 작업(스캔·리포트·팩 확인) 등록.
7. **Listener 바인딩** — 배포 타깃별로 localhost 또는 0.0.0.0.

## 3.5 데이터 경계

### 저장 경계

| 유형 | 위치 | 이유 |
|---|---|---|
| 구조화 데이터 (Robot·Scan·Result·Audit) | 관계형 DB (SQLite/PG) | 트랜잭션, 쿼리, 감사 |
| Evidence 원본 (stdout·파일) | Blob Store (로컬 디스크 / MinIO / S3) | 대용량·불변·해시 주소 |
| 비밀 (SSH 키·LLM API 키) | Secret Store (OS keychain / Vault / 파일+KEK) | 접근 감사·로테이션 |
| 팩 (벤치마크·매핑·템플릿) | 전용 팩 디렉터리 + 서명 | 제품 업데이트와 독립 |
| 로그 | Rotating file + stdout | 관제·디버깅 |
| 설정 | 파일 (yaml) + 환경변수 + DB (테넌트별) | 3-tier |

### 네트워크 경계

- **사용자 ↔ Gateway**: HTTPS (TLS 1.3 권장)
- **Gateway ↔ Core**: 같은 프로세스 or 내부 네트워크
- **Core ↔ 로봇**: SSH (키 기반 인증 권장)
- **Core ↔ LLM**: HTTPS (클라우드) 또는 localhost (Ollama)
- **Core ↔ Pack Mirror**: HTTPS + 팩 서명

## 3.6 이벤트 모델

### 도메인 이벤트 목록 (초기)

| 도메인 | 주요 이벤트 |
|---|---|
| tenant | TenantCreated, UserInvited, RoleAssigned |
| robot | RobotAdded, RobotConnectionTested, CredentialRotated |
| benchmark | PackInstalled, PackActivated, SelfTestCompleted |
| scan | ScanScheduled, ScanStarted, ScanCompleted, ScanFailed |
| evidence | EvidenceStored, EvidenceReferenced |
| insight | DriftDetected, AnomalyDetected, RootCauseIdentified |
| compliance | ScoreRecomputed, ControlStatusChanged |
| reporting | ReportGenerated, ReportSigned |
| audit | AuditEntryAppended, ChainVerified |

### 이벤트 구조

```jsonc
{
  "id": "evt_...",
  "type": "ScanCompleted",
  "version": 1,
  "tenantId": "tn_...",
  "occurredAt": "2026-04-23T01:12:00Z",
  "aggregate": { "type": "ScanSession", "id": "ss_..." },
  "payload": { /* 스키마 per type */ },
  "causationId": "evt_..." // 이 이벤트를 일으킨 이벤트
}
```

- `causationId`로 이벤트 계보 추적 가능.
- 버전 필드로 이벤트 스키마 진화 지원.
- 모든 이벤트는 영속(`audit` 도메인 또는 별도 테이블).

### 구독 패턴

- **도메인 내부 구독**: 해당 도메인 서비스가 자기 도메인 이벤트에 반응 (eg. scan이 Result 저장 후 Insight 트리거).
- **도메인 간 구독**: L3 Application Service 경유 또는 명시적 event handler. 도메인 서비스끼리 직접 묶지 않음.
- **외부 구독(Webhook)**: 구독자 URL 등록 → Event Bus가 재시도·서명 포함 POST.

## 3.7 API Gateway

### 책임

- **인증** (AuthN): API Key, OIDC JWT, OS 로컬 세션(데스크톱), SAML SSO(서버)
- **인가** (AuthZ): 테넌트 스코프 + 역할 기반 권한 체크
- **엔벨로프**: 모든 응답을 `{ok: true, value} | {ok: false, error}`로 감쌈
- **버저닝**: `/api/v1/*`, `/api/v2/*` 병행. 한 버전은 최소 12개월 유지.
- **WebSocket 허브**: 이벤트 스트리밍, 진행 중 스캔 상태, advisor 대화
- **Rate Limit**: 테넌트·사용자 단위
- **관측**: 모든 요청을 `audit` 도메인에 로깅(WRITE 계열)

### OpenAPI 스키마

- 설계 단계부터 **OpenAPI 3.1 스키마가 원천(source of truth)**.
- 서버 라우트와 클라이언트(Web/CLI) 타입은 스키마에서 **코드 생성**.
- 공개 문서는 Swagger UI로 자동 서빙(선택적으로 켬).

## 3.8 도메인 서비스 내부 구조 (공통 템플릿)

```
domains/<name>/
  ├─ model/          // 불변 값·엔터티·도메인 타입
  ├─ repository/     // 저장소 인터페이스 + 구현(SQLite/PG)
  ├─ service/        // 도메인 서비스(유스케이스)
  ├─ policy/         // 도메인 규칙·validation
  ├─ event/          // 이벤트 타입·발행
  ├─ api/            // HTTP 라우트·WebSocket 핸들러·OpenAPI 조각
  └─ test/           // 단위·통합 테스트
```

### 파일·함수 크기 제한

- 파일 **≤ 400줄**(권장), **≤ 800줄**(최대).
- 함수 **≤ 50줄**(권장).
- 단일 책임 원칙: 한 파일은 한 관심사.

## 3.9 관측성 (L1 Telemetry)

- **구조화 로그** (JSON) → stdout + rotating file
- **메트릭** (OpenTelemetry) → Prometheus scrape endpoint (옵션)
- **추적** (OpenTelemetry) → OTLP exporter (옵션)
- **감사 로그** (해시 체인) → 별도 append-only 테이블, 외부 검증 API 제공

상세는 `10-audit-and-observability.md` 참조.

## 3.10 실패·복구 모델

### 트랜잭션 경계

- **도메인 서비스 단위**로 트랜잭션 관리.
- 다중 도메인 변경이 필요한 유스케이스는 L3 Application Service가 **saga 패턴**으로 조율 (보상 이벤트).

### 재시도·타임아웃

- **외부 호출**(SSH·LLM·Pack Mirror)은 타임아웃 + 지수 백오프 재시도 + 회로 차단기.
- **이벤트 버스 전달 실패**는 DLQ(Dead Letter Queue)로 격리 + 알림.

### Graceful degradation

- LLM 장애 → 규칙 기반 fallback
- Ollama 장애 → 클라우드 LLM으로 전환(사용자 허용 시) or 설명 기능 일시 비활성화
- Pack Mirror 장애 → 설치된 팩으로 계속 동작, 업데이트 알림만 일시 중지
- Scheduler 장애 → 다음 기동 시 누락된 잡 재생성

## 3.11 확장 포인트 (Plugin Points)

| 포인트 | 설명 | 버전 |
|---|---|---|
| **Benchmark Pack** | 외부 벤치마크 추가 (서명된 팩) | v1 |
| **Check Type** | 새 종류의 체크(SSH 명령 외: ROS2 topic introspection 등) | v1 |
| **Evidence Source** | 로봇 외 다른 증거원 (CI 파이프라인, 레지스트리) | v2 |
| **LLM Provider** | 추가 모델 provider | v1 |
| **Report Format** | PDF 외 출력 포맷 | v1 |
| **Identity Provider** | SSO/디렉터리 커넥터 | v2 |
| **Notification Channel** | Slack·Teams·이메일·SMS·SIEM | v1 |
| **Attack Simulator** | (옵션) CAI 같은 외부 공격 도구 연동 | v3 |

플러그인은 **서명 필수**. 미서명 플러그인은 개발 모드에서만 로드.

## 3.12 스레드·동시성 모델

- **언어에 의존**(Go: goroutine, TS: async/Worker threads). 결정은 `11-tech-stack-and-roadmap.md`.
- **SSH 풀**: 동시 연결 최대 N (tenant별 · fleet별 rate limit 병행)
- **스캔 실행**: check 단위 병렬, 로봇당 동시 check 수 제한
- **Evidence 저장**: 쓰기 큐를 통해 배치. 해시 계산은 worker thread/goroutine.

## 3.13 테스트 피라미드

```
         ┌──────────────┐
         │  E2E (몇십건)  │   브라우저 + 실제 SSH 대상 VM
         ├──────────────┤
         │ 통합 (수백건)  │   도메인 + 저장소 + 이벤트버스
         ├──────────────┤
         │ 단위 (수천건)  │   순수 도메인 로직
         └──────────────┘
```

- **단위**: 의존성 없음, 순수 함수/순수 도메인 객체.
- **통합**: 실제 DB(in-memory SQLite or testcontainers PG), 실제 이벤트 버스.
- **E2E**: 완전한 배포 시나리오 (docker-compose 기반 테스트 하네스 + 목 로봇 VM).

## 3.14 보안 경계 (개요)

- **모든 API 요청**은 tenant·user·role로 인가 확인.
- **tenant 간 데이터 접근**은 기술적으로 차단(저장소 레이어 enforce).
- **비밀**(SSH 키·API 키)은 평문 저장 금지, KEK/DEK 분리 암호화.
- **감사 로그**는 append-only, 삭제 불가.
- **팩**은 서명 검증 후에만 활성.
- 상세: `06-security-and-tenancy.md`.

다음 문서: [04-domain-and-data-model.md](./04-domain-and-data-model.md)
