# CIS Ubuntu 24.04 잔여 NoMarker 31건 분류 + 차기 epic 후보 — Design

> **상태**: Phase 0 design (다음 세션 진입점). CIS Ubuntu 24.04 pack 자동 변환 차기 epic 식별. 본 문서는 코드 0줄 / pack 변경 0 — 패턴 분류 + epic 후보 ROI 평가까지만 마감.

## 1. 배경

CIS Ubuntu 24.04 pack 자동 변환률은 D epic(`4844b71`) 직후 **83.3%** (260/312 자동, +5.7%p)에 도달했습니다. 잔여 degraded 52건은 다음과 같이 분포합니다:

- **NoMarker 31건** — `assessment_status=Automated`이지만 PASS 마커 + 자동 변환 패턴 9종(SSHD numeric/range/boolean, stat permission, grep verify, grep alternation, awk exact, expect-empty, expect-non-empty, hashbang body wrap) + 신규 6.2.3.x auditctl 합성기 모두 미매칭. 합성 패턴을 추가하지 않는 한 cover 불가.
- **Manual 21건** — `assessment_status=Manual` (CIS가 명시적 manual review 요구). 자동 변환 비대상 — 본 문서 범위 외.

본 문서는 **NoMarker 31건의 audit text를 표면 형태별로 17개 그룹**으로 분류하고, **차기 epic 후보 3개**를 ROI(추가 변환률 / 합성 복잡도 비) 순으로 제시합니다. 권장 우선순위 1번 epic은 §5에서 별 design doc 분리 추천.

## 2. 31건 ID 추출 방법 + 결과

### 2.1 추출 방법

방법: pack-tools convert 출력의 degraded sentinel + baseline assessment_status cross-reference.

```
1. packs/cis-ubuntu-2404/checks/*.yaml 중 `auditCommand: "true"` (degraded
   sentinel) 매칭 → 52건.
2. nrobotcheck baseline JSON에서 각 ID의 assessment_status 조회:
   - "Automated" → NoMarker (31건)
   - "Manual"    → Manual   (21건)
```

selftest 디렉토리는 `evaluationRule.value == "** PASS **"` 매칭 시 자동 생성되므로, NoMarker 31건은 selftest yaml이 없습니다(260 selftest vs 312 checks = 52 누락 — 31 NoMarker + 21 Manual 동치).

### 2.2 31 NoMarker ID 목록

```
1.3.1.3   1.3.1.4   1.4.1     1.6.4     1.7.1     1.7.4     1.7.6
1.7.8     2.1.20    4.2.4     4.2.6     4.2.7     4.3.4     4.3.5
4.3.8     4.3.10    4.4.2.1   4.4.2.2   4.4.2.4   5.1.4     5.1.14
5.2.6     5.3.1.1   5.4.1.6   5.4.2.2   5.4.2.3   5.4.2.4   5.4.3.2
6.2.2.3   6.2.3.6   7.1.10
```

총 31건. 영역 분포: §1.x(8) §2.x(1) §4.x(8) §5.x(11) §6.x(2) §7.x(1).

### 2.3 21 Manual ID 목록 (참고 — 본 문서 범위 외)

```
1.1.1.10  1.2.1.1   1.2.1.2   1.2.2.1   2.1.22    3.1.1     4.2.5
4.3.3     4.3.7     4.4.2.3   4.4.3.3   5.3.3.2.3 5.4.1.2   6.1.1.2
6.1.1.3   6.1.2.1.2 6.1.3.5   6.1.3.6   6.1.3.8   6.2.3.21  7.1.13
```

## 3. 패턴 분류 (17 그룹)

각 그룹: 그룹명 / IDs / 미매칭 사유 / sample audit text(1건 인용).

### G1 — nftables `nft list ruleset | grep 'hook X'` 3 cmds × 1 expected line (2건)

**IDs**: 4.3.5, 4.3.8.

**사유**: `# nft list ruleset | grep 'hook input'` 형식의 cmd가 3개 연속 + 각 cmd 직후 expected 1줄. 기존 grep alternation은 단일 cmd만 대상, multi-cmd verify는 미커버.

