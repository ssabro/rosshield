# 세분 RBAC 후속 — fleet scope 정밀화 + SSO group → role 자동 매핑 — Design

> **상태**: RBAC fine-grained epic 5/5 마감 직후(head `76ae2f0`) Phase 5 후속 design. 본 문서는 코드 0줄 / 마이그레이션 0 / pack 변경 0 — 옵션 비교 + Stage 분해 + 결정 항목 권장 default까지만 마감합니다.
> **참조**:
> - 직전 RBAC epic: `docs/design/notes/rbac-fine-grained-design.md` (568줄, Stage 1~5 마감).
> - Stage 1 `7507106` / Stage 2-A `157f07c` / 2-B `a787506` / 2-C `5d46689` / 2-D `fd2dce5` / 2-E `9a88e40` / Stage 4 `0452941` / Stage 5 `4ec5620`.
> - 시드 코드: `internal/platform/authz/{decision,policy,permission_matrix}.go`, `internal/api/handlers/{handlers,rbac_middleware}.go`, `internal/domain/tenant/{tenant,jwt,sso/sso}.go`.
> - 설계서: `docs/design/05-api-and-auth.md`, `docs/design/06-security-and-tenancy.md`, `docs/design/01-principles.md` §4 (멀티테넌시) §5 (DDD 경계) §12 (점진 적용).
> **R 식별자**: R-RBACEX-1 (본 epic 전체) — 결정 항목은 D-RBACEX-1~10.

---

## 1. 상태·배경

### 1.1 RBAC 5 epic 마감 직후 위치

직전 epic은 5 stage 모두 마감했습니다:

- Stage 1 — `internal/platform/authz/` PDP 패키지 + 6 시스템 role × 9 resource × 6 action 매트릭스 + 단위 결정 테이블.
- Stage 2 — 마이그레이션 0028 `user_roles.scope_type / scope_id` 컬럼 + `tenant.RoleBinding` 도메인 타입 + `Service.AssignRoleScoped`.
- Stage 3 — JWT `bindings` claim + `RequirePermission(resource, action)` middleware factory + 옛 토큰 fallback.
- Stage 4 — `handlers.go` 24 mutation gate를 `RequireRole("admin")` → `RequirePermission(...)` 로 교체 + 195 sub-test 통합 매트릭스 (`rbac_integration_test.go`).
- Stage 5 — web `useHasPermission` hook + sidebar/router guard 확장 + 13 페이지 점진 교체.

직전 epic은 **수직(role tier) + 수평(scope) + action별 분리** 3축 중 수직과 action별 분리는 끝, **수평(fleet scope) 정밀화는 부분 진행** 상태로 남았습니다.

### 1.2 본 doc 범위

직전 epic에서 미해결로 남은 두 가치를 마감합니다:

1. **fleet scope 정밀화** — `RequirePermission` middleware가 chi `URLParam("fleetID"|"fleetId")`만 추출하는 한계를 해소. body·cross-resource·다중 binding 평가 결선.
2. **SSO group → role 자동 매핑** — 엔터프라이즈 customer가 OIDC group claim 또는 SAML attribute로 role binding을 자동 생성하도록 IdP attribute → 내부 RoleBinding 매핑 표면 추가.

본 doc은 코드 0 / 마이그레이션 0. 옵션 비교 + Stage 분해 + 결정 항목 권장 default까지 마감하여, 다음 세션 진입 시 즉시 사용자 합의 → Stage 1 착수가 가능하도록 합니다.

### 1.3 비목표

- **새 인증 방식 추가** — SCIM·LDAP는 별 트랙. 본 epic은 OIDC/SAML 2종에 한정.
- **사용자 정의 role builder UI** — Phase 6+. 본 epic은 시스템 role 6개 + scope/group 매핑까지.
- **dynamic policy** — 시간·IP·MFA 조건은 본 epic 비대상 (직전 epic §1.3 일관).
- **role 자동 회수(de-provisioning)** — 본 epic은 login 시점 매핑 갱신. 사용자 비활성·offboarding은 별 트랙.

---

## 2. 현재 상태 진단

### 2.1 24 mutation endpoint × fleet scope 평가 매트릭스

직전 Stage 4가 24 mutation을 모두 `RequirePermission` 으로 게이트했지만, `fleetIDFromRequest` 헬퍼는 chi URL param `fleetID|fleetId`만 추출합니다 (`internal/api/handlers/rbac_middleware.go::123~128`). 결과:

| # | path | 메서드 | resource.action | path fleet 추출 | fleet scope 평가 |
|---|---|---|---|---|---|
| 1 | `/api/v1/invitations` | POST | tenant_admin.admin | — | tenant scope (자연 일치) |
| 2 | `/api/v1/invitations/{invitationId}` | DELETE | tenant_admin.admin | — | tenant scope (자연 일치) |
| 3 | `/api/v1/sso/providers` | POST | tenant_admin.admin | — | tenant scope (자연) |
| 4 | `/api/v1/sso/providers/{providerId}` | PUT | tenant_admin.admin | — | tenant scope (자연) |
| 5 | `/api/v1/sso/providers/{providerId}` | DELETE | tenant_admin.admin | — | tenant scope (자연) |
| 6 | `/api/v1/webhooks` | POST | tenant_admin.admin | — | tenant scope (자연) |
| 7 | `/api/v1/webhooks/{endpointId}` | PUT | tenant_admin.admin | — | tenant scope (자연) |
| 8 | `/api/v1/webhooks/{endpointId}` | DELETE | tenant_admin.admin | — | tenant scope (자연) |
| 9 | `/api/v1/webhooks/{endpointId}/test` | POST | tenant_admin.admin | — | tenant scope (자연) |
| 10 | `/api/v1/robots` | POST | robot.write | **body.fleetId** | ⚠️ **body lookup 부재 — tenant scope만** |
| 11 | `/api/v1/robots/{robotId}` | DELETE | robot.write | **robotId → robot.fleet_id** | ⚠️ **cross-resource lookup 부재** |
| 12 | `/api/v1/robots/{robotId}/credential:rotate` | POST | robot.admin | **robotId → robot.fleet_id** | ⚠️ **cross-resource lookup 부재** |
| 13 | `/api/v1/utils/ssh-fingerprint` | POST | tenant_admin.admin | — | tenant scope (자연) |
| 14 | `/api/v1/scans` | POST | scan.execute | **body.fleetId** | ⚠️ **body lookup 부재** |
| 15 | `/api/v1/scans/{sessionId}:cancel` | POST | scan.execute | **sessionId → scan.fleet_id** | ⚠️ **cross-resource lookup 부재** |
| 16 | `/api/v1/audit/verify` | POST | audit.verify | — | tenant scope (자연) |
| 17 | `/api/v1/reports/{reportId}:verify` | POST | report.verify | reportId → report.fleet_id | ⚠️ **cross-resource lookup 부재** |
| 18 | `/api/v1/insights/{insightId}:dismiss` | POST | insight.write | **insightId → insight.fleet_id** | ⚠️ **cross-resource lookup 부재** |
| 19 | `/api/v1/fleets/{fleetId}/insights:run` | POST | insight.execute | **fleetId(path)** | ✅ **fleet scope 정확** |
| 20 | `/api/v1/fleets` | POST | fleet.admin | — | tenant scope (자연) |
| 21 | `/api/v1/fleets/{fleetId}` | PATCH | fleet.write | **fleetId(path)** | ✅ **fleet scope 정확** |
| 22 | `/api/v1/fleets/{fleetId}` | DELETE | fleet.admin | — | tenant scope (admin grant 자연) |
| 23 | `/api/v1/compliance/profiles` | POST | compliance.admin | — | tenant scope (자연) |
| 24 | `/api/v1/compliance/profiles/{profileId}/snapshots` | POST | compliance.execute | profileId → profile.fleet_id (옵션 — D-RBACEX-3) | ⚠️ tenant scope만 |

