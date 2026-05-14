# CIS Ubuntu 24.04 — E-3 epic (stat 옵트 + apparmor + dpkg-query, 7건) — Design

> **상태**: Phase 0 design (다음 세션 진입점). 본 문서는 코드 0줄 / pack 변경 0 — 그룹별 합성 전략 옵션·권장 default·Stage 분해까지만 마감. 직전 design doc(`cis-nomarker-31-analysis.md` §4 E-3) 후속.

## 1. 배경

직전 epic E-1(gsettings/sshd OR/auditd grep, 6건) + E-2(nftables/iptables, 4건) 완료 후 CIS Ubuntu 24.04 자동 변환률은 **86.5%** (270/312)입니다. 잔여 NoMarker 21건 중 본 epic은 **G12 + G8 + G9 묶음 7건**을 cover합니다.

- **잠재 변환률**: 86.5% → **88.7%** (+2.2%p, 7건 추가)
- **추정 시간**: 1.0일 — design doc 우선 정책 임계 도달(`feedback_design_doc_first.md`).
- **그룹 통합 근거**: 세 그룹 모두 **출력 라인 카운트 또는 단순 token 매칭** 패턴이지만, 표면 형태가 달라 합성 함수 3개를 별 파일로 분리(D-N-8 패턴 일관: `cis_stat_opt.go` / `cis_apparmor.go` / `cis_dpkg.go`).
- **회귀 위험**: 저(G12·G9) ~ 중(G8 — apparmor 출력 텍스트가 locale·버전 의존).

## 2. 7건 ID 목록 + audit sample

baseline JSON 발췌(`D:\robot\dev\nrobotcheck\resources\baselines\cis_ubuntu_2404_benchmark.json`, utf-8). 7건 모두 `assessment_status=Automated` 확인, 현재 pack에서 7건 모두 `auditCommand: "true"` degraded 상태.

### 2.1 G12 — file stat 옵트 (2건)

**IDs**: `1.6.4`, `7.1.10`. `[ -e file ] && stat -Lc '...'` cmd + "OR Nothing is returned" 분기. 파일 존재 시 stat 출력 매칭, 미존재 시 PASS.

**Sample (1.6.4)**:
```
Run the following command and verify that if /etc/motd exists, Access is 644 or more
restrictive, Uid and Gid are both 0/root:
# [ -e /etc/motd ] && stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: { %g/
%G)' /etc/motd
Access: (0644/-rw-r--r--) Uid: ( 0/ root) Gid: ( 0/ root)
-- OR --
Nothing is returned
```

**Sample (7.1.10 발췌)**: 2개 path(`/etc/security/opasswd` + `.old`) 각각 `[ -e ... ] && stat ...` cmd + 각 cmd 직후 expected stat 출력 + `-OR-` + `Nothing is returned` 반복.

### 2.2 G8 — apparmor_status grep + count (2건)

**IDs**: `1.3.1.3`, `1.3.1.4`. `apparmor_status | grep profiles` 출력 4줄 (`N profiles are loaded.` / `M profiles are in enforce mode.` 등) + 별도 `apparmor_status | grep processes` cmd 4줄. 카운트 추출 + 비교 (loaded ≥ 1, complain == 0 등).

**Sample (1.3.1.4)**:
```
Run the following commands and verify that profiles are loaded and are not in complain
mode:
# apparmor_status | grep profiles
Review output and ensure that profiles are loaded, and in enforce mode:
34 profiles are loaded.
34 profiles are in enforce mode.
0 profiles are in complain mode.
2 processes have profiles defined.
Run the following command and verify that no processes are unconfined:
apparmor_status | grep processes
Review the output and ensure no processes are unconfined:
2 processes have profiles defined.
2 processes are in enforce mode.
0 processes are in complain mode.
0 processes are unconfined but have a profile defined.
```

