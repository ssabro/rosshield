# CIS Ubuntu 24.04 Manual 21건 운영자 fixture 작성 가이드 — Design

> **상태**: Phase 0 design (다음 세션 진입점). NoMarker 31건 자동 변환 트랙(`cis-nomarker-31-analysis.md`)의 후속으로, **`assessment_status="Manual"` 21건**의 분류 + 운영자 fixture 작성 가이드 + 우선순위를 정합니다. 본 문서는 코드 0줄 / pack 변경 0 — 분류·우선순위·template·결정 항목까지만 마감.

## 1. 배경

CIS Ubuntu 24.04 pack 자동 변환률 **83.3%** (260/312, D epic 직후) 도달 후 잔여 degraded 52건은 **NoMarker 31건**(자동 변환 epic 후보, 별 design doc) + **Manual 21건**(본 문서)로 분리됩니다.

Manual 21건은 CIS 원본 baseline에서 `assessment_status="Manual"`로 명시된 항목 — **CIS가 운영자의 정성적 판단·정책 검토·환경 의존 결정을 명시적으로 요구**합니다. 본 문서 §3에서 보듯 일부는 deterministic verify 명령(자동 변환 후보)이지만 절대 다수는 site policy 검토 또는 정성적 review가 본질입니다.

본 문서 목표:
1. 21건을 5개 카테고리로 분류 (§3)
2. high/medium/low 우선순위 평가 (§4)
3. 카테고리별 운영자 fixture 작성 template 제시 (§5)
4. 자동 변환 가능 여부 재평가 (§6)
5. Stage 분해 + 결정 항목 (§7~8)

본 트랙 완료 시 잔여 degraded는 **자동 변환 비대상이지만 운영자 직접 PASS/FAIL 판정 기준이 패키지로 제공**되어, 사용자 환경에 fleet 적용 시 즉시 가치 발생.

## 2. 21 Manual ID 목록

```
1.1.1.10  1.2.1.1   1.2.1.2   1.2.2.1   2.1.22    3.1.1     4.2.5
4.3.3     4.3.7     4.4.2.3   4.4.3.3   5.3.3.2.3 5.4.1.2   6.1.1.2
6.1.1.3   6.1.2.1.2 6.1.3.5   6.1.3.6   6.1.3.8   6.2.3.21  7.1.13
```

총 21건. 영역 분포: §1.x(1) §1.2.x(3) §2.x(1) §3.x(1) §4.x(6) §5.x(2) §6.1.x(6) §6.2.x(1) §7.x(1).

## 3. 카테고리 분류 (5개)

각 ID를 audit text 본질에 따라 분류합니다. 분류 기준:

- **C1 — Policy review**: CIS audit text에 "site policy", "approved by local site policy", "IAW site policy", "your organization", "configured correctly" 등 운영자 정책 판단 직접 명시.
- **C2 — Hardware/environment dependent**: 시스템 인스턴스마다 결과 차이가 본질 (예: GPU 유무, 마운트된 fs, 특정 패키지 설치 여부).
- **C3 — Multi-step verify (synthesis candidate)**: deterministic verify 명령의 묶음 — 운영자 종합 판단 명목이지만 실은 "출력이 비어 있는지 / 특정 token 포함 여부" 검증으로 환원 가능. 자동 변환 후보 (§6 재평가).
- **C4 — Configuration audit**: 설정 파일 cat/grep 후 운영자가 내용 검토 — site policy 의존이지만 cmd 자체는 결정론적.
- **C5 — Subjective check**: "ensure secure", "review the contents", "verify ... appropriate" 등 정성적 검증, 자동화 불가.

### 3.1 ID별 분류 표

