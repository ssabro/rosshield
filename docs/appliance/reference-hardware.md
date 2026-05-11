# 레퍼런스 하드웨어 검증 (E36)

> Phase 5 어플라이언스 트랙 — 흔한 mini-PC 2종(NUC + OptiPlex)에 대한 hands-on burn-in 절차와 결과 표.
>
> **상태**: scaffolding (측정값 TBD — 사용자가 hands-on으로 채움)
>
> **연관 epic**: E33 snap 빌드(`616403c`) · E34 TPM 봉인(`e96937c`) · E35 A/B OTA(`c0f8a4b`)
>
> **연관 결정**: R40-1 core22 / R40-2 swtpm CI / R40-4 Onprem 1차 SKU

---

## 1. 개요

### 1.1 epic 목적

rosshield는 **자체 하드웨어 제조를 비목표로 한다** (CLAUDE.md "하지 말 것" 5번 + design `11-tech-stack-and-roadmap.md` §11.9). 그렇다면 customer가 어떤 박스에 우리 snap을 올리는지 — 그리고 그 박스에서 install/scan/OTA/TPM 봉인이 모두 의도대로 동작하는지 — 를 사전에 보장해야 한다.

E36은 **시장에서 가장 흔한 mini-PC 2종**(Intel NUC + Dell OptiPlex)을 우리가 직접 사서 burn-in 측정하고, 결과를 본 문서에 게시함으로써:

1. customer가 SKU를 결정할 때 참고할 수 있는 **신뢰할 수 있는 호환성 기준선**을 제공한다.
2. swtpm CI에서는 잡히지 않는 **실 TPM 칩 / 실 Secure Boot / 실 부팅 사이클**의 회귀를 사전에 발견한다.
3. R40-4 Onprem SKU 첫 출하 직전 **사용자 수용 시험(UAT) 체크리스트**의 근거가 된다.

### 1.2 검증 대상

- **amd64 2 모델**: Intel NUC 13 Pro + Dell OptiPlex 7010 SFF
- arm64(Raspberry Pi 5 등)는 본 epic 범위 외 — Phase 5 후속 stage에서 분리 처리 (§8 후속·잔여 참조)

### 1.3 검증 항목 5종

| # | 항목 | 핵심 질문 |
|---|---|---|
| 1 | install 시간 | UC22 첫 부팅부터 `/healthz` 200까지 30분 이내인가 |
| 2 | 메모리 footprint | idle RSS가 200MB 이하인가, 8h burn-in 동안 누수 없는가 |
| 3 | idle CPU | 1시간 idle 평균 CPU%가 5% 이하인가 |
| 4 | full scan latency | CIS Ubuntu pack(47 checks) 1회 scan이 60초 이내인가 |
| 5 | TPM·Secure Boot·snap 통합 | seal/unseal/A·B refresh가 실제 칩에서 정상 동작하는가 |

### 1.4 Phase 5 Exit 기준

- [ ] 2 모델 모두 30분 install 통과
- [ ] 2 모델 모두 8h burn-in 통과 (메모리 누수 ≤ 50MB · 크래시 0회)
- [ ] 결과 표(§5)가 본 문서에 게시됨 — TBD 0건
- [ ] 트러블슈팅(§6) 항목이 hands-on에서 발견된 실 사례로 보강됨

---

## 2. 하드웨어 매트릭스

### 2.1 1차 검증 대상 (필수)

| 모델 | CPU | RAM | TPM 2.0 | Secure Boot | 가격대 | 비고 |
|---|---|---|---|---|---|---|
| **Intel NUC 13 Pro (NUC13ANHi5)** | i5-1340P (12-core, 4P+8E) | 16GB DDR4-3200 | dTPM 2.0 (Infineon SLB9670) | UEFI 지원 | ~$650 | 일반 customer 표준, BareBone + RAM/SSD 별매 |
| **Dell OptiPlex 7010 SFF** | i5-13500 (14-core, 6P+8E) | 16GB DDR4-3200 | fTPM 2.0 (Intel PTT) | UEFI 지원 | ~$700 | enterprise 채널 익숙, 5년 보증 옵션 |

### 2.2 2차 옵션 (cost·perf 양극단 검증, 여력 시)

