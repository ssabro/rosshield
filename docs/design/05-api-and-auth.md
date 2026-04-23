# 05. API 및 인증 설계

## 5.1 API 원칙

1. **OpenAPI 3.1이 원천 진실** — 서버/클라이언트/문서가 한 스키마에서 생성.
2. **엔벨로프 통일** — 모든 응답이 `{ok, value} | {ok, error}` 형태.
3. **버전 병행** — 한 메이저 버전은 **최소 12개월** 서비스.
4. **HATEOAS는 안 한다** — 엔터프라이즈 클라이언트에게 오히려 불편. 대신 풍부한 메타데이터.
5. **WebSocket은 이벤트 스트리밍 전용** — CRUD는 HTTP.
6. **gRPC는 선택** — 1.x에서는 HTTP+JSON만. 성능/폴리글랏 요구가 검증되면 도입.

## 5.2 URL 구조

```
/api/v1/...                         # 메이저 버전 1
/api/v2/...                         # 다음 메이저 버전 (병행 기간)
/ws/v1/...                          # WebSocket
/.well-known/...                    # 공개 메타데이터 (OpenID discovery 등)
/healthz  /readyz                   # 헬스체크 (무인증)
/metrics                            # Prometheus scrape (옵션, 내부망)
/pack-mirror/...                    # 팩 미러 (어플라이언스의 내부 서빙)
```

### 리소스 네이밍

- 복수 명사: `/robots`, `/scans`, `/reports`
- ID는 path: `/robots/{robotId}`
- 중첩은 2단계까지: `/scans/{scanId}/results`
- 액션은 `POST /resource/{id}:action` 스타일: `/scans/{id}:cancel`, `/packs/{id}:activate`

## 5.3 응답 엔벨로프

### 성공

```json
{
  "ok": true,
  "value": {
    "id": "ro_01H...",
    "name": "amr-01",
    "..."
  },
  "meta": {
    "requestId": "req_01H...",
    "serverVersion": "1.3.2",
    "responseAt": "2026-04-23T03:14:15Z"
  }
}
```

### 실패

```json
{
  "ok": false,
  "error": {
    "code": "robot.not_found",
    "message": "Robot 'ro_01H...' was not found in tenant 'tn_01H...'.",
    "category": "not_found",
    "target": { "type": "robot", "id": "ro_01H..." },
    "details": {}
  },
  "meta": {
    "requestId": "req_01H..."
  }
}
```

### 에러 카테고리

`unauthenticated` · `forbidden` · `not_found` · `conflict` · `validation` · `rate_limited` · `upstream` · `internal`

HTTP 상태 코드는 카테고리에서 파생되지만, 클라이언트는 **카테고리 문자열을 신뢰**해야 합니다(프록시 장비의 코드 변형 대응).

## 5.4 페이지네이션 · 필터 · 정렬

- **페이지네이션**: `cursor` 기반. `?limit=50&cursor=...`
- 응답에 `nextCursor`, `prevCursor`, `approxTotal`(가능할 때).
- **필터**: 쿼리스트링 `?status=completed&fleetId=fl_...`
- **정렬**: `?sort=-completedAt` (앞 `-`는 descending)

## 5.5 주요 엔드포인트 (요약)

### 인증

```
POST   /api/v1/auth/login                   # 로컬 자격증명 로그인
POST   /api/v1/auth/token/refresh           # refresh 토큰 교환
POST   /api/v1/auth/logout
GET    /api/v1/auth/me                      # 현재 세션 정보
GET    /.well-known/openid-configuration    # OIDC discovery (위임 로그인 시)
GET    /api/v1/auth/providers                # 활성 IdP 목록
```

### Tenant · User

```
GET    /api/v1/tenants/current
PATCH  /api/v1/tenants/current
GET    /api/v1/users
POST   /api/v1/users:invite
PATCH  /api/v1/users/{userId}
DELETE /api/v1/users/{userId}
GET    /api/v1/roles
POST   /api/v1/roles
PATCH  /api/v1/roles/{roleId}
```

### Fleet · Robot

