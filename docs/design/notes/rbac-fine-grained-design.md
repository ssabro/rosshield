# 세분 RBAC — fleet 단위 scope + permission tier + auditor read-only 분리 — Design

> **상태**: Phase 5 carryover design (다음 세션 진입점). 본 문서는 코드 0줄 / 마이그레이션 0 / pack 변경 0 — 옵션 비교 + Stage 분해 + 결정 항목 권장 default까지만 마감.
> **참조**:
> - 직전 RBAC stage: Stage 1 `7507106` / Stage 2-A `157f07c` / 2-B `a787506` / 2-C `5d46689` / 2-D `fd2dce5` / 2-E `9a88e40`.
> - 시드 코드: `internal/domain/tenant/tenant.go` (Role/Permission), `internal/domain/tenant/sqliterepo/repo.go::seedSystemRoles`.
> - 설계서: `docs/design/05-api-and-auth.md` (인증·인가), `docs/design/06-security-and-tenancy.md` (멀티테넌시), `docs/design/01-principles.md` §4 (멀티테넌시 기본값) §5 (DDD 경계) §12 (점진 적용).
> - SESSION_HANDOFF: "세분 RBAC — fleet 단위 admin·permission level + auditor read-only 분리. 별 epic, 데이터 모델 변경 가능성. 1주+."
> **R 식별자**: R-RBAC-1 (본 epic 전체) — 결정 항목은 D-RBAC-1~10.

---

## 1. 상태·배경

### 1.1 본 doc 범위

코드 0 / 마이그레이션 0. 옵션 비교 + Stage 분해 + 결정 항목 권장 default까지 마감하여, 다음 세션 진입 시 즉시 사용자 합의 → Stage 1 착수가 가능하도록 합니다.

### 1.2 왜 세분 RBAC가 필요한가

현 RBAC는 **2-tier(admin / 그 외)** 입니다. enterprise 고객 + compliance 환경에서 다음이 막힙니다:

1. **fleet 단위 권한 분리**: "fleet A 운영자는 fleet A robot만 등록·스캔, fleet B는 read도 안 됨"이 표현 불가. SOC 2/ISO 27001 customer는 사업부별 자산 격리 의무가 있고, 한 tenant 안에서 fleet=사업부 매핑이 자연스럽지만 권한이 글로벌이라 의무 위반.
2. **auditor read-only 분리 미완성**: 시스템 역할 `auditor`는 시드돼 있지만(`tenant.go::SystemRolePermissions`, audit/scan/report read 권한) 현 server gate는 admin/auditor 묶음 1개(`backup download` + `system 페이지`)만 분리. 나머지 mutation 19개는 admin 단독 → auditor가 read-only로 운영하려 해도 admin 권한 없는 read 사용자 그룹과 동일.
3. **least-privilege 위반**: SOC operator가 robot 등록도 가능, scan 시작도 가능, report 생성도 가능, SSO provider 추가도 가능. 모두 admin 1개 role에 묶여 있어 "scan만 시작할 수 있는 사람"이 표현 불가.
4. **first paying customer 진입 마찰**: D5 R30-4 "open-core, 첫 enterprise customer 직전 분리" 정책상, 첫 paying customer 전 RBAC 세분화는 enterprise 차별화 가치 + 도입 장벽 동시 해결.

### 1.3 비목표

- **새 인증 방식 추가** — OIDC/SAML/API key는 별 트랙(O5·E33). 본 epic은 인가만.
- **PDP/PEP 분리 외부 서비스화** — Casbin/OPA는 옵션 평가만, 채택 여부는 D-RBAC-1.
- **role 사용자 정의 UI** — Phase 5는 시스템 역할 + 정적 fleet binding까지. role builder UI는 Phase 6+.
- **dynamic policy (시간·IP·MFA 조건)** — ABAC 깊이 들어가지 않음. 본 epic은 RBAC + 단일 attribute(fleetID) 한정.

---

## 2. 현재 상태 진단

### 2.1 Stage 1+2 적용 결과 매트릭스

| 영역 | 현 보호 | 위치 | RBAC 한계 |
|---|---|---|---|
| **server admin gate (mutation)** | admin 단독 19건 | `internal/api/handlers/handlers.go::237~319` `r.Use(h.RequireRole("admin"))` 그룹 | scope 글로벌 — fleet 무관 / auditor·operator 분리 0 |
| **server admin/auditor gate** | admin 또는 auditor | `cmd/rosshield-server/main.go::newMux` backup download (`/api/v1/system/backups/{id}` GET) | 1 endpoint만, mutation 분리 미적용 |
| **server read endpoint** | 인증만 (모든 role) | handlers.go::190~234 (GET /api/v1/robots, scans, fleets, audit, …) | fleet 단위 read 격리 0 — 모든 fleet 자료 동일 사용자 view |
| **web sidebar 가시성** | admin·admin/auditor 메뉴 숨김 | `web/src/components/Sidebar` (Stage 2-D `fd2dce5`) | role 단위만, fleet 단위 0 |
| **web router guard** | `requireRole` beforeLoad | `web/src/lib/route-guards.ts` 3 페이지 (sso·users·system) | 글로벌 role만 |
| **web button conditional render** | useIsAdmin/useIsAdminOrAuditor | `web/src/api/hooks.ts::141~153` 8 페이지 | 글로벌 role만, fleet 컨텍스트 미반영 |
| **JWT claims** | `Roles []string` | `internal/domain/tenant/jwt.go::31` AccessClaims | role 이름만, fleet binding 없음 |
| **DB 스키마** | `roles` (id·tenant_id·name·permissions·is_system) + `user_roles` (user_id·role_id) | `internal/domain/tenant/sqliterepo/repo.go::842~866` | fleet scope 컬럼 없음. permission JSONB 슬롯은 있지만 와일드카드 `*` 단일만 사용 |
| **시드 시스템 role** | admin(`*`), auditor(audit/scan/report read+verify+export+download), operator(robot read·write + scan read·execute + report read) | `tenant.go::SystemRolePermissions` | 정의는 있으나 server middleware는 role 이름만 검사 — permission 슬라이스는 미사용 |