**관찰**:

- **2건만 fleet scope 정밀 평가 적용** (PATCH /fleets/{fleetId}, /fleets/{fleetId}/insights:run).
- **9건이 tenant 글로벌 자연 일치** — invitation/sso/webhook/audit verify/fleet POST·DELETE/compliance profile/system. fleet 단위 분해 무의미.
- **7건이 fleet scope 정밀화 잠재 가치 있음 — 현 tenant scope만 평가** — robot create/delete/rotate, scan create/cancel, report verify, insight dismiss.
- 1건(compliance snapshot)은 D-RBAC-4 결정상 fleet 단위. 본 epic D-RBACEX-3에서 재확정.
- **결과적으로 fleet-admin@fleet_A 사용자는 `RequirePermission(robot.write)` 통과 — fleet 격리 미적용**. 즉 `operator@fleet_A` 토큰으로 fleet_B robot 생성 시 PDP는 통과(테이블에 robot.write가 operator에 있음, scope 검사가 빈 fleetId라 fleet 매칭 skip 못함). 직전 epic에서도 이 한계 명시 (handlers.go::281).

### 2.2 SSO group → role 매핑 부재

현 SSO 결선(E20-D)은 user 매핑까지만 수행합니다:

- `internal/domain/tenant/sso/sso.go::ExternalIdentity` — provider sub → users.id 매핑 row.
- `internal/api/handlers/sso.go::CompleteSSOLoginOIDC|SAML` — state 검증 + UpsertExternalIdentity 호출.
- **role 할당은 운영자 수동** — admin이 user 페이지에서 role binding 직접 추가.

**한계 시나리오**:

- enterprise customer 100명 사용자 + 5 fleet — 운영자 수동 binding 500건. role 변경(승급/해임)도 수동. customer offboarding 절차 누락 risk.
- 표준 SSO 통합 기대값(Okta·Azure AD·Google Workspace 모두 group claim 지원)과 어긋남. enterprise sales 진입 마찰.

**관련 코드**:
- `internal/domain/tenant/sso/oidc.go::IDTokenClaims` (88~102행) — Subject·Email·EmailVerified·Name·iat·exp만 추출. **groups claim 미추출**.
- `internal/domain/tenant/sso/saml.go::SAMLAssertion::Attributes` (84~89행) — gosaml2 attribute map은 보유하나, role 매핑 없음.

### 2.3 한계 요약

1. **horizontal scope 부분만 결선** — path 추출 2건만, 나머지 7건은 tenant scope만 평가 → fleet 격리가 사용자 레벨에서 무력.
2. **다중 binding 일치 정책 부재** — 사용자가 owner+operator@fleet_A+operator@fleet_B 동시 보유 시 PDP는 첫 일치 binding만 보고 ALLOW. body fleetId가 fleet_C일 때 operator binding이 두 개 있어도 매칭 안 함 — 정확하지만 reason 메시지가 빈약.
3. **SSO 통합 미완** — user 자동 생성은 되지만 role binding은 수동 운영자 일.
4. **role refresh 정책 부재** — group 매핑 도입 시 IdP에서 group 변경 → 토큰 만료까지 client 권한 stale.

---

## 3. 요구 사항 분류

### 3.1 fleet scope 정밀화 (3 sub-카테고리)

#### 3.1.1 body lookup (2 endpoint)

POST/PATCH 요청 본문(JSON)에 `fleetId` 필드를 포함하는 endpoint:

- `POST /api/v1/robots` — body.fleetId.
- `POST /api/v1/scans` — body.fleetId.

**평가 흐름**: middleware가 body를 peek하여 fleetId 추출 → Subject.FleetID에 주입 → PDP가 fleet binding scope 비교.

**기술적 어려움**:
- middleware에서 body 읽기 — `http.Request.Body`는 io.ReadCloser, 한 번 읽으면 handler가 파싱 못함.
- 해결: middleware가 `io.ReadAll` 후 `r.Body = io.NopCloser(bytes.NewReader(buf))` 로 재복원. 비대 body(TODO: 본 두 endpoint는 small JSON, ≤ 16KB 가정).
- 또는 handler가 명시적 PDP 재호출 — middleware는 tenant scope 평가만 하고 handler가 body 파싱 후 `authz.Decide` 재호출. 본 doc은 옵션 §4에서 비교.

#### 3.1.2 cross-resource lookup (5 endpoint)

path에 `robotId|sessionId|insightId|reportId` 만 등장하는 endpoint — fleet ID는 DB lookup 필요:

- `DELETE /api/v1/robots/{robotId}` — robot.fleet_id JOIN.
- `POST /api/v1/robots/{robotId}/credential:rotate` — robot.fleet_id JOIN.
- `POST /api/v1/scans/{sessionId}:cancel` — scan_session.fleet_id JOIN.
- `POST /api/v1/reports/{reportId}:verify` — report.fleet_id JOIN.
- `POST /api/v1/insights/{insightId}:dismiss` — insight.fleet_id JOIN.

