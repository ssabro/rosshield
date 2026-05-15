# Phase 6 Backlog — Phase 5 retrospective + 후보 5종 비교 + 권장 우선순위 — Design

> **상태**: Phase 5 5 epic 100% 마감 직후(head `ccddde8`) Phase 6 진입 합의용 design doc. 본 문서는 코드 0줄 / 마이그레이션 0건 / pack 변경 0 — Phase 5 회고 + Phase 6 후보 5종 매트릭스 + 권장 우선순위 + 결정 항목 권장 default까지만 마감합니다.
> **참조**:
> - Phase 5 design doc 6종: `scanrun-ssh-integration-design.md`(539줄) · `pwa-offline-design.md`(553줄) · `rbac-fine-grained-design.md`(568줄) · `rbac-fleet-scope-precision-design.md`(660줄) · `pwa-persist-design.md`(585줄) · `scanrun-extras-design.md`(667줄).
> - carryover 3종: `cis-manual-21-fixture-design.md`(391줄) · `e22-f-boolean-recovery-design.md`(417줄) · `scanrun-extras-design.md`(667줄).
> - 설계서: `docs/design/11-tech-stack-and-roadmap.md` §11.13 로드맵 + §11.16 결정 로그 · `docs/design/12-migration-and-non-goals.md` · `docs/design/13-patent-strategy.md`(D8 청구권).
> - SESSION_HANDOFF.md "결정 로그" 2026-05-15 RBAC fleet Stage 5/6 마감 시점.
> **R 식별자**: R-PHASE6-1 (본 doc 전체) — 결정 항목은 D-PHASE6-1~5.
> **본 worktree**: `agent-a9739048322bba6c4`, main(head `ccddde8`)에서 분기. 단독 sub-agent.

---

## 1. Phase 5 retrospective

### 1.1 Phase 5 5 epic 마감 요약

Phase 5 시작점은 `6f893de`(design doc 첫 commit)이며 마감점은 `ccddde8`(RBAC fleet Stage 6 closing handoff)입니다. 5 epic이 모두 100% 마감되었습니다.

| Epic | Stage | head 마감 commit | 누적 산출 |
|---|---|---|---|
| **scanrun SSH 통합** | 5/5 (Stage 1·2·3·4·5a·5b·5c) | `ee2aa34` | `robot.RobotHostKey` 도메인 + 마이그레이션 0027 (TOFU host key) + `KnownHostsManager` + `sshpool` Pool idle 재사용·keepalive·metrics 5종 + bootstrap 결선 + `sudo -n` non-interactive + per-robot health window(HealthFailureThreshold=3) + docker-compose.ssh.yml + sshd_e2e_test.go 5 phase |
| **세분 RBAC** | 5/5 (Stage 1~5) | `4ec5620` | `internal/platform/authz/` PDP + 6 role × 9 resource × 6 action 매트릭스 + `tenant.RoleBinding` 도메인 + 마이그레이션 0028(scope_type/scope_id) + JWT `bindings` claim + `RequirePermission` middleware factory + 24 mutation gate 교체 + 195 sub-test 통합 매트릭스 + web `useHasPermission` hook + sidebar/router guard |
| **PWA 오프라인** | 4/4 (Stage 1~4) | `70ef3d6` | manifest + 아이콘 4종 + vite-plugin-pwa generateSW + SW 등록 + `OfflineIndicator` + `UpdatePrompt` UX + mutation 가드 + 운영자 docs |
| **PWA persist** | 4/4 (Stage 1~3 + operations docs) | `350c38d` | `idb-storage.ts` IndexedDB AsyncStorage 어댑터(idb-keyval@6.2.2) + `PersistQueryClientProvider` 결선 + dehydrate filter(보안 차단 list) + logout flow clear(multi-tenant 격리) + 운영자 가이드 |
| **RBAC fleet 정밀화** | 5/5 + Stage 6 closing | `77180db` | PDP `MatchedBindings` 확장(explainability) + `RequirePermissionWithFleet` middleware + body peek + `ScopeResolver` + handlers 5+2 endpoint 교체 + 통합 매트릭스 + SSO group 매핑 도메인 + 마이그레이션 0029 + `user_roles.source` + SSO callback sync + audit `user_role.synced` + web admin UI(GroupMappings + 토글) |

**Phase 5 수직축**: 보안 격리(scanrun SSH host key MITM 차단 + 세분 RBAC + RBAC fleet 정밀화) · 에어갭/오프라인(PWA 2 epic) · enterprise 자동화(SSO group → role) — 세 축이 첫 paying customer 진입 가능 baseline을 형성합니다.

### 1.2 누적 통계 (정확)

`6f893de^..ccddde8` 범위 git stat:

- **commit**: 46건 (design 6 + feat/test 23 + docs handoff 17, 본 worktree 외 main 기준).
- **파일 변경**: 211 files changed.
- **줄 수**: +29,321 / −7,791 (web dist asset rebuild 다수 포함, 순수 src는 약 1/3 추정).
- **마이그레이션**: 3건 신규 (0027 robot_host_keys · 0028 user_roles scope_type/scope_id · 0029 sso_group_mappings + user_roles.source).
- **신규 도메인 타입**: `robot.RobotHostKey` · `tenant.RoleBinding` · `tenant.SSOGroupMapping` · `authz.Decision/Permission`.
- **신규 패키지**: `internal/platform/authz/`.
- **신규 web hook**: `useHasPermission` · `useIsAdmin` 외 4 helper.
- **테스트**: 단위 50+ 패키지 + 통합 195 sub-test 매트릭스(`rbac_integration_test.go`) + e2e sshd 5 phase + manual fixture round-trip 12.

