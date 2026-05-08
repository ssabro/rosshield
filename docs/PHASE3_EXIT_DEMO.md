# Phase 3 Exit 시연 가이드

> Phase 3 종료 검증을 위한 6 항목 시연 시나리오. 본 문서는 **운영 시연(operator demo)** 방법을 정리한 참조 문서이며, 자동화 가능한 부분은 통합 테스트로 분리되어 있습니다.
>
> **마지막 업데이트**: 2026-05-08 (E20-C SAML 종료 직후)
> **참조**: `docs/design/phase3-backlog.md` Exit 체크리스트

---

## Exit 항목 개요

| # | 항목 | 자동 검증 | 운영 시연 |
|---|---|---|---|
| 1 | OIDC 1개 + SAML 1개 IdP로 SSO 로그인 | ✅ E20-B(OIDC 11 test) + E20-C(SAML 5 test) | ✅ Google/Okta IdP에서 실 흐름 |
| 2 | Web Console에서 admin이 사용자 초대 → 새 사용자 로그인 → role 반영 | ✅ E21 통합 7건 | ✅ B2 `/users` 페이지 (sub-agent 진행 중) |
| 3 | PostgreSQL 백엔드로 모든 도메인 테스트 통과 | ⚠ 부분 (PG 마이그·Tx 정적 검증) | ✅ Docker PG로 실 부팅 + smoke |
| 4 | scan.completed 시 webhook + SIEM ECS log 1회 송출 | ✅ E23-A·B·C·D 통합 + bridge 5건 | ✅ 외부 webhook receiver 시연 |
| 5 | 라이선스 키 검증 + 한도 초과 시 402 응답 | ✅ E24-A~D 11+6건 | ✅ unlimited dev key + customer key 둘 다 시연 |
| 6 | (옵션) HA 2 인스턴스 leader/follower 동작 | ❌ E25 미구현 | (Phase 4 후속) |

---

## 사전 준비

### 0-A. 빌드

```bash
cd D:/robot/dev/fleetguard
$env:Path = "C:\Program Files\Go\bin;$env:Path"   # Windows PowerShell
make web-build && make build
```

### 0-B. 시드 admin

```bash
./bin/rosshield-server.exe seed admin --email admin@test.local --password verylongpassword12 --name "Test Tenant"
# stdout JSON: {"tenantId":"tn_...","userId":"us_...","email":"admin@test.local"}
```

### 0-B-bis. (옵션) 시드 demo — 시연 데이터 일괄 주입 (O8)

`seed demo`는 admin 시드 후 시연 흐름에 필요한 데이터를 한 번에 주입합니다.
멱등 — 두 번째 호출은 동일 row를 재사용, invitation token은 1회 노출.

```bash
./bin/rosshield-server.exe seed demo --email admin@test.local
# stdout JSON 주요 필드:
#   tenantId/fleetId/packId/robotIds (3개)/sessionIds (5개, 마지막 drift)
#   webhookEndpointId  ← 항목 4 (webhook + SIEM) 시연용 endpoint (URL=http://localhost:9999/sink)
#   ssoProviderId      ← 항목 1 (SSO) 시연용 OIDC provider (issuer=Google, clientId 더미)
#   invitationToken    ← 항목 2 (초대) — 1회 노출, accept URL 시연 가능
#   invitationAcceptUrl ← http://localhost:8080/invitations/accept?token=<token>
```

이 데이터는 다음 항목 시연을 단축합니다:
- **항목 1**: ssoProviderId가 이미 등록되어 있어 `/sso` UI에서 즉시 확인 가능 (실 IdP 호출은 customer client_id로 교체 필요)
- **항목 2**: invitationToken으로 `/invitations/accept` 흐름 즉시 시연 (별 admin 로그인 + curl POST 단계 생략)
- **항목 4**: webhookEndpointId가 scan.completed를 구독 — drift session(5번째 scan)이 자동으로 송출 트리거

### 0-C. 서버 부팅

```bash
./bin/rosshield-server.exe --addr 127.0.0.1:8080
```

---

## 1. OIDC + SAML SSO

### 1-A. OIDC (Google Workspace)

1. admin 로그인 → Web `/sso` 페이지.
2. "Provider 등록" → type=OIDC, name="Google Workspace", config:
   ```json
   {
     "issuer": "https://accounts.google.com",
     "clientId": "<your-client-id>.apps.googleusercontent.com",
     "redirectUri": "http://localhost:8080/api/v1/auth/sso/{providerId}/callback",
     "scopes": ["openid", "email", "profile"]
   }
   ```
