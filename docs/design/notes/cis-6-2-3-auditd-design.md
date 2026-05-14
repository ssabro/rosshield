# CIS 6.2.3.x auditd rules 21건 자동 변환 — Design

> **상태**: Phase 0 design (다음 세션 진입점). CIS Ubuntu 24.04 pack 자동 변환 차기 epic. 본 문서는 코드 0줄 — 합성 전략 결정·stage 분해까지만 마감.

## 1. 배경

CIS Ubuntu 24.04 pack 자동 변환률 **18.9% → 77.6%** (243/313)까지 9개 합성 패턴으로 끌어올렸습니다(직전 commit `9e003fa`). 잔여 NoMarker 56건 중 **6.2.3.x auditd rules 21건**이 가장 큰 미커버 그룹입니다(전체 미커버의 37.5%). 이 그룹을 cover하면 변환률은 약 84.3%(264/313)로 +6.7%p 상승 — 단일 epic으로 가장 큰 ROI.

목표: 6.2.3.x 21건의 audit 자연어 가이드를 결정론적 bash로 합성해 빌트인 pack에 자동 포함합니다(false positive 0). 21건 중 6.2.3.20(`-e 2` 단순 grep)은 직전에 이미 합성됨, 6.2.3.21은 Manual로 분류 — **신규 cover 대상은 19건**입니다.

## 2. 현재 schema·패턴

### 2.1 합성 dispatch (`cmd/pack-tools/converter/cis.go::convertCISItem`)

우선순위 7-tier (line 174~211):

1. `** PASS **` 마커 + bash hashbang → `extractCISBashBody` 그대로 wrap
2. `synthesizeCISShellAssertion` 분기(SSHD numeric/range/boolean → stat permission → grep verify → grep alternation → awk exact → expect-empty → expect-non-empty)
3. fallback: bash hashbang body가 있고 expect-empty/non-empty 키워드 매칭 시 `synthesizeBashBodyExpectEmpty/NonEmpty`로 base64 wrap
4. 모두 실패 → `degradedEvalRuleJSON` sentinel + `auditCommand: "true"` (manual fixture)

### 2.2 기존 합성 함수(재사용 후보)

| 함수 | 출력 패턴 | 6.2.3.x 적용성 |
|---|---|---|
| `synthesizeExpectEmpty(cmd)` | `out=$(cmd); [ -z "$out" ] && PASS` | △ — auditctl은 출력 non-empty가 정상이라 부적합 |
| `synthesizeExpectNonEmpty(cmd)` | `out=$(cmd); [ -n "$out" ] && PASS` | △ — "출력 존재"만 검증해 정확 매칭 결여 |
| `synthesizeExpectExact(cmd, val)` | `out=$(cmd); [ "$out" = "val" ] && PASS` | △ — 단일 라인 정확 매칭, multi-line expected 불가 |
| `synthesizeBashBodyExpectEmpty/NonEmpty(body)` | base64 hashbang body → exec → 출력 검사 | ○ — base64 wrap 인프라 재사용 |

### 2.3 selftest skeleton 자동 생성 (`selftest.go::GenerateSelfTestSkeletons`)

`evaluationRule`이 `{"op":"contains","value":"** PASS **"}` 정확 매칭이면 PASS/FAIL 두 fixture 자동 생성. 신규 합성 21건도 동일 마커를 사용하면 추가 코드 없이 skeleton 21개 자동 생성됩니다.

## 3. 21건 데이터 분석

### 3.1 audit text 형태 (sample 인용)

`D:\robot\dev\nrobotcheck\resources\baselines\cis_ubuntu_2404_benchmark.json` 9131~9782 line.

**Sample 1 — 6.2.3.1 (단순 watch + 단일 awk + 정확 매칭, expected 2 lines)**:
```
On disk configuration
Run the following command to check the on disk rules:
# awk '/^ *-w/ \
&&/\/etc\/sudoers/ \
&&/ +-p *wa/ \
&&(/ key= *[!-~]* *$/||/ -k *[!-~]* *$/)' /etc/audit/rules.d/*.rules
Verify the output matches:
-w /etc/sudoers -p wa -k scope
-w /etc/sudoers.d -p wa -k scope
Running configuration
Run the following command to check loaded rules:
# auditctl -l | awk ...
Verify the output matches:
-w /etc/sudoers -p wa -k scope
-w /etc/sudoers.d -p wa -k scope
```