```
GET    /api/v1/fleets
POST   /api/v1/fleets
GET    /api/v1/fleets/{fleetId}
PATCH  /api/v1/fleets/{fleetId}
DELETE /api/v1/fleets/{fleetId}

GET    /api/v1/robots?fleetId=&tag=&q=
POST   /api/v1/robots
GET    /api/v1/robots/{robotId}
PATCH  /api/v1/robots/{robotId}
DELETE /api/v1/robots/{robotId}
POST   /api/v1/robots/{robotId}:testConnection
POST   /api/v1/robots/{robotId}:rotateCredential
POST   /api/v1/robots:importCsv            # multipart upload
```

### Benchmark Pack

```
GET    /api/v1/packs
POST   /api/v1/packs:install               # multipart 서명된 팩 업로드
POST   /api/v1/packs/{packId}:activate
POST   /api/v1/packs/{packId}:deactivate
GET    /api/v1/packs/{packId}/checks
GET    /api/v1/packs/{packId}/selftest
POST   /api/v1/packs:checkForUpdates       # Pack Mirror 조회 (온라인 모드)
```

### Scan

```
POST   /api/v1/scans                        # 새 세션 생성 (robots, packId, level 등)
GET    /api/v1/scans?status=&fleetId=&from=&to=
GET    /api/v1/scans/{scanId}
POST   /api/v1/scans/{scanId}:cancel
GET    /api/v1/scans/{scanId}/results
GET    /api/v1/scans/{scanId}/results/{resultId}
GET    /api/v1/scans/{scanId}/results/{resultId}/evidence/{evidenceId}
```

### Insight · Advisor

```
GET    /api/v1/insights?scope=&kind=&severity=
POST   /api/v1/insights/{insightId}:dismiss

POST   /api/v1/advisor/conversations
POST   /api/v1/advisor/conversations/{convId}/messages
GET    /api/v1/advisor/conversations/{convId}/messages
```

### Compliance

```
GET    /api/v1/compliance/frameworks
POST   /api/v1/compliance/frameworks/{framework}:enable
GET    /api/v1/compliance/snapshots?sessionId=&from=&to=
GET    /api/v1/compliance/controls?framework=&status=
POST   /api/v1/compliance/mappings:suggest    # (LLM 옵트인) 자동 매핑 제안
POST   /api/v1/compliance/mappings:approve
```

### Report

```
POST   /api/v1/reports                        # 생성 요청 (scope, format, templateId)
GET    /api/v1/reports
GET    /api/v1/reports/{reportId}
GET    /api/v1/reports/{reportId}/download    # 서명된 본문 다운로드
POST   /api/v1/reports/{reportId}:verify      # 서명 검증
```

### Audit

```
GET    /api/v1/audit/entries?from=&to=&actor=
GET    /api/v1/audit/head                     # 현재 ChainHead
POST   /api/v1/audit/verify                   # 구간 체인 검증 (외부 검증자용, 테넌트 단위)
GET    /api/v1/audit/export?format=ndjson     # 감사 로그 내보내기 (서명 동봉)
```

## 5.6 WebSocket 스트림

```
/ws/v1/events?topics=scan,insight,audit&tenantId=...
```

서버가 `EventEnvelope` 포맷으로 푸시:

```json
{
  "type": "ScanProgress",
  "sessionId": "ss_01H...",
  "robotId": "ro_01H...",
  "completedChecks": 23,
  "totalChecks": 180,
  "emittedAt": "2026-04-23T03:14:20Z"
}
```

구독 토픽:

- `scan` — 세션 진행·완료·실패
- `insight` — 신규 insight 감지
- `audit` — (admin만) 감사 엔트리
- `advisor:{conversationId}` — LLM 응답 스트리밍
- `report:{reportId}` — 리포트 생성 진행

## 5.7 인증 (AuthN)

### 지원 방식