차이: 1.3.1.3은 "in either enforce or complain mode" → loaded ≥ 1 만 검증, 1.3.1.4는 "in enforce mode" + "not in complain mode" → loaded ≥ 1 AND complain == 0. 두 건 모두 `unconfined` count == 0 (processes 절).

### 2.3 G9 — dpkg-query 설치 상태 (3건)

**IDs**: `1.7.1`, `2.1.20`, `5.3.1.1`. 표면 형태 3종.

**Sample (1.7.1, not-installed 검증)**:
```
Run the following command and verify gdm3 is not installed:
# dpkg-query -W -f='${binary:Package}\t${Status}\t${db:Status-Status}\n' gdm3
gdm3 unknown ok not-installed not-installed
```

**Sample (2.1.20, "Nothing should be returned")**:
```
- IF - a Graphical Desktop Manager or X-Windows server is not required and approved
by local site policy:
Run the following command to Verify X Windows Server is not installed.
dpkg-query -s xserver-common &>/dev/null && echo "xserver-common is
installed"
Nothing should be returned
```

**Sample (5.3.1.1, install ok installed 검증)**:
```
Run the following command to verify the version of libpam-runtime on the system:
# dpkg-query -s libpam-runtime | grep -P -- '^(Status|Version)\b'
The output should be similar to:
Status: install ok installed
Version: 1.5.3-5
```

**중요 관찰** — 2.1.20은 `#` prefix 없는 cmd + `&>/dev/null && echo "..."` + "Nothing should be returned" 패턴. 표면적으로 기존 `extractCISLastShellLine` + expect-empty 분기와 호환되어 보이지만, 현재 degraded 분류된 사실로 미루어 다음 중 하나가 원인:
- `dpkg-query` cmd가 `looksLikeShellCommand` 또는 `extractCISLastShellLine` 휴리스틱에 미일치 (다음 줄 `installed"` continuation 후 quoted context 분리)
- expect-empty 인식 phrase가 "Nothing should be returned"의 위치 또는 형식과 충돌
- Stage 1에서 실제 원인 진단 후 본 epic G9 분기 또는 기존 expect-empty 보강 둘 중 선택.

## 3. 합성 전략 옵션 (그룹별)

### 3.1 G12 — file stat 옵트

**옵션 A** — 기존 `synthesizeExpectStatPerm`에 옵트 분기 추가:
- 인식 단계: audit text에 `[ -e <path> ] &&` prefix + "Nothing is returned" co-occurrence 시 옵트 모드 활성.
- 합성: `[ -e <path> ] && (out=$(stat ...) ...)` 또는 path 미존재 시 직접 `printf '** PASS **\n'` 분기.
- 다중 path(7.1.10 — 2 path)는 cmd 반복.
- **Pros**: 기존 함수 확장만 — 코드 lines ↓. selftest fixture 자동 생성 호환.
- **Cons**: `cis.go` 합성 함수 line ↑(현재 800줄 한계 근접 — 분리 필요 시 옵션 B 전환).

**옵션 B (권장)** — 신규 합성기 `cis_stat_opt.go`:
- D-N-8 패턴 일관(`cis_auditctl.go`/`cis_gsettings.go` 등). dispatch에 `isStatOptAuditText(it.Audit)` + `synthesizeStatOpt(it.Audit)` 신규.
- 다중 path 자연스러운 슬라이스 처리 (`statOptCheck struct{ path, expectedMode string }`).
- **Pros**: cis.go line 압력 0. 회귀 격리(기존 stat permission 분기는 무수정).
- **Cons**: 코드 ~80 lines 추가. dispatch 우선순위 결정 필요(stat permission 분기보다 위 — `[ -e file ] && stat ...` 형태가 specific).

**권장 default**: **옵션 B**. cis.go 분리 일관성 + 회귀 격리 우선.