**Sample 2 — 6.2.3.4 (multi-cmd `{ ... }` block + expected 5 lines, on-disk vs running 별 expected 다름)**:
```
# {
awk '/^ *-a *always,exit/ ... ' /etc/audit/rules.d/*.rules
awk '/^ *-w/ &&/\/etc\/localtime/ ... ' /etc/audit/rules.d/*.rules
}
Verify output of matches:
-a always,exit -F arch=b64 -S adjtimex,settimeofday -k time-change
-a always,exit -F arch=b32 -S adjtimex,settimeofday -k time-change
-a always,exit -F arch=b64 -S clock_settime -F a0=0x0 -k time-change
-a always,exit -F arch=b32 -S clock_settime -F a0=0x0 -k time-change
-w /etc/localtime -p wa -k time-change
... (Running configuration block — auditctl -l 변형)
Verify the output includes:
-a always,exit -F arch=b64 -S adjtimex,settimeofday -F key=time-change
-a always,exit -F arch=b32 -S settimeofday,adjtimex -F key=time-change
... (running config은 키 표기 `-F key=` vs on-disk `-k`, syscall 순서도 다름)
```

**Sample 3 — 6.2.3.7 (UID_MIN 변수 + expected 4 lines + 1000 하드코딩)**:
```
# {
UID_MIN=$(awk '/^\s*UID_MIN/{print $2}' /etc/login.defs)
[ -n "${UID_MIN}" ] && awk "/^ *-a *always,exit/ \
&&/ -F *arch=b(32|64)/ \
... &&/ -F *auid>=${UID_MIN}/ \
... " /etc/audit/rules.d/*.rules \
|| printf "ERROR: Variable 'UID_MIN' is unset.\n"
}
Verify the output includes:
-a always,exit -F arch=b64 -S creat,open,openat,truncate,ftruncate -F exit=-EACCES -F auid>=1000 -F auid!=unset -k access
... (4 lines)
```

**Sample 4 — 6.2.3.6 (bash hashbang script + expected "all output is OK")**:
```
#!/usr/bin/env bash
{
for PARTITION in $(findmnt ...); do
  for PRIVILEGED in $(find "${PARTITION}" -xdev -perm /6000 -type f); do
    grep -qr "${PRIVILEGED}" /etc/audit/rules.d && printf "OK: ..." || printf "Warning: ..."
  done
done
}
Verify that all output is OK.
```

**Sample 5 — 6.2.3.19 (multi-block: awk + UID_MIN + symlink check, 3개 독립 검증)**:
```
#!/usr/bin/env bash
{
awk '/^ *-a *always,exit/ ... &&(/init_module/||/finit_module/||...)' /etc/audit/rules.d/*.rules
UID_MIN=$(awk ...)
[ -n "${UID_MIN}" ] && awk "... -F path=\/usr\/bin\/kmod ..." /etc/audit/rules.d/*.rules \
  || printf "ERROR: ..."
}
Verify the output matches:
-a always,exit -F arch=b64 -S init_module,finit_module,delete_module,create_module,query_module -F auid>=1000 -F auid!=unset -k kernel_modules
-a always,exit -F path=/usr/bin/kmod -F perm=x -F auid>=1000 -F auid!=unset -k kernel_modules
... (Symlink audit block — 별도 bash hashbang)
```

### 3.2 21건 분류 행렬

