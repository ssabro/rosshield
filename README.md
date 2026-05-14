# `<ProductName>` — 코드네임 rosshield

> **상태**: **Phase 5 진행 중** — Phase 0~4 마감, enterprise build tag scaffold 완료, 첫 customer 진입 자료 준비.
> **산출**: Go server + TypeScript web UI + CLI 3종(`rosshield`, `rosshield-server`, `rosshield-audit-verify`) + pack-tools converter. v0.2.0 release(2026-05-08, 47 assets cosign keyless 서명).
> **CIS Ubuntu 24.04 자동 변환률**: **93.3%** (291/312, 2026-05-14). nrobotcheck baseline JSON에서 24 합성 패턴 자동 변환 — 기본 9종 + 12 epic별(D auditctl / E-1 gsettings+sshd+auditd / E-2 nftables+iptables / E-3 dpkg+stat+apparmor / G16/G14/G3/G10/G6/G13/G5) + 잔여 3종(6.2.3.6 hashbang OK/Warning / 4.4.2.2 iptables -L X -v -n / 4.2.4 ufw before.rules + status). **NoMarker 0건 완전 마감** — 잔여 21건은 모두 `assessment_status="Manual"` (CIS 명시적 manual review 요구) 자동 변환 비대상.
> **탄생일**: 2026-04-23

ROS2 로봇 플릿 보안 감사 플랫폼. 감사인이 받아들이는 결정론적 증거와 서명된 리포트를 생성하는 상용 B2B 제품.

> **제품 브랜드**는 미확정(D1 연기). 문서·UI 등 사용자 대면 문자열은 `<ProductName>` placeholder를 씁니다. **코드 네임스페이스는 `rosshield`로 확정**(2026-04-23) — Go 모듈·내부 패키지·설정 경로에서 사용. 결정 추적은 [`SESSION_HANDOFF.md`](./SESSION_HANDOFF.md).

## 한 줄 포지셔닝

> **CAI가 로봇을 침투한다면, `<ProductName>`은 플릿이 그 공격에 대비되어 있는지 매일 증명한다.**

## 지금 무엇을 볼 수 있는가

- **실 동작 코드**: `cmd/rosshield-server` (HTTP API + scan engine + audit chain) · `cmd/rosshield` (CLI: backup/ha status/scan/license) · `cmd/rosshield-audit-verify` (외부 검증 SDK) · `cmd/pack-tools` (CIS·ROS2 baseline → rosshield pack 변환·서명)
- **Web UI**: TypeScript + TanStack Router/Query + shadcn/ui — `/system` (헬스·HA·라이선스·백업) · `/robots` (drill-down + 진단 이력 + credential rotate) · `/scans` (live progress + cancel) · `/packs` (built-in CIS + ROS2 baseline) · `/compliance` (ISMS-P/ISO27001/NIST 800-53 매핑) · `/findings` · `/integrations` (webhook + SSO + invitation) · `/users`
- **자동 변환된 CIS pack**: `packs/cis-ubuntu-2404/` (235 checks 자동 변환, 21 Manual + 49 NoMarker degraded marker)
- **운영 자료**: [`docs/operations/`](./docs/operations/) (HA / snap deployment / TPM Secure Boot / release pack signer setup) · [`deploy/grafana/`](./deploy/grafana/) (Prometheus dashboard JSON 12 panel)
- **첫 customer onboarding 자료**: [`docs/onboarding/`](./docs/onboarding/) (README + quickstart + customer intake template + demo script)
- **전체 시스템 설계서**: [`docs/design/`](./docs/design/) 13개 마크다운 + [`docs/design/notes/`](./docs/design/notes/) 16개 deepdive
- **세션 진입점**: [`SESSION_HANDOFF.md`](./SESSION_HANDOFF.md) — 현재 상태·결정 대기 항목·다음 선택지 (살아있는 작업 로그)
- **Claude Code 지침**: [`CLAUDE.md`](./CLAUDE.md) — AI 세션이 이 리포에서 작업할 때 따라야 하는 원칙·컨벤션

## 설계 문서 읽는 순서