**평가 흐름**: middleware가 path param 추출 → 해당 도메인 repo lookup (read-only Tx) → fleet_id 추출 → Subject.FleetID 주입 → PDP 평가.

**기술적 어려움**:
- middleware가 도메인 repo를 호출 — DDD 경계 §5 (도메인 서비스가 다른 도메인 repo 직접 호출 금지) 위반 risk.
- 해결: `internal/platform/authz/scoperesolver.go` 같은 평가용 thin lookup 인터페이스 추가 — middleware는 인터페이스만 호출, 실 lookup은 application service가 주입(deps에 ScopeResolver). Subject 도메인을 거치지 않으므로 DDD 경계 보존.
- 추가 cost: mutation마다 1 DB read (small index lookup, ms 단위). cache는 §3.1.5 cache 정책에서.

#### 3.1.3 다중 binding 일치 (정책 강화)

사용자가 다음 binding을 동시에 보유:
- `{owner, tenant, ""}` — 모든 fleet implicit
- `{operator, fleet, "fleet_A"}`
- `{operator, fleet, "fleet_B"}`

PDP `Decide`는 첫 일치 binding을 보고 ALLOW를 반환합니다 (`internal/platform/authz/decision.go::40~69`). 정확하지만:
- ALLOW reason은 첫 일치 binding의 role/scope만 — audit 로그상 정확성은 OK이나 운영자가 "왜 통과했는지" 디버깅 시 모든 일치 binding을 보고 싶을 수 있음.
- DENY 시 reason은 "no binding allows" — 사용자가 어떤 binding을 갖고 있고 어떤 게 부족한지 명시 X.

**개선 옵션**:
- A) 현 동작 유지 — 단순.
- B) `Decision` 에 `MatchedBindings []RoleBinding` 추가 — 모든 일치 binding 노출. 디버깅 친화. 본 doc 권장 (D-RBACEX-2).

### 3.2 SSO group → role 자동 매핑

#### 3.2.1 OIDC claim 매핑

OIDC IdP 표준 claim:
- `groups: ["fleet-admin-warehouse-a", "operator-warehouse-b"]` — group 이름 슬라이스.
- `roles: ["admin"]` — role 이름 슬라이스 (Auth0/Okta 표준).

**매핑 정책 옵션**:
- A) **명시 mapping 테이블** — provider config에 `{"groupRoleMap": {"fleet-admin-warehouse-a": {"role": "fleet-admin", "scope": "fleet", "fleetId": "flt_warehouse_a"}}}`.
- B) **naming convention** — group 이름 패턴 `<role>-<fleet-slug>` 자동 파싱 + fleet_slug → fleet_id resolve. 단점: fleet 이름 변경 시 매핑 누락.
- C) **hybrid** — 정확 매핑이 우선, naming convention은 fallback.

권장: **A (명시 mapping)** — 결정론 + customer 합의 명확. naming convention은 onboarding wizard helper로 별 작업.

#### 3.2.2 SAML attribute 매핑

SAML attribute는 OIDC claim과 1:1 — 보통 `MemberOf` 또는 `Groups` attribute에 group 이름 리스트가 옵니다.

**매핑 정책**: OIDC와 동일 mapping 테이블 — provider config에 `{"groupAttribute": "MemberOf", "groupRoleMap": {...}}` 형식.

#### 3.2.3 role 갱신 시점

옵션:
- A) **login 시 매번** — CompleteSSOLogin이 매번 user_roles row 동기화 (insert + delete). 가장 신선.
- B) **login 시 매번 + cache** — claim 셋이 동일하면 skip (해시 비교).
- C) **login 시 매번 sync + scheduled refresh job** — IdP 변경을 active 세션에도 반영 (token 만료까지 기다리지 않고 강제 reissue).

권장: **A** (Phase 5). C는 customer 요청 시 Phase 6+.

### 3.3 정밀화 후 PDP 동작 매트릭스 (재요약)

| 카테고리 | endpoint 수 | fleet ID 출처 | middleware 변경 |
|---|---|---|---|
| tenant 글로벌 자연 일치 | 9 | — | 변경 없음 (Stage 4 그대로) |
| path fleetId | 2 | chi.URLParam | 변경 없음 |
| body fleetId | 2 | body peek 또는 handler 재평가 | §4 옵션 결정 |
| cross-resource lookup | 5 | repo lookup | ScopeResolver 인터페이스 신설 |
| 정책 결정(snapshot) | 1 | D-RBACEX-3 | snapshot용 lookup 또는 tenant 유지 |

---

## 4. 합성 전략 옵션

### 4.1 옵션 A — fleet scope 정밀화만

§3.1 7 endpoint를 정밀 평가까지 결선. SSO group 매핑은 별 epic으로 보류.

**Pros**:
- 직전 RBAC epic의 자연스러운 마무리. 같은 코드 영역 — context cost 낮음.
- enterprise customer 첫 진입 시 fleet 격리 정확성 보장 (보안 약속 일관).
- 회귀 위험 제한적 — middleware factory 시그니처 추가, 기존 PDP 결정 테이블 무변경.

**Cons**:
- SSO 자동화 가치 누락 — customer 운영 부담 지속.

**회귀 위험**: 중. body peek은 io.ReadAll 패턴이 표준이지만 large body·streaming·multipart endpoint(없지만 미래 확장)에 영향. cross-resource lookup은 mutation 추가 latency (~1~5ms). 통합 테스트 매트릭스 확장 필요 (Stage 4 195 sub-test → 약 290 case 추정 — fleet_A vs fleet_B 분기 곱).

**추정 시간**: 5~6일 (보수적, memory `feedback_design_doc_conservative.md`).

### 4.2 옵션 B — SSO group 매핑만

§3.2를 별 epic으로 결선. fleet scope 정밀화는 보류.

**Pros**:
- SSO 통합 완성도 ↑ — customer onboarding 자동화. Okta·Azure AD·Google Workspace 표준 통합.
- 코드 변경 영역 작음 (sso 패키지 + handlers/sso.go + 마이그레이션 1건).
- enterprise sales 진입 친화 — RFP 항목 cover.

**Cons**:
- fleet 격리 한계 지속 — fleet-admin@fleet_A가 robot/scan을 fleet_B에 생성 가능 (PDP가 fleet 매칭 skip).
- "보안" 카테고리 미흡 — 자동화는 좋지만 격리가 안 되면 enterprise 의심.

