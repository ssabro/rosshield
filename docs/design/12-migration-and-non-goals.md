# 12. nrobotcheck 자산 재활용 및 비목표·리스크

## 12.1 승계 철학

- **코드가 아니라 의사결정과 데이터를 승계**한다. 언어·아키텍처가 달라질 수 있다는 가정 하에, 가장 이전 가능한 자산부터 정리.
- **자동 이전 스크립트**를 만들되, 출시 시점에 필요한 것에만 한정.
- 승계 대상은 **"가치 대비 이전 비용이 낮은 것"**.

## 12.2 자산 분류

### Tier A — 직접 이전 (거의 수정 없이 재사용)

| 자산 | 현재 위치 | 목적지 |
|---|---|---|
| 벤치마크 CSV | `rawdata/ros2_jazzy_robot_benchmark.csv`, `rawdata/ros2_humble_robot_benchmark.csv` | 팩 포맷(`*.pack.tar.gz`)으로 변환 |
| CIS Ubuntu JSON | `resources/baselines/cis_ubuntu_2404_benchmark.json` | 팩 변환 |
| 프레임워크 매핑 | `resources/baselines/ros2_*_framework_*.json` | 매핑 팩으로 변환 |
| SCAP 연구 | `docs/SCAP_CONVERSION_RESEARCH.md` | `newprj/07-*` 참조 |
| UI shadcn 래퍼 | `src/renderer/src/components/ui/*` | 프론트 리포에 복사 |
| Tailwind 테마·토큰 | `tailwind.config.js` | 그대로 이관 |
| 로봇 등록·감사 UX 흐름 | 페이지 컴포넌트 | IA에 맞춰 재배치 |

### Tier B — 개념 승계 (재구현하되 설계 승계)

| 자산 | 승계 포인트 |
|---|---|
| DDD 도메인 분해 (8개) | 신 프로젝트에서 11개로 확장 (tenant·audit·reporting 독립) |
| v2 IPC 엔벨로프 `V2IpcResult<T>` | HTTP 엔벨로프로 직접 매핑 |
| `registerRoute()` 패턴 | OpenAPI 라우트 등록기로 |
| Lazy require 패턴 | 언어별 dependency injection 전략 |
| LLM fault-tolerance 경로 | LlmProvider 구조 승계 |
| Opt-in intelligence 플래그 | TenantSettings로 이전 |
| PassLogic 트리 | `evaluationRule.expression` 문법으로 |
| Peer grouping 로직 | Insight 파이프라인 규칙 |
| F17 Advisor tool use | Advisor 대화 오케스트레이터 |
| F19 온보딩 마법사 | Fleet → Add Robot 플로우 |
| Compliance Phase A~D.3 로직 | ComplianceProfile·자동 매핑 제안 |

### Tier C — 참고만 (실질 이전 없음)

| 자산 | 이유 |
|---|---|
| Electron main 프로세스 코드 | Go 백엔드로 대체 |
| `handlers.ts` IPC | OpenAPI + HTTP 라우트로 대체 |
| `better-sqlite3` 바인딩 | DB 드라이버 교체 |
| SSH 연결 풀 구현 | Go `crypto/ssh` 기반 재구현 |
| Native rebuild 스크립트 | 불필요 (정적 바이너리) |

## 12.3 데이터 마이그레이션

### 운영 데이터 이전 여부

- **출시 초기**: 필요 없음. 신 프로젝트는 새 고객에게 판매.
- **기존 nrobotcheck 사용자에게 어필 시**: `fg import nrobotcheck <sqlite-file>` CLI 제공.

### 마이그레이션 도구 스펙 (필요 시)

```
fg import nrobotcheck <path-to-nrobotcheck.db> \
    --tenant-id tn_xxx \
    --dry-run | --apply
```

- 로봇 엔터티·자격증명(재암호화) 이관.
- 과거 스캔 결과는 **선택적 이관** (Evidence가 충분히 남아있는 경우만).
- 감사 로그는 이관 불가(다른 체인).

## 12.4 벤치마크 팩 마이그레이션

### CSV → 팩 변환 도구

```
pack-tools convert \
    --input rawdata/ros2_jazzy_robot_benchmark.csv \
    --vendor "<ProductName>" \
    --output packs/ros2-jazzy-1.0.0
```

- 각 행을 CheckDefinition YAML로.
- `audit_command`를 `spec.auditCommand.argv`로 정규화.
- `PassLogic` 트리를 `evaluationRule.expression`으로.
- 한/영 필드는 그대로.
- Self-Test fixture는 **빈 상태로 스켈레톤**만 생성, 이후 수작업 채움.