### 2.2 현 mutation 19건 + admin/auditor 1건 분류

| ID | path | 메서드 | 현 gate | 자연스러운 fine-grained 매핑 |
|---|---|---|---|---|
| 1 | `/api/v1/invitations` | POST/DELETE | admin | tenant.users:write |
| 2 | `/api/v1/sso/providers` | POST/PUT/DELETE | admin | tenant.sso:admin |
| 3 | `/api/v1/webhooks` | POST/PUT/DELETE/test | admin | tenant.integration:admin |
| 4 | `/api/v1/robots` | POST | admin | **fleet[X].robot:write** |
| 5 | `/api/v1/robots/{id}` | DELETE | admin | **fleet[X].robot:write** |
| 6 | `/api/v1/robots/{id}/credential:rotate` | POST | admin | **fleet[X].robot:rotate** (sensitive — 별 권한 권장) |
| 7 | `/api/v1/utils/ssh-fingerprint` | POST | admin | tenant.utility (현행 유지 권장) |
| 8 | `/api/v1/scans` | POST | admin | **fleet[X].scan:execute** |
| 9 | `/api/v1/scans/{id}:cancel` | POST | admin | **fleet[X].scan:execute** |
| 10 | `/api/v1/audit/verify` | POST | admin | tenant.audit:verify (admin/auditor) |
| 11 | `/api/v1/reports/{id}:verify` | POST | admin | tenant.report:verify (admin/auditor) |
| 12 | `/api/v1/insights/{id}:dismiss` | POST | admin | **fleet[X].insight:write** |
| 13 | `/api/v1/fleets/{id}/insights:run` | POST | admin | **fleet[X].insight:execute** |
| 14 | `/api/v1/fleets` | POST | admin | tenant.fleet:admin |
| 15 | `/api/v1/fleets/{id}` | PATCH | admin | **fleet[X]:admin** |
| 16 | `/api/v1/fleets/{id}` | DELETE | admin | tenant.fleet:admin |
| 17 | `/api/v1/compliance/profiles` | POST | admin | tenant.compliance:admin |
| 18 | `/api/v1/compliance/profiles/{id}/snapshots` | POST | admin | **fleet[X].compliance:execute** (or tenant — D-RBAC-4) |
| 19 | `/api/v1/system/backups/{id}` | GET | admin/auditor | tenant.system:read |

**관찰**:
- 19건 중 **9건(robot/scan/insight/fleet 단건/compliance snapshot)이 fleet scope에 자연스럽게 떨어짐** — fleet 단위 권한 격리의 1차 가치는 이 9건.
- 6건(invitation/sso/webhook/fleet 생성/compliance profile/system)은 **tenant 글로벌이 자연스러움** — fleet 단위 분해 무의미.
- 3건(audit/report verify, ssh-fingerprint)은 **세부 사용 권한 분리(verify-only role)** 로 가치 발생.

### 2.3 한계 요약

1. **수직 tier 부재** — admin 또는 not. operator/auditor가 시드는 됐지만 server middleware는 role 이름만 매칭, permission 셋은 미활용.
2. **수평 scope 부재** — 모든 권한이 tenant 글로벌. fleet/robot 단위 격리 0.
3. **action별 분리 부재** — admin이 sso, robot, scan, report 모두 가능. "scan만 가능한 SOC operator" 미표현.
4. **client-side 단순 매핑** — useIsAdmin/IsAuditor 4 helper만, fleet 컨텍스트(현재 사용자가 보고 있는 fleet)에 따라 button gate가 달라지는 표현 0.
5. **JWT 부재 binding** — claims.Roles는 role 이름 슬라이스만. fleet binding을 어디 둘지 미정.

---

## 3. 요구 사항 분류 (3축)

### 3.1 수직 — permission tier

| tier | 약칭 | 권한 본질 | 예시 사용자 |
|---|---|---|---|
| **owner** | own | tenant 자체 관리(plan·billing·삭제·tenant config). admin 상위. | tenant 결제·법무 책임자 |
| **admin** | adm | tenant 글로벌 관리 — sso·users·webhook·fleet 생성. 모든 fleet에 admin. | IT manager |
| **fleet-admin** | fadm | 특정 fleet 한정 admin (robot CRUD + scan 실행 + insight 관리 + fleet 설정). | 사업부 SOC lead |
| **operator** | op | 특정 fleet 한정 — robot CRUD + scan execute + insight read. fleet 설정 변경 X. | SOC operator |
| **scanner** | scn | 특정 fleet 한정 — scan execute만. robot CRUD X. | 자동화 봇 / 외부 스캔 트리거 |
| **auditor** | aud | tenant 글로벌 read-only + audit/report verify + export. | 외부 감사인 |
| **read-only** | ro | tenant 글로벌 read-only. verify·export X. | 모니터링 dashboard / 경영진 |

