# rosshield Phase 3 Backlog — Multi-tenant SKU

**범위**: 8~10주 (`docs/design/11-tech-stack-and-roadmap.md` §11.13).

**전제**: Phase 2 10/10 epic + 운영 갭 회수(A1 CreateRobot · A3 Web 등록 form), Web Console 9 페이지(Overview·Robots·Scans·Findings·Compliance·Advisor·Reports·Audit·Settings), C5 i18n + C6 HttpOnly cookie + dark mode + CI에 frontend job 통합. 단일 바이너리는 그대로 유지하되 SKU 분기를 통해 SSO·MT·외부 시스템 연동을 지원.

**Exit 기준**: 첫 유료 Enterprise 고객 PoC 배포 (단일 테넌트 SaaS 또는 온프렘 Compose).

---

## Phase 2 Carryover (deferred — Phase 3 합류 후보)

Phase 2에서 명시 deferred한 항목 — Phase 3 진입 시 우선 처리할지 결정 필요.

| ID | 출처 | 내용 | 추정 |
|---|---|---|---|
| C4 | E10.T4 | Playwright E2E (docker-compose harness) | 2~3일 |
| O1 | A2 backlog 후속 | `POST /api/v1/scans/run` (이미 구현 — 별 endpoint 추가 시) | — |
| O2 | C7 강화 | Refresh reuse detection 후속 — 운영 알림 + 사용자 강제 logout UX | 1일 |
| O3 | spec drift 정리 | OpenAPI에 ListReports / Audit verify / Insight·Compliance 일부 표면 추가 | 0.5일 |
| O4 | dev UX | Vite proxy + Tauri dev 통합 — local dev에서 한 명령으로 Tauri + 백엔드 띄우기 | 0.5일 |

**권장 우선순위**: C4(릴리스 안전망) → O3(spec 일관성) → 그 외.

---

## Phase 3 신규 Epic

`docs/design/11-tech-stack-and-roadmap.md` §11.13 Phase 3 spec 기반 + Phase 2 운영 회고.

### E20. SSO — OIDC + SAML (1.5주)

**왜**: Enterprise 고객의 1순위 요구. 자체 패스워드 관리 부담 제거 + 감사 로그에 IdP 발행 기록.

#### 스코프

```
internal/domain/tenant/sso/
  ├─ oidc.go           # Authorization Code + PKCE (Google / Okta / Auth0)
  ├─ saml.go           # SP-initiated (Okta·Azure AD)
  └─ sqliterepo/       # sso_providers 테이블 + 사용자 매핑 (마이그레이션 0019)
internal/api/handlers/auth_sso.go  # /auth/sso/{provider}/login + /auth/sso/{provider}/callback
```

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E20.T1 | `TestOIDCDiscoveryFetchesIssuerMetadata` (mock IdP) | RFC 8414 endpoint 발견 |
| E20.T2 | `TestOIDCAuthorizationCodeFlowMintsAccessToken` | code → IdP token → email/sub claim → tenant 매핑 |
| E20.T3 | `TestOIDCStateAndPKCEMismatchRejected` | CSRF 방어 |
| E20.T4 | `TestSAMLAssertionVerifiedWithIdPCert` | XML signature verify |
| E20.T5 | `TestSSOFirstLoginCreatesUserWithExternalSubject` | auth_provider="oidc"·external_subject |
| E20.T6 | `TestSSOEmailRouteToExistingTenant` | 같은 도메인 사용자 자동 합류 |

#### Exit 기준

- OIDC 1개 IdP(Google or Okta) + SAML 1개 IdP(Okta) 동작 데모.
- 기존 local 패스워드 사용자와 공존(혼합 모드).
- 모든 SSO 콜백이 audit emit.

#### 설계 참조

§5.2 Auth, §10.3 audit (IdP·Subject·external claim 영속).

---

### E21. 초대·역할 관리 UI (1주)

**왜**: 첫 admin 외 추가 사용자 초대가 현재 미존재(seed admin·SSO만 가능). 운영자가 webcons로 사용자 추가 가능해야.

#### 스코프

- `internal/domain/tenant/invitation/` — 초대 토큰 + 만료 + 역할
- `POST /tenants/current/invitations` + `POST /invitations/{token}/accept`
- Web Console `/users` 페이지 — 초대 발송 + 역할 변경 + 사용자 비활성화
- 이메일 발송 어댑터(SMTP·SES·noop) — 옵트인

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E21.T1 | `TestInvitationTokenIsSingleUseAndExpires` | 7일 default + 1회 사용 |
| E21.T2 | `TestAcceptInvitationCreatesUserWithRoles` | 초대 시 지정한 role 자동 할당 |
| E21.T3 | `TestInvitationAuditEmitted` | invite_sent·invite_accepted |
| E21.T4 | `TestUsersListRoleAssign` | admin이 role 변경 시 audit + RBAC 즉시 반영 |