| ID | Cmd 형태 | UID_MIN 사용 | Expected 라인수 | On-disk vs Running 차이 | 비고 |
|---|---|---|---|---|---|
| 6.2.3.1 | 단일 awk | X | 2 | 동일 | sudoers watch |
| 6.2.3.2 | 단일 awk | X | 2 | running은 `-F key=` 표기 | execve user_emulation |
| 6.2.3.3 | `{ ... }` + SUDO_LOG_FILE 변수 | X (SUDO_LOG) | 1 | 동일 | sudo log |
| 6.2.3.4 | `{ ... }` 2 awk | X | 5 | running 키 표기 + syscall 순서 | time-change |
| 6.2.3.5 | 2 단일 awk | X | 8 | running 키 표기 | system-locale |
| 6.2.3.6 | hashbang 2-loop | X | "all OK" | running 별도 hashbang | privileged commands |
| 6.2.3.7 | `{ ... }` + UID_MIN | O | 4 | running 키 표기 + UID 1000 명시 | unsuccessful access |
| 6.2.3.8 | 단일 awk | X | 8 | 동일 | identity files |
| 6.2.3.9 | `{ ... }` + UID_MIN | O | 6 | running 키 표기 + UID 1000 명시 | DAC perm_mod |
| 6.2.3.10 | `{ ... }` + UID_MIN | O | 2 | running 키 표기 | mounts |
| 6.2.3.11 | 단일 awk | X | 3 | 동일 | session utmp/wtmp/btmp |
| 6.2.3.12 | 단일 awk | X | 2 | 동일 | logins lastlog/faillock |
| 6.2.3.13 | `{ ... }` + UID_MIN | O | 2 | running 키 표기 + syscall 순서 | delete unlink/rename |
| 6.2.3.14 | 단일 awk | X | 2 | 동일 | MAC apparmor |
| 6.2.3.15 | `{ ... }` + UID_MIN | O | 1 | running `-S all` 추가 | chcon |
| 6.2.3.16 | `{ ... }` + UID_MIN | O | 1 | running `-S all` 추가 | setfacl |
| 6.2.3.17 | `{ ... }` + UID_MIN | O | 1 | running `-S all` 추가 | chacl |
| 6.2.3.18 | `{ ... }` + UID_MIN | O | 1 | running `-S all` 추가 | usermod |
| 6.2.3.19 | hashbang multi-block + UID_MIN | O | 2 + symlink | 동일 + 별도 hashbang | kernel_modules + kmod symlink |
| 6.2.3.20 | 단일 grep | X | 1 (`-e 2`) | n/a (immutable) | **이미 합성 완료** |
| 6.2.3.21 | `augenrules --check` | X | "No change" | n/a | **Manual 분류** |

### 3.3 공통 특성 (19건 신규 대상)

- **이중 검증**: 모든 항목이 on-disk(`/etc/audit/rules.d/*.rules`) + running(`auditctl -l | ...`) 두 번 검증. 한쪽만 PASS면 reboot 대기 상태(false PASS 위험).
- **Verify the output matches/includes**: 18/19 항목이 정확히 이 phrase로 expected를 도입(6.2.3.6 "all output is OK"만 예외).
- **expected 라인 수**: 1~8 라인. 모두 audit text 본문에 plain text로 포함 — 추출 가능.
- **on-disk vs running expected 표기 차이**: `-k key` ↔ `-F key=key`, syscall 순서, `-S all` 접두 — **단순 string match 불가**. 정규화 또는 syscall set 비교 필요.
- **UID_MIN 1000 가정**: 11/19 항목이 expected에 `auid>=1000` 하드코딩 — 실 시스템 UID_MIN ≠ 1000(예 1001)이면 false FAIL. audit text는 UID_MIN 변수를 코드에서 사용하지만 expected는 1000.
- **multi-cmd `{ ... }` block**: 11/19 항목. 단일 줄로 평탄화하면 `{` `}` 안 multi-line awk pattern + `\` continuation 보존 필요(absorbCISContinuation 한계).
- **6.2.3.6 / 6.2.3.19**: 특수 케이스. 6.2.3.6은 expected가 "all OK"(loop 출력 모두 OK 시작), 6.2.3.19는 audit script에 symlink check가 추가로 포함.

## 4. 옵션 비교 (≥3)

### 옵션 A — 기존 base64 hashbang body wrap 확장

**전략**: audit text에서 첫 `#` 줄부터 expected 직전까지를 hashbang body로 추출 → 기존 `synthesizeBashBodyExpectNonEmpty`에 새 분기 추가("Verify the output matches"+expected 라인 수 ≥ 1 → 출력 라인 수가 expected와 같거나 superset이면 PASS).