권장 시드 셋: **owner / admin / fleet-admin / operator / auditor / read-only** (6개). scanner는 customer 요구 시 사용자 정의로(Phase 6).

### 3.2 수평 — scope

| scope | 의미 | 예 |
|---|---|---|
| **tenant** | 전체 tenant | `admin`, `auditor`, `read-only` |
| **fleet[X]** | 특정 fleet ID 한정 | `fleet-admin@flt_warehouse_a`, `operator@flt_warehouse_b` |
| ~~**robot[X]**~~ | 특정 robot 한정 | **본 epic 비대상** — fleet 단위면 충분(YAGNI). robot 단위 권한은 Phase 6+ |

**중요**: tenant scope role은 모든 fleet에 implicit 적용. fleet scope role은 명시 fleet에만.

### 3.3 action별 분리 (resource × action 매트릭스)

| resource | read | write | execute | admin | verify | export |
|---|---|---|---|---|---|---|
| **robot** | ro/aud/op/fadm/adm | op/fadm/adm | — | fadm/adm | — | aud/adm |
| **scan** | ro/aud/op/fadm/adm | — | op/fadm/adm | fadm/adm | — | aud/adm |
| **report** | ro/aud/op/fadm/adm | — | — | fadm/adm | aud/adm | aud/adm |
| **insight** | ro/aud/op/fadm/adm | fadm/adm | fadm/adm | adm | — | — |
| **audit** | aud/adm | — | — | — | aud/adm | aud/adm |
| **fleet** (단건) | ro/aud/op/fadm/adm | fadm/adm (settings) | — | adm (생성·삭제) | — | — |
| **compliance** | ro/aud/op/fadm/adm | adm (profile) | fadm/adm (snapshot) | adm | — | aud/adm |
| **sso·webhook·users·invitation** | adm | adm | — | adm | — | — |
| **system (backup·integrity)** | aud/adm | — | — | adm | — | — |

(ro = read-only, aud = auditor, op = operator, fadm = fleet-admin, adm = admin. owner는 모든 칸 implicit 포함.)

---

## 4. 합성 전략 옵션 (4개)

### 4.1 옵션 A — 기존 Role/Permission 시스템 확장 + scope 컬럼

기존 `roles` 테이블과 `Permission` 문자열 형식을 확장합니다.

**스키마 변경**:

```sql
-- 마이그레이션 0027
ALTER TABLE user_roles ADD COLUMN scope_type TEXT NOT NULL DEFAULT 'tenant'; -- 'tenant' | 'fleet'
ALTER TABLE user_roles ADD COLUMN scope_id   TEXT;                            -- NULL when scope_type='tenant', fleet_id when 'fleet'
CREATE INDEX user_roles_scope_idx ON user_roles (user_id, scope_type, scope_id);
```

`Permission` 문자열은 `<resource>.<action>` 형식 유지(이미 정의됨). scope는 user_roles row에 binding.

**런타임 결정 로직**:
```go
// PermissionCheck(user, "scan.execute", fleet="flt_a")
// → user_roles JOIN roles
// → row.Permissions ∋ "scan.execute" || "*"
// → row.scope_type='tenant' OR (row.scope_type='fleet' AND row.scope_id='flt_a')
// → 1+ row 존재 → ALLOW
```

**JWT 영향**:
- 옵션 A1: Roles 슬라이스를 `[]RoleBinding{Name, Scope, ScopeID}`로 확장 → token 크기 ↑.
- 옵션 A2: Roles는 그대로 두고 server-side에서 매 요청 DB lookup → DB load ↑ + 캐시 필요.

**권장**: A1 (token 크기 trade-off가 DB lookup 비용보다 작음. 일반적으로 user당 5~10 role binding).

| pros | cons |
|---|---|
| 기존 코드 호환 — `RequireRole` factory 보존, 새 시그니처(RequirePermission) 추가 | 마이그레이션 1건 + JWT claim 형식 변경(클라이언트 cache 호환 처리 필요) |
| 외부 dep 0 — Casbin/OPA 추가 없음 | 정책 평가 로직을 자체 작성 — Casbin 같은 검증된 알고리즘 미활용 |
| DDD 경계 보존 — tenant 도메인이 PDP 역할, 다른 도메인은 결정만 받음 | scope 추가 차원(시간·IP)이 미래에 필요하면 ABAC로 확장 부담 |
| 데이터 모델 변경 작음 — 컬럼 2개 ADD | role binding 성능 — user_roles row 폭발(1 user × 50 fleet × 3 role = 150 row) 잠재 |

**회귀 위험**: 중. JWT 형식 변경 → 기존 발급 토큰 호환성 처리(D-RBAC-7).
**추정 시간**: 4~5일.

### 4.2 옵션 B — Casbin/OPA 외부 권한 엔진 통합