### 1.3 sub-agent 병렬 패턴 (회고 핵심)

본 Phase 5의 운영 패턴은 **sub-agent worktree 병렬 작업** 11회 연속 검증되었습니다 (memory `feedback_parallel_agents.md`):

- **3종 design doc 병렬**: Stage 1 entry — scanrun + RBAC + PWA 동시 dispatch.
- **3종 코드 Stage 1·2·3 병렬**: 같은 도메인 비충돌 보장 후 worktree 3개 동시.
- **2종 후속 Stage 병렬 (RBAC fleet + PWA persist)**: 같은 사용자 round 안에서.
- **단독 sub-agent 5건**: RBAC fleet Stage 5(epic 마감, 메인 컨텍스트 보호) + Stage 6 + 본 design doc.

병렬 효율: 사용자 단일 round당 평균 2~3 commit이 동시 산출. 예: `Stage 5/4/4 3종 병렬`(`e3f795e` handoff 1 commit으로 3 epic Stage 동시 마감).

### 1.4 핵심 결정 회고

| 결정 | 시점 | 근거 | 회고 |
|---|---|---|---|
| design doc 우선 (1일+ 임계) | Phase 5 시작 | memory `feedback_design_doc_first.md` | ✅ 사용자 다음 세션 즉시 진입 부담 0 — D-* 권장 default 모두 명시. context 한도 회피 효과 큼. |
| sub-agent 병렬 | 매 stage 시작 | memory `feedback_parallel_agents.md` | ✅ 11회 연속 회귀 0. 사용량 한도 도달 시 메인 fallback commit으로 작성 산출 보존. |
| design doc conservative 추정 | 모든 후보 평가 | memory `feedback_design_doc_conservative.md` | ✅ Manual fixture는 추정 80% → 실제 94.6% (보수적 추정 정확). |
| 휴식 옵션 자동 추천 X | 매 round | memory `feedback_no_rest_recommendation.md` | ✅ 진행 중 선택지에 휴식 비포함, 자동 commit. |
| RBAC fleet 옵션 C(정밀화 + SSO 동시) | Stage 1 entry | enterprise 가치 두 축 / 같은 PDP context 효율 | ✅ 5 commit 8~10일 추정 → 실제 6 commit + Stage 6 closing(2 endpoint 추가)으로 끝남 = 정확. |

### 1.5 잘된 점

1. **5 epic 100% 마감** — 진입 시 추정 5/5 마감 가능성을 보수적으로 봤으나 100% 도달.
2. **SSO 자동화 완성** — RBAC fleet Stage 5에서 SSO callback sync까지 결선되어 enterprise customer 가치 명확(IdP 진실의 원천 + audit explainability).
3. **회귀 0** — Phase 5 전체에서 회귀 0건. 195 sub-test 통합 매트릭스가 1차 방벽.
4. **병렬 worktree 패턴 정착** — sub-agent dispatch + worktree cleanup + cherry-pick fallback이 안정 반복.
5. **carryover docs 권장 default 일관** — Manual·E22-F·scanrun extras 모두 보류 권장으로 다음 세션 즉시 진입 부담 0.

### 1.6 아쉬운 점

1. **RBAC fleet Stage 6 reports/insights service 확장은 closing 단계로 분리됨** — 원안 5 commit Stage 분해에서 누락된 service 확장이 Stage 6에서 후속 처리(`77180db`). 향후 design doc Stage 분해 시 service layer 변경 범위를 더 보수적으로 잡을 것. 학습: 매트릭스 분류 시 endpoint 단위 + service 확장 단위 둘 다 명시 카운트.
2. **사용량 한도 도달 시 sub-agent worktree commit 실패** — `af0b84d` PWA persist design doc은 worktree commit 실패 후 메인 fallback. 패턴 자체는 보존되나 worktree 사용량 사전 견적 부족. 학습: sub-agent dispatch 시 사용량 한도 시점 사전 검사 + design doc 우선 작업은 사용량 한도 직전에 commit 가능.
3. **Phase 5 design doc 3종 동시 작성 시 같은 commit round handoff 1건만** — design doc 3종 병렬은 좋았으나 handoff 갱신은 1 commit으로 압축. 다음 epic 진입 시 round별 결정 항목 모니터링 부담. 학습: 병렬 design doc 후 handoff entry는 doc별 분리 또는 명시 압축 표시.
4. **D8 청구권 enterprise 패키지 7종 placeholder만 — 실 구현 부재** — `internal/enterprise/{crosswitness,selectdisclose,multihash,wasmrt,robotid,rostopo,fleetxval}/`에 `EditionTag` const만 존재. D8 KR 우선출원 후 E32 stage가 trigger지만 사용자 외부 트랙(D1 변리사)에 의존. 본 Phase 6 후보 2가 이 트랙을 직접 다루지만 보류 권장(§3.2).
5. **Phase 5 마감 후 README 본 head 미갱신** — `ccddde8` handoff commit은 됐으나 README 변환률·Phase 진척 표시 갱신 누락 가능성(별 worktree에서 작업 중일 수 있음). 다음 round 진입 시 README sync 사전 점검 권장.
6. **carryover 보류 일관성**: 3 carryover 모두 보류 권장이나 사용자에게 매 round "보류 유지 vs 진입" 명시 옵션을 진행 중 선택지에 명시 — 사용자 부담은 작지만 round당 의사결정 1건 추가. 학습: carryover는 별 섹션(예: 휴면 epic 카탈로그)으로 분리 + customer trigger 시점에 한 번에 재평가.

---

## 2. 현재 carryover 잔여 분석

Phase 5 마감 시점에 명시적으로 보류 권장된 3 트랙:

### 2.1 Manual fixture Stage 3 low 5건

- **권장 default**: D-MAN-6 = 보류.
- **trigger 조건**: 첫 paying customer 진입 또는 enterprise customer 명시 요청.
- **잔여 IDs**: 1.2.1.2(C1) · 6.1.2.1.2(C4) · 6.1.3.5(C5) · 6.1.3.6(C5) · 6.1.3.8(C5).
- **추정 시간**: 0.5~1일 (Stage 1·2 패턴 일관, sub-agent 일괄 가능).
- **잠재 효과**: 변환률 94.6% → 96.0% (1.4%p), Manual 21건 100% cover. Phase 5에서 high 3 + medium 9 = 12건 cover로 핵심 정책 검토 영역은 완료.
- **보류 이유**: low 5건은 모두 site policy 의존(C1·C4·C5)이며 fixture 자체는 운영자 직접 fileng 비중 높아 첫 customer 환경 확인 후 진입이 효율적. ROI는 첫 customer 진입 시점에서 결정.

### 2.2 E22-F BOOLEAN 회수 옵션 A

- **권장 default**: D-E22F-1 = 옵션 C 보류 (영구).
- **trigger 조건**: 옵션 B(Big bang driver-aware repo) 일괄 진입 시점 — 즉 R30-1 hybrid 정책에서 paying customer 진입 후 PG-native 전면 회수 epic.
- **잔여 컬럼**: 5건 (roles.is_system · compliance_frameworks.enabled · webhooks.enabled · webhook_deliveries.succeeded · sso_providers.enabled).
- **추정 시간**: 옵션 C 0일(본 design doc commit이 결론) / 옵션 A 0.5일(부분 회수, 회귀 위험 중).
- **잠재 효과**: BOOLEAN ↔ SMALLINT query plan/storage 차이 사실상 0. 가치는 schema 의미 명확성 + driver-native API 호환에 한정.
- **보류 이유**: ROI 부재(R30-1 = C 하이브리드 정책 일관) + 회귀 위험 vs 이득 비대칭(5 사이트 코드 + WHERE literal 정리 + bool 호환성 실증 비용). 첫 enterprise customer 시점에서 옵션 B 일괄.

### 2.3 scanrun extras (Pool size 동적 + per-tenant rate limit + per-robot circuit breaker + observability)

- **권장 default**: D-SCANEX-1 = 옵션 B 보류 (customer trigger 대기).
- **trigger 조건**: (시나리오 1) multi-tenant noisy neighbor 발생 — 첫 enterprise customer 환경에서 customer A 폭주가 customer B 영향. (시나리오 2) 환경별 robot 부하 편차. (시나리오 3) 영구 장애 robot 다수. (시나리오 4) Grafana dashboard 운영 디버깅 자료 요청.
- **잔여 4 epic**: A(Pool size 동적) · B(per-tenant rate limit) · C(per-robot circuit breaker) · D(observability metric 확장).
- **추정 시간**: epic A 1주 / B 1주 / C 1.5주 / D 0.5주 → 옵션 B 합계 4주.
- **잠재 효과**: customer 진입 *전*에는 baseline scanrun(Stage 1~5c)으로 충분. 진입 *후* 즉시 trigger 가능성 높은 epic은 C(영구 장애 robot Run scope 한계 — 매 Run에서 timeout × 3).
- **보류 이유**: 첫 customer 진입 *전*에는 부하 측정 데이터 부재(가설만 존재). 진입 후 부하 측정 → 우선순위 재평가 권장.

### 2.4 carryover 종합

3 carryover 모두 **보류 권장**이며 trigger 조건은 일관되게 **첫 paying customer 진입**입니다. Phase 6 진입 후에도 별 트랙으로 두고 사용자가 customer 진입 시점에 명시적 진입 요청 시까지 유지가 적절합니다.

---

## 3. Phase 6 후보 5종 매트릭스

각 후보별 가치·추정·위험·선결·즉시 진입 가능 여부를 평가합니다.

### 3.1 후보 1 — 첫 paying customer onboarding 보강

| 항목 | 평가 |
|---|---|
| **가치** | paying customer ★★★★★ / enterprise ★★★ / compliance ★★ / operational ★★★ / 기술 부채 ★ |
| **세부 가치** | E38 docs 강화(첫 customer 손에 쥐는 가이드) + customer intake 연동(가입 → tenant 생성 → role binding 자동화) + 첫 PoC scenario walkthrough(robot 1대 실 SSH + scan 1회 + report 서명·검증). Phase 5 baseline이 모두 결선되어 가치 즉시 회수 가능. |
| **추정 시간** | **1~2주** (보수적). E38 docs 강화 0.5주 + intake API/UI 0.5~1주 + walkthrough script + e2e 1주. |
| **회귀 위험** | **낮음**. 신규 docs + intake API thin wrapper + 기존 endpoint 재사용. 회귀 표면 0~최소. |
| **선결 조건** | 없음. Phase 5 baseline이 baseline. 사용자 외부 트랙(D1·E36·E37) 의존 0. |
| **즉시 진입 가능?** | **✅ 즉시**. design doc 1개 작성 후 Stage 1 진입. |
| **도메인 hand-off** | tenant(intake API) + RBAC(첫 admin 자동 binding) + scanrun(walkthrough scan trigger) + reporting(walkthrough PDF). 도메인 경계 침범 없음 — 모두 기존 Application Service 경유. |
| **design doc trigger** | 1~2주 작업으로 memory `feedback_design_doc_first.md` 1일+ 임계 충족. design doc 1개 권장. |
| **합성 전략 옵션** | §5.1 |

### 3.2 후보 2 — D8 청구권 코드 분리 (enterprise build tag)