**Pros**:
- 기존 base64 wrap 인프라 재사용 → 코드 추가 최소 (~80 lines)
- multi-cmd `{ ... }` block 그대로 base64로 보존 — escape 부담 0
- audit text 직접 실행이라 CIS 가이드와 동작 1:1

**Cons**:
- expected 라인 수 비교만으론 syscall 순서 차이/정확 매칭 불가 — 운영자가 "이 라인이 빠짐"을 알기 어려움
- 6.2.3.6 ("all OK") 같은 변형은 별 분기 필요
- on-disk vs running 둘 중 한쪽만 PASS여도 PASS 처리 위험 (reboot 대기 false PASS)
- audit text 그대로 실행 → CIS PDF의 PCRE escape 오류가 그대로 전이(이미 5.1.x에서 본 issue)

**회귀 위험**: 중. base64 wrap 분기 확장이 다른 audit text(7.2.x grep verify 등)에 false positive 가능 — verify-output-matches phrase + expected 라인 수 ≥ 1 + 명시적 audit rule fragment(`-w /` 또는 `-a always,exit`)로 narrow.

**변환 정확도**: 라인 수만 검증 시 ~70%, syscall set 비교 추가 시 ~85%.

### 옵션 B — 신규 `synthesizeAuditctlMatch` 합성 함수 (권장)

**전략**: 6.2.3.x 전용 인식기 `isAuditctlExpectedMatch(audit)` + 합성기 `synthesizeAuditctlMatch(audit) → bash`. audit text에서 expected 라인 셋(`-w /...` 또는 `-a always,exit -F ...`)을 정규식으로 추출 → 정규화(syscall set 정렬, `-k X` ↔ `-F key=X` 통일, `auid>=` 임계값 변수화) → 검증 스크립트는 다음 두 명령을 모두 실행:

```bash
# 합성된 검증 본문 (개념 — Stage 2에서 fixture 검증)
bash -c '
need_disk=( "rule_normalized_1" "rule_normalized_2" ... )
need_run=( "rule_normalized_1" ... )
disk_out="$(cat /etc/audit/rules.d/*.rules 2>/dev/null | normalize_fn)"
run_out="$(auditctl -l 2>/dev/null | normalize_fn)"
missing=0
for r in "${need_disk[@]}"; do
  printf "%s\n" "$disk_out" | grep -qxF "$r" || { echo "miss-disk: $r"; missing=$((missing+1)); }
done
for r in "${need_run[@]}"; do
  printf "%s\n" "$run_out" | grep -qxF "$r" || { echo "miss-run: $r"; missing=$((missing+1)); }
done
if [ "$missing" -eq 0 ]; then printf "%s\n" "** PASS **"; else printf "%s\n" "** FAIL **"; fi
'
```

여기서 `normalize_fn`은:
1. `-k key` ↔ `-F key=key` 통일 → `-F key=key`
2. `-S a,b,c` 안 syscall list 정렬 → `-S a,b,c` (알파벳)
3. `auid>=N` → `auid>=UID_MIN` (런타임 UID_MIN 치환 후 비교)
4. 양 끝 공백 정규화

**Pros**:
- 결정론적·정확 매칭 (false positive ≈ 0) — syscall 순서 무관
- on-disk + running 모두 검증 → reboot 대기 false PASS 차단
- expected 라인 셋이 yaml에 인라인 — 운영자가 "어느 rule이 빠졌는지" 한눈에
- 19건 모두 동일 합성기로 cover (6.2.3.6 / 6.2.3.19 별도 분기 시 +α)
- UID_MIN 런타임 치환으로 ≠ 1000 환경에서 false FAIL 회피
- 신규 `auditctl_*` 분기는 별 함수 — 기존 9 패턴 회귀 0

**Cons**:
- 신규 코드 ~250 lines (인식기 + 정규화 + 합성기 + 테스트)
- 6.2.3.6 (privileged commands "all OK")은 expected 라인 추출 불가 → 별 분기 또는 옵션 A로 fallback
- 6.2.3.19 (kmod symlink) 추가 검증은 별도 — symlink check만 별 합성 함수
- audit text의 expected 라인 추출 regex가 21건의 표기 차이(`-w /etc/foo` vs `-w /etc/foo/`)에 견고해야 — 정밀 fixture 필요