**합성 출력 sketch (1.6.4 PASS 케이스)**:
```bash
missing=0
if [ -e /etc/motd ]; then
  out_0=$(stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: ( %g/ %G)' /etc/motd 2>/dev/null)
  mode=$(printf '%s\n' "$out_0" | sed -n 's|.*Access: (\([0-7]\{3,4\}\)/.*|\1|p' | head -1)
  { [ -n "$mode" ] && [ "$((8#$mode))" -le "$((8#0644))" ] && \
    printf '%s\n' "$out_0" | grep -qE 'Uid: \(\s*0/'; } || \
    { printf 'mismatch: /etc/motd\n'; missing=$((missing+1)); }
fi
if [ "$missing" -eq 0 ]; then printf '** PASS **\n'; else printf '** FAIL **\n'; fi
```

**정확도**: ~95%. stat 출력 형식 안정 + path 미존재 자동 PASS는 audit text "OR Nothing is returned" 의미와 일치.

### 3.2 G8 — apparmor_status grep + count

**옵션 A** — 카운트 추출 + 정수 비교 (권장):
- `apparmor_status | grep profiles` 출력 4줄 각각 `awk '{print $1}'` (또는 `grep -oP '^\d+'`)로 정수 추출.
- 1.3.1.3: `loaded ≥ 1` 단일 가드.
- 1.3.1.4: `loaded ≥ 1` AND `complain == 0`.
- 두 건 공통: processes 절은 `unconfined == 0` 가드(audit text 마지막 줄 명시 의도).
- audit text의 sample 숫자(34/35/0/2 등)는 **example only** — baseline threshold 추출 안 함, 의미 기반 가드 hard-code.

**옵션 B** — 단순 substring 매칭(loaded > 0만 검증):
- `apparmor_status | grep profiles | grep -q 'profiles are loaded'` 단순 phrase 매칭 후 첫 토큰 != 0 검사.
- **Pros**: 코드 ↓.
- **Cons**: 1.3.1.4의 complain == 0 강제 미반영 → false PASS 위험. 1.3.1.3·1.3.1.4 구분 불가.

**옵션 C** — audit text expected line의 N을 baseline threshold로 사용:
- **Pros**: 결정론적.
- **Cons**: 현재 시스템과 audit text sample 시스템(34 profiles 등)은 다름 → false FAIL 만성. 의미 무관 비교.

**권장 default**: **옵션 A**. 의미 기반 가드(loaded ≥ 1 / complain == 0 / unconfined == 0)가 audit text 의도 정확 반영.

**ID 분기 전략**:
- audit text의 phrase로 `inEnforceOrComplain` vs `inEnforceOnly` 자동 판정.
  - "in either enforce or complain mode" → 1.3.1.3 모드 (loaded ≥ 1 만)
  - "are not in complain mode" 또는 "in enforce mode" (without "or complain") → 1.3.1.4 모드 (complain == 0 추가)
- ID 직접 매칭은 회피(audit text 패턴이 권위, ID는 baseline 변경 시 깨짐).

**합성 출력 sketch (1.3.1.4)**:
```bash
out_p=$(apparmor_status 2>/dev/null | grep profiles)
out_pr=$(apparmor_status 2>/dev/null | grep processes)
loaded=$(printf '%s\n' "$out_p" | grep -oP '^\d+(?=\s+profiles are loaded)' | head -1)
complain=$(printf '%s\n' "$out_p" | grep -oP '^\d+(?=\s+profiles are in complain mode)' | head -1)
unconfined=$(printf '%s\n' "$out_pr" | grep -oP '^\d+(?=\s+processes are unconfined but)' | head -1)
missing=0
[ -n "$loaded" ] && [ "$loaded" -ge 1 ] || { printf 'profiles loaded < 1 or unparsed\n'; missing=$((missing+1)); }
[ -n "$complain" ] && [ "$complain" -eq 0 ] || { printf 'profiles in complain != 0\n'; missing=$((missing+1)); }
[ -n "$unconfined" ] && [ "$unconfined" -eq 0 ] || { printf 'processes unconfined != 0\n'; missing=$((missing+1)); }
if [ "$missing" -eq 0 ]; then printf '** PASS **\n'; else printf '** FAIL **\n'; fi
```