| 항목 | 평가 |
|---|---|
| **가치** | paying customer ★ / enterprise ★★★★★ / compliance ★★★ / operational ★ / 기술 부채 ★★★★ |
| **세부 가치** | 현재 placeholder 7 패키지(crosswitness · selectdisclose · multihash · wasmrt · robotid · rostopo · fleetxval) 본체 구현. D8 1순위 결합 청구항(A-1 cross-witness fold-in + B-1 multi-hash evidence + C-1 WASM sandboxed evaluator + D-3 robot identity binding) 알고리즘 채움. open-core 분리 베이스 + 특허 grant 회피 구조. |
| **추정 시간** | **1개월+** (보수적). 7 패키지 본체 + WASM runtime 통합 + TPM/EK 바인딩 + cross-witness fold-in 알고리즘 + 단위·통합 테스트 + 13-patent-strategy.md 갱신. |
| **회귀 위험** | **중**. enterprise build tag로 코어 분리되어 코어 빌드 회귀는 낮으나 build matrix CI 추가 필요(enterprise 빌드 검증) + 기존 audit chain·signer·evidence 패키지와의 hand-off 지점 복잡. |
| **선결 조건** | **D8 KR 우선출원 완료 후 E32 stage trigger** — 출원 전 GitHub public 전환·외부 공개·NDA 없는 PoC 모두 금지(D8 결정). 출원은 사용자 외부 트랙(D1 변리사 컨설팅 → 선행기술 조사 → 명세서 → KR 출원). |
| **즉시 진입 가능?** | **❌ 보류**. 사용자 외부 트랙 의존 — D1 출원 완료 시까지 enterprise 코드 추가는 patent disclosure 위험. 다만 build tag scaffold는 이미 존재(`E31`)하므로 출원 후 즉시 본체 채움 가능. |
| **도메인 hand-off** | audit chain(cross-witness fold-in) + evidence(multi-hash) + benchmark/scan(WASM evaluator) + robot/keystore(robot identity binding via TPM EK + MAC + CPU serial). 코어 도메인은 interface만 노출, 본체는 enterprise build tag 안에서만. |
| **design doc trigger** | 1개월+ 작업으로 우선출원 후 7 패키지 본체 단계별 design doc 4~5개 권장. |
| **합성 전략 옵션** | §5.2 |

### 3.3 후보 3 — multi-region/cluster 지원

| 항목 | 평가 |
|---|---|
| **가치** | paying customer ★ / enterprise ★★★ / compliance ★★ / operational ★★★ / 기술 부채 ★★★ |
| **세부 가치** | region-aware deployment + cross-region replication + audit chain region scope. 글로벌 customer(여러 대륙 robot fleet) 또는 disaster recovery 요구 시 진입. |
| **추정 시간** | **1개월+** (보수적). region 도메인 추가 + 마이그레이션 1~2건 + cross-region replication(PostgreSQL streaming + 또는 logical) + audit chain region anchor + Helm 차트 region matrix + e2e 2 region 시나리오. |
| **회귀 위험** | **높음**. audit chain 도메인 변경(region anchor) + multi-tenant 격리 region 차원 추가 + replication conflict 처리 + 기존 단일 region 배포 회귀 표면 큼. |
| **선결 조건** | (1) 첫 enterprise customer 진입 후 region 요구 명시 (2) 또는 글로벌 fleet 시나리오 PoC. 현 단계에서는 가설. |
| **즉시 진입 가능?** | **❌ 보류 권장**. 가설 단계, 첫 customer 환경에서 region 요구 확인 후 진입이 효율적. |
| **도메인 hand-off** | tenant(region 컬럼) + audit(region anchor) + scan/evidence(region 격리 보존) + reporting(cross-region report 옵션) + storage(blobstore region 분배). 변경 표면 큼. |
| **design doc trigger** | customer 명시 요구 후 별 design doc(architecture-level) — Phase 6 본 문서 시점에는 후속 트랙으로 catalog만. |
| **합성 전략 옵션** | §5.3 |

### 3.4 후보 4 — audit chain key rotation 자동화

| 항목 | 평가 |
|---|---|
| **가치** | paying customer ★★ / enterprise ★★★★ / compliance ★★★★★ / operational ★★★★ / 기술 부채 ★★★ |
| **세부 가치** | 현재 audit chain signer key는 keystore 결선만(`internal/platform/keystore/{file,tpm}` + `internal/platform/signer/{soft}`) 있고 **rotation 로직 0**. ISMS-P·NIST 800-53(SC-12 Cryptographic Key Establishment) 등은 정기 rotation 명시. 현재 운영자가 수동 rotation 필요(미문서화). 정기 자동 rotation + key escrow + 운영자 승인 flow 추가. |
| **추정 시간** | **2~4주** (보수적). rotation scheduler + 새 key 생성 + chain anchor 변경 처리(rotation event audit emit) + key escrow(분할 보관 옵션) + 운영자 승인 UI + 외부 검증 도구(`fg-verify`) rotation aware 갱신 + 마이그레이션 1건 + 단위/통합 + docs. |
| **회귀 위험** | **중~높음**. audit chain head 변경 직후 외부 검증 호환성 보장 필수. rotation 실패 시 audit emit 차단 위험 — fail-safe 설계 critical. |
| **선결 조건** | 없음. Phase 5 baseline + 기존 keystore/signer 구조 활용. compliance 가치는 첫 customer 진입 *전*에도 명확(감사인 friendly). |
| **즉시 진입 가능?** | **✅ 즉시 가능**(중급 — design doc 우선). compliance·기술 부채 둘 다 가치 있음. 다만 회귀 위험 중급으로 design doc 우선 권장. |
| **도메인 hand-off** | keystore(`internal/platform/keystore/{file,tpm}` 신규 rotation API) + signer(hot-swap) + audit(rotation event emit, 새 chain anchor) + reporting(외부 검증 도구 rotation 호환) + tenant(admin UI 승인 flow). 도메인 경계는 platform 영역에서 처리. |
| **design doc trigger** | 2~4주 작업으로 design doc 1개 + 옵션 A·B·C(key escrow) 비교 + Stage 분해 4~5 commit + D-KEYROT-N 권장 default. |
| **합성 전략 옵션** | §5.4 |