`github.com/casbin/casbin/v2` (Go-native, 매트릭스 ACL/RBAC/ABAC 모델 모두 지원) 또는 OPA(별 프로세스 + REST/gRPC)를 PDP로 채택.

**구조**:
```
[chi handler] → [PEP middleware]
                    ↓
                [Casbin Enforcer.Enforce(sub, obj, act)]
                    ↓ (model.conf + policy.csv from DB)
                ALLOW/DENY
```

| pros | cons |
|---|---|
| 검증된 정책 엔진 — RBAC + ABAC + 그룹 상속 모두 지원 | 외부 dep 1개 추가 — P10(에어갭 1급)에는 OK(Go-native), 그러나 운영 학습곡선 ↑ |
| 정책 변경이 코드 변경 없이 가능(model.conf/policy.csv 갱신) | 정책 디버깅 — Casbin 정책 파일은 가독성 낮고 customer 이해 어려움 |
| 미래 ABAC 확장(시간·IP·MFA) 무료 | 정책 저장소를 DB로 옮기려면 Casbin Adapter 작성 필요 |
| OPA 채택 시 정책을 Rego로 표현 — 산업 표준 | OPA는 별 프로세스 → 어플라이언스·데스크톱 deployment 복잡도 ↑↑ |

**회귀 위험**: 고. 인증/인가 코드 경로를 모두 PEP로 통과시키는 큰 변경. testcontainers로 모든 endpoint × role 조합 정합성 테스트 필요.
**추정 시간**: 7~10일 (Casbin 통합 + 정책 변환 + 테스트 + 운영 가이드).

### 4.3 옵션 C — ABAC (Attribute-Based) policy decision point 자체 구현

resource attribute(`fleet_id`, `tenant_id`, `created_by`, `tags`) + subject attribute(`role`, `groups`, `mfa_verified`) + action attribute(`request.method`, `clock`)를 입력으로 하는 정책 함수를 작성.

```go
type Decision struct { Allow bool; Reason string }
type Policy func(sub Subject, obj Resource, act Action) Decision

policies := []Policy{
    AllowAdminAlways,
    AllowOwnFleet("scan.execute"),
    DenyOutsideBusinessHours, // 미래 확장
}
```

| pros | cons |
|---|---|
| 표현력 최강 — 임의 attribute로 분기 | 정책 정의를 코드에 박아야 함 → customer가 정책 변경 시 코드 수정 |
| Phase 6+ MFA·IP 분기 자연스러움 | 본 epic 범위(fleet scope)에 비해 과잉 — YAGNI 위반 |
| 자체 구현 — 외부 dep 0 | 정책 정합성 테스트 부담 매우 큼(매트릭스 폭발) |

**회귀 위험**: 고.
**추정 시간**: 10~14일.

### 4.4 옵션 D — 유지(현 2-tier만) + auditor 활용 강화

코드 변경 최소. 19개 mutation을 admin/operator/admin+auditor 3그룹으로 재분류만 합니다.

| pros | cons |
|---|---|
| 변경 최소 — 1~2일 | fleet 단위 격리 0 — 핵심 가치 미충족 |
| 회귀 위험 0 | enterprise customer 요구 미충족 — "한 customer가 여러 사업부 fleet 운영"이 불가 |
| 다음 epic 보류 가능 | auditor·operator 활용은 늘지만, paying customer 진입 가치는 작음 |

**추정 시간**: 1~2일 (개선 가치는 있으나 본 epic 본질이 아님).

---

## 5. 권장 옵션 + 근거

### 5.1 권장: 옵션 A (기존 시스템 확장 + scope 컬럼) — 단계적 진입

**근거**:
1. **데이터 모델 영향 작음** — `user_roles` 컬럼 2개 ADD로 scope 표현 충분(§4.1). 마이그레이션 1건.
2. **DDD 경계 보존** — tenant 도메인이 PDP, 다른 도메인은 결정만 호출. 원칙 §5(DDD 경계) 준수.
3. **점진 적용 가능** — Stage 분해 후 server gate → fleet scope DB → JWT 확장 → web 결선 순서로 회귀 위험 최소화. 원칙 §12(점진 적용).
4. **외부 dep 0** — P10(에어갭 1급) 보존. Casbin 도입 시 정책 정의 학습곡선 + customer 이해 부담 발생.
5. **회귀 위험 중** — 옵션 B/C 대비 작음. 기존 RequireRole 코드 경로 보존하면서 새 RequirePermission 추가하는 방식 가능.
6. **ROI** — 4~5일 투자로 enterprise customer 요구 90% 충족. Casbin 통합(7~10일)은 정책 변경 자유도가 가치를 정당화하지 못함(Phase 5).
7. **future-proof** — 정책 평가 로직을 `internal/platform/authz/` 에 격리하면 Phase 6+ Casbin 마이그레이션 시 PDP만 교체.

**옵션 B 보류 사유**: 정책 엔진의 가치는 "정책 변경 빈도가 높을 때" 발생. 현재 customer 0~10 추정 단계에서는 코드 변경이 더 빠르고 검증 용이. paying customer 30+ 시점에 재평가.

**옵션 C 비채택 사유**: ABAC 표현력은 fleet scope 1차 요구를 넘어섬. YAGNI 위반.

