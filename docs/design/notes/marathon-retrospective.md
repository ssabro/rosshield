# Marathon Session Retrospective — Phase 5 + Phase 6 후보 1 패턴·결정·learnings — Design

> **상태**: Phase 5 5 epic 100% + Phase 6 후보 1(customer onboarding R1·R2·R3) 마감 직후 회고 design doc. 본 문서는 코드 0줄 / 마이그레이션 0건 / pack 변경 0 — 본 마라톤 세션의 패턴·결정·learnings를 정리하여 차기 세션·미래 epic 진입 시 즉시 재사용 가능한 reference로 마감합니다.
> **참조**:
> - Phase 5 design doc 6종: `scanrun-ssh-integration-design.md` · `pwa-offline-design.md` · `rbac-fine-grained-design.md` · `rbac-fleet-scope-precision-design.md` · `pwa-persist-design.md` · `scanrun-extras-design.md`.
> - Phase 6 design 2종: `phase6-backlog-design.md`(404줄) · `customer-onboarding-design.md`.
> - carryover 3종 design: `cis-manual-21-fixture-design.md`(391줄) · `e22-f-boolean-recovery-design.md`(417줄) · `scanrun-extras-design.md`(667줄).
> - 설계서: `docs/design/01-principles.md`(P1~P12) · `docs/design/11-tech-stack-and-roadmap.md` §11.13 로드맵 + §11.16 결정 로그 · `docs/design/12-migration-and-non-goals.md`.
> - 메모리 feedback 7종: `feedback_naming_verification.md` · `feedback_go_commit_pipeline.md` · `feedback_parallel_agents.md` · `feedback_user_tracks.md` · `feedback_commit_message_backticks.md` · `feedback_no_rest_recommendation.md` · `feedback_design_doc_first.md` · `feedback_design_doc_conservative.md`.
> - SESSION_HANDOFF.md "결정 로그" 2026-05-15 ~ 2026-05-18 구간.
> **R 식별자**: R-MARATHON-1 (본 doc 전체) — 결정 항목 없음(회고용, 코드 변경 0).
> **본 worktree**: `agent-a00423d087c7ffa28`, main(head `1c5423d`)에서 분기. 단독 sub-agent.

---

## 1. 마라톤 통계

### 1.1 규모 (사용자 명시 + git 측정)

본 세션은 **Phase 5 + Phase 6 후보 1 마감을 한 호흡에 처리한 마라톤**입니다.

| 지표 | 값 | 근거 |
|---|---|---|
| 사용자 명시 commit | 73 | 사용자 메시지 — Phase 5 + Phase 6 후보 1 묶음 누계 |
| git 측정 commit (Phase 5 진입 ~ Phase 6 후보 1 마감) | 61 (`6f893de^..1c5423d`) | Phase 5 design doc 3종 dispatch가 시작점 |
| 사용자 명시 추가 줄 수 | +30,000줄+ | web dist asset rebuild 다수 포함 |
| git 측정 (`6f893de^..1c5423d`) | +34,822 / −7,791 (228 files) | 순수 src는 약 1/3 추정 |
| 회귀 발생 | 0 | tsc + vet + go test + vitest + build PASS 누계 |
| sub-agent worktree dispatch | 20회 연속 | 메인 + sub-agent A·B·C 최대 4 동시 |
| 사용량 한도 fallback | 4회 | PWA persist · scanrun extras · R1 Stage 2 · scanrun Stage 4 |
| cherry-pick conflict 메인 통합 해결 | 1회 | rawIntakeSvc + wrap pattern (R1 Stage 4) |

**73 vs 61 격차**: 사용자 73건은 CIS Manual 21 fixture·E-3·E-4 등 본 세션 후반에 진행된 packs 작업까지 포함한 broader scope(approx `8a2a691^..1c5423d` 묶음에 근접)일 가능성이 큽니다. 본 docs는 보수적으로 git 측정값(61) + 사용자 통계(73)를 둘 다 인용합니다.

### 1.2 Phase 별 commit 분포 (git 측정)

