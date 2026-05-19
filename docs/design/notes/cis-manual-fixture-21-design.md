# CIS Ubuntu 24.04 Manual fixture 잔여 cover — env-var skip 패턴 (Phase 5 carryover) — Design

> **상태**: Phase 5 carryover design (코드 0줄, pack content 변경 0). 직전 design doc `cis-manual-21-fixture-design.md`(prompt UI 기반 `op: manual` 채택, Stage 1+2 = 12 fixture 완료)의 **후속 + 대안 비교**. 본 문서는 잔여 5건 placeholder를 customer-supplied **env var skip 패턴**(옵션 A+B)으로 cover하여 CIS Ubuntu 24.04 pack을 **100%(자동 or manual or env-skip 명시)** 도달시키는 길을 설계합니다.
>
> **읽는 순서 가이드**: 본 문서를 읽기 전에 `cis-manual-21-fixture-design.md`(이하 "선행 doc")를 먼저 읽으면 본 문서의 §3·§7 옵션 비교 맥락이 명확해집니다.

## 1. 상태 / 배경 (정확 진단)

본 doc 작성 시점(2026-05-19, head `e4989f9`) `packs/cis-ubuntu-2404/` 정확 inventory:

| 항목 | 수 | 비고 |
|---|---|---|
| 총 check yaml | **301** | `checks/*.yaml` 289 + `checks/manual/*.yaml` 12 |
| 자동 변환 check | 289 | `evaluationRule.op` ∈ {`contains`, `exact`, `regex`, ...} |
| `op: manual` 운영자 fixture | **12** | `checks/manual/{1.1.1.10, 1.2.1.1, 2.1.22, 4.2.5, 4.3.7, 4.4.2.3, 4.4.3.3, 5.3.3.2.3, 5.4.1.2, 6.1.1.2, 6.1.1.3, 7.1.13}.yaml` |
| degraded placeholder 잔여 | **5** | `checks/{1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8}.yaml` — `evaluationRule.value = <degraded — Phase 2 fixture required>` |
| selftest manual fixture | 12 | `selftest/manual/<id>.yaml` (1:1, expectedOutcome=INDETERMINATE 패턴) |

### 1.1 "21건"의 의미 재정의

`SESSION_HANDOFF.md` carryover 메시지의 "manual fixture 21건"은 선행 doc의 **21건 원본 inventory** (Stage 분해 전):

```
1.1.1.10  1.2.1.1   1.2.1.2   1.2.2.1   2.1.22    3.1.1     4.2.5
4.3.3     4.3.7     4.4.2.3   4.4.3.3   5.3.3.2.3 5.4.1.2   6.1.1.2
6.1.1.3   6.1.2.1.2 6.1.3.5   6.1.3.6   6.1.3.8   6.2.3.21  7.1.13
```

이 21건은 v0.4.x cascade ~ v0.6.x 사이 다음 4 트랙으로 분기됐습니다:

| 분기 | 수 | 결과 yaml 위치 | 비고 |
|---|---|---|---|
| **자동 변환 완료** | 4 | `checks/{1.2.2.1, 3.1.1, 4.3.3, 6.2.3.21}.yaml` | `** PASS **`/`** FAIL **` 마커 합성 — 선행 doc §6.1 후보가 모두 epic E-4로 처리 |
| **`op: manual` 운영자 fixture** | 12 | `checks/manual/<id>.yaml` | 선행 doc Stage 1 (high 3) + Stage 2 (medium 9) — prompt UI 패턴 |
| **degraded placeholder (잔여)** | 5 | `checks/{1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8}.yaml` | 선행 doc Stage 3 "low" — **본 doc 대상** |
| (중복) | — | — | 21 = 4 + 12 + 5 |

즉 잔여 **5건**(선행 doc Stage 3, "low" niche 환경)이 본 doc 대상입니다. **"잔여 21건" 표현은 carryover 메시지 시점에 자동 4건/`op: manual` 12건 완료 진척을 반영하지 않은 것**으로 보수적으로 추정합니다(`feedback_design_doc_conservative.md`).

### 1.2 CIS Ubuntu 24.04 cover 비율 현황

