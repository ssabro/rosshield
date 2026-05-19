# CIS Ubuntu 24.04 customer-supplied policy 가이드

> **대상**: CIS Ubuntu 24.04 pack(`rosshield-cis-ubuntu-2404-1.0.0`) 사용 customer 운영자.
> **상태**: v0.6.2+ — CIS Ubuntu 24.04 cover 100% 도달(자동 289 + `op: manual` 12 + env-skip 5 = 301/301). 본 문서는 잔여 5건의 customer-supplied env var 정의 절차를 다룹니다.
> **선행 문서**: [`docs/operations/cis-ubuntu-2404-manual.md`](../operations/cis-ubuntu-2404-manual.md) (기존 12 `op: manual` fixture 운영 가이드), [`docs/design/notes/cis-manual-fixture-21-design.md`](../design/notes/cis-manual-fixture-21-design.md) (env-var skip 패턴 설계).

---

## 1. 배경

CIS Ubuntu 24.04 Benchmark는 301개 check 중 **17건이 site policy 의존** — 자동 PASS/FAIL 판정이 불가능합니다. rosshield는 이를 두 패턴으로 cover합니다.

| 패턴 | 수 | 설명 |
|---|---|---|
| `op: manual` 운영자 prompt | 12 | 운영자가 audit text를 보고 직접 PASS/FAIL/REVIEW 판정 (선행 doc 참조) |
| **env-var skip 패턴** | **5** | customer-supplied env var 정의 시 audit cmd가 자동 PASS/FAIL marker emit, 미정의 시 INDETERMINATE(review) |

본 문서는 **env-var skip 패턴 5건**의 정의 절차를 다룹니다. 이 5건은 v0.4.x ~ v0.6.x 사이 placeholder(`<degraded — Phase 2 fixture required>`)로 false-FAIL을 발생시켜 왔으나, v0.6.2+부터 manual fixture로 정식 cover됩니다.

---

## 2. 잔여 5건 일람

각 check는 `checks/manual/{ID}.yaml`에 정의되며, `op: manual` 패턴(audit cmd는 `"true"`, evaluator `ManualNode`가 항상 `INDETERMINATE` outcome 반환)으로 false-FAIL을 회피합니다. 운영자는 prompt 안내에 따라 env var를 정의한 뒤 별 cmd(§4 참조)로 검증합니다.

| CIS ID | 제목 | env var | 값 형식 | 환경 의존 항목 |
|---|---|---|---|---|
| **1.2.1.2** | apt 저장소 site policy | `ROSSHIELD_CIS_1_2_1_2_POLICY` | 쉼표 구분 허용 도메인 목록 | 공식 vs 사내 mirror, ROS2·NVIDIA 등 vendor repo |
| **6.1.2.1.2** | systemd-journal-upload 인증 | `ROSSHIELD_CIS_6_1_2_1_2_POLICY` | `<URL>,<cert-path>` | 원격 log host URL + 서명 인증서 경로 |
| **6.1.3.5** | rsyslog 필수 logging 규칙 | `ROSSHIELD_CIS_6_1_3_5_POLICY` | 쉼표 구분 정규식 패턴 | site facility/priority별 destination 정책 |
| **6.1.3.6** | rsyslog 원격 forward host | `ROSSHIELD_CIS_6_1_3_6_POLICY` | `<loghost>:<port>` | 중앙 로그 관리 서버 도메인·포트 |
| **6.1.3.8** | logrotate 회전·보존 정책 | `ROSSHIELD_CIS_6_1_3_8_POLICY` | `<frequency>:<rotate-count>` | site archival policy (주기·보존 개수) |

**env var 명명 규칙**: `ROSSHIELD_CIS_<ID_NORM>_POLICY` — `<ID_NORM>`은 CIS ID의 `.` 을 `_` 로 정규화(env var 명에 `.` invalid). CLAUDE.md 코드 네임스페이스 `rosshield` 일관 prefix (D-CM-2 결정, 2026-05-19).

---

## 3. site policy 결정 가이드 (check별 권장값)

각 check의 권장 정책 결정 가이드입니다. customer 환경별 조정 필요.

### 3.1 CIS 1.2.1.2 — apt 저장소

**권장값**: `archive.ubuntu.com,security.ubuntu.com` (Ubuntu 공식 only)
- ROS2 fleet: `archive.ubuntu.com,security.ubuntu.com,packages.ros.org`
- NVIDIA GPU 워크로드: 위 + `developer.download.nvidia.com`
- 사내 mirror 사용 시: 위를 사내 도메인으로 치환 (`mirror.acme.example` 등)