| ID | 카테고리 | 핵심 audit text 발췌 (요약) | site policy 의존 |
|---|---|---|---|
| 1.1.1.10 | C2 | hashbang script — 사용 안 하는 fs kernel module 식별 (CVE 목록 vs lsmod 비교) → "review generated output" | 아니오 (env) |
| 1.2.1.1 | C1 | `apt-key list` → "REVIEW and VERIFY ... IAW site policy" | **예** |
| 1.2.1.2 | C1 | `apt-cache policy` → "verify package repositories are configured correctly" | **예** |
| 1.2.2.1 | C3 | `apt update` + `apt -s upgrade` → "Verify there are no updates or patches" | 아니오 (cmd) |
| 2.1.22 | C1 | `ss -plntu` → "All services listed are required ... approved by local site policy" | **예** |
| 3.1.1 | C3 | hashbang script — "IPv6 is enabled" 또는 "not enabled" 출력 | 아니오 (cmd) |
| 4.2.5 | C1 | `ufw status numbered` → "verify all rules ... match site policy" | **예** |
| 4.3.3 | C3 | `iptables -L` + `ip6tables -L` → "No rules should be returned" | 아니오 (cmd) |
| 4.3.7 | C1 | `nft list ruleset | awk ... | grep ...` 2회 → "match site policy" | **예** |
| 4.4.2.3 | C1 | `iptables -L -v -n` → "verify all rules ... match site policy" | **예** |
| 4.4.3.3 | C1+C3 | `ip6tables -L -v -n` OR IPv6 disabled hashbang → policy + script 분기 | **예** |
| 5.3.3.2.3 | C1 | `grep ... pwquality.conf` + common-password grep → "Complexity conforms to local site policy" | **예** |
| 5.4.1.2 | C1 | `grep PASS_MIN_DAYS` + awk shadow → "follows local site policy" | **예** |
| 6.1.1.2 | C4 | hashbang script — journald logfile mode 검증, **자체 PASS/REVIEW 마커 emit** | △ (script 자율) |
| 6.1.1.3 | C5 | hashbang script — log rotation 파라미터 → "review the output to ensure logs are rotated according to site policy" | **예** |
| 6.1.2.1.2 | C4 | hashbang script — journal-upload URL/cert 파일 → 자체 PASS/FAIL emit | △ (script 자율) |
| 6.1.3.5 | C5 | hashbang script — rsyslog config 출력 → "review the output ... appropriate logging is set" | **예** |
| 6.1.3.6 | C5 | hashbang script — rsyslog remote loghost 출력 → "Verify that logs are sent to a central host used by your organization" | **예** |
| 6.1.3.8 | C5 | `systemd-analyze cat-config /etc/logrotate.conf` → 운영자 검토 | **예** |
| 6.2.3.21 | C3 | `augenrules --check` → "/usr/sbin/augenrules: No change" deterministic | 아니오 (cmd) |
| 7.1.13 | C2+C5 | hashbang script — SUID/SGID 파일 enumerate → "Review ... ensure no rogue programs" | **예** (env+review) |

### 3.2 카테고리별 갯수

| 카테고리 | 갯수 | IDs |
|---|---|---|
| C1 — Policy review | **9** | 1.2.1.1, 1.2.1.2, 2.1.22, 4.2.5, 4.3.7, 4.4.2.3, 4.4.3.3, 5.3.3.2.3, 5.4.1.2 |
| C2 — Hardware/environment dependent | **2** | 1.1.1.10, 7.1.13 (C5 중복) |
| C3 — Multi-step verify (synthesis candidate) | **5** | 1.2.2.1, 3.1.1, 4.3.3, 4.4.3.3 (C1 중복), 6.2.3.21 |
| C4 — Configuration audit (script self-emits) | **2** | 6.1.1.2, 6.1.2.1.2 |
| C5 — Subjective check | **5** | 6.1.1.3, 6.1.3.5, 6.1.3.6, 6.1.3.8, 7.1.13 (C2 중복) |

(중복 분류 2건 — 4.4.3.3은 C1+C3, 7.1.13은 C2+C5 — 본질이 두 카테고리에 걸침. 합 21건 = 9+2+5+2+5 - 2 중복 = 21.)

## 4. 우선순위 평가 (high/medium/low)

평가 기준:
- **high** — fleet 보안에 critical하고, 운영자 판정 기준만 명확하면 즉시 fleet 적용 시 보안 가치 발생.
- **medium** — 일반 fleet 환경에서 적용 가치 있으나, site policy 분기가 환경마다 차이 큼.
- **low** — 결과 해석에 deep 도메인 지식 필요 / niche 환경 / 정성적 review 비중 매우 높음.

### 4.1 ID별 우선순위 표