- 자동(deterministic) 변환: 289/301 = **96.0%**
- 자동 + manual prompt: 301/301 = **100.0%** (단 placeholder 5건은 `<degraded — Phase 2 fixture required>` 문자열로 무조건 FAIL 평가 — **운영 시 false FAIL 발생**)
- 본 doc 완료 시: 자동 289 + `op: manual` 12 + env-skip 5 = **301/301 무손실 100%**

### 1.3 본 doc 범위

- 잔여 5건의 evaluation 패턴 결정 (env-var skip vs 추가 `op: manual` vs 그대로 두기 vs LLM advisor 등 ≥3 옵션)
- env-var skip 채택 시 schema·prefix·SKIP 결과 처리 결정
- customer onboarding 문서(`docs/onboarding/cis-customer-policy.md` 신규)에 customer-supplied policy 정의 절차
- 본 doc 자체는 코드/pack 변경 0

## 2. 위협 모델 / 요구사항

### 2.1 위협 모델

- **T1 — false FAIL by placeholder**: 잔여 5건이 현재 `evaluationRule.value = <degraded — Phase 2 fixture required>` 그대로 → scan 실행 시 audit cmd 출력이 절대 매칭하지 않아 **무조건 FAIL** 산출. customer audit 시점 보고서가 잘못된 FAIL 5건으로 dashboard 신뢰도 ↓.
- **T2 — false PASS by silent skip**: env-var 미정의 시 묵시적 PASS 처리하면 customer가 site policy 미수립 상태에서 **PASS로 착각**. audit 통과 환경이 실제 보안 통제 없는 상태.
- **T3 — policy drift**: customer가 env var 정의했다가 후속 운영 변경 시 env var 삭제·갱신 누락 → 평가가 silently SKIP/FAIL로 변경.
- **T4 — audit report integrity**: SKIP 결과가 보고서·해시 체인에 어떻게 기록되는지 명확하지 않으면 외부 검증자가 "왜 평가 안 됐는지" 추적 불가.

### 2.2 요구사항

- **R1** — 잔여 5건이 customer audit 결과에서 **false FAIL을 emit하지 않아야** 한다.
- **R2** — env-var 미정의 시 **SKIP/REVIEW 명시**, 절대 silent PASS 금지 (T2).
- **R3** — SKIP 결과는 audit report `Manual review required` 섹션에 별 카운트 + 해시 체인에 평등하게 append (R8 불변성 원칙).
- **R4** — customer onboarding 시점 env-var 정의 절차가 README + onboarding doc 양쪽에 명시.
- **R5** — Phase 8+ LLM advisor 통합 가능성을 schema가 막지 않는다 (forward compat).
- **R6** — 기존 자동 289 + `op: manual` 12 cover 패턴과 schema·selftest harness 일관 (선행 doc D-MAN-2/D-MAN-3 호환).

## 3. 옵션 비교 (≥3)

본 doc의 핵심 결정은 **잔여 5건의 evaluation 패턴**입니다. 4개 옵션 + 권장 default:

| 옵션 | 접근 | 강점 | 약점 | 적합도 |
|---|---|---|---|---|
| **A** | customer-supplied env var 패턴 — `$ROSSHIELD_CIS_<ID>_POLICY` 정의 시 audit cmd가 site policy 검증 / 미정의 시 SKIP marker emit | site 의존 자연스럽게 표현, audit cmd 그대로 활용 | env var 5개 정의 부담, customer 학습 곡선 | ★★★★☆ |
| **B** | 선행 doc Stage 3 그대로 적용 — `op: manual` + prompt UI (운영자 직접 PASS/FAIL/REVIEW 입력) | 선행 doc 패턴 일관, schema 추가 0 | scan 자동화 흐름에서 매번 운영자 입력 필요, fleet scan 비현실적 | ★★★☆☆ |
| **C** | LLM advisor가 site policy 추정 + 자동 fixture 생성 (옵트인) | AI 자동화, customer 학습 부담 ↓ | Phase 0 LLM 비활성 원칙(02-system), Phase 8+ 트랙 의존 | ★☆☆☆☆ (Phase 5 부적합) |
| **D** | customer onboarding wizard (UI interactive) — 첫 scan 직전 5개 질문 → 답변을 env var/profile에 저장 | UX 최상, 학습 곡선 ↓ | UI 추가 부담, Phase 5 범위 외 (Phase 6+ UI 트랙) | ★★☆☆☆ |
| **A+B 결합 (권장)** | env var 정의되면 audit cmd 실행 + 결과 평가 (PASS/FAIL), 미정의면 `op: manual` prompt fallback (선행 doc 패턴) | 자동화 가능 fleet에서는 deterministic, 정책 미수립 customer는 prompt 유도 | schema 분기 1개 추가 | ★★★★★ |