**회귀 위험**: 중. provider config 스키마 확장(JSON additive — 호환). user_roles 자동 sync는 기존 admin 수동 binding 충돌 처리 필요(D-RBACEX-7).

**추정 시간**: 3~4일.

### 4.3 옵션 C — 둘 다 (권장)

§3.1 + §3.2 모두 결선. 직전 RBAC epic의 자연스러운 종결.

**Pros**:
- 보안(fleet 격리) + 자동화(group 매핑) 동시 달성 — enterprise 진입에 필요한 두 축 일관 마감.
- 같은 PDP·middleware 영역 — context 1회로 두 축 cover 시 효율적. 별 epic 분리 시 context switching cost 발생.
- 통합 테스트 매트릭스도 한 번에 — 페르소나 × fleet 격리 × group 매핑 1 epic으로 cover.

**Cons**:
- 시간 ↑ — 8~10일 (보수적). context 한도 risk.
- 두 축 동시 결선 시 회귀 발견 시 원인 진단 부담 ↑ — Stage 분해 필수.

**회귀 위험**: 중~고. 마이그레이션 2건(SSO mapping + 가능 시 audit kind 추가). 통합 테스트 매트릭스 확장 (페르소나 5 × fleet 2 × endpoint 24 + SSO group 매핑 5 case ≈ 245 case).

**추정 시간**: **8~10일** (Stage 5분해 + 보수적 추정 — memory `feedback_design_doc_conservative.md`).

### 4.4 옵션 D — 보류

본 doc 자체로 정리만 마치고 paying customer 진입 후 우선순위 결정.

**Pros**:
- 즉시 cost 0 — 다른 백로그 우선 진행 가능.
- customer 요구가 fleet 격리 / SSO 자동화 중 어느 쪽인지 학습 후 결정 — YAGNI.

**Cons**:
- 직전 RBAC epic의 미해결 issue 지속 (handlers.go::280~282 코멘트의 "정밀 fleet scope 결정은 후속 stage" 약속 미이행).
- 첫 paying customer 진입 시 격리 부재가 deal-breaker일 수 있음.
- 본 doc은 어차피 작성 — Stage 진입 합의가 미뤄지면 다음 세션 부담 동일.

**추정 시간**: 0일.

---

## 5. 권장 옵션 + 근거

### 5.1 권장: 옵션 C (둘 다, Stage 분해 + 점진 적용)

**근거**:

1. **enterprise 진입 두 축 동시 cover** — fleet 격리(보안) + group 매핑(자동화)는 enterprise customer 인터뷰에서 가장 자주 묶여 등장. 별 epic으로 분리하면 customer 인지 시점이 어긋남.
2. **같은 PDP·middleware 영역** — context cost 효율. SSO 매핑은 user_roles row 생성 ↔ fleet scope 정밀화는 user_roles row 평가 — 두 축이 한 데이터 모델을 공유.
3. **회귀 위험 관리 가능** — Stage 분해 5단계, 각 단계 자체 검증. 직전 epic이 같은 패턴(5 stage)으로 회귀 0 마감했으므로 재현 가능.
4. **권고 시점** — 직전 epic 마감 직후가 PDP 코드 컨텍스트 신선. 시간이 흐르면 컨텍스트 재로드 cost 발생.
5. **첫 enterprise customer 직전 분리(D5 R30-4)** 시점 — 두 축 모두 코어 Apache-2.0에 포함이 자연스럽다 (multi-tenancy 기본 격리 + 표준 SSO 통합). enterprise tier로 분리되는 가치는 dynamic policy / role builder UI / SCIM에 둠.
6. **추정 시간 8~10일** — 1주+ 작업이지만, 별 epic 2번(5~6일 + 3~4일 = 8~10일 + context 재로드 1~2일)보다 효율.

**옵션 A 재검토 사유 부재**: 옵션 C가 옵션 A를 포함합니다. SSO 매핑 비대 위험은 §6 Stage 분해로 격리.

**옵션 B 비채택 사유**: fleet 격리 한계가 enterprise 의심 카테고리. SSO 자동화만 진행해도 격리 부재가 상쇄.

**옵션 D 비채택 사유**: 직전 epic의 미해결 약속(handlers.go 코멘트) 지속. customer 진입 timing이 가까울수록 Stage 진입 cost는 동일.

---

## 6. 변경 사항 outline (옵션 C 채택 가정)

### 6.1 마이그레이션 (1건)

`internal/platform/storage/migrations/sqlite/0029_sso_group_mapping.sql`:
```sql
-- SSO provider별 group → role binding 매핑 테이블.
-- provider.config JSON 안에 둘 수도 있으나, 별 테이블로 분리하면 관계형 쿼리·index가 가능.
CREATE TABLE sso_group_role_mappings (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    provider_id   TEXT NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    group_value   TEXT NOT NULL,        -- IdP claim group 값 ("fleet-admin-warehouse-a")
    role_id       TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    scope_type    TEXT NOT NULL DEFAULT 'tenant',
    scope_id      TEXT NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    UNIQUE(provider_id, group_value, role_id, scope_type, scope_id)
);
CREATE INDEX sso_group_role_mappings_provider ON sso_group_role_mappings(provider_id);
```

PG 마이그레이션은 동등 — `INTEGER` → `BIGINT`.

### 6.2 신규 파일

- `internal/platform/authz/scoperesolver.go` — fleet scope cross-resource lookup 인터페이스:
  ```go
  type ScopeResolver interface {
      ResolveRobotFleet(ctx context.Context, tx storage.Tx, robotID string) (fleetID string, err error)
      ResolveScanFleet(ctx context.Context, tx storage.Tx, sessionID string) (fleetID string, err error)
      ResolveInsightFleet(ctx context.Context, tx storage.Tx, insightID string) (fleetID string, err error)
      ResolveReportFleet(ctx context.Context, tx storage.Tx, reportID string) (fleetID string, err error)
  }
  ```
- `internal/platform/authz/scoperesolver_test.go` — 단위 테스트 (mock repo).
- `internal/api/handlers/rbac_scope_extractor.go` — body fleetId peek + ScopeResolver 호출 wrapper:
  ```go
  type fleetExtractor func(*http.Request) (fleetID string, err error)
  func (h *Handlers) FleetFromBody(field string) fleetExtractor { ... }
  func (h *Handlers) FleetFromRobotID(paramName string) fleetExtractor { ... }
  // ...
  ```