**회귀 위험**: 중. apparmor_status 출력 phrase는 `apparmor` 패키지 버전·locale 의존. POSIX C locale 가정(rosshield agent 환경) — 실 환경 verify는 운영자 책임. design doc 정확도 ~85%.

**정확도**: ~85% (audit text 의도 정확 반영, 단 apparmor 버전 차이로 phrase 변형 시 false FAIL).

### 3.3 G9 — dpkg-query 설치 상태

세 ID는 표면 형태가 모두 다름 — 단일 합성기로 cover하되 audit text 패턴 분기 3종 분리 권장.

**옵션 A** — `not-installed` substring 매칭 (1.7.1):
- cmd 실행 → 출력에 `not-installed` substring 포함이면 PASS.
- audit text expected line `gdm3 unknown ok not-installed not-installed` 그대로 substring 검사.

**옵션 B** — `&>/dev/null && echo` cmd가 빈 출력 (2.1.20):
- 실제로는 기존 expect-empty 분기와 동일 의미. cmd 추출 시 quoted continuation(`"xserver-common is\ninstalled"`) 처리 필요.
- **Stage 1 진단 결과** 에 따라 분기 선택:
  - 원인이 `extractCISLastShellLine`의 quoted continuation 미처리 → 기존 `absorbCISContinuation` 보강 (G9 epic 외 별 fix). 본 epic은 1.7.1·5.3.1.1만 cover.
  - 원인이 `looksLikeShellCommand` 등 phrase 충돌 → 본 epic G9 분기에서 명시적 cover.

**옵션 C** — `Status: install ok installed` substring 매칭 (5.3.1.1):
- cmd 실행 → 출력에 `install ok installed` substring 포함이면 PASS. Version 라인은 검증 안 함(audit text "should be similar to" — 정확 매칭 비대상).

**통합 합성기**: `cis_dpkg.go`에 `dpkgQueryCheck struct{ cmd string; mode dpkgMode; expected string }` (mode: `notInstalled` | `installOK` | `emptyOutput`). audit text 패턴 매칭으로 mode 자동 판정.

**권장 default**: **옵션 A + C 우선 cover (1.7.1, 5.3.1.1만)**, **2.1.20은 Stage 1 진단 후 결정** — 진단 결과가 "기존 expect-empty 분기 fix 가능"이면 본 epic 외 별 commit, "신규 G9 분기 필요"이면 옵션 B 통합.

**합성 출력 sketch (1.7.1)**:
```bash
out=$(dpkg-query -W -f='${binary:Package}\t${Status}\t${db:Status-Status}\n' gdm3 2>/dev/null)
if printf '%s\n' "$out" | grep -qF 'not-installed'; then
  printf '** PASS **\n'
else
  printf '** FAIL **\n'
fi
```

**회귀 위험**: 저. dpkg-query 출력은 안정적, substring 매칭 결정론적.

**정확도**: ~95% (1.7.1·5.3.1.1), 2.1.20은 진단 결과 의존.

## 4. 변경 사항 outline

### 4.1 신규 파일 (D-N-8 패턴)

```
cmd/pack-tools/converter/cis_stat_opt.go      (G12, ~80 lines)
cmd/pack-tools/converter/cis_stat_opt_test.go (unit tests)
cmd/pack-tools/converter/cis_apparmor.go      (G8, ~100 lines)
cmd/pack-tools/converter/cis_apparmor_test.go
cmd/pack-tools/converter/cis_dpkg.go          (G9, ~90 lines)
cmd/pack-tools/converter/cis_dpkg_test.go
```