**Sample (4.3.8)**:
```
Run the following commands and verify that base chains contain a policy of DROP.
# nft list ruleset | grep 'hook input'
type filter hook input priority 0; policy drop;
# nft list ruleset | grep 'hook forward'
type filter hook forward priority 0; policy drop;
# nft list ruleset | grep 'hook output'
type filter hook output priority 0; policy drop;
```

### G2 — nftables 단일 cmd "Return should include" (1건)

**IDs**: 4.3.4.

**사유**: 단일 `nft list tables` + "Return should include" 1줄 (`table inet filter`). expect-non-empty 매칭 가능해 보이지만 인식 휴리스틱(`Nothing returned` co-occurrence 등)에 미일치.

**Sample (4.3.4)**:
```
Run the following command to verify that a nftables table exists:
# nft list tables
Return should include a list of nftables:
Example:
table inet filter
```

### G3 — nftables `include` + awk hook block scan (1건)

**IDs**: 4.3.10.

**사유**: 복잡한 cmd substitution(`$(awk ... /etc/nftables.conf)`) 안에 awk 인라인 → audit text의 multi-line `\` continuation이 깨짐. 3개 hook(input/forward/output) 검증 cmd 반복.

**Sample (4.3.10 발췌)**:
```
# [ -n "$(grep -E '^\s*include' /etc/nftables.conf)" ] && awk '/hook
input/,/}/' $(awk '$1 ~ /^\s*include/ { gsub("\"","",$2);print $2 }'
/etc/nftables.conf)
Output should be similar to:
type filter hook input priority 0; policy drop;
...
```

### G4 — iptables `-L` policy 매칭 (2건)

**IDs**: 4.4.2.1, 4.4.2.2.

**사유**: `iptables -L [CHAIN] [-v -n]` 출력의 multi-line table 안 특정 token(`policy DROP`, `ACCEPT all -- lo`) 검증. 표 형식 출력은 line order + column alignment 검증 필요 — 현재 패턴 미지원.

**Sample (4.4.2.1)**:
```
Run the following command and verify that the policy for the INPUT , OUTPUT , and
FORWARD chains is DROP or REJECT :
# iptables -L
Chain INPUT (policy DROP)
Chain FORWARD (policy DROP)
Chain OUTPUT (policy DROP)
```

### G5 — open ports vs firewall rule cross-reference (2건)

**IDs**: 4.2.6, 4.4.2.4.

**사유**: `ss -tuln`으로 추출한 open ports와 `ufw status` / `iptables -L`의 rule을 cross-reference하는 set diff. 4.2.6은 hashbang script, 4.4.2.4는 narrative + 운영자 manual 판단.

**Sample (4.2.6 발췌)**:
```
#!/usr/bin/env bash
{
unset a_ufwout;unset a_openports
while read -r l_ufwport; do
[ -n "$l_ufwport" ] && a_ufwout+=("$l_ufwport")
done < <(ufw status verbose | grep -Po '^\h*\d+\b' | sort -u)
... (a_diff 계산 후 FAIL/PASS 출력)
```

### G6 — ufw status verbose grep + multi-line example (2건)

**IDs**: 4.2.4, 4.2.7.

**사유**: 4.2.4는 2개 cmd(`grep ... before.rules` + `ufw status verbose`) + 표 형식 expected, 4.2.7은 단일 `ufw status verbose | grep Default:` + Example 1줄. example output에 IPv6/v4 분기 + 표 정렬 → 단순 grep verify 미일치.

**Sample (4.2.7)**:
```
Run the following command and verify that the default policy for incoming, outgoing,
and routed directions is deny, reject, or disabled:
# ufw status verbose | grep Default:
Example output:
Default: deny (incoming), deny (outgoing), disabled (routed)
```

### G7 — `gsettings get` scalar + 정확/범위 매칭 (3건)

**IDs**: 1.7.4, 1.7.6, 1.7.8.

**사유**: `gsettings get <schema> <key>` 출력이 `uint32 N` (5/900) 또는 `false`/`true`. SSHD numeric/range 합성기와 매우 유사한 형태이지만 인식 휴리스틱이 sshd 특화.

**Sample (1.7.4)**:
```
# gsettings get org.gnome.desktop.screensaver lock-delay
uint32 5
# gsettings get org.gnome.desktop.session idle-delay
uint32 900
```

### G8 — apparmor_status grep + count text (2건)

**IDs**: 1.3.1.3, 1.3.1.4.

**사유**: `apparmor_status | grep profiles` 출력 `N profiles are loaded.` + `M profiles are in enforce mode.` 4줄. 카운트 추출 + 비교(loaded ≥ 1, complain == 0 등) 필요.

**Sample (1.3.1.4)**:
```
# apparmor_status | grep profiles
34 profiles are loaded.
34 profiles are in enforce mode.
0 profiles are in complain mode.
2 processes have profiles defined.
```

### G9 — `dpkg-query` 설치 상태 (3건)

**IDs**: 1.7.1, 2.1.20, 5.3.1.1.

**사유**: `dpkg-query -W -f=...` 또는 `dpkg-query -s` 출력 `not-installed` / `Status: install ok installed` 검증. 1.7.1·2.1.20은 "not-installed 검증", 5.3.1.1은 "installed + version 검증".

**Sample (1.7.1)**:
```
# dpkg-query -W -f='${binary:Package}\t${Status}\t${db:Status-Status}\n' gdm3
gdm3 unknown ok not-installed not-installed
```

### G10 — bash hashbang body PASS/FAIL emit (2건)

**IDs**: 5.4.1.6, 5.4.3.2.

**사유**: audit text의 hashbang script가 자체적으로 `PASSED` / `FAILED` 또는 "Nothing is returned" 의미의 출력을 emit. 5.4.1.6은 expect-empty body wrap이 작동했어야 하지만 인식 미일치, 5.4.3.2는 `PASSED\n\nTMOUT...` 마커가 `** PASS **`와 다른 형태.

**Sample (5.4.3.2 발췌)**:
```
{
... (TMOUT 검증)
if [ -n "$output1" ] && [ -z "$output2" ]; then
echo -e "\nPASSED\n\nTMOUT is configured in: \"$output1\"\n"
else
[ -z "$output1" ] && echo -e "\nFAILED\n\nTMOUT is not configured\n"
[ -n "$output2" ] && echo -e "\nFAILED\n\nTMOUT is incorrectly configured...\n"
fi
}
```

### G11 — `sshd -T` grep alternation/option (2건)

**IDs**: 5.1.4, 5.1.14.

**사유**: 5.1.4는 "matches at least one of the following lines" + 4 alternation, 5.1.14는 "matches loglevel VERBOSE or loglevel INFO" + Match block 추가 검증. 기존 grep alternation 합성기는 단일 키 + alternation set 매칭이지만 두 항목 모두 multi-line/Match block 컨텍스트가 추가되어 인식 미일치.

**Sample (5.1.14)**:
```
# sshd -T | grep loglevel
loglevel VERBOSE
- OR -
loglevel INFO
```

### G12 — file stat + "Nothing is returned" 옵트 (2건)

**IDs**: 1.6.4, 7.1.10.

**사유**: `[ -e file ] && stat ...` cmd + "OR Nothing is returned" 분기. 파일 존재 시 stat 출력 매칭, 미존재 시 PASS. 기존 stat permission 합성기는 file 존재 가정 — 옵트 분기 미커버.

**Sample (1.6.4)**:
```
# [ -e /etc/motd ] && stat -Lc 'Access: (%#a/%A) Uid: ( %u/ %U) Gid: { %g/
%G)' /etc/motd
Access: (0644/-rw-r--r--) Uid: ( 0/ root) Gid: ( 0/ root)
-- OR --
Nothing is returned
```

### G13 — `sudo` cache timeout 캡처 + ≤15 비교 (1건)

**IDs**: 5.2.6.

**사유**: `grep -roP "timestamp_timeout=\K[0-9]*" /etc/sudoers*` 출력 정수 ≤ 15 비교 + 미설정 시 default 15분(`sudo -V | grep ...` fallback). multi-step 분기.

### G14 — grub.cfg multi-line verify (2 cmds × 1 line) (1건)

**IDs**: 1.4.1.

**사유**: `grep "^set superusers"` + `awk -F. '/^\s*password/'` 두 cmd 출력 매칭. 각 expected가 placeholder(`<username>`) 포함 → 정확 매칭 불가, prefix 매칭 필요.

**Sample (1.4.1 전체)**:
```
# grep "^set superusers" /boot/grub/grub.cfg
set superusers="<username>"
# awk -F. '/^\s*password/ {print $1"."$2"."$3}' /boot/grub/grub.cfg
password_pbkdf2 <username> grub.pbkdf2.sha512
```

### G15 — auditd.conf grep alternation (2 cmds) (1건)

**IDs**: 6.2.2.3.

**사유**: `grep -Pi -- '... (halt|single)' /etc/audit/auditd.conf` 두 cmd × 1 expected. 기존 grep alternation 합성기와 매우 유사하지만 2 cmd 묶음에 미매칭(단일 cmd 가정).

**Sample (6.2.2.3)**:
```
# grep -Pi -- '^\h*disk_full_action\h*=\h*(halt|single)\b'
/etc/audit/auditd.conf
disk_full_action = <halt|single>
# grep -Pi -- '^\h*disk_error_action\h*=\h*(syslog|single|halt)\b'
/etc/audit/auditd.conf
disk_error_action = <syslog|single|halt>
```

### G16 — `passwd`/`group` awk + alternative match (3건)

**IDs**: 5.4.2.2, 5.4.2.3, 5.4.2.4.

**사유**: `awk -F: '...' /etc/passwd|/etc/group` 출력이 `root:0` 1줄(positive)만 허용하거나, `passwd -S root` 출력이 `User: "root" Password is status: P|L` alternation. 단일 라인 expected + Note 필터링 + alternation 분기 동시 필요.

**Sample (5.4.2.4)**:
```
# passwd -S root | awk '$2 ~ /^(P|L)/ {print "User: \"" $1 "\" Password is
status: " $2}'
Verify the output is either:
User: "root" Password is status: P
- OR -
User: "root" Password is status: L
```

### G17 — privileged commands hashbang loop ("all OK") (1건)

**IDs**: 6.2.3.6.

**사유**: D epic 직전 design doc(`cis-6-2-3-auditd-design.md` D-N-6)에서 manual fixture 권장으로 의도적 degraded 유지. audit text의 partition·privileged file traversal이 시스템마다 다른 결과 → 결정론적 정규화 어려움.

## 4. 차기 epic 후보 (ROI 순 3개)

ROI 정의: **포함 ID 수 / 합성 복잡도(추정 코드 lines + Stage 수)**. 각 후보의 변환률 향상은 312 기준 +N.N%p.

### 후보 E-1 (권장) — `gsettings` + `sshd -T` + 단순 grep alternation 묶음 (G7 + G11 + G15, 6건, +1.9%p)

**그룹 통합 근거**: 세 그룹 모두 **단일 cmd 출력 → 정확/범위/alternation 매칭** 패턴이며, 기존 합성기(`synthesizeExpectSSHDOption`, `synthesizeExpectExact`, grep alternation)와 1:1 대응. 인식 휴리스틱만 확장하면 별 합성 함수 거의 추가 없음.

**합성 전략 옵션**:
1. **인식 휴리스틱 확장 (권장)** — 기존 `synthesizeCISShellAssertion` 7-tier dispatch에 새 분기 3개:
   - G7: `^# gsettings get \S+ \S+\n(uint32 N|true|false)` regex → `synthesizeExpectExact` + (uint32 prefix 처리 시) `synthesizeExpectSSHDRange` 변형
   - G11: `^# sshd -T \| grep ...\n(matches at least one|matches loglevel)` regex → 기존 grep alternation 분기에 multi-line "OR" expected 추가
   - G15: 2 cmd × `grep -Pi ... (halt|single)` 반복 → 단일 cmd로 평탄화 후 기존 grep alternation 재사용