| ID | 우선순위 | 근거 |
|---|---|---|
| 1.1.1.10 | **high** | 미사용 fs kernel module CVE 노출 — 모든 fleet 보안 critical, kernel module 목록은 결정론적 |
| 1.2.1.1 | medium | GPG key 검증 — 보안 중요하지만 키 식별 운영자 판단 |
| 1.2.1.2 | low | apt repo URL 정책 검토 — 환경별 차이 매우 큼 |
| 1.2.2.1 | **high** | 미적용 패치 검출 — fleet 보안 critical, `apt -s upgrade` 출력 라인 카운트 결정론적 |
| 2.1.22 | medium | 노출 포트 — 보안 중요하지만 site policy 의존 |
| 3.1.1 | medium | IPv6 enable 상태 식별 — 정책에 따라 PASS/FAIL 다름 |
| 4.2.5 | medium | ufw outbound rules — site policy 의존 |
| 4.3.3 | **high** | nftables 사용 시 iptables flush 검증 — "No rules" 결정론적, 보안 critical |
| 4.3.7 | medium | nftables outbound/established — site policy 의존 |
| 4.4.2.3 | medium | iptables outbound — site policy 의존 |
| 4.4.3.3 | medium | ip6tables outbound — IPv6 분기 + policy |
| 5.3.3.2.3 | **high** | 패스워드 복잡도 — 모든 fleet 보안 critical, dcredit/ucredit/lcredit/ocredit 결정론적 검증 가능 |
| 5.4.1.2 | **high** | PASS_MIN_DAYS — 패스워드 정책 critical, ≥1 검증 + shadow awk 결정론적 |
| 6.1.1.2 | medium | journald logfile perm — script 자체가 PASS/REVIEW emit |
| 6.1.1.3 | medium | journald log rotation — site policy 의존 |
| 6.1.2.1.2 | low | journal-upload — niche (중앙 로그서버 운영 환경 한정) |
| 6.1.3.5 | low | rsyslog 검토 — niche + 정성적 |
| 6.1.3.6 | low | rsyslog remote — niche (중앙 로그서버 운영 환경 한정) |
| 6.1.3.8 | low | logrotate 검토 — 정성적 |
| 6.2.3.21 | **high** | augenrules --check — auditd rules drift 검출, deterministic, D epic 6.2.3.x 동반 |
| 7.1.13 | medium | SUID/SGID enumerate — 환경별 결과 차이 크지만 보안 가치 ↑ |

### 4.2 우선순위별 갯수

| 우선순위 | 갯수 | IDs |
|---|---|---|
| **high** | **6** | 1.1.1.10, 1.2.2.1, 4.3.3, 5.3.3.2.3, 5.4.1.2, 6.2.3.21 |
| medium | 9 | 1.2.1.1, 2.1.22, 3.1.1, 4.2.5, 4.3.7, 4.4.2.3, 4.4.3.3, 6.1.1.2, 6.1.1.3, 7.1.13 (10건 — 본 행은 10이 정확, 표에서 medium 카운트는 10) |
| low | 5 | 1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8 |

(정정: medium 10건 — 1.2.1.1, 2.1.22, 3.1.1, 4.2.5, 4.3.7, 4.4.2.3, 4.4.3.3, 6.1.1.2, 6.1.1.3, 7.1.13. 합 6+10+5 = 21.)

## 5. 운영자 fixture 작성 template (카테고리별)

운영자가 직접 작성하는 manual fixture는 자동 변환 yaml과 분리해서 `packs/cis-ubuntu-2404/checks/manual/<id>.yaml`에 둡니다(D-MAN-1). selftest는 `packs/cis-ubuntu-2404/selftest/manual/<id>/{pass,fail}.yaml`에 두 fixture만 작성(자동 skeleton 미생성).

### 5.1 C1 (Policy review) template

```yaml
# packs/cis-ubuntu-2404/checks/manual/<id>.yaml
id: cis-ubuntu-2404-<id>
title: "<title>"
auditCommand: |
  # CIS audit cmd 인용 (예: apt-key list, ss -plntu)
  <원본 cmd 그대로>
evaluationRule:
  op: "manual"          # 신규 op — D-MAN-3 결정 필요
  prompt: |             # 운영자 화면에 표시할 정책 검토 프롬프트
    <CIS audit text의 "REVIEW" 절을 한국어로 요약>
    체크 항목:
    - <bullet 1>
    - <bullet 2>
  defaultVerdict: "review"   # PASS/FAIL/REVIEW 중 하나, 운영자 입력 전 default
references:
  - "CIS Ubuntu 24.04 Benchmark §<id>"
```