각 파일 헤더 주석은 `cis_auditctl.go`·`cis_gsettings.go` 패턴 답습 — epic 출처(`E-3 epic G12/G8/G9`) + 잠재 변환률 + 직전 design doc(본 문서) 참조.

### 4.2 cis.go dispatch 추가 (3 분기)

`convertCISItem`의 합성 dispatch에 다음 3분기 추가. **순서가 중요**: G12 stat 옵트는 기존 stat permission 분기보다 위(specific 우선). G8·G9는 기존 분기와 cmd shape 충돌 없음 → 위치 자유롭지만 일관성 위해 stat 옵트 직후.

```go
// Pattern 14 (E-3 G12): file stat 옵트 — `[ -e file ] && stat ...` + "Nothing is returned".
// 1.6.4·7.1.10. 기존 stat permission 분기(synthesizeCISShellAssertion 내부)보다 specific.
if isStatOptAuditText(it.Audit) {
    if synthesized, ok := synthesizeStatOpt(it.Audit); ok {
        check.AuditCommand = wrapBash(synthesized)
        check.EvaluationRule = cisAutoEvalRuleJSON
        return check, ""
    }
}

// Pattern 15 (E-3 G8): apparmor_status | grep profiles + count 비교. 1.3.1.3·1.3.1.4.
if isApparmorStatusAuditText(it.Audit) {
    if synthesized, ok := synthesizeApparmorStatus(it.Audit); ok {
        check.AuditCommand = wrapBash(synthesized)
        check.EvaluationRule = cisAutoEvalRuleJSON
        return check, ""
    }
}

// Pattern 16 (E-3 G9): dpkg-query 설치 상태. 1.7.1·5.3.1.1·(2.1.20 진단 결과 의존).
if isDpkgQueryAuditText(it.Audit) {
    if synthesized, ok := synthesizeDpkgQuery(it.Audit); ok {
        check.AuditCommand = wrapBash(synthesized)
        check.EvaluationRule = cisAutoEvalRuleJSON
        return check, ""
    }
}
```

### 4.3 selftest fixture 자동 생성

`evaluationRule = cisAutoEvalRuleJSON` 그대로 → `GenerateSelfTestSkeletons`가 7건 fixture(PASS + FAIL) 자동 생성. selftest harness 변경 0.

### 4.4 packs/cis-ubuntu-2404/ 영향

- 7 checks yaml 갱신 (1.6.4, 7.1.10, 1.3.1.3, 1.3.1.4, 1.7.1, 2.1.20, 5.3.1.1) — 단, 2.1.20은 D-E3-3 결과에 따라 6건 또는 7건.
- 7 selftest yaml 신규 추가 (또는 6).
- manifest.json hash 갱신 (R30-3 sign-pack 자동).
- `make ci`의 pack-tools convert 자동 호출로 diff commit.

## 5. TDD Stage 분해

각 그룹별 step + integration + pack 재변환. 권장 분리 — **5 commit**.

### Stage 1 — 진단 + G9 (가장 단순) — 1 commit

- 2.1.20의 현재 degraded 원인 진단 (디버그 print 또는 단위 테스트로 `extractCISLastShellLine` 출력 확인).
- 결과 — D-E3-3 결정.
- `cis_dpkg.go` + `cis_dpkg_test.go` 추가:
  - `extractDpkgQueryChecks` (단일 cmd + mode 판정 — `notInstalled` / `installOK` / `emptyOutput`).
  - `synthesizeDpkgQuery`.
  - 단위 테스트 ≥ 6 (1.7.1 PASS/FAIL, 5.3.1.1 PASS/FAIL, optional 2.1.20 PASS/FAIL).
- dispatch는 Stage 4에서 일괄 추가 (Stage 1~3은 합성기·인식기만, integration은 마지막).

### Stage 2 — G12 stat 옵트 — 1 commit