| 모델 | CPU | RAM | TPM 2.0 | Secure Boot | 가격대 | 비고 |
|---|---|---|---|---|---|---|
| Intel NUC 12 Pro (NUC12WSHi5) | i5-1235U (10-core) | 16GB DDR4 | dTPM 2.0 | UEFI 지원 | ~$500 | budget — 8h burn-in idle 검증 위주 |
| ASRock Industrial NUC BOX-1260P | i7-1260P | 32GB DDR4 | dTPM 2.0 (Nuvoton) | UEFI 지원 | ~$900 | high-end — full scan 동시 4 fleet 시뮬레이션 |

### 2.3 모델별 보충 설명

#### Intel NUC 13 Pro (NUC13ANHi5)

Intel이 직접 설계·판매하는 mini-PC의 13세대 모델. dTPM(discrete TPM) 칩이 메인보드에 탑재되어 fTPM(firmware TPM) 대비 BIOS update에 덜 민감하다. **TPM PCR 안정성을 우선시하는 customer**(예: 금융·의료)에 권장. BareBone 형태이므로 RAM(16GB SODIMM) + NVMe SSD(256GB)를 별도 구매해야 한다.

- 구매 채널: Amazon US, Newegg, 국내 컴퓨존·다나와 *(채널별 가격·재고는 검증 시점에 채움)*
- 단종 리스크: Intel NUC 사업부가 ASUS로 이전(2023). 후속 제품군은 ASUS NUC로 이어짐 — 본 검증 후속 stage에서 ASUS NUC 14 Pro도 동일 매트릭스로 추가 검토.

#### Dell OptiPlex 7010 SFF

enterprise 시장에서 수십 년간 표준이었던 Dell의 비즈니스 데스크톱. fTPM(Intel PTT — Platform Trust Technology) 사용. **enterprise IT 부서가 이미 조달 프로세스를 갖고 있는 모델**이라 customer 측 PoC 진입 장벽이 가장 낮다.

- 구매 채널: Dell 직접(B2B 견적), Newegg Business, 국내 Dell 파트너
- 5년 ProSupport 옵션: ~$120 추가 — Onprem SKU 번들에 포함 검토
- 주의: PTT는 BIOS update 시 PCR 값이 바뀔 수 있음 → §6 트러블슈팅 "TPM unseal 실패" 항목 참조

#### Intel NUC 12 Pro (옵션, budget)

가격을 우선시하는 customer용. CPU 세대만 다르고 TPM/Secure Boot 동일.

#### ASRock Industrial NUC BOX-1260P (옵션, high-end)

산업용 grade(0~50°C 작동 온도, 24/7 정격) — 공장·물류 창고 등 환경이 거친 customer용.

---

## 3. 측정 항목 정의

### 3.1 측정 항목 표

| 항목 | 측정 방법 | 합격 기준 | 도구 |
|---|---|---|---|
| **Install 시간** | UC22 첫 부팅 ~ `snap install rosshield` ~ `/healthz` 200까지 | ≤ 30분 | stopwatch + `journalctl --since` |
| **Cold boot 시간** | `reboot` 입력 ~ `/healthz` 200까지 | ≤ 60s | `systemd-analyze` + curl |
| **메모리 footprint (idle)** | 부팅 5분 후 RSS | ≤ 200MB | `systemctl status snap.rosshield.server` (Memory:) |
| **Idle CPU** | 1시간 idle 평균 CPU% | ≤ 5% | `sar -u 1 3600` |
| **Full scan latency** | CIS Ubuntu pack scan 1회(47 checks) | ≤ 60s | `time rosshield-cli scan run --pack=cis-ubuntu` |
| **8h burn-in: 메모리 누수** | 8h 동안 RSS 증가량 | ≤ 50MB 증가 | `sar -r 60 480` + grep snap.rosshield.server |
| **8h burn-in: 크래시** | service restart 횟수 | 0회 restart | `systemctl status snap.rosshield.server.service` (Restart Counter) |
| **OTA 시간** | `snap refresh` ~ post-refresh hook ~ `/healthz` OK | ≤ 90s | E35 hook log + journalctl |
| **TPM seal/unseal** | `--keystore=tpm` 첫 부팅 (seal) → reboot → 두 번째 부팅 (unseal) | 정상 동작 + log에 `keystore=tpm` 명시 | `snap logs rosshield.server` |
| **Secure Boot 검증** | `mokutil --sb-state` | 출력에 `SecureBoot enabled` | `mokutil` |

