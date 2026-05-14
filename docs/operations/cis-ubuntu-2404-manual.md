# CIS Ubuntu 24.04 — Manual fixture 운영자 가이드

> **대상**: rosshield CIS Ubuntu 24.04 pack의 `assessment_status="Manual"` 항목.
> **상태**: Stage 1 — high 3건만 작성. medium/low는 후속 Stage.

CIS Ubuntu 24.04 baseline의 일부 항목은 site policy·환경 의존이라 자동 변환이 부적절합니다(false PASS 위험). rosshield는 이런 항목을 `op="manual"` evaluation rule로 표현하고, 운영자가 환경별 판정 기준에 따라 직접 검토합니다.

## 1. 분류와 위치

| 디렉토리 | 용도 |
|---|---|
| `packs/cis-ubuntu-2404/checks/manual/<id>.yaml` | 운영자가 작성한 manual check 정의(prompt + defaultVerdict) |
| `packs/cis-ubuntu-2404/selftest/manual/<id>.yaml` | 본 정의가 의도대로 동작하는지 검증하는 selftest fixture |

`pack-tools convert`는 본 디렉토리 두 개를 보존합니다(round-trip backup → write → restore). 자동 변환 결과로 덮어쓰지 않습니다.

## 2. evaluation rule schema

```yaml
spec:
  auditCommand: "true"           # manual은 audit cmd 실행 의미 없음 — 항상 "true"
  evaluationRule:
    op: manual
    prompt: |-
      <CIS audit text의 review 절을 한국어 운영자 안내로 재작성>

      확인 절차:
        1. <명령>
        2. <명령>
      판정 기준:
        PASS  — <조건>
        FAIL  — <조건>
        REVIEW — <조건 — 보통 site policy 미정의>
    defaultVerdict: review        # pass | fail | review (기본 review)
```

**defaultVerdict 매핑**:
- `pass` → `EvalStatus.PASS` (환경상 항상 만족)
- `fail` → `EvalStatus.FAIL` (환경상 항상 미흡)
- `review` → `EvalStatus.INDETERMINATE` (운영자 정성 검토 필요, default)

**audit 입력은 무시됩니다** — 어떤 stdout/stderr/exitCode가 와도 결과는 `defaultVerdict` 그대로.

## 3. selftest fixture 작성

selftest는 manual rule이 의도대로 verdict를 반환하는지 검증합니다. `defaultVerdict=review`라면 모든 case가 `INDETERMINATE`를 expectedOutcome으로 가집니다.

```yaml
apiVersion: rosshield.io/v1
kind: SelfTest
metadata:
    checkId: <id>
spec:
    cases:
        - name: "default review verdict (no audit cmd execution)"
          input:
              stdout: ""
              stderr: ""
              exitCode: 0
          expectedOutcome: INDETERMINATE
        - name: "audit input ignored — verdict still review"
          input:
              stdout: "<임의 stdout>"
              stderr: ""
              exitCode: 0
          expectedOutcome: INDETERMINATE
```

`go test ./internal/domain/benchmark/ -run ManualFixturesRoundTrip`로 모든 manual fixture가 통과하는지 회귀 가능합니다.

## 4. Stage 1 — high 3건 (작성 완료)

| ID | 카테고리 | defaultVerdict | 운영자 판정 핵심 |
|---|---|---|---|
| 1.1.1.10 | C2 (HW dependent) | review | 12 fs 모듈 중 site workload 미사용분 blacklist 확인 |
| 5.3.3.2.3 | C1 (Policy) | review | minclass·dcredit·ucredit·lcredit·ocredit 5개 매개변수 site policy 부합 |
| 5.4.1.2 | C1 (Policy) | review | login.defs PASS_MIN_DAYS + shadow 4번째 필드 ≥1 |

## 5. Stage 2·3 — medium 9건 + low 5건 (후속)

설계서: `docs/design/notes/cis-manual-21-fixture-design.md` §7.3·7.4.

- Stage 2 medium 9건: 1.2.1.1, 2.1.22, 4.2.5, 4.3.7, 4.4.2.3, 4.4.3.3, 6.1.1.2, 6.1.1.3, 7.1.13
- Stage 3 low 5건: 1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8 (first paying customer 또는 enterprise 요청 시)

## 6. 새 manual fixture 추가 절차

1. `packs/cis-ubuntu-2404/checks/manual/<id>.yaml` 작성 — §2 schema
2. `packs/cis-ubuntu-2404/selftest/manual/<id>.yaml` 작성 — §3 fixture
3. `go test -run ManualFixturesRoundTrip ./internal/domain/benchmark/` — round-trip 통과 확인
4. `pack-tools convert` 재실행 — manual fixture 보존(round-trip backup) 확인
5. 본 문서 §4 표에 추가