### 3.5 후보 5 — LLM advisor 옵트인 강화

| 항목 | 평가 |
|---|---|
| **가치** | paying customer ★★★ / enterprise ★★★★ / compliance ★ / operational ★★ / 기술 부채 ★★ |
| **세부 가치** | 현재 `internal/platform/llm/{anthropic,ollama,noop}` 어댑터 + `internal/app/advisorrun/` orchestrator·tools가 결선되어 있으나 air-gapped 환경 옵션 명시·private LLM 통합·reasoning trace UI 강화 부재. Ollama 어댑터는 존재(self-hosted LLM 베이스). 옵션 1(원칙 §2 옵트인 / §3 에어갭 1급) 강화는 enterprise customer가 명시 요구하는 경우 가치 큼. |
| **추정 시간** | **2~4주** (보수적). air-gapped LLM 옵션 명시(설정 + bootstrap) + Ollama 어댑터 production-ready 검증(TLS·자체 host validation·시간 초과·재시도) + advisor reasoning trace 강화(각 도구 호출 + 입력 출력 audit chain emit) + UI 표시(advisor 결정 trace) + docs(air-gapped 가이드). |
| **회귀 위험** | **낮음~중**. LLM 어댑터는 옵트인이고 noop fallback 항상 존재 — 회귀 표면 작음. reasoning trace 추가는 audit emit 변경(P5·P11 친화) 검토 필요. |
| **선결 조건** | 없음. Ollama 어댑터 베이스 활용. |
| **즉시 진입 가능?** | **✅ 즉시 가능** (중급). enterprise customer가 명시 요구하지 않는 한 우선순위 중급. customer 진입 *후* trigger 가능성 더 높음. |
| **도메인 hand-off** | llm(어댑터 hardening) + advisor(orchestrator + tools reasoning trace) + audit(advisor 도구 호출 audit emit) + web(advisor trace UI). 도메인 경계 침범 없음 — 모두 advisor 도메인 내. |
| **design doc trigger** | 2~4주 작업으로 design doc 1개 + air-gapped 가이드 + reasoning trace 정책 + Stage 분해 + D-LLMOPT-N. |
| **합성 전략 옵션** | §5.5 |

### 3.6 매트릭스 종합

| 후보 | 가치 종합 | 시간 | 위험 | 즉시 진입 | 권장 우선순위 |
|---|---|---|---|---|---|
| 1. customer onboarding 보강 | ★★★★★ (paying customer 직격) | 1~2주 | 낮음 | ✅ | **1순위** |
| 4. audit chain key rotation 자동화 | ★★★★ (compliance + 기술 부채) | 2~4주 | 중~높음 | ✅(design doc 우선) | **2순위** |
| 5. LLM advisor 옵트인 강화 | ★★★ (enterprise 옵션) | 2~4주 | 낮음~중 | ✅ | **3순위** |
| 3. multi-region/cluster | ★★★ (글로벌 customer 가설) | 1개월+ | 높음 | ❌(가설) | **보류** (customer trigger) |
| 2. D8 청구권 코드 분리 | ★★★★★ (enterprise + IP) | 1개월+ | 중 | ❌(D1 출원 의존) | **보류** (D1 출원 후) |

---

## 4. 권장 default 우선순위

**memory `feedback_design_doc_conservative.md` 일관 — 잠재 효과 / 시간 보수적**.

### 4.1 1순위 — 후보 1 첫 paying customer onboarding 보강

**근거**: ROI 가장 큼 + 가장 작음(1~2주) + 즉시 진입 가능 + 회귀 위험 낮음 + Phase 5 baseline 즉시 가치 회수. 첫 paying customer 진입은 D5 open-core·D6 GitHub public 전환·D8 출원 등 다른 결정의 trigger이므로 **시간 가치**가 가장 높음.

### 4.2 2순위 — 후보 4 audit chain key rotation 자동화

**근거**: compliance 가치(★★★★★, 감사인 friendly + ISMS-P/NIST SC-12 명시 요구)는 첫 customer 진입 *전*에도 명확. 기술 부채 정리(현재 수동 rotation 미문서화)도 함께 해소. 회귀 위험은 중~높음이지만 design doc 우선 + Stage 분해로 관리 가능. customer가 audit 요구 시 즉시 답변 가능 baseline.

### 4.3 3순위 — 후보 5 LLM advisor 옵트인 강화

**근거**: enterprise customer가 명시 요구하지 않는 한 우선순위 중급이나 Ollama 어댑터 베이스 활용으로 가치 회수 비교적 쉬움. air-gapped LLM 옵션은 정부·국방·금융 customer 진입 시 필수. 1·2순위 마감 후 자연스러운 진입.

### 4.4 보류 권장 — 후보 3 multi-region/cluster

**근거**: 가설 단계. 글로벌 fleet customer 진입 또는 DR 요구 명시 *후* 진입. 회귀 위험 높음 + 1개월+ 작업으로 가설 기반 진입 비효율.

### 4.5 보류 권장 — 후보 2 D8 청구권 코드 분리