### 3.2 측정 환경 표준

- **네트워크**: 유선 1Gbps, DHCP, 외부 인터넷 가능 (snap store refresh 위함). 측정 중 다른 트래픽 없음.
- **전원**: AC 직결 (UPS 거치지 않음 — 전력 측정 정확도 위함). 절전 모드 disable.
- **온도**: 실온 22±3°C, 상부 통풍 확보(15cm 이상).
- **storage**: 모든 모델 NVMe SSD 256GB(가능하면 동일 모델 — Samsung 980 등)로 통일하여 disk I/O 변수 제거.

### 3.3 합격 기준 근거

- **30분 install**: UAT 1세션(대개 1시간) 안에 install + 첫 demo가 모두 끝나야 한다. 30분 install + 30분 demo 가 상한.
- **200MB RSS**: customer 환경 기준으로 mini-PC 16GB RAM 중 1.25% 미만이어야 "백그라운드 운영" 인상.
- **5% idle CPU**: 같은 박스에 customer가 추가 워크로드(예: NVR, 게이트웨이)를 올릴 여지 확보.
- **60s full scan**: customer 운영자가 "한 잔 마시고 오니 끝났다" 체감 — UX 임계.
- **90s OTA**: snap refresh window가 일반적으로 10분 단위로 묶임 → 90s면 한 fleet 100대를 1 window에 처리 가능.

---

## 4. 측정 절차

### 4.1 사전 준비

1. 하드웨어 unboxing — 외관 손상 + 모델 번호 + S/N 사진 기록.
2. Ubuntu Core 22 install media(USB) 준비 — `dd` 또는 `Rufus`로 공식 이미지 굽기.
3. 네트워크: DHCP 할당되는 유선 LAN 포트 연결 + ssh 공개키(`~/.ssh/id_ed25519.pub`) 준비.
4. BIOS/UEFI 진입(F2 또는 Del):
   - **Secure Boot**: Enabled
   - **TPM 2.0**: Enabled (NUC는 dTPM, OptiPlex는 PTT)
   - **Boot order**: USB 우선
   - 변경사항 저장 후 재부팅.

### 4.2 install (30분 측정)

| 단계 | 명령/조작 | 시각 기록 |
|---|---|---|
| 1 | UC22 install media 부팅 | T0 |
| 2 | 언어·키보드·네트워크·ssh 키(`ssabro_k@naver.com` Ubuntu One 계정) 입력 | — |
| 3 | install 완료 후 자동 reboot, ssh 접속 가능 시점 | T1 |
| 4 | `snap install rosshield_<version>_amd64.snap --dangerous` (dev 채널) 또는 `snap install rosshield --channel=stable` (정식) | T2 |
| 5 | `snap services` — `snap.rosshield.server` enabled+active 확인 | — |
| 6 | `curl -s http://localhost:8080/healthz` → HTTP 200 | T3 |

**Install 시간 = T3 − T0**, 합격 기준 ≤ 30분.

각 단계의 `journalctl -u snap.rosshield.server.service --since "5 minutes ago"` 로그를 별도 파일(`install-<model>-<date>.log`)로 보관.

### 4.3 1시간 burn-in (idle 메모리·CPU)

```bash
# 측정 시작 시각 기록
date -u +%s > /tmp/burnin-start

# sar 1초 간격 1시간 idle CPU%
sar -u 1 3600 > /tmp/sar-cpu-1h.log &

# 1분 간격 RSS
( for i in $(seq 1 60); do
    date -u +%s
    systemctl show snap.rosshield.server.service -p MemoryCurrent
    sleep 60
  done ) > /tmp/rss-1h.log &

wait
```

분석:
- `awk '{sum+=$3; n++} END {print sum/n}' /tmp/sar-cpu-1h.log` → 평균 idle CPU%
- `/tmp/rss-1h.log` 의 MemoryCurrent 값에서 평균/최대/최소 산출 → 결과 표(§5)에 기록

### 4.4 8시간 burn-in

1시간 burn-in을 8회 반복(총 8h). 동시에 5분 간격으로 full scan 수행 → scan latency 분포 확보.