**권장 default: A+B 결합**. 이유:
- (1) env var 정의 customer는 fleet scan 시 deterministic PASS/FAIL 산출 → R1 만족
- (2) env var 미정의 customer는 `op: manual` prompt fallback → 선행 doc Stage 3 패턴 일관, R2 만족
- (3) Phase 8+ LLM advisor 도입 시 env var 자동 추정 기능 추가 가능 → R5 forward compat
- (4) onboarding wizard(옵션 D)는 Phase 6+ 후속 트랙으로 자연 연결 → R4 충족 점진적

## 4. 아키텍처

### 4.1 yaml schema (A+B 결합 패턴)

각 잔여 5건의 audit yaml 구조:

```yaml
apiVersion: rosshield.io/v1
kind: Check
metadata:
  id: <ID>
  title: "<title>"
  severity: <severity>
spec:
  auditCommand: |-
    bash -c 'env_var="ROSSHIELD_CIS_<ID_SLUG>_POLICY"
    if [ -z "${!env_var:-}" ]; then
      printf "** SKIP ** customer policy 미정의 (env %s 필요)\n" "$env_var"
      exit 0
    fi
    # site policy 적용 검증 — env var 값에 따라 분기
    <원본 CIS audit cmd>
    # 결과를 ** PASS **/** FAIL ** 마커로 변환
    '
  evaluationRule:
    op: contains
    value: '** PASS **'
  manualFallback:
    # 선행 doc op:manual schema 호환
    prompt: |
      CIS <ID> — <site policy 검토 항목 한국어 요약>
      env $ROSSHIELD_CIS_<ID_SLUG>_POLICY 정의 시 자동 평가됩니다.
    defaultVerdict: review
```

### 4.2 SKIP marker 처리 (D-CM-3 결정)

audit cmd가 `** SKIP **` emit 시:
1. `evaluationRule.op: contains, value: '** PASS **'` → 매칭 실패
2. **신규 fallback 분기**: `auditCommand` stdout이 `** SKIP **` 포함 시 evaluator는 `INDETERMINATE` outcome emit (FAIL 아님)
3. audit report rendering: `INDETERMINATE` 결과를 `Manual review required` 섹션에 별 카운트 (PASS/FAIL과 분리)
4. 해시 체인: outcome=`INDETERMINATE` + stdout/stderr 그대로 append (R3 불변성)

이 분기는 기존 selftest harness의 `expectedOutcome: INDETERMINATE` 패턴(선행 doc `selftest/manual/1.1.1.10.yaml` 참조)과 동일 — **harness 코드 신규 분기 0**.

### 4.3 env var prefix·naming (D-CM-2 결정)

| 옵션 | 예시 |
|---|---|
| **권장: `ROSSHIELD_CIS_<ID_NORM>_POLICY`** | `ROSSHIELD_CIS_1_2_1_2_POLICY` (dot → underscore) |
| `LODESTAR_CIS_<ID>_POLICY` | 브랜드명 prefix, 다른 OS pack과 자연 분리 |
| `CIS_UBUNTU_2404_<ID>` | pack-specific, 다른 OS pack에 재사용 불가 |

권장: `ROSSHIELD_CIS_<ID_NORM>_POLICY` — `rosshield` 코드 네임스페이스 일관(CLAUDE.md), dot은 env var 명에 invalid이므로 underscore 정규화.

### 4.4 잔여 5건 site policy 정의 예시

| ID | 환경 의존 항목 | env var 값 예시 | site policy 검증 cmd 골격 |
|---|---|---|---|
| 1.2.1.2 | 허용 apt repo 목록 | `archive.ubuntu.com,security.ubuntu.com` | `apt-cache policy | grep -E "$(echo $POLICY | tr , '\|')"` |
| 6.1.2.1.2 | journal-upload URL + cert path | `https://logs.example.com:443,/etc/ssl/journal.pem` | URL/cert 분리 후 `[ -f <cert> ]` + `grep URL= /etc/systemd/journal-upload.conf` |
| 6.1.3.5 | rsyslog config 필수 라인 | `*.crit\t/var/log/warn,auth,authpriv.* /var/log/secure` | `grep` chain |
| 6.1.3.6 | rsyslog remote loghost | `loghost.example.com:514` | `grep "@@\?$(echo $POLICY | cut -d: -f1)" /etc/rsyslog.conf /etc/rsyslog.d/*` |
| 6.1.3.8 | logrotate 주기 정책 (일) | `7` | `grep -E "^(daily|weekly|rotate $POLICY)" /etc/logrotate.conf` |