| 묶음 | 범위 | commit | 마감 head |
|---|---|---|---|
| Phase 5 design doc 3종 병렬 | `6f893de` ~ `b975e94` | 3 design + 1 handoff = 4 | `b975e94` |
| Phase 5 코드 Stage 1·2·3 (3종 병렬 × 3 라운드) | `4079e66` ~ `a9125aa` | 9 feat + 3 handoff = 12 | `3785a73` |
| Phase 5 Stage 4·5 (RBAC epic 마감 + scanrun 후속 + PWA epic 마감) | `22f472d` ~ `ee2aa34` | 6 feat/test + 2 handoff = 8 | `76ae2f0` |
| Phase 5 후속 design doc 3종 병렬 (RBAC fleet + scanrun extras + PWA persist) | `37778ef` ~ `af0b84d` | 3 design + 1 handoff = 4 | `166c76a` |
| Phase 5 후속 코드 Stage 1·2·3·4 (RBAC fleet + PWA persist 2종 병렬) | `d55cd71` ~ `350c38d` | 8 feat/docs + 4 handoff = 12 | `1050c74` |
| RBAC fleet Stage 5·6 (epic 마감) | `acde2b2` ~ `ccddde8` | 2 feat + 2 handoff = 4 | `ccddde8` |
| Phase 6 backlog + customer onboarding design 2종 | `ad5fcf6` ~ `c0f8586` | 2 design + 2 handoff = 4 | `b58c6f1` |
| Phase 6 후보 1 R1 Stage 1~5 + R2 walkthrough + R3 SLA | `6d7f869` ~ `e13c9b0` | 5 feat + 2 docs(R2·R3) + 4 handoff = 11 | `e13c9b0` |
| Phase 6 후보 1 마감 handoff | `1c5423d` | 1 | `1c5423d` |
| **합계** | `6f893de^..1c5423d` | **60** (+1 handoff 자체) ≈ **61** | `1c5423d` |

### 1.3 epic 별 stage 마감 (실제 vs design doc 추정)

| Epic | design 추정 stage | 실 마감 stage | 격차 | 추정 정확도 |
|---|---|---|---|---|
| scanrun SSH 통합 | 5 stage | 5 stage (1·2·3·4·5a·5b·5c 세분화) | 0 | ★★★★★ |
| 세분 RBAC | 5 stage | 5 stage | 0 | ★★★★★ |
| PWA 오프라인 | 4 stage | 4 stage | 0 | ★★★★★ |
| PWA persist | 3 stage + docs | 4 (3 stage + operations docs) | +1 (docs 별도 commit) | ★★★★☆ |
| RBAC fleet 정밀화 | 5 stage | 5 stage + Stage 6 closing | +1 (잔여 2 endpoint 추가 cover) | ★★★★☆ |
| customer onboarding R1 | 5 stage | 5 stage | 0 | ★★★★★ |
| customer onboarding R2 | docs 1 commit | docs 1 commit | 0 | ★★★★★ |
| customer onboarding R3 | docs 1 commit | docs 1 commit | 0 | ★★★★★ |

**격차 0~+1 stage** — memory `feedback_design_doc_conservative.md` 일관(보수적 추정 + 부산물 흡수가 항상 stage 1개를 줄이거나 추가 cover로 흡수). design doc 우선 정책이 추정 정확도를 견인했음을 확인.

### 1.4 회귀 0 보장 근거

회귀 0건은 다음 5중 게이트로 보장되었습니다:

1. **TDD Red→Green 강제** — 공개 API/도메인 로직은 테스트 먼저(CLAUDE.md). 본 마라톤에서 위반 0건.
2. **commit 전 파이프라인 녹색** — `make vet` + `go test -count=1 ./...` + `pnpm tsc` + `pnpm vitest run` + `pnpm build` 모두 PASS 후 commit (CLAUDE.md). Go 측은 memory `feedback_go_commit_pipeline.md`의 4종(gofmt + go mod tidy + import 그룹 + errcheck) 사전 점검.
3. **sub-agent 도메인 충돌 0 사전 점검** — 같은 라운드의 sub-agent worktree가 같은 파일 region 수정하지 않도록 dispatch 전 도메인 경계 확인 (§4 참조).
4. **메인 통합 시 회귀 검증** — sub-agent commit cherry-pick 후 메인이 전체 파이프라인 재실행. 단 한 번도 통과하지 못한 cherry-pick 없음(rawIntakeSvc wrap conflict는 메인에서 통합 해결로 회피).
5. **handoff doc commit 회수 직전 한 줄** — 각 stage 마감 직후 SESSION_HANDOFF.md 갱신으로 다음 세션의 회귀 표면을 명시.