- `internal/api/handlers/rbac_scope_test.go` — middleware 통합 테스트 (페르소나 × endpoint × fleet 분기).
- `internal/domain/tenant/sso/group_mapping.go` — `GroupRoleMapping` 도메인 타입 + Service 인터페이스 (CRUD + ResolveBindingsForGroups).
- `internal/domain/tenant/sso/group_mapping_test.go`.
- `internal/domain/tenant/sso/sqliterepo/group_mapping.go` — repo 구현 (sqliterepo 분리는 직전 epic 관례).
- `internal/api/handlers/sso_group_mapping.go` — CRUD HTTP 핸들러 + sync 흐름 진입점.

### 6.3 수정 site

- **fleet scope 정밀화**:
  - `internal/api/handlers/rbac_middleware.go::RequirePermission` — 시그니처 확장: 기존 `RequirePermission(resource, action)` 보존 + 신규 `RequirePermissionWithFleet(resource, action, extractor fleetExtractor)`. extractor가 nil이면 기존 동작.
  - `internal/api/handlers/handlers.go::282~308` — robot/scan/insight 7건 mount를 새 시그니처로 교체:
    ```go
    r.With(h.RequirePermissionWithFleet(authz.ResourceRobot, authz.ActionWrite, h.FleetFromBody("fleetId"))).
        Post("/api/v1/robots", ...)
    r.With(h.RequirePermissionWithFleet(authz.ResourceRobot, authz.ActionWrite, h.FleetFromRobotID("robotId"))).
        Delete("/api/v1/robots/{robotId}", ...)
    // ... (5 cross-resource endpoint 동일 패턴)
    ```
  - `internal/api/handlers/handlers.go::Deps` — `ScopeResolver` 필드 추가, main.go에서 주입.
  - `internal/api/handlers/dependencies.go` (있을 시) — Deps 빌더 갱신.
  - `internal/platform/authz/decision.go::Decision` — `MatchedBindings []RoleBinding` 추가 (D-RBACEX-2 권장 default 채택 시).
  - `cmd/rosshield-server/main.go` — Deps에 ScopeResolver 인스턴스 주입(robot·scan·insight·report repo wrap).

- **SSO group 매핑**:
  - `internal/domain/tenant/sso/oidc.go::IDTokenClaims` — `Groups []string` 추가 + `claimsFromMap` 에 group 추출.
  - `internal/domain/tenant/sso/saml.go::SAMLAssertion` — 이미 Attributes map 보유, `GroupAttributeKey` 가 cfg에서 추출되도록 확장.
  - `internal/domain/tenant/sso/sso.go::CompleteLoginResult` — `GrantedBindings []tenant.RoleBinding` 추가 (sync 결과).
  - `internal/api/handlers/sso.go::CompleteSSOLoginOIDC|SAML` — group 추출 → GroupMappingService.ResolveBindings → tenant.Service.SyncRoleBindings 호출. audit emit `role.sso_synced`.

### 6.4 단위·통합 테스트

- **단위 (fleet scope)**: `scoperesolver_test.go` — mock repo로 robot/scan/insight/report fleet 추출.
- **단위 (group 매핑)**: `group_mapping_test.go` — claim 셋 + mapping → binding 슬라이스 결정론.
- **단위 (PDP)**: `decision_test.go` 확장 — `MatchedBindings` 다중 일치 case.
- **통합 (fleet scope)**: `rbac_scope_test.go` — 5 페르소나 × 7 endpoint × {fleet_A, fleet_B} = 70 case (testcontainers).
- **통합 (SSO sync)**: `sso_group_sync_test.go` — OIDC mock IdP + group claim → 자동 binding 생성.
- **회귀**: 기존 `rbac_integration_test.go` 195 sub-test PASS 보장 — middleware 시그니처 호환.
- **e2e (web)**: Playwright 또는 vitest — fleet[A] operator로 로그인 → fleet[B] robot create body 보내면 403 + reason 표시.

---

## 7. TDD Stage 분해 (5 commit)

원칙 §12 (점진 적용) — big-bang 금지. 각 stage 자체 검증 가능.

### Stage 1 — `ScopeResolver` 인터페이스 + `MatchedBindings` PDP 확장

**산출**:
- `internal/platform/authz/scoperesolver.go` 인터페이스 정의 (구현 없이).
- `internal/platform/authz/decision.go::Decision.MatchedBindings` 추가 + 다중 일치 평가 로직.
- 단위 테스트 — mock resolver + 다중 binding 일치 결정.

**테스트**:
- `TestDecisionMatchedBindings_MultipleAllow` — 사용자가 owner+operator@A+operator@B 보유 시 fleet_A 평가 → MatchedBindings에 owner와 operator@A 모두 포함.
- `TestScopeResolverInterface` — 인터페이스 선언만 검증 (compile-time).

**검증**: `go test ./internal/platform/authz/... -count=1` PASS + 기존 결정 테이블 회귀 0.

**시간**: 0.5일.

### Stage 2 — `RequirePermissionWithFleet` middleware factory + body peek + cross-resource lookup

**산출**:
- `internal/api/handlers/rbac_middleware.go::RequirePermissionWithFleet(resource, action, extractor)` 신규.
- `internal/api/handlers/rbac_scope_extractor.go` — `FleetFromBody`, `FleetFromRobotID`, `FleetFromScanID`, `FleetFromInsightID`, `FleetFromReportID` 5종.
- 기존 `RequirePermission` 보존 — extractor nil이면 fleetIDFromRequest fallback (옛 호출 호환).
- handlers.go::Deps에 `ScopeResolver` 필드 추가.

**테스트**:
- `TestFleetFromBody_RobotsCreate` — body `{"fleetId": "flt_A", ...}` peek + Subject.FleetID="flt_A".
- `TestFleetFromBody_BodyRereadable` — middleware peek 후 handler가 동일 body 다시 파싱 가능.
- `TestFleetFromRobotID_RepoLookup` — mock resolver → robot.fleet_id 추출 → Subject.FleetID 주입.
- `TestRequirePermissionWithFleet_NoExtractor_FallbackToPath` — extractor=nil → 기존 fleetIDFromRequest 동작 (회귀).

**검증**: `go test ./internal/api/handlers/... -count=1` PASS + Stage 4 통합 테스트 회귀 0.

**시간**: 1.5일.

