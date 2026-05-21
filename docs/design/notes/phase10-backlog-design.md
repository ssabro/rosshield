# Phase 10 Backlog — Phase 9 마감 직후 차기 milestone 8 후보 매트릭스 + Top 3 권장 — Design

> **상태**: Phase 9(자동 failover 통합, v0.8.0) + v0.8.1~v0.8.5 patch 시리즈(CI baseline + E35-refresh redesign + snap hot fix) 마감 직후 Phase 10 진입 합의용 design doc. 본 문서는 코드 0줄 / 마이그레이션 0건 / pack 변경 0 — Phase 0~9 회고 + Phase 10 후보 8종 매트릭스 + Top 3 권장 + 결정 항목 권장 default까지만 마감합니다.
> **참조**:
> - 직전 design doc 패턴: `notes/auto-failover-research.md`(Phase 9 진입) · `notes/phase6-backlog-design.md`(Phase 6 진입) · `notes/multi-region-ha-design.md`(Phase 8) · `notes/customer-onboarding-design.md`(Phase 6 1순위).
> - 마감 release: `docs/releases/v0.8.5.md`(2026-05-21 hot fix) — head `5a55da5` 기준.
> - CHANGELOG: `CHANGELOG.md` [0.8.0]~[0.8.5] entry.
> - 설계서: `docs/design/11-tech-stack-and-roadmap.md` 로드맵 + `docs/design/01-principles.md` 12 원칙 + `docs/design/13-patent-strategy.md` D8 청구권.
> **R 식별자**: R-PHASE10-1 (본 doc 전체) — 결정 항목은 D-P10-1·2.
> **본 문서 작성 위치**: main(head `5a55da5`), 단독 sub-agent.

---

## 1. 상태 / 배경

### 1.1 Phase 0~9 마감 요약 (한 줄씩)

| Phase | 마감 시점 | 핵심 산출 한 줄 |
|---|---|---|
| **Phase 0** | 2026-04-23 | 설계서 13개 + D1~D8 결정 6건 + 코드 부트스트랩 0(설계 only). |
| **Phase 1** | 2026-05-08 | E1~E5 storage·auth·audit·robot fleet 도메인 + 멀티테넌시·PG·sqlite repo dual-target. |
| **Phase 2** | 2026-05-12 | E6~E8 scan 엔진·evidence·reporting + 첫 pack(CIS Ubuntu 22.04). |
| **Phase 3** | 2026-05-14 | ROS2 Jazzy pack baseline(329 check 자동 변환) + audit verify SDK(`fg-verify`). |
| **Phase 4** | 2026-05-15 | E25 HA leader/follower(PG advisory lock) + E27 observability(Prometheus + Grafana) + E28 backup. |
| **Phase 5** | 2026-05-15 | E6-T scanrun SSH + 세분 RBAC + PWA 오프라인·persist + RBAC fleet 정밀화 + SSO group → role + E33~E36 appliance(snap·TPM·OTA). |
| **Phase 6** | 2026-05-17 | R1+R2+R3 customer onboarding(intake API + walkthrough script + SLA template). |
| **Phase 7** | 2026-05-19 | R-BRAND Lodestar 확정 + Apache 2.0 + R-D8 v3 청구권 4종 + repo public 전환. |
| **Phase 8** | 2026-05-20 | PG cross-region logical replication + Route53/Cloudflare DNS routing + manual failover runbook. |
| **Phase 9** | 2026-05-20 | Patroni 자동 failover 통합(v0.8.0) — D-AF-1~4 모두 권장 default 채택 + RoleProvider Patroni REST swap + `--ha-rp` flag. |
| **v0.8.1~v0.8.5 patch** | 2026-05-21 | CI baseline 안정화(PG flaky · MinIO · Playwright · Snap Smoke) + E35-refresh redesign(post-refresh 단순화 + check-health hook 신규) + v0.8.4 broken release v0.8.5 hot fix. |

### 1.2 Phase 10 진입 가치

Phase 9 마감 시점에 baseline은 다음과 같이 정리됩니다:
- **HA·DR**: single-region E25 + cross-region replication + Patroni 자동 failover 3 layer 모두 결선. RTO ≤ 60초 도달 가능.
- **인증·격리**: SSO + 세분 RBAC + fleet scope + tenant 멀티 격리 모두 결선.
- **scan·evidence**: ROS2 baseline 329 check + cis-ubuntu-2404 + ros2-jazzy 22 check(8/8 카테고리 cover) + audit chain hash 체인 + cosign signed evidence 모두 결선.
- **appliance**: snap strict confined + TPM 봉인 + Secure Boot + A/B OTA + reference HW docs 결선.
- **enterprise scaffold**: 7 패키지 중 4 패키지(crosswitness · multihash · wasmrt · robotid) 실 구현 진행 중, 3 패키지(fleetxval · rostopo · selectdisclose) placeholder.
- **release infra**: v0.3.0~v0.8.5 = 33 release(cosign keyless 서명 + Sigstore Rekor 등록) + amd64+arm64 snap.

차기 milestone은 (a) 결선된 인프라를 **customer-facing UX로 표면화** (b) **enterprise/compliance 가치 증분** (c) **observability/scale 진화** 세 축이 후보 풀입니다. memory `feedback_design_doc_first.md` 일관 — Phase 10 진입 시 1일+ 임계 작업은 design doc 우선.

---

## 2. 현재 상태 fact-check

본 §은 코드/디렉터리 직접 grep 결과 — 추측 0, fact만 명시.

### 2.1 enterprise 패키지 7종 현황 (가설 4)

`internal/enterprise/` 디렉터리(2026-05-21 기준):