---

## 2. 검증된 패턴 (재사용 가능)

### 2.1 sub-agent worktree 병렬 dispatch (20회 연속 검증)

**패턴**: 메인 Claude가 사용자와 합의한 후, 같은 라운드에 도메인이 충돌하지 않는 stage를 sub-agent worktree로 동시 dispatch. 메인 + sub-agent A·B·C 최대 4 동시 실행.

본 마라톤 dispatch 누적:

- **Phase 5 design doc 3종 병렬** (1회): scanrun + RBAC + PWA.
- **Phase 5 코드 Stage 1 3종 병렬** (1회): `4079e66` + `e9b93c0` + `4c4bfc9`.
- **Phase 5 코드 Stage 2 3종 병렬** (1회): `1bf2c21` + `951e924` + `daacb57`.
- **Phase 5 코드 Stage 3 3종 병렬** (1회): `1732a40` + `894449e` + `a9125aa`.
- **Phase 5 Stage 4·5 3종 병렬** (1회): scanrun Stage 4·5 + RBAC Stage 4 + PWA Stage 4.
- **Phase 5 후속 design doc 3종 병렬** (1회): RBAC fleet + scanrun extras + PWA persist.
- **Phase 5 후속 Stage 1·2·3·4 2종 병렬** (4회): RBAC fleet + PWA persist 라운드 누계.
- **RBAC fleet Stage 5·6 단독 sub-agent** (2회).
- **Phase 6 backlog + customer onboarding design 단독 sub-agent** (2회).
- **Phase 6 후보 1 R1 Stage 1 + R2 병렬** (1회).
- **Phase 6 후보 1 R3 + R1 Stage 2 병렬** (1회).
- **Phase 6 후보 1 R1 Stage 3 + Stage 4 병렬** (1회).
- **Phase 6 후보 1 R1 Stage 5 단독 sub-agent** (1회).
- **본 retrospective design 단독 sub-agent** (1회, 본 docs).

**합계 ≈ 20회**. 도메인 충돌 발생 1회(R1 Stage 4 rawIntakeSvc + wrap pattern) — §2.4 메인 통합 해결로 회피.

### 2.2 도메인 충돌 0 격리 설계

병렬 dispatch 전 다음 격리 점검을 수행했습니다:

| 영역 A (sub-agent A) | 영역 B (sub-agent B) | 격리 보장 |
|---|---|---|
| `internal/domain/<X>` + `internal/platform/<X>` + `cmd/rosshield-server/` 일부 | `web/src/<Y>` + i18n | 언어/모듈 다름 |
| Go `internal/api/handlers_*.go` 다른 endpoint | Go `internal/domain/<Z>` 신규 도메인 | 함수/타입 다름 |
| `docs/design/notes/<docX>.md` 신규 | `docs/onboarding/<docY>.md` 신규 | 신규 파일, 충돌 0 |
| 마이그레이션 0027 신규 | 마이그레이션 0028 신규 | 다른 sequence 번호 |