**옵션 D 비채택 사유**: 본 epic 핵심 가치(fleet scope) 미달성. 단, 옵션 A의 Stage 1로 부분 흡수 가능.

---

## 6. 변경 사항 outline (옵션 A 채택 가정)

### 6.1 마이그레이션 (1건)

`internal/platform/storage/migrations/0027_user_roles_scope.sql`:
```sql
-- PG + SQLite 양 driver — IF NOT EXISTS 사용.
ALTER TABLE user_roles ADD COLUMN scope_type TEXT NOT NULL DEFAULT 'tenant';
ALTER TABLE user_roles ADD COLUMN scope_id   TEXT;
CREATE INDEX IF NOT EXISTS user_roles_scope_idx
    ON user_roles (user_id, scope_type, scope_id);

-- 기존 row는 모두 scope_type='tenant', scope_id=NULL — 글로벌 호환 보존.
```

추가로 신규 시드 role 3개(`fleet-admin`, `read-only`, `owner` — D-RBAC-3 결정 시):
```sql
-- 0028_seed_extra_system_roles.sql (옵션, D-RBAC-3 결정에 따라)
-- bootstrap 시점에 코드로 INSERT — SQL 직접 INSERT는 tenant_id 미상으로 부적절.
-- repo.go::seedSystemRoles 에 추가.
```

### 6.2 신규 파일

- `internal/platform/authz/decision.go` — `Decision`, `Subject`, `Resource`, `Action` 타입 + PDP 진입점.
- `internal/platform/authz/policy.go` — 정책 함수 컬렉션 (`AllowIfPermission`, `AllowIfFleetMember`, `AllowAdminAlways`).
- `internal/platform/authz/decision_test.go` — 결정 테이블 단위 테스트 (resource × action × scope × role 조합).
- `internal/api/handlers/rbac_middleware.go::RequirePermission` — `RequireRole`과 병행 신규 factory:
  ```go
  func (h *Handlers) RequirePermission(perm tenant.Permission, scopeExtractor func(*http.Request) (scopeType, scopeID string)) func(http.Handler) http.Handler
  ```
- `internal/domain/tenant/binding.go` — `RoleBinding{Role Role; ScopeType string; ScopeID string}` + `Service.AssignRoleScoped(userID, roleID, scopeType, scopeID)`.
- `web/src/api/permissions.ts` — `useHasPermission(perm, fleetId?)` hook + `PermissionMatrix` 정적 정의.
- `web/src/lib/route-guards.ts::requirePermission(perm, fleetId?)` — beforeLoad용.

### 6.3 수정 site

- `internal/domain/tenant/tenant.go` — `Permission` 상수 셋 보강 (현재 9개 → §3.3 매트릭스에 따라 15~20개), `AccessClaims.Bindings []RoleBinding` 추가.
- `internal/domain/tenant/jwt.go::SignAccessToken` / `ParseAccessToken` — accessJWT struct에 `bindings` 클레임 추가. 기존 `roles` 슬라이스는 호환 유지(D-RBAC-7).
- `internal/domain/tenant/sqliterepo/repo.go::assignRole` → `assignRoleScoped` 시그니처 확장. `getUserRolesScoped` 신설(scope 포함 반환).
- `internal/api/handlers/handlers.go::237~319` — admin 그룹 19건 중 fleet scope 9건을 `RequirePermission(...)` 로 교체. 6건은 `RequirePermission(perm, scopeTenant)` 유지.
- `internal/api/handlers/auth.go::325` — `/me` 응답에 `bindings` 필드 추가.
- `web/src/stores/auth.ts::User` — `bindings: RoleBinding[]` 추가.
- `web/src/api/hooks.ts::140~153` — useIsAdmin 등 5 helper에 `useHasPermission(perm, fleetId?)` 신설. 기존 helper는 deprecated 표시 후 보존(점진 마이그레이션).
- `web/src/pages/{robots,scans,fleets,insights,compliance}/*.tsx` — fleet 컨텍스트가 있는 페이지에서 `useHasPermission('scan.execute', currentFleetId)` 패턴으로 button gate 갱신.

### 6.4 단위·통합 테스트

- **단위**: `authz/decision_test.go` 결정 테이블 — 5 role × 8 resource × 6 action × 2 scope = 약 480 case (테이블 driven, 단일 테스트 함수).
- **단위**: `tenant/binding_test.go` — RoleBinding 시리얼라이즈/디시리얼라이즈 + JWT roundtrip.
- **통합**: `internal/api/handlers/rbac_scope_test.go` — fleet[A] operator가 fleet[B] robot POST 시 403 (testcontainers).
- **통합**: `cmd/rosshield-server/integration_test.go` 확장 — 5 role 페르소나 × 19 mutation = 95 case 매트릭스.
- **e2e**: web Playwright — fleet[A] operator로 로그인 → fleet[B] robot 페이지 진입 시 redirect 또는 빈 list (D-RBAC-9에 따라).

---

## 7. TDD Stage 분해 (5 commit)

원칙 §12(점진 적용) — big-bang 금지. 각 stage 자체 검증 가능.

### Stage 1 — `authz` 패키지 + 결정 테이블 (단위 테스트만)

**산출**: `internal/platform/authz/{decision,policy,permission_matrix}.go` + 결정 테이블 단위 테스트. server middleware·DB·JWT 변경 0.