| 패키지 | 파일 수 | 상태 | 산출 |
|---|---|---|---|
| `crosswitness/` | 8 files | **실 구현 중** | `anchor.go`(WebhookAnchor + FilesystemDumpAnchor) + `fold.go` + `scheduler.go` + 단위 테스트. A-1 cross-witness 외부 anchoring R-D8 청구권 cover. |
| `multihash/` | 8 files | **실 구현 중** | `compute.go` + `verify.go` + `jsonpath.go` + 단위 테스트. B-1 multi-hash evidence R-D8 청구권 cover. |
| `wasmrt/` | 13 files | **실 구현 중** | `runtime.go` + `cosign.go` + `policy.go` + `sigstore.go` + `limits.go` + 단위 테스트. C-1 WASM sandboxed evaluator R-D8 청구권 cover. |
| `robotid/` | 17 files | **실 구현 중** | `collector.go`(linux + other) + `fingerprint.go` + `quote_attestation.go` + `tpm_linux.go` + simulator test. D-3 robot identity binding R-D8 청구권 cover. |
| `fleetxval/` | 2 files | **placeholder** | `enterprise.go`에 `EditionTag = "enterprise"`만. |
| `rostopo/` | 2 files | **placeholder** | `enterprise.go`에 `EditionTag = "enterprise"`만. |
| `selectdisclose/` | 2 files | **placeholder** | `enterprise.go`에 `EditionTag = "enterprise"`만. |

7 패키지 중 4개는 R-D8 v3 마감으로 핵심 본체 진행. **잔여 placeholder 3개**(fleetxval · rostopo · selectdisclose)는 미진행.

### 2.2 LLM provider 현황 (가설 2)

`internal/platform/llm/`:
- `llm.go` — `Adapter` interface + `LlmTrace` + ErrLLMDisabled 등.
- `noop/` — 기본값(R14-1).
- `anthropic/` — cloud HTTPS.
- `ollama/` — 로컬 daemon(self-hosted, CPU/GPU).
- `vllm/` — OpenAI-compatible(GPU + continuous batching, self-hosted production).

`docs/design/notes/llm-private-deployment-design.md` + `docs/onboarding/llm-private-deployment.md` 결선 — ollama·vLLM driver 양쪽 운영자 가이드 + CPU/GPU 시나리오 매트릭스 cover. **가설 2의 "Ollama/llama.cpp 통합"은 사실상 결선**.

미진행 영역:
- **redaction 자동화** — `evidence.Redact`은 caller(Insight/Advisor)가 prompt에 적용해야 하나 advisor orchestrator·tool 호출 trace의 audit emit 자동화는 부재.
- **reasoning trace UI 강화** — advisor 결정에 step-by-step trace 표시(어드바이저 도구 호출 입력 + 출력 digest 보존).
- **token budget cost guardrail UI** — `R14-6` tenant daily token limit 통계 운영자 UI 미존재.

### 2.3 docs/compliance 디렉터리 (가설 3)

`docs/` 트리:
- `docs/onboarding/` — 12 markdown(quickstart + walkthrough + multi-region + audit-rotation + llm-private 등).
- `docs/operations/` — 10 markdown(snap-deployment + ha-deployment + patroni-deployment + multi-region-dns + multi-region-failover-runbook 등).
- `docs/appliance/` — `reference-hardware.md` 1 file.
- `docs/ip/` — D8 spec draft 2 file.
- `docs/releases/` — v0.3.0~v0.8.5 = 33 release notes.
- `docs/design/` — 14 design 문서 + 50+ notes.

**`docs/compliance/` 디렉터리는 없음**. SOC2 control mapping · CC1~CC9 매트릭스 · 외부 감사인 접근 절차 · 통제 effectiveness 측정 dashboard 모두 미존재. compliance grep 결과는 release notes/CHANGELOG/release verification 맥락만.

### 2.4 ros2-jazzy pack 현황 (가설 7)

`packs/`:
- `cis-ubuntu-2404/` — CIS Ubuntu 24.04 pack.
- `ros2-jazzy/` — **22 check** (8/8 카테고리 cover 완성, Phase 7 마감 시점):
  - C1 sros2/security (2) · C2 cmd-vel ACL/publisher (2) · C3 domain-id (1) · C4 binary 무결성 (5) · C5 launch 안전 (5) · C6 distro lifecycle (3) · C7 RMW (1) · C8 governance encryption (1).
- `ros2-jazzy-baseline/` — **329 check** 자동 변환 from nrobotcheck 전신.

**미진행 영역**:
- **ros2-humble pack** — Humble Hawksbill 신규 pack 없음.
- **SROS2 cert chain validation** — C1에서 keystore exists + security enable만, 실 cert chain expiry/CA trust 검증 부재.
- **DDS topic ACL 정밀화** — C2 cmd-vel만, 일반 topic ACL 검증 부재.
- **ROS2 Round 3 carryover 6건** — apt_key 만료 · colcon_install_hash digest · signed_packages_only · param_files_owner · argv_no_remote_url · lifecycle_node_used (handoff 명시).

### 2.5 multi-region UI 현황 (가설 1)

코드 grep 결과:
- `internal/api/handlers/replication.go` — GET `/api/v1/replication/replicas` + POST `/api/v1/replication/heartbeat` + POST `/api/v1/replication/failover` + GET `/api/v1/audit/head-sha` (E-MR Phase 8 Stage 2). admin manual failover endpoint 결선.
- `internal/platform/replication/` — repository + policy + middleware + lagmetric + setup + sqliterepo.
- `web/src/` — region 키워드 0(test/axe.ts와 a11y.test.tsx의 'region' aria role만).