### Stage 3 — handlers.go 7 endpoint mount 교체 + 통합 테스트

**산출**:
- handlers.go::282~324 7 endpoint mount를 `RequirePermissionWithFleet` 로 교체:
  - POST /robots (body), DELETE /robots/{robotId} (cross), POST /robots/{robotId}/credential:rotate (cross), POST /scans (body), POST /scans/{sessionId}:cancel (cross), POST /reports/{reportId}:verify (cross), POST /insights/{insightId}:dismiss (cross).
- `cmd/rosshield-server/main.go` — Deps에 ScopeResolver 인스턴스 주입.
- 통합 테스트 매트릭스 확장 — `rbac_scope_test.go` 5 페르소나 × 7 endpoint × 2 fleet = 70 case.

**테스트**:
- `TestFleetIsolation_OperatorA_DenyFleetBRobotCreate` — operator@fleet_A 토큰 + body fleetId=fleet_B → 403.
- `TestFleetIsolation_FleetAdminA_AllowOwnFleetScanCancel` — fleet-admin@fleet_A + scan_session(fleet_A) cancel → 200.
- `TestFleetIsolation_AdminAlwaysAllow` — admin tenant scope → 모든 fleet endpoint 200 (회귀).
- 회귀: 기존 `rbac_integration_test.go` 195 sub-test 모두 PASS.

**검증**: `go test ./internal/api/handlers/... -count=1` PASS + `make ci`.

**시간**: 2일.

### Stage 4 — SSO group 매핑 도메인 + 마이그레이션 0029 + provider config 확장

**산출**:
- 마이그레이션 0029 (sso_group_role_mappings 테이블) — sqlite + pg.
- `internal/domain/tenant/sso/group_mapping.go` — `GroupRoleMapping` 타입 + `GroupMappingService` 인터페이스.
- `internal/domain/tenant/sso/sqliterepo/group_mapping.go` — repo 구현 (CRUD + ResolveBindingsForGroups).
- `internal/domain/tenant/sso/oidc.go::IDTokenClaims.Groups` 추출 + `claimsFromMap` 갱신.
- `internal/domain/tenant/sso/saml.go::SAMLConfig.GroupAttribute` (옵션 필드) — gosaml2 attribute에서 group 추출.

**테스트**:
- `TestGroupMappingResolveBindings_OIDCGroups` — 매핑 테이블 + claim groups → binding 슬라이스.
- `TestGroupMappingResolveBindings_SAMLAttribute` — SAML attribute MemberOf → binding 슬라이스.
- `TestGroupMappingCRUD_RepoIdempotent` — UNIQUE 제약 + 중복 INSERT 시 멱등.

**검증**: `go test ./internal/domain/tenant/sso/... -count=1` PASS.

**시간**: 1.5일.

### Stage 5 — SSO callback group sync 결선 + audit emit + 통합 테스트 + web admin UI

**산출**:
- `internal/api/handlers/sso.go::CompleteSSOLoginOIDC|SAML` — group 추출 + GroupMappingService.ResolveBindings + tenant.Service.SyncRoleBindings 호출.
- `internal/domain/tenant/tenant.go::Service.SyncRoleBindings(userID, bindings []RoleBinding)` 신규 — 전체 swap (delete-all-from-source + insert).
- audit kind 신설: `role.sso_synced` (before/after binding diff).
- `internal/api/handlers/sso_group_mapping.go` — admin이 매핑 CRUD 가능 (POST/PUT/DELETE /api/v1/sso/providers/{id}/group-mappings).
- `web/src/pages/sso/group-mappings.tsx` — provider 상세 페이지 안 매핑 관리 UI.

**테스트**:
- `TestSSOLoginAutoBindings_OIDC` — mock IdP id_token에 groups claim → user_roles row 자동 INSERT.
- `TestSSOLoginAutoBindings_SAML` — SAML attribute MemberOf → 동일 결과.
- `TestSSOLoginRevokesPreviousBindings` — IdP에서 group 제거 → 다음 login에서 binding row 삭제 (D-RBACEX-7 권장 default).
- 회귀: 기존 SSO 테스트 PASS.

**검증**: `go test ./internal/{api/handlers,domain/tenant}/... -count=1` PASS + `make ci` + vitest.

**시간**: 2일.

**총 추정**: **7~8일** 코어 + 1~2일 buffer (회귀·문서) = **8~10일**. memory `feedback_design_doc_conservative.md` 일관 — 보수적 lower bound.

---

## 8. 결정 항목 (D-RBACEX-1 ~ D-RBACEX-10)

### D-RBACEX-1 — body fleetId 추출 패턴

- **A) middleware peek + body 재복원** — `io.ReadAll` 후 `r.Body = io.NopCloser(bytes.NewReader(buf))`. 표준 패턴, handler 코드 무수정. **권장 default**.
- B) handler 명시 PDP 재호출 — middleware는 tenant scope만 평가, handler가 body 파싱 후 `authz.Decide` 두 번째 호출. middleware 단순. handler 코드 분산.
- C) request decoder 일원화 — 별 wrapper(decodeAndAuthorize) 함수가 body 파싱 + PDP 평가 동시 수행. 새 추상화 필요.

**근거**: A는 middleware 일관성 + handler 코드 무영향. body 크기 ≤ 16KB 가정 (POST /robots, /scans 모두 small JSON).

### D-RBACEX-2 — cross-resource lookup 정책

- **A) ScopeResolver 인터페이스 + 도메인 repo wrap** — middleware는 인터페이스만 호출, deps 주입. DDD 경계 §5 보존. **권장 default**.
- B) middleware가 storage.Tx 직접 사용해 SQL 실행 — 빠르나 중복 코드 + 도메인 우회.
- C) handler 사전 lookup + ctx에 fleetID 주입, middleware는 ctx만 추출 — handler 책임 비대.

**근거**: A는 도메인 경계 + 테스트 친화 (mock resolver). lookup latency 1~5ms 수용 가능.

### D-RBACEX-3 — compliance snapshot scope

- A) 현행 tenant 글로벌 유지 — fleet 무관 1개 snapshot.
- **B) fleet 단위 — profile.fleet_id 도입 + snapshot 실행 권한 fleet 단위** — 직전 epic D-RBAC-4 권장 default 일관. **권장 default**.
- C) hybrid — admin은 tenant, fleet-admin은 fleet 단위.

