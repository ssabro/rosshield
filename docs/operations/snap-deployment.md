# rosshield snap 배포 가이드 (E33 Phase 5)

> **상태**: 1차 amd64 빌드 + smoke test 자동화 완료. arm64는 후속 stage. snap store
> 공개 채널 발행은 customer 검증 후 수동.
> **R40-1 결정**: `core22` base (LTS, 2027까지 지원).

## 1. 개요

rosshield snap은 **strict confined Ubuntu Core / Classic snap**으로 패키징됩니다.
대상:
- Ubuntu Core 22 어플라이언스 (E36 레퍼런스 HW)
- Ubuntu Server 22.04+ classic 환경
- 데스크톱 / 개발자 머신 (`snap install --dangerous`로 dev snap)

**비목표**:
- snap channel 공개(`stable`/`candidate`/`edge`) 발행 — customer 검증 후 수동 (`snapcraft upload`).
- arm64 — 1차는 amd64만, 후속 stage에서 추가.
- snap configure hooks — Phase 5 후속(E34 TPM·E35 OTA)에서 추가.

## 2. 빌드 파이프라인

### 2.1 자동 (CI)

`v*.*.*` 태그 push 또는 `workflow_dispatch`로 트리거:

1. **`.github/workflows/snap-build.yml`** — `snapcore/action-build@v1`로 amd64 snap 빌드
   - core22 base + Go 1.22 빌드 snap + Node 20 (web 빌드용)
   - artifact upload + 기존 release에 attach (release-pipeline.yml 완료 후 본 workflow 자동 실행)

2. **`.github/workflows/snap-smoke.yml`** — snap-build 완료 시 자동 실행
   - `sudo snap install --dangerous`
   - `snap services rosshield` (active 확인)
   - `curl /healthz` 200 OK + 필수 필드(`status`, `components.storage`, `components.signer`) 검증
   - `snap remove --purge` 정리

### 2.2 로컬 빌드

snapcraft 7.x 또는 8.x + LXD가 필요:

```bash
# 사전 준비
sudo snap install snapcraft --classic
sudo snap install lxd
sudo lxd init --auto

# 빌드 (amd64 native)
cd /path/to/rosshield
snapcraft

# 결과
ls -lh rosshield_*.snap
```

빌드 시간: ~5~8분 (Go 1.22 + web 빌드 + snap pack).

## 3. 설치

### 3.1 dev snap (서명 없음, 로컬·시연용)

```bash
sudo snap install rosshield_<version>_amd64.snap --dangerous

# 첫 시작 확인
sudo snap services rosshield
# Service              Startup  Current  Notes
# rosshield.server     enabled  active   -

# /healthz
curl http://localhost:8080/healthz | jq .
```

### 3.2 snap store (향후, customer 검증 후)

```bash
# 운영자가 한 번 발행 (CI 완료 + smoke 통과 + 수동 review)
snapcraft upload --release=stable rosshield_<version>_amd64.snap

# 사용자 측
sudo snap install rosshield
```

## 4. 설정

snap config는 `snap set rosshield <key>=<value>` 패턴 (Phase 5 후속에서 추가):

```bash
# 1차는 환경 변수 + CLI flag로만 설정.
# rosshield-server snap이 systemd service로 등록되므로:
sudo systemctl edit snap.rosshield.server
# [Service]
# Environment="ROSSHIELD_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable"
# Environment="ROSSHIELD_LICENSE_TOKEN=<token>"
# Environment="ROSSHIELD_LICENSE_PUBKEY_HEX=<hex>"
sudo systemctl daemon-reload
sudo snap restart rosshield.server
```

## 5. 데이터 디렉터리

snap의 strict confinement는 `/var/snap/rosshield/common/` 또는 `/var/snap/rosshield/current/`만 접근 가능:

| 경로 | 용도 |
|---|---|
| `$SNAP_COMMON/data/` | SQLite DB·blob storage·keys (snap upgrade 시 보존) |
| `$SNAP_DATA/` | snap 인스턴스별 (upgrade 시 마이그레이션) |
| `$SNAP/bin/` | 바이너리 (read-only) |

기본 `ROSSHIELD_DATA_DIR=$SNAP_COMMON/data` (snapcraft.yaml apps.server.environment 참조).

## 6. CLI 사용

snap이 설치되면 3 바이너리가 PATH에 등록:

```bash
rosshield --help                      # 인증·초대·webhook·license CLI
rosshield-server backup --output ...  # 백업 (E28)
rosshield-audit-verify --bundle ...   # 외부 감사인 검증 (E30)
```

## 7. 업데이트 (snap refresh) + 자동 rollback (E35)

`snap channel` 발행이 있으면 자동 업데이트:

```bash
sudo snap refresh rosshield
sudo snap refresh --hold=72h rosshield   # 72시간 holding
```

### 7.1 자동 rollback 메커니즘 (E35)

snap이 새 revision을 install한 직후 `snap/hooks/post-refresh`가 실행됩니다.
healthz 폴링으로 새 revision의 정상 부팅을 검증 — **실패 시 snapd가 자동으로
이전 revision으로 복원**합니다 (snap의 standard rollback, 운영자 개입 불필요).