**web UI에 region-aware UX 없음**. 운영자는 `/api/v1/replication/*` REST 또는 ops runbook(`docs/operations/multi-region-failover-runbook.md`)으로 cutover 수행. region별 health dashboard · cross-region audit chain consistency 운영자 UI · region failover alert 자동화 모두 미존재.

### 2.6 observability 현황 (가설 8)

- `internal/platform/metrics/` — Prometheus metrics + eventbridge + HA gauge(rosshield_ha_role · leader_epoch · failover_total).
- `deploy/grafana/` — Grafana dashboard JSON(17 panel, schemaVersion 39).
- OpenTelemetry / distributed tracing: **코드 0**. go.mod에 transitively appears(외부 lib dep) 외 자체 사용 없음. tracing 키워드는 design docs/CIS pack(audit log path)만.

### 2.7 web UI i18n 현황 (가설 6)

- `web/src/i18n/` — `dict.ts`(ko + en 2개 language) + `t.ts` + `store.ts`.
- ko 기본, en fallback. dict 키 동기 강제(누락 시 CI fail).
- 다국어 추가 없음 — ja/zh/es 등 없음.

### 2.8 scanrun 현황 (가설 5)

- `internal/platform/sshpool/` — Pool idle 재사용 + keepalive + 5 metrics(Phase 5 Stage 5b 마감).
- `internal/scanrun/`(추정 위치) — SSH integration + per-robot HealthFailureThreshold=3.
- **scanrun extras epic A·B·C·D 보류**(Phase 6 carryover) — Pool size 동적 · per-tenant rate limit · per-robot circuit breaker · observability metric 확장. customer trigger 대기.
- 실 fleet scale 50~100 robot 검증 부재.

---

## 3. 위협 모델 / 요구사항 (Phase 10 진입 시)

### 3.1 신규 위협

| 위협 | 가능성 | 영향 | Phase 10 cover 영역 |
|---|---|---|---|
| enterprise customer data sovereignty 위반 | 중(국방·금융·정부) | 계약 불성립 + 외부 SaaS LLM 차단 | LLM private 결선됨(가설 2 사실상 cover), 부수 redaction 자동화는 후보. |
| SOC2 Type II 통과 실패 | 높음(enterprise 영업 critical gate) | 영업 기회 손실 | docs/compliance/ 트리 + control mapping(가설 3). |
| multi-region cutover 운영자 사각지대 | 중(자동 failover 후 알람 미수신) | 외부 알람 누락 + customer 모름 | region UX 표면화(가설 1). |
| ros2-humble customer 진입 거부 | 중(Jazzy 외 distro 사용 customer) | 영업 기회 손실 | humble pack(가설 7). |
| 영구 장애 robot이 Run scope 막힘 | 낮음(첫 customer 환경에서 가설) | scan duration 길어짐 | scanrun extras epic C(가설 5). |
| audit chain cross-region inconsistency 감지 부재 | 낮음(replication 정합 시) | 외부 검증 시 회의 발생 | region UX + observability(가설 1 + 8). |

### 3.2 요구사항

| ID | 요구 | 측정 |
|---|---|---|
| R10-1 | Phase 10 1순위 epic은 외부 트랙 의존 0 | D1 변리사/E36 hands-on/customer trigger 의존 없음 |
| R10-2 | 1순위 epic 추정 ≤ 1개월 (보수적) | Stage 분해 합계 |
| R10-3 | 회귀 위험 ≤ 중급 | audit chain · 멀티테넌시 격리 변경 표면 작음 |
| R10-4 | Phase 0~9 baseline 회귀 0 | 기존 50+ Go test 패키지 + 195 RBAC 통합 + Playwright e2e PASS |
| R10-5 | design doc 우선 | 1일+ 임계 작업은 doc 먼저 (memory `feedback_design_doc_first.md`) |
| R10-6 | 보수적 추정 일관 | memory `feedback_design_doc_conservative.md` |

---

## 4. Phase 10 후보 8 옵션 비교

각 옵션마다 (a) 설계 요약 (b) 가치 (c) 노력 추정(보수적) (d) 전제·의존 (e) 리스크.

### 4.1 옵션 A — multi-region 실 UI 표면화 (가설 1)

**설계 요약**: Phase 8+9에서 결선된 replication/Patroni 인프라를 **customer-facing UX**로 노출. 신규 web 페이지 `/regions` — region별 health 카드 + replication lag 시각화 + 자동 cutover event timeline + cross-region audit chain consistency 검증 결과. region failover 시점 운영자 알람 자동화(WebHook + email + slack).

**가치**:
- paying customer ★★★ (multi-region 도입 customer 직접 가치) / enterprise ★★★ / compliance ★★ / operational ★★★★ (운영자 가시성) / 기술 부채 ★★

**노력 추정 (보수적)**: **3~4주**. web /regions 페이지 1주(replication 4 endpoint 이미 결선) + cross-region audit chain head 비교 UI 0.5주 + region cutover timeline component 0.5주 + Prometheus alert rule + webhook trigger 1주 + e2e + i18n 0.5~1주.

**전제·의존**: Phase 8+9 결선만 의존. region 추가 customer가 없어도 가치(기존 customer multi-region 운영 가시성).

**리스크**: **낮음**. 신규 endpoint 없음(기존 replication API 재사용). 회귀 표면 작음.

### 4.2 옵션 B — SOC2 Type II readiness (가설 3)

