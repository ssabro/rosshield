# rosshield Phase 4 Backlog — Production Hardening

**범위**: 6~8주 (`docs/design/11-tech-stack-and-roadmap.md` §11.13 후속).

**전제**: Phase 3 핵심 5/5 완료 — OIDC + SAML SSO·초대·역할(B2 결선)·PG 백엔드(testcontainers 통합 검증)·webhook+SIEM·라이선스+402. 한 사용자 PoC 시연 가능 상태. Phase 4는 production 운영을 위한 강화·자동화·외부 가시성 확보 + Phase 3 carryover 회수.

**Exit 기준**: 첫 유료 customer가 단일 인스턴스/Compose로 30일 운영하며 incident 0건 + 외부 감사인이 report bundle 1건 자체 검증 완료.

---

## Phase 3 Carryover (deferred — Phase 4 합류)

| ID | 출처 | 내용 | 추정 |
|---|---|---|---|
| E25 | Phase 3 backlog (옵션) | HA — PostgreSQL advisory lock + leader/follower (single-writer audit) | 4일 |
| E22-F | E22-E follow-up | PG-native repo 분리 — JSONB·TIMESTAMPTZ·BOOLEAN 활용 + driver-aware repo (현재 sqliterepo 단일 경로) | 1주 |
| O5 | E20-D 후속 | SSO 사용자 자동 프로비저닝 — IdentityResolver 결선 + 첫 로그인 시 user 생성 + 기본 role 할당 | 3일 |
| O6 | E21 후속 | 초대 이메일 발송 어댑터 (SMTP·SES·noop 옵트인). 현 운영은 admin이 token URL 수동 전달 | 3일 |
| O7 | E23 후속 | Webhook UI에 Delivery 재전송·dead-letter 명시 표시 + dispatcher 통계 | 2일 |
| O8 | demo seed 확장 | seed demo가 invitation·webhook·SSO provider 시드까지 포함 (Exit demo 자동화) — ✅ 2026-05-08 완료 | 1일 |

---

## Phase 4 신규 Epic

### E26. 릴리스 파이프라인 + 서명된 바이너리 (1주)

**왜**: PoC 시연 후 실제 customer가 다운로드·검증할 수 있는 binary release 경로 필요.

#### 스코프
- GitHub Actions release workflow: tag push → multi-arch (linux/amd64·linux/arm64·darwin·windows) 빌드
- cosign로 binary + SBOM 서명 (verifiable provenance, P1 외부 검증 가능성)
- `rosshield version` CLI 출력에 빌드 메타(commit·build time·signer keyId) 노출
- `gh release` 자동 게시 + checksum + cosign signature

#### TDD 태스크
| ID | 테스트 | 구현 |
|---|---|---|
| E26.T1 | `TestVersionCommandIncludesBuildMetadata` | -ldflags로 commit/buildTime 주입 + version 출력 |
| E26.T2 | `TestReleaseWorkflowProducesSignedArtifacts` (CI) | tag-trigger smoke run + cosign verify |
| E26.T3 | `TestSBOMIncludesAllDirectDeps` | syft generated SBOM가 go.mod direct deps 포함 |

#### Exit 기준
- v0.x.0 tag → multi-arch 바이너리 + .sig + .sbom.spdx 자동 게시
- 외부 검증자가 cosign verify로 서명 확인 가능

---

### E27. 운영 관측성 — Prometheus + 구조 로그 (1주)

**왜**: production 운영자는 healthz 외에 dispatch latency·event throughput·LLM 토큰 사용량을 metric으로 봐야.

#### 스코프
- `/metrics` endpoint (Prometheus exposition format) — 옵트인 `--metrics-addr`
- 핵심 metric: `rosshield_scans_started_total{tenant}`, `rosshield_webhook_deliveries_total{status}`, `rosshield_audit_chain_head_seq{tenant}`, `rosshield_event_publish_duration_seconds`
- slog 구조 로그 일관성 — 모든 도메인 emit이 tenant_id·correlation_id 포함
- README에 Prometheus scrape config + Grafana dashboard 예시

#### TDD 태스크
| ID | 테스트 | 구현 |
|---|---|---|
| E27.T1 | `TestMetricsEndpointExposesAllExpectedSeries` | `/metrics` 응답 파싱 + 핵심 시리즈 존재 검증 |
| E27.T2 | `TestScanStartedMetricIncrementsOnce` | scan 1회 시작 → counter +1 |
| E27.T3 | `TestStructuredLogContainsTenantAndCorrelation` | scan 흐름 log 캡처 → 모든 entry에 두 필드 존재 |