| 역할 | 최소 세트 |
|---|---|
| **임원·PM** | `design/00-mission-and-positioning.md` · `design/02-system-overview-and-deployment.md` · `design/11-tech-stack-and-roadmap.md` |
| **아키텍트** | `design/01-principles.md` · `design/03-architecture.md` · `design/04-domain-and-data-model.md` · `design/05-api-and-auth.md` · `design/06-security-and-tenancy.md` |
| **구현 엔지니어** | `design/03-architecture.md` · `design/07-scan-engine-and-benchmarks.md` · `design/08-intelligence-and-compliance.md` · `design/11-tech-stack-and-roadmap.md` |
| **보안·감사** | `design/06-security-and-tenancy.md` · `design/08-intelligence-and-compliance.md` · `design/10-audit-and-observability.md` |
| **새 Claude 세션** | `CLAUDE.md` → `SESSION_HANDOFF.md` → `design/README.md` |

## 핵심 결정 (설계 수준에서 확정)

- **포지셔닝**: 결정론적 감사 + 컴플라이언스 증명 + 로봇 특화
- **배포**: 같은 코어 + 3종 셸(데스크톱·온프렘·어플라이언스)
- **어플라이언스**: 자체 제조 없음. 이미지 + 레퍼런스 디자인 + 파트너 채널
- **테넌시**: 멀티테넌시 기본값
- **감사**: 해시 체인 + 외부 검증 API + 서명된 PDF
- **LLM**: 완전 옵트인, 결정론적 fallback 필수
- **비목표**: 자율 공격 에이전트·자체 HW 제조·SaaS-only·범용 IT 감사 도구

## 결정 현황 (Phase 5 진행 중)

| # | 항목 | 상태 |
|---|---|---|
| D1 | 제품명·도메인 | 🟡 연기 — 코드네임 `rosshield` 확정, 제품 브랜드는 D8 출원 잠금 해제 후 (Top 3 후보: Custos·Lodestar·Praxis, R40-5 2026-05-11) |
| D2 | 백엔드 언어 | ✅ Go (1.26) + TypeScript (web) |
| D3 | 데스크톱 셸 | ✅ Tauri 2.x |
| D4 | 어플라이언스 OS | ✅ Ubuntu Core 24 + snap (R40-1 core22 base, 2026-05-11) |
| D5 | 라이선스 | ✅ Open-core — 코어 Apache-2.0, 엔터프라이즈는 별 라이선스 (R30-4, 2026-05-08) |
| D6 | 리포 호스팅 | ✅ GitHub private 유지 — release binary + audit verify SDK가 P1 외부 검증 대체 (R30-4) |
| D7 | 초기 타깃 벤치마크 | ✅ CIS Ubuntu 24.04 (자동 변환 93.3% — NoMarker 0건) + ROS2 Jazzy baseline |
| D8 | 특허 전략 | ✅ 후보 D 채택 + 1순위 결합 청구항(A-1+B-1+C-1+D-3) + enterprise build tag 가속 (2026-05-08) |
| R30-1 | PG-native repo | ✅ C 하이브리드 — driver-aware sqliterepo 단일 경로 + 핫 path 3 컬럼 회수 |
| R30-2 | HA | ✅ PostgreSQL advisory lock + leader/follower (E25 마감) |
| R40-2 | TPM 시뮬레이터 | ✅ swtpm — google/go-tpm-tools v0.4.8 + PCR [0,2,4,7] |
| R40-3 | WASM 런타임 | ✅ wazero (Pure Go, CGO=0 유지) |
| R40-4 | 첫 customer SKU | ✅ Onprem (Compose/단일 서버 multi-user, v0.2.0 형태) |

상세는 [`SESSION_HANDOFF.md`](./SESSION_HANDOFF.md) 결정 로그 + [`docs/design/phase5-backlog.md`](./docs/design/phase5-backlog.md).

## 기여

[`CONTRIBUTING.md`](./CONTRIBUTING.md) 참조.

## 라이선스

[`LICENSE`](./LICENSE) — Apache License 2.0 (코어). 엔터프라이즈 모듈은 추후 별도 라이선스.

## Release 검증 (R30-4 / E26)

GitHub 비공개 repo이므로 외부 검증자는 release binary + Sigstore cosign keyless 서명으로 무결성을 확인합니다.