**risk**: 미승인 PPA(예: launchpad ppa)로 손상된 패키지 도입.

### 3.2 CIS 6.1.2.1.2 — systemd-journal-upload

**권장값**: `https://logs.example.com:443,/etc/ssl/certs/journal-upload.pem`
- 원격 log host URL은 HTTPS + 사내 PKI 인증서 권장
- cert 경로는 read-only owner-only(0400) 권장
- client-side logging이 **rsyslog** 면 본 check N/A — env var 미정의로 두고 운영 가이드에 명시

**risk**: 인증서 부재 시 log 전송 채널이 MITM 위험.

### 3.3 CIS 6.1.3.5 — rsyslog 필수 logging 규칙

**권장값**: `auth.*,authpriv.*,\\*\\.crit` (최소 보안 로그)
- 정밀 정책: `auth.*,authpriv.*,\\*\\.crit,mail.err,cron.\\*`
- 정규식 패턴은 grep -E로 해석 — site 패턴이 복잡하면 다중 패턴 권장

**risk**: 필수 로그 누락 시 사고 대응 자료 부재.

### 3.4 CIS 6.1.3.6 — rsyslog 원격 forward

**권장값**: `loghost.example.com:514` (TCP 권장 — audit cmd에서 `@@` 패턴 자동 검사)
- HA: 다중 host는 별 drop-in 파일로 정의, env var는 1차 host만 명시
- 도메인 부재 환경(IP 직접): `10.10.10.10:514`

**risk**: 원격 백업 부재 시 local log 변조·삭제 후 사고 추적 불가.

### 3.5 CIS 6.1.3.8 — logrotate

**권장값**: `weekly:7` (주간 회전 + 7주 보존)
- 컴플라이언스 요건 1년 보존: `daily:365`
- 디스크 절약: `weekly:4`
- `compress`·`delaycompress` 옵션은 본 check 범위 외 — fixGuidance 참조

**risk**: 회전 미적용 시 디스크 풀 또는 비대 log 검색 비용 ↑.

---

## 4. 적용 절차

### 4.1 .env 파일 (단일 인스턴스)

```bash
# /etc/rosshield/cis-policy.env
ROSSHIELD_CIS_1_2_1_2_POLICY="archive.ubuntu.com,security.ubuntu.com,packages.ros.org"
ROSSHIELD_CIS_6_1_2_1_2_POLICY="https://logs.example.com:443,/etc/ssl/certs/journal-upload.pem"
ROSSHIELD_CIS_6_1_3_5_POLICY="auth.*,authpriv.*,\\*\\.crit"
ROSSHIELD_CIS_6_1_3_6_POLICY="loghost.example.com:514"
ROSSHIELD_CIS_6_1_3_8_POLICY="weekly:7"
```

권한: `chmod 0640 /etc/rosshield/cis-policy.env` + `chown root:rosshield`.

### 4.2 systemd EnvironmentFile (rosshield-server 단위 적용)

```ini
# /etc/systemd/system/rosshield-server.service.d/override.conf
[Service]
EnvironmentFile=/etc/rosshield/cis-policy.env
```

`systemctl daemon-reload && systemctl restart rosshield-server` 후 적용.

### 4.3 fleet 분산 (secret manager 권장)

다수 robot에 동일 정책 배포 시:

- **HashiCorp Vault**: `vault kv put secret/rosshield/cis-policy @cis-policy.json` → vault agent로 각 노드 inject
- **AWS Systems Manager Parameter Store**: SecureString 5개 등록 + `aws ssm get-parameter` 으로 robot에 fetch
- **Ansible Vault**: `ansible-vault encrypt cis-policy.env` → playbook으로 push
- **Kubernetes Secret**: `kubectl create secret generic cis-policy --from-env-file=cis-policy.env` → pod env로 mount

**금기**: env var 값을 git에 plain commit하지 않음 — 5건 모두 site 내부 인프라 정보(URL·도메인) 포함.

### 4.4 검증 (운영자 직접 cmd 실행)

본 5건은 `op: manual` 패턴이므로 fleet scan은 항상 INDETERMINATE 결과를 emit합니다. 운영자가 env var 정의 후 별 cmd로 site policy 적용 여부를 검증합니다.