### 품질 검증

- 변환 후 Self-Test 실행 → fixture가 없어 "degraded" 체크 목록 산출.
- Phase 2에서 fixture를 점진적으로 채움.

## 12.5 UI 컴포넌트 이전

### 그대로 가져오는 것

- `components/ui/*` (shadcn 스타일)
- Tailwind 설정·디자인 토큰
- 아이콘 선택 (Lucide React)

### 재설계하는 것

- 페이지 레이아웃 (멀티테넌시 네비게이션 변경)
- 로그인·SSO 플로우
- 역할별 메뉴 구성

### 폐기하는 것

- Electron preload 관련 훅
- IPC 직접 호출 코드 (HTTP 클라이언트 어댑터로 교체)

## 12.6 라이선스·법적 고려

- `nrobotcheck`의 라이선스 결정에 영향을 받음. 현재는 내부 프로젝트.
- 신 제품이 closed-source 또는 open-core라면, 이전 대상 자산은 **재라이선싱 가능한 범위**만.
- 3rd-party 의존성은 신 프로젝트에서도 호환 라이선스인지 재검토.

---

## 12.7 비목표 (명시적으로 하지 않을 것)

### 제품 범위

1. **자율 공격 에이전트 프레임워크가 되지 않는다** — CAI 영토.
2. **범용 멀티 LLM 오케스트레이션 플랫폼이 되지 않는다** — LLM 어댑터는 우리 기능을 위한 것만.
3. **범용 IT 보안 감사 도구가 되지 않는다** — ROS2 로봇이 주 대상, 확장은 플러그인으로.
4. **실시간 APM / 연속 관측이 되지 않는다** — 스냅샷 기반. 연속은 별도 agent 제품이 필요할 때 검토.
5. **로봇 제어·운용 기능은 넣지 않는다** — 보안 감사에 한정.

### 기술 범위

6. **자체 하드웨어를 설계·제조하지 않는다** — 어플라이언스는 이미지 + 레퍼런스 + 파트너.
7. **SaaS-only 모드가 되지 않는다** — 오프라인이 일등급.
8. **클라우드 의존을 전제하지 않는다** — 모든 기능이 로컬에서 완결 가능.
9. **모바일 앱 네이티브 개발을 하지 않는다** (v1) — 웹 앱의 모바일 뷰로 충분.
10. **자체 LLM을 학습·배포하지 않는다** — 오픈/상용 모델 활용.

### 고객·시장 범위

11. **비-ROS2 / 비-Ubuntu 로봇 지원을 주력으로 삼지 않는다** — v3+에 플러그인 확장.
12. **컨슈머 제품이 되지 않는다** — B2B 전용.
13. **실시간 SLA 24/7 운영**을 Phase 4 이전에 약속하지 않는다.
14. **자체 인증 기관·감사 서비스**를 제공하지 않는다 — 도구에 집중.

---

## 12.8 리스크 매트릭스

| # | 리스크 | 가능성 | 영향 | 완화 |
|---|---|:---:|:---:|---|
| R1 | CAI·유사 OSS가 감사 영역에 진입 | 중 | 중 | 포지셔닝(결정론·감사증거) 강화, 컨텐츠 해자 구축 |
| R2 | 팀 스택 선택 실패로 재작업 | 중 | 고 | Phase 0 스파이크, 점진적 검증 |
| R3 | 벤치마크 품질 부족으로 오탐·누락 다수 | 고 | 고 | Self-Test 의무, 내부 QA VM, 베타 고객 피드백 루프 |
| R4 | 국내 규제 해석 오류 | 중 | 고 | 법무·심사원 자문, 면책 조항, 인증 파트너십 |
| R5 | 어플라이언스 지원 부담 폭증 | 중 | 중 | 파트너 채널, 원격 지원 도구, 이미지 표준화 |
| R6 | LLM 비용 관리 실패 | 중 | 중 | 옵트인·쿼터·로컬 모델 우선, 월 상한 |
| R7 | 감사 체인 구현 버그로 외부 검증 실패 | 저 | 치명 | 전수 속성 기반 테스트, 외부 감사, 버그바운티 |
| R8 | 키 관리 사고(유출·분실) | 저 | 치명 | TPM 봉인, 키 로테이션, 복구 절차 문서화 |
| R9 | SSO 통합 복잡도 과소평가 | 중 | 중 | 초기에 OIDC 집중, SAML은 수요 검증 후 |
| R10 | 매출 목표 대비 초기 고객 부족 | 중 | 고 | PoC 전환율 모니터링, Free 티어로 파이프라인 형성 |
| R11 | 오픈소스 결정 뒤집힘 | 저 | 중 | Phase 0에 라이선스 결정 고정 |
| R12 | CI/CD 파이프라인 지연 | 저 | 중 | 경험 많은 DevOps 인력 Phase 1 전 확보 |
| R13 | 팩 서명 키 분실 | 저 | 치명 | 오프라인 백업 + 다중 서명자 |
| R14 | 경쟁사가 한국어 UX·ISMS-P 매핑을 빠르게 따라옴 | 중 | 중 | 매핑 업데이트 속도·고객사 인용·국내 파트너 우선권 |
| R15 | 데스크톱 Tauri 생태계 불안정 | 저 | 저 | Electron fallback 유지, 버전 보수적 선택 |