- `cis_stat_opt.go` + `cis_stat_opt_test.go` 추가:
  - `extractStatOptChecks` (`statOptCheck` 슬라이스 — multiple path, 1.6.4 = 1, 7.1.10 = 2).
  - `synthesizeStatOpt` (각 path: `[ -e ... ] && stat ...` + mode 비교 + Uid 검증, missing 카운트).
  - 단위 테스트 ≥ 6 (1.6.4 PASS/FAIL, 7.1.10 PASS/FAIL/path 미존재 PASS).

### Stage 3 — G8 apparmor — 1 commit

- `cis_apparmor.go` + `cis_apparmor_test.go` 추가:
  - `extractApparmorMode` (audit text phrase로 `enforceOnly` vs `enforceOrComplain` 자동 판정).
  - `synthesizeApparmorStatus` (loaded ≥ 1 / complain == 0 (mode 의존) / unconfined == 0).
  - 단위 테스트 ≥ 6 (1.3.1.3 PASS/FAIL, 1.3.1.4 PASS/FAIL, mode 판정 분기 2종).

### Stage 4 — dispatch 결선 + integration test — 1 commit

- `cis.go` dispatch 3분기 추가 (§4.2 순서).
- `cis_synth_integration_test.go` 보강:
  - 7 ID(또는 6) 각각 baseline JSON 입력 → `convertCISItem` 결과 PASS 마커 emit 확인.
  - dispatch 우선순위 검증 (G12가 stat permission 분기보다 우선).

### Stage 5 — convert 재실행 + handoff — 1 commit

- `pack-tools convert ... cis_ubuntu_2404` 재실행.
- 변환률 변화 확인: 86.5% → 88.7% (+2.2%p) 또는 88.5% (D-E3-3 결과 6건만).
- `packs/cis-ubuntu-2404/checks/{1.6.4,7.1.10,1.3.1.3,1.3.1.4,1.7.1,2.1.20?,5.3.1.1}.yaml` diff.
- selftest 7(또는 6) 신규 yaml.
- manifest hash 재계산.
- `docs/operations/cis-ubuntu-2404-degraded.md` 갱신 (52 → 45 또는 46).
- `SESSION_HANDOFF.md` 업데이트.

**총 ~1.0일** (Stage 1: 0.25일, Stage 2: 0.2일, Stage 3: 0.25일, Stage 4: 0.15일, Stage 5: 0.15일).

## 6. 결정 항목 (D-E3-N)

각 항목 권장 default 명시 — 다음 세션 즉시 진입 부담 0.

### D-E3-1 — 합성기 파일 분리 vs 통합

**선택지**:
1. **신규 3개 파일 (`cis_stat_opt.go` + `cis_apparmor.go` + `cis_dpkg.go`)** — D-N-8 패턴 일관 ← **권장 default**
2. cis.go에 인라인 추가 (line 한계 진입 — 현재 ~870 lines)

**근거**: cis.go는 800줄 권장 한계 초과 진입 직전(현재 ~870 lines, design doc §11.x file size guideline). E-2에서 이미 `cis_iptables.go`·`cis_nftables.go` 분리 선례. 회귀 격리 + 향후 epic 동일 패턴 답습.

### D-E3-2 — G8 apparmor mode 판정 방법

**선택지**:
1. **audit text phrase 매칭 자동 판정** (`"in either enforce or complain"` vs `"are not in complain"`) ← **권장 default**
2. CIS ID 직접 매칭 (1.3.1.3 vs 1.3.1.4)
3. baseline JSON metadata 추가 (별 epic — 본 epic 비대상)

**근거**: phrase 매칭은 audit text 의미 기반(권위), ID 매칭은 baseline 갱신 시 깨질 위험. nrobotcheck baseline 변경 빈도는 낮지만 결정론 유지 우선.

### D-E3-3 — 2.1.20 cover 방식