```bash
# 예: CIS 1.2.1.2 — apt 저장소 allow-list 매칭 검증
export ROSSHIELD_CIS_1_2_1_2_POLICY="archive.ubuntu.com,security.ubuntu.com"
allowed=$(printf "%s" "$ROSSHIELD_CIS_1_2_1_2_POLICY" | tr "," "|")
apt-cache policy | grep -E "$allowed" >/dev/null && echo "PASS — allow-list 매칭" \
  || echo "FAIL — site policy 외 항목 검출"

# 예: CIS 6.1.3.6 — rsyslog forward host 검증
export ROSSHIELD_CIS_6_1_3_6_POLICY="loghost.example.com:514"
host=$(printf "%s" "$ROSSHIELD_CIS_6_1_3_6_POLICY" | cut -d: -f1)
grep -qE "(@@?$host|target=\"$host\")" /etc/rsyslog.conf /etc/rsyslog.d/*.conf \
  && echo "PASS" || echo "FAIL"

# 예: CIS 6.1.3.8 — logrotate 주기/개수 검증
export ROSSHIELD_CIS_6_1_3_8_POLICY="weekly:7"
freq=$(printf "%s" "$ROSSHIELD_CIS_6_1_3_8_POLICY" | cut -d: -f1)
cnt=$(printf "%s" "$ROSSHIELD_CIS_6_1_3_8_POLICY" | cut -d: -f2)
grep -qE "^[[:space:]]*$freq" /etc/logrotate.conf /etc/logrotate.d/*.conf \
  && grep -qE "^[[:space:]]*rotate[[:space:]]+$cnt" /etc/logrotate.conf /etc/logrotate.d/*.conf \
  && echo "PASS — $freq/$cnt 적용" || echo "FAIL — 주기/개수 불일치"
```

운영자 검증 결과(PASS/FAIL/REVIEW)를 audit report 본문에 첨부 — Phase 8+ LLM advisor 트랙에서 audit cmd 자동화 및 자동 PASS/FAIL 승격 예정 (D-CM-4).

---

## 5. audit report 영향

본 5건의 결과는 audit report `Manual review required` 섹션에 분류됩니다 — `op: manual` 12 fixture와 동일 평가 분기(`ManualNode`, defaultVerdict=review). false-FAIL 회피와 customer 정성 검토 흐름 통합이 핵심입니다.

- **fleet scan 결과**: 항상 INDETERMINATE 반환 — false-FAIL 회피.
- **운영자 검증 결과**: §4.4 별 cmd 실행 결과(PASS/FAIL)를 audit report 본문에 trace로 첨부, 운영자가 최종 판정.
- **자동화 단계(Phase 8+)**: LLM advisor 옵트인 트랙에서 audit cmd를 env-var driven bash로 진화 + 자동 PASS/FAIL 승격 가능 — 본 v0.6.2 단계는 보수적 운영자 정성 검토 유지 (D-CM-4 결정).

---

## 6. 트러블슈팅

| 증상 | 원인 | 해결 |
|---|---|---|
| audit cmd가 `** INDETERMINATE **` 만 emit (env 정의했는데도) | systemd 단위에 EnvironmentFile 미반영 | `systemctl show rosshield-server -p Environment` 확인 후 daemon-reload + restart |
| `** FAIL **` 항상 출력 (PASS 예상 환경) | env var 값 형식 오류(쉼표 vs 콜론 혼동 등) | §2 표의 "값 형식" 컬럼 재확인 |
| 일부 robot만 INDETERMINATE | fleet 분산 시 일부 노드에 env var 미배포 | secret manager 동기화 상태 점검 |
| audit cmd timeout | env var 값에 정규식 backreference 등 catastrophic pattern | 패턴을 단순 substring으로 분해 후 multi-pattern으로 |

---

## 7. 변경 이력

| 날짜 | 변경 | 작성자 |
|---|---|---|
| 2026-05-19 | 초판 — env-var skip 패턴 5건 정식 cover (CIS Ubuntu 24.04 100% 도달) | rosshield core team |

---

## 8. 참조

- 설계 문서: [`docs/design/notes/cis-manual-fixture-21-design.md`](../design/notes/cis-manual-fixture-21-design.md)
- 선행 운영 가이드 (12 `op: manual`): [`docs/operations/cis-ubuntu-2404-manual.md`](../operations/cis-ubuntu-2404-manual.md)
- check yaml: `packs/cis-ubuntu-2404/checks/manual/{1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8}.yaml`
- selftest fixture: `packs/cis-ubuntu-2404/selftest/manual/{1.2.1.2, 6.1.2.1.2, 6.1.3.5, 6.1.3.6, 6.1.3.8}.yaml`
- evaluator: `internal/domain/benchmark/eval.go` (ManualNode + defaultVerdict 패턴)
- 설계 원칙: [`docs/design/01-principles.md`](../design/01-principles.md) §1(결정론) · §4(멀티테넌시) · §6(결정론적 fallback) · §10(프라이버시 default)