**테스트**:
- `TestDecisionTable_AllRoleResourceActionMatrix` — §3.3 매트릭스 480 case.
- `TestPermissionImpliesWildcard` — `*` 와일드카드 동작.
- `TestFleetScopeBeatsTenantDeny` — fleet[A] operator + tenant scope read-only 동시 보유 → fleet[A] write 가능.

**검증**: `go test ./internal/platform/authz/... -count=1` PASS.

**시간**: 0.5일.

### Stage 2 — RoleBinding 타입 + DB 컬럼 + repo 확장

**산출**:
- 마이그레이션 0027 (scope_type/scope_id 컬럼 + 인덱스).
- `tenant.RoleBinding` + `Service.AssignRoleScoped` + `GetUserRoleBindings`.
- `sqliterepo/repo.go` — `assignRole` 시그니처 호환 유지 + `assignRoleScoped` 신설.
- 기존 row은 자동으로 `scope_type='tenant'` — 호환 보존.

**테스트**:
- `TestAssignRoleScoped_FleetBinding` — fleet[X]에 fleet-admin 할당 → GetUserRoleBindings에서 (role, fleet, X) 복원.
- `TestExistingTenantBindingPreserved` — 0027 마이그레이션 후 기존 user_roles row이 scope_type='tenant'로 자동 분류.
- `TestCrossTenantScopeIsolation` — fleet[X]@tenant_A 할당 → tenant_B 사용자가 fleet[X] 권한 0 (scope_id는 tenant scope test에서 이미 격리되지만 명시 검증).

**검증**: `go test ./internal/domain/tenant/... -count=1` PASS + 기존 RBAC 테스트 회귀 0.

**시간**: 1일.

### Stage 3 — JWT bindings claim + middleware `RequirePermission` factory

**산출**:
- `AccessClaims.Bindings` 추가 + `accessJWT.Bindings` JSON claim.
- `RequirePermission(perm, scopeExtractor)` middleware factory.
- 기존 `RequireRole` 보존 — 호환 유지.
- 기존 발급 토큰 호환 — claim 부재 시 전체 `roles`를 `[{Name: r, ScopeType: "tenant"}]`로 fallback 변환(D-RBAC-7).
- `/me` 응답에 `bindings` 추가.

**테스트**:
- `TestJWTBindingRoundtrip` — Sign → Parse → 동일 binding 셋.
- `TestRequirePermission_FleetMatch` — operator@fleet_A 토큰 + scope=fleet_A 요청 → 200; scope=fleet_B → 403.
- `TestLegacyRolesClaimFallback` — bindings 없는 옛 토큰 + admin role → 모든 admin permission 통과.

**검증**: `go test ./internal/{api/handlers,domain/tenant}/... -count=1` PASS.

**시간**: 1일.

### Stage 4 — server handlers.go 19 mutation gate 교체 + 통합 테스트

**산출**:
- handlers.go::237~319 — fleet scope 9건을 `RequirePermission(perm, fleetIDFromPath)` 로 전환. tenant 글로벌 6건은 `RequirePermission(perm, tenantOnly)`. admin/auditor 3건(audit/report verify, system backup) 분리.
- 시드 role 추가(D-RBAC-3 결정에 따라 owner/fleet-admin/read-only).
- 통합 테스트 매트릭스 (rbac_scope_test.go) — 5 role × 19 endpoint = 95 case.

**테스트**:
- `TestRBACMatrix_AllPersonas` — 페르소나(admin·fleet-admin@A·operator@A·auditor·read-only) × 19 mutation × {fleet_A, fleet_B} 조합.
- 회귀: 기존 admin 단일 토큰의 모든 mutation은 200(legacy 호환).

**검증**: `go test ./internal/api/handlers/... -count=1` PASS + `make ci`.

**시간**: 1.5일.

### Stage 5 — web `useHasPermission` + page 결선 + sidebar/router guard 확장

**산출**:
- `web/src/api/hooks.ts::useHasPermission(perm, fleetId?)` 신설. 기존 `useIsAdmin` 등은 deprecated 표시 + 내부 구현은 useHasPermission 위임.
- `web/src/lib/route-guards.ts::requirePermission(perm, fleetId?)`.
- 8 mutation 페이지에서 button gate를 `useHasPermission('scan.execute', currentFleet)` 패턴으로 갱신.
- sidebar 메뉴: fleet 단위 메뉴(fleets list 안)는 binding 보유 fleet만 표시.

**테스트**:
- vitest: `useHasPermission` pure helper 단위.
- Playwright (또는 수동 QA 체크리스트): operator@fleet_A로 로그인 → /fleets/B/robots 진입 시 redirect.

**검증**: `tsc + vitest + pnpm build` PASS.

**시간**: 1일.

**총 추정**: **5일** (보수적, memory feedback_design_doc_conservative.md 준수). 1주+ 추정의 lower bound.

---

## 8. 결정 항목 (D-RBAC-1 ~ D-RBAC-10)

### D-RBAC-1 — 권한 모델 엔진

- **A) 자체 PDP (옵션 A)** — `internal/platform/authz/` 결정 테이블 + Permission 문자열 매칭. **권장 default**.
- B) Casbin v2 통합 — 정책 파일 외부화.
- C) OPA + Rego — 별 프로세스.