**선택지**:
1. **Stage 1 진단 결과에 따라 분기**:
   - 진단이 "기존 `extractCISLastShellLine` quoted continuation 미처리" → **본 epic 외 별 fix commit + 본 epic은 6건만 cover (1.6.4·7.1.10·1.3.1.3·1.3.1.4·1.7.1·5.3.1.1)** ← **권장 default**
   - 진단이 "phrase/cmd shape 충돌" → 본 epic G9 분기에 emptyOutput mode 추가
2. 진단 없이 본 epic G9 분기에서 명시적 cover (옵션 B 통합)
3. 2.1.20을 본 epic 비대상으로 영구 제외

**근거**: 기존 expect-empty 분기와 의미 동일 — 본 epic 외 fix가 더 깨끗(다른 유사 항목 동시 cover 가능). Stage 1 진단 비용 ~0.05일 — 권장.

### D-E3-4 — Stage 분리 단위 (5 commit vs 통합)

**선택지**:
1. **5 commit (Stage 1~5)** — 각 그룹 독립 commit + dispatch + convert ← **권장 default**
2. 4 commit (G9·G12·G8 합성기 1 commit + dispatch + convert + handoff)
3. 통합 단일 commit

**근거**: D epic 5 commit 선례(`cis-6-2-3-auditd-design.md`) 답습. 회귀 발생 시 bisect 효율, 각 그룹별 단위 테스트 격리.

### D-E3-5 — G12 다중 path 처리 (7.1.10)

**선택지**:
1. **path 슬라이스 (`statOptCheck struct{ path, expectedMode string }`)** — 자연스러운 N path 확장 ← **권장 default**
2. 단일 path 가정 (1.6.4만 cover, 7.1.10은 별 분기)

**근거**: 7.1.10은 2 path(`/etc/security/opasswd` + `.old`), 향후 유사 audit text 추가 가능 — 슬라이스가 일반화. 코드 lines 차이 무시 가능.

### D-E3-6 — apparmor `unconfined` 절 cover 여부

**선택지**:
1. **cover (loaded ≥ 1 / complain (mode 의존) / unconfined == 0 모두 검증)** ← **권장 default**
2. cover 안 함 (loaded ≥ 1 / complain만 — 단순화)

**근거**: audit text 마지막 절(`processes are unconfined but have a profile defined`)은 명시적 가드 의도. 단순화 시 false PASS 위험 — 보안 도메인 우선.

### D-E3-7 — apparmor 출력 phrase 변형 대응

**선택지**:
1. **POSIX C locale 가정 + 운영자 환경 verify 책임** ← **권장 default**
2. multi-locale phrase 매핑 테이블 (en/de/ja/...)
3. apparmor_status 대안(JSON output: `apparmor_status --json`) 사용 — Ubuntu 24.04 apparmor 4.0+ 지원 확인 필요

**근거**: rosshield agent 환경은 LANG=C 강제 가정(별 design doc 권장 — `02-system-overview-and-deployment.md` 운영 환경 절). multi-locale은 별 epic 비용 큼. JSON output은 backwards compat 검증 필요 — 본 epic 비대상.

## 7. 회귀 위험 / 운영 고려