**설계 요약**: `docs/compliance/` 신규 트리 — SOC2 CC1~CC9 + A1~A6 control mapping (Lodestar 결선 자산 + audit chain · cosign 서명 · RBAC · TPM 봉인 등을 SOC2 통제로 매핑). 외부 감사인 접근 절차(read-only role + 감사 사본 export wizard) + 통제 effectiveness 측정 dashboard + 신규 benchmark pack `soc2-controls` (CIS 처럼 yaml 구조, SOC2 통제 단위).

**가치**:
- paying customer ★★★★★ (enterprise 영업 critical gate) / enterprise ★★★★★ / compliance ★★★★★ / operational ★★★ / 기술 부채 ★

**노력 추정 (보수적)**: **6~8주**. control mapping 매트릭스 docs 2주 + 외부 감사인 access wizard(read-only role + export) 1주 + effectiveness dashboard(통제별 audit event 집계) 1~2주 + soc2-controls pack 신규(~80~120 check) 2~3주 + 외부 SOC2 감사인 검토 라운드(외부 트랙).

**전제·의존**: 외부 SOC2 감사인 컨설팅(파트너 + 실 감사 라운드)은 별 외부 트랙(★ 표기). docs/control mapping은 외부 트랙 의존 0이나 실 SOC2 Type II 인증은 외부 감사인 90일 운영 측정 후 가능.

**리스크**: **중**. 신규 docs 트리 + 신규 pack은 회귀 위험 낮으나 effectiveness dashboard가 audit event 집계로 read-heavy query 추가 → 성능 영향 검증 필요.

### 4.3 옵션 C — enterprise plugin 잔여 3 패키지 1차 구현 (가설 4)

**설계 요약**: 7 패키지 중 placeholder 잔여 3개(fleetxval · rostopo · selectdisclose) 1차 본체 구현. 후보 우선순위:
- **selectdisclose** (D-2 R-D8 청구항 — selective disclosure / ZK redaction): customer가 외부 감사인에 evidence 일부만 공개 + 나머지 cryptographic commitment 유지.
- **rostopo** (E-1 R-D8 — ROS2 그래프 cross-validation): scan 결과 graph topology 검증으로 ROS2 application layer 정밀.
- **fleetxval** (F-1 R-D8 — fleet 간 cross-validation): 여러 robot의 동시 scan 결과 일관성 검증.

**가치**:
- paying customer ★★ / enterprise ★★★★ / compliance ★★★ / operational ★ / 기술 부채 ★★★ / IP 보호 ★★★★

**노력 추정 (보수적)**: **4~6주** (한 패키지당 ~2주). selectdisclose 알고리즘 + jsonpath redact + cryptographic commitment(SHA256 또는 Merkle commitment) + 단위 테스트 + 통합. rostopo는 ROS2 그래프 parsing + cross-validation 알고리즘. fleetxval은 fleet 간 다수결 알고리즘.

**전제·의존**: D1 변리사 출원 진행 상태에 따라 disclosure 시점 영향(★ 표기 — 사용자 외부 트랙). 출원 *전* 외부 PoC 차단은 D8 결정 일관. 코드 자체는 enterprise build tag 안이므로 코어 코드베이스 disclosure 0.

**리스크**: **중**. enterprise build tag 안에서만 컴파일되므로 코어 회귀 0. R-D8 청구권 disclosure 위험은 출원 후 진입 권장(★ 의존).

### 4.4 옵션 D — audit chain key rotation 자동화 (Phase 6 carryover에서 이월)

**설계 요약**: Phase 6 design doc(`phase6-backlog-design.md` 후보 4)에서 2순위로 권장되었으나 customer onboarding(1순위) 진입 후 carryover 처리됨. `audit-rotation-cosign.md` + `audit-rotation-s3.md` + `audit-rotation-verify.md` docs는 결선이나 자동 rotation 코드 미진행.

옵션 B(정기 + 운영자 승인 + audit emit) — scheduler(rosshield_audit_rotation_total metric) + admin UI 승인 + `audit_chain.key_rotated` event + `fg-verify` rotation aware + 마이그레이션 1건(audit_chain_keys epoch별 public key 보존).

**가치**:
- paying customer ★★ / enterprise ★★★★ / compliance ★★★★★ (ISMS-P · NIST 800-53 SC-12) / operational ★★★★ / 기술 부채 ★★★

**노력 추정 (보수적)**: **2~4주**. scheduler + 새 key 생성 + signer hot-swap + admin UI 승인 + audit emit + 외부 검증 도구 갱신 + 마이그레이션 + 단위/통합 + docs.

**전제·의존**: 없음(Phase 6 carryover 일관). compliance baseline 강화는 첫 customer 진입 *전*에도 가치.

**리스크**: **중~높음**. audit chain head 변경 직후 외부 검증 호환성 보장 critical. rotation 실패 시 audit emit 차단 위험 — fail-safe 설계 필수.

### 4.5 옵션 E — ros2-humble pack + DDS/SROS2 깊이 확장 (가설 7)

**설계 요약**: ROS2 Humble Hawksbill 신규 pack(LTS 분기 customer cover). 카테고리 깊이 확장 — SROS2 cert chain expiry/CA trust 검증 + DDS topic ACL 정밀화(`/scan`·`/odom` 등 일반 topic ACL). ROS2 Round 3 carryover 6건(apt_key 만료 · colcon_install_hash digest · param_files_owner 등)도 자연 cover.

**가치**:
- paying customer ★★★ (Humble distro customer 진입) / enterprise ★★ / compliance ★★★ / operational ★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **3~4주**. ros2-humble pack 신규(jazzy 22 check를 humble로 변환, distro 차이 검증) 1주 + SROS2 cert chain check 3종 1주 + DDS topic ACL check 4~6종 1주 + Round 3 carryover 6건 0.5주 + e2e fixture + selftest.