2. 단일 신규 함수 `synthesizeKeyValueAlternation(cmd, alternatives)` 도입 — 코드 분기 더 깨끗하지만 6건만 cover하기엔 추가 추상화 비용 ↑.

**회귀 위험**: 저. 신규 인식 regex는 정확한 cmd shape(`gsettings get`, `sshd -T | grep`, `auditd.conf` 경로 명시)로 narrow → 다른 항목 false trigger 0. 기존 합성 함수는 그대로 재사용.

**정확도**: ~95% (G7 uint32 범위 매칭 + G11 alternation은 결정론적, G15는 2 cmd 평탄화 시 양쪽 모두 grep 통과 확인 필요).

**추정 시간**: 0.6일. design doc 0.2일 + Stage 1 인식 regex(0.1) + Stage 2 분기 결선(0.15) + Stage 3 integration test(0.1) + Stage 4 convert + handoff(0.05).

**잠재 변환률**: 83.3% → **85.2%** (+1.9%p, 6건 추가).

### 후보 E-2 — nftables + iptables verify 묶음 (G1 + G2 + G4, 5건, +1.6%p)

**그룹 통합 근거**: 모두 `nft list ...` 또는 `iptables -L`의 부분 출력에 특정 token(`hook input`, `policy DROP`)이 포함되는지 검증. 기존 grep verify와 유사하지만 multi-cmd 또는 multi-token 매칭.