selftest fixture (PASS):
```yaml
# packs/cis-ubuntu-2404/selftest/manual/<id>/pass.yaml
input: |
  <auditCommand 출력 sample — site policy 합치 케이스>
expectedVerdict: "pass"
notes: "<운영자가 PASS로 판정한 근거 1줄>"
```

selftest fixture (FAIL): 동일 schema, expectedVerdict: "fail".

### 5.2 C2 (Hardware/env dependent) template

C1과 거의 동일하나 prompt에 환경 정보 명시:

```yaml
evaluationRule:
  op: "manual"
  prompt: |
    환경 의존 검사. 다음 환경 정보를 먼저 수집하세요:
    - <환경 정보 1: 예 "현재 사용 중인 filesystem 종류">
    - <환경 정보 2>
    그런 뒤 다음 기준으로 판정:
    PASS — <조건>
    FAIL — <조건>
  defaultVerdict: "review"
```

selftest fixture: 두 가지 환경(예: ext4-only fleet vs ceph-mounted fleet)별 PASS/FAIL pair.

### 5.3 C3 (Multi-step verify, synthesis candidate) template

§6 재평가 결과 자동 변환 가능으로 판정되면 본 카테고리는 자동 트랙으로 이관 — 본 문서는 fallback manual fixture template만 남김.

```yaml
auditCommand: |
  <multi-step cmd 묶음, $? 또는 출력 라인 카운트 검증 가능>
evaluationRule:
  op: "manual"
  prompt: |
    deterministic 검사. 출력이 비어 있으면 PASS, 그렇지 않으면 운영자 검토:
    PASS — <빈 출력 / "No change" / 0 line>
    FAIL — <non-empty / "drift" / 1+ line>
  defaultVerdict: "pass"   # deterministic 후보는 default pass
```

(자동 변환으로 이관 시 evaluationRule이 `{"op":"contains","value":"** PASS **"}` + auditCommand가 합성 bash로 대체.)

### 5.4 C4 (Script self-emits) template

audit text의 hashbang script가 이미 `** PASS **` / `** REVIEW **` / `** FAIL **` 마커를 emit:

```yaml
auditCommand: |
  #!/usr/bin/env bash
  <baseline hashbang body 그대로 — base64 wrap 가능>
evaluationRule:
  op: "manual"
  prompt: |
    스크립트가 자체 판정 마커를 emit합니다:
    - "** PASS **" → 운영자 확인 후 PASS
    - "** REVIEW **" → site policy 검토 후 판정
    - "** FAIL **" → FAIL
  defaultVerdict: "review"
```

(D-MAN-4: C4는 자동 변환 트랙으로 이관 가능 — `** PASS **` 마커 매칭 dispatch 1순위에 이미 hit. 단 `** REVIEW **` 또는 `** FAIL **` 분기가 추가 dispatch 필요.)

### 5.5 C5 (Subjective check) template

C1과 동일 schema, prompt에 정성적 review 항목 bullet:

```yaml
evaluationRule:
  op: "manual"
  prompt: |
    정성적 검토. 다음 항목을 직접 확인:
    - <log path>의 내용이 site logging policy에 부합하는가?
    - rotation 주기가 정책 기준 N일 이내인가?
    - <site-specific>
  defaultVerdict: "review"
```

## 6. 자동 변환 가능성 재평가 (NoMarker 31 design doc 의도 vs 보수적 추정)

NoMarker 31 design doc은 Manual을 일괄 자동 변환 비대상으로 분류했으나, `feedback_design_doc_conservative.md`(보수적 추정 학습)를 적용해 audit text별 deterministic 가능 여부 재평가 결과:

### 6.1 자동 변환 후보 5건 (C3 카테고리)