**전제·의존**: 없음. paying customer가 Humble 명시 요구 *전*에도 baseline 가치.

**리스크**: **낮음**. pack 변경은 isolated(scan 엔진 변경 0). cis-ubuntu-2404 + ros2-jazzy-baseline 결선 패턴 일관.

### 4.6 옵션 F — observability OpenTelemetry 전면 (가설 8)

**설계 요약**: Prometheus metrics는 결선되어 있으나 **distributed tracing** 부재. OpenTelemetry SDK 도입 — scan flow(SSH connect → check exec → evidence write → audit emit) trace + multi-region request trace + advisor LLM call trace. Prometheus federation으로 customer-facing metrics export. log aggregation pipeline(structured slog → Loki/ELK).

**가치**:
- paying customer ★★ / enterprise ★★★★ (관측성 가치 enterprise 영업 도움) / compliance ★★★ (audit event 트레이싱 cross-system) / operational ★★★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **6~8주**. otel SDK 통합 + scan/advisor/audit/replication 4 hot path tracing 2주 + multi-region trace propagation 1주 + customer Prometheus federation 1주 + log aggregation pipeline + Loki/ELK 호환 어댑터 2주 + docs/dashboard 갱신 + e2e.

**전제·의존**: 없음. 기존 Prometheus metrics 회수 없음.

**리스크**: **중**. otel SDK 추가 dep + 모든 hot path span emit으로 인한 latency 영향 측정 필요(sample rate 조절). air-gap customer otel 비활성 옵션 필수.

### 4.7 옵션 G — web UI 진화 + dashboard 커스터마이징 (가설 6)

**설계 요약**: 운영자별 dashboard 커스터마이징(panel 선택 + 배치 + saved layout) + 다국어 폭 확장(ko/en → ja/zh/es 추가) + dark mode tuning(C5b-10 a11y carryover) + report builder UI(현재 PDF 외 JSON/CSV export).

**가치**:
- paying customer ★★★ (UX 가치) / enterprise ★★ / compliance ★ / operational ★★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **4~6주**. dashboard 커스터마이징(panel registry + drag-drop layout + saved layout persist) 2주 + 다국어 3개 추가(번역 + dict 동기 + RTL 미진행) 1.5~2주 + dark mode tuning(C5b-10) 0.5주 + report builder UI(JSON/CSV export + 형식 선택 wizard) 1주 + e2e.

**전제·의존**: 없음. PWA + RBAC + i18n 결선 baseline 활용.

**리스크**: **낮음~중**. dashboard layout persist는 IndexedDB(PWA persist 결선 활용) — 회귀 표면 작음. 다국어는 dict 동기 부담 증가(번역 품질 외부 검토 별 트랙).

### 4.8 옵션 H — scanrun extras + 실 fleet scale 검증 (가설 5)

**설계 요약**: Phase 6 carryover scanrun extras epic A·B·C·D 진입. concurrent scan 동시성 한도(Pool size 동적) + 결과 aggregation 성능 + cancellation graceful + scan profile templating + per-tenant rate limit + per-robot circuit breaker + observability metric 확장. 50~100 robot scale e2e benchmark.

**가치**:
- paying customer ★★ (customer trigger 후 가치 증분) / enterprise ★★★ / compliance ★ / operational ★★★★ / 기술 부채 ★★★

**노력 추정 (보수적)**: **4~6주**. epic A 1주 + B 1주 + C 1.5주 + D 0.5주 + scale benchmark e2e fixture(docker-compose 50~100 sshd) 1~2주.

**전제·의존**: customer 환경 부하 데이터(가설 단계). customer 진입 *전*에는 부하 측정 데이터 부재.

**리스크**: **중**. concurrent scan + circuit breaker 변경은 scanrun core path 영향 — 회귀 위험. e2e benchmark는 CI 비용 추가.

### 4.9 매트릭스 종합

| 옵션 | 가설 | 가치 종합 | 시간 | 위험 | 즉시 진입 | 외부 트랙 의존 |
|---|---|---|---|---|---|---|
| **A** multi-region UI 표면화 | 1 | ★★★★ | 3~4주 | 낮음 | ✅ | 0 |
| **B** SOC2 Type II readiness | 3 | ★★★★★ | 6~8주 | 중 | ✅(docs 우선) | ★(실 감사 외부) |
| **C** enterprise 잔여 3 패키지 | 4 | ★★★★ | 4~6주 | 중 | ⚠️(D1 출원 후 권장) | ★(D1) |
| **D** audit chain key rotation | (Phase 6 carryover) | ★★★★ | 2~4주 | 중~높음 | ✅ | 0 |
| **E** ros2-humble + DDS/SROS2 깊이 | 7 | ★★★ | 3~4주 | 낮음 | ✅ | 0 |
| **F** OpenTelemetry 전면 | 8 | ★★★★ | 6~8주 | 중 | ✅ | 0 |
| **G** web UI 진화 | 6 | ★★★ | 4~6주 | 낮음~중 | ✅ | 0 |
| **H** scanrun extras + scale | 5 | ★★★ | 4~6주 | 중 | ⚠️(customer trigger 권장) | ★(customer) |

---

## 5. Top 3 권장 + 권장 진입 순서

memory `feedback_design_doc_conservative.md` 일관 — 잠재 효과/시간 보수적.

### 5.1 1순위 — 옵션 A (multi-region UI 표면화)