#### Exit 기준
- Prometheus가 30s 주기로 metric 수집 → Grafana로 시각화 1개 dashboard 시연

---

### E28. Backup·Restore + 운영 도구 (3일)

**왜**: customer가 단일 인스턴스에서 데이터 손실 위험을 인지하고 백업 절차 필요.

#### 스코프
- `rosshield-server backup --output <path>` 서브커맨드 — SQLite VACUUM INTO + evidence blobs tar.gz
- `rosshield-server restore --input <path>` — 빈 DataDir에 복원
- 백업 파일은 `cosign attest`로 무결성 보증 옵션
- README에 cron 예시 + S3 업로드 patten

#### TDD 태스크
| ID | 테스트 | 구현 |
|---|---|---|
| E28.T1 | `TestBackupRoundTrip` | 시드 데이터 → backup → 새 디렉터리에 restore → 도메인 read 동등 |
| E28.T2 | `TestBackupSkipsLockedFiles` | 서버 실행 중 backup → consistent snapshot |

#### Exit 기준
- backup 1회 실행 → 다른 머신에 restore → admin login + scan list 동작

---

### E29. CLI 운영 명령 확장 (3일)

**왜**: PoC 운영자가 GUI 없이 CLI로 모든 admin task를 수행 가능해야.

#### 스코프
- `rosshield invite create --email <e> --role <r>` → invitation token 출력
- `rosshield invite list` / `rosshield invite revoke <id>`
- `rosshield user list` / `rosshield user role-assign <userId> <roleName>`
- `rosshield webhook list` / `rosshield webhook test <id>` (one-off ping)
- `rosshield license info` (이미 있음 — 확장: usage 도 포함)

#### TDD 태스크
| ID | 테스트 | 구현 |
|---|---|---|
| E29.T1 | `TestInviteCreateOutputsTokenAndAcceptUrl` | mock 서버 + CLI → invitation token + accept URL 출력 |
| E29.T2 | `TestUserRoleAssignAuditEmitted` | role 변경 → audit chain에 user.role.assigned |

#### Exit 기준
- README "운영 cheatsheet" 1페이지 완성 (모든 admin task를 CLI로)

---

### E30. 외부 감사인 검증 SDK (3일, 옵션)

**왜**: 외부 감사인이 report bundle을 자체 도구로 검증할 수 있어야 P1 충족.

#### 스코프
- `cmd/rosshield-audit-verify/` — 외부에서 download 가능한 standalone 검증 도구
- 입력: report tar.gz, 출력: PASS/FAIL + 각 단계 (사인 검증·체인 anchor·evidence sha256·payload 검증)
- 의존: 본 repo의 `internal/domain/reporting` 일부만 export (별 module 또는 same module)

#### TDD 태스크
| ID | 테스트 | 구현 |
|---|---|---|
| E30.T1 | `TestAuditVerifyToolValidatesGoldenBundle` | golden bundle → PASS |
| E30.T2 | `TestAuditVerifyDetectsSignatureTamper` | sig 변조 → FAIL with reason |

#### Exit 기준
- 외부 감사인 1명이 README만 보고 verify 명령 실행 가능 (자가 시연)

---

## Phase 4 Web Console 갭

| ID | 페이지 | 의존 |
|---|---|---|
| B6 | `/metrics-dashboard` (옵션) — backend Prometheus 그래프 | E27 |
| B7 | `/backups` — 최근 backup 목록 + 다운로드 | E28 |

---

## 의존 그래프

```
Carryover E25 HA ────┐
Carryover E22-F PG-native ────┤   (병렬 진입)
Carryover O5 SSO autoprov ───┤
Carryover O6 invite email ───┤
Carryover O7 webhook UI ─────┤
E26 release pipeline ────────┤
E27 observability ───────────┤
E28 backup ──────────────────┤
E29 CLI ext ─────────────────┘
        │
        └──→ E30 audit verify SDK (선택, E26 release 후 게시)
```

병렬 가능: 거의 모두 (도메인 영향 적은 운영 강화 epic). E30은 release 파이프라인 후 효과적.

---

## 추정 (병렬 + 1인 운영 가정)