#### Exit 기준

- admin이 Web Console에서 사용자 추가/삭제·role 변경 가능.
- 초대 메일 noop 모드도 이메일 본문 stdout 출력으로 시연 가능.

---

### E22. PostgreSQL 프로덕션 배포 경로 (1.5주)

**왜**: SQLite는 단일 인스턴스 한정. SaaS·HA 배포에는 PG 필수. storage layer 분리 설계는 이미 되어 있으나 PG 구현체가 없음.

#### 스코프

```
internal/platform/storage/postgres/
  ├─ pg.go             # storage.Storage + Tx 구현
  ├─ migrations/       # 0001~0019 PG 버전 (textual translation)
  └─ pg_test.go        # docker compose pgcontainer
deploy/k8s/                # Helm chart (옵션)
deploy/compose/postgres.yml # PG service compose
```

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E22.T1 | `TestPostgresImplementsStorageInterface` | tenant scope tx + WithTenantID |
| E22.T2 | `TestMigrationsApplyIdempotently` | 0001~0019 PG 컴파일 |
| E22.T3 | `TestPostgresAndSQLiteShareDomainTests` | shared test suite로 양 driver 동등성 |
| E22.T4 | `TestConnectionPoolSizingAndTimeout` | pgxpool 설정 |

#### Exit 기준

- `--storage=postgres` flag로 부팅, 기존 도메인 테스트 동일 통과.
- compose에 PG service 추가, helm chart 1차.

---

### E23. Webhook + SIEM 연동 (1주)

**왜**: 감사 결과·Insight를 외부 SIEM(Splunk·Elastic·Datadog)으로 송출 — 보안 운영 통합.

#### 스코프

```
internal/domain/integration/webhook/
  ├─ webhook.go        # WebhookEndpoint + WebhookEvent + 재시도 큐
  ├─ siem.go           # Common Event Format(CEF) / ECS / OpenTelemetry log
  └─ sqliterepo/       # 마이그레이션 0020
```

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E23.T1 | `TestWebhookSignedWithHMAC` | X-Rosshield-Signature: sha256=... |
| E23.T2 | `TestRetryWithExponentialBackoff` | 5회·1m·5m·15m·1h·24h |
| E23.T3 | `TestEventTypeFilterByTenantConfig` | scan.completed·insight.created·audit.checkpoint 선택 |
| E23.T4 | `TestCEFFormatPassesSplunkSanityScan` | OOTB Splunk source type 호환 |

#### Exit 기준

- scan completed 후 webhook 1회 송신 + 1회 SIEM ECS json 영속.
- 실패 webhook 자동 재시도 + 콘솔에서 dead-letter 큐 확인.

---

### E24. 라이선스·쿼터·과금 훅 (3일)

**왜**: open-core (D5) 모델의 Enterprise 기능 게이트. 라이선스 키 + 사용량 미터.

#### 스코프

- `internal/platform/license/` — Ed25519 서명된 라이선스 토큰 (오프라인 검증 — P3)
- 메트릭: tenant당 robot 수·scan/일·LLM 토큰
- enforcement: SSO·SAML·Webhook 같은 enterprise endpoint는 라이선스 검증 후 진입

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E24.T1 | `TestLicenseTokenVerifiedWithEmbeddedPubKey` | sk_rs/pk_rs 동봉 |
| E24.T2 | `TestQuotaExceededReturns402PaymentRequired` | robot/scan 한도 초과 |
| E24.T3 | `TestUsageMeterAggregatesPerTenant` | daily counter + 보고 |

#### Exit 기준

- Phase 0 시드 라이선스(unlimited dev key)와 Phase 3 customer 라이선스 두 가지 시연.

---

### E25. 고가용성 배포 (옵션, 4일)

**왜**: 다수 인스턴스에서 audit chain·scan orchestration이 결정론적으로 동작해야.

#### 스코프

- `--leader=postgres-advisory-lock` 옵션 — single-writer 보장
- audit chain 진입은 leader만, read는 모든 인스턴스 가능
- HAProxy/Caddy 앞에 sticky session

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E25.T1 | `TestAdvisoryLockEnsuresSingleAuditWriter` | 두 인스턴스 동시 부팅 시 leader 1명 |
| E25.T2 | `TestReadReplicaServesListEndpoints` | follower가 read 요청 처리 가능 |

#### Exit 기준

- compose에 2 인스턴스 배포 → leader 죽으면 follower가 cookie 유지 + 새 leader 선출.

---

## Phase 3 Web Console 갭 (B2~B3 — 별도 epic 분해 후보)

Phase 2에서 도입한 9 페이지 외에 SSO·초대·webhook·라이선스 UI가 필요.