**근거**: 직전 epic 결정 일관 (Stage 4 mount 시 fleet scope로 의도). 본 epic Stage 3에서 결선.

### D-RBACEX-4 — 다중 binding 일치 reason 노출

- A) 현 단일 MatchedRole만 — 단순.
- **B) `Decision.MatchedBindings []RoleBinding` 추가 — 모든 일치 binding 노출** — 디버깅 친화. **권장 default**.
- C) Reason 문자열에만 모두 포함, 구조 변경 X — log grep 친화하나 client에서 분기 어려움.

**근거**: B는 audit 로그 + admin UI에서 "왜 통과했는지/막혔는지" 명시. server response body는 그대로(Reason 1줄만), Decision struct만 확장.

### D-RBACEX-5 — SSO group naming convention

- **A) 명시 mapping 테이블 (sso_group_role_mappings)** — 결정론. customer 합의 명확. **권장 default**.
- B) naming convention `<role>-<fleet-slug>` 자동 파싱 — 운영 편의. fleet 이름 변경 시 매핑 누락 risk.
- C) hybrid — 명시 매핑 우선, naming convention fallback.

**근거**: A는 audit 친화 (모든 매핑이 DB row으로 조회 가능). naming convention은 onboarding helper로 별 작업.

### D-RBACEX-6 — role 갱신 시점

- **A) 매 login 시 매번 sync** — 가장 신선. SSO 통합 표준 패턴. **권장 default**.
- B) 매 login + 토큰 만료 시 cache (claim 셋 해시 비교) — 부하 ↓. 신선도 ↓.
- C) login 시 sync + scheduled refresh job (active 세션도 강제 reissue) — IdP 변경 즉시 반영. 운영 부담 ↑.

**근거**: A는 Phase 5 적정. C는 customer 요청 시 Phase 6+.

### D-RBACEX-7 — 기존 수동 binding과 자동 sync 충돌

- A) auto sync가 admin 수동 binding을 덮어씀 — 일관성 단순. customer 운영자 binding 분실 risk.
- **B) auto sync는 자동 생성된 binding(source='sso')만 sync, 수동 binding(source='manual')은 보존** — `user_roles.source` 컬럼 추가 (마이그레이션 0029 또는 0030). **권장 default**.
- C) auto sync는 추가만, 삭제 안 함 — sync 의미 약함, IdP 해임 시 binding 누락.

**근거**: B는 두 운영 패턴(자동 + 수동) 공존. source 컬럼 추가로 origin 추적 가능 — audit 친화.

### D-RBACEX-8 — IdP claim → fleet ID resolve

- A) mapping 테이블에 fleet_id 직접 — customer가 fleet ID(ULID)를 알아야 함. 운영 부담.
- **B) mapping 테이블에 scope_type/scope_id 둘 다 명시 — admin이 fleet 선택 UI로 매핑 생성** — 직관. **권장 default**.
- C) IdP claim에 fleet_slug 포함 + DB resolve — 매핑 자유도 ↑. naming convention 부담.

**근거**: B는 web admin UI에서 dropdown 선택 — customer 친화. fleet 이름 변경에도 매핑 안정 (fleet_id ULID 불변).

### D-RBACEX-9 — body peek 실패 처리

- A) body 파싱 실패 → 400 (middleware 단계 거부) — handler 진입 차단.
- **B) body 파싱 실패 → fleetID 빈 값으로 PDP 평가 (tenant scope binding만 통과)** — handler가 별도 검증. 기존 동작 일관. **권장 default**.
- C) body 파싱 실패 → 500 — 안전하지만 사용자 친화 X.

**근거**: B는 middleware 책임 최소화 (인가만, 파싱은 handler). PDP가 빈 fleetID로 평가하면 fleet binding은 자동 거부 → fleet-admin만 가진 사용자는 자연 403.

### D-RBACEX-10 — cross-resource lookup 캐시

- **A) cache 0 — 매 mutation마다 DB read 1회** — SQLite 인덱스 lookup 1ms. 정합성 명확. **권장 default**.
- B) per-request cache (ctx에 robotID→fleetID 저장) — 동일 요청에서 multiple lookup 회피. mutation 1회면 가치 0.
- C) tenant-wide LRU cache — 부하 ↓. invalidation 복잡 (robot 이동 시).

**근거**: A는 단순 + 정확. mutation 빈도 ≤ 100 req/s 가정에서 부하 무시. 캐시 도입은 부하 측정 후 별 작업 (Phase 6).

---

## 9. 회귀 위험 / 운영 고려

### 9.1 직전 RBAC 5 epic 영향

- **Stage 1 결정 테이블** — `MatchedBindings` 추가는 additive (기존 단일 MatchedRole 보존). 단위 테스트 회귀 0.
- **Stage 2 마이그레이션 0028** — 본 epic 마이그레이션 0029는 별 테이블, 영향 0.
- **Stage 3 JWT bindings claim** — 변경 없음. group sync는 user_roles row만 갱신, JWT는 다음 발급 시 자연 반영.
- **Stage 4 handlers.go 24 mutation gate** — 7 endpoint mount만 시그니처 교체 (`RequirePermission` → `RequirePermissionWithFleet`). 9 tenant 글로벌 + 2 path-fleet + 1 compliance(D-RBACEX-3 결정에 따라)는 무변경. Stage 4 195 sub-test 회귀 0 보장 — 단, 7 endpoint sub-test의 fleet 분기 case는 Stage 3 통합 테스트로 대체.
- **Stage 5 web useHasPermission** — 변경 없음. fleet scope 평가는 server에서 강제, web mirror는 UX 친화로 동일 결정 (이미 fleetId 전달 가능).

### 9.2 customer 데이터 마이그레이션

- **0029 sso_group_role_mappings 테이블** — 신규 테이블, 기존 데이터 0. customer 작업 0.
- **0030 user_roles.source 컬럼** (D-RBACEX-7 채택 시) — `ALTER TABLE ... ADD COLUMN source TEXT NOT NULL DEFAULT 'manual'`. 기존 row은 'manual' — admin 수동 binding 의미 보존. 자동 sync는 source='sso' INSERT.
- 0029 + 0030 모두 forward-only — rollback은 컬럼 보존 (SQLite ALTER DROP 회피).

### 9.3 web client mirror 동기화