**합성 전략 옵션**:
1. **신규 `synthesizeMultiCmdGrepAll(cmds, expectedTokens)`** — N개 cmd 출력을 순차 실행, 각 expected token이 적어도 1개 cmd 출력에 존재해야 PASS. G1(3 cmd × 1 token), G4(1 cmd × 3 token) 모두 cover.
2. **per-그룹 분기** — G1·G2·G4 각각 별 인식 regex + 기존 grep verify 재사용. 코드 분기 ↑이지만 정확도 ↑.

**회귀 위험**: 중. iptables 출력은 표 형식 — token 일치만으론 false PASS 가능(예: `policy DROP`이 다른 chain에서 매칭). chain별 substring 매칭(`Chain INPUT.*policy DROP`) 정규식 필요.

**정확도**: ~85% (4.4.2.2의 multi-line ACCEPT/DROP rule 순서 검증은 부분만 cover).

**추정 시간**: 0.7일. design doc 0.2일 + Stage 1 regex(0.15) + Stage 2 합성 함수(0.2) + Stage 3 integration(0.1) + Stage 4 convert + handoff(0.05).

**잠재 변환률**: 83.3% → **84.9%** (+1.6%p, 5건 추가).

### 후보 E-3 — file stat 옵트 + apparmor count + dpkg-query 묶음 (G12 + G8 + G9, 7건, +2.2%p)