| ID | 합성 패턴 | 합성 복잡도 | 정확도 | 우선순위 |
|---|---|---|---|---|
| 1.2.2.1 | `apt -s upgrade | grep -c '^Inst '` → 0이면 PASS | 저 (synthesizeExpectExact 변형) | 90% | high |
| 3.1.1 | hashbang script 자체가 `IPv6 is (not )?enabled` emit → grep verify 1줄 | 저 (expect-non-empty 또는 hashbang body wrap) | 95% | medium |
| 4.3.3 | `iptables -L; ip6tables -L` 출력 라인 카운트 (chain header 제외 0) → PASS | 중 (multi-cmd line count) | 85% | high |
| 6.2.3.21 | `augenrules --check` 출력이 "No change"이면 PASS | 저 (synthesizeExpectExact) | 95% | high |
| 4.4.3.3 (부분) | IPv6 disabled 분기만 자동, iptables -L -v -n verify는 site policy → 분리 | 중 | 70% | medium |

**잠재 변환률 향상**: 4건 확정(1.2.2.1, 3.1.1, 4.3.3, 6.2.3.21) → 312 기준 **+1.3%p** (83.3% → 84.6%). 4.4.3.3 부분 cover 시 +1.6%p.

### 6.2 manual 유지 16건

C1·C2·C4·C5 모두 운영자 site policy 또는 정성적 review가 본질 — 자동 변환 시 **false PASS** 위험 (예: 4.2.5의 `ufw status numbered`는 출력 형태는 결정론적이지만 "match site policy" 판단은 환경 의존).

### 6.3 권장 — 본 문서는 manual fixture 트랙만 마감, 자동 후보 4건은 별 epic

자동 변환 가능 4건(1.2.2.1, 3.1.1, 4.3.3, 6.2.3.21)은 NoMarker 31 design doc의 차기 epic 후보(E-1/E-2/E-3)에 합쳐서 추가 epic E-4로 분리 추진 권장 (D-MAN-5).

## 7. Stage 분해 — 운영자 fixture 작성

§4 우선순위 + §6 자동 변환 후보 분리 적용 후 manual 트랙 17건(21 - 4 자동 후보).

### 7.1 manual 트랙 17건 우선순위 재집계

| 우선순위 | 갯수 | IDs |
|---|---|---|
| **high** | **2** | 1.1.1.10, 5.3.3.2.3, 5.4.1.2 (3건 — 정정) |
| medium | 9 | 1.2.1.1, 2.1.22, 4.2.5, 4.3.7, 4.4.2.3, 4.4.3.3 (잔여), 6.1.1.2, 6.1.1.3, 7.1.13 |
| low | 5 | 1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8 |

(자동 후보 4건 제외: 1.2.2.1, 3.1.1, 4.3.3, 6.2.3.21. high에서 1.2.2.1·4.3.3·6.2.3.21 빠짐. high 정정: 1.1.1.10, 5.3.3.2.3, 5.4.1.2 — **3건**. 합 3+9+5 = 17.)

### 7.2 Stage 1 — high 3건 (~0.3일)

대상: 1.1.1.10, 5.3.3.2.3, 5.4.1.2.

작업:
- §5.1 (C1) 또는 §5.2 (C2) template 적용
- 각 ID별 auditCommand baseline 인용 + prompt 한국어 작성
- selftest PASS/FAIL 2 fixture씩 = 6 fixture
- pack manifest hash 갱신 (sign-pack 재실행)

산출:
- `packs/cis-ubuntu-2404/checks/manual/1.1.1.10.yaml` 외 2건
- `packs/cis-ubuntu-2404/selftest/manual/1.1.1.10/{pass,fail}.yaml` 외 4건
- `packs/cis-ubuntu-2404/manifest.json` (hash 갱신)
- `docs/operations/cis-ubuntu-2404-manual.md` (운영자 가이드 신규 — high 3건만)

### 7.3 Stage 2 — medium 9건 (~0.5일)

대상: 1.2.1.1, 2.1.22, 4.2.5, 4.3.7, 4.4.2.3, 4.4.3.3 (잔여), 6.1.1.2, 6.1.1.3, 7.1.13.

C1 9건 + C4 1건 (6.1.1.2). 작업량 Stage 1 × 3 (9건 ≈ 3×high). selftest 18 fixture.

### 7.4 Stage 3 — low 5건 (~0.5일, 후속 Phase에 보류)

대상: 1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8.