**근거**:
- Phase 8+9에서 결선된 인프라(replication 4 endpoint + Patroni REST + Route53/Cloudflare + ops runbook)의 가치 회수 — **기 결선 자산의 customer-facing 표면화**.
- 외부 트랙 의존 0 + customer trigger 의존 0.
- 회귀 위험 낮음(신규 endpoint 0, 기존 read API + Prometheus alert 신규만).
- 추정 3~4주 — R10-2(≤ 1개월) 충족.
- multi-region customer가 이미 진입했거나 잠재 진입 시 즉시 가치(operational 가시성 + cutover 안전성).
- Phase 9 Patroni 자동 failover 직후 운영자 가시성 gap 자연스럽게 보강.

### 5.2 2순위 — 옵션 D (audit chain key rotation 자동화)

**근거**:
- Phase 6 design doc에서 2순위로 권장되었으나 carryover 처리 — Phase 10에서 자연 재진입.
- compliance 가치 ★★★★★ (ISMS-P · NIST 800-53 SC-12 명시 요구).
- 외부 트랙 의존 0.
- 추정 2~4주 — R10-2 충족.
- 1순위(옵션 A) 마감 후 compliance baseline 강화로 enterprise 영업 가치 증분.
- 옵션 B(SOC2)의 선결 부속 — SOC2 CC6.6(암호 키 관리)에서 정기 rotation 명시.

### 5.3 3순위 — 옵션 E (ros2-humble + DDS/SROS2 깊이)

**근거**:
- paying customer Humble distro 진입 시 즉시 활용 가능 — baseline pack 확장은 paying customer 영업 직격.
- 회귀 위험 **낮음** (pack 변경 isolated, scan 엔진 회귀 0).
- 추정 3~4주.
- 외부 트랙 의존 0 + customer trigger 의존 0(있어도 가치).
- ROS2 Round 3 carryover 6건이 자연 cover 되어 ROS2 영역 마감.

### 5.4 권장 보류 (Phase 10 default에서 제외)

#### 5.4.1 옵션 B (SOC2 Type II) — 보류, 2순위 후 재평가

가치 ★★★★★ + 추정 6~8주 + 실 SOC2 감사는 외부 트랙(★). docs/mapping 1차 작업은 1·2·3순위 마감 후 자연 진입. **Phase 11 1순위 후보**.

#### 5.4.2 옵션 C (enterprise 잔여 3 패키지) — 보류, ★ D1 출원 의존

memory `feedback_user_tracks.md` 정책 — D1 변리사 출원은 사용자 외부 트랙. 출원 *전* 외부 disclosure 위험. R-D8 v3 4 청구권은 이미 cover됨(crosswitness · multihash · wasmrt · robotid).

#### 5.4.3 옵션 F (OpenTelemetry 전면) — 보류, 우선순위 중

추정 6~8주 + 옵션 A의 region 표면화 후 자연 진입(multi-region trace propagation 자연 cover). **Phase 11 2순위 후보**.

#### 5.4.4 옵션 G (web UI 진화) — 보류, 우선순위 중

UX 가치 중급 + customer 명시 요구 *전* 우선순위 낮음. dark mode tuning(C5b-10 carryover)만 별도 micro epic 가능.

#### 5.4.5 옵션 H (scanrun extras + scale) — 보류, ★ customer trigger

memory `feedback_user_tracks.md` 일관 — customer 부하 데이터 부재한 가설 단계. customer 진입 후 부하 측정 → 우선순위 재평가.

### 5.5 권장 진입 순서 timeline (보수적)

| 순서 | 옵션 | 추정 누적 시간 | trigger 시점 |
|---|---|---|---|
| 1순위 | A multi-region UI 표면화 | 3~4주 | 본 design doc 채택 직후 |
| 2순위 | D audit chain key rotation | 5~8주 누적 | 1순위 마감 |
| 3순위 | E ros2-humble + DDS/SROS2 깊이 | 8~12주 누적 | 2순위 마감 |
| 보류 (★) | B SOC2 readiness | — | 3순위 마감 + Phase 11 진입 |
| 보류 (★) | C enterprise 잔여 3 패키지 | — | D1 출원 완료 후 |
| 보류 | F OpenTelemetry 전면 | — | Phase 11 |
| 보류 | G web UI 진화 | — | customer 요구 시 |
| 보류 (★) | H scanrun extras + scale | — | 첫 paying customer 부하 측정 후 |

**Phase 10 마감 추정**: 보수적 **8~12주 누적** (1·2·3순위 순차). 마감 시 multi-region 운영자 가시성 + compliance 키 rotation baseline + ROS2 Humble cover 세 축 추가.

---

## 6. Stage 분해 (1순위 옵션 A — multi-region UI 표면화)

memory `feedback_design_doc_first.md` 일관 — 1순위만 본 doc에서 Stage 분해, 2·3순위는 진입 시점에 별 design doc 위임.

### 6.1 Stage 10.A-1 — design doc 채택 + 본 doc

본 round (docs only, 코드 0).

### 6.2 Stage 10.A-2 — `/regions` 페이지 scaffold + region health card

추정 **1주**.
- web `/regions` route + RBAC gate(`viewer` 이상) + sidebar nav.
- React Query hook `useReplicas` — `GET /api/v1/replication/replicas` 30s polling.
- RegionHealthCard 컴포넌트 — region별 role(primary/standby) badge + endpoint + lag(`last_replay_at` vs now) + 색상 코드(green ≤ 5s · yellow 5~30s · red > 30s).
- i18n 키 ~15건 ko+en.
- 단위 test(vitest) + Playwright e2e 1 scenario.

### 6.3 Stage 10.A-3 — cross-region audit chain consistency 검증 UI