| 방식 | 대상 SKU | 설명 |
|---|---|---|
| **OS Local** | 데스크톱 | OS 로그인 사용자 매핑. PIN 옵션. |
| **Local 계정** | 온프렘·어플라이언스 | 이메일 + 해시된 비밀번호. TOTP 2FA 옵션. |
| **OIDC** | 온프렘·어플라이언스 | Google/Azure AD/Keycloak/Okta 등 |
| **SAML** | 엔터프라이즈 | Enterprise SSO |
| **API Key** | 전부 | 프로그래매틱 접근 |
| **Session Cookie** | Web UI | HttpOnly + Secure + SameSite=strict |
| **Bearer JWT** | CLI·API | `Authorization: Bearer <jwt>` |

### JWT 구조 (내부 발급)

```json
{
  "sub": "us_01H...",
  "tid": "tn_01H...",
  "roles": ["auditor"],
  "iat": 1714..., "exp": 1714..., "jti": "jti_..."
}
```

- 서버는 `RS256` 또는 `EdDSA` 서명.
- 토큰 수명: access 15분, refresh 14일 (변경 가능).
- 로그아웃 시 refresh 토큰 무효화 (DB에 allowlist/denylist).

## 5.8 권한 (AuthZ)

### 체크 흐름

```
request
  → AuthMiddleware: JWT/Session/APIKey 해석 → Principal
  → TenantMiddleware: principal.tenantId vs URL/쿼리의 tenant 일치 확인
  → RBAC: endpoint에 필요한 Permission ⊆ principal.permissions
  → Handler 실행
  → AuditMiddleware: WRITE 계열이면 감사 엔트리 append
```

### 엔드포인트별 필요 권한 (예)

| 엔드포인트 | 필요 권한 |
|---|---|
| `GET /robots` | `robot.read` |
| `POST /robots` | `robot.write` |
| `POST /scans` | `scan.execute` |
| `POST /packs:install` | `plugin.install` |
| `GET /audit/entries` | `audit.read` |
| `POST /users:invite` | `admin.user` |

### 리소스 소유자 규칙

- 일반 사용자는 **자신이 생성한** 리소스만 수정 가능 (기본).
- `admin.*` 권한자는 테넌트 전체 리소스에 접근.
- 레이블·fleet 단위 위임 권한(eg. "fleet X의 auditor")은 v2에서 도입.

## 5.9 API Key

```
POST /api/v1/apikeys
  → { "name": "ci-scanner", "scopes": ["scan.execute","report.read"],
       "expiresAt": "2027-04-23T00:00:00Z" }
  → 201 { "value": { "key": "fg_live_1234...abcd" (최초 1회만 반환),
                     "prefix": "fg_live_1234", "id": "ak_..." } }
```

- Key는 `Authorization: Bearer fg_live_...`로 사용.
- 저장은 argon2id 해시. 원문은 최초 발급 시에만 반환.
- 삭제는 `revokedAt` 설정 (append-only, 실제 삭제 안 함).

## 5.10 Rate Limit

- 테넌트별 **기본 쿼터**: 1000 req/min (enterprise 상향 가능).
- 감사 시간대에는 완화 옵션.
- 429 응답에 `Retry-After` 헤더.
- 쿼터 저장: in-memory (단일 노드) 또는 Redis (분리 모드).

## 5.11 버저닝 정책

- **SemVer** (기계판독용): `1.3.2` — API 버전 URL 프리픽스와 독립.
- **URL 프리픽스**: `/api/v1`, `/api/v2` — 호환 깨지는 변경이 있을 때만 메이저 증가.
- **병행 기간**: v1 → v2 전환 시 v1을 **최소 12개월** 유지.
- **Deprecation 헤더**: `Deprecation: true`, `Sunset: 2027-04-23`, `Link: <new-url>; rel="successor-version"`

## 5.12 OpenAPI 관리

### 파일 구조

```
newprj/openapi/
  ├─ v1/
  │   ├─ openapi.yaml              # 최상위: servers, tags, security
  │   ├─ components/
  │   │   ├─ schemas/              # Robot.yaml, ScanSession.yaml, ...
  │   │   ├─ parameters/
  │   │   └─ responses/
  │   └─ paths/
  │       ├─ robots.yaml
  │       ├─ scans.yaml
  │       └─ ...
  └─ v2/ (v2 시작 시)
```