niche 환경 (중앙 로그서버, GPG key 정책) 한정 — first paying customer 또는 enterprise 환경 요청 시 우선순위 재평가. 본 트랙 default는 **연기** (D-MAN-6).

### 7.5 총 추정 시간

- design doc(본 문서): 0.2일 (현재 진행 중)
- Stage 1 (high 3건): 0.3일
- Stage 2 (medium 9건): 0.5일
- Stage 3 (low 5건): 0.5일 — **연기 권장**
- 자동 변환 후보 4건 → 별 epic E-4: NoMarker design doc 트랙으로 이관

**Stage 1+2 누적: 1.0일** (Stage 3 보류 시).

## 8. 결정 항목 (D-MAN-N)

### D-MAN-1 — manual fixture 디렉토리 구조

**옵션**:
1. (권장 default) `packs/cis-ubuntu-2404/checks/manual/<id>.yaml` + `selftest/manual/<id>/{pass,fail}.yaml` — 자동 변환과 분리, convert overwrite 위험 0
2. `packs/cis-ubuntu-2404/checks/<id>.yaml` 단일 디렉토리 + manual flag — convert overwrite 위험, manual fixture 보호 분기 필요
3. 별 pack `cis-ubuntu-2404-manual` — 분리 명확, manifest 2개 관리 부담

**권장 default: 1**. 이유: pack-tools convert가 `manual/` 서브디렉토리 skip 분기 1줄로 충분, 운영자 fixture와 자동 변환 yaml의 lifecycle 완전 분리.

### D-MAN-2 — manual fixture selftest harness 호환

**옵션**:
1. (권장 default) selftest harness에 manual fixture 인식 분기 추가 — `evaluationRule.op == "manual"` 시 expectedVerdict 직접 비교 (PASS/FAIL/REVIEW 3분기)
2. manual fixture는 selftest 미실행 — 운영자 fixture 작성 품질 보장 안 됨, 회귀 위험 ↑
3. 별 manual selftest CLI — 코드 분리되지만 통합 CI 부담

**권장 default: 1**. selftest harness가 PASS/FAIL/REVIEW 3-state 검증으로 확장.

### D-MAN-3 — `evaluationRule.op = "manual"` schema

**옵션**:
1. (권장 default) `op: "manual"` + `prompt: <string>` + `defaultVerdict: <pass|fail|review>` — 단순 schema, 기존 op enum에 추가
2. `op: "review"` + nested `criteria: [...]` 다단계 검증 — schema 복잡, manual fixture 작성 부담 ↑
3. 별 schema `manualEvaluation` — top-level 분기, 기존 evaluationRule 코드 영향 없으나 schema 이중화

**권장 default: 1**. CIS audit text의 review 절을 prompt 1개로 표현 가능, 기존 op enum 확장 비용 최소.

### D-MAN-4 — C4 (script self-emits) 자동 변환 vs manual fixture

**옵션**:
1. (권장 default) **manual 유지** — `** PASS **` 마커는 매칭하지만 `** REVIEW **` / `** FAIL **` 분기는 운영자 정책 의존 → manual fixture로 prompt 표시
2. 자동 변환 — `** PASS **` 매칭 시 PASS, `** FAIL **` 매칭 시 FAIL, `** REVIEW **`만 manual fallback

**권장 default: 1**. 이유: 6.1.1.2·6.1.2.1.2의 script가 emit하는 `** REVIEW **`는 site policy 의존 → 자동 PASS 부여 시 false positive 위험. 자동 변환 ROI ↓ (2건 +0.6%p)이고 안전성 ↓.

### D-MAN-5 — 자동 변환 후보 4건의 처리

**옵션**:
1. (권장 default) 별 epic E-4로 분리 — NoMarker 31 design doc 트랙에 합류, manual 트랙은 17건만
2. 본 트랙 Stage 0에 통합 — Stage 분해 복잡, 자동/manual 코드 동시 작업
3. manual 유지 (자동 변환 포기) — 변환률 +1.3%p 잠재 가치 손실

**권장 default: 1**. NoMarker design doc §5의 E-1 권장 epic 완료 후 E-4(자동 변환 후보 4건)로 자연스럽게 연속.

### D-MAN-6 — Stage 3 (low 5건) 진행 시점