### 4.5 audit report rendering 영향

기존 report에 카테고리 3개(PASS·FAIL·SKIP/REVIEW)가 별도 섹션으로 분기 — `INDETERMINATE` outcome은 이미 기존 12 `op: manual` fixture가 emit하는 outcome과 동일 → **report 코드 변경 0** (기존 분기 재사용).

## 5. TDD 진입

### 5.1 selftest 패턴 (R6 호환)

기존 `selftest/manual/<id>.yaml`의 `expectedOutcome: INDETERMINATE` 패턴을 잔여 5건에도 적용 + 추가 case:

```yaml
apiVersion: rosshield.io/v1
kind: SelfTest
metadata:
  checkId: 1.2.1.2
spec:
  cases:
    - name: "env var 미정의 → SKIP marker"
      input:
        env: {}
        stdout: "** SKIP ** customer policy 미정의 (env ROSSHIELD_CIS_1_2_1_2_POLICY 필요)"
        exitCode: 0
      expectedOutcome: INDETERMINATE
    - name: "env var 정의 + repo 일치 → PASS"
      input:
        env: {ROSSHIELD_CIS_1_2_1_2_POLICY: "archive.ubuntu.com"}
        stdout: "** PASS **"
        exitCode: 0
      expectedOutcome: PASS
    - name: "env var 정의 + repo 불일치 → FAIL"
      input:
        env: {ROSSHIELD_CIS_1_2_1_2_POLICY: "archive.ubuntu.com"}
        stdout: "fail: rogue repo detected\n** FAIL **"
        exitCode: 0
      expectedOutcome: FAIL
```

### 5.2 round-trip 일관성

`ros2_jazzy_fixture_test.go` 패턴 적용 — fixture yaml 파싱 + audit cmd 골격이 SKIP/PASS/FAIL 각 marker를 정확히 emit하는지 검증. **단 audit cmd 실행은 selftest input 매개로 mock** (env var 주입 + stdout sample 비교).

### 5.3 D-CM-1 cover 범위 결정에 따른 우선순위

- 모든 잔여 5건 cover (옵션 1, 권장) → 5 yaml × 3 case = 15 selftest
- 핫 path만 (1.2.1.2 + 6.1.3.6 — 일반 fleet 일반적 환경) → 2 yaml × 3 case = 6 selftest

## 6. Stage 분해 (5 stage)

### 6.1 Stage 1 — 잔여 Manual inventory 정확 진단 + env var 정의 (~0.2일)

작업:
- 잔여 5건 yaml 본문 재독 → site policy 의존 항목 catalog (§4.4 표 검증·확장)
- env var 명·값 schema 결정 (D-CM-2)
- 선행 doc과의 schema cross-reference (특히 `op: manual` 호환성 D-MAN-3)

산출:
- 본 design doc §4.4 표 finalize
- env var 명·값 reference markdown (onboarding doc draft)

### 6.2 Stage 2 — 각 잔여 Manual에 env var 패턴 적용 (~0.5일)

작업:
- 잔여 5 yaml 갱신: §4.1 schema 적용
- audit cmd가 env var 확인 + SKIP/PASS/FAIL marker emit하는 bash 합성
- selftest harness가 `INDETERMINATE` outcome 인식하도록 fixture 패턴 통일 (선행 doc 12 `op: manual` selftest와 호환)
- pack manifest hash 갱신 (sign-pack 재실행)

산출:
- `packs/cis-ubuntu-2404/checks/{1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8}.yaml` 갱신 (5 file, ~50줄/건 = ~250줄)
- pack manifest 재서명

### 6.3 Stage 3 — selftest fixture 추가 (~0.3일)

작업:
- 각 잔여 5건당 selftest 3 case (SKIP·PASS·FAIL) 작성
- env var 주입 + stdout sample 검증 fixture
- ros2_jazzy_fixture_test.go 패턴 일관