추정 **0.5주**.
- React Query hook `useAuditChainHeadSHA` — `GET /api/v1/audit/head-sha` (이미 결선).
- AuditConsistencyCard — region 별 head sha + match 표시(✅ 또는 ❌).
- mismatch 시 ops runbook 링크.
- e2e 1 scenario.

### 6.4 Stage 10.A-4 — region cutover event timeline

추정 **0.5주**.
- 신규 endpoint 0 — 기존 `audit_entries` 중 `action LIKE 'audit.replication.failover%'` 조회(audit handler 활용 또는 dedicated query).
- RegionTimelineCard — 마지막 N개 cutover event(actor + from-region + to-region + status + completed_at).
- 표시는 read-only(failover trigger UI는 별 epic).
- e2e 1 scenario.

### 6.5 Stage 10.A-5 — Prometheus alert rule + webhook trigger

추정 **1주**.
- `deploy/prometheus/alerts/multi-region.yml` 신규 — rules: replication_lag > 30s(warning) · replication_lag > 60s(critical) · audit_chain_head_sha_mismatch(critical) · ha_role_swap(info).
- 기존 webhook delivery 활용 — alert manager → webhook endpoint POST → tenant 알람.
- ops docs 갱신(`docs/operations/multi-region-failover-runbook.md` §10 신규 — alert + dashboard).

### 6.6 Stage 10.A-6 — testcontainers e2e (2-region cutover scenario)

추정 **0.5~1주**.
- 기존 `replication_test.go` 패턴 활용 — 2-region setup + cutover → UI fetch 검증.
- Playwright e2e — `/regions` 페이지 fetch + region health card 확인.

### 6.7 Stage 10.A-7 — release notes + CHANGELOG

추정 0.5일.
- v0.9.0 minor — Phase 10 진입 첫 minor.
- release notes + CHANGELOG entry.

**Stage 10.A-2~10.A-7 = ~3~4주** (보수적). 1순위 마감 시 multi-region 운영자 가시성 + alert + 첫 v0.9.0 minor release.

---

## 7. 결정 항목 (D-P10-1·2)

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시.

### 7.1 D-P10-1 — 본 design doc 채택 + Top 3 우선순위

- (1) **채택 + Top 3 순서 합의** — A(multi-region UI) → D(audit key rotation) → E(ros2-humble + DDS/SROS2) (**권장 default**).
- (2) 채택 + 1순위 변경 — D 또는 E 또는 다른 옵션을 1순위로.
- (3) 채택 + 보류 옵션 진입 — B(SOC2) 또는 F(otel) 등 보류 권장 옵션을 1순위로.
- (4) 거부 — 본 doc 비채택, 별 backlog 접근.

**근거**: Phase 0~9 9 milestone 마감 + 8 후보 매트릭스 + 권장 default 명시되어 다음 round 즉시 진입 부담 0. 옵션 A는 외부 트랙 의존 0 + 회귀 위험 낮음 + 기 결선 자산 표면화로 ROI 가장 빠름.

### 7.2 D-P10-2 — 1 epic 진행 vs 병렬 2 epic

- (1) **1 epic 진행** — 옵션 A design doc 1개 → Stage 1·2·3·4·5·6 순차. context 안정 (**권장 default**).
- (2) 2 epic 병렬 — 옵션 A + 옵션 E 동시(worktree 2개). 두 epic 도메인 충돌 없음(web vs pack) + sub-agent 가능. 같은 round 압축.
- (3) 옵션 A만 우선, 마감 후 옵션 D 진입 (옵션 1 동등하나 명시).

**근거**: 옵션 A는 3~4주 단독 epic이라 1 epic 진행이 정합. 옵션 E(ros2-humble pack)는 web 도메인 충돌 0 + sub-agent dispatch 가능(pack yaml + selftest만)이라 병렬 옵션도 합리. 사용자 선택. memory `feedback_parallel_agents.md`는 매 stage 시작 시 병렬 가능성 재평가 의무 — 본 결정에서 default 1 epic이나 stage 진입 시 재평가.

---

## 8. Carryover 통합

### 8.1 Phase 5~9 잔여 carryover

| Carryover | 권장 default | trigger 조건 | 외부 트랙 |
|---|---|---|---|
| Manual fixture Stage 3 low 5건 | 보류 유지 | 첫 paying customer 진입 후 | — |
| E22-F BOOLEAN 회수 옵션 A | 보류 유지(영구) | Big bang driver-aware repo 별 epic | — |
| scanrun extras epic A·B·C·D | 보류 유지 | ★ customer 부하 측정 후 | — |
| ROS2 Round 3 carryover 6건 | 보류 유지 또는 옵션 E와 통합 | paying customer 진입 또는 옵션 E 마감 시 | — |
| C5b-10 a11y polish Tailwind palette | 보류 유지 또는 옵션 G와 통합 | UI 진화 옵션 G 진입 시 | — |
| MR.T4 application restart integration | 보류 유지 | Phase 9.5 testcontainers e2e Patroni 3-node + etcd 진입 시 | — |
| Stage 4.5 BIND/PowerDNS Terraform sample | 보류 유지 | DNS routing customer 명시 요구 시 | — |
| Stage 5b 잔여 carryover (C5b-6/7/8/9) | 보류 유지 | UI 진화 옵션 G 진입 시 | — |
| Phase 9.5 testcontainers e2e Patroni 3-node | 보류 유지 | Patroni customer 진입 또는 Phase 11 | — |

### 8.2 사용자 외부 트랙 (★ 표기, memory `feedback_user_tracks.md` 일관 — 본 doc 권장 default에서 제외)