**회귀 위험**: 저. 신규 인식기는 6.2.3.x ID 또는 `Verify the output matches` + `auditctl -l` co-occurrence로 narrow → 다른 항목 false trigger 0. 신규 함수는 cis_synth_integration_test.go에 +21 case 추가만 필요.

**변환 정확도**: ~95% (6.2.3.6 + 6.2.3.19 symlink만 partial).

### 옵션 C — per-rule 매뉴얼 fixture 정정 (자동화 회피)

**전략**: 21건은 합성하지 않고 운영자가 yaml별 `auditCommand` 직접 작성 — converter는 degraded 유지.

**Pros**:
- converter 코드 변경 0
- 회귀 위험 0
- 21 yaml 각각 컨텍스트 맞춤 — 가장 정확

**Cons**:
- 변환률 +0%p (77.6%에 정체)
- 21 yaml 수동 유지 — pack 재배포마다 sync 부담
- baseline JSON 갱신 시(예 CIS Ubuntu 24.04 v2.0.0 release) 21건 다시 수동 정정
- "결정론적 자동화" 원칙(설계서 §1·§9)과 어긋남

**회귀 위험**: 0 (코드 변경 없음).

**변환 정확도**: 100% (수동 작성). 단 변환률 통계엔 잡히지 않음.

### 옵션 D — audit text 직접 실행 + diff (옵션 A 변형)

**전략**: audit text의 첫 `#` block을 base64로 wrap 실행 + 출력을 expected와 line-sorted diff(공백 정규화 후) → diff empty면 PASS.

**Pros**: 옵션 A보다 정확도 ↑ (라인 수 + 내용 모두 비교).

**Cons**: on-disk vs running 표기 차이(`-k` vs `-F key=`)를 정규화해야 — 결국 옵션 B의 normalize_fn 필요. 옵션 B의 inner work를 audit text 실행 후로 미루는 것뿐, 코드 양은 비슷한데 audit text PCRE escape 위험은 그대로.

**회귀 위험**: 옵션 A와 유사.

## 5. 권장 — 옵션 B (`synthesizeAuditctlMatch`)

근거:
- **결정론적 정확성** — syscall 순서·키 표기 무관, false positive 사실상 0 (원칙 §1·§6)
- **운영자 가독성** — expected 라인 셋이 합성된 yaml에 인라인 출력되어 진단 용이
- **on-disk + running 이중 검증** — reboot 대기 상태 정확 식별 (audit 가이드 의도 보존)
- **회귀 위험 최소** — 신규 함수는 별 분기, 기존 9 패턴 비변경
- **재사용성** — `normalize_fn` bash snippet은 향후 6.2.4.x audit 로그 검증에도 응용 가능

옵션 A는 6.2.3.6 fallback으로 보조(2건 정도) — Stage 2 마지막에 결정. 옵션 C는 6.2.3.6 + 6.2.3.19 symlink 부분만 적용(2건만 manual). 옵션 D는 채택 안 함.

## 6. 변경 사항

### 6.1 `cmd/pack-tools/converter/cis.go` — 신규 함수

```go
// 새 분기 위치: convertCISItem line 192~207 사이 (Pattern 7 fallback 직전)
//
// 우선순위: Pattern 6.5 (auditctl match) — Pattern 7 (hashbang body)보다 우선해
// audit text에 expected rule 라인이 있으면 정확 매칭 분기로 흡수.
if synthesized, ok := synthesizeAuditctlMatch(it.Audit); ok {
    check.AuditCommand = wrapBash(synthesized)
    check.EvaluationRule = cisAutoEvalRuleJSON
    return check, ""
}

// 신규 함수 시그니처 (cis.go 또는 신규 cis_auditctl.go)
func synthesizeAuditctlMatch(audit string) (string, bool)
func extractAuditctlExpectedRules(audit string) (onDisk, running []string, ok bool)
func normalizeAuditctlRule(raw string) string  // syscall sort + key 표기 통일
func isAuditctlAuditText(audit string) bool    // 인식 휴리스틱

// regex 신규 (cis.go 변수 블록)
var (
    // Verify <the> output (matches|includes): 다음 줄부터 빈 줄/자연어 경계까지 expected 라인 추출.
    regexpVerifyOutputBlock = regexp.MustCompile(
        `(?im)^\s*Verify\s+(?:the\s+)?output\s+(?:matches|includes)\s*:?\s*\n((?:^\s*[-]\s*[awS].*(?:\n|$))+)`)
    // auditctl -l co-occurrence (인식 휴리스틱 보강).
    regexpAuditctlList = regexp.MustCompile(`(?i)\bauditctl\s+-l\b`)
    // audit rule 라인 1차 식별 (-w /path -p wa -k key 또는 -a always,exit -F ... -k key).
    regexpAuditRuleLine = regexp.MustCompile(`^\s*-(?:w\s+\S+|a\s+always,exit)`)
)
```