산출:
- `packs/cis-ubuntu-2404/selftest/manual/{1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8}.yaml` 신규 (5 file × 3 case = 15 case, ~120줄)
- selftest CI 실행 시간 +1~2초 추정

### 6.4 Stage 4 — onboarding doc 작성 (~0.3일)

작업:
- `docs/onboarding/cis-customer-policy.md` 신규 (~150줄)
- env var 정의 절차, 5개 env var 명·값 의미·예시, profile 파일(.env)로 영구화 방법, fleet 분산 시 secret manager 연동 가이드
- README.md 또는 quickstart.md 링크 추가

산출:
- `docs/onboarding/cis-customer-policy.md` (~150줄)
- `docs/onboarding/README.md` 또는 `quickstart.md` 1줄 링크

### 6.5 Stage 5 — customer pilot 검증 (~0.5일, 별 sprint)

작업:
- 본인 fleet 또는 first paying customer 환경에 env var 정의 + 잔여 5건 scan 실행
- SKIP/PASS/FAIL 3 결과 모두 자연 산출되는지 확인
- audit report rendering이 `Manual review required` 섹션에 적절히 분류하는지 확인

산출:
- pilot 결과 회고 메모 (`SESSION_HANDOFF.md` 결정 로그 1줄)
- 회귀 발견 시 backlog 등록

### 6.6 총 추정

Stage 1+2+3+4 = **1.3일** (Stage 5는 customer pilot 의존 별 sprint).

## 7. 결정 항목 (D-CM-N, 권장 default)

### D-CM-1 — cover 범위 (잔여 5건 전체 vs 핫 path만)

**옵션**:
1. (권장 default) **잔여 5건 전체** — CIS Ubuntu 24.04 cover 100% 도달
2. 핫 path 2건만 (1.2.1.2, 6.1.3.6) — 일반 fleet에 가장 자주 적용
3. 영구 placeholder 유지 — false FAIL 위험 그대로

**권장 default: 1**. 5건은 추가 작업 비용 차이 미미(~0.3일 + ~0.5일 = 0.8일), 100% cover의 영업·문서 효과 ↑.

### D-CM-2 — env var prefix

**옵션**:
1. (권장 default) `ROSSHIELD_CIS_<ID_NORM>_POLICY` — 코드 네임스페이스 일관, dot 정규화
2. `LODESTAR_CIS_<ID>_POLICY` — 브랜드 prefix
3. `CIS_UBUNTU_2404_<ID>` — pack-specific

**권장 default: 1**. CLAUDE.md 코드 네임스페이스 `rosshield` 일관, env var 명에 invalid char(dot) 회피.

### D-CM-3 — SKIP 결과 처리

**옵션**:
1. (권장 default) audit report `Manual review required` 별 섹션 + 해시 체인 일반 append (`INDETERMINATE` outcome) — 기존 `op: manual` 12 fixture와 동일 분기
2. SKIP을 별 outcome 신설 (`SKIPPED`) — schema 추가, evaluator·report·harness 모두 갱신 부담
3. SKIP을 FAIL로 합산 — false FAIL 회귀, R2 위반

**권장 default: 1**. 기존 `INDETERMINATE` outcome 재사용 → 코드 변경 0.

### D-CM-4 — LLM advisor 통합 시점

**옵션**:
1. (권장 default) Phase 8+ carryover (옵션 C) — 본 round 제외, env var 정의 자동 추정 기능을 LLM advisor 옵트인 트랙에 위임
2. 본 round 포함 — Phase 0 LLM 비활성 원칙(02-system-and-positioning) 위반, 일정 부담
3. 영구 미진행 — customer 학습 부담 영구 유지

**권장 default: 1**. CLAUDE.md "옵트인 지능화" 원칙 일관.

### D-CM-5 — onboarding wizard (옵션 D) 도입 시점

**옵션**:
1. (권장 default) Phase 6+ UI 트랙 — UI 추가 부담은 별 트랙에서 처리, 본 round는 markdown 가이드만
2. 본 round 통합 — UI 변경, Phase 5 범위 초과
3. 영구 markdown only — customer UX ↓ 영구

**권장 default: 1**. Phase 5 범위 보존, markdown 가이드로 1차 cover.

### D-CM-6 — selftest case 수 (잔여 5건당)