**그룹 통합 근거**: 세 그룹은 표면 형태가 다르지만 모두 **출력 라인 카운트 또는 단순 token 매칭** 패턴. 단, 각 그룹별 합성 코드가 다름 — 통합 epic 추진 시 코드 ↑.

**합성 전략 옵션**:
1. **3개 별 신규 함수** — `synthesizeStatOrEmpty(cmd, mode)`, `synthesizeApparmorCountCheck(...)`, `synthesizeDpkgInstalled(pkg, status)`. G12·G8·G9 각각 1 함수.
2. **G12만 우선 cover** (2건, +0.6%p) — 가장 단순(stat permission 기존 합성기에 옵트 분기 추가). G8·G9는 별 epic으로 분리.

**회귀 위험**: 저(G12) ~ 중(G8 — apparmor 출력 텍스트가 locale·버전 의존).

**정확도**: ~90% (G12 stat + 옵트 결정론적, G8 카운트 ≥1 확인 결정론적, G9 dpkg-query 출력 안정).

**추정 시간**: 1.0일. design doc 0.3일 + 3 그룹별 Stage 분해(각 0.2일) + integration + handoff(0.1).

**잠재 변환률**: 83.3% → **85.5%** (+2.2%p, 7건 추가).

### 후보 비교 표

| 후보 | 그룹 | ID 수 | 잠재 +%p | 추정 시간 | 코드 추가 lines | 회귀 위험 | 정확도 | ROI(ID/일) |
|---|---|---|---|---|---|---|---|---|
| **E-1** | G7+G11+G15 | 6 | +1.9 | 0.6일 | ~80 | 저 | 95% | **10.0** |
| E-2 | G1+G2+G4 | 5 | +1.6 | 0.7일 | ~120 | 중 | 85% | 7.1 |
| E-3 | G12+G8+G9 | 7 | +2.2 | 1.0일 | ~180 | 저~중 | 90% | 7.0 |