3. enabled=true → 등록.
4. 별 브라우저에서 `GET /api/v1/auth/sso/{providerId}/login?redirectAfter=/overview` → 302 Google.
5. Google 로그인 → callback → 백엔드 audit `sso.login.completed` ok=true 확인.
6. (E20-D 후속) IdentityResolver 결선 후 access·refresh 토큰 발급.

### 1-B. SAML (Okta)

1. Okta admin → Application 추가 → SAML 2.0 → metadata.xml 다운로드.
2. metadata.xml에서 entityID·SingleSignOnService URL·X509Certificate 추출.
3. Web `/sso` → type=SAML, config:
   ```json
   {
     "idpEntityId": "http://www.okta.com/exk...",
     "ssoUrl": "https://your-okta.okta.com/app/.../sso/saml",
     "acsUrl": "http://localhost:8080/api/v1/auth/sso/{providerId}/saml/acs",
     "idpCertPem": "-----BEGIN CERTIFICATE-----\nMIID...\n-----END CERTIFICATE-----",
     "audienceUri": "http://localhost:8080/api/v1/auth/sso/{providerId}/saml/acs"
   }
   ```
4. `GET /api/v1/auth/sso/{providerId}/login` → Okta SSO URL → SAMLResponse POST to acs.
5. audit `sso.login.completed` ok=true 확인.

**자동 검증**:
```bash
go test -count=1 ./internal/domain/tenant/sso/...
# OIDC 11 test + SAML 5 test 모두 PASS
```

---

## 2. 사용자 초대·역할

### 2-A. CLI/curl 시연 (B2 `/users` UI 결선 전)

```bash
# admin 로그인 (cookie 또는 Bearer)
curl -X POST http://127.0.0.1:8080/api/v1/auth/login \
     -H "Content-Type: application/json" \
     -d '{"email":"admin@test.local","password":"verylongpassword12"}'
# → {"accessToken":"...", "refreshToken":"..."}

# 초대 발송
TOKEN=...  # accessToken
curl -X POST http://127.0.0.1:8080/api/v1/invitations \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"email":"newuser@test.local","roleName":"operator"}'
# → 201 + {"id":"inv_...","email":"...","token":"<INVITATION_TOKEN>",...}

# 토큰 미리보기 (비인증)
curl http://127.0.0.1:8080/api/v1/invitations/by-token/<INVITATION_TOKEN>
# → {"email":"newuser@test.local","roleName":"operator","accepted":false,...}

# 초대 수락 (비인증)
curl -X POST http://127.0.0.1:8080/api/v1/invitations/by-token/<INVITATION_TOKEN>/accept \
     -H "Content-Type: application/json" \
     -d '{"email":"newuser@test.local","password":"verylongpassword34","displayName":"New User"}'
# → 200 + {"userId":"us_...","roles":["operator"]}

# 새 user로 로그인
curl -X POST http://127.0.0.1:8080/api/v1/auth/login \
     -H "Content-Type: application/json" \
     -d '{"email":"newuser@test.local","password":"verylongpassword34"}'
```

**자동 검증**:
```bash
go test -count=1 ./internal/domain/tenant/sqliterepo/ -run Invitation
# T1~T3 + dup·mismatch·invalid role·격리 7 PASS
```

### 2-B. Web 시연 (B2 `/users` 결선 후)

`/users` 페이지에서 초대 발송 → 사용자에게 accept URL 전달 → `/invitations/accept/{token}` 로 비인증 접근 → user 생성 → `/login`.

---

## 3. PostgreSQL 백엔드

### 3-A. PG 부팅

```bash
docker run -d --name rosshield-pg -p 5433:5432 \
  -e POSTGRES_PASSWORD=changeme -e POSTGRES_DB=rosshield postgres:16

./bin/rosshield-server.exe \
  --storage=postgres \
  --storage-dsn="postgres://postgres:changeme@127.0.0.1:5433/rosshield?sslmode=disable" \
  --addr 127.0.0.1:8081
# 부팅 시 0001~0021 마이그레이션 자동 적용 (golang-migrate, 멱등)
```

### 3-B. seed admin (PG 모드)