**옵션**:
1. (권장 default) **3 case** (SKIP·PASS·FAIL) — 모든 분기 검증
2. 2 case (SKIP·PASS) — FAIL 분기 trust by inspection
3. 1 case (SKIP만) — env var 정의 case 무검증

**권장 default: 1**. 회귀 위험 ↓, ~15 case 추가는 CI 시간 ~1~2초.

### D-CM-7 — manualFallback prompt 다국어

**옵션**:
1. (권장 default) **한국어 default** (선행 doc D-MAN-3과 동일)
2. 영어 default — 국제 customer 대비
3. 다국어 (한·영) — schema 부담 ↑

**권장 default: 1**. 선행 doc 패턴 일관, i18n은 Phase 1 별 트랙.

## 8. 변경 사항 outline (~600줄 추정)

| 파일 | 줄 수 추정 | 카테고리 |
|---|---|---|
| `packs/cis-ubuntu-2404/checks/{1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8}.yaml` | 5 × ~50 = ~250 | check yaml (env-var skip 패턴 + manualFallback) |
| `packs/cis-ubuntu-2404/selftest/manual/{1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8}.yaml` | 5 × ~25 = ~125 | selftest fixture (3 case) |
| `docs/onboarding/cis-customer-policy.md` | ~150 | onboarding guide 신규 |
| `docs/operations/cis-ubuntu-2404-manual.md` 갱신 | ~30 | 잔여 5건 env var 명·SKIP marker 안내 추가 |
| `packs/cis-ubuntu-2404/pack.yaml` (manifest hash 갱신) | ~5 | sign-pack 재실행 |
| (옵션) audit report rendering 갱신 | ~40 | `INDETERMINATE` 섹션 카운트 분리 — Phase 5 carryover의 carryover |

**총: ~600줄** (Stage 1+2+3+4 = 1.3일 추정). 옵션 audit report 부분 포함 시 ~640줄.

## 9. 검증

### 9.1 round-trip 일관

- ros2_jazzy_fixture_test.go 패턴 적용 — CIS도 같은 동적 fixture
- 잔여 5건 yaml 파싱 → audit cmd bash 합성 → 각 marker(SKIP/PASS/FAIL) emit 단위 확인
- selftest harness가 3 case 모두 `expectedOutcome` 매칭

### 9.2 env var 정의 시 PASS / 미정의 시 SKIP

- 단위 test: env var 주입 + audit cmd 실행 + stdout 매칭 검증
- 통합 test: fleet scan workflow에서 SKIP marker가 audit report `Manual review required` 섹션으로 routing되는지 e2e 확인

### 9.3 회귀 방지

- 자동 변환된 4건(1.2.2.1, 3.1.1, 4.3.3, 6.2.3.21)이 env var 패턴 도입 이후에도 영향받지 않는지 fixture round-trip 일관
- `op: manual` 12 fixture가 동일 evaluator 분기 사용하는지 (`INDETERMINATE` outcome path 1개로 통일)

## 10. 비즈니스 / 라이선스 영향

- **코어 라이선스**: Apache 2.0 (CLAUDE.md D5) — 변경 없음
- **pack content 라이선스**: CIS Ubuntu 24.04 자산 — Center for Internet Security 비상업 또는 member 라이선스 (별 doc `docs/operations/cis-ubuntu-2404-degraded.md` 또는 LICENSE-CIS 참조 필요)
- **customer-supplied env var의 PII 분류**: 일부 env var 값(예: `loghost.example.com`)은 customer 내부 인프라 정보 → secret manager에 보관 권장 (onboarding doc §"secret 관리")
- **enterprise license에 대한 영향**: 본 doc는 코어 트랙 — enterprise 분리 시점(D5 R30-4)에 customer-supplied env var 추정 기능(LLM advisor, 옵션 C)이 enterprise feature로 이관 가능성

## 11. 리스크

