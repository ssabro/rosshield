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

## 7. 업데이트 (snap refresh) + health 모니터링 (E35 redesign)

`snap channel` 발행이 있으면 자동 업데이트:

```bash
sudo snap refresh rosshield
sudo snap refresh --hold=72h rosshield   # 72시간 holding
```

### 7.1 hook 책임 분리 (2026-05-21 redesign)

본 snap의 hook 3종은 snap lifecycle의 각기 다른 시점에 호출되며, 책임이 명확히 분리되어 있습니다.

| Hook | 호출 시점 | 책임 | exit 1 동작 |
|---|---|---|---|
| `post-refresh` | 새 revision unpack 직후, services start **전** | binary 무결성 + configure 값 sanity check | snapd 자동 revert (catastrophic case only) |
| `check-health` | services start 후 + 주기적(약 5분) | healthz polling + `snapctl set-health` 호출 | exit status는 snapd가 무시; set-health 결과만 활용 |
| `configure` | `snap set` 호출 시 | healthz-url·healthz-timeout·backup-* 값 검증 | snap set 거부 |

**중요한 lifecycle 사실** (snapcraft.io forum #37238 확정):
- `post-refresh` hook은 새 revision의 daemon services가 **시작되기 전**에 실행됩니다.
- 따라서 post-refresh에서 `/healthz` polling은 architecturally 불가능 — 새 daemon이 아직 listen 시작 전.
- daemon health 검증은 `check-health` hook 책임 (snapd 2.41+ 표준).

### 7.2 자동 rollback 범위 (축소됨)

snap 표준 rollback은 **post-refresh hook이 exit 1** 시에만 트리거됩니다. 본 설계에서 post-refresh는 catastrophic case만 cover:
- `bin/rosshield-server` 미존재 또는 빈 파일 (binary corruption)
- configure 값에 잘못된 schema (예: healthz-url이 http(s):// prefix 아님)

**daemon이 부팅 후 unhealthy가 되는 case는 자동 rollback 대상이 아님** — `check-health` hook이 `snapctl set-health waiting` 또는 unhealthy 상태를 표시하며, 운영자/Prometheus alert가 대응합니다.

설계 근거: snap 표준 hook 중 daemon active 이후 호출되면서 자동 revert를 트리거하는 hook은 존재하지 않습니다(`docs/design/notes/e35-refresh-rollback-redesign.md` §2 참조).

### 7.3 health 모니터링

#### 명령어로 직접 확인

```bash
# snap의 현재 health status — okay / waiting / blocked / error
sudo snap health rosshield

# unhealthy인 snap만 출력 (정상이면 출력 0건)
sudo snap health

# snap info로 health summary 포함 메타 정보
snap info rosshield
```

`snap health` 출력 의미:
- **okay**: 정상 (또는 출력 자체가 비어 있음 — "no health concerns")
- **waiting**: transient — daemon이 막 부팅 중이거나 일시적 healthz timeout. snapd가 5분 후 재호출하므로 자동 catch up 가능.
- **blocked / error**: admin 개입 필요 — 운영자가 snap revert 또는 디버깅 필요.

#### Prometheus + Grafana

`deploy/grafana/rosshield-dashboard.json`이 `/healthz`의 components.* 필드를 시각화합니다. 운영자는 Grafana alert rule으로 unhealthy 상태가 N분 이상 지속 시 호출되도록 설정 권장:

```yaml
# 예: prometheus alert rule
groups:
  - name: rosshield-snap
    rules:
      - alert: RosshieldUnhealthy
        expr: up{job="rosshield"} == 0
        for: 5m
        labels: { severity: critical }
        annotations:
          summary: "rosshield daemon unhealthy ≥ 5m — check snap health rosshield + consider snap revert"
```

### 7.4 healthz 폴링 정책 조정

```bash
# 별도 healthz endpoint (예: /metrics 포트와 분리)
sudo snap set rosshield healthz-url=http://127.0.0.1:9090/healthz

# healthz-timeout은 configure hook 검증용 (5~600초)
sudo snap set rosshield healthz-timeout=120

# 현재 값 확인
sudo snap get rosshield
```

**주의**: `healthz-timeout`은 configure hook의 값 검증 범위만 의미합니다. check-health hook 자체는 snapd 권장에 따라 max 30s로 cap (fast + idempotent). 이는 hook이 idempotent하게 주기 호출되어야 하므로 의도된 제약입니다.

`healthz-url`은 `http://` 또는 `https://` prefix 필수. 잘못된 값은 `configure` hook이 거부합니다.

### 7.5 broken refresh 대응 절차

새 revision install 후 unhealthy 또는 동작 이상 감지 시:

```bash
# 1. snap health 상태 확인
sudo snap health rosshield

# 2. 직접 /healthz body 검사
curl http://localhost:8080/healthz | jq .

# 3. snap log 확인
sudo snap logs rosshield -n 100

# 4. 이전 revision으로 복원
sudo snap revert rosshield                    # 직전 revision으로
sudo snap list rosshield --all                # 보유 revision 목록 확인
sudo snap revert rosshield --revision=<N>     # 특정 revision으로

# 5. refresh 자동 시도 holding (별 release 대기)
sudo snap refresh --hold=24h rosshield
```

snap은 기본 2 revision을 보유합니다(`snap set system refresh.retain=3`로 늘림).

### 7.6 채널 정책 (자동 rollback 부재 보완)

자동 rollback이 축소되었으므로 channel staged rollout이 broken revision 차단의 1차 방어선입니다.

| Channel | 용도 | 권장 운영 |
|---|---|---|
| `latest/edge` | 매 commit 빌드 | 개발자·QA — production 사용 금지 |
| `latest/candidate` | tag rc 빌드 | staging 환경, **3~7일 burn-in** + check-health 모니터링 |
| `latest/stable` | candidate burn-in 통과 후 수동 승급 | production |

운영자는 `sudo snap refresh --channel=latest/stable rosshield`로 채널 잠금 권장.

snap store 공식 발행 시:
- CI는 tag push 시 `latest/edge`로 자동 발행
- staging 환경은 `latest/candidate` 추적, 운영자가 모니터링
- staging burn-in 통과 후 운영자가 `snapcraft release rosshield <revision> stable` 수동 승급

### 7.7 OTA round-trip 검증

CI(`snap-smoke.yml`)에서 자동 검증:
- 같은 .snap을 두 번째 install → snapd가 refresh로 처리, 새 revision 부여
- post-refresh hook 실행 (파일 무결성 check) + daemon restart
- 외부 healthz polling으로 새 revision daemon ready 확인
- `snap health` okay 도달 확인 (check-health hook 호출 후)

수동 검증 (multipass 또는 LXD VM):
```bash
# 1. snap channel candidate에서 install
sudo snap install rosshield --channel=latest/candidate

# 2. /healthz 200 + revision 기록 + health okay 확인
curl localhost:8080/healthz
snap list rosshield                  # revision X 메모
sudo snap health rosshield           # okay 또는 출력 0건

# 3. 새 candidate 발행 후 refresh
sudo snap refresh rosshield

# 4. 정상 시: revision Y로 갱신 + healthz 200 + snap health okay
# 5. catastrophic 실패 시: post-refresh exit 1 → snapd 자동 revert → revision X 복원
# 6. daemon unhealthy 시: snap health waiting/blocked → 운영자 절차(§7.5)
snap list rosshield --all
sudo snap changes rosshield | head -5
```

broken refresh 시뮬레이션 (E36 hands-on 또는 staging):
- 의도적으로 binary corruption 또는 잘못된 configure 값 주입 → post-refresh exit 1 → 자동 revert
- 의도적으로 unhealthy daemon (예: DB 권한 박탈) → `snap health rosshield` waiting → 운영자 수동 revert

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
- **snap store 발행** — 본 stage는 release attach + dev install만. 공개 channel 발행은 customer feedback 후. 발행 전 channel staged rollout 운영 절차(§7.6) 필수.
- **TPM 봉인** — E34 완료 (keystore plug + TPM 2.0 PCR 봉인 + Secure Boot enrollment 가이드).
- **A/B OTA + 자동 rollback** — E35 1차 후 2026-05-21 redesign (`docs/design/notes/e35-refresh-rollback-redesign.md`):
  - 1차 설계(post-refresh에서 healthz polling + exit 1로 자동 revert)는 snap lifecycle 표준 제약(post-refresh가 services start 전 호출)으로 architectural mismatch 발견.
  - 재설계 후: post-refresh는 catastrophic case(binary corruption + 잘못된 configure schema)만 cover, daemon health는 check-health hook + Prometheus alert로 위임.
  - 자동 rollback 범위는 binary corruption 등 catastrophic case로 축소. daemon이 unhealthy가 되는 case는 운영자 수동 절차(§7.5)로 대응.
- **post-refresh hook은 daemon health 검증 불가** — snap lifecycle 표준 제약. healthz 검증은 `check-health` hook 또는 외부 monitor가 책임.
- **multi-revision OTA broken simulation** — multipass/LXD VM nested virt 필요, GH Actions 미지원. customer 환경 또는 E36 hands-on에서 검증.
- **레퍼런스 HW** — E36 docs scaffolding 완료, 실 측정은 사용자 hands-on.
- **snap store channel staged rollout** — docs 권장(§7.6)만, 실제 snap store 등록 + CI release pipeline 분리는 별 epic (paying customer 진입 시점).

## 10. 참조

- snapcraft.yaml: `snap/snapcraft.yaml`
- 빌드 workflow: `.github/workflows/snap-build.yml`
- smoke workflow: `.github/workflows/snap-smoke.yml`
- 설계: `docs/design/phase5-backlog.md` §E33
- R40-1 결정: `SESSION_HANDOFF.md` 결정 로그 (2026-05-11)
- snapcraft 공식: <https://snapcraft.io/docs>