```bash
# 8h RSS 추적 (1분 간격)
sar -r 60 480 > /tmp/sar-mem-8h.log &

# 5분 간격 full scan (8h × 12회/h = 96회)
( for i in $(seq 1 96); do
    /usr/bin/time -v snap run rosshield-cli scan run --pack=cis-ubuntu \
      > /tmp/scan-$i.log 2>&1
    sleep 300
  done ) &

wait
```

검증:
- **메모리 누수**: `/tmp/sar-mem-8h.log` 의 t=0 RSS vs t=8h RSS 차이 ≤ 50MB
- **크래시**: `systemctl show snap.rosshield.server.service -p NRestarts` 가 0
- **scan latency**: 96회 scan 중 p50 / p95 / max 산출 — p95 ≤ 60s 합격

### 4.5 OTA 검증

#### 4.5.1 정상 refresh

```bash
# 새 revision install
sudo snap refresh rosshield --revision=<new>

# E35 post-refresh hook 로그 확인
sudo snap logs rosshield -n 100 | grep post-refresh

# /healthz 200 확인
curl -s http://localhost:8080/healthz
```

OTA 시간 = `snap refresh` 시작 ~ `/healthz` 200 까지. 합격 기준 ≤ 90s.

#### 4.5.2 자동 rollback 시나리오

의도적으로 깨진 build(예: healthz 핸들러 panic)를 dev 채널에 publish 후:

```bash
sudo snap refresh rosshield --channel=edge
# post-refresh hook 의 healthz check 실패 → snapd 가 자동 revert
sudo snap logs rosshield -n 200 | grep -E "(post-refresh failed|reverting)"
sudo snap list rosshield  # 이전 revision 으로 복귀했는지 확인
```

검증:
- `snap list rosshield` 의 Rev 값이 refresh 직전 값과 동일
- `/healthz` 200 정상 동작
- snapd journal에 `failure-mode=revert` 기록

### 4.6 TPM 검증

#### 4.6.1 첫 부팅 — seal

```bash
sudo snap set rosshield keystore=tpm
sudo snap restart rosshield.server
sudo snap logs rosshield.server -n 50 | grep -E "(keystore=tpm|sealed)"
```

검증:
- 로그에 `keystore=tpm` 명시
- E34 의 TPM seal 메시지(예: `master key sealed to PCR 0,2,4,7`) 출력

#### 4.6.2 reboot — unseal

```bash
sudo reboot
# 부팅 후
sudo snap logs rosshield.server -n 50 | grep -E "(unsealed|keystore=tpm)"
curl -s http://localhost:8080/healthz
```

검증:
- 로그에 `unsealed` 메시지
- `/healthz` 200

#### 4.6.3 (옵션) PCR 변조 — unseal 실패

BIOS update 또는 Secure Boot 키 변경으로 PCR 값을 의도적으로 변조한 후:

```bash
sudo reboot
sudo snap logs rosshield.server -n 50 | grep -E "(unseal failed|PCR mismatch)"
```

검증:
- unseal 실패 메시지
- 서비스 부팅 거부 (failed state)
- `snap set rosshield keystore=file` 로 임시 fallback 후 재봉인 절차(§6 참조)

---

## 5. 결과 표

> **사용자가 hands-on 측정 후 채움.** 모든 TBD 가 0건이 될 때 Phase 5 Exit 기준 충족.

### 5.1 Intel NUC 13 Pro (NUC13ANHi5)

**측정일**: TBD · **측정자**: TBD · **펌웨어**: TBD · **UC22 channel**: TBD

| 항목 | 합격 기준 | 측정값 | 합격 |
|---|---|---|---|
| Install 시간 | ≤ 30분 | TBD min | ⏳ |
| Cold boot 시간 | ≤ 60s | TBD s | ⏳ |
| 메모리 footprint (idle, 5분 후) | ≤ 200MB | TBD MB | ⏳ |
| Idle CPU (1h 평균) | ≤ 5% | TBD % | ⏳ |
| Full scan latency (단일 회) | ≤ 60s | TBD s | ⏳ |
| 8h burn-in: 메모리 증가 | ≤ 50MB | TBD MB | ⏳ |
| 8h burn-in: 크래시 | 0회 | TBD 회 | ⏳ |
| 8h burn-in: scan p95 | ≤ 60s | TBD s | ⏳ |
| OTA 시간 (정상 refresh) | ≤ 90s | TBD s | ⏳ |
| OTA rollback (broken build) | 자동 revert | TBD | ⏳ |
| TPM seal (첫 부팅) | 정상 | TBD | ⏳ |
| TPM unseal (재부팅) | 정상 | TBD | ⏳ |
| Secure Boot | enabled | TBD | ⏳ |