**비고 — 후보 외 잔여 13건**: G3 (4.3.10, 1건), G5 (4.2.6+4.4.2.4, 2건), G6 (4.2.4+4.2.7, 2건), G10 (5.4.1.6+5.4.3.2, 2건), G13 (5.2.6, 1건), G14 (1.4.1, 1건), G16 (5.4.2.{2,3,4}, 3건), G17 (6.2.3.6, 1건). 합 13건 = 31 - 6 - 5 - 7. 이들은 합성 복잡도 ↑ 또는 단일 ID per 그룹으로 ROI ↓ — 후속 epic 또는 manual fixture로 처리 권장.

## 5. 권장 — 우선순위 1 epic: E-1

근거:

- **ROI 최고 (10.0 ID/일)** — 6건 cover에 0.6일, 코드 ~80 lines.
- **회귀 위험 최저** — 인식 regex 확장 + 기존 합성 함수 재사용, 신규 합성 함수 거의 0.
- **정확도 95%** — gsettings/sshd/auditd.conf cmd shape이 안정적, alternation 매칭 결정론적.
- **자연스러운 다음 단계** — D epic이 6.2.3.x auditctl을 cover한 직후, 인접 6.2.2.x(auditd.conf G15) + 5.1.x(sshd G11) + 1.7.x(gsettings G7)으로 영역 확장.

E-1 완료 후 변환률 **85.2%** → 다음 epic 후보 재평가 시 E-3(+2.2%p, 잠재 87.4%) 또는 E-2(+1.6%p, 잠재 86.8%) 선택.

## 6. E-1 변경 사항 outline (다음 design doc 분리 추천)

본 design doc은 epic 후보 식별까지만 — E-1 상세 설계는 별 design doc(`cis-gsettings-sshd-auditd-design.md`)으로 분리 권장. 이유: §1 큰 작업 design doc 우선 원칙(0.6일 임계 근접) + 결정 항목 6개 이상 예상.

### 6.1 예상 신규 인식 분기 (cmd/pack-tools/converter/cis.go)

`synthesizeCISShellAssertion` 7-tier dispatch에 다음 3개 분기 추가(SSHD 분기와 grep verify 분기 사이):

```go
// G7: gsettings get scalar
if synthesized, ok := synthesizeGsettingsScalar(cmd, expectedLines); ok {
    return synthesized, true
}
// G11: sshd -T grep alternation (multi-line "OR" expected)
if synthesized, ok := synthesizeSSHDGrepAlternation(cmd, expectedLines); ok {
    return synthesized, true
}
// G15: 2 cmd × grep alternation (auditd.conf 등)
if synthesized, ok := synthesizeMultiCmdGrepAlternation(cmds, expectedTokens); ok {
    return synthesized, true
}
```

### 6.2 예상 신규 regex (cis.go 변수 블록 또는 신규 cis_kvalt.go)