**근거**: D1 변리사 출원 사용자 외부 트랙 의존. 출원 *전* enterprise 코드 추가는 patent disclosure 위험(D8 결정 §4 명시). 출원 후 E32 stage trigger 시 즉시 진입 가능. build tag scaffold는 이미 존재(E31).

### 4.6 carryover 3종 — 보류 유지

§2 분석대로 Manual·E22-F·scanrun extras 모두 첫 paying customer 진입 *후* 재평가 권장. Phase 6 진입에 영향 0.

### 4.7 권장 진입 순서 timeline (보수적)

| 순서 | 후보 | 추정 누적 시간 | trigger 시점 |
|---|---|---|---|
| 1순위 | 후보 1 customer onboarding | 1~2주 | 본 design doc 채택 직후 |
| 2순위 | 후보 4 audit chain key rotation | 4~6주 누적 | 1순위 마감 + customer 진입 *전* compliance baseline 강화 |
| 3순위 | 후보 5 LLM advisor 옵트인 강화 | 6~10주 누적 | 2순위 마감 + enterprise customer 명시 요구 시 |
| 보류 | 후보 3 multi-region | — | 글로벌 fleet customer 진입 후 |
| 보류 | 후보 2 D8 청구권 | — | D1 KR 우선출원 완료 후 |

**Phase 6 마감 추정**: 보수적 6~10주 (1·2·3순위 순차) — 후보 1·4·5 모두 마감 시 enterprise + compliance + air-gapped 세 축 강화 baseline 완성. 다만 customer 진입 시점에 따라 carryover trigger + 후보 3·2 진입 가능성 평가.

---

## 5. 각 후보별 합성 전략 옵션

각 후보 1~2 페이지로 간략 정리. 본격 분석은 후속 별 design doc(권장 1·2순위는 Phase 6 진입 시 우선).

### 5.1 후보 1 합성 전략 옵션 (3종)

**옵션 A — E38 docs only**: 가장 작음. 첫 customer 가이드 + walkthrough script만 추가. customer intake는 수동(현재 admin이 tenant 생성·role binding). 추정 0.5주, 회귀 위험 0. 단점: customer가 "guide 따라하기 부담" 느낌. 적용 시점: customer 합의 전 마케팅 자료 단계.

**옵션 B — docs + intake API**: 옵션 A + customer intake API/UI 추가. signup → tenant 자동 생성 → 첫 admin user invite → role binding. 추정 1~1.5주, 회귀 위험 낮음(thin wrapper). **권장** — paying customer 가치 즉시 회수 + 운영 부담 감소. 신규 endpoint: `POST /api/v1/intake/start` + `POST /api/v1/intake/{id}/complete`(이메일 verify token). RBAC: 신규 `intake_anonymous` 권한 + 완료 시 자동 admin role binding. handler thin wrapper(domain `tenant.Service`+`integration` 재사용).

**옵션 C — 풀 onboarding wizard**: 옵션 B + 첫 PoC 시나리오 (robot 등록 wizard + 첫 scan 자동 trigger + 첫 report PDF 자동 생성·서명·외부 검증 데모). 추정 2~3주, 회귀 위험 중(scan/report flow 의존). UX 강점이나 Phase 5 baseline UI가 이미 결선이라 ROI 비교적 낮음. customer 직접 demo 시 가치. 옵션 B 마감 후 customer 첫 PoC 시점에서 trigger 가능.

### 5.2 후보 2 합성 전략 옵션 (3종 — 출원 후 진입 가정)

**옵션 A — 1순위 결합 청구항만 (A-1 + B-1 + C-1 + D-3)**: 4 알고리즘 본체 + 단위 테스트만. 추정 2~3주(출원 후). enterprise 가치 즉시. 나머지 3 패키지(selectdisclose · rostopo · fleetxval)는 placeholder 유지.

**옵션 B — 7 패키지 모두 1차 구현**: A 옵션 + 나머지 3 패키지(selectdisclose는 ZK 또는 redact 알고리즘 / rostopo는 ROS2 그래프 cross-validation / fleetxval은 fleet 간 cross-validation). 추정 1.5~2개월. 청구항 전체 cover.

**옵션 C — 7 패키지 stub interface + 1순위 4 알고리즘 본체** (**권장**): 7 패키지에 interface 정의 + noop 구현 + 1순위 4 알고리즘만 본체. 추정 1개월. 미래 확장 표면 + 즉시 가치.

### 5.3 후보 3 합성 전략 옵션 (3종 — customer trigger 후)

**옵션 A — region 도메인만**: region 메타데이터 + tenant.region 컬럼. cross-region replication 0. 추정 0.5주. 단순 region tagging.

**옵션 B — region + read-replica**: 옵션 A + PostgreSQL streaming replication + region read endpoint. 추정 2주. 단방향 글로벌 read.

**옵션 C — 풀 multi-master**: 옵션 B + write conflict 해결 + audit chain region anchor. 추정 1개월+. **글로벌 fleet customer 명시 요구 시만**.

### 5.4 후보 4 합성 전략 옵션 (3종)

**옵션 A — 정기 rotation only**: scheduler(`internal/platform/scheduler/`) + 새 key 생성 + signer hot-swap. key escrow 0. 운영자 승인 flow 0. 추정 1~1.5주. 회귀 위험 낮음. compliance baseline. 설정: `audit.rotation.intervalDays=90` 등 config 추가. 단점: 운영자 가시성 낮음(rotation 자동만).