```bash
# 1) cosign 서명 검증 (Rekor public log + GitHub OIDC)
cosign verify-blob \
  --certificate <binary>.cert \
  --signature <binary>.sig \
  --certificate-identity-regexp 'https://github.com/ssabro/rosshield/.github/workflows/release.yml@refs/tags/.*' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  <binary>

# 2) checksum
sha256sum -c checksums.sha256

# 3) 빌드 메타 (commit·built·go)
./rosshield version

# 4) report bundle 검증 (외부 감사인 standalone tool, E30 산출)
./rosshield-audit-verify --bundle <report.tar.gz>
```

## Invite Email (O6)

초대 발송은 옵트인입니다. 기본값(`--email-provider=noop`)은 SMTP 연결 없이 발송 시도를
slog Info 한 줄로만 기록합니다 — admin이 `POST /api/v1/invitations` 응답의 `token`을
사용자에게 직접 전달하는 모델 (데스크톱·소규모 SKU 가정).

### Noop (기본)

```bash
rosshield-server --addr 127.0.0.1:8080
# CreateInvitation 시 logger Info 한 줄:
# {"time":"...","level":"INFO","msg":"email noop send","payload":"{\"ts\":\"...\",\"to\":\"newuser@acme.test\",...}"}
```

### SMTP

```bash
ROSSHIELD_SMTP_PASSWORD='secret' rosshield-server \
  --addr 127.0.0.1:8080 \
  --email-provider smtp \
  --email-smtp-host smtp.example.com \
  --email-smtp-port 587 \
  --email-smtp-user noreply@example.com \
  --email-from 'rosshield <noreply@example.com>' \
  --public-base-url https://app.example.com
```

`--public-base-url`이 설정되면 초대 이메일에 `<base>/invitations/accept/<token>` 형식의
accept URL이 포함됩니다. 비어 있으면 본문에 토큰만 명시되고 admin이 별도로 전달.

발송 실패는 invitation INSERT를 rollback하지 않습니다 — best-effort. 실패 시에도 admin은
`POST /api/v1/invitations` 응답의 `token`을 받아 수동 전달 가능. 실패는 logger.Warn으로 기록.

## Backup·Restore (E28)

단일 인스턴스 데이터 손실 위험 완화. SQLite VACUUM INTO로 일관 스냅샷을 떠내므로 서버
실행 중에도 안전합니다.

```bash
# 백업 — 데이터 디렉터리 → tar.gz (data.db 스냅샷 + keys/* + evidence/*)
rosshield-server backup --output /backups/rosshield-$(date +%Y%m%d).tar.gz

# evidence 제외 (메타데이터 전용 — 빠르고 작음)
rosshield-server backup --output /backups/meta-$(date +%Y%m%d).tar.gz --skip-evidence

# 복원 — 빈 디렉터리에. 기존 파일이 있으면 거부 (--force로 강제).
rosshield-server restore --input /backups/rosshield-20260508.tar.gz \
  --data-dir /var/lib/rosshield
```

cron 예시 (매일 03:15 KST 백업 + S3 업로드):

```cron
15 3 * * * /usr/local/bin/rosshield-server backup \
  --output /var/backups/rosshield/$(date +\%Y\%m\%d).tar.gz \
  --data-dir /var/lib/rosshield && \
  aws s3 cp /var/backups/rosshield/$(date +\%Y\%m\%d).tar.gz \
  s3://my-bucket/rosshield-backups/ --storage-class STANDARD_IA
```

stdout JSON 예 (backup): `{"path":"...","size":18297,"sha256":"232267...","includesEvidence":true,"generatedAt":"2026-05-08T04:31:20.9056265Z"}` — 운영 자동화 파이프라인이 sha256으로 무결성 추적 가능.

## CIS Ubuntu 24.04 자동 변환 (E12)

`pack-tools convert`는 nrobotcheck baseline JSON을 rosshield pack(`pack.yaml` + `checks/*.yaml` + `selftest/*.yaml`)으로 변환합니다.