- `web/src/lib/authz/policy.ts` — SystemRolePermissions 매트릭스 변경 없음 (본 epic은 결정 테이블 무변경). 동기화 부담 0.
- `web/src/api/hooks.ts::useHasPermission(resource, action, fleetId?)` — fleetId 파라미터 이미 지원. 변경 없음.
- 신규: SSO group mapping 관리 UI (`web/src/pages/sso/group-mappings.tsx`) — Stage 5에서 추가.

### 9.4 cache 정합성

- per-request body peek은 stateless — cache 정합성 N/A.
- cross-resource lookup은 매 mutation 1 read — cache 미도입(D-RBACEX-10).
- JWT bindings claim은 사용자 세션당 1회 발급 — group sync 후 다음 발급까지 stale (15분~1h). 즉시 반영 필요 시 admin이 강제 재로그인 (별 운영 가이드).
- React Query 인증 store invalidation — login 후 me 응답에 새 bindings 자동 반영.

### 9.5 audit chain 영향

- 신규 audit kind 2~3개:
  - `role.sso_synced` — group sync 후 binding diff (added/removed).
  - `sso.group_mapping.created|updated|deleted` — admin 매핑 CRUD.
- 기존 audit entry 영향 0 — append-only 보존.
- audit replay 시 신규 kind 미인식 risk → Stage 4 audit replay 테스트 필수.

### 9.6 SSO sync 실패 처리

- IdP가 group claim을 보내지 않으면 (예: 신규 사용자 group 미할당) → SyncRoleBindings는 빈 슬라이스 → 기존 source='sso' binding 모두 삭제 + source='manual' 보존. user는 manual binding이 없으면 어떤 endpoint도 통과 못 함 — 명시적 401 reason "no role bindings".
- group 매핑 CRUD 실패는 admin UI에 명시.
- IdP 호출 실패 (network) — 본 epic 비대상 (E20 외부 dep 정책).

### 9.7 multi-tenant scaling

- `sso_group_role_mappings` row — provider 1 × group 50 × role 5 = 250 row. tenant 100개면 25K row. INDEX (provider_id) 충분.
- `user_roles` row — group sync로 자동 binding이 늘어남. 50 fleet customer + 100 user면 5K row. 직전 epic §9.5 가정(150K) 안.

### 9.8 dev·prod 차이

- 데스크톱 single-user — SSO 미사용 가정. group 매핑 변경 0.
- 어플라이언스 — 1~5 user, SSO 옵션. group 매핑 0~5건.
- enterprise prod — 본 epic 1차 타깃.

### 9.9 first paying customer 진입 영향

- D5 R30-4 "open-core" — 본 epic 두 축 모두 코어 Apache-2.0 권장 (multi-tenancy 기본 격리 + 표준 SSO 통합).
- enterprise tier로 분리될 가치는 dynamic policy / role builder UI / SCIM / IdP-pushed provisioning.

---

## 10. 참조

### 본 리포 design doc
- `docs/design/notes/rbac-fine-grained-design.md` — 직전 epic 568줄, Stage 1~5 마감.
- `docs/design/05-api-and-auth.md` — 인증/인가.
- `docs/design/06-security-and-tenancy.md` — 멀티테넌시 격리 원칙.
- `docs/design/01-principles.md` §4 멀티테넌시, §5 DDD 경계, §12 점진 적용.
- `docs/design/04-domain-and-data-model.md` — Role/Permission/User_Role SQL 스키마.

### 본 리포 코드 — fleet scope 정밀화
- `internal/api/handlers/handlers.go::236~349` — 24 mutation mount + 직전 Stage 4 코멘트.
- `internal/api/handlers/rbac_middleware.go::70~161` — RequirePermission factory + fleetIDFromRequest 헬퍼 + bindingsForSubject.
- `internal/platform/authz/decision.go::35~75` — Decide PDP 진입점.
- `internal/platform/authz/policy.go::128~141` — RoleBinding/Subject 타입.
- `internal/platform/authz/permission_matrix.go` — 6 시스템 role × 9 resource × 6 action 매트릭스.
- `internal/api/handlers/robot.go::333~348` — createRobotRequest body 형식 (FleetID 필드).
- `internal/api/handlers/scan.go::40~45,127~128` — startScanRequest body 형식.

### 본 리포 코드 — SSO group 매핑
- `internal/domain/tenant/sso/sso.go::97~105` — ExternalIdentity 매핑.
- `internal/domain/tenant/sso/oidc.go::88~102,496~533` — IDTokenClaims + claimsFromMap (Groups 추출 site).
- `internal/domain/tenant/sso/saml.go::84~89,151~167` — SAMLAssertion.Attributes (group attribute 추출 site).
- `internal/api/handlers/sso.go::124~205` — CompleteSSOLogin OIDC/SAML — group sync hook 진입점.
- `internal/domain/tenant/sqliterepo/repo.go::169~195` — AssignRole + AssignRoleScoped (sync 위임).

### 본 리포 코드 — web mirror
- `web/src/lib/authz/policy.ts::1~141` — client PDP mirror.
- `web/src/api/hooks.ts::useHasPermission` — fleetId 파라미터 이미 지원.

### 메모리·결정
- memory `feedback_design_doc_first.md` — 큰 작업 design doc 우선.
- memory `feedback_design_doc_conservative.md` — 보수적 추정.
- memory `feedback_parallel_agents.md` — 본 epic은 stage 의존성 강함 (Stage 1→2→3 직렬, Stage 4→5 직렬). Stage 2와 4는 영역 분리되어 부분 병렬 가능.
- D5 R30-4 — open-core 정책, 본 epic 두 축은 코어 Apache-2.0.
- SESSION_HANDOFF Phase 5 backlog — 직전 RBAC epic 마감 직후 후속 entry 추가.

### 외부 (참조 정도)
- Casbin v2 — 직전 epic D-RBAC-1에서 비채택 (자체 PDP 채택). 본 epic도 동일 — 결정 엔진 변경 없음.
- OPA / Rego — 외부 PDP. 본 epic 비대상.
- OIDC Core 1.0 §5.1 standard claims — `groups` claim은 표준 외(Auth0/Okta/Azure AD 확장). provider별 claim 이름 mapping은 §3.2.1 옵션.
- SAML 2.0 §2.7 attribute statement — `MemberOf` / `Groups` / `urn:oid:1.3.6.1.4.1.5923.1.5.1.1` 등 IdP별 attribute 이름 다양 — config에서 customer 명시.
- SCIM 2.0 (RFC 7644) — IdP-pushed provisioning. 본 epic 비대상 (Phase 6+).