## 12.9 Go/No-Go 체크포인트

각 Phase 끝에 다음을 질문:

### Phase 1 끝

- [ ] 내부 환경 3대 로봇 감사 전체 성공?
- [ ] 서명 PDF 리포트 외부 검증 성공?
- [ ] Self-Test 커버리지 60%+?
- [ ] 팀 스택 결정 만족도?

### Phase 2 끝

- [ ] ISMS-P 매핑 품질(샘플 심사원 리뷰)?
- [ ] LLM 옵트인 시 드리프트 설명 유용성(베타 사용자 설문)?
- [ ] 결정론적 fallback 100% 커버?

### Phase 3 끝

- [ ] 첫 유료 Enterprise 계약 성사?
- [ ] 멀티테넌시 격리 감사 통과?
- [ ] SSO 호환성(3개 이상 IdP)?

### Phase 4 끝

- [ ] 파트너사 어플라이언스 PoC 성공?
- [ ] OTA 업데이트·롤백 시연?
- [ ] TPM 봉인 키 복구 시나리오 통과?

## 12.10 포기·피벗 조건

다음 조건 중 **2개 이상** 충족 시 전략 재검토:

- Phase 3 끝났는데 유료 고객 0명 유지 3개월 이상.
- 경쟁사가 동일한 "ROS2 컴플라이언스 감사" 포지션을 선점하고 동등 품질 제공.
- 핵심 규제(ISMS-P, NIS2 등) 체계가 근본적으로 변경되어 기존 매핑 가치 상실.
- 핵심 인력 2명 이상 동시 이탈.

피벗 옵션:

- 일반 IT 컴플라이언스 도구로 확장 (ROS2 특화 축 완화).
- OSS로 전환하고 유료 지원·컨설팅 수익 모델.
- 특정 산업(방산 등) 전용 SI 제품으로 집중.

## 12.11 이 문서의 핵심 결정

1. **자산은 설계·데이터 중심 승계**, 코드는 재작성이 빠르다.
2. **벤치마크 팩 변환 도구**는 초기 Phase 1에서 반드시 완성.
3. **비목표는 제품 관리의 무기** — "안 한다"가 "한다"보다 많아야 집중된다.
4. **리스크 R3(벤치마크 품질)과 R7(감사 체인 버그)가 치명 리스크** — 엔지니어링 투자 집중.
5. **Go/No-Go 체크포인트**에서 정직하게 멈출 용기가 있어야 한다.

---

## 문서 세트 끝

이 문서는 13개 설계서 세트의 마지막입니다. 주기적 업데이트와 결정 로그 추가가 살아있는 문서의 조건입니다.

- [README.md](./README.md) — 인덱스
- [00-mission-and-positioning.md](./00-mission-and-positioning.md)
- [01-principles.md](./01-principles.md)
- [02-system-overview-and-deployment.md](./02-system-overview-and-deployment.md)
- [03-architecture.md](./03-architecture.md)
- [04-domain-and-data-model.md](./04-domain-and-data-model.md)
- [05-api-and-auth.md](./05-api-and-auth.md)
- [06-security-and-tenancy.md](./06-security-and-tenancy.md)
- [07-scan-engine-and-benchmarks.md](./07-scan-engine-and-benchmarks.md)
- [08-intelligence-and-compliance.md](./08-intelligence-and-compliance.md)
- [09-ui-and-clients.md](./09-ui-and-clients.md)
- [10-audit-and-observability.md](./10-audit-and-observability.md)
- [11-tech-stack-and-roadmap.md](./11-tech-stack-and-roadmap.md)
- [12-migration-and-non-goals.md](./12-migration-and-non-goals.md) (이 문서)