**근거**: §5.1.

### D-RBAC-2 — scope 모델

- **A) 2-level (tenant + fleet)** — 본 doc 기본 가정. **권장 default**.
- B) 3-level (tenant + fleet + robot) — Phase 6+로 보류.
- C) tag-based (fleet 무관, attribute로 자유) — ABAC, 옵션 C.

**근거**: 4.1 fleet 단위가 enterprise 1차 요구. robot 단위는 YAGNI.

### D-RBAC-3 — 시드 시스템 role 셋

- A) 현행 3개 유지(admin/auditor/operator) + 사용자 정의 role builder Phase 6.
- **B) 6개로 확장(owner/admin/fleet-admin/operator/auditor/read-only)** — 본 doc §3.1. **권장 default**.
- C) 4개(admin/fleet-admin/operator/auditor) — owner/read-only 후순위.

**근거**: enterprise customer 요구 매트릭스(§3.3) 충족 최소셋. owner는 plan/billing 분리 가치, read-only는 dashboard-only 사용자.

### D-RBAC-4 — compliance snapshot scope

- A) tenant 글로벌(현행) — fleet 무관 1개 snapshot.
- **B) fleet 단위** — fleet[X] snapshot은 fleet[X] member만 생성. **권장 default**.
- C) hybrid — admin은 tenant, fleet-admin은 fleet 단위.

**근거**: compliance profile은 tenant 단위지만, snapshot 실행 권한은 fleet 단위가 자연스러움(§2.2 매핑).

### D-RBAC-5 — 마이그레이션 정책 (기존 user_roles row)

- **A) 자동 변환 — 모든 기존 row을 `scope_type='tenant'`로 분류** (마이그레이션 0027 DEFAULT). 기존 admin은 tenant admin. **권장 default**.
- B) 수동 마이그레이션 — admin 1명에게만 owner role 자동 부여, 나머지 role은 tenant 분류. customer 진입 시 합의.

**근거**: 기존 customer 0~5명 단계에서 자동 변환이 안전. 수동 마이그레이션은 customer 합의 비용 ↑.

### D-RBAC-6 — 기존 admin/operator/auditor mapping

- **A) admin → tenant admin (모든 fleet implicit), operator → fleet[*] operator (전 fleet member 자동), auditor → tenant auditor (read-only + verify+export)** — **권장 default**.
- B) admin → owner, 나머지 동일 — owner와 admin 의미 분리.
- C) operator는 자동 변환 시 어느 fleet에도 binding 없음(빈 셋) — customer가 명시 할당.

**근거**: 기존 사용자 동작 호환 보존(원칙 §12 점진 적용). owner는 신규 customer 진입 시 명시 부여.

### D-RBAC-7 — JWT 호환성 (기존 발급 토큰)

- **A) 옛 토큰(bindings claim 부재)은 server에서 `roles`를 모두 tenant scope로 가정하여 fallback** — 토큰 만료 자연 회수(15분~1h). **권장 default**.
- B) 옛 토큰 즉시 무효화 — 모든 사용자 강제 재로그인. UX 마찰.
- C) 신구 토큰 병행 발급 — claim 두 셋 모두 포함, 만료 후 신 형식만.

**근거**: A는 자연 회수 + UX 무충격. claim 형식 변경에서 표준 패턴.

### D-RBAC-8 — API 노출 형식 (`/me` 응답 + REST endpoint)

- **A) `/me` 응답에 `bindings: [{role, scope_type, scope_id}]` 추가, role list endpoint 신설(`GET /api/v1/tenant/roles`)** — **권장 default**.
- B) `/me`에 `effective_permissions: ["scan.execute@fleet_A", ...]` 평탄화 — 표현 단순, customer integration 친숙.
- C) 둘 다 — 비대.

**근거**: A는 데이터 구조 정확. 평탄화는 client-side가 필요 시 계산.

### D-RBAC-9 — web UI에서 권한 부족 시 동작

- **A) 페이지 router guard로 redirect (현 패턴 유지)** — 자연. **권장 default**.
- B) 권한 부족 페이지에 명시적 "권한이 없습니다" 안내 카드 + 요청 button.
- C) 메뉴 자체에서 disabled tooltip — 페이지 진입 0.

**근거**: A는 현 패턴(Stage 2-E)와 일관. B는 enterprise UX로 가치 있으나 별 작업.

### D-RBAC-10 — 시스템 역할 보호 (수정/삭제 금지)

- **A) is_system=true role은 수정/삭제 모두 거부** — bootstrap 시드 보존. **권장 default**.
- B) is_system role도 admin이 permission 변경 가능 — customer customization.
- C) is_system role은 불변, 사용자 정의 role(is_system=false)만 수정 — Phase 6 role builder 진입 전 안전.

**근거**: A는 audit chain의 결정론 보존. B는 customer 환경 일관성 깨질 위험.

---

## 9. 회귀 위험 / 운영 고려

### 9.1 데이터 마이그레이션

- **0027 마이그레이션** — `ALTER TABLE user_roles ADD COLUMN ... DEFAULT 'tenant'` + INDEX. 100~10000 row 규모, 빠름.
- 기존 row 동작 보존 — D-RBAC-5 옵션 A로 자동 분류, customer 작업 0.
- rollback — 컬럼 DROP은 SQLite에서 까다로움(테이블 재생성). Stage 2 commit 자체는 forward-only.