**옵션 B — 정기 + 운영자 승인 + audit emit** (**권장**): 옵션 A + admin UI 승인 (rotation 발동 시 admin notification + 승인 후 실행) + `audit_chain.key_rotated` event(원칙 §1·§9 일관) + 외부 검증 도구(`fg-verify`) rotation aware 갱신(여러 epoch chain anchor 검증) + 운영자 docs(rotation 시점 + 외부 검증 수순). 추정 2~3주. 회귀 위험 중. compliance 강한 cover. 마이그레이션 1건(audit_chain_keys 테이블 — epoch 별 public key 보존, 외부 검증용).

**옵션 C — 옵션 B + key escrow (분할 보관)**: 옵션 B + Shamir secret sharing 분할 + N-of-M 복원 + escrow 운영자 admin UI. 추정 4주+. 회귀 위험 높음(escrow 분할 실패 시 audit chain 차단 위험). 군사·금융 customer 명시 요구 시. 본 design doc 시점에는 가설.

### 5.5 후보 5 합성 전략 옵션 (3종)

**옵션 A — Ollama 어댑터 production hardening**: 현 `internal/platform/llm/ollama` 어댑터에 TLS·재시도·timeout·자체 host validation 추가. air-gapped 가이드 docs(Ollama self-host + LLM 모델 download + bootstrap 설정). 추정 0.5~1주. 회귀 위험 0(LLM은 옵트인, noop fallback 항상 존재). 첫 customer가 air-gapped 명시 시 즉시 회수.

**옵션 B — 옵션 A + advisor reasoning trace 강화** (**권장**): 옵션 A + advisor 도구 호출 audit emit(`audit.kind="advisor.tool_called"` + payload tool name + input redacted + output digest, P5·P11 일관) + UI trace 표시(advisor 결과 옆에 reasoning trace tab) + docs(advisor explainability). 추정 2~3주. 회귀 위험 낮음~중(audit emit 추가는 도메인 hand-off 검토). enterprise + compliance 두 축 cover.

**옵션 C — 옵션 B + 다국어 LLM 모델 매핑 + 사용자 지정 모델 추가 UI**: 옵션 B + 다국어 LLM 모델 매핑(advisor가 사용자 locale 자동 감지) + admin UI 모델 추가/제거(Ollama 모델 dynamic load). 추정 4주+. 옵션 B 마감 후 customer 명시 요구 시.

---

## 6. 결정 항목 (D-PHASE6-1 ~ D-PHASE6-5)

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시 + 다음 세션 즉시 진입 부담 0.

### D-PHASE6-1 — 본 문서 채택 여부

- (1) **채택** — Phase 6 backlog로 본 문서 진입, 우선순위 합의 후 후보 1순위 design doc 또는 직접 코드 진입 (**권장 default**).
- (2) 수정 후 채택 — 후보 추가/제외 또는 우선순위 재배열 후 채택.
- (3) 거부 — 본 문서 비채택, Phase 6 backlog 별 접근.

**근거**: Phase 5 5 epic 100% 마감 + 5 후보 매트릭스 + 권장 1순위까지 정리되어 다음 round 즉시 사용 가능. memory 일관.

### D-PHASE6-2 — Phase 6 1순위 채택

- (1) **후보 1 customer onboarding 보강** — ROI ★★★★★, 1~2주, 즉시 진입 가능 (**권장 default**).
- (2) 후보 4 audit chain key rotation — compliance 강한 가치, 2~4주, design doc 우선.
- (3) 후보 5 LLM advisor 옵트인 강화 — enterprise 옵션, 2~4주.
- (4) 후보 3 multi-region — 가설 단계, 1개월+ (보류 권장).
- (5) 후보 2 D8 청구권 — D1 출원 의존 (보류 권장).

**근거**: 후보 1은 첫 paying customer 진입을 직접 가속. 사용자 외부 트랙 의존 0 + Phase 5 baseline 즉시 회수. customer 진입은 D5/D6/D8 등 모든 결정의 trigger.

### D-PHASE6-3 — 한 번에 1 epic 진행 vs 병렬 2 epic

- (1) **1 epic 진행** — Phase 5 후속처럼 design doc 1개 → Stage 1·2·3 순차. context 안정 (**권장 default**).
- (2) 2 epic 병렬 — 후보 1 + 후보 4 동시 (worktree 2개). 같은 round 압축. 단 도메인 충돌 사전 검토 필수.
- (3) 후보 1만 우선, 마감 후 후보 4 진입 (옵션 1 동등하나 명시).

**근거**: 후보 1은 1~2주 작은 epic이라 1 epic 진행이 정합. 후보 4 진입 시점에서 후보 5와의 병렬 가능성 재평가. memory `feedback_parallel_agents.md`는 매 stage 시작 시 재평가 의무.

### D-PHASE6-4 — 각 후보별 design doc 우선 vs 직접 코드 진입

- (1) **design doc 우선** — 후보 1 design doc 작성 후 Stage 1 진입. memory `feedback_design_doc_first.md` 1일+ 임계 (**권장 default**).
- (2) 직접 코드 진입 — 후보 1은 작아서 design doc 생략 가능. Stage 1 즉시.
- (3) docs only — 후보 1 옵션 A(E38 docs only) 선택 시 design doc 없이 docs PR.

**근거**: 후보 1은 1~2주 작업으로 1일+ 임계 충족. design doc 작성 시 옵션 A·B·C 비교 + Stage 분해 + 결정 항목으로 다음 세션 즉시 진입 부담 0. 작은 epic이지만 customer intake API 도입은 기존 endpoint와의 hand-off 명시 필요.

### D-PHASE6-5 — Phase 5 carryover (Manual·E22-F·scanrun extras) 보류 유지 vs 명시 진입