- ★ **D1 변리사 의뢰** — R-D8 청구권 KR 우선출원. 옵션 C(enterprise 잔여 3 패키지) trigger.
- ★ **E36 레퍼런스 HW burn-in** — NUC + OptiPlex + TPM 봉인 + Secure Boot 측정. 사용자 hands-on.
- ★ **첫 paying customer 진입** — Phase 6 R1+R2+R3 결선 후 customer trigger 대기. 옵션 H + Manual fixture + scanrun extras carryover 모두 영향.
- ★ **SOC2 외부 감사인 컨설팅** — 옵션 B 실 인증 부분.

---

## 9. 비목표 / 거부

본 Phase 10에서 명시 거부:

### 9.1 자체 하드웨어 제조 진입

nrobotcheck 전신에서도 거부 — Lodestar는 software-only. 어플라이언스 OS(snap + TPM) 결선은 reference HW(★ E36) customer가 직접 burn-in.

### 9.2 에이전트 프레임워크화 또는 자율 공격

설계서 §12 비목표 명시 — CAI 영토 회피. advisor는 옵트인 + reasoning trace + 결정론 fallback 일관.

### 9.3 단일 customer 의존 epic

옵션 H(scanrun extras + scale)는 customer trigger ★. 단일 customer 부하 데이터로 일반화 위험 — 여러 customer 진입 후 패턴 분석 권장.

### 9.4 LLM 필수 경로 생성

설계서 §1.2 옵트인 원칙 일관 — Phase 10 어느 옵션도 LLM 필수 경로 도입 0. 옵션 B SOC2 control mapping에서 advisor reasoning trace 옵션 활용은 가능하나 필수 0.

### 9.5 tenant_id 없는 신규 테이블

설계서 §1.4 멀티테넌시 원칙 일관 — 옵션 D audit_chain_keys 신규 테이블에 tenant_id 컬럼 필수(또는 정책 명시 컬럼 부재 시 system-wide singleton 명시).

### 9.6 UPDATE/DELETE 가능한 audit 테이블

설계서 §1.9 불변성 원칙 — append-only 강제. 옵션 D key rotation은 새 key 추가만, 기존 audit_entries 변경 0.

### 9.7 Remote push 자동화

CLAUDE.md 일관 — local 커밋 OK, remote push 사용자 명시 요청 시에만. v0.9.0 minor release tag push도 사용자 결정.

---

## 10. 회귀 위험 / 운영 고려

- **본 문서 자체 영향**: 0. docs only, 코드 0 / 마이그레이션 0 / pack 변경 0 / API 0.
- **Phase 0~9 baseline 회귀 0**: 본 doc은 후보 매트릭스 + 권장. 진입 epic은 별 Stage 분해에서 회귀 영향 평가.
- **다음 세션 진입 부담 0**: D-P10-1·2 모두 권장 default 명시 — 사용자 round 1회로 합의 가능.
- **carryover 보류 일관**: 9 carryover + 4 사용자 외부 트랙 모두 default 보류/제외 — Phase 10 진입에 영향 0.

---

## 11. 참조

### 11.1 직전 design doc 패턴

- `docs/design/notes/auto-failover-research.md` — Phase 9 진입 doc 패턴.
- `docs/design/notes/phase6-backlog-design.md` — Phase 6 진입 doc 패턴(본 doc 1차 모방).
- `docs/design/notes/multi-region-ha-design.md` — Phase 8 epic 본체.
- `docs/design/notes/customer-onboarding-design.md` — Phase 6 1순위 doc.
- `docs/design/notes/llm-private-deployment-design.md` — LLM private deployment 결선 (가설 2 cover).

### 11.2 release / CHANGELOG

- `docs/releases/v0.8.0.md` — Phase 9 Patroni 자동 failover.
- `docs/releases/v0.8.5.md` — v0.8.4 broken release hot fix.
- `CHANGELOG.md` — [0.8.0]~[0.8.5] entry.

### 11.3 설계서

- `docs/design/01-principles.md` — 12 원칙.
- `docs/design/11-tech-stack-and-roadmap.md` — 로드맵 + 결정 로그.
- `docs/design/12-migration-and-non-goals.md` — 비목표.
- `docs/design/13-patent-strategy.md` — R-D8 청구권.

### 11.4 코드/디렉터리 fact-check 참조

- `internal/enterprise/{crosswitness,multihash,wasmrt,robotid,fleetxval,rostopo,selectdisclose}/` — 4 실 구현 + 3 placeholder.
- `internal/platform/llm/{noop,anthropic,ollama,vllm}/` — 4 provider 결선.
- `internal/api/handlers/replication.go` — multi-region 4 endpoint.
- `internal/platform/metrics/` — Prometheus + Grafana.
- `packs/{cis-ubuntu-2404,ros2-jazzy,ros2-jazzy-baseline}/` — 3 pack.
- `web/src/i18n/dict.ts` — ko/en 2 language.
- `docs/onboarding/*` · `docs/operations/*` — 22 markdown 결선.

### 11.5 memory feedback

- `feedback_design_doc_first.md` — 1일+ 임계 design doc 우선.
- `feedback_design_doc_conservative.md` — 잠재 효과/시간 보수적.
- `feedback_parallel_agents.md` — 매 stage 시작 시 병렬 가능성 재평가.
- `feedback_user_tracks.md` — D1·E36·SOC2 감사·customer trigger 등 외부 트랙 제외(★ 표기).
- `feedback_no_rest_recommendation.md` — 휴식 옵션 자동 포함 X.
- `feedback_recommend_next_actions.md` — 다음 추천 작업 3~5건 명시.
- `feedback_skip_handoff.md` — handoff edit/commit/push 생략, CHANGELOG + release notes + commit 메시지로 trace.