```bash
# 빌드
make pack-tools-build

# 변환 (CIS Ubuntu 24.04 → packs/cis-ubuntu-2404/)
./bin/pack-tools convert \
  --format cis-ubuntu-json-v1 \
  --input <nrobotcheck-dir>/resources/baselines/cis_ubuntu_2404_benchmark.json \
  --output packs/cis-ubuntu-2404

# 출력 예:
# CIS Ubuntu 변환 완료: packs/cis-ubuntu-2404
#   total: 312, auto-converted: 291 (93.3% auto)
#   degraded: Manual=21, NoMarker=0 (모든 자동 변환 가능 항목 cover 완료)

# tar.gz 서명 + built-in embed
make pack-archive  # bin/pack-tools archive로 packs/*.tar.gz 생성 + internal/builtin/packs/_archives/ 복사
```

**자동 변환 패턴 21종** — 본 세션 18.9% → 92.0% 진척(+73.1%p, 3일):

기본 9종 (Phase 4까지):
1. PASS/FAIL 마커 + bash hashbang body wrap (61건)
2. "Nothing should be returned" 변형 expect-empty (다수)
3. "is installed/enabled/active/mounted" expect-non-empty (다수)
4. stat -Lc + octal mode + Uid 권한 검증 (12건)
5. sshd boolean / numeric range / numeric ≤·≥ / `between N and M` (16건)
6. multi-line cmd 흡수 (quote-balance 기반 join, dangling `--`/`-` 보강)
7. base64 sub-shell wrap — PASS 마커 부재 hashbang body
8. grep + "verify output matches" / "Output should be similar"
9. awk + "verify that only X is returned" 정확 매칭

신규 12 영역별 (Phase 5, 본 세션):
10. **D 6.2.3.x auditctl** (19건) — `auditctl -l` + Verify output matches/includes + audit rule normalize (syscall sort + `-k` ↔ `-F key=` + UID_MIN placeholder + auid 동치)
11. **E-1 G7-bool gsettings** (2건) — `gsettings get` true/false 정확 매칭
12. **E-1 G7-uint32 gsettings** (1건) — uint32 N + threshold 비교
13. **E-1 G15 multi-cmd grep auditd.conf** (1건) — 2+ grep 명령 path continuation join
14. **E-1 G11 sshd -T multi-line OR** (2건) — case insensitive substring + placeholder 제거
15. **E-2 G1 nftables hook** (2건) — 3+ `nft list ruleset | grep 'hook X'` substring 매칭
16. **E-2 G2 nftables list tables** (1건) — single cmd Return should include
17. **E-2 G4 iptables chain policy** (1건) — `iptables -L` 3+ Chain X (policy Y) substring
18. **E-3 G9 dpkg-query** (3건) — installed status + emptyOutput mode (cmd wrap join)
19. **E-3 G12 stat 옵트** (2건) — `[ -e file ] && stat ...` + path 슬라이스 + 옵트 가드
20. **E-3 G8 apparmor count** (2건) — profiles loaded/complain/unconfined 카운트 + mode phrase 자동 판정
21. **G16 + G14 + G3 + G10 + G6 + G13 + G5** (7+ 건) — passwd/group awk + grub.cfg + nftables include + hashbang emit (PASSED·Audit Passed·brace block) + ufw status default + sudo timeout

**NoMarker 0건 완전 마감** — 본 세션 56 → 0 (자동 변환 가능 모든 항목 cover). 잔여 degraded 21건은 모두 `assessment_status="Manual"` (CIS 명시적 manual review 요구) 자동 변환 비대상.

**합성 bash 회귀 검증**: `tags=cis_synth_integration` 옵트인 28 케이스(기존 24 + D Stage 3의 4 auditctl) — CI ubuntu-latest job으로 PR마다 자동 차단.

## 전신 프로젝트

이 리포는 [`D:\robot\dev\nrobotcheck`](../nrobotcheck)(Electron 데스크톱 앱, v2.0 DDD 리팩토링 중)에서 상업화 전략 검토 결과 분리 개설되었습니다. 전신의 CIS·ROS2 벤치마크 자산과 도메인 설계 개념을 차용하되, 코드는 완전히 새로 작성합니다.

배경: `D:\robot\dev\nrobotcheck\docs\COMMERCIALIZATION_STRATEGY.md`

자산 승계 Tier 분류: [`docs/design/12-migration-and-non-goals.md`](./docs/design/12-migration-and-non-goals.md) §12.2