| ID | 페이지 | 의존 |
|---|---|---|
| B2 | `/users` (초대·역할) | E21 |
| B3 | `/integrations` (webhook·SIEM) | E23 |
| B4 | `/sso` (provider 관리) | E20 |
| B5 | `/license` (키 + 사용량) | E24 |

---

## 의존 그래프

```
E20 SSO ──┬─→ E21 초대 ──→ B2 /users
          └────────────────→ B4 /sso
E22 PG ──→ E25 HA
E23 Webhook ────→ B3 /integrations
E24 License ────→ B5 /license

Carryover C4 Playwright (independent)
```

병렬 가능: E20·E22·E23·E24는 독립 (E21·B2~B5는 후속).

---

## 추정 (병렬 + 1인 운영 가정)

| Epic | 단독 추정 | 병렬 단축 |
|---|---|---|
| E20 SSO | 1.5주 | (병렬 진입) |
| E21 초대 | 1주 | E20 후 |
| E22 PostgreSQL | 1.5주 | (병렬 진입) |
| E23 Webhook | 1주 | (병렬 진입) |
| E24 License | 3일 | (병렬 진입) |
| E25 HA | 4일 | E22 후 |
| B2~B5 Web | 1주 | 후속 |
| C4 Playwright | 2~3일 | (병렬 진입) |
| **합계** | **8주 + Web** | **~6주** |

---

## 리스크 (Phase 3 한정)

| 리스크 | 완화 |
|---|---|
| SAML XML 파싱·서명 검증 함정 | gosaml2 등 검증된 라이브러리 + 외부 PoC |
| PG 마이그레이션과 SQLite 분기 | shared schema YAML → 두 driver SQL 자동 변환 우선 검토 |
| Multi-tenant cross-tenant leak | 모든 query에 tenant_id WHERE + 기존 통합 테스트 확장 |
| Webhook 재시도 큐 레이턴시 | 큐는 SQLite/PG persistent (in-mem 금지) |
| 라이선스 토큰 위조 | Ed25519 + 빌드 시 pubkey 임베드 (외부 검증 가능 — P1) |

---

## Phase 3 Exit 체크리스트

- [ ] OIDC 1개 + SAML 1개 IdP로 SSO 로그인 시연
- [ ] Web Console에서 admin이 사용자 초대 → 새 사용자가 로그인 → role 반영
- [ ] PostgreSQL 백엔드로 모든 도메인 테스트 통과
- [ ] scan.completed 시 webhook + SIEM ECS log 1회 송출
- [ ] 라이선스 키 검증 + 한도 초과 시 402 응답
- [ ] (옵션) HA 2 인스턴스 leader/follower 동작

---

## Phase 2 → Phase 3 진입 체크리스트

- [x] phase2-backlog.md → archive로 이전 (Phase 3 진입 결정 시)
- [x] phase3-backlog.md(본 문서) 신규 작성
- [ ] **R20-1** 라이선스 모델 — Apache-2.0(현 D5) 유지 vs BSL/Elastic License/SSPL 같은 보호 라이선스로 변경. 결정 트리거: D6(public 전환) 직전. 시장 전례: HashiCorp Vault MPL→BSL(2023), Elastic Apache→SSPL/Elastic License, Sentry BSL, Grafana·GitLab open-core 분리. 우려: AWS·대형 SaaS 기업의 같은 코드 그대로 SaaS화.
- [ ] **R20-2** 코어/엔터프라이즈 코드 분리 — open-core 모델(D5) 실현. 별 repo로 분리할지(`rosshield-core` Apache + `rosshield-enterprise` closed) vs 단일 repo + 빌드 플래그(`-tags=enterprise`). 영향 범위: SSO·MT·webhook·라이선스(E20~E24)는 모두 enterprise 후보.
- [ ] **R20-3** GitHub repo visibility (D6 재논의) — R20-1 결정 후 수행. private 유지 vs public(보호 라이선스 동봉).
- [ ] R20-4 SSO IdP 선택 우선순위 (Google Workspace · Okta · Azure AD · Auth0 — 첫 PoC 고객 합의)
- [ ] R20-5 PG 마이그레이션 자동화 도구 (golang-migrate · atlasgo · pure-Go schema YAML → SQL 변환 자체)
- [ ] Carryover C4 우선순위 사용자 합의
- [ ] 운영 갭 후속(O3 spec drift) 정리 합의

---

## 문서 생명주기

- 본 백로그는 **살아있는 문서**. 태스크 완료 시 `[x]` + 커밋 해시.
- Phase 3 완료 시 `docs/design/archive/phase3-backlog.md`로 이동, Phase 4 백로그를 동일 경로에 신규.
- 결정 사항은 `SESSION_HANDOFF.md` "결정 로그"에 R20-X 형식으로 기록.
