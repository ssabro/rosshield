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

## 7. 업데이트 (snap refresh)

`snap channel` 발행이 있으면 자동 업데이트:

```bash
sudo snap refresh rosshield
sudo snap refresh --hold=72h rosshield   # 72시간 holding
```

자동 롤백은 E35 stage(별 epic)에서 추가.

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
- **TPM 봉인** — E34 stage에서 keystore plug + TPM 2.0 PCR 봉인.
- **A/B OTA** — E35 stage에서 snap channel + 자동 rollback policy.
- **레퍼런스 HW** — E36 stage에서 NUC + OptiPlex burn-in.

## 10. 참조

- snapcraft.yaml: `snap/snapcraft.yaml`
- 빌드 workflow: `.github/workflows/snap-build.yml`
- smoke workflow: `.github/workflows/snap-smoke.yml`
- 설계: `docs/design/phase5-backlog.md` §E33
- R40-1 결정: `SESSION_HANDOFF.md` 결정 로그 (2026-05-11)
- snapcraft 공식: <https://snapcraft.io/docs>