### 9.2 API 호환성

- 기존 사용자 admin role은 그대로 모든 admin endpoint 통과(D-RBAC-7 fallback).
- 신규 endpoint(`GET /api/v1/tenant/roles`, scoped role assign mutation) 추가만, 기존 endpoint 시그니처 변경 0.
- OpenAPI spec 갱신 필요 — Stage 3+4에서 추가.

### 9.3 cache invalidation

- JWT claim 형식 변경 → 클라이언트 token cache 호환 처리(D-RBAC-7).
- web stores/auth User.bindings 추가 → persisted store 호환 — 옛 schema는 자동 migration(zustand persist version bump).
- React Query `me` 응답 schema 변경 → invalidate 1회.

### 9.4 audit chain 영향

- role binding 변경(`role.assigned`, `role.revoked`, `role.scoped`)은 audit append-only — 신규 audit kind 3개 추가. `tenant.AuditEmitter` 인터페이스 확장.
- 기존 entry는 영향 0 — append-only 보존.
- entry replay/verify 시 신규 kind 미인식 risk → Stage 2에서 audit replay test 필수.

### 9.5 multi-tenant scaling

- `user_roles` row 폭발 위험 — user 1 × 50 fleet × 3 role = 150 row. 1000 user customer는 150K row.
- INDEX `(user_id, scope_type, scope_id)`로 lookup O(log n) 보장.
- JWT bindings claim 크기 — 50 binding × ~50 byte = 2.5KB. cookie 4KB 한계 안. PG 환경 권장 옵션은 binding 5~10개 하한 customer 가이드.

### 9.6 dev·prod 차이

- 데스크톱 single-user는 owner/admin 1명만 — fine-grained 가치 0이지만 호환은 보존.
- 어플라이언스는 보통 1 customer × 1~5 user — fleet 단위 가치 있음.
- enterprise(prod) — 본 epic 1차 타깃.

### 9.7 first paying customer 진입 영향

- D5 R30-4 "open-core, 첫 enterprise customer 직전 분리" — fine-grained RBAC는 enterprise tier 핵심 차별화 후보. 분리 시 Apache-2.0 코어 vs BSL/Commercial enterprise 경계가 권한 모델 깊이로 결정.
- 권장: **본 epic 자체는 코어 Apache-2.0** (fleet 단위는 정직한 multi-tenancy 기본 요구). dynamic policy(시간·MFA·IP)·SCIM·role builder UI는 enterprise.

### 9.8 운영 가이드

- customer onboarding 문서에 "fleet 1개 = 사업부 1개" 패턴 권장.
- role binding 변경 audit 로그 — admin 패널에서 "최근 권한 변경 history" 카드 필요(별 작업, Phase 6).

---

## 10. 참조

### 본 리포 design doc
- `docs/design/05-api-and-auth.md` — JWT 발급·검증·미들웨어
- `docs/design/06-security-and-tenancy.md` — 멀티테넌시 격리 원칙
- `docs/design/01-principles.md` §4 멀티테넌시 기본값, §5 DDD 경계, §12 점진 적용
- `docs/design/04-domain-and-data-model.md` — Role/Permission/User_Role SQL 스키마
- `docs/design/notes/e25-ha-design.md` — 결정 항목 형식 참조
- `docs/design/notes/e22-f-boolean-recovery-design.md` — 옵션 비교 + 점진 적용 형식 참조

### 본 리포 코드
- `internal/domain/tenant/tenant.go` — Permission 상수, Role/User struct, SystemRolePermissions 시드 정의
- `internal/domain/tenant/jwt.go::27~35` — AccessClaims 구조
- `internal/domain/tenant/sqliterepo/repo.go::120,138~152,820~895` — seedSystemRoles + roles/user_roles 스키마
- `internal/api/handlers/rbac_middleware.go` — RequireRole factory (Stage 1)
- `internal/api/handlers/handlers.go::237~319` — admin 그룹 19 mutation
- `internal/api/handlers/auth.go::325` — /me 응답 roles 추출
- `web/src/api/hooks.ts::123~153` — hasAnyRole + 4 helper hook
- `web/src/lib/route-guards.ts` — requireRole client guard
- `web/src/stores/auth.ts::User` — bindings 확장 site

### 메모리·결정
- memory `feedback_design_doc_first.md` — 큰 작업 design doc 우선
- memory `feedback_design_doc_conservative.md` — 보수적 추정
- memory `feedback_parallel_agents.md` — 본 epic은 직렬 stage 의존성 강함, 병렬 0
- D5 R30-4 — open-core 정책, 본 epic의 코어/enterprise 경계 영향
- SESSION_HANDOFF "Phase 5 backlog (큰 작업, design 의사결정 필요)" 후속 entry

### 외부 (참조 정도)
- Casbin v2 — github.com/casbin/casbin (옵션 B)
- OPA — openpolicyagent.org (옵션 B 변형)
- AWS IAM policy 모델 — resource-based policy 패턴 (scope_id 컬럼 영감)
- "RBAC vs ABAC" — NIST IR 8112 (옵션 분류 근거)
