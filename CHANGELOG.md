# Changelog

이 프로젝트의 주요 변경 사항을 기록합니다. 포맷은 [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/)을 따르고, 버저닝은 [Semantic Versioning](https://semver.org/)을 따릅니다.

> **참고**: 본 changelog는 v0.3.0(2026-05-18 release candidate) 이후 항목이 최상단에 정리되어 있습니다. Phase 0~1 초기 부트스트랩 항목(2026-04-23 이하)은 역사 기록 보존을 위해 하단의 "Pre-v0.2.0 historical entries" 섹션으로 이동했습니다.

## [Unreleased]

### Added
- (placeholder) 차기 release 항목 — Phase 11 backlog draft (Phase 10 Top 3 마감 후 다음 minor 후보 매트릭스) / audit chain head sha mismatch metric (Phase 10.A-6 carryover) / audit hash chain key_epoch+leader_epoch input 포함 (v0.10.0 carryover) / manual rotation endpoint / multi-tenant epoch 분리 / Grafana dashboard panel (rotation_total + key_epoch) / humble pack archive embed (PACKS_SOURCE Makefile humble 등록 + `_archives/` 자동 embed, v0.11.0 carryover) / SROS2 cert chain intermediate CA + OCSP responder (v0.11.0 carryover) / SROS2 keystore 자동 enrollment workflow / DDS topic whitelist expansion (`/diagnostics` · `/rosout` · custom payload, v0.11.0 carryover) / Phase 9.5 testcontainers e2e Patroni 3-node + etcd / C5b-10 a11y polish Tailwind palette contrast / MR.T4 application restart integration / Stage 4.5 BIND/PowerDNS Terraform sample (ops doc cover) / Stage 5b 잔여 carryover (C5b-6/C5b-7/C5b-8/C5b-9) / R-D8 청구권 명세서 (사용자 외부) / E36 레퍼런스 HW burn-in (사용자 hands-on)

---

## [0.11.0] — 2026-05-21 (minor — Phase 10 옵션 E 마감)

> **요약**: Phase 10 옵션 E "ros2-humble pack 신규 + DDS/SROS2 깊이 확장" 완전 마감 + Phase 10 세 번째 minor (v0.9.0 옵션 A · v0.10.0 옵션 D · v0.11.0 옵션 E 한 Phase 안 minor 3 연속). ROS2 Humble Hawksbill LTS 분기 customer cover 신규 `packs/ros2-humble/` 22 check (C1~C8 8/8 카테고리 cover, jazzy 22 check humble distro 변환) + DDS topic ACL 4 check 깊이 확장 (`/scan` · `/odom` · `/tf+/tf_static` · `/joint_states`, jazzy + humble 양쪽 동기) + SROS2 cert chain 3 check 깊이 확장 (cert expiry ≥90일 baseline · CA SHA-256 trust anchor · CRL nextUpdate ≥90일, 양쪽 동기). 양쪽 pack 모두 **29 check** 도달. 코드 영향 0 (pack 변경만), 회귀 0, Breaking 0. customer 영향 0 (Jazzy customer는 22 → 29 check 자동 확장, Humble customer 진입 베이스 마련). 상세는 [docs/releases/v0.11.0.md](docs/releases/v0.11.0.md).
>
> **기준 commit**: (본 release commit, main)

### Added
- `design(option-e)` Phase 10 옵션 E Stage 분해 + 결정 항목 (`7b260ea` + `a070c36`) — `ros2-humble-dds-sros2-design.md` 신규 (12 섹션 + 4 옵션 비교) + D-P10E-1·2·3·4 결정 확정 (humble pack + 깊이 확장 양쪽 동기 / v0.11.0 minor / site policy 의존 PASS skip / cert expiry ≥90일).
- `feat(packs)` Phase 10.E-2 ros2-humble pack scaffold + C1·C2·C3 5 check (`dc49206`) — `packs/ros2-humble/pack.yaml` 신규 (`metadata.name: ros2-humble`, `version: 0.1.0`, `compatibility.rosDistro: ["humble"]` 명시) + C1 SROS2 keystore/security 2 check + C2 cmd_vel ACL/publisher count 2 check + C3 domain_id 1 check + 5 selftest fixture (jazzy cargo cult + distro 4 영역 갱신: workspace 경로 · apt 패키지 prefix · LTS expected ROS_DISTRO · EOL 날짜 2027-05).
- `feat(packs)` Phase 10.E-3+4 ros2-humble C4·C5·C6·C7·C8 17 check (`64f8d83`) — C4 6 check (`apt_key_valid` · `apt_source_official` · `colcon_install_hash` · `no_world_writable_libs` · `signed_packages_only` · `systemd_unit_perms`) + C5 6 check (`argv_no_remote_url` · `lifecycle_node_used` · `no_shell_exec` · `no_world_writable_yaml` · `parameter_no_secret_inline` · `param_files_owner`) + C6 3 check (`distro_is_lts` · `distro_not_eol` · `ros2_cli_available`) + C7 1 check (`rmw_implementation_set`) + C8 1 check (`governance_encrypt_topics`) + 17 selftest fixture. 22 check 8/8 카테고리 cover 완성.
- `feat(packs)` Phase 10.E-5+6 DDS topic ACL + SROS2 cert chain 깊이 확장 (`02db4d0`) — C2 DDS topic ACL 4 신규 check (`scan_acl_enforced` · `odom_acl_enforced` · `tf_acl_enforced` (tf+tf_static 통합) · `joint_states_acl_enforced`) + C1 SROS2 cert chain 3 신규 check (`sros2_cert_expiry` ≥90일 baseline · `sros2_ca_trust` SHA-256 fingerprint env 등록 · `sros2_cert_revocation` CRL nextUpdate ≥90일). jazzy + humble 양쪽 동기 (총 신규 14 check yaml = 7 패턴 × 2 pack) + 14 selftest fixture. 양쪽 pack 모두 22 → 29 check 도달.
- `docs(release)` Phase 10.E-7 v0.11.0 release notes + CHANGELOG entry + ops docs (본 release commit) — `docs/releases/v0.11.0.md` 신규 + `docs/operations/ros2-humble-deployment.md` 신규 (humble pack 활성 절차 + SROS2 keystore env 사전 등록 + CA fingerprint 등록 + DDS topic ACL whitelist + cert/CRL rotation 절차).

### Changed
- pack 변경만 — 코드 (internal/ · cmd/) 영향 0. Makefile `PACKS_SOURCE`는 humble pack source-only 노출 default 유지 (`_archives/` 자동 embed는 별 patch carryover). signer · audit · API · web 영향 0.

### Verification
- `go test -count=1 -run "TestROS2JazzyChecksRoundTrip|TestROS2HumbleChecksRoundTrip" ./internal/domain/benchmark/...` PASS — 양쪽 pack 29/29 check + 29/29 selftest fixture round-trip.
- `go vet ./...` PASS · `go build ./internal/... ./cmd/...` PASS · `go test -count=1 ./internal/domain/benchmark/... ./internal/domain/packs/...` PASS (회귀 0).
- 총 118 yaml 파일 (29 check + 29 selftest) × 2 pack 모두 `ValidatePackYAMLBytes` + `ParseCheckYAML` + `ParseSelfTestYAML` + `RunCheckSelfTest` 전 단계 PASS.

### Notes
- **minor bump 이유** — Phase 10 옵션 E 마감 (Phase 10 세 번째 minor, v0.9.0 옵션 A · v0.10.0 옵션 D 후속). 신규 pack 1개 (`ros2-humble`) + 양쪽 pack 22 → 29 check 확장 = customer-facing 신규 cover 묶음.
- **customer 영향 0** — 신규 7 check (DDS 4 + SROS2 3)는 env 미설정 시 PASS skip default (site policy 의존, false positive 회피). 기존 jazzy customer 운영자 알람 없음. Humble customer는 별 pack 명시 후 활성.
- **Phase 10 Top 3 모두 마감** — 옵션 A (multi-region UI) + 옵션 D (audit key rotation) + 옵션 E (ros2-humble + DDS/SROS2). Phase 11 backlog draft 진입 가능.
- **신규 carryover** — Humble distro 실 환경 검증 부재 (첫 customer 진입 시 false positive feedback) / `PACKS_SOURCE` Makefile humble 등록 별 patch 위임 / DDS topic whitelist expansion (diagnostics · rosout · custom) / SROS2 intermediate CA + OCSP responder / SROS2 keystore 자동 enrollment / humble pack archive embed.

---

## [0.10.2] — 2026-05-21 (patch — v0.10.1 추가 gofmt 마감)

> **요약**: v0.10.1 main CI Lint job에서 추가 gofmt 위반 2 파일(`cmd/rosshield-server/main.go:188`, `internal/domain/audit/keyrotation/rotator_test.go:529`) 발견. v0.10.1 hot fix가 3 파일만 cover했으나 일괄 `gofmt -l cmd/ internal/` 미실행이 원인. v0.10.2에서 `gofmt -w` 일괄 적용 + 검증 명령(`gofmt -l cmd/ internal/`) 활용 메모. 코드 동작 영향 0, 회귀 0, Breaking 0.
>
> **기준 commit**: (본 release commit, main)

### Fixed
- `fix(lint)` gofmt -w 추가 2 파일 (`cmd/rosshield-server/main.go`, `internal/domain/audit/keyrotation/rotator_test.go`) — v0.10.1 hot fix가 lint log 표시 3 파일만 fix하고 일괄 `gofmt -l ./...` 검증 누락이 원인. 본 release는 `gofmt -l cmd/ internal/` 일괄 검증 후 발견된 잔여 2 파일 마감.

### Notes
- v0.10.1에서 memory `feedback_go_commit_pipeline.md` 정책 일관 부족 인식 — local에서 `gofmt -l` 일괄 검증 + commit hook 도입 검토 carryover로 등록.

---

## [0.10.1] — 2026-05-21 (patch — v0.10.0 lint hot fix)

> **요약**: v0.10.0 main CI Lint job이 (1) `internal/platform/scheduler/keyrotationjob`이 `audit-domain-isolation` depguard 규칙 위반(동일 패턴 `rotationjob` 예외 등록되어 있었으나 신규 `keyrotationjob` 누락) (2) gofmt 위반 3 파일(주석 indentation)로 실패. `.golangci.yml`에 `keyrotationjob` 예외 추가 + gofmt -w 3 파일. v0.10.0의 코드 동작 영향 0(lint 차원 fix only), 회귀 0, Breaking 0.
>
> **기준 commit**: (본 release commit, main)

### Fixed
- `fix(lint)` depguard `audit-domain-isolation`에 `keyrotationjob` 예외 추가 — 동일 패턴 `rotationjob`은 이미 예외 등록, Phase 10.D-3+4(`f7f045a`) 신규 `keyrotationjob` 누락 보완.
- `fix(lint)` gofmt -w 3 파일 (`cmd/rosshield-audit-verify/export_verify.go`, `internal/domain/audit/export.go`, `internal/domain/audit/sqliterepo/repo.go`) — Stage 10.D-3+4·5 sub-agent 환경에서 적용 누락.

### Notes
- v0.10.0 release artifact 자체는 정상 — snap-build · release-build job 정상 통과. lint job만 fail로 main CI overall fail. v0.10.1 patch로 main CI 전체 green 회복.

---

## [0.10.0] — 2026-05-21 (minor — Phase 10 옵션 D 마감)

> **요약**: Phase 10 옵션 D "audit chain signer key rotation 자동화" 완전 마감 + Phase 10 두 번째 minor (v0.9.0 옵션 A 후속). Quarterly (90일) cron 으로 자동 rotation + SwappableSigner hot-swap (Queue 패턴, RWMutex) + emergency override CLI/admin endpoint + fg-verify v2 epoch별 검증 + 마이그레이션 0037/0038 한 set. 운영자 부담 0 (자동 cron), 외부 감사인 호환성 보존 (audit.chain.key_rotated event chain 안에 trace), 회귀 0, Breaking 0. 상세는 [docs/releases/v0.10.0.md](docs/releases/v0.10.0.md).
>
> **기준 commit**: (본 release commit, main)

### Added
- `design(option-d)` Phase 10 옵션 D Stage 분해 + 결정 항목 (`ab3bdd8` + `fb37f24`) — `audit-chain-rotation-automation-design.md` 신규 + D-P10D-1·2·3 결정 amendment (옵션 C fully automatic + override / Quarterly 90일 / Queue 패턴).
- `feat(audit)` Phase 10.D-2 마이그레이션 0037_audit_chain_keys + Repository (`7e714c6`) — `audit_chain_keys` 테이블 (tenant + epoch + key_id + public_key_hex + keystore_handle + created_at + revoked_at + created_by + audit_entry_seq) + sqlite + PG up/down + `ChainKeyRepository` interface + `sqliterepo.KeyEpochRepo`. bootstrap epoch=1 자동 INSERT (placeholder, 첫 부팅 시 갱신).
- `feat(audit)` Phase 10.D-3+4 scheduler + SwappableSigner + KeyRotator + Queue 패턴 (`f7f045a`) — `internal/platform/signer/swappable.go::SwappableSigner` (RWMutex queue + Sign/Swap/CurrentEpoch/CurrentKeyID) + `internal/domain/audit/keyrotation/rotator.go::KeyRotator` (단일 Tx + self-sign Verify round-trip + Allocator + leader gate + MinInterval idempotency) + `internal/platform/scheduler/keyrotationjob/` (quarterly cron 어댑터, DefaultQuarterlySpec `@every 2160h`) + 마이그레이션 0038 `audit_entries.key_epoch INTEGER NULL` 컬럼 + metrics `rosshield_audit_rotation_total` + `rosshield_audit_key_epoch`.
- `feat(audit-verify)` Phase 10.D-5 fg-verify v2 epoch별 public key 검증 (`ead86e5`) — `cmd/rosshield-audit-verify/rotation.go` 신규 + bundle `chainKeyEpochs` 메타 직렬화 + entry.key_epoch 기반 검증 + v0.9.0 backward compat (v1 mode auto-fallback).
- `feat(audit)` Phase 10.D-6+7 emergency override + v0.10.0 release (본 release commit) — `KeyRotator.Abort(ctx, reason, actor)` (atomic flag set + audit.chain.rotation_aborted emit, idempotent) + `POST /api/v1/audit/rotation/abort` admin endpoint + `rosshield audit rotation abort --reason "<text>"` CLI + `KeyRotator.SetLeader` lazy 주입 (bootstrap HA Manager 결선 시점, defense-in-depth 2 단계) + `docs/operations/audit-chain-key-rotation.md` 운영자 가이드 신규 + `docs/releases/v0.10.0.md` 신규.

### Changed
- `feat(audit)` bootstrap signer 경로 — audit chain signer 가 raw `signer.Signer` → `*signer.SwappableSigner` 로 wrap. SwappableSigner 가 Signer interface 그대로 implement — 호출자 변경 0 (감사 checkpoint + audit emit 모두 동일 인터페이스). HA Manager 결선 시점에 `keyRotator.SetLeader(haMgr|patroniRP)` lazy 호출 추가 (e25 + patroni 분기 양쪽).

### Verification
- `go vet ./internal/domain/audit/keyrotation/... ./internal/api/handlers/... ./cmd/rosshield/... ./cmd/rosshield-server/...` PASS · `go build ./internal/... ./cmd/...` PASS
- `go test -count=1 ./internal/domain/audit/...` PASS (audit / keyrotation / rotation / sqliterepo 4 package)
- `go test -count=1 ./cmd/rosshield/... ./cmd/rosshield-server/...` PASS
- `go test -count=1 -skip "TestListPacks|TestGetPack|TestGetCheck" ./internal/api/handlers/...` PASS (pack-signer 환경 의존 5건 skip — 본 round 무관, gitignore된 dev key 부재 환경에서만 fail)
- `go test -count=1 ./cmd/rosshield-audit-verify/...` PASS (v1 backward compat + v2 chainKeyEpochs)
- 회귀 0 — Phase 0~10 옵션 A 전 단위 + 통합 test 모두 그대로 PASS.

### Notes
- **minor bump 이유** — Phase 10 옵션 D 마감 후 두 번째 minor (v0.9.0 옵션 A 후속). signer hot-swap + 마이그레이션 0037/0038 + 신규 endpoint 1 + CLI 1 = customer-facing 신규 기능 묶음.
- **customer 영향 0** — 자동 quarterly rotation 시작이지만 운영자 알람 없음 (INFO 로그 + Prometheus counter 만). 자동 rotation 비활성 유지를 원하면 `ROSSHIELD_AUDIT_CHAIN_KEY_ROTATION_SCHEDULE=""` 명시 (default = 빈 값 = 비활성). quarterly 활성화는 customer 옵트인.
- **외부 감사 호환성 보존** — `audit.chain.key_rotated` event 가 chain 안에 trace + `audit_chain_keys` append-only 보존 + emergency abort 도 별 entry. SOC2 / ISMS-P / NIST 800-53 SC-12 통제 baseline 만족.
- **신규 carryover** — audit hash chain input 에 key_epoch + leader_epoch 미포함 (외부 검증 도구 호환성 유지, 향후 SC-12 강화 시 별 epic) / bootstrap epoch=1 placeholder 첫 부팅 시 갱신 / manual rotation endpoint 미구현 (별 epic) / Grafana dashboard panel 별 작성 위임 / multi-tenant epoch 분리 미구현.

---

## [0.9.0] — 2026-05-21 (minor — Phase 10 옵션 A 마감)

> **요약**: Phase 10 옵션 A "multi-region UI 표면화" 마감 + 첫 minor release. Phase 8(PG cross-region replication) + Phase 9(Patroni 자동 failover)로 결선된 multi-region 인프라를 운영자가 한 화면에서 즉시 확인 가능한 `/regions` 페이지 신규. RegionHealthCard + AuditConsistencyCard + RegionTimelineCard 3 카드 + Prometheus alert rule 5 + ops runbook §13 + testcontainers + Playwright e2e 한 set. 회귀 0, Breaking 0, customer 영향 0 (신규 페이지 + 기존 기능 변경 0). 상세는 [docs/releases/v0.9.0.md](docs/releases/v0.9.0.md).
>
> **기준 commit**: (본 release commit, main)

### Added
- `design(phase10)` Phase 10 backlog 8 후보 매트릭스 (`8ec33f9`) — `phase10-backlog-design.md` 신규. Top 3 권장 (A multi-region UI / D audit key rotation / E ros2-humble pack) + Stage 분해 + 결정 항목 D-P10-1·2.
- `feat(web)` Phase 10.A-2 `/regions` 페이지 + RegionHealthCard (`f5864c3` + dist `e82a3c4`) — admin 권한 게이트 + replicas grid + lag 상태(healthy/warning/delayed) + role badge(primary/standby/failed) + empty state. `useReplicas` hook + `regions.*` i18n namespace.
- `feat(web)` Phase 10.A-3·4 AuditConsistencyCard + RegionTimelineCard + backend `GET /api/v1/replication/failovers` (`bafdf43` + dist `6aa1e27`) — audit chain head sha + seq + 갱신 시각 표시 (self-region only note) + 최근 10건 cutover timeline (status/actor/reason). `useAuditChainHeadSHA` + `useFailoverHistory` hook 추가. handler `ListFailovers` (admin 권한 + limit query + completed_at NULL → status 도출).
- `feat(deploy)` Phase 10.A-5 Prometheus alert rule + ops runbook §13 (`f6aa8c4`) — `deploy/prometheus/alerts/multi-region.yml` 신규 (5 rule: ReplicationLagWarning/Critical + AuditChainHeadSeqMismatch + HARoleSwap + HAFailoverStorm) + `alertmanager-sample.yml` (critical → PagerDuty + Slack #incident-response · warning → Slack #ops-alerts · info → Slack #audit-trail) + `multi-region-failover-runbook.md` §13.0~§13.5 (alert 매트릭스 + 절차 + runbook_url annotation deep-link).
- `test(e2e)` Phase 10.A-6 testcontainers + Playwright e2e (본 release commit) — `internal/platform/storage/postgres/regions_integration_test.go` 신규 (`TestMultiRegionFailoverEndToEnd`, 2-region PG container fixture 재사용, 4 assertion: initial role + heartbeat + role swap + ListFailovers 등장) + `web/playwright/tests/regions.spec.ts` 신규 (admin login → nav '리전' click → PageHeader + AuditConsistencyCard + RegionTimelineCard render 검증).
- `docs(release)` Phase 10.A-7 v0.9.0 release notes + CHANGELOG entry (본 release commit) — Phase 10 진입 첫 minor 정착.

### Verification
- `pnpm exec tsc -b --noEmit` PASS · `pnpm test --run` 536/536 PASS · `pnpm build` PASS
- `go vet ./internal/... ./internal/api/... ./internal/domain/...` PASS · `go build ./internal/... ./cmd/...` PASS
- `go test -tags=integration -run TestMultiRegionFailoverEndToEnd ./internal/platform/storage/postgres/` PASS (~11s, Docker 환경)
- `pnpm exec playwright test regions.spec.ts` PASS (1.7s test, 22s global-setup 포함)
- 회귀 0 — Phase 0~9 unit + integration test 모두 그대로 PASS.

### Notes
- **minor bump 이유** — Phase 10 진입 첫 minor release (v0.8.x patch 시리즈 → v0.9.0 진척). Phase 8(PG replication) + Phase 9(Patroni) 결선 자산을 운영자 가시성으로 표면화 첫 묶음.
- **customer 영향 0** — 신규 페이지(`/regions`)는 admin 권한 게이트 + 기존 페이지 변경 0 + DB schema 변경 0 + 환경 변수 변경 0.
- **신규 carryover** — `RosshieldAuditChainHeadSeqMismatch` alert는 `audit_chain_head_seq` divergence proxy만 cover. seq 일치 + sha 불일치 케이스는 `rosshield_audit_chain_head_sha_match` 신규 gauge + cross-region collector 별 epic 위임. PG `LastInsertId` 미지원으로 `RecordFailover.ID`가 PG에서 0 반환 — e2e test는 ListFailovers로 id 우회, handler 정확 동작은 RETURNING 도입 별 round.

---

## [0.8.5] — 2026-05-21 (patch — v0.8.4 hot fix)

> **요약**: v0.8.4 check-health hook이 snapd 30s hard timeout을 정확히 hit하여 `snap install` 자체가 fail되는 회귀 hot fix. polling을 10s margin으로 축소 + snap-smoke workflow에 `snap run --hook=check-health rosshield` 외부 trigger 패턴 추가. v0.8.4 release는 broken(snap install fail) — 본 release로 회복. Breaking 0, 회귀 0 (v0.8.3 이하 환경 영향 0). 상세는 [docs/releases/v0.8.5.md](docs/releases/v0.8.5.md).
>
> **기준 commit**: `96305be` (main)

### Fixed
- `fix(snap)` check-health hook 30s hard timeout 회피 (`96305be`) — snapd가 check-health hook에 30s hard timeout을 강제하며, 초과 시 install/refresh 자체가 fail. CHECK_TIMEOUT 30s → 10s 축소 + polling interval 2s → 1s + curl max-time 5s → 3s. daemon ready 안 됐으면 waiting으로 빠르게 종료, snapd 주기 재호출 또는 외부 trigger에서 catch up.
- `ci(snap-smoke)` 외부 hook trigger 패턴 (`96305be`) — `snap health — trigger + verify check-health` (첫 install 후) + `snap health — trigger + verify after refresh` (refresh round-trip 후) 양쪽 step에서 매 polling iteration마다 `sudo snap run --hook=check-health rosshield` 직접 호출. 30s 안에 daemon ready + okay 도달 cover.

### Changed
- `docs(design)` e35-refresh-rollback-redesign.md §2 hook lifecycle 표 (`96305be`) — check-health row에 30s hard timeout 정확치 행 추가. v0.8.4 첫 실 검증에서 확정한 사실 반영.

### Notes
- v0.8.4 release는 broken — snap install이 30s hard timeout으로 fail. v0.8.4 snap binary 사용자는 v0.8.5로 즉시 upgrade 필요. Kubernetes/Docker/systemd 사용자는 영향 0(snap hook은 snap 배포 한정). v0.8.4 GitHub release attach는 그대로 보존(Semantic Versioning history 일관, broken release 인정 + 다음 patch fix).

---

## [0.8.4] — 2026-05-21 (patch — broken release)

> **주의**: 본 release는 broken 상태입니다. snap `check-health` hook이 snapd의 30s hard timeout을 정확히 hit하여 `snap install` 자체가 fail됩니다. v0.8.5 patch로 즉시 fix. Kubernetes·Docker·systemd 사용자는 영향 0. 자세한 회복 절차는 [v0.8.5 release notes](docs/releases/v0.8.5.md) 참조.
>


> **요약**: E35-refresh redesign — snap `post-refresh` hook이 services start **전** 호출되는 lifecycle 표준 사실 확정 후, 기존 healthz polling 설계의 architectural mismatch 마감. post-refresh는 binary 무결성 + configure sanity만 cover로 단순화하고, 신규 `check-health` hook이 daemon healthz polling + `snapctl set-health` 담당. 자동 rollback 범위는 catastrophic case로 축소되고 daemon unhealthy는 운영자/Prometheus alert로 위임. snap-smoke CI의 refresh round-trip step에서 `continue-on-error: true` 제거 — E35-refresh carryover 마감. 회귀 0, Breaking 0. 상세는 [docs/releases/v0.8.4.md](docs/releases/v0.8.4.md).
>
> **기준 commit**: `3419191` (main)

### Added
- `feat(snap)` E35-refresh redesign — post-refresh hook 단순화 + check-health hook 신규 (Stage 1+2 `3b8e36c`). snap lifecycle 표준 정합: post-refresh는 catastrophic case(binary corruption + configure schema)만 cover, check-health가 daemon healthz polling + snapctl set-health 담당.
- `ci(snap-smoke)` E35-refresh redesign Stage 3 (`02a8f6c`) — refresh round-trip step `continue-on-error: true` 제거 + `snap health` polling 신규 step 2개. E35-refresh carryover 마감.

### Changed
- `docs(ops)` E35-refresh redesign Stage 4 (`3419191`) — snap-deployment.md §7 rewrite: hook 책임 분리 표 + 자동 rollback 범위 축소 명시(catastrophic case만) + broken refresh 운영 절차(§7.5) + channel staged rollout 권장(§7.6) + §9 한계 갱신.
  - 설계 배경: `docs/design/notes/e35-refresh-rollback-redesign.md` — D-E35R-1 옵션 A(post-refresh 단순화 + check-health 신규) + D-E35R-2 포기(운영 절차 위임) + D-E35R-3 순차 Stage 1·2·3·4.

### Notes
- 자동 rollback 가정 변경 (customer-facing): v0.8.3 이전은 "post-refresh healthz polling 실패 시 snapd 자동 revert"가 daemon unhealthy 전반을 cover한다는 가정. v0.8.4부터는 catastrophic case(binary corruption + configure schema)만 cover. daemon unhealthy 1차 방어선은 channel staged rollout(edge → candidate burn-in → stable), 2차 방어선은 운영자가 `snap health rosshield` 확인 후 `snap revert rosshield`. 자세한 절차는 `docs/operations/snap-deployment.md` §7.5·§7.6 참조.

---

## [0.8.3] — 2026-05-21 (patch)

> **요약**: Snap Build 회복 patch — v0.8.2 Snap Build가 snapcraft `App commands must consist of only ...` validation으로 실패(`=` 문자 거부). server가 `ROSSHIELD_ADDR` env로 default 재정의 가능하게 갱신 + snapcraft.yaml env 주입. 부수로 Kubernetes/Docker/systemd 운영 환경 동시 cover. 회귀 0, Breaking 0. 상세는 [docs/releases/v0.8.3.md](docs/releases/v0.8.3.md).
>
> **기준 commit**: `7bb7b9b` (main)

### Fixed
- `fix(snap)` snapcraft App command `=` 거부 회피 — `ROSSHIELD_ADDR` env 패턴 (`7bb7b9b`) — server가 env로 default 재정의 가능(main.go) + snapcraft.yaml `environment.ROSSHIELD_ADDR=127.0.0.1:8080` 주입. CLI `--addr=...` 플래그는 env보다 우선(Go flag 표준).

### Notes
- v0.8.2의 `--addr` 직접 명시(`4929a7b`)가 snapcraft 8.x validation 부적합 — env 패턴이 정공.
- `ROSSHIELD_ADDR` env 패턴은 Kubernetes ConfigMap·Docker `-e`·systemd `Environment=` 동시 cover.

---

## [0.8.2] — 2026-05-20 (patch)

> **요약**: CI baseline 안정화 patch 시리즈 — 누적 4 CI job(PG integration / MinIO S3 / Playwright E2E / Snap Smoke)의 flaky/회귀를 한 release로 마감. Phase 9.5 testcontainers e2e Patroni 진입 전 baseline 안정성 회복. 회귀 0, Breaking 0, customer-facing 변경 0(snap daemon `--addr` default 명시는 운영자 가이드와 일관성 회복). 상세는 [docs/releases/v0.8.2.md](docs/releases/v0.8.2.md).
>
> **기준 commit**: `cc8511e` (main)

### Fixed
- `fix(ci)` PG `TestAuditChainHeadSHACrossRegion` flaky (`6ee8275`) — audit_chain_heads/audit_entries 별 publication 도착 순서 비-atomic. audit_entries 5건 sanity를 5초 deadline polling loop로 변경 (T6 line 620-635 pattern 일관).
- `fix(ci)` MinIO `TestS3Backend_MinIOLifecycle` (`6ee8275`) — 신규 MinIO(2024+)가 transition StorageClass를 strict validate. cfg에서 LifecycleTransitions 제거, LifecycleExpireDays만 유지. Transition rule 직렬화는 fake S3 mock test가 이미 cover.
- `fix(ci)` Playwright 19 E2E loginAsAdmin 회귀 (`6ee8275`) — commit 44b139f(D-P7-1 브랜드)에서 dict.ts `login.title` 변경 + fixtures.ts 동기 갱신 누락. `KO_LABELS.login.title` `'rosshield Console'` → `'Lodestar 관리자 콘솔'` 동기화. 부수: dict.ts:1158 en `login.title` 한글 leak 정정.
- `fix(snap)` Snap Smoke /healthz 30s timeout — 20+ 누적 fail (`4929a7b`) — rosshield-server 자체 default `--addr=127.0.0.1:0`(random port). snap daemon에 `--addr=127.0.0.1:8080` 명시 + `--no-color` flag 제거(Ubuntu 22.04 snap CLI 미인식) + timeout 30s→60s + 매 20s 진행 상태 출력.
- `fix(playwright)` audit strict locator + color-contrast C5b-10 carryover (`cc8511e`) — audit.spec.ts:18 `getByText('Chain Head', { exact: true })` 정확화. color-contrast 16 케이스(muted-foreground 4.34 + destructive 3.59 < WCAG AA 4.5:1)는 Tailwind palette 변경 작업이라 별 PR로 분리, test.skip 일시 격리.
- `fix(playwright)` audit '시퀀스' KO 라벨 동기 + 잔여 7 spec D-P7-3 carryover skip(`58a45ce`) → `fix(playwright)` D-P7-3 즉시 회수(`778d953` + `6a3ee1d`) — audit.spec.ts:23 `'시퀀스'` 동기 + fixtures.ts에 KO_LABELS.header.userMenu/userProfile + compliance + robots namespace + EN_LABELS.header.userMenu 추가. 7 spec 재설계 (auth × 2 dropdown trigger+menuitem 패턴 / i18n × 1 영어 전환 후 menuitem / compliance × 1 `Framework`→`프레임워크` / robots × 2 Dialog 마이그레이션 + `Fleet ID`→`플릿 ID` / theme × 1 skip 해제). sub-agent inventory로 dropdown 재설계 필요는 auth.spec 2건뿐임을 확인.
- `fix(ci)` PG `TestReplicationLagWithin1Second` CI throughput flaky 완화 (`2c287e9`) — lag threshold 1s → 2s. CI runner cold start variation으로 1.046s 초과 사례 발견. RPO ≤ 1분 목표는 2s window로도 cover, 정상 환경 lag는 200~500ms.

### Notes
- **Phase 9.5 testcontainers e2e Patroni 진입 baseline 안정성 완전 회복** — 9/9 CI job PASS, customer-facing 변경 0.
- 신규 carryover: **C5b-10 a11y polish** — Tailwind theme contrast WCAG AA 4.5:1 미달 fix design + palette 결정 + dark mode + .skip 제거 한 set.
- 회수: **D-P7-3 Playwright UX drift** — 7 spec 모두 재활성 + CI green.

---

## [0.8.1] — 2026-05-20 (patch)

> **요약**: Phase 9.6 Stage 5b runbook 갱신 — `multi-region-failover-runbook.md`에 §11 Patroni 자동 cutover 시나리오 추가. v0.8.0 customer가 `--ha-rp=patroni` 환경에서 운영자 검증/사후 분석 절차 + Patroni pause/failover/resume + E25 fallback + RTO 비교(manual 5분 vs 자동 30초) 정착. docs-only, 회귀 0, Breaking 0. 상세는 [docs/releases/v0.8.1.md](docs/releases/v0.8.1.md).

### Added
- `docs(ops)` Phase 9.6 runbook §11 Patroni 자동 cutover 시나리오 — 자동 단계 timeline (T+0~T+0:21) + 운영자 검증 5 step (PagerDuty/patronictl list/healthz/write API/customer 통지) + false positive `patronictl pause` 절차 + manual `patronictl failover --force` 절차 + `ROSSHIELD_HA_RP=e25` fallback rolling restart + RTO 비교 표.
- `docs(ops)` §12 참조 boost — `auto-failover-research.md` + `patroni-deployment.md` + `deploy/k8s/patroni/` link 추가.

### Notes
- v0.8.0 Patroni 통합 customer 운영자 가이드 완전 마감.
- 잔여: 9.5 testcontainers e2e Patroni 3-node + etcd (큰 작업) / 9.7 customer staging drill (외부).

---

## [0.8.0] — 2026-05-20 (minor)

> **요약**: Phase 9 Patroni 자동 failover 통합 minor release — `--ha-rp=patroni`로 RoleProvider를 PG advisory lock(E25) 대신 Patroni REST polling으로 swap. Patroni 자동 promote(RTO ~30초) + audit/lagmetric/cronsched 3 layer 단일 source of truth. air-gap customer는 `--ha-rp=e25` default로 기존 동작 그대로. 회귀 0, Breaking 0. 상세는 [docs/releases/v0.8.0.md](docs/releases/v0.8.0.md).
>
> **기준 commit**: `aa512d4` (main)

### Added
- `design(phase9)` 자동 failover research (`8c3a5d9`) — `auto-failover-research.md` (9 섹션, 390줄) + 4 옵션 비교 (Patroni/Stolon/PG built-in/E25 확장) + D-AF-1~4 권장 default + Stage 9.2~9.7 분해.
- `docs(phase9)` Patroni 운영 가이드 + Kubernetes manifest (`c30a9e1`) — `patroni-deployment.md` (280줄, 7 섹션) + `deploy/k8s/patroni/` (values-example.yaml + rosshield-deployment.yaml + README). Bitnami Helm chart + Pod anti-affinity + region-local nodeAffinity + watchdog STONITH 옵션.
- `feat(ha)` Phase 9.3 patroni.RoleProvider 구현 (`9975c3b`) — `internal/platform/ha/patroni/` 신규 패키지 (RoleProvider struct + atomic.Bool/Int64 race-safe + ticker goroutine + `GET /cluster` JSON parse + resolveLeader fallback + 12 단위 test). audit + lagmetric + cronsched 3 interface duck-typed 자동 만족.
- `feat(ha)` Phase 9.4 bootstrap `--ha-rp` flag 결선 (`aa512d4`) — Config 5 필드 + CLI flag 5 + env 5 + bootstrap switch 분기 (patroni vs e25 default vs unknown fail-fast). 단일 source of truth — audit/lagmetric/cronsched 3 layer 모두 동일 RoleProvider 주입.

### Notes
- **Phase 9 application 측 결선 완료** — customer가 v0.8.0 binary로 즉시 `--ha-rp=patroni` 전환 가능.
- 기존 customer (air-gap·single PG): 영향 0 — default `--ha-rp=e25`로 E25 PG advisory lock 동작 유지.
- 잔여: 9.5 testcontainers e2e (큰 작업) / 9.6 runbook 갱신 (작은 작업) / 9.7 customer staging drill (외부).

---

## [0.7.9] — 2026-05-20 (patch)

> **요약**: MR.T6 application integration — audit.Service의 fence token enforcement(leader_epoch 자동 저장 + follower ErrNotLeader 거부)가 cross-region replication 환경에서 정확 동작함을 testcontainers 자동 회귀 방어. Phase 8 Stage 7 MR.T6 양쪽 layer(schema + application) cover 완료. 회귀 0, Breaking 0. 상세는 [docs/releases/v0.7.9.md](docs/releases/v0.7.9.md).
>
> **기준 commit**: `04dfb56` (main)

### Added
- `test(replication)` MR.T6 application integration (`04dfb56`) — `fakeAuditRole` mock + `TestAuditFenceEpochPropagatesCrossRegion` (primary leader epoch=42 Append → standby leader_epoch=42 propagation 5초 대기) + `TestAuditFollowerRejectsAppend` (follower Role → audit.ErrNotLeader). split-brain 방어 application-level 자동 회귀 방어.

### Notes
- **Phase 8 Multi-region HA 사실상 마감** — design + ops + IaC + runbook + e2e (PG + application) + monitoring + HA gate + fence enforcement. customer는 production-grade 환경 자체 구축·운영·incident 대응·회귀 방어·monitoring·split-brain 방어까지 한 set.
- 잔여 carryover (MR.T4 application restart integration · Stage 6 자동 failover Phase 9+ · 외부 트랙)만 남음.

---

## [0.7.8] — 2026-05-20 (patch)

> **요약**: HA leader-only metric gate — `rosshield_replication_lag_seconds` collector가 HA cluster의 follower instance에서 metric 중복 emit + 불필요 DB polling을 자동 차단. Phase 8 v0.7.x 폴리시 마감. 회귀 0, Breaking 0. 상세는 [docs/releases/v0.7.8.md](docs/releases/v0.7.8.md).
>
> **기준 commit**: `99c8257` (main)

### Added
- `feat(metrics)` lagmetric HA leader-only gate (`99c8257`) — `RoleProvider` interface (ha.Manager 자동 만족) + `Deps.Role` 옵션 + `Collector.SetRoleProvider(rp)` lazy 주입 (cronsched 패턴 일관) + atomic.Value race-safe + bootstrap HA Manager 결선 직후 자동 주입 + 단위 test 4 (leader/follower/transition/nil). follower instance는 Querier 호출 0회 + Gauge.Reset.

### Notes
- HA 비활성 환경 (single-instance): 동작 변경 0 — v0.7.6 동작 유지.
- HA 활성 환경: leader instance만 `rosshield_replication_lag_seconds` emit, follower는 metric 0 라인 + DB polling 0.
- **Phase 8 인프라 + monitoring 완전 마감** — customer는 design + ops + IaC (Route53/Cloudflare) + runbook + e2e + Prometheus monitoring (HA gate 포함) 한 set으로 production-grade.

---

## [0.7.7] — 2026-05-20 (patch)

> **요약**: Phase 8 Stage 4.4 Cloudflare Load Balancer Terraform module — Route53 alternative 완성. Cloudflare 사용 중인 enterprise customer가 즉시 plan/apply 가능한 Pool + Monitor + Load Balancer 자동 결선. 코드 변경 0건, Breaking 0. 상세는 [docs/releases/v0.7.7.md](docs/releases/v0.7.7.md).
>
> **기준 commit**: `01ae29b` (main)

### Added
- `feat(deploy)` Stage 4.4 Cloudflare Load Balancer Terraform module (`01ae29b`) — `deploy/terraform/multi-region-ha/modules/cloudflare-loadbalancer/` (Monitor + Pool×2 + Load Balancer) + 신규 root `deploy/terraform/multi-region-ha-cloudflare/` (README + main.tf + variables/outputs + envs/example.tfvars + .gitignore) + multi-region-ha/README.md 링크 활성 + multi-region-dns.md §5.4 Terraform 자동화 절차 추가. Cloudflare Pro plan + Load Balancer 옵션 customer 즉시 적용.

### Notes
- **Phase 8 cross-region cutover 인프라 완전 완성** — design + ops + Route53 IaC + Cloudflare module + failover runbook + testcontainers e2e + Prometheus monitoring. customer는 Route53 또는 Cloudflare 선택.
- Cloudflare 강점: Global anycast 200+ POP + DDoS 무료 (proxy on) + TTL 30s.
- Route53 강점: AWS multi-region 통합 + 비용 효율 + monitor interval 30s.

---

## [0.7.6] — 2026-05-20 (patch)

> **요약**: Phase 8 MR.T8 `rosshield_replication_lag_seconds` Prometheus metric emit + v0.7.5 schema drift fix. Primary PG에서 30초 간격 pg_stat_replication 폴링 — Prometheus + Alertmanager로 즉시 RPO 모니터링 가능. 회귀 0, Breaking 0. 상세는 [docs/releases/v0.7.6.md](docs/releases/v0.7.6.md).
>
> **기준 commit**: `1ee429e` (main)

### Added
- `feat(metrics)` rosshield_replication_lag_seconds Prometheus emit (`c834388`) — `metrics.go`에 `ReplicationLagSeconds *prometheus.GaugeVec` (label: application_name) + 신규 패키지 `internal/platform/replication/lagmetric/` (Querier interface + Collector + ticker goroutine, Gauge.Reset로 stale label cleanup) + bootstrap 결선 (primary PG + replication enabled 조합 silent gate) + CLI flag 2 + env 2 + 9 단위 test.

### Fixed
- `fix(replication)` audit_entries TIMESTAMPTZ 호환 (`1ee429e`) — 마이그레이션 0024가 `occurred_at` + `audit_chain_heads.updated_at`을 TEXT → TIMESTAMPTZ로 변경. testcontainers test의 `NOW()::TEXT` cast 거부 fix (MR.T5/T6 PG integration CI fail 해소).
- `chore(deps)` go.mod tidy smithy-go direct 승격 (`1ee429e`) — v0.7.2 cosign middleware의 smithymiddleware/smithyhttp import 누락 fix.

### Notes
- MR.T8 metric으로 Phase 8 cross-region replication 운영 가시성 완성 — Prometheus dashboard + Alertmanager rule 즉시 활용 가능.
- HA leader-only metric gate(cronsched RoleProvider 패턴)는 별 carryover — 현재 single primary 가정.
- v0.7.5 Stage 7 schema drift 2건 fix로 PG integration CI 그린 회복.

---

## [0.7.5] — 2026-05-20 (patch)

> **요약**: Phase 8 Stage 7 testcontainers e2e cover — 2 PG container fixture + MR.T1·T4·T5·T6·T7·T8 6/8 test 자동 회귀 방어 마련. v0.7.4 운영 docs/IaC/runbook과 결합해 **Phase 8 Multi-region HA 사실상 완성**. 코드 변경 0건, Breaking 0. 상세는 [docs/releases/v0.7.5.md](docs/releases/v0.7.5.md).
>
> **기준 commit**: `d842583` (main)

### Added
- `test(replication)` Phase 8 Stage 7 testcontainers fixture + MR.T1·T7 (`3803369`) — `internal/platform/storage/postgres/replication_integration_test.go` 신규 (build tag `integration`) + 2 PG container(Docker network) + wal_level=logical + PUBLICATION/SUBSCRIPTION 자동 setup. MR.T1 replication lag < 1s + MR.T7 tenant cross-region 검증.
- `test(replication)` MR.T4·T5·T6·T8 추가 (`d842583`) — MR.T4 failover promote (ALTER SUBSCRIPTION DISABLE + standby isolation) + MR.T5 audit chain head_hash cross-region 일치 + MR.T6 leader_epoch column replicate (split-brain 방어 base) + MR.T8 pg_stat_replication lag 측정 가능. MR.T1~T8 6/8 cover (T2/T3는 기존 unit test).

### Notes
- Phase 8 Multi-region HA 사실상 완성: design + ops + IaC + runbook + 6/8 e2e test로 customer가 production-grade 환경 자체 구축·운영·incident 대응·회귀 방어까지 가능.
- MR.T4 leader-election restart / MR.T6 fence token enforcement / MR.T8 metric emit은 application-level integration carryover.
- 기존 pg-integration CI job(timeout 8분)이 본 test 자동 cover — 추가 인프라 변경 없음.

---

## [0.7.4] — 2026-05-20 (patch)

> **요약**: Phase 8 Multi-region cutover 운영 단위 완성 — design + ops + Terraform IaC + failover runbook 4 종을 한 release에 묶어 customer가 cross-region failover 환경을 자체 구축·운영 가능. 코드 변경 0건, Breaking 0. 상세는 [docs/releases/v0.7.4.md](docs/releases/v0.7.4.md).
>
> **기준 commit**: `24fb4e6` (main)

### Added
- `design(phase8)` Multi-region HA Stage 4 DNS routing + failover records (`1f1d192`) — `docs/design/notes/multi-region-ha-stage4-design.md` (12 섹션) + 4 DNS provider 비교 + RTO ≤ 5분 분해 + D-MR-4 sub-decisions 5종.
- `docs(ops)` Stage 4.2 multi-region-dns.md (`c0b1222`) — Route53 setup + 운영자 cutover 절차 + Cloudflare alternative + 자체 DNS BIND/PowerDNS + Prometheus 메트릭.
- `feat(deploy)` Stage 4.3 Terraform IaC sample (`9a59732`) — `deploy/terraform/multi-region-ha/` 9 파일 + Route53 failover module + envs/example.tfvars. customer plan/apply 즉시 가능.
- `docs(ops)` Stage 5 failover runbook (`24fb4e6`) — `docs/operations/multi-region-failover-runbook.md` (11 섹션) + 운영자 step-by-step + escalation + roll-back + Primary 복구 + 사후 분석 + Quick reference card.

### Notes
- Phase 8 cross-region cutover 운영 단위 완성: design + ops + IaC + runbook 4 종이 한 release에서 customer가 자체 환경 구축·운영 가능.
- D-MR-4 권장 default 5종 수용 (Route53 / 60s TTL / 3회 연속 fail / DNS 자동 + application manual / Terraform).
- 잔여 Phase 8 carryover: Stage 4.4 Cloudflare module, Stage 6 자동 failover research, Stage 7 testcontainers e2e (모두 후속).

---

## [0.7.3] — 2026-05-20 (patch)

> **요약**: D-UI-1 Stage 5b drill-down a11y axe scan — 10 페이지 추가 cover (Audit·Reports·SSO·Integrations·License·Advisor·Fleet·Robot·Pack·Check details). 누적 a11y cover **20 페이지 / 28 케이스 / violation 0**. carryover C5b-3 일소. 상세는 [docs/releases/v0.7.3.md](docs/releases/v0.7.3.md).
>
> **기준 commit**: `af5bfd1` (main)

### Added
- `test(web)` D-UI-1 Stage 5b drill-down a11y axe scan (`af5bfd1`) — `web/src/routes/__tests__/a11y-drilldown.test.tsx` 신규 (370+ 줄, 13 케이스: 10 light + 3 dark sampling) + 10 페이지 named export 정비 (audit/reports/sso/integrations/license/advisor/packs.$packKey + 3 detail view 분리) + test 인프라 개선 (createFileRoute mock useParams + Link aria-label fallback). 회귀 0.

### Notes
- 누적 a11y cover **20 페이지 / 28 케이스 / WCAG violation 0** — admin·auditor 권한·인증 전 surface·drill-down 거의 전체.
- 잔여 Stage 5b carryover는 시간 큰 폴리시 또는 별 epic (C5b-6 단순 카드, C5b-7 3rd party widget, C5b-8 키보드, C5b-9 인터랙션).

---

## [0.7.2] — 2026-05-20 (patch)

> **요약**: cosign 2.x 호환성 prod 버그 fix + cosign keyless e2e CI job 추가. e2e 검증으로 release 직후 발견한 wire layer 버그를 customer 노출 전 차단. **v0.7.0~v0.7.1에서 cosign 활성화한 customer의 bundle은 verify 불가 상태였음 — v0.7.2 후 신규 rotation부터 정상**. 상세는 [docs/releases/v0.7.2.md](docs/releases/v0.7.2.md).
>
> **기준 commit**: `53b19aa` (main)

### Fixed
- `fix(audit)` cosign sign-blob bundle을 stdout 대신 임시 파일 경유 (`5df41f9`) — **잠재 prod 버그**. cosign 2.x는 stdout에 base64 signature(`MEUC...`)와 bundle JSON 혼재 출력 → 외부 감사인 verify-blob 호출 시 'invalid character M' JSON parse 실패. `os.CreateTemp` + `--bundle=<tmpfile>` 패턴(cosign docs 표준)으로 정정. 단위 test 14건 회귀 0(FakeSigner는 binary 호출 안 함, e2e만 잡았음).
- `test(storage)` migration sequence test 0036 등록 (`53b19aa`) — v0.7.1 0036_audit_gc_marker 추가 후 TestNoUnexpectedMigrationFiles + TestStorageMigrateIdempotent 갱신 누락.

### Added
- `test(audit)` cosign keyless e2e wire 호환성 (`73f7a94`) — build tag `cosign_e2e` + `internal/domain/audit/rotation/cosign_e2e_test.go` 신규. 실 cosign binary로 sign-blob → verify-blob round-trip + bundle 변조 거부 검증. CI cosign-e2e job 신규(sigstore/cosign-installer + permissions: id-token: write). v0.7.x carryover 마지막 코드 작업 항목 일소.

### Notes
- **e2e test 가치 입증** — release 직후 cosign 2.x 호환성 버그를 발견. 단위 test (FakeSigner)는 binary 호출 안 해서 잡지 못한 wire layer 버그. e2e의 직접적 ROI.
- 본 release 이전 customer는 cosign_bundle 컬럼이 손상된 상태일 수 있음. v0.7.2 후 신규 rotation부터 정상.
- v0.7.x 코드 작업 carryover **모두 일소**. 잔여는 시간 큰 폴리시 또는 외부 트랙(Stage 5b Playwright, HA Stage 4~6 docs, R-D8, E36).

---

## [0.7.1] — 2026-05-20 (patch)

> **요약**: sqlite hot GC marker mode 활성화 patch — v0.7.0 한계 "sqlite hot GC" 항목 일소. sqlite customer(데스크톱·단일 노드)도 audit chain hot row 무한 누적 없이 운영 가능. 회귀 0. 상세는 [docs/releases/v0.7.1.md](docs/releases/v0.7.1.md).
>
> **기준 commit**: `ee5f3c8` (main)

### Added
- `feat(audit)` sqlite hot GC marker mode (`ee5f3c8`) — 마이그레이션 0036 (sqlite `audit_gc_mode` table + `audit_entries_no_delete` trigger WHEN 절 변환 / PG noop) + `HotGCDeps.UseMarkerMode` 분기 (sqlite INSERT/DELETE marker, PG SET LOCAL 유지) + bootstrap auto-wiring (cfg.StorageDriver) + `Platform.HotGC` + `handlers.Deps.HotGC` 결선 (POST /api/v1/audit/gc/run 503 → 200) + 단위 test 3 (1 갱신 + 2 신규: marker mode 실제 DELETE + emit / HotGC 완료 후 direct DELETE 차단 검증).

### Notes
- 마이그레이션 0036은 자동 적용 — application 코드 변경 없음. marker 비활성 상태(default)에서 동작은 기존과 동일.
- sqlite 환경에서 hot GC 활성화는 운영자 명시 trigger (manual API 또는 cron schedule 옵트인).
- PG customer는 0036 noop으로 영향 0 — 기존 0034 GUC 경로 그대로 유지.

---

## [0.7.0] — 2026-05-20 (minor)

> **요약**: Multi-region HA + S3 backend 운영 진입 minor release — slot cleanup cron 자동 wiring + S3 lifecycle bootstrap config parser + **MinIO Content-MD5 middleware로 S3 호환 storage 완전 호환**. 회귀 0. 상세는 [docs/releases/v0.7.0.md](docs/releases/v0.7.0.md).
>
> **기준 commit**: `810171a` (main)

### Added
- `feat(replication)` E-MR Stage 3 후속 slot cleanup cron job bootstrap 결선 (`742e0c1`) — `internal/platform/scheduler/replicationcleanupjob/` 신규 패키지 (rotationjob 패턴) + bootstrap auto-register (primary PG + replication enabled 조합) + CLI flag 4 + env 4 + 12 단위 test. SlotPrefix 빈 값 fail-fast 안전 가드.
- `feat(audit)` S3 lifecycle bootstrap config parser (`3f44860`) — Config 5 필드 (Enabled + IA/Glacier/DeepArchive Days + ExpireDays) + buildS3LifecycleTransitions helper + CLI flag 5 + env 5 + resolveIntEnvFallback helper. 표준 audit retention (1년 IA → 5년 GLACIER → 7년 만료) opt-in.
- `feat(audit)` MinIO Content-MD5 middleware + lifecycle 통합 검증 복원 (`810171a`) — smithy-go finalize middleware로 PutBucketLifecycleConfiguration request body MD5 base64 자동 주입 (Set-MD5 헤더). AWS 본가 + MinIO 양쪽 호환. MinIO integration test `MinIOLifecycle` 복원 — NewS3Backend 자동 적용 + idempotency 검증.

### Fixed
- `fix(audit)` S3 lifecycle 1차 ChecksumAlgorithm 시도 (`666682d`) — AWS 본가는 정상이지만 MinIO 미지원, middleware로 최종 해소.
- `fix(audit)` MinIO integration test lifecycle 임시 분리 (`201c71d`) — middleware 도입 전 lifecycle 부분 분리, middleware 후 복원.
- `test(scanrun)` TestRunCancelSkipsRemainingButWaitsInFlight flaky 안정화 (`c3ec3f1`) — exec 100ms→500ms + cancel 50ms→100ms로 CI runner 부하 timing tolerance 강화.
- `chore(deps)` go mod tidy aws-sdk-go-v2/credentials direct 승격 (`01fafa1`) — MinIO integration test가 NewStaticCredentialsProvider 직접 사용.

### Notes
- slot cleanup wiring + S3 lifecycle parser + MinIO middleware 모두 v0.6.9 한계 항목 일소
- minor bump 이유: 신규 운영 기능 3종 + customer-facing 자동화 + opt-in (Breaking 0)
- MinIO·Wasabi·Backblaze B2 같은 S3 호환 storage에서 lifecycle 정상 동작 확보

---

## [0.6.9] — 2026-05-20 (patch)

> **요약**: v0.6.8 한계 carryover 4건 일괄 해소 — audit-verify CLI cosign 통합 + S3 lifecycle + MinIO testcontainer 통합 검증 + HA replication 후속 (publication tables sync · slot cleanup) + Web bundle code-splitting (835KB → 239KB main chunk). 회귀 0. 상세는 [docs/releases/v0.6.9.md](docs/releases/v0.6.9.md).
>
> **기준 commit**: `d9a2df4` (main)

### Added
- `feat(audit-verify)` rotation CLI cosign verify 통합 (`7e19a9f`) — single + chain 모드 모두 cosign 5/6 flag (`--cosign-bundle`/`--bundle-dir`/`--identity`/`--oidc-issuer`/`--binary`/`--rekor-url`) + cosignVerify step (skip/PASS/FAIL/bundle 부재 분기) + function var pattern test (5 신규)
- `feat(audit)` S3 lifecycle 자동 적용 (`42d44cc`) — S3Config 확장 (LifecycleEnabled + LifecycleTransitions + LifecycleExpireDays + S3Transition) + ApplyLifecyclePolicy (Rule ID "rosshield-rotation" 고정 + Filter.Prefix) + NewS3Backend 자동 호출 + 단위 6 신규
- `feat(audit)` MinIO testcontainer 통합 검증 (`42d44cc`) — `backend_s3_minio_integration_test.go` (`rosshield_enterprise && integration`) + minio-integration CI job 신규
- `feat(replication)` E-MR Stage 3 후속 (`905fbf8`) — publication tables 자동 sync (ensurePublication exists 경로 + syncPublicationTables + diffTables) + dead slot cleanup (CleanupInactiveSlots + SlotPrefix 강제 + DryRun) + Executor.QueryStrings 추가 + 단위 13 신규
- `perf(web)` bundle code-splitting (`2c8c8e9`) — vite.config.ts manualChunks 8 vendor chunk 분리 (react/router/query/radix/form/ui/state/vendor) + 단일 main 835KB → 239KB + 500KB warning 0

### Fixed
- `fix(ci)` web vitest + Go lint 그린화 (`b86eb86`) — manifest 기대값 Lodestar 적용 + `pwa-virtual.ts` 격리 + vi.mock + gofmt 3 + errcheck 5 (v0.6.7~v0.6.8 누적 27 commit 회귀 일소)
- `chore(fmt)` gofmt 2 파일 정정 (`d9a2df4`) — rotation.go + backend_s3_minio_integration_test.go 정렬

### Notes
- sub-agent 2 병렬 dispatch + 메인 1 병렬 = 3 영역 동시 진행 (HA replication + Web code-splitting + S3 lifecycle/MinIO)
- 도메인 격리 P5 보존 — rotationjob lint 예외 추가 (cmd-equivalent composer 명시)
- AWS SDK PutBucketLifecycleConfiguration이 s3API interface에 추가 — fake mock 단순 유지
- MinIO RELEASE.2024-12-18 pin (renovate 후속)

---

## [0.6.8] — 2026-05-20 (patch)

> **요약**: cosign keyless Sigstore 실서명 + Multi-region HA Stage 3 (PG publication/subscription 자동 setup) + S3 backend 실 SDK (BSL 1.1 enterprise). paying customer demo 가치 3건 동시 진척. 회귀 0. 상세는 [docs/releases/v0.6.8.md](docs/releases/v0.6.8.md).
>
> **기준 commit**: `5dd72b9` (main)

### Added
- `feat(audit)` rotation cosign keyless 서명 결선 (`904c55e`) — Signer interface + CosignSigner (외부 cosign CLI wrap) + FakeSigner + rotation.go signArchive 결선 + bootstrap env 5 + onboarding doc + 14 test
- `feat(replication)` E-MR Stage 3 PG publication/subscription 자동 setup (`cc559ff`) — `internal/platform/replication/setup/` 신규 패키지 (6 파일, 846줄) + idempotent + FOR ALL TABLES default + Executor interface + bootstrap config 5 + 23 unit test + onboarding doc
- `feat(audit)` rotation S3 backend 실 SDK (`5dd72b9`) — build tag `rosshield_enterprise` (BSL 1.1) 분리 + AWS SDK v2 + s3API interface + SSE (AES256/KMS) + S3-compatible (MinIO endpoint + path style) + bootstrap config 8 + onboarding doc + 22 enterprise test

### Notes
- sub-agent 3 병렬 dispatch 모두 성공 — 도메인 충돌 0
- cherry-pick conflict 2회 resolve (main.go + bootstrap.go) — cosign + HA Stage 3 + S3 Config 필드 통합
- enterprise 빌드 분리 — Apache-2.0 코어 + BSL 1.1 enterprise (build tag)
- AWS SDK v2 dep 추가 (이미 sigstore-go indirect → explicit 승격)

---

## [0.6.7] — 2026-05-20 (patch)

> **요약**: Audit rotation Stage 4 hot GC (PG GUC trigger bypass) + Stage 6 cron 자동 job + Stage 5b 잔여 페이지 axe scan (9 페이지 누적). 회귀 0. 상세는 [docs/releases/v0.6.7.md](docs/releases/v0.6.7.md).
>
> **기준 commit**: `ae7e560` (main)

### Added
- `feat(audit)` rotation Stage 4 hot GC (`4d42306`) — 마이그레이션 0034 (audit_gc_guc PG trigger bypass) + HotGC 본체 (290줄) + `audit.gc.complete` entry + manual API + admin 권한 + 13 test (단위 7 + PG integration 2 + handler 4)
- `feat(audit)` rotation Stage 6 cron 자동 job (`f431eb1`) — `internal/platform/scheduler/rotationjob/` sub-package (cycle 회피) + bootstrap 결선 + HA gate 자동 적용 + CLI flag + env + 11 test
- `test(web)` Stage 5b additional 잔여 페이지 4건 axe scan (`2eb9783`) — Login + Invitation accept + Settings + Users + System (5 페이지 × light/dark = 8 케이스). 누적 9 페이지 cover (Stage 5 5 + Stage 5b 4)

### Notes
- sub-agent 3 병렬 dispatch 모두 성공 (Stage 4 재진행 + Stage 6 + Stage 5b additional) — 도메인 충돌 0
- audit chain 무결성 보존 — hot GC도 audit chain 정상 entry (`audit.gc.complete`)
- migrations_test + migrate_test linter 자동 0034 inserted (cherry-pick 후)

---

## [0.6.6] — 2026-05-20 (patch)

> **요약**: Multi-region HA Stage 1~2 (replication scaffold + standby middleware + manual failover) + Audit rotation Stage 5 (verify CLI + prev_segment_hash chain). 회귀 0. 상세는 [docs/releases/v0.6.6.md](docs/releases/v0.6.6.md).
>
> **기준 commit**: `eae9f47` (main)

### Added
- `feat(replication)` Multi-region HA Stage 1~2 (`eae9f47`) — 마이그레이션 0033 (replication_replicas + replication_failovers) + `internal/platform/replication/` 5 파일 (policy + repository + middleware + sqliterepo + test) + handler 4 endpoint (replicas · heartbeat · failover · head-sha) + `audit.replication.failover` entry emit + standby read-only middleware + bootstrap config + env 4
- `feat(audit)` rotation Stage 5 verify CLI + prev_segment_hash chain (`6c7ab60`) — 마이그레이션 0035 (prev_segment_hash column) + builder/archiver/rotation chain 처리 + `rosshield-audit-verify rotation`/`chain` 서브커맨드 + UnmarshalEntryLine + onboarding doc + 15 신규 test

### Notes
- sub-agent 3 dispatch (Multi-region HA + Stage 4 hot GC + Stage 5 verify CLI) — Stage 4 stalled (handler 작성 중) → carryover #522로 분리
- audit chain 무결성 보존 — failover/rotation 모두 audit chain 정상 entry
- cherry-pick conflict resolve (migrations_test + migrate_test 0033 + 0035 통합)

---

## [0.6.5] — 2026-05-20 (patch)

> **요약**: Audit chain rotation 본체 Stage 1~3 (마이그레이션 0032 + rotation 패키지 + 581줄 test) + Stage 5b color-contrast 실측 e2e + drill-down spacing 통일. 회귀 0. 상세는 [docs/releases/v0.6.5.md](docs/releases/v0.6.5.md).
>
> **기준 commit**: `de9e380` (main)

### Added
- `feat(audit)` chain rotation Stage 1~3 (`de9e380`) — 마이그레이션 0032 (audit_rotation_segments + 불변성 트리거) + `internal/domain/audit/rotation/` 패키지 8 파일 (policy + builder + archiver + file backend + S3 stub + rotator) + `audit.rotate.complete` entry emit (chain 정상 link) + env override 3 + 581줄 test
- `test(web)` Stage 5b color-contrast 실측 e2e (`a1f7353`) — Playwright + @axe-core/playwright. 5 페이지 × light/dark = 10 test. jsdom 한계 회피
- `style(web)` Stage 5b drill-down + 일반 페이지 spacing 통일 (`ec28349`) — compliance + fleets.$fleetId 일관화 (`space-y-4`). 19 페이지 중 18 통일. robots.$robotId만 carryover (위험 액션 분리)

### Notes
- sub-agent 3 병렬 dispatch (Audit rotation + color-contrast + drill-down) — 도메인 충돌 0
- audit chain 무결성 보존 — rotation 자체가 audit chain 정상 entry (외부 검증 누락 0)
- Audit Stage 4~6 carryover: hot GC + cosign 실서명 + S3 SDK + cron + verify CLI 확장 + cross-witness fold-in + prev_segment_hash chain
- Stage 5b carryover: 실 실행 + CI 임계치 + 잔여 페이지 (Login · Settings · Users · System)

---

## [0.6.4] — 2026-05-20 (patch)

> **요약**: LLM private deployment 본체 (vLLM driver + Ollama 강화) + D-UI-1 Stage 5 a11y polish (axe-core 5 페이지 violation 0) + integrations delivery detail dialog (3 tab + URL state). 회귀 0. 상세는 [docs/releases/v0.6.4.md](docs/releases/v0.6.4.md).
>
> **기준 commit**: `fc1640d` (main)

### Added
- `feat(intelligence)` LLM private deployment 본체 (`97b7b36`) — vLLM driver 신규 (14 test) + Ollama KeepAlive/AutoPull/PullModel 강화 (7 test) + bootstrap vllm case + env 7 + CLI flag 3 + onboarding doc (~280줄)
- `test(web)` D-UI-1 Stage 5 axe-core a11y scan (`e4ab01f`) — 5 페이지 light/dark violation **0** (overview · findings · scans · robots · fleets) + spacing 일관화 (scans/fleets `space-y-4`) + vitest-axe/axe-core devDep
- `feat(web)` integrations delivery detail dialog (`cb1b48b`) — 3 tab (Request/Response/Retries) + URL state `?delivery=<id>` + i18n 16 키 + a11y (role=button + tabIndex + Enter/Space)

### Notes
- sub-agent 3 병렬 dispatch (LLM 본체 + Stage 5 polish + delivery detail) — 도메인 충돌 0
- LLM Stage 4 (e2e testcontainers) carryover — GPU 의존
- Stage 5b carryover — color-contrast 실측 (Playwright) + drill-down spacing + 3rd party a11y

---

## [0.6.3] — 2026-05-19 (patch)

> **요약**: Phase 5~7 carryover 일소 round — design doc 4건 (LLM private · Multi-region HA · Audit rotation · CIS Manual fixture) + 작은 본체 3건 (E22-F BOOLEAN + CIS Manual 5건 + Optimistic+Undo). CIS Ubuntu 24.04 100% cover (false-FAIL 0). 회귀 0. 상세는 [docs/releases/v0.6.3.md](docs/releases/v0.6.3.md).
>
> **기준 commit**: `1ac5f35` (main)

### Added
- `design(phase8)` LLM private deployment (`d5b075f`, 431줄) — vLLM on-prem 옵션 5 + Stage 5 + 결정 7 (옵션 C: Ollama edge + vLLM)
- `design(phase8)` Multi-region HA (`5afbed1`, 412줄) — 옵션 4 + Stage 7 + 결정 5 (옵션 A: PG logical+DNS)
- `design(phase8)` Audit chain rotation (`be9239c`, 457줄) — 옵션 5 + Stage 6 + 결정 10 (옵션 A: 월 1회 + S3)
- `design(phase5-carryover)` CIS Manual fixture 5건 (`465258c`, 406줄) — 옵션 5 + Stage 5 + 결정 7 (옵션 A+B: env-var skip + manual prompt). 잔여 정확 진단 (21건 → 5건)
- `feat(packs)` CIS Ubuntu 24.04 잔여 Manual 5건 정식 cover (`f1b2dc5`) — 100% 도달 (false-FAIL 0). manual yaml 17건. onboarding doc 신규
- `feat(web)` Optimistic update + Undo window (`6ec135f`) — D-UI-1 P0 carryover. 5 mutation hook + `undoableAction` helper + 5 destructive handler 적용 + i18n 3 키

### Fixed
- `feat(storage)` E22-F R30-1.2 BOOLEAN 회수 (`aa78984`) — 5 컬럼 PG SMALLINT → BOOLEAN (`0031_boolean_native_recovery`). sqliterepo bool 전환 + integration test 5개. 회귀 0

### Notes
- sub-agent 5+3 병렬 dispatch (5 design + 3 본체) — 도메인 충돌 0
- design doc 메모리 정책 일관 (옵션 ≥3 + Stage 분해 + 권장 default + 보수적 추정)
- CIS Manual "21건" 보수 진단 → 잔여 5건 정확 (자동 변환 4건 + op:manual 12건 이미 cover)

---

## [0.6.2] — 2026-05-19 (patch)

> **요약**: ROS2 baseline pack Round 3 — 8/8 카테고리 cover 깊이 확장 (16→22 check) + 잔여 컴포넌트 hardcoded 영어 정리 6 파일. 회귀 0. 상세는 [docs/releases/v0.6.2.md](docs/releases/v0.6.2.md).
>
> **기준 commit**: `2da1e66` (main)

### Added
- `feat(packs)` ROS2 Round 3 C4+C5 carryover 6 check (`a914735`) — apt_key_valid · colcon_install_hash · signed_packages_only · param_files_owner · argv_no_remote_url · lifecycle_node_used. **22 check 총** (Round 1+2+3 = 8/8 깊이 확장)

### Fixed
- `fix(web)` 컴포넌트 hardcoded 영어 잔존 i18n 적용 (`5380269`) — packs/scans/system SeverityStats `t('severity.X')` + uppercase className 제거 + advisor opt-in 옵트인

### Notes
- supply chain 3 layer 직렬 검증 (apt source → key → digest → origin)
- launch 안전 3 layer (world-writable → owner/perms → remote URL → lifecycle)
- A sub-agent (Optimistic+Undo) killed — carryover

---

## [0.6.1] — 2026-05-19 (patch)

> **요약**: UI/UX 사용자 피드백 반영 round — 한국어 페이지 영어 단어 mix 139건 일괄 한국어 + D-UI-2 List+Dialog 패턴 5 페이지 적용 (scans + robots + fleets + integrations + users) + 긴 ID 축약 (`TruncatedId` 신규). 회귀 0. 상세는 [docs/releases/v0.6.1.md](docs/releases/v0.6.1.md).
>
> **기준 commit**: `1290d4c` (main)

### Fixed
- `fix(i18n)` ko dict 60건 영어 → 한국어 일괄 (`c87b0af`) — 도메인 용어 · 상태 라벨 · severity 약어 (CRIT→치명 등)
- `fix(web)` findings 페이지 SeverityStats 한국어 + description 영어 단어 제거 (`3ade9ab`)
- `fix(i18n)` ko 영역 한국어 문장 안 영어 단어 mix 79건 일괄 (`0becb26`) — Python script 2 round (Insight/Robot/Tenant/Fleet/Scan/Session/credential/drift/anomaly/peer/detector/dismiss 등)

### Added
- `refactor(web)` D-UI-2 A — scans + robots List+Dialog 패턴 + TruncatedId 신규 (`fa2c039`)
  - scans: CreateScanDialog + SessionDetailDialog (`?session=` query param)
  - robots: CreateRobotDialog (RHF+zod)
  - TruncatedId: prefix+…+suffix + tooltip full + clipboard copy
- `refactor(web)` D-UI-2 B — fleets + integrations + users Dialog 패턴 (`0e46737`)
  - 각 Create Dialog + 긴 ID truncate (endpoint.id · delivery.id · invitation.id 등)

### Notes
- 회귀 0 (vitest 425+ PASS)
- 신규 dep 0
- carryover Phase 8 — Optimistic update + Undo window + hardcoded 영어 잔존 5 파일

---

## [0.6.0] — 2026-05-19

> **요약**: UI/UX 전면 개선. 4 페르소나 전문가 리뷰 → D-UI-1 통합 design doc → Stage 1+2+3+4 (token + 7 컴포넌트 + 4 그룹 layout + 17 페이지) — **18 P0 중 16건 cover (88.9%)**. 비즈니스 로직·API·라우팅 변경 0. 회귀 0. 상세는 [docs/releases/v0.6.0.md](docs/releases/v0.6.0.md).
>
> **기준 commit**: `6458a75` (main)

### Added

#### Design System
- design token 20 (severity 5×2 + status 5×2, WCAG AA 4.5:1) + Pretendard Variable self-host
- shared component 7 신규 (Toast/sonner + ConfirmDialog + Skeleton + EmptyState 보강 + SeverityBadge + StatusBadge + Form rhf+zod + PageHeader 보강)
- layout 8 신규 (Sidebar 4 그룹 + Header role+tenant + theme/locale toggle + UserMenu + Skip-to-content + Mobile Sheet drawer + Breadcrumbs 일관 + html lang 동적)

#### Stage 4 페이지 적용 (17 페이지)
- 운영: overview (카드 4→6) + scans + findings + robots (Form pilot) + robots/:id (credential rotate typing) + fleets + fleets/:id
- 컴플라이언스: compliance + reports (audit evidence download P0 fix) + audit + packs/:key + packs/:key/checks/:id (VerifyButton 보존)
- 지능화: advisor (LLM opt-in badge)
- 관리: license + integrations (webhook Form pilot) + users (RoleBadge a11y P0) + system (4 카드 polish)

#### 신규 dep
- pretendard (한국어 font) · sonner (toast) · react-hook-form · zod · @hookform/resolvers

### Fixed
- `44b139f` Sidebar 그룹 헤더 visibility (text-xs + opacity 100%) + 한국어 nav 6건 (Findings→탐지·이슈 등) + brand/version stale + virtual:pwa-register CORS
- `173d6e7` alert.tsx hardcoded `bg-red-50`/`bg-slate-50` → token (다크 모드 정상 대비)
- `6458a75` PWA manifest "rosshield Console" → "Lodestar 관리자 콘솔"
- `29e857b` run-data/ git untrack + .gitignore (실수 commit 정리)

### Notes
- 16/18 4 리뷰 P0 cover (Optimistic update + Undo window carryover)
- 회귀 0 (vitest 420/420 PASS, enterprise 264+ PASS 그대로)
- sub-agent worktree pattern 50+회 누적, 본 release 12 sub-agent 활용

---

## [0.5.2] — 2026-05-19 (patch)

> **요약**: ROS2 baseline pack Round 2 마감 (C4 binary 무결성 + C5 launch 안전) → **8/8 카테고리 cover 완성** + TPM simulator integration CI 검증 통과 (R-D8 v3 D-3 full verification 32 cover). 회귀 0. 상세는 [docs/releases/v0.5.2.md](docs/releases/v0.5.2.md).
>
> **기준 commit**: `e636c1a` (main)

### Added
- `feat(packs)` ROS2 Round 2 C4 binary 무결성 3 check (`e885df0`) — apt source 공식 + world-writable libs + systemd unit perms
- `feat(packs)` ROS2 Round 2 C5 launch 안전 3 check (`ab1775b`) — world-writable yaml + secret inline 검출 + shell exec 검출
- `test(enterprise)` D-3 v3 TPM Quote simulator integration test (`7f1e7f4`) — 7 신규 simulator test (round_trip + nonce_mismatch + PCR_tamper + signature_tamper + 결정론) + ci.yml tpm-integration job robotid 패키지 추가 + CI 7/7 ALL PASS 검증

### Notes
- ROS2 baseline pack **Round 1+2 = 8/8 카테고리 완성** (15 check + 15 selftest)
- R-D8 v3 D-3 full verification: mock 9 + simulator 7 + unit 16 = **32 cover**
- 회귀 0, 신규 dep 0
- sub-agent worktree pattern 42회 누적

---

## [0.5.1] — 2026-05-19 (patch)

> **요약**: R-D8 v3 후속 보강 2/2 (C-1 Sigstore keyless + D-3 TPM Quote signature) + SSHPool flaky 결정론화. enterprise 단위 +47 (217+ → 264+). 회귀 0. 상세는 [docs/releases/v0.5.1.md](docs/releases/v0.5.1.md).
>
> **기준 commit**: `c60d9eb` (main)

### Fixed
- `fix(sshpool)` TestSSHPoolTenantsIsolated flaky race 제거 (`fba42f3`) — sync.Barrier 패턴, 10/10 결정론 PASS

### Added
- `feat(enterprise)` C-1 wasmrt v3 cosign Sigstore keyless 통합 (`0929995`) — CosignSigstoreVerifier + Fulcio + Rekor + OIDC + VirtualSigstore in-memory test + sigstore-go v1.1.4 dep + 23 신규 단위
- `feat(enterprise)` D-3 robotid v3 TPM Quote signature attestation flow (`b2a02c3`) — QuoteAttestation + VerifyQuote OS-agnostic + Linux QuoteLinux + AK ECC P-256 + ConstantTimeCompare + 24 신규 단위 + dep 0

### Notes
- enterprise 누적 264+ 단위 PASS (v2 217+ → v3 +47)
- sub-agent worktree pattern 39회 누적, 본 round 3 sub-agent 동시 dispatch (메모리 정책 일관)

---

## [0.5.0] — 2026-05-19

> **요약**: Phase 7 마지막 epic R-PUBLIC 마감(repo public + community 파일 + README badge) + R-D8 v2 후속 보강 4/4 마감(scheduler/anchor + wildcard JSONPath + cosign keyed verifier + TPM PCR 결합). enterprise 단위 +88 (129+ → 217+). 회귀 0. 상세는 [docs/releases/v0.5.0.md](docs/releases/v0.5.0.md).
>
> **기준 commit**: `6b0c247` (main)

### Added

#### Phase 7 — R-PUBLIC (GitHub repo public 전환 + community baseline)
- `feat(public)` Stage 2 community 파일 신규 (`537c98a`) — SECURITY + CODE_OF_CONDUCT + CONTRIBUTING 보강 + .github templates
- `feat(public)` Stage 3 README badge + repo description (`d61a8eb`) — CI/Release/Apache 2.0/Enterprise BSL 1.1/cosign badge + 영어 첫 paragraph + 외부 사용자 시작 섹션

#### Phase 7 — R-D8 v2 후속 보강 (1순위 결합 청구권 4/4)
- `feat(enterprise)` A-1 cross-witness v2 scheduler + anchor (`138185b`) — Scheduler + WebhookAnchor + FilesystemDumpAnchor + 15 신규 단위
- `feat(enterprise)` B-1 multi-hash v2 wildcard JSONPath (`35ecbb3`) — `$.foo[*].bar` + 중첩 cartesian + 26 신규 단위
- `feat(enterprise)` C-1 wasmrt v2 cosign keyed verifier (`a18a4f9`) — ECDSA P-256 + ed25519 + RSA PKCS#1 v1.5 + 28 신규 단위
- `feat(enterprise)` D-3 robotid v2 TPM PCR 결합 (`d281c66`) — pcr_digest + HasPCRQuote flag + 19 신규 단위

#### main CI 7/7 + enterprise 217+
- enterprise 단위 누적 217+ PASS (v1 129+ → v2 +88) — crosswitness 32 + multihash 74 + wasmrt 73 + robotid 38
- 신규 dep 0 (stdlib + 이미 indirect dep)
- sub-agent worktree pattern 36회 누적

### Notes
- 자체 코드 회귀 0 (v1 backward compat 검증 모두 PASS)
- R-D8 v3 후보 별 round 명시 — C-1 Sigstore keyless + D-3 TPM Quote signature 검증

---

## [0.4.3] — 2026-05-19 (patch)

> **요약**: v0.4.2 Snap Build fail fix — snap override-build에 pack-archive step 추가 (embed.go `_archives/*.tar.gz` 부재). 자체 코드 회귀 0. 상세는 [docs/releases/v0.4.3.md](docs/releases/v0.4.3.md).
>
> **기준 commit**: `7fbde90` (main)

### Fixed
- `fix(snap)` override-build에 pack-archive step 추가 (`7fbde90`) — embed.go fresh clone 부재 fix, ci.yml/release-pipeline.yml 패턴 일관.

---

## [0.4.2] — 2026-05-19 (patch)

> **요약**: v0.4.1 Snap Build fail fix — snapcraft 8.x `hooks.configure.plugs: []` 빈 list 거부. 자체 코드 회귀 0, v0.4.1 ↔ v0.4.2 차이는 snap config 1줄. 상세는 [docs/releases/v0.4.2.md](docs/releases/v0.4.2.md).
>
> **기준 commit**: `40b64a0` (main)

### Fixed
- `fix(snap)` configure hook hooks section 제거 (`40b64a0`) — snapcraft 8.x pydantic validator가 빈 plugs list 거부. snap/hooks/configure script는 snapd 자동 인식.

---

## [0.4.1] — 2026-05-18 (patch)

> **요약**: v0.4.0 직후 CI infrastructure fix cascade 14 round 마감 + snap binary 빌드 fix. 자체 코드 회귀 0. main CI 7/7 ALL PASS 완전 안정화 milestone 도달. 상세는 [docs/releases/v0.4.1.md](docs/releases/v0.4.1.md).
>
> **기준 commit**: `921a2cc` (main)

### Fixed

#### CI fix cascade 14 round

- `ci(go)` pack-archive pre-build step 추가 (`c7a630c`) — embed `_archives/*.tar.gz` fresh clone 부재 fix, Secret `DEV_PACK_SIGNER_KEY_B64` 사용
- `fix(snap)` architectures 중복 arm64 제거 (`cd29d62`) — snapcraft 8.x validation 호환
- `fix(packs)` cis-ubuntu-2404 duplicate placeholder 12건 제거 (`885700e`) — manual fixture 작성 후 obsolete
- `ci(go)` pack-archive 3 job 확장 (`b2f30b9`) — go-enterprise + pg-integration + e2e
- `ci(go)` test timeout 10m → 20m (`b21be1e`) — cmd/rosshield-server 일관 초과
- `fix(lint)` golangci-lint v8 cascade 2 round 14건 (`70851d1` + `9889fb5`) — gofmt + errcheck + staticcheck + unused
- `fix(postgres)` 마이그레이션 0024 evidence_json JSONB cast DEFAULT DROP/SET (`b19802a`)
- `fix(postgres)` pgnative_hotpath tenants schema drift (`2e1ba6a`) + insights schema drift (`c978964`)
- `ci(pg)` TESTCONTAINERS_RYUK_DISABLED=true (`282fb9a`) — Reaper hang fix
- `fix(ha)` pglock integration 다층 차단 + migrate conn leak 차단 + assertion 일관 (`a1d7e14` + `5bc5fdd` + `f17777d`)

#### main CI 결과
- **7/7 ALL PASS** — Go + Enterprise + Web + PG integration + CIS + TPM + Playwright E2E

### Notes
- 자체 코드 회귀 0 — 모든 fix는 CI infra · test infra · 마이그레이션 schema drift
- sub-agent stack trace 정독으로 migrate driver borrowed conn leak 진짜 root cause 발견 (golang-migrate/v4 postgres.WithInstance `instance.Conn(ctx)` 영구 borrow)

---

## [0.4.0] — 2026-05-18

> **요약**: Phase 7 코드 트랙 3/4 epic 마감(R-BRAND · R-LICENSE · R-D8 4/4 청구권 완전 마감) + ROS2 baseline pack Round 1 MVP 6/8 카테고리 cover. v0.3.0 이후 20 commit. 회귀 0건. 상세는 [docs/releases/v0.4.0.md](docs/releases/v0.4.0.md).
>
> **기준 commit**: `01e41fc` (main)

### Added

#### Phase 7 — R-BRAND (Lodestar 브랜드 확정)
- `feat(brand)` R-BRAND Stage 1 — Lodestar 채택 + `<ProductName>` placeholder 사용자 대면 6 파일 교체 (`3e3d892`)
- `feat(brand)` R-BRAND Stage 1 보완 — design/onboarding/web 잔여 9 파일 11 위치 교체 + d1-brand-candidates §5.6 확정 근거 (`20eddee`)
- 코드 네임스페이스 `rosshield` 보존 (Go 모듈 · CLI · YAML apiVersion · PWA manifest short_name 변경 0)

#### Phase 7 — R-LICENSE (Open-core 라이선스 양분)
- `docs(license)` R-LICENSE — LICENSE-ENTERPRISE (BSL 1.1, Change Date 2030-05-18) + NOTICE (third-party OSS attribution ~20 dep) (`ea8d5d7`)
- 기존 LICENSE(Apache 2.0) 보존 — 코어/enterprise 라이선스 양분 결선

#### Phase 7 — R-D8 (D8 청구권 코드 분리, enterprise build tag) — **4/4 본체 완전 마감**
- `feat(enterprise)` R-D8 A-1 — cross-witness fold-in 본체 (multi-fold hash chain, RFC 8785 canonical JSON, 17 단위 PASS) (`b4e77eb`)
- `feat(enterprise)` R-D8 B-1 — multi-hash evidence 본체 (sha256+blake3 cross-check + JSONPath/line sub-hash + VerifyMode enum, 48 단위 PASS, `lukechampine.com/blake3 v1.4.1` dep 추가) (`5292585`)
- `feat(enterprise)` R-D8 D-3 — robotid fingerprint 본체 (TPM EK + sorted MACs + CPU serial + tenant salt, 19+ 단위 PASS, `go-tpm-tools v0.4.8` indirect 활용) (`b8bbae7`)
- `feat(enterprise)` R-D8 C-1 — WASM sandboxed evaluator 본체 (wazero v1.11.0 + WASI 격리 + CPU timeout + hand-crafted WASM 4종, 45 단위 PASS, PolicyVerifier interface) (`012fe3f`)
- **enterprise 8 패키지 누적 129+ 단위 PASS** (crosswitness 17 + multihash 48 + wasmrt 45 + robotid 19+ = 4 청구권 본체 cover) + 코어 → enterprise import 0 (boundary_test 회귀 0)
- **1순위 결합 청구항 4 본체 모두 enterprise build tag 격리** — `docs/design/13-patent-strategy.md` §13.5 spec 정확 일치

#### ROS2 baseline pack Round 1 (솔루션 핵심 차별화 영역)
- `feat(packs)` Round 1 Stage 1 — `packs/ros2-jazzy/` 신규 pack + C1 SROS2 보안 활성화 + C6 distro(LTS/EOL/CLI) (`8eb3d7d`)
- `feat(packs)` Round 1 Stage 2 — C3 ROS_DOMAIN_ID 격리 + C7 RMW_IMPLEMENTATION (`edfba4f`)
- `feat(packs)` Round 1 Stage 3 — C8 governance.xml ENCRYPT topics (`f34f8b9`)
- `feat(packs)` Round 1 Stage 4 — C2 cmd_vel publisher count + ACL (`c6ea725`)
- **카테고리 cover 6/8** (C1·C2·C3·C6·C7·C8 ✅ / C4 binary 무결성·C5 launch 안전 carryover Round 2)
- 9 check 총 + 9 selftest fixture (mock 작성, D-ROS2-9 정확 준수) — ros2_jazzy_fixture_test.go 동적 round-trip cover

### Notes
- 메모리 정책 일관: 큰 작업 design doc 우선(`feedback_design_doc_first`) · 보수적 추정(`feedback_design_doc_conservative`) · 병렬 작업 사전 판단(`feedback_parallel_agents`) · backtick hash 보호(`feedback_commit_message_backticks`)
- sub-agent worktree 패턴 누적 31회 — 마라톤 retrospective(`c85838c`) 학습 반영
- Phase 7 코드 트랙 R-D8 본체 100% 마감 — 다음 자연 진입은 R-PUBLIC (사용자 GitHub Settings 권한 대기) / ROS2 pack Round 2 carryover (paying customer trigger 권장)

---

## [0.3.0] — 2026-05-18

> **요약**: Phase 5(Enterprise & Appliance) 5 epic 100% 마감 + Phase 6 후보 1(첫 paying customer onboarding 보강) 마감. v0.2.0 이후 90 commit. 회귀 0건. 상세는 [docs/releases/v0.3.0.md](docs/releases/v0.3.0.md).
>
> **기준 commit**: `c85838c` (main, marathon retrospective 후 handoff 갱신)

### Added

#### Phase 5 — scanrun SSH 통합 (epic 마감)
- `feat(robot)` scanrun SSH 통합 Stage 1 — `robot_host_keys` 도메인 + 마이그레이션 0027 (TOFU) (`e9b93c0`)
- `feat(sshpool)` Stage 2 — `KnownHostsManager` + TOFU host key callback (`951e924`)
- `feat(scanrun)` Stage 3 — bootstrap KnownHostsManager 결선 + sudo non-interactive (`894449e`)
- `feat(sshpool)` Stage 4 — Pool idle 재사용 + keepalive + metrics 5종 (`22f472d`)
- `feat(scanrun)` Stage 5a — per-robot health window (`robot_offline` 즉시 skip) (`cade719`)
- `feat(scanrun)` Stage 5b — Pool 결선 (idle 재사용 활성화) (`1d67cef`)
- `test(scanrun)` Stage 5c — docker compose + sshd e2e 5 phase (`ee2aa34`)

#### Phase 5 — 세분 RBAC (epic 마감)
- `feat(authz)` Stage 1 — authz 결정 테이블 + 6 시스템 role permission matrix (`4c4bfc9`)
- `feat(tenant)` Stage 2 — `RoleBinding` + 마이그레이션 0028 + repo 확장 (`scope_type`/`scope_id`) (`daacb57`)
- `feat(rbac)` Stage 3 — JWT bindings claim + `RequirePermission` middleware factory (`a9125aa`)
- `feat(rbac)` Stage 4 — handlers.go 24 mutation gate `RequireRole` → `RequirePermission` 교체 + 통합 매트릭스 (`0452941`)
- `feat(rbac)` Stage 5 — web `useHasPermission` + sidebar/router guard 확장 (`4ec5620`)

#### Phase 5 — PWA 오프라인 (epic 마감)
- `feat(web)` PWA Stage 1 — manifest + 아이콘 4종 + index.html link (installable, SW 없이) (`4079e66`)
- `feat(web)` PWA Stage 2 — vite-plugin-pwa generateSW + SW 등록 (오프라인 셸 캐싱) (`1bf2c21`)
- `feat(web)` PWA Stage 3 — `OfflineIndicator` + `UpdatePrompt` UX (`1732a40`)
- `feat(web)` PWA Stage 4 — mutation 가드 + 운영자 docs (`70ef3d6`)

#### Phase 5 — PWA persist (epic 마감)
- `feat(web)` PWA persist Stage 1 — idb-storage 모듈 (IndexedDB AsyncStorage 어댑터) (`2499722`)
- `feat(web)` PWA persist Stage 2 — `PersistQueryClientProvider` 결선 + dehydrate filter (보안 차단 list) (`7e855a8`)
- `feat(web)` PWA persist Stage 3 — logout flow clear (multi-tenant 격리) (`1f985c7`)
- `docs(operations)` PWA persist 운영자 가이드 (`350c38d`)

#### Phase 5 — RBAC fleet 정밀화 (epic 마감)
- `feat(authz)` Stage 1 — PDP `MatchedBindings` 확장 (explainability) (`d55cd71`)
- `feat(rbac)` Stage 2 — `RequirePermissionWithFleet` + body peek + `ScopeResolver` (`0deb4c8`)
- `feat(rbac)` Stage 3 — handlers 5 endpoint 교체 + ScopeResolver 구체 + 통합 매트릭스 (`e3a7958`)
- `feat(rbac)` Stage 4 — SSO group 매핑 도메인 + 마이그레이션 0029 + `user_roles.source` (`07fb0a8`)
- `feat(rbac)` Stage 5 — SSO callback sync + audit + web admin UI (`acde2b2`)
- `feat(rbac)` Stage 6 — reports/insights service 확장 + 2 endpoint 정밀화 (`77180db`)

#### Phase 6 후보 1 — Customer onboarding 보강 (R1·R2·R3 마감)
- `feat(intake)` R1 Stage 1 — intake 도메인 + 마이그레이션 0030 (`6d7f869`)
- `feat(intake)` R1 Stage 2 — intake handler + endpoint + RBAC mount (`09c20cf`)
- `feat(intake)` R1 Stage 3 — chi mount + RBAC + bootstrap intake 결선 (`6da6ffd`)
- `feat(intake)` R1 Stage 4 — auto-provisioning wrap (accept → tenant + admin invite) (`975109e`)
- `feat(intake)` R1 Stage 5 — 실 e2e 통합 + 운영자 docs 갱신 (`e13c9b0`)
- `docs(onboarding)` R2 — PoC walkthrough (단계별 명령 + 예상 결과 + 트러블슈팅 12개) (`f8446de`)
- `docs(onboarding)` R3 — SLA template + 지원 채널 정책 (`2b47546`)

#### Design docs (Phase 5 + Phase 6)
- `design(scanrun)` scanrun SSH 통합 design doc (`6f893de`)
- `design(web)` PWA 오프라인 지원 design doc (`eeebfdd`)
- `design(rbac)` 세분 RBAC (fleet scope + permission tier) design doc (`b975e94`)
- `design(rbac)` fleet-scope 정밀화 + SSO group 매핑 design doc (`37778ef`)
- `design(scanrun)` scanrun 후속 (Pool size 동적 + rate limit + circuit breaker) design doc (`7d26bfd`)
- `design(web)` PWA persist design doc — 옵션 C trigger (`af0b84d`)
- `design(phase6)` Phase 6 backlog — Phase 5 retrospective + 후보 5종 비교 + 권장 우선순위 (`ad5fcf6`)
- `design(onboarding)` 첫 customer onboarding 보강 design doc — intake API + walkthrough + SLA + 지원 채널 + license lifecycle (`c0f8586`)
- `design(meta)` 마라톤 세션 retrospective — 73 commit 패턴·결정·learnings 정리 (`ebc2b80`)

### Changed
- `AssignRoleScoped(..., source)` 매개변수 추가 — backward-compat (`source=""`/`"manual"` → 기존 동작, `"sso:<provider>"` → 자동 동기화 경로)
- 24 mutation gate `RequireRole` → `RequirePermission` 단계적 전환 (관리자 전용 gate는 `RequireRole` 잔존)

### Deprecated
- 없음

### Removed
- 없음

### Fixed
- 본 release 구간 내 회귀 0건 (separate fix commits 없음)

### Security
- TOFU host key 정책 도입 (마이그레이션 0027 `robot_host_keys`) — SSH MITM 방지
- fleet-scope 정밀 권한 검사로 cross-fleet 데이터 누출 차단 (RBAC fleet 정밀화 epic)
- PWA persist의 dehydrate filter에 보안 차단 list 적용 (token·자격 증명·민감 도메인 캐시 금지)

### Migrations
| ID | 내용 | down sql |
|---|---|---|
| `0027_robot_host_keys` | TOFU host key 저장 | ✅ |
| `0028_user_roles_scope` | `scope_type`/`scope_id` 컬럼 추가 (fleet-scope RBAC) | ✅ |
| `0029_sso_group_mappings` | SSO group → role 매핑 + `user_roles.source` | ✅ |
| `0030_customer_intakes` | customer intake 도메인 | ✅ |

---

## [0.2.0] — 2026-05-08

> **요약**: Phase 4(Production hardening) carryover 11/11 마감 + 첫 공식 release. 47 release assets + cosign keyless 서명 (Sigstore Fulcio).
>
> **기준 commit**: `14a3ccb` 이하 (tag `v0.2.0`).

### Added
- E12 — release CI signer + dual trust bundle (dev + release pack signer)
- E25 — HA leader-election scaffold (PG advisory lock + leader_epoch fence token, 마이그레이션 0022·0023)
- E22-F (1차) — PG-native 핫 path 3 컬럼 회수 (R30-1=C 하이브리드, JSONB + TIMESTAMPTZ, 마이그레이션 0024)
- E27 — Grafana dashboard + Prometheus scrape 가이드
- E29 — `rosshield ha status` + `backup list`/`download` CLI
- E31 — enterprise build tag scaffold (`//go:build rosshield_enterprise`)
- E33 — Ubuntu Core snap 빌드 파이프라인 + smoke test (R40-1=core22)
- E34 — TPM 2.0 PCR-sealed ed25519 (`go-tpm-tools`, PCR `[0,2,4,7]`)
- E35 — A/B OTA post-refresh hook + 자동 rollback + healthz polling
- E36 — 레퍼런스 HW 매트릭스 + 측정 절차 docs
- E38 — 첫 paying customer onboarding 사전 자료 (`docs/onboarding/`)
- O6 — invite email adapter (Noop + SMTP + `InvitationNotifier`)
- O7 — webhook UI 강화 (Test 버튼 + delivery 통계 + dead-letter)
- B6+B7 — `/system` 운영 정보 dashboard (헬스·HA·라이선스·백업) + 자동 백업 schedule + 다운로드 API
- OpenAPI spec — Webhook test + SSO 8 + Invitation 5 endpoint 추가

### Changed
- `apiClient` 100% 전환 (webhook·sso·invitation 4 wrapper 제거 + 16 hook 전환)
- 데스크톱 셸 Tauri 2.x 결선 (D3)

### Decisions (이 release 구간 확정)
- D5 — Open-core 채택 (코어 Apache-2.0 + enterprise BSL/Commercial 별 라이선스)
- D6 — GitHub private 유지 (release binary + report verify CLI로 P1 외부 검증 대체)
- R30-1=C 하이브리드 (E22-F 1차)
- R30-2 (E25 HA 권고안)
- R30-4 (Open-core + private repo 종결)
- R40-1~4 (snap 트랙)
- R41 (TPM 3종 결정 — B Keystore + go-tpm-tools + PCR `[0,2,4,7]`)

### Fixed
- `fix(bootstrap)` `WriteString(Sprintf)` → `Fprintf` (staticcheck QF1012) (`b700ff7`)

### Security
- cosign keyless 서명 (Sigstore Fulcio) — release artifact 무결성
- audit chain leader-gate + leader_epoch fence token (split-brain 방지)
- TPM 2.0 PCR-sealed key (E34)

---

## Pre-v0.2.0 historical entries

> Phase 0~1 초기 부트스트랩 기록 (2026-04-23). 본 entries는 v0.2.0 release 시점에 changelog 정식화가 진행되기 전 작성된 초기 항목으로, 역사 기록 보존을 위해 유지합니다. 향후 별도 chronological release tag로 정리될 가능성 있음.

### Added (Phase 0 — 설계)

- 2026-04-23 — 13개 설계 문서 초안 작성 (`docs/design/` Draft v0.1)
  - `00-mission-and-positioning.md` 미션·CAI 대비 포지셔닝
  - `01-principles.md` 12개 설계 원칙
  - `02-system-overview-and-deployment.md` 3종 배포 타깃
  - `03-architecture.md` 레이어·도메인·프로세스 토폴로지
  - `04-domain-and-data-model.md` 도메인 모델·SQL 스키마
  - `05-api-and-auth.md` HTTP/WS API·인증
  - `06-security-and-tenancy.md` 보안·멀티테넌시
  - `07-scan-engine-and-benchmarks.md` 스캔·벤치마크 팩
  - `08-intelligence-and-compliance.md` LLM·컴플라이언스
  - `09-ui-and-clients.md` Web/Desktop/CLI
  - `10-audit-and-observability.md` 해시 체인·관측성
  - `11-tech-stack-and-roadmap.md` 스택 선택·로드맵
  - `12-migration-and-non-goals.md` 자산 승계·비목표·리스크
- 2026-04-23 — `CLAUDE.md`, `SESSION_HANDOFF.md`, `README.md`, `CONTRIBUTING.md` 작성
- 2026-04-23 — 리포 부트스트랩(`.gitignore`, `.editorconfig`, `LICENSE` placeholder)

### Added (추가)

- 2026-04-23 — `contrib/source-benchmarks/README.md` 작성 — 전신 `nrobotcheck/resources/baselines/`의 원본 자료(CIS·ROS2 베이스라인 JSON·SCAP XML) 경로·크기·SHA-256·라이선스·타깃 팩 포인터. 파일 자체는 복사하지 않음.

### Added (Step 0.2 — Go 부트스트랩)

- 2026-04-23 — Apache License 2.0 본문 `LICENSE` 등록 (Copyright 2026 rosshield Contributors).
- 2026-04-23 — Go 모듈 초기화: `go.mod` (module `github.com/ssabro/rosshield`, go 1.26).
- 2026-04-23 — `Makefile` — `build`·`test`·`test-race`·`vet`·`fmt`·`tidy`·`lint`·`openapi`·`ci`·`clean` 타깃.
- 2026-04-23 — `.golangci.yml` v2 — `errcheck`·`govet`·`staticcheck`·`ineffassign`·`unused` + `gofmt`/`goimports` 포매터.
- 2026-04-23 — `.github/workflows/ci.yml` — Go 1.26 `ubuntu-latest` tidy → vet → build → test(-race) → golangci-lint 파이프라인.
- 2026-04-23 — `cmd/rosshield-server/main.go`/`main_test.go` — `/healthz` GET 200 JSON 스텁 + TDD 단위 테스트 2건(200/JSON body, POST 거부).

### Added (Step 0.3 — OpenAPI 스켈레톤)

- 2026-04-23 — `openapi/openapi.yaml` v0.0.1 (OpenAPI 3.1) — 엔벨로프(`Envelope`/`ErrorEnvelope`) + 8-카테고리 `ErrorCategory` + `Meta`/`PageMeta` + 공통 파라미터(`Limit`/`Cursor`/`Sort`/`IdempotencyKey`) + 보안 스키마(`bearerAuth`/`apiKeyAuth`). 대표 경로 11종(`/healthz`, `/readyz`, `/api/v1/auth/{login,me}`, `/api/v1/tenants/current`, `/api/v1/robots{,/{id}}`, `/api/v1/scans`, `/api/v1/reports/{id}:verify`, `/api/v1/audit/{head,verify}`) 스텁. 미구현 경로는 `x-status: todo`로 표기. 설계서 §5.12의 split 구조는 파일 크기 400줄 근처 진입 시 분할 예정.

### Added (Step 0.4 — Phase 1 백로그)

- 2026-04-23 — `docs/design/phase1-backlog.md` Draft v0.1 — Phase 1(Core MVP) 체크리스트를 에픽 12개(E1 Platform L1 → E2 Audit → E3 Tenant/Auth → E4 Pack 시스템 → E5 Robot/Fleet → E6 SSH+Scan → E7 Evidence → E8 Reporting → E9 CLI → E10 Web UI → E11 Compose 번들 → E12 pack-tools) × TDD 단위 태스크로 분해. 의존 그래프, 에픽별 인터페이스·대표 테스트·Exit 기준·설계 참조·기간 추정(총 11.5주 + 0.5주 범퍼 = 12주) 포함. 설계 문서 인덱스 README에 Part VII 섹션으로 등록.

### Added (Phase 1 — 구현 착수)

- 2026-04-23 — **E1.T1 Logger** (`internal/platform/logger/`) — `context.Context` 기반 구조화 로그. `slog.Handler` 래퍼가 `tenantId`/`requestId`/`traceId`를 자동 첨부. `WithTenantID`/`WithRequestID`/`WithTraceID` 주입 API + 동명 추출 API. TDD 5건(fields 실림, 미설정 필드 생략, 추출 헬퍼, 빈 ctx 추출, `With()` 후 ctx 필드 유지) 모두 pass. CI green.

### Added (Phase 1 — 사전 설계 노트)

- 2026-04-23 — `docs/design/notes/e1-storage-deepdive.md` (502줄) — E1.T4/T5 Storage 레이어 사전 설계. 드라이버 선택(`modernc.org/sqlite` 채택, 단일 정적 바이너리 원칙 정합), PRAGMA 고정값, SQLite↔PG 공존 전략(런타임 config + 분리 마이그레이션), 마이그레이션 툴(`pressly/goose`), Tx 함수형 API, Audit append-only 트리거, 테넌시 로우 레벨 격리, 테스트 전략, Go 인터페이스 스케치(`Storage`/`Tx`/`Repository[T,ID]`), E1.T4 착수 전 결정 필요 7건.
- 2026-04-23 — `docs/design/notes/e1-eventbus-deepdive.md` (444줄) — E1.T6/T7 EventBus 사전 설계. 아키텍처(channel-per-subscriber fan-out), 구독 lifecycle, goroutine 모델, backpressure(기본 DropOldest+256, audit Block+1024 override), panic 격리, 이벤트 envelope(§3.6 정합), correlation/causation ctx 전파, **audit 통합 후보 B 추천**(명시 `audit.Append()` + 커밋-후-퍼블리시 + outbox), 테스트 synchronous drain, NATS/Redis future compat interface 경계, Go 인터페이스 스케치, E1.T6 착수 전 결정 필요 7건.

### Decisions

- 2026-04-23 — 리포를 `D:\robot\dev\nrobotcheck` 전신과 분리해 `D:\robot\dev\fleetguard`로 신설
- 2026-04-23 — 상업화 방향: 헤드리스 코어 + 배포 3종(데스크톱·온프렘·어플라이언스 이미지)
- 2026-04-23 — 어플라이언스 자체 제조 포기, 이미지 + 파트너 채널 모델 채택
- 2026-04-23 — CAI와의 포지션 분리: 자율 공격 에이전트 프레임워크는 비목표
- **2026-04-23 — D2**: 백엔드 `Go`, 프론트 `TypeScript` 확정. 단일 정적 바이너리 + 에어갭 원칙 부합.
- **2026-04-23 — D3**: 데스크톱 셸 `Tauri 2.x` 확정 (Electron fallback 보류).
- **2026-04-23 — D5**: 라이선스 `Open-core` — 코어 Apache-2.0 + 엔터프라이즈 closed.
- **2026-04-23 — D6**: 리포 호스팅 `GitHub private` → Phase 1 exit 후 public 전환.
- **2026-04-23 — D1 부분 확정**: 코드네임 `rosshield` 채택(Google 검색으로 충돌 없음 확인). 제품 브랜드는 placeholder로 유지 → 2026-05-18 D-P7-1에서 **Lodestar**로 최종 확정. 초기 가칭 "FleetGuard"는 Cummins·Attestor.ai·TrustArc 등과 상표 충돌로 폐기.
- **2026-04-23 — D4 연기**: 어플라이언스 OS 기본 가정 `Ubuntu Core 24`, Phase 3 exit 재확정.