- 최종 단일 번들(`openapi.bundle.json`)은 빌드 시 생성.

### 코드 생성

- 서버 라우트 핸들러 시그니처 · 요청/응답 타입 · 클라이언트 SDK · CLI 명령 플래그 — 모두 **스키마에서 파생**.
- 수동 타입 정의 금지 (도메인 내부 타입은 별개).

### 검증

- CI에서 스키마 린트(spectral) + breaking change 감지(openapi-diff).
- **Backwards-incompatible 변경은 v2 도입 시에만 허용**.

## 5.13 CORS · CSRF

### CORS

- 기본: 데스크톱 = same-origin, 서버 = 관리자가 화이트리스트 설정.
- 프리플라이트 캐시 24h.
- 자격 증명 포함 요청은 origin 체크 엄격.

### CSRF

- Session Cookie 사용 시 **SameSite=strict** + double-submit token.
- API Key/JWT 사용 시 CSRF 취약점 없음 (쿠키 기반 아님).

## 5.14 멱등성 (Idempotency)

- 변경 엔드포인트(`POST`, `PATCH`, `DELETE`)는 `Idempotency-Key: <ulid>` 헤더 지원.
- 동일 키로 재요청 시 이전 응답 반환 (24h 윈도우).
- `POST /scans`처럼 중복 트리거 가능성이 있는 엔드포인트에서 필수.

## 5.15 파일 업·다운로드

- **업로드**: multipart/form-data 또는 사전 서명된 URL(대용량).
- **다운로드**: 서명된 URL + 짧은 만료 (1~5분).
- 팩·리포트 · 감사 export는 **내용 서명 검증** 후 제공.

## 5.16 에러 처리 상세

### 필드 검증 에러

```json
{
  "ok": false,
  "error": {
    "code": "validation.failed",
    "category": "validation",
    "message": "Request body did not match schema.",
    "details": {
      "fieldErrors": [
        { "path": "/host", "code": "required", "message": "host is required" },
        { "path": "/port", "code": "range", "message": "port must be 1..65535" }
      ]
    }
  }
}
```

### 국제화

- 메시지는 **영어 기본**, 클라이언트가 `Accept-Language: ko` 보내면 한국어 메시지.
- 번역 테이블은 코드에서 분리된 YAML.

## 5.17 WebHook (도메인 외부 통지)

```
POST /api/v1/webhooks
  → { "url": "https://slack/...", "events": ["ScanCompleted","DriftDetected"],
       "secret": "whsec_..." }
```

- 서버가 이벤트 발생 시 `POST <url>` + HMAC-SHA256 서명 헤더.
- 실패 시 지수 백오프 재시도 (최대 24h 유지).
- 구독자는 서명으로 출처 검증.

## 5.18 설계 개요 다이어그램

```
┌────────────────────────┐        ┌─────────────────┐
│ Client (Web/Desktop/CLI)│──HTTPS─▶│  API Gateway   │
└────────────────────────┘        │ Auth·Rate·Envlp │
       ▲                           └──────┬──────────┘
       │ WS events                          │
       │                                    ▼
       │                           ┌────────────────┐
       └───────────────────────────│  WebSocket Hub │
                                    └────────┬────────┘
                                             │
                                    ┌────────▼────────┐
                                    │ Domain Services │
                                    └────────┬────────┘
                                             │
                                    ┌────────▼────────┐
                                    │   Event Bus     │
                                    └─────────────────┘
```

## 5.19 이 문서의 핵심 결정

1. **OpenAPI가 원천** — 서버·클라이언트·문서 자동 생성.
2. **엔벨로프 통일** + **에러 카테고리 기반 처리**.
3. **WebSocket은 이벤트 스트리밍 전용**, CRUD는 HTTP.
4. **v1/v2 병행 12개월**, breaking change는 메이저 증가 때만.
5. **멱등성 키** 지원으로 네트워크 재시도 안전성.
6. **API Key는 해시 저장 + 발급 시 1회만 원문 노출**.

다음 문서: [06-security-and-tenancy.md](./06-security-and-tenancy.md)