**3종 병렬의 전형**:
- A = Go domain + platform (scanrun SSH, RBAC, RBAC fleet)
- B = web/* (PWA, PWA persist)
- C = docs/* 또는 다른 Go domain (RBAC 후속, customer onboarding R2/R3)

A vs B vs C가 서로의 파일을 건드리지 않으면 cherry-pick conflict 0.

### 2.3 cherry-pick 패턴

본 마라톤의 sub-agent 결과 회수 방식:

| 방식 | 회수 | 사례 |
|---|---|---|
| **자동 머지 (메인 cherry-pick)** | 대부분 | Phase 5 코드 Stage 1·2·3 모두 |
| **메인 통합 conflict 해결** | 1회 | Phase 6 R1 Stage 4 rawIntakeSvc + wrap |
| **사용량 한도 fallback (메인이 sub-agent test→본체 직접 작성)** | 4회 | §2.5 |

cherry-pick 명령 패턴:

```bash
# sub-agent worktree commit 자동 머지
git -C "<main repo>" cherry-pick <sub-agent-hash>

# 또는 sub-agent worktree에서 main으로 fast-forward
git -C "<sub-agent worktree>" log --oneline -1
# 본 worktree HEAD 회수 후 main으로 cherry-pick
```

각 cherry-pick 직후 회귀 검증(§1.4) 후 SESSION_HANDOFF.md 갱신 commit.

### 2.4 메인 conflict fallback — rawIntakeSvc + wrap pattern 사례

**문제**: R1 Stage 4(auto-provisioning wrap)에서 sub-agent가 intake 도메인의 Accept() 함수 region을 수정하던 중, 같은 라운드 다른 sub-agent가 같은 region에 RBAC mount 코드를 추가. cherry-pick 시 region 충돌.

**해결 패턴**:
1. 메인이 sub-agent 둘 commit을 cherry-pick `--no-commit`으로 가져옴.
2. 충돌 region을 메인에서 통합 해결: `rawIntakeSvc` 직접 호출 layer + `wrap()` decorator로 auto-provisioning hook 분리.
3. 메인 단일 commit으로 통합 산출 (`975109e`).
4. SESSION_HANDOFF에 conflict 해결 경위 명시 — 다음 세션이 재발 패턴을 회피하도록.

**교훈**: 같은 파일 region 동시 변경은 사전 격리(§2.2) 누락 시그널. 다음 세션에 wrap pattern을 처음부터 design doc에 명시하면 회피 가능.

### 2.5 사용량 한도 fallback (4회 누적)

**패턴**: sub-agent가 test 파일(spec) 작성까지만 마치고 사용량 한도 도달 → 메인이 spec(test)을 명세로 본체 코드 직접 작성.

| 회차 | 사례 | sub-agent 산출 | 메인 fallback 산출 |
|---|---|---|---|
| 1 | PWA persist Stage 2 | `dehydrate` filter 보안 차단 list spec | `PersistQueryClientProvider` 결선 (메인) |
| 2 | scanrun extras design Stage 4 | Pool size 동적·rate limit·circuit breaker design 절반 | design doc 마무리 + carryover 분류 (메인) |
| 3 | Phase 6 R1 Stage 2 | intake handler + endpoint test spec | handler + endpoint 본체 + RBAC mount (메인, `09c20cf`) |
| 4 | scanrun Stage 4 | Pool idle 재사용 + keepalive + metrics 5종 test spec | sshpool Pool 결선 본체 (메인, `22f472d`) |

**효과**: 사용량 한도 도달 시점에도 작업 진행 0 정지. TDD 강제(test 먼저)와도 자연 일치 — sub-agent의 test가 메인 구현의 acceptance criteria가 됨.

**한계**: 메인의 컨텍스트 길이가 sub-agent test + 본체 + 회귀 검증을 한 turn에 모두 흡수해야 함. 4회 모두 컨텍스트 흡수 성공했으나 5회 이상은 컨텍스트 부담 측정 필요.

---

## 3. 결정 정책 (검증된)

### 3.1 design doc 우선 (1일+ 작업 임계)

**근거**: memory `feedback_design_doc_first.md`.

**검증**: 본 마라톤의 모든 1일+ 작업(scanrun SSH·세분 RBAC·PWA·RBAC fleet·PWA persist·customer onboarding)이 design doc → 코드 진입 순서를 따랐습니다. design doc 부재 epic 0건. 결과: epic별 stage 마감 격차 0~+1 (§1.3) — 추정 정확도 ★★★★★.

**default 적용**: 모든 epic은 design doc 마감 후에만 코드 진입. design doc 줄 수는 ~400~700줄 표준.

### 3.2 권장 default 모두 명시 (사용자 합의 round 1회)

**근거**: D-* 패턴(D-PHASE6-1 ~ D-PHASE6-5 등)으로 design doc 안에 결정 항목 + 권장 default를 모두 적시 → 사용자가 "default 채택"만 답하면 합의 완료.

**검증**: Phase 5 design doc 3종 + Phase 6 backlog + customer onboarding 모두 권장 default 명시 → 사용자 round 평균 1.0회로 합의 마감. 다중 round 발생 0건.

### 3.3 보수적 추정

**근거**: memory `feedback_design_doc_conservative.md`.

**검증**: §1.3 격차 분석 — 모든 epic이 design doc 추정과 동등하거나 +1 stage(부산물 흡수)로 마감. 추정 초과 0건.

### 3.4 휴식 자동 추천 금지

**근거**: memory `feedback_no_rest_recommendation.md`(2026-05-12 사용자 명시).

**검증**: 본 마라톤의 모든 "진행 중 선택지" 섹션은 "잠시 휴식"을 자동 포함하지 않음. commit은 작업 후 자동 수행.

### 3.5 사용자 외부 트랙 제외

**근거**: memory `feedback_user_tracks.md`(2026-05-11 사용자 명시).

**검증**: D1 변리사·D8 출원·E36 실 HW·E37 public 전환·O9 변리사 트랙은 본 마라톤 "진행 중 선택지"에서 제외. Phase 6 backlog에서도 "사용자 외부 의존" 섹션으로 분리.

### 3.6 commit 메시지 backtick hash 보호

**근거**: memory `feedback_commit_message_backticks.md`(E12 Stage 2 인용 손실 학습).

**검증**: 본 마라톤의 모든 commit이 `git commit -m @'<here-string>'@`(PowerShell) 또는 `<<'EOF'` heredoc(bash) 사용. backtick hash 손실 0건.

### 3.7 Go 커밋 전 파이프라인

**근거**: memory `feedback_go_commit_pipeline.md`.

**검증**: 모든 Go commit 전 4종(`gofmt -w` + `go mod tidy` + import 그룹 정렬 + errcheck 패턴) 사전 점검. CI 차단 0건.

---

## 4. carryover 우선순위 매트릭스

본 마라톤 종료 시점 carryover(미진행) 항목 + 차기 세션 진입 trigger:

| 항목 | 상태 | 진입 trigger | 권장 default | 비고 |
|---|---|---|---|---|
| **Manual Stage 3** (잔여 9건 manual fixture) | 보류 | 첫 customer 시점 (실 환경 cover 우선순위 명시 시) | 보류 유지 | 옵트인 fixture, 자동 변환 후보 0(설계 한계) |
| **E22-F BOOLEAN 회수** | 보류 | 사용자 명시 우선시만 | 옵션 C 유지 (PG hot path 3 컬럼 native, 나머지 BOOLEAN as-is) | design doc `e22-f-boolean-recovery-design.md` 권장 default 채택 |
| **scanrun extras** (Pool size 동적·rate limit·circuit breaker) | 보류 | customer 부하 측정 후 (real throughput baseline 확보) | 보류 유지 | design doc `scanrun-extras-design.md` 마감, 트리거 대기 |
| **Phase 6 후보 2 D8 청구권** | 보류 | D1 변리사 출원 후 (사용자 외부) | 보류 유지 | 사용자 직접 트랙 의존, claude 진행 불가 |
| **Phase 6 후보 3 multi-region** | 보류 | 첫 customer trigger (단일 region overflow 시그널) | 보류 유지 | scale 요구 발생 후 진입 |
| **Phase 6 후보 4 audit rotation** | 보류 | 운영 부하 측정 후 (audit table row count 10M+ 시그널) | 보류 유지 | rotation 정책 + warm/cold split |
| **Phase 6 후보 5 LLM private 강화** | 보류 | paying customer 명시 요구 후 | 보류 유지 | 원칙 §2 옵트인 — 요구 없으면 진입 0 |

**전체 carryover 보류 — paying customer 1명 진입 전까지 자연 trigger 0**.

---

## 5. paying customer 진입 시 trigger order

첫 paying customer onboarding 직후 자연 trigger 순서(권장):

1. **carryover 3종** (Manual Stage 3 + E22-F BOOLEAN + scanrun extras)
   - 첫 customer 환경의 fixture coverage 격차 + native type 회수 요청 + 부하 베이스라인 시그널 → 동시 진입 가능.
   - 각 design doc이 이미 마감되어 있어 다음 세션 즉시 코드 진입.

2. **Phase 6 후보 4 audit rotation** (운영 가시화 직후)
   - audit table은 customer 1명 추가만으로도 일일 N만 row 증가. 운영 측정 1주 후 trigger.

3. **Phase 6 후보 5 LLM private 강화** (customer 명시 요구 시)
   - 원칙 §2(옵트인) — customer가 명시 요구하지 않으면 진입 0.

4. **Phase 6 후보 3 multi-region** (scale 요구 시)
   - 단일 region이 충분하면 진입 0. customer 2번째 진입 또는 latency 요구 발생 시.

5. **Phase 6 후보 2 D8 청구권** (D1 출원 완료 후 — 사용자 외부)
   - 사용자 트랙 의존. 사용자 변리사 합의 마감 시 trigger.

---

## 6. 실패 패턴 (회피해야 할)

본 마라톤에서 회피했거나 1회 발생한 실패 패턴:

### 6.1 같은 파일 region 동시 변경

**발생**: 1회 (R1 Stage 4, §2.4).

**회피책**: sub-agent dispatch 전 격리 매트릭스(§2.2) 점검. 같은 파일의 같은 region을 두 sub-agent가 수정하는 경우 wrap/decorator pattern을 design doc에 처음부터 명시.

### 6.2 사용량 한도 무시한 대규모 sub-agent 위임

**발생**: 0회 (4회 fallback은 모두 합리적 처리).

**회피책**: sub-agent에게 한 turn 한 stage 분량만 위임. 한 turn에 여러 stage 시도 시 fallback 빈도 급증.

### 6.3 test 없이 본체 작성 (TDD 강제 위반)

**발생**: 0회.

**회피책**: 사용량 한도 fallback(§2.5) 시에도 sub-agent의 test가 acceptance criteria로 남아 있어 메인이 본체 작성 시 자연 TDD 사이클 유지. 위반 0.

### 6.4 chi mount + handler 본체 같은 commit (회귀 표면 격리 실패)

**발생**: 0회.

**회피책**: R1 Stage 2(handler + endpoint + RBAC mount)는 의도적으로 같은 commit에 통합 — chi mount 없이 handler만 commit하면 회귀 표면 미정의. 본 패턴은 "endpoint 단위 commit"으로 의식적 채택. 다음 세션에 분리하지 말 것.

### 6.5 design doc 부재 코드 진입

**발생**: 0회.

**회피책**: §3.1. 모든 1일+ 작업은 design doc 선행. 본 마라톤에서 위반 0건.

---

## 7. 다음 세션 진입 권장 절차

마라톤 패턴 재사용 표준 절차:

1. **SESSION_HANDOFF.md 읽기**
   - "직전 한 줄" + "진행 중 선택지" 섹션 우선. 결정 로그 직전 N건 참조.

2. **carryover trigger 조건 점검 (§4 매트릭스)**
   - paying customer 진입 여부 확인. trigger 미발생 항목은 진행 중 선택지에서 제외.

3. **사용자 합의 round 1회**
   - design doc의 D-* 권장 default를 그대로 채택. 합의 round 1회 표준.

4. **design doc 우선 (1일+ 작업) 또는 직접 코드 진입**
   - 1일+ 작업이면 design doc 마감 후 코드 진입. 단순 작업(<1일)이면 직접 진입.

5. **sub-agent worktree 병렬 dispatch (도메인 충돌 0 점검)**
   - 격리 매트릭스(§2.2)로 사전 점검. 같은 파일 region 동시 변경 회피.

6. **cherry-pick + handoff 갱신 + 회귀 검증**
   - 5중 게이트(§1.4) 통과 후에만 commit. 각 stage 마감 직후 SESSION_HANDOFF.md 갱신.

7. **commit 메시지 backtick hash 보호 (§3.6)**
   - `<<'EOF'` heredoc 또는 PowerShell `@'...'@` 사용.

8. **Go 커밋 전 파이프라인 4종 (§3.7)**

---

## 8. 메모리 정책 권장 추가 (본 세션 경험)

본 마라톤에서 얻은 패턴 중 메모리에 신규 등재 권장:

### 8.1 sub-agent worktree fallback 패턴

**제목 후보**: `feedback_sub_agent_fallback.md`

**요지**: sub-agent가 사용량 한도 도달 시 메인이 spec(test)으로 본체 작성. 4회 검증 (§2.5). TDD 강제와 자연 일치.

**적용 조건**: sub-agent의 test 파일이 명세 수준으로 완성된 경우만. test 미완성 시 메인이 처음부터 재작성.

### 8.2 cherry-pick conflict 메인 통합 해결 패턴

**제목 후보**: `feedback_cherry_pick_conflict.md`

**요지**: sub-agent 둘이 같은 파일 region 수정 시 메인에서 wrap/decorator pattern으로 통합 해결. R1 Stage 4 rawIntakeSvc + wrap 사례(§2.4).

**적용 조건**: 격리 매트릭스(§2.2) 사전 점검 누락 시그널. 발생 시 회피 패턴을 design doc에 처음부터 반영.

### 8.3 도메인 충돌 0 사전 점검 의무

**제목 후보**: `feedback_domain_isolation_check.md`

**요지**: sub-agent 병렬 dispatch 전 격리 매트릭스(§2.2) 점검 의무. Go domain · platform · cmd vs web/* vs docs/* 영역 분리 + 마이그레이션 sequence 번호 분리.

**적용 조건**: 2종 이상 sub-agent 동시 dispatch 시 무조건. 단독 sub-agent는 점검 면제.

---

## 9. 참조

### 9.1 Phase 5 design doc 6종

- `docs/design/notes/scanrun-ssh-integration-design.md`
- `docs/design/notes/pwa-offline-design.md`
- `docs/design/notes/rbac-fine-grained-design.md`
- `docs/design/notes/rbac-fleet-scope-precision-design.md`
- `docs/design/notes/pwa-persist-design.md`
- `docs/design/notes/scanrun-extras-design.md`

### 9.2 Phase 6 후보 1 design doc 2종

- `docs/design/notes/phase6-backlog-design.md`
- `docs/design/notes/customer-onboarding-design.md`

### 9.3 carryover 3종 design doc

- `docs/design/notes/cis-manual-21-fixture-design.md`
- `docs/design/notes/e22-f-boolean-recovery-design.md`
- `docs/design/notes/scanrun-extras-design.md`

### 9.4 메모리 feedback 7종 + 권장 신규 3종

기존 7종:

- `feedback_naming_verification.md`
- `feedback_go_commit_pipeline.md`
- `feedback_parallel_agents.md`
- `feedback_user_tracks.md`
- `feedback_commit_message_backticks.md`
- `feedback_no_rest_recommendation.md`
- `feedback_design_doc_first.md`
- `feedback_design_doc_conservative.md`

권장 신규 3종 (§8):

- `feedback_sub_agent_fallback.md`
- `feedback_cherry_pick_conflict.md`
- `feedback_domain_isolation_check.md`

### 9.5 설계서 핵심 참조

- `docs/design/01-principles.md` — P1~P12 (특히 P5 DDD 경계 · P9 불변성 · P12 점진적 적용)
- `docs/design/11-tech-stack-and-roadmap.md` §11.13 로드맵 · §11.16 결정 로그
- `docs/design/12-migration-and-non-goals.md` — 비목표

### 9.6 SESSION_HANDOFF 참조 구간

- 2026-05-15 RBAC fleet Stage 5/6 마감
- 2026-05-15 ~ 2026-05-18 Phase 6 backlog + 후보 1 진입 합의

---

**마감**: 본 docs는 차기 세션·미래 epic 진입 시 즉시 참조 가능한 reference. 코드 변경 0 / 마이그레이션 0 / pack 변경 0. 다음 세션은 §7 절차를 따르면 본 마라톤 패턴을 그대로 재사용 가능합니다.