- (1) **보류 유지** — 3 carryover 모두 첫 paying customer 진입 *후* 재평가 (**권장 default**).
- (2) Manual fixture Stage 3 low 5건만 진입 — 0.5~1일, 변환률 96.0% 도달.
- (3) E22-F 옵션 A 진입 — 0.5일, schema 가독성 한정 가치.
- (4) scanrun extras epic C(circuit breaker)만 진입 — 1.5주, customer 진입 *전* 영구 장애 robot 시나리오 가설.

**근거**: 3 carryover 모두 design doc에서 보류 권장 default. 첫 customer 진입 *전*에는 가설 기반 진입 비효율. Phase 6 1순위(customer onboarding)와 자연스러운 trigger 대기.

---

## 7. Phase 5 → Phase 6 전환 절차

본 문서 채택(D-PHASE6-1=1) 후 다음 세션 진입 절차:

1. **SESSION_HANDOFF.md "현재 상태 한 줄"** 갱신 — Phase 5 5 epic 100% 마감 → Phase 6 backlog 채택, 1순위 = 후보 1 customer onboarding 보강 (head 본 commit).
2. **SESSION_HANDOFF.md "결정 로그"** 신규 entry 추가 — 본 design doc 채택 + D-PHASE6-1~5 권장 default 명시.
3. **SESSION_HANDOFF.md "진행 중 선택지"** 갱신 — 1·2·3순위 후보를 진입 옵션으로 + 보류 carryover 명시.
4. **README.md** Phase 5 마감 + Phase 6 진입 한 줄 갱신.
5. **`docs/design/notes/`** 다음 design doc 작성 — `customer-onboarding-design.md`(또는 동등 파일명) — 옵션 A·B·C 비교 + Stage 분해 + D-ONBOARD-N 권장 default. memory `feedback_design_doc_first.md` 일관.
6. **Stage 1 진입** — design doc 채택(D-ONBOARD-1) 후 Stage 1 commit.

memory `feedback_no_rest_recommendation.md` — "잠시 휴식" 옵션 자동 포함 X.

---

## 8. 회귀 위험 / 운영 고려

- **본 문서 자체 영향**: 0. docs only, 코드 0 / 마이그레이션 0 / pack 변경 0 / API 0.
- **Phase 5 마감 baseline**: 5 epic 100% 마감으로 paying customer 진입 가능 baseline 보장. scanrun SSH + RBAC + PWA + RBAC fleet + SSO 결선 — 본 문서 채택 여부와 무관하게 운영 가능.
- **carryover 보류 일관**: 3 carryover 모두 design doc 권장 default 따름 — 회귀 위험 0.
- **사용자 외부 트랙 의존 0**: 본 문서는 D1·E36·E37 등 사용자 외부 트랙에 직접 의존하지 않음(후보 2만 D1 출원 의존, 보류 권장).
- **다음 세션 진입 부담 0**: D-PHASE6-1~5 모두 권장 default 명시 — 사용자 round 1회로 합의 가능.

---

## 9. 참조

### 9.1 Phase 5 design doc 6종

- `docs/design/notes/scanrun-ssh-integration-design.md` — 539줄, 옵션 비교 + Stage 분해 + D-SCAN-N. Stage 1~5c 마감.
- `docs/design/notes/pwa-offline-design.md` — 553줄, D-PWA-N. Stage 1~4 마감.
- `docs/design/notes/rbac-fine-grained-design.md` — 568줄, D-RBAC-N. Stage 1~5 마감.
- `docs/design/notes/rbac-fleet-scope-precision-design.md` — 660줄, D-RBACEX-N. Stage 1~5 마감 + Stage 6 closing.
- `docs/design/notes/pwa-persist-design.md` — 585줄, D-PWAPER-N. Stage 1~3 + operations docs 마감.
- `docs/design/notes/scanrun-extras-design.md` — 667줄, D-SCANEX-N. 권장 옵션 B 보류.

### 9.2 carryover 3종

- `docs/design/notes/cis-manual-21-fixture-design.md` — 391줄, D-MAN-N. Stage 3 low 5건 보류 권장.
- `docs/design/notes/e22-f-boolean-recovery-design.md` — 417줄, D-E22F-N. 옵션 C 보류 영구.
- `docs/design/notes/scanrun-extras-design.md` — 667줄(상기 중복 참조), D-SCANEX-N. 옵션 B 보류 (customer trigger).

### 9.3 설계서

- `docs/design/11-tech-stack-and-roadmap.md` — §11.13 로드맵 Phase 0~5 + §11.16 결정 로그 D1~D8.
- `docs/design/12-migration-and-non-goals.md` — 비목표 + 자산 승계.
- `docs/design/13-patent-strategy.md` — D8 청구권 전략(후보 2 의존).
- `docs/design/01-principles.md` — 12 원칙(원칙 §2 옵트인 / §3 에어갭 1급 / §5 DDD 경계 / §11 explainability).

### 9.4 SESSION_HANDOFF 결정 로그

- 2026-05-15 RBAC fleet Stage 5/6 마감(epic 5/5 close, Phase 5 100%).
- 2026-05-15 Phase 5 후속 Stage 1·2·3·4 2~3종 병렬 패턴.
- 2026-05-15 Phase 5 후속 design doc 3종(scanrun extras + RBAC fleet + PWA persist).

### 9.5 memory feedback

- `feedback_design_doc_first.md` — 1일+ 임계 design doc 우선.
- `feedback_design_doc_conservative.md` — 잠재 효과/시간 보수적.
- `feedback_parallel_agents.md` — 매 stage 시작 시 병렬 가능성 재평가.
- `feedback_no_rest_recommendation.md` — 휴식 옵션 자동 포함 X.
- `feedback_user_tracks.md` — D1·E36·E37 등 사용자 외부 트랙 제외.