```bash
./bin/rosshield-server.exe \
  --storage=postgres \
  --storage-dsn="postgres://postgres:changeme@127.0.0.1:5433/rosshield?sslmode=disable" \
  seed admin --email admin@pg.test --password verylongpassword12
```

### 3-C. PG에서 도메인 흐름 1회

login → CreateInvitation → AcceptInvitation → robot 등록 → scan 시작.

**자동 검증** (정적):
```bash
go test -count=1 ./internal/platform/storage/postgres/
# 마이그레이션 파일 존재·짝·sanity 검증
```

**자동 검증** (실 PG 통합 — testcontainers, 후속 stage):
```bash
# go test -count=1 -tags=integration ./internal/platform/storage/postgres/...
```

---

## 4. Webhook + SIEM

### 4-A. 외부 receiver 띄우기

```bash
# 임시 webhook receiver (e.g. webhook.site 또는 ngrok + local server)
# 또는 nc -l 9000 로 raw 캡처
```

### 4-B. Web `/integrations`에서 endpoint 등록

- URL: receiver 주소
- secret: `shared-secret-1234`
- events: scan.completed
- format: json

### 4-C. scan 1회 실행

```bash
# Web `/scans` 또는 API
curl -X POST http://127.0.0.1:8080/api/v1/scans \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"fleetId":"fl_...","packId":"pk_...","trigger":"manual"}'
```

scan 완료 후 EventBus → webhook bridge → dispatcher → POST receiver:
- 헤더: `X-Rosshield-Signature: sha256=<hex>`, `X-Rosshield-Event: scan.completed`, `X-Rosshield-Delivery: wd_...`
- 본문: scan completion JSON

**자동 검증**:
```bash
go test -count=1 ./internal/app/webhookrun/
# Bridge 5건 + Dispatcher 12건 PASS
```

---

## 5. 라이선스 + 한도 초과

### 5-A. unlimited dev key (community 시연 X — 라이선스 없음)

```bash
./bin/rosshield-server.exe --addr 127.0.0.1:8080
# License: community → SSO·MT·Webhook 등 enterprise feature middleware는 통과 (E20-D는 protected group 안에 있어 admin이면 통과)
# CheckRobotsAdd/ScansToday/LLMTokens는 community SKU에서는 무한 (라이선스 nil)
```

### 5-B. Enterprise 라이선스 (한도 1로 시연)

**(개발자) license 토큰 생성**:
```bash
# 별 도구로 Ed25519 keypair 생성 후 license.Sign 호출 (테스트 헬퍼 활용 가능)
# payload: {edition:"enterprise", features:["sso","webhook"], quotas:{robots_max:1, scans_per_day:1, llm_tokens_per_day:100}}
```

**서버 부팅**:
```bash
./bin/rosshield-server.exe \
  --license-token="<TOKEN>" --license-pubkey-hex="<32B HEX>" \
  --addr 127.0.0.1:8080
```

**시연**:
1. CreateRobot 1번 (성공) → 두 번째 (`POST /api/v1/robots`) → 402 + `{"field":"robots_max"}`
2. CreateScan 1번 (성공) → 두 번째 → 402 + `{"field":"scans_per_day"}`
3. AskAdvisor 100 token 사용 → 다음 ask → 402 + `{"field":"llm_tokens_per_day"}`

**자동 검증**:
```bash
go test -count=1 ./internal/api/handlers/ -run Quota
go test -count=1 ./cmd/rosshield-server/ -run LicenseUsage
# 게이트 6건 + 어댑터 11건 PASS
```

---

## 6. (옵션) HA — Phase 4 후속

E25 미구현. Phase 4에서 PostgreSQL advisory lock + leader/follower로 결선 예정.

---

## 자동 검증 일괄

```bash
go test -count=1 ./...                # 전 패키지 그린
go vet ./...                          # 0 issues
gofmt -l cmd/ internal/               # 0 lines
cd web && pnpm test                   # vitest 121+ PASS
cd web && pnpm build                  # vite + tsc 통과
```

CI 검증: GitHub Actions `Go build, test, lint` + `Web tsc, vitest, build` + `Playwright E2E (smoke)` 3 job 모두 ✓.

---

## 다음 stage

- B2 `/users` 페이지 결선 후 본 문서 §2-B 갱신.
- PG 통합 테스트(testcontainers) 후속 stage에서 §3 자동화.
- E25 HA 후속 epic으로 §6 진척.