`isAuditctlAuditText` 인식 조건 (and):
- `regexpVerifyOutputBlock` 매칭(expected 라인 ≥ 1)
- `regexpAuditctlList` 매칭(running config 검증 명시)
- 적어도 1 expected 라인이 `regexpAuditRuleLine` 통과

19/21 매칭 (6.2.3.20 이미 합성, 6.2.3.21 Manual 제외). 6.2.3.6은 expected 라인 패턴 미일치 → degraded 유지 또는 옵션 A fallback.

### 6.2 `normalize_fn` bash snippet (합성 출력에 인라인)

bash 안 awk 한 줄로 처리 — 외부 도구 무의존:

```bash
normalize_rule() {
  # input: stdin (audit rule 한 줄)
  # output: 정규화된 라인 (-k key → -F key=key, -S a,b,c → 정렬, 양끝 공백 trim)
  awk '{
    line=$0
    # -k key → -F key=key (단어 경계로 한 번만)
    sub(/ -k +([^ ]+)/, " -F key=\\1", line)
    # -S a,b,c 안 syscall list 정렬: 추출 → split → asort → join
    if (match(line, / -S +[A-Za-z0-9_,]+/)) {
      raw=substr(line, RSTART+4, RLENGTH-4)
      n=split(raw, arr, ",")
      asort(arr)
      sorted=arr[1]; for (i=2;i<=n;i++) sorted=sorted","arr[i]
      line=substr(line,1,RSTART)" -S "sorted substr(line,RSTART+RLENGTH)
    }
    gsub(/[[:space:]]+/, " ", line); sub(/^ /, "", line); sub(/ $/, "", line)
    print line
  }'
}
```

UID_MIN 치환은 expected 라인을 합성 시점에 `auid>=__UID_MIN__` placeholder로 변환하고, bash 실행 시 `__UID_MIN__` → 실 UID_MIN 값으로 sed 치환. 이렇게 하면 환경 UID_MIN ≠ 1000도 cover.

### 6.3 selftest fixture 자동 생성

`evaluationRule = cisAutoEvalRuleJSON`(= `{"op":"contains","value":"** PASS **"}`)을 그대로 사용 → `GenerateSelfTestSkeletons`가 변경 없이 19개 fixture(PASS + FAIL) 자동 생성. 별 변경 없음.

### 6.4 `packs/cis-ubuntu-2404/` 영향

- `checks/6.2.3.{1..19}.yaml`(20·21 제외) 19개 `auditCommand` + `evaluationRule` 갱신 — pack-tools convert 재실행
- `manifest.json` hash 갱신 (signed pack — 자동)
- `selftests/6.2.3.{1..19}.yaml` 19개 신규 추가
- `tar.gz` archive 재빌드

### 6.5 pack-tools 명령 사용법

변화 없음. `pack-tools convert --in baseline.json --out packs/cis-ubuntu-2404` 그대로. 변환률 출력에 19 항목 자동 흡수.

## 7. TDD Stage 분해