- **G12 dispatch 우선순위** — 기존 stat permission 분기(`synthesizeCISShellAssertion` 내부 `isStatCommand`)는 `[ -e file ] &&` prefix 미인식 → false trigger 0이지만, dispatch 위치가 `synthesizeCISShellAssertion` 호출 **위**여야 함(specific 우선). Stage 4 integration test에서 1.6.4·7.1.10이 G12 분기로 라우팅되는지 검증 필수.
- **G8 apparmor 출력 변형** — Ubuntu 24.04 base apparmor는 안정적이지만 향후 minor 업데이트 시 phrase 변형 가능 (`profiles loaded` → `enforced profiles` 등). 향후 변형 시 `regexpApparmorLoadedCount` 등 다 phrase OR 매칭으로 보강.
- **G9 2.1.20 진단 결과 처리** — Stage 1 진단이 "기존 expect-empty 분기 fix 가능"으로 결론 시 본 epic 외 별 commit (`fix(converter): expect-empty 분기 quoted continuation 처리`). 해당 fix는 다른 유사 항목 동시 cover 가능 — 변환률 추가 향상 잠재.
- **manifest hash 변경** — pack 7 checks + 7 selftest = 14 file change(또는 12) → R30-3 sign-pack 워크플로우 재실행 필요(D·E1·E2 epic 동일 패턴, 운영자 영향 0).
- **CI auto-convert** — `make ci`의 `pack-tools convert` 자동 호출 단계 있음 → 변환 결과 diff가 commit되어야 — Stage 5에 명시.
- **degraded 가이드 갱신** — `docs/operations/cis-ubuntu-2404-degraded.md` 52 → 45(또는 46) NoMarker 갱신, 7건 fixture 자동 생성으로 selftest 갯수도 갱신.
- **잔여 NoMarker 14건** — 본 epic 완료 후 잔여 NoMarker 14건(31 - 6 [E-1] - 4 [E-2 실현분] - 7 [본 epic]). 단, E-2는 4건만 실현 — `cis-nomarker-31-analysis.md` §4 E-2(5건 예상)와 차이 있음. 실현 4건은 `1.4.1`/`5.2.6`/`5.4.1.6`/`5.4.3.2`/`6.2.3.6`/G16 3건/G3·G5·G10·G13·G14·G16·G17 등 후속 epic 후보 재평가 (별 design doc).
- **변환률 90% 도달 경로** — 본 epic(86.5% → 88.7%) + G16 epic(`5.4.2.{2,3,4}` 3건, +1.0%p, 89.7%) + G14(`1.4.1`, +0.3%p, 90.0%) = 90.0% 달성. 즉 **본 epic + 추가 4건 epic 1~2개로 90% 진입 가능**.

## 8. 참조

- 직전 design doc: `docs/design/notes/cis-nomarker-31-analysis.md` §3 G8/G9/G12, §4 E-3 후보
- D epic design doc(스타일 reference): `docs/design/notes/cis-6-2-3-auditd-design.md`
- 합성 함수 패턴 (D-N-8 일관):
  - `cmd/pack-tools/converter/cis_auditctl.go` (D epic, 246 lines)
  - `cmd/pack-tools/converter/cis_gsettings.go` (E-1 epic, 175 lines — bool + uint32 2 변형)
  - `cmd/pack-tools/converter/cis_sshd_or.go` (E-1 epic, 120 lines)
  - `cmd/pack-tools/converter/cis_grep_multi.go` (E-1 epic auditd grep, 88 lines)
  - `cmd/pack-tools/converter/cis_nftables.go` (E-2 epic)
  - `cmd/pack-tools/converter/cis_iptables.go` (E-2 epic, 100 lines)
- 변환기 dispatch: `cmd/pack-tools/converter/cis.go::convertCISItem` (line 150~290)
- 기존 stat permission 합성기: `cmd/pack-tools/converter/cis.go::synthesizeExpectStatPerm` (line 759)
- selftest 자동 생성: `cmd/pack-tools/converter/selftest.go::GenerateSelfTestSkeletons`
- nrobotcheck baseline: `D:\robot\dev\nrobotcheck\resources\baselines\cis_ubuntu_2404_benchmark.json` (utf-8)
- pack 출력: `packs/cis-ubuntu-2404/checks/*.yaml` (312 checks, 270 자동 + 7 본 epic 후보 + 14 잔여 NoMarker + 21 Manual)
- degraded 가이드: `docs/operations/cis-ubuntu-2404-degraded.md` (Stage 5에서 52 → 45/46 갱신)
- 설계 원칙: `docs/design/01-principles.md` §1(결정론) · §6(결정론적 fallback) · §9(불변성)
- 메모리 패턴: `feedback_design_doc_first.md` (1일 작업 — 본 design doc 우선 정책 적용 대상)