| ID | 리스크 | 영향 | 완화 |
|---|---|---|---|
| RC-1 | env var 5개 정의 customer 부담 | onboarding 학습 곡선 ↑ | Stage 4 onboarding doc + Phase 6+ wizard (D-CM-5) |
| RC-2 | env var 명 갱신 시 customer profile breaking | breaking change 발생 시 customer scan 일제 SKIP | env var 명을 D-CM-2 결정 후 영구 유지, deprecation 정책 별 doc |
| RC-3 | site policy 의존 cmd 골격 정확성 | false PASS 위험 | Stage 5 pilot 검증 + selftest 3 case 강제 |
| RC-4 | audit report `INDETERMINATE` 분기가 dashboard 집계에서 누락 | customer가 "5건 평가 안 된 줄 모름" | Stage 4 onboarding doc에 `Manual review required` 섹션 해석 가이드 |
| RC-5 | LLM advisor 트랙 지연 시 customer 학습 부담 영구 | UX ↓ | D-CM-4 옵션 1 default — Phase 8+ 보장은 별 트랙, RC-1 완화로 보완 |
| RC-6 | selftest harness가 env var 주입 패턴 미지원 시 추가 코드 필요 | Stage 3 일정 ↑ | Stage 1에서 harness 코드 cross-reference 우선 검증 |
| RC-7 | pack manifest hash 변경 + sign-pack 재실행 누락 시 release pipeline 실패 | release artifact 부재 | R30-3 sign-pack 워크플로우 Stage 2 직후 강제 실행 |

## 12. 결정 로그

| 날짜 | 결정 | 근거 |
|---|---|---|
| 2026-05-19 | 본 doc 작성 — 선행 doc Stage 3 "low" 5건을 env-var skip 패턴(A+B)으로 cover | 선행 doc은 prompt UI(`op: manual`) default — fleet scan 자동화에 비현실적, A+B 결합이 자동화 + fallback 양립 |
| 2026-05-19 | env var prefix = `ROSSHIELD_CIS_<ID_NORM>_POLICY` (D-CM-2) | CLAUDE.md 코드 네임스페이스 일관, dot 정규화 |
| 2026-05-19 | SKIP outcome = `INDETERMINATE` 재사용 (D-CM-3) | 기존 12 `op: manual` selftest 패턴과 동일, evaluator·harness·report 코드 변경 0 |
| 2026-05-19 | LLM advisor 통합 = Phase 8+ carryover (D-CM-4) | CLAUDE.md "옵트인 지능화" 원칙 일관, Phase 5 범위 보존 |
| 2026-05-19 | onboarding wizard = Phase 6+ UI 트랙 (D-CM-5) | Phase 5 범위 보존, markdown 가이드로 1차 cover |
| 2026-05-19 | manualFallback prompt 한국어 default (D-CM-7) | 선행 doc D-MAN-3 일관 |

## 13. 참조

- 선행 design doc: `docs/design/notes/cis-manual-21-fixture-design.md` — prompt UI(`op: manual`) 기반 Stage 1+2 = 12 fixture 완료
- 자동 변환 design doc: `docs/design/notes/cis-nomarker-31-analysis.md`
- D epic auditd: `docs/design/notes/cis-6-2-3-auditd-design.md`
- pack content: `packs/cis-ubuntu-2404/checks/*.yaml` (301 checks)
- 기존 `op: manual` 12 fixture: `packs/cis-ubuntu-2404/checks/manual/*.yaml`
- 잔여 placeholder 5건: `packs/cis-ubuntu-2404/checks/{1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8}.yaml`
- selftest 기존 패턴: `packs/cis-ubuntu-2404/selftest/manual/*.yaml` (12 file, `expectedOutcome: INDETERMINATE`)
- degraded 운영 가이드: `docs/operations/cis-ubuntu-2404-degraded.md`
- 기존 manual 운영 가이드: `docs/operations/cis-ubuntu-2404-manual.md`
- 변환기 코드: `cmd/pack-tools/converter/cis.go`
- selftest 자동 생성: `cmd/pack-tools/converter/selftest.go::GenerateSelfTestSkeletons`
- nrobotcheck baseline (audit text 출처): `D:\robot\dev\nrobotcheck\resources\baselines\cis_ubuntu_2404_benchmark.json`
- 설계 원칙: `docs/design/01-principles.md` §1(결정론) · §3(에어갭) · §6(결정론적 fallback) · §9(불변성) · §10(프라이버시 default)
- 메모리 패턴:
  - `feedback_design_doc_first.md` — 본 트랙 1.3일 누적이라 design doc 우선 적용
  - `feedback_design_doc_conservative.md` — "잔여 21건" 표현 정확 진단 후 실제 잔여 5건으로 보수적 재정의
  - `feedback_parallel_agents.md` — 본 sub-agent는 Phase 5 carryover 병렬 5 sub-agent 중 1