| Stage | 산출 | 추정 | 검증 방법 |
|---|---|---|---|
| 1 | regex 3종 + `isAuditctlAuditText` + `extractAuditctlExpectedRules` 함수 | 0.25일 | cis_test.go +12 unit (positive 6.2.3.{1,4,5,7,8,11,15,19} + negative 6.2.3.{20,21} + 비-audit text 2건) |
| 2 | `normalizeAuditctlRule` + `synthesizeAuditctlMatch` 합성 함수 | 0.25일 | cis_test.go +6 unit (정규화 round-trip + 합성 출력 string snapshot 6건) |
| 3 | `convertCISItem` 결선 + integration test | 0.25일 | cis_synth_integration_test.go +19 case (6.2.3.x 19건 PASS·FAIL 합성 bash 실행 — Linux runner only build tag) |
| 4 | selftest skeleton 자동 생성 검증 | 0.1일 | selftest_test.go +1 (변환 후 pack에 19 skeleton 추가됨 + custom·degraded 카운트 감소) |
| 5 | pack-tools convert 재실행 + manifest hash 갱신 + tar.gz | 0.15일 | `make ci` 통과 + 변환률 보고 (77.6% → 약 84%) + `go test ./packs/...` 회귀 0 |
| 6 | SESSION_HANDOFF 갱신 + commit chain (Stage별 1 commit, 마지막 design doc 닫음) | 0.1일 | git log 6 commit + handoff "직전 한 줄" 갱신 |

**합계 추정**: 1.1일 (1일+ — design doc 우선 원칙 적용 대상).

## 8. 회귀 위험

- **다른 audit text false trigger** — `isAuditctlAuditText`가 7.2.x("Verify output matches" + grep)에 잘못 매칭할 위험. `regexpAuditctlList` AND 조건으로 narrow → 7.2.x는 auditctl -l 미사용이라 안전. Stage 1 negative test에 7.2.x 2건 포함.
- **selftest harness 회귀** — `cis_synth_integration_test.go`는 Linux build tag(`//go:build linux`) — Windows에서 skip. CI Linux runner에서 19 case 추가, harness 변경 0.
- **pack manifest hash 변경 (signed pack)** — 19 yaml + 19 selftest 추가 = 약 38 file change → manifest hash 자동 재계산 + R30-3 sign-pack 워크플로우 재실행 필요. 운영자 영향 0.
- **CI auto-convert** — `make ci`에 `pack-tools convert` 자동 호출 단계 있음 → 변환 결과 diff가 commit되어야 — Stage 5에 명시.
- **6.2.3.6 (privileged commands)** — expected pattern("all OK") 미매칭으로 degraded 유지 → 변환률 명목 +6.4%p (84.0%, 19/313). 운영자 manual fixture 1건 추가 시 +6.7%p.
- **6.2.3.19 symlink check 부분 cover** — kmod 본 검증은 합성, symlink readlink check는 별 hashbang으로 audit text에 분리되어 있어 합성 시 누락. Stage 2에 symlink block 인식 추가 또는 partial PASS 명시.
- **UID_MIN 치환 sed 위험** — 환경에 UID_MIN 미설정 시 합성 bash가 `__UID_MIN__` 그대로 치환 실패 → grep 매칭 0 → FAIL. 변환된 bash에 UID_MIN 빈 값 가드 (`UID_MIN=${UID_MIN:-1000}`).

## 9. 결정 항목 (D-N-x · 권장 default)

- **D-N-1 — auditctl 비교 방식**: line-sorted diff vs grep 패턴 매칭. **권장: grep -qxF + 정규화** (옵션 B의 normalize_fn). 이유: line-sorted diff는 missing rule을 명시적으로 식별 어렵고, grep -qxF는 라인 단위 매칭 + missing 카운트 출력으로 진단 가능.

- **D-N-2 — multi-cmd `{ ... }` block 흡수**: 단일 합성 wrap vs per-cmd 분리. **권장: 단일 합성 wrap** (옵션 B 인라인). 이유: `{ ... }` block은 audit 가이드의 논리적 unit이고 expected는 합산 출력에 대해 정의됨 → 분리 시 expected 라인 셋 매핑이 모호.

- **D-N-3 — expected output 인용 방식**: heredoc vs base64 vs bash array. **권장: bash array** (`need_disk=( "rule1" "rule2" ... )`). 이유: heredoc은 multi-line처리 + escape 부담, base64는 디버그 시 yaml에서 안 보임, array는 가독성 + 라인 매핑 1:1.