**옵션**:
1. (권장 default) **연기** — first paying customer 또는 enterprise 요청 시 재평가, 본 Phase 0에는 미진행
2. Stage 2 직후 진행 — manual 트랙 완성도 ↑이나 0.5일 추가
3. 영구 미진행 — niche 환경 운영자가 직접 작성하도록 가이드만 제공

**권장 default: 1**. 1.0일 누적(Stage 1+2)으로 high+medium 14건 cover 시 일반 fleet 충분, low 5건은 niche.

### D-MAN-7 — 운영자 가이드 문서 위치

**옵션**:
1. (권장 default) `docs/operations/cis-ubuntu-2404-manual.md` — degraded 가이드와 동일 디렉토리, 운영자 reference
2. pack 안 `packs/cis-ubuntu-2404/MANUAL.md` — pack 자체에 포함, 배포 시 동봉
3. README.md 안 섹션 추가 — 분량 부담

**권장 default: 1**. degraded 가이드와 동일 패턴 유지, pack 배포 시 별도 문서 동봉은 packaging 단계에서 결정.

## 9. 회귀 위험 / 운영 고려

- **pack manifest hash 변경** — Stage 1 (3 checks + 6 selftest = 9 file) → R30-3 sign-pack 워크플로우 재실행 필요. Stage 2 27 file 추가, manifest hash 재갱신.
- **selftest harness 호환성** — D-MAN-2 옵션 1 채택 시 harness 코드에 PASS/FAIL/REVIEW 3-state 확장 필요. Stage 1 직전 unit test 추가.
- **pack-tools convert overwrite 위험** — D-MAN-1 옵션 1로 디렉토리 분리 시 convert가 `manual/` skip 분기 1줄 추가. 미적용 시 자동 변환이 manual fixture 덮어씀 → **Stage 1 진입 전 convert 분기 패치 필수**.
- **selftest CI 시간** — manual fixture 17건 × 2 fixture = 34 추가 케이스. 기존 260 자동 케이스에 +13% 부담 → CI 워크플로우 timeout 재확인.
- **운영자 prompt 한국어/영어 결정** — D-MAN-3 schema에 `prompt`만 single-language 가정 — 다국어 지원은 Phase 1 i18n 트랙에서 재평가 (본 트랙은 한국어 default).
- **자동 변환 후보 4건 epic E-4 conflict** — NoMarker 31 design doc의 E-1 epic이 진행 중에 본 트랙 자동 후보 4건 합성기 추가 시 dispatch 우선순위 충돌 가능 — Stage 1 진입 전 NoMarker E-1 epic의 dispatch chain과 cross-reference 필수.
- **defaultVerdict의 의미** — `pass`/`fail`/`review` 3-state가 보고서·dashboard 집계에서 어떻게 표시되는지 결정 필요 — `review`를 fail-equivalent로 처리할지, 별 카테고리로 분리할지는 reporting 트랙에서 결정 (본 트랙 범위 외).

## 10. 참조

- 직전 design doc(NoMarker 31 자동 변환): `docs/design/notes/cis-nomarker-31-analysis.md`
- 직전 design doc(D epic, 6.2.3.x auditd 21건): `docs/design/notes/cis-6-2-3-auditd-design.md`
- nrobotcheck baseline (audit text 출처): `D:\robot\dev\nrobotcheck\resources\baselines\cis_ubuntu_2404_benchmark.json`
- pack 출력: `packs/cis-ubuntu-2404/checks/*.yaml` (312 checks)
- degraded 가이드: `docs/operations/cis-ubuntu-2404-degraded.md`
- 변환기 코드: `cmd/pack-tools/converter/cis.go`
- selftest 자동 생성: `cmd/pack-tools/converter/selftest.go::GenerateSelfTestSkeletons`
- 설계 원칙: `docs/design/01-principles.md` §1(결정론) · §6(결정론적 fallback) · §10(프라이버시 default)
- 메모리 패턴:
  - `feedback_design_doc_first.md` (1일+ 작업은 design doc 우선 — 본 트랙 1.0일 누적이라 적용)
  - `feedback_design_doc_conservative.md` (보수적 추정 — Manual 일괄 비대상 분류 → §6 재평가로 4건 자동 후보 식별)