| Epic | 단독 추정 | 병렬 단축 |
|---|---|---|
| E25 HA | 4일 | (병렬) |
| E22-F PG-native | 1주 | (병렬) |
| O5 SSO autoprov | 3일 | (병렬) |
| O6 invite email | 3일 | (병렬) |
| O7 webhook UI | 2일 | (병렬) |
| O8 demo seed 확장 | 1일 | (병렬) |
| E26 release pipeline | 1주 | (병렬) |
| E27 observability | 1주 | (병렬) |
| E28 backup | 3일 | (병렬) |
| E29 CLI ext | 3일 | (병렬) |
| E30 audit verify SDK | 3일 | E26 후 |
| **합계** | **6.5주** | **~5주** |

---

## 리스크 (Phase 4 한정)

| 리스크 | 완화 |
|---|---|
| HA leader/follower split-brain | PG advisory lock + heartbeat + 명시적 fence token |
| PG-native repo 분기 시 cross-tenant leak | shared 테스트 fixture로 양 driver 동등성 강제 |
| cosign keyless flow가 OIDC 의존 → CI 환경에서 깨짐 | self-managed key option fallback (env로 sk_release.pem) |
| metric cardinality explosion (tenant 수 많아지면) | summary metric은 tenant label 제외, counter만 포함 |
| backup tar.gz 크기 (evidence blob) | --skip-evidence option으로 metadata-only 백업 분리 |

---

## Phase 4 Exit 체크리스트

- [ ] HA 2 인스턴스 leader/follower 동작 (E25)
- [ ] tag push → multi-arch signed binary 자동 게시 (E26)
- [ ] Prometheus metric + Grafana dashboard 1개 시연 (E27)
- [x] backup → restore round-trip 동작 (E28)
- [ ] admin task 100%를 CLI로 수행 가능 (E29)
- [ ] 외부 감사인이 report verify CLI 실행 (E30)
- [ ] (옵트인) PG-native repo 분리 (E22-F)
- [ ] customer 1명이 30일 운영 + incident 0

---

## R30 결정 후보 / 결정 항목

- **R30-1** PG-native repo 분리 시점 — Phase 4 초입 vs 첫 customer feedback 후. 영향: storage layer 큰 변경. (보류)
- **R30-2** HA 첫 구현 — single-writer(advisory lock) vs 무관(read replica). single-writer가 audit chain 일관성에 안전. (보류 — E25 시작 전 결정)
- **R30-3** release signature — cosign keyless(GitHub OIDC) vs self-managed key. CI 의존도 차이. (보류 — E26 시작 전 결정)
- **R30-4** D5/D6 라이선스·visibility — **✅ 2026-05-08 결정**:
  - **D5**: Open-core 채택 — 코어 Apache-2.0 + enterprise 별 라이선스(BSL/Commercial 구체는 첫 paying customer 직전). 코드 분리는 단일 repo + build tag(R20-2). 실제 분리 시점은 첫 paying customer 직전.
  - **D6**: GitHub private 유지. release binary + report verify CLI(E30)가 P1 외부 검증 대체. 첫 enterprise customer 또는 Phase 5 진입 시 재논의 옵션.
  - **R20-1** 코어 라이선스 = Apache-2.0 (확정)
  - **R20-2** 코드 분리 = 단일 repo + build tag (확정)
  - **R20-3** GitHub visibility = private (확정 — D6와 동일)

---

## Phase 3 → Phase 4 진입 체크리스트

- [x] phase3-backlog.md → archive로 이전
- [x] phase4-backlog.md(본 문서) 신규 작성
- [x] **R30-4 D5/D6 라이선스·visibility 결정** (2026-05-08): Open-core + Apache 코어 + private repo + 단일 repo build tag
- [ ] Carryover 우선순위 사용자 합의 (E25/E22-F/O5~O8 중)
- [ ] 첫 customer PoC onboarding 자료 (Phase 4 시작 전 소프트 마일스톤)

---

## 문서 생명주기

- 본 백로그는 **살아있는 문서**. 태스크 완료 시 `[x]` + 커밋 해시.
- Phase 4 완료 시 `docs/design/archive/phase4-backlog.md`로 이동, Phase 5 백로그를 동일 경로에 신규.
- 결정 사항은 `SESSION_HANDOFF.md` "결정 로그"에 R30-X 형식으로 기록.