```go
regexpGsettingsCmd  = regexp.MustCompile(`^#\s*gsettings\s+get\s+(\S+)\s+(\S+)`)
regexpGsettingsExp  = regexp.MustCompile(`^(uint32\s+(\d+)|true|false)$`)
regexpSSHDGrepCmd   = regexp.MustCompile(`^#\s*sshd\s+-T\s*\|\s*grep\s+(.+)`)
regexpAtLeastOneOf  = regexp.MustCompile(`(?i)matches\s+at\s+least\s+one\s+of`)
regexpAuditdConfGrep = regexp.MustCompile(`grep\s+-Pi\s+--\s+'.*'\s*\n?\s*/etc/audit/auditd\.conf`)
```

### 6.3 selftest fixture 자동 생성

`evaluationRule = cisAutoEvalRuleJSON` 그대로 → `GenerateSelfTestSkeletons`가 6 fixture(PASS + FAIL) 자동 생성. selftest harness 변경 0.

### 6.4 packs/cis-ubuntu-2404/ 영향

- 6 checks yaml 갱신 (1.7.4, 1.7.6, 1.7.8, 5.1.4, 5.1.14, 6.2.2.3)
- 6 selftest yaml 신규 추가
- manifest.json hash 갱신 (R30-3 sign-pack 자동)
- `make ci`의 pack-tools convert 자동 호출로 diff commit

## 7. 회귀 위험 / 운영 고려

- **신규 인식 regex의 false trigger** — gsettings/sshd-T/auditd.conf cmd shape으로 narrow하면 다른 280건에 영향 0. 단 5.1.x sshd 합성기와 5.1.4·5.1.14 신규 분기가 우선순위 충돌 가능 — Stage 1 unit test에서 dispatch 순서 검증 필수.
- **expected line 추출 견고성** — gsettings `uint32 N`은 한 줄, sshd alternation은 `- OR -` 구분자, auditd.conf grep alternation은 `<halt|single>` placeholder. 각 그룹 expected 추출 regex가 placeholder(`<...>`) 안전 처리 필요.
- **5.2.6 / 5.4.3.2 PASSED 마커 정합성** — 5.4.3.2의 `PASSED` 마커는 `** PASS **`와 다름 → G10에서 별 처리. E-1에는 미포함이지만 향후 epic에서 marker 다양화 합성기 검토 시 동일 문제 발생.
- **manifest hash 변경** — pack 파일 6 checks + 6 selftest = 12 file change → R30-3 sign-pack 워크플로우 재실행 필요(D epic과 동일 패턴, 운영자 영향 0).
- **CI auto-convert** — `make ci`에 `pack-tools convert` 자동 호출 단계 있음 → 변환 결과 diff가 commit되어야 — Stage 4에 명시.
- **나머지 13 NoMarker 후속** — E-1 + E-2 + E-3 모두 적용 시 변환률 **88.7%** (260+18=278/312), 잔여 13건은 단일 ID per 그룹 또는 high-complexity(G3 awk hook block, G5 set diff, G17 privileged loop) — manual fixture 또는 Phase 2 재평가 권장.
- **변환률 90% 도달 예상 경로** — E-1(0.6일) + E-3(1.0일) + E-2(0.7일) = 2.3일 누적, 변환률 88.7%. 추가 +1.3%p(4건)는 G16(5.4.2.x 3건, +1.0%p) cover 시 89.7%, G14(1.4.1) + G13(5.2.6) 추가 시 90.3% 달성. 즉 **5건 epic + ~4일 작업으로 90% 진입 가능**.

## 8. 참조

- 직전 design doc(스타일 참고): `docs/design/notes/cis-6-2-3-auditd-design.md` (D epic, 6.2.3.x 21건)
- 직전 변환 commit chain (D epic):
  - `d947342` design doc
  - `9c9c236` Stage 1 (regex+인식+추출 + 13 unit)
  - `7c749a2` Stage 2 (normalize+synthesize + 6 unit)
  - `f3ee594` Stage 3 (dispatch 결선 + 4 integration)
  - `4844b71` Stage 4·5 (convert 재실행, 77.6% → 83.3%)
  - `8460294` handoff finalize
- 변환기 코드:
  - `cmd/pack-tools/converter/cis.go` (845 lines, dispatch line 150~211)
  - `cmd/pack-tools/converter/cis_auditctl.go` (246 lines, D epic 신규)
- selftest 자동 생성: `cmd/pack-tools/converter/selftest.go::GenerateSelfTestSkeletons`
- nrobotcheck baseline: `D:\robot\dev\nrobotcheck\resources\baselines\cis_ubuntu_2404_benchmark.json`
- pack 출력: `packs/cis-ubuntu-2404/checks/*.yaml` (312 checks, 260 자동 + 31 NoMarker + 21 Manual)
- degraded 가이드: `docs/operations/cis-ubuntu-2404-degraded.md` (재생성 필요 — 70 → 52 갱신)
- 설계 원칙: `docs/design/01-principles.md` §1(결정론) · §6(결정론적 fallback) · §9(불변성)
- 메모리 패턴: `feedback_design_doc_first.md` (1일+ 작업은 design doc 우선 — E-1은 0.6일이라 경계, E-2/E-3는 design doc 필수)