기본 동작:
- `/healthz`에 최대 60s 폴링 (2s 간격 + 5s curl timeout)
- 응답 본문에 `"status":"ok"` 발견 시 OK
- 60s 안에 OK 못 받으면 hook이 exit 1 → snapd 자동 revert

### 7.2 healthz 폴링 정책 조정

```bash
# 기본 60s를 120s로 늘림 (느린 부팅 환경)
sudo snap set rosshield healthz-timeout=120

# 별도 healthz endpoint (예: /metrics 포트와 분리)
sudo snap set rosshield healthz-url=http://127.0.0.1:9090/healthz

# 현재 값 확인
sudo snap get rosshield
```

`healthz-timeout`은 5~600초 범위. `healthz-url`은 `http://` 또는 `https://`
prefix 필수. 잘못된 값은 `configure` hook이 거부.

### 7.3 수동 rollback

자동 rollback이 동작하지 않거나(예: hook 자체 버그) 운영자가 강제로 이전 버전을
원할 때:

```bash
sudo snap revert rosshield                    # 즉시 이전 revision으로 복원
sudo snap list rosshield --all                # 보유 revision 목록 확인
sudo snap revert rosshield --revision=<N>     # 특정 revision으로 복원
```

snap은 기본 2 revision을 보유합니다(`snap set system refresh.retain=3`로 늘림).

### 7.4 채널 정책

| Channel | 용도 | 권장 운영 |
|---|---|---|
| `latest/edge` | 매 commit 빌드 | 개발자·QA — production 사용 금지 |
| `latest/candidate` | tag rc 빌드 | staging 환경, 1주 burn-in |
| `latest/stable` | tag stable release | production |

운영자는 `sudo snap refresh --channel=latest/stable rosshield`로 채널 잠금 가능.

### 7.5 OTA round-trip 검증

CI(`snap-smoke.yml`)에서 자동 검증:
- 같은 .snap을 두 번째 install → snapd가 refresh로 처리, 새 revision 부여
- post-refresh hook 실행 + healthz 검증 → 정상 시 새 revision 활성

수동 검증 (multipass 또는 LXD VM):
```bash
# 1. snap channel candidate에서 install
sudo snap install rosshield --channel=latest/candidate

# 2. /healthz 200 + revision 기록
curl localhost:8080/healthz
snap list rosshield   # revision X 메모

# 3. 새 candidate 발행 후 refresh
sudo snap refresh rosshield

# 4. 정상 시: revision Y로 갱신 + post-refresh hook OK + healthz 200
# 5. 실패 시: post-refresh exit 1 → snapd 자동 revert → revision X 복원
snap list rosshield --all   # X와 Y 모두 보유, 활성은 X (revert 시) 또는 Y (정상)
sudo snap changes rosshield | head -5   # refresh 단계 + hook 결과 확인
```

broken refresh 시뮬레이션 (E36 hands-on 또는 staging):
- 의도적으로 healthz가 timeout 나도록 새 revision 빌드 (예: --addr 오타)
- snap refresh 실행 → post-refresh hook 60s 폴링 → exit 1 → snapd 자동 revert
- 운영자 개입 0 검증

## 8. 트러블슈팅

| 증상 | 원인 | 해결 |
|---|---|---|
| `snap install` 실패 with "snap not signed" | dev snap이라 `--dangerous` 누락 | `sudo snap install ... --dangerous` |
| service inactive after install | snap services 미활성 | `sudo snap start --enable rosshield.server` |
| /healthz timeout | port 8080 conflict | `lsof -i :8080` 후 다른 process kill |
| storage 'error: ...' in /healthz | data dir 권한 | `sudo ls -la /var/snap/rosshield/common/data` 확인 |
| log에 "permission denied" 빈번 | strict confinement 외 path 접근 시도 | snap interface 확인: `snap connections rosshield` |

## 9. 한계 및 후속

- **arm64 빌드** — 1차는 amd64만. 후속 stage에서 multi-arch matrix 또는 cross-compile.
- **snap store 발행** — 본 stage는 release attach + dev install만. 공개 channel 발행은 customer feedback 후.
- **TPM 봉인** — E34 완료 (keystore plug + TPM 2.0 PCR 봉인 + Secure Boot enrollment 가이드).
- **A/B OTA** — E35 완료 (snap channel + post-refresh hook + 자동 rollback). CI에서 same-snap refresh round-trip 검증.
- **multi-revision OTA broken simulation** — multipass/LXD VM nested virt 필요, GH Actions 미지원. customer 환경 또는 E36 hands-on에서 검증.
- **레퍼런스 HW** — E36 docs scaffolding 완료, 실 측정은 사용자 hands-on.

## 10. 참조

- snapcraft.yaml: `snap/snapcraft.yaml`
- 빌드 workflow: `.github/workflows/snap-build.yml`
- smoke workflow: `.github/workflows/snap-smoke.yml`
- 설계: `docs/design/phase5-backlog.md` §E33
- R40-1 결정: `SESSION_HANDOFF.md` 결정 로그 (2026-05-11)
- snapcraft 공식: <https://snapcraft.io/docs>