**총평**: TBD (예: 모든 항목 합격, 권장 customer SKU)

### 5.2 Dell OptiPlex 7010 SFF

**측정일**: TBD · **측정자**: TBD · **BIOS 버전**: TBD · **UC22 channel**: TBD

| 항목 | 합격 기준 | 측정값 | 합격 |
|---|---|---|---|
| Install 시간 | ≤ 30분 | TBD min | ⏳ |
| Cold boot 시간 | ≤ 60s | TBD s | ⏳ |
| 메모리 footprint (idle, 5분 후) | ≤ 200MB | TBD MB | ⏳ |
| Idle CPU (1h 평균) | ≤ 5% | TBD % | ⏳ |
| Full scan latency (단일 회) | ≤ 60s | TBD s | ⏳ |
| 8h burn-in: 메모리 증가 | ≤ 50MB | TBD MB | ⏳ |
| 8h burn-in: 크래시 | 0회 | TBD 회 | ⏳ |
| 8h burn-in: scan p95 | ≤ 60s | TBD s | ⏳ |
| OTA 시간 (정상 refresh) | ≤ 90s | TBD s | ⏳ |
| OTA rollback (broken build) | 자동 revert | TBD | ⏳ |
| TPM seal (첫 부팅, PTT) | 정상 | TBD | ⏳ |
| TPM unseal (재부팅) | 정상 | TBD | ⏳ |
| Secure Boot | enabled | TBD | ⏳ |

**총평**: TBD

### 5.3 (옵션) NUC 12 Pro

| 항목 | 합격 기준 | 측정값 | 합격 |
|---|---|---|---|
| (전 항목) | (§5.1과 동일) | TBD | ⏳ |

### 5.4 (옵션) ASRock Industrial NUC BOX

| 항목 | 합격 기준 | 측정값 | 합격 |
|---|---|---|---|
| (전 항목) | (§5.1과 동일) | TBD | ⏳ |

---

## 6. 트러블슈팅

| 증상 | 가능한 원인 | 해결 |
|---|---|---|
| TPM 미인식 (`tpm2_pcrread` 실패) | BIOS에서 fTPM/dTPM disable | BIOS setup → Security → TPM/PTT enable → save & reboot |
| Secure Boot 비활성 (`mokutil --sb-state` → disabled) | OEM 기본 disabled (특히 OptiPlex 일부 lot) | UEFI setup → Secure Boot → Enabled → "Standard" 모드 선택 |
| `snap install` 실패 (`network is unreachable`) | DHCP 실패 또는 DNS 해석 실패 | `systemctl status systemd-networkd` + `resolvectl status` 점검, 필요 시 `netplan apply` |
| `/healthz` timeout | port 8080 사용 중 | `sudo lsof -i :8080` — 충돌 프로세스 종료 또는 snap config로 포트 변경 (`snap set rosshield server.port=8081`) |
| `snap refresh` 실패 — post-refresh hook timeout | E35 hook 의 healthz wait가 90s로 충분치 않음 | `snap set rosshield refresh.healthz-timeout=180` |
| TPM unseal 실패 (`PCR mismatch`) | BIOS update 또는 Secure Boot 키 변경으로 PCR 변조 | (1) `snap set rosshield keystore=file` 로 임시 fallback (2) 신규 PCR 값 확인 후 `rosshield-cli tpm reseal` 로 재봉인 (3) keystore 다시 tpm 으로 복귀 |
| OTA rollback 무한 반복 | broken revision 이 dev 채널에 머무름 + auto-refresh 가 매번 시도 | `snap refresh --hold=72h rosshield` 로 잠시 정지 + 다음 정상 release 대기 후 `snap refresh --unhold` |
| `journalctl: storage is full` | 8h burn-in 로그 누적 | `sudo journalctl --vacuum-time=1d` |
| OptiPlex PTT 사용 시 reboot 후 unseal 간헐 실패 | PTT는 BIOS firmware 기반이라 micro-update에도 PCR 변동 가능 | dTPM 모델로 교체 권장하거나, customer 환경에서는 reseal 자동화 hook 추가 검토 |