- **D-N-4 — UID_MIN 치환 시점**: 변환 시점(고정값) vs 런타임 (`${UID_MIN:-1000}`). **권장: 런타임 치환** (`UID_MIN=${UID_MIN:-1000}` + expected 라인은 `__UID_MIN__` placeholder를 합성 후 sed). 이유: UID_MIN ≠ 1000 환경에서 false FAIL 회피 — Phase 1 audit 환경 다양성 대비.

- **D-N-5 — on-disk vs running 표기 차이 정규화 범위**: `-k` ↔ `-F key=` 만 vs syscall 순서까지. **권장: 둘 다** (normalize_fn에서 syscall list 정렬 + key 표기 통일). 이유: 6.2.3.{4,5,7,9,13}이 syscall 순서 차이 보유 — 정렬 안 하면 false FAIL.

- **D-N-6 — 6.2.3.6 (privileged commands "all OK") 처리**: 옵션 A fallback vs 매뉴얼 fixture vs 별 분기. **권장: 매뉴얼 fixture (degraded 유지)**. 이유: audit text의 loop가 시스템마다 다른 partition·privileged file을 traverse — 결정론적 정규화 어려움. 19/21 자동 + 1 manual + 1 Manual + 1 already done = 21건 cover 완결.

- **D-N-7 — 6.2.3.19 symlink check 별 분기**: 부분 cover vs 전체 cover. **권장: 부분 cover** (kernel_modules 본 검증만 합성, symlink readlink check는 audit text의 별 hashbang block — 별 cis_test.go case 1건 추가하지 않고 합성기 단순 유지). 이유: symlink check는 audit text 마지막 별 script로 명시 — 합성기 복잡도 ↑ vs 검증 가치 ↓ 트레이드오프, kmod 본 검증만으로 spec intent 충족.

- **D-N-8 — 신규 함수 위치**: `cis.go` 안 vs 새 파일 `cis_auditctl.go`. **권장: 신규 파일 `cis_auditctl.go`** (~250 lines). 이유: cis.go 이미 831줄(권장 400줄, 최대 800줄 한계 초과 임박). 새 파일로 분리해 도메인 독립성 + 향후 6.2.4.x 확장 시 동일 파일에 추가.

- **D-N-9 — Stage 3 integration test 실행 환경**: Linux only build tag vs cross-platform mock. **권장: Linux only build tag** (`//go:build linux`). 이유: auditctl·`/etc/audit/rules.d`는 Linux 전용 — Windows mock은 가치 ↓ vs 복잡도 ↑. 기존 `cis_synth_integration_test.go`도 동일 패턴.

## 10. 참조

- 직전 design doc: `docs/design/notes/scans-severity-aggregate-design.md` (스타일 참고)
- 직전 변환 commit chain (9건):
  - `dd0a801` 18.9% → 58.0% (Nothing returned + is installed)
  - `b33c702` 58.0% → 64.4% (stat + sshd grep)
  - `ed36d93` 64.4% → 65.1% (sshd numeric)
  - `d0bea83` multi-line cmd 흡수
  - `9c3c79a` 65.4% → 67.6% (is mounted findmnt)
  - `f0c7891` 67.6% → 70.2% (bash hashbang body wrap base64)
  - `ba3c63a` 70.2% → 72.1% (grep verify output)
  - `70156bf` 72.1% → 74.4% (PAM grep + similar)
  - `7d92a15` 74.4% → 75.0% (grep alternation + awk exact)
  - `afe5b8b` 75.0% → 75.3% (sshd numeric range)
  - `9e003fa` 75.3% → 77.6% (no results + if any line found)
- 변환기 코드: `cmd/pack-tools/converter/cis.go` (831 lines, dispatch line 150~211)
- selftest 자동 생성: `cmd/pack-tools/converter/selftest.go::GenerateSelfTestSkeletons`
- nrobotcheck baseline: `D:\robot\dev\nrobotcheck\resources\baselines\cis_ubuntu_2404_benchmark.json` line 9131~9782 (6.2.3.x 21건)
- pack 출력: `packs/cis-ubuntu-2404/checks/6.2.3.{1..21}.yaml`
- degraded 가이드: `docs/operations/cis-ubuntu-2404-degraded.md` (NoMarker 56건 분류)
- 설계 원칙: `docs/design/01-principles.md` §1(결정론) · §6(결정론적 fallback) · §9(불변성)