---

## 7. 비용·운영 추정

| 항목 | NUC 13 Pro | OptiPlex 7010 SFF | 비고 |
|---|---|---|---|
| HW 가격 (BareBone + 16GB + 256GB SSD) | $650 | $700 | 1회, 2026년 기준 정가 |
| 전력 (idle, 측정 기반) | ~15W | ~20W | TDP가 아닌 실측 idle (UPS 차단·OS 설치 후) |
| 연 전력 비용 | ~$16 | ~$21 | 8760h × $0.12/kWh × idle W |
| 5년 TCO (HW + 전력) | ~$730 | ~$805 | 유지보수 0 가정 |
| customer 월 임대 호가 (5x margin) | $30~50 | $40~60 | Onprem SKU 옵션, HW lease + remote support |
| 5년 customer 누적 (월 $40 기준) | $2,400 | $2,400 | gross margin ~70% |

운영 가정:
- HW 5년 운용, 5년차 EOL — customer 측 교체 안내
- 정전·낙뢰는 별도 UPS(~$80, customer 부담)
- 원격 관리: snap auto-refresh + canonical store 통한 release window 운영 (별도 management plane 비용 없음)

---

## 8. 후속·잔여

### 8.1 즉시 후속 (Phase 5 잔여 stage)

- [ ] **arm64 빌드**: `.github/workflows/snap-build.yml` 의 matrix에 `arm64` 추가 — 후속 epic E37 (예정) 에서 처리
- [ ] **Raspberry Pi 5 검증**: R40-4 SKU 확장. Pi 5는 fTPM 미탑재 → keystore=file 시나리오만 검증
- [ ] 측정 자동화: §4.3~§4.6 절차를 `scripts/burnin/` 디렉터리의 shell script로 패키징

### 8.2 customer 명시 요청 시

- [ ] **24h burn-in**: 8h → 24h 확장. 메모리 누수 임계는 비례 확장(150MB)
- [ ] **다중 fleet 동시 시뮬레이션**: 한 박스에서 4 fleet (각 50 robot) 동시 ingest 부하 측정
- [ ] **PoE+ 전원 단절·복구 시나리오**: 1초·10초·1분 단절 후 unseal 동작

### 8.3 swtpm CI에서 검증되지 않는 항목 (실 칩 hands-on 필수)

- 실 TPM 칩 PCR_Extend BIOS 변조 시나리오 (§4.6.3)
- Secure Boot 사용자 키 enrollment (Microsoft + Canonical 키만 현재 지원)
- BIOS firmware 업데이트 후 PTT PCR 안정성 (특히 OptiPlex)

### 8.4 본 epic 비목표 (재확인)

- 자체 hardware 제조 — 비목표 유지
- 실시간 OS(RTOS) 지원 — 비목표 유지
- Secure Boot 자체 키 발급(MOK 아닌 PK 교체) — 비목표 유지

---

## 부록 A — 참고 링크

- Ubuntu Core 22: <https://ubuntu.com/core/docs/uc22>
- snap post-refresh hooks: <https://snapcraft.io/docs/supported-snap-hooks>
- TPM 2.0 PCR allocation: <https://trustedcomputinggroup.org/resource/pc-client-platform-tpm-profile-ptp-specification/>
- mokutil(1): <https://manpages.ubuntu.com/manpages/jammy/man1/mokutil.1.html>

## 부록 B — 측정 로그 보관 위치

각 모델·측정 회차별로 다음 디렉터리에 raw 로그 보관:

```
docs/appliance/measurements/
  ├─ nuc13-pro/
  │   └─ 2026-MM-DD/
  │       ├─ install.log
  │       ├─ sar-cpu-1h.log
  │       ├─ sar-mem-8h.log
  │       ├─ scan-{1..96}.log
  │       └─ snap-logs.log
  └─ optiplex-7010/
      └─ 2026-MM-DD/
          └─ ...
```

raw 로그는 `.gitignore` 처리 후 별도 evidence bundle로 customer에게 전달.
