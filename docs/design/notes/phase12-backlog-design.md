# Phase 12 Backlog — Phase 11 마감 직후 차기 milestone 8 후보 매트릭스 + Top 3 권장 — Design

> **상태**: Phase 11 Top 3 (B·C·A) 모두 완전 마감(v0.12.0 SOC2 readiness + v0.13.0 audit hash key_epoch + v0.14.0 OpenTelemetry tracing 전면) 직후 Phase 12 진입 합의용 design doc. 본 문서는 코드 0줄 / 마이그레이션 0건 / pack 변경 0 — Phase 0~11 회고 + Phase 12 후보 8종 매트릭스 + Top 3 권장 + 결정 항목 권장 default까지만 마감합니다.
> **참조**:
> - 직전 design doc 패턴: `notes/phase11-backlog-design.md`(Phase 11 진입 doc, 본 doc 1차 모방) · `notes/phase10-backlog-design.md`(Phase 10 진입 doc) · `notes/soc2-readiness-design.md`(옵션 B 본체) · `notes/audit-hash-key-epoch-input-design.md`(옵션 C 본체) · `notes/opentelemetry-tracing-design.md`(옵션 A 본체).
> - 마감 release: `docs/releases/v0.14.0.md`(2026-05-22, Phase 11 옵션 A OpenTelemetry tracing 전면) — head `d539b45` 기준.
> - CHANGELOG: `CHANGELOG.md` [0.12.0]·[0.13.0]·[0.14.0] entry + [Unreleased] carryover 22+ 항목.
> - 설계서: `docs/design/11-tech-stack-and-roadmap.md` 로드맵 + `docs/design/01-principles.md` 12 원칙 + `docs/design/13-patent-strategy.md` D8 청구권.
> **R 식별자**: R-PHASE12-1(본 doc 전체) — 결정 항목은 D-P12-1·2.
> **본 문서 작성 위치**: main(head `d539b45`), 단독 sub-agent.

---

## 1. 상태 / 배경

### 1.1 Phase 0~11 마감 요약 (한 줄씩)

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
| **v0.8.1~v0.8.5 patch** | 2026-05-21 | CI baseline 안정화 + E35-refresh redesign + snap hot fix. |
| **Phase 10 옵션 A** | 2026-05-21 | v0.9.0 minor — `/regions` 페이지 + 3 카드(RegionHealth + AuditConsistency + RegionTimeline) + Prometheus alert 5 rule + ops runbook §13 + testcontainers 2-region + Playwright e2e. |
| **Phase 10 옵션 D** | 2026-05-21 | v0.10.0~v0.10.2 — SwappableSigner Queue 패턴 + KeyRotator 90일 quarterly cron + emergency override CLI/admin + fg-verify v2(epoch별 검증) + 마이그레이션 0037+0038. |
| **Phase 10 옵션 E** | 2026-05-21 | v0.11.0 — `packs/ros2-humble/` 신규 29 check + `packs/ros2-jazzy/` 22→29 check 깊이 확장(DDS topic ACL 4 + SROS2 cert chain 3 양쪽 동기). |
| **Phase 11 옵션 B** | 2026-05-22 | v0.12.0 minor — `docs/compliance/` 14 매트릭스(47 sub-control) + `auditor` role + audit export wizard + effectiveness dashboard + `soc2-controls` pack 61 check. |
| **Phase 11 옵션 C** | 2026-05-22 | v0.13.0 minor — `canonicalMetaJSONv3`(9 키) + `ComputeEntryHashV3` + transition marker entry + `_bundleVersion: "v3"` + fg-verify v3 3-tier backward compat. |
| **Phase 11 옵션 A** | 2026-05-22 | v0.14.0 minor — `internal/platform/otel/` SDK + parent_based 0.05 sampling + 5 CLI flag + 4 hot path span(HTTP middleware + scan 5 span + multi-region/Patroni + LLM 4 provider) + PII 회피 엄격. |

### 1.2 Phase 12 진입 가치

Phase 11 마감 시점에 baseline은 다음과 같이 확정되었습니다:
- **compliance**: SOC2 TSC 14 매트릭스(47 sub-control) + auditor role + audit export wizard + effectiveness dashboard + `soc2-controls` pack 61 check 결선. AICPA TSC ~95% 자연 cover. 외부 SOC2 firm 진입 baseline 정착(★ 외부 트랙).
- **audit chain 완전 결합**: signer key rotation(v0.10.0) + epoch input v3 hash(v0.13.0) + fg-verify v1/v2/v3 자동 분기 결선. 외부 감사인 호환성 + 위변조 차단 모두 cover. v0.10.0 carryover "key_epoch+leader_epoch input 미포함" 완전 마감.
- **observability**: Prometheus metrics + Grafana + structured slog + audit chain 위에 분산 trace 채널(v0.14.0)까지 확장. OTLP gRPC/HTTP exporter + W3C propagator + 4 instrumented hot path 결선. opt-in default(`--otel-enabled=false`) — air-gap customer 영향 0.
- **HA·DR·UI**: single-region E25 + cross-region replication + Patroni 자동 failover + `/regions` 운영자 가시성 표면화 결선. RTO ≤ 60초 + 운영자 카드 + alert + runbook 모두 cover.
- **ROS2 baseline 양분기**: Jazzy(LTS 2024-05~2029-05) + Humble(LTS 2022-05~2027-05) 양쪽 29 check 동기.
- **enterprise scaffold**: 7 패키지 중 4 실 구현(crosswitness · multihash · wasmrt · robotid, R-D8 v3 4 청구권 cover) + **3 placeholder 잔여**(fleetxval · rostopo · selectdisclose).
- **LLM private**: 4 provider(noop · anthropic · ollama · vllm) + private deployment docs + v0.14.0 `llm.complete` span + PII 회피 엄격.
- **release infra**: v0.3.0~v0.14.0 = 40+ release(cosign keyless 서명 + Sigstore Rekor 등록) + amd64+arm64 snap.

차기 milestone은 (a) **observability carryover 마감**(testcontainers Jaeger smoke + Grafana panel + UI trace_id 표시 + otel metric exporter) (b) **enterprise 잔여 3 패키지 본체**(R-D8 출원 ★ 의존) (c) **AI advisor 진화**(reasoning trace UI + multi-turn persist + token budget UI) (d) **customer experience**(feature gate + health dashboard, ★ customer trigger) 네 축이 후보 풀입니다. memory `feedback_design_doc_first.md` 일관 — Phase 12 진입 시 1일+ 임계 작업은 design doc 우선.

---

## 2. 현재 상태 fact-check

본 §은 코드/디렉터리 직접 grep 결과 — 추측 0, fact만 명시 (head `d539b45`).

### 2.1 enterprise placeholder 3 패키지 현황 (가설 1)

`internal/enterprise/` 디렉터리:

| 패키지 | 파일 수 | 상태 | 산출 |
|---|---|---|---|
| `crosswitness/` | 8 files | **실 구현** | `anchor.go`(WebhookAnchor + FilesystemDumpAnchor) + `fold.go` + `scheduler.go` + 단위 테스트. A-1 cross-witness anchoring R-D8 청구권 cover. |
| `multihash/` | 8 files | **실 구현** | `compute.go` + `verify.go` + `jsonpath.go` + 단위 테스트. B-1 multi-hash evidence R-D8 청구권 cover. |
| `wasmrt/` | 13 files | **실 구현** | `runtime.go` + `cosign.go` + `policy.go` + `sigstore.go` + `limits.go` + 단위 테스트. C-1 WASM sandboxed evaluator R-D8 청구권 cover. |
| `robotid/` | 17 files | **실 구현** | `collector.go`(linux + other) + `fingerprint.go` + `quote_attestation.go` + `tpm_linux.go` + simulator test. D-3 robot identity binding R-D8 청구권 cover. |
| `fleetxval/` | **2 files** | **placeholder 잔여** | `doc.go` + `enterprise.go`(`EditionTag = "enterprise"`만). |
| `rostopo/` | **2 files** | **placeholder 잔여** | `doc.go` + `enterprise.go`(`EditionTag = "enterprise"`만). |
| `selectdisclose/` | **2 files** | **placeholder 잔여** | `doc.go` + `enterprise.go`(`EditionTag = "enterprise"`만). |

7 패키지 중 4 실 구현 + **3 placeholder 잔여**(Phase 11 진입 시 baseline 그대로). 우선순위 후보(R-D8 청구항):
- **selectdisclose**(D-2 R-D8) — selective disclosure / ZK redaction. 외부 감사인에 evidence 일부만 공개 + 나머지 cryptographic commitment 유지.
- **rostopo**(E-1 R-D8) — ROS2 그래프 cross-validation. `ros2 node list`·`ros2 topic list` topology cross-check.
- **fleetxval**(F-1 R-D8) — fleet 간 cross-validation. 다수결/cross-witness 알고리즘.

### 2.2 customer onboarding R1+R2+R3 결선 사실 (가설 2)

`docs/onboarding/` 트리:
- `customer-info-template.md` · `walkthrough.md` · `quickstart.md` · `demo-script.md` · `cis-customer-policy.md` · `multi-region-ha-setup.md` · `llm-private-deployment.md` · `audit-rotation-cosign.md` · `audit-rotation-s3.md` · `audit-rotation-verify.md` · `otel-collector-setup.md` · `sla-template.md` · `support-channels.md` · `README.md`.

Phase 6 R1+R2+R3 결선 + `/api/v1/usage/stats` endpoint(`internal/api/handlers/usage_stats.go`) — tenant별 scansStarted/scansCompleted/scanFailedChecks 분포 노출 결선. 미진행 영역:
- **customer health dashboard UI** — 운영자가 customer별 health 한눈에 확인 UI 부재(query API는 있으나 web UI 페이지 없음).
- **billing integration** — Stripe/Toss 등 외부 결제 통합 0.
- **multi-tenant edition feature gate** — 현 build tag `enterprise`만, tenant별 plan(community/pro/enterprise) gate 부재. `EditionPlan|edition_gate|feature_flag` grep 결과 0.
- **customer churn signal** — login 빈도/scan 빈도 등 paying customer health signal 부재.

### 2.3 LLM 4 provider + advisor 결선 사실 (가설 3)

`internal/platform/llm/`:
- `llm.go` — `Adapter` interface + `LlmTrace` + `ErrLLMDisabled` 등.
- `noop/` — 기본값(R14-1).
- `anthropic/` — cloud HTTPS.
- `ollama/` — 로컬 daemon(self-hosted, CPU/GPU).
- `vllm/` — OpenAI-compatible(GPU + continuous batching, self-hosted production).

`internal/app/advisorrun/`:
- `llm_client.go` · `llm_client_otel_test.go` · `orchestrator.go` · `orchestrator_test.go` · `tools.go` · `tools_test.go`.
- v0.14.0(Stage 11.A-6) `llm.complete` span 결선 — provider/model/token/cost/duration attribute + PII 회피.

`internal/domain/advisor/`:
- `advisor.go` + `sqliterepo/`. 마이그레이션 0018 advisor 단발 요청-응답 cover.

미진행 영역(grep 결과):
- **reasoning trace UI** — `Advisor` 결정에 step-by-step trace UI 표시 미존재(`ReasoningTrace|advisor.reasoning` grep 결과 0 in `internal/domain/advisor/`).
- **multi-turn conversation persist** — `ConversationHistory|MultiTurn` 키워드 grep 결과 0(현 단발 요청-응답만).
- **advisor RBAC 정밀** — 현 `advisor:read` 단일 권한, `advisor:read/write/admin` 세분 부재.
- **token budget UI** — `TenantQuota|TokenBudget|daily_token` grep 결과 0. usage_stats.go에 scansStarted/scansCompleted 일부만 노출, LLM token quota UI 부재.
- **redaction 자동화 audit emit** — advisor orchestrator tool 호출 trace의 audit emit 자동화 부재.

### 2.4 scanrun extras carryover (가설 4)

- `internal/app/scanrun/scanrun.go` · `scanrun_test.go` · `scanrun_span.go`(v0.14.0 5 span) · `test/integration/sshd_e2e_test.go` 결선.
- `internal/platform/sshpool/` — Pool idle 재사용 + keepalive + 5 metrics(Phase 5 Stage 5b 마감).
- per-robot HealthFailureThreshold=3 결선.
- `CircuitBreaker|circuit_breaker|RateLimit|rate_limit|PerTenantLimit` grep 결과 — sshpool/llm 일부 일반 패턴만, 별 epic 결선 0.
- `ParallelScan|parallel_scan|ScanCancel|CancelScan` grep 결과 — 일반 scan flow 패턴만, 별 epic 결선 0.
- **scanrun extras epic A·B·C·D 보류**(Phase 6 + 10 + 11 carryover) — Pool size 동적 · per-tenant rate limit · per-robot circuit breaker · observability metric 확장. customer trigger 대기.
- 실 fleet scale 50~100 robot 검증 부재.

### 2.5 web i18n + theme 현재 상태 (가설 5)

`web/src/i18n/`:
- `dict.ts`(`export type Locale = 'ko' | 'en'`) + `t.ts` + `store.ts` + `dict.test.ts` + `t.test.ts`.
- ko 기본, en fallback. dict 키 동기 강제(누락 시 CI fail).
- 다국어 추가 없음 — ja/zh/es/de/fr 등 부재.
- `nextLocale` 토글은 ko↔en 2-way switch.

`web/src/components/`:
- `OfflineIndicator.tsx`(dark 키워드 일부 사용) + `UpdatePrompt.tsx` + `regions/` 3 card + `common/` + `layout/` + `ui/`.
- dark mode 토글 컴포넌트 자체는 부재 — Tailwind palette만 결선, C5b-10 a11y polish Tailwind palette contrast carryover(CHANGELOG [Unreleased]).

`web/src/routes/_authenticated/`:
- 21 페이지(advisor · audit · compliance.effectiveness · compliance.export · compliance · findings · fleets · integrations · license · overview · packs · regions · reports · robots · scans · settings · sso · system · users). 페이지 커스터마이징 + report builder UI(PDF 외 JSON/CSV/XLSX export wizard) 부재 — 현재 PDF 단일 형식.

### 2.6 v0.14.0 OpenTelemetry 결선 사실 + carryover (가설 6)

`internal/platform/otel/`:
- `provider.go` + `provider_test.go` + `exporter.go` + `resource.go` + `sampler.go` + `trace_context.go` + `llm.go` + `llm_test.go`.

v0.14.0 결선:
- Provider + Config(Enabled/ServiceName/Endpoint/ExporterType grpc+http/Insecure/SamplingRatio 0.05/Headers/Region/Environment).
- W3C TraceContext + Baggage composite propagator.
- 5 CLI flag + 5 env (`--otel-enabled` · `--otel-endpoint` · `--otel-exporter` · `--otel-sampling` · `--otel-insecure`).
- 4 instrumented hot path — HTTP middleware + scan 5 span(`ssh.connect`/`check.exec`/`check.evaluate`/`evidence.write`/`scan.publish`) + multi-region/Patroni REST + LLM 4 provider.
- `internal/platform/httpclient/otel.go::WrapClient/WrapTransport` outbound transport wrap.
- PII 회피 엄격 — prompt/response/tool args/ssh credential/evidence content 모두 attribute 노출 0.

미진행 carryover (CHANGELOG [Unreleased] 기준):
- **testcontainers e2e Jaeger smoke** — CI docker pull 비용 회피로 별 epic carryover.
- **otel metric exporter plug** — D-P11A-5 양쪽 emit 중 otel metric 부분 미진행. Prometheus federation만 결선, OTLP metric exporter 미plug.
- **Lodestar Grafana dashboard panel** — trace volume + sampling effective rate + cross-region trace coverage panel 신규 dashboard epic.
- **Lodestar UI 안 trace_id 표시** + Jaeger/Tempo deep link — `trace_id|traceId|traceID` grep in `web/src` 결과 0. UI 안 표시 미존재.
- **audit chain head sha mismatch metric** (Phase 10.A-6 carryover) — `audit_chain_head_sha|head_sha_mismatch` grep 결과 0. carryover 보류.
- **manual rotation endpoint** (v0.10.0 carryover) — endpoint 부재.
- **multi-tenant epoch 분리** (v0.10.0 carryover).
- **Grafana dashboard panel** — `deploy/grafana/rosshield-dashboard.json` 1 파일만 결선, rotation_total + key_epoch + hash_version 신규 panel 미추가.

### 2.7 tenant 인프라 결선 사실 (가설 7)

`internal/domain/tenant/`:
- `apikey.go` · `invitation.go` · `jwt.go` · `password.go` · `tenant.go` · `rbac_test.go` + `sqliterepo/` + `sso/`.

`internal/platform/storage/`:
- 멀티테넌시 + PG + sqlite repo dual-target 결선(Phase 1 baseline). 모든 audit/scan/evidence 테이블 `tenant_id` 컬럼 강제.

multi-tenant edition feature gate(community/pro/enterprise) 부재 — 현 build tag `enterprise`만, tenant별 plan gate 미존재(가설 2 fact-check 일관). multi-tenant epoch 분리(v0.10.0 carryover) — audit chain epoch는 현재 global per-tenant single key. tenant 격리 e2e 검증 부재.

### 2.8 compliance docs + pack 결선 사실 (가설 8)

`docs/compliance/`:
- `README.md` + `soc2/` 14 매트릭스(cc1~cc9 + a1~a5). v0.12.0 결선(47 sub-control 매핑).

`packs/soc2-controls/`:
- `pack.yaml` + `checks/` **61 yaml** + `selftest/`. v0.12.0 결선(CC1~CC9 + A1~A5 8 카테고리 cover).

`internal/domain/compliance/`:
- `compliance.go` + `frameworks.go` + `mapping.go` + `soc2_mapping.go` + `sqliterepo/`. v0.12.0 결선(14 카테고리 × 40 sub-control × audit action 매핑).

`internal/api/handlers/`:
- `compliance.go` + `compliance_effectiveness.go` + `compliance_export.go` + 단위/integration test. v0.12.0 결선.

미진행 carryover (CHANGELOG [Unreleased] 기준):
- **A4 Privacy GDPR/CCPA/한국 PIPA 상세 매핑** (v0.12.0 carryover) — A4 docs 안 SOC2 baseline 매핑만, 별 framework 상세 mapping 부재.
- **DC physical (CC6.4) + environmental (A1.3) IaaS cloud SOC2 inheritance** — IaaS provider(AWS/GCP) attestation cross-reference 부재.
- **awareness training** (★ HR internal) — 외부 트랙.
- **vendor inventory + pen test schedule** (★ 외부).
- **formal IR program + risk register** — internal governance, 별 epic.
- **soc2-controls pack archive embed** (PACKS_SOURCE Makefile 등록) — `_archives/` 자동 embed 미진행.

---

## 3. 위협 모델 / 요구사항 (Phase 12 진입 시)

### 3.1 신규/잔여 위협

| 위협 | 가능성 | 영향 | Phase 12 cover 영역 |
|---|---|---|---|
| v0.14.0 OpenTelemetry carryover 잔여(panel + UI trace_id + metric exporter) 미마감 → 운영자/customer 체감 가치 분산 | 중(observability 사용자 진입 시) | 운영 디버깅 ROI 저하 | 옵션 A observability carryover 마감. |
| enterprise 잔여 3 패키지 placeholder → 영업 demo 시 "어차피 4개만 실제 작동" 회의 + R-D8 청구항 disclosure 위험 | 중(★ D1 출원 의존) | 영업 회의 + IP 보호 | 옵션 B enterprise 잔여 3 패키지 본체. |
| LLM advisor 가치 검증 부재(reasoning trace 부재 + multi-turn 부재 + token budget UI 부재) → paying customer "ROI 모름" | 중(advisor 옵트인 시점) | upsell 실패 | 옵션 C advisor 진화. |
| customer 진입 시 feature gate 부재 → community/pro/enterprise plan 차별화 부재 + billing 통합 0 | 중(영업 진입 시 가격 책정 표면) | 가격 책정 불명 + 영업 자동화 부재 | 옵션 D customer experience. |
| paying customer 부하 trigger 대비 scanrun extras 4 epic 보류 → scale 50~100 robot 진입 시 운영 부담 | 중(★ customer trigger 후) | 운영 부담 + 부정적 customer 경험 | 옵션 E scan 엔진 진화. |
| web UI ko/en 2-language 한정 + dark mode 부재 + report builder 부재 → 한국/영어 외 customer 진입 어려움 | 중~낮(국제 진입 시) | 시장 확장 제약 | 옵션 F web UI 진화. |
| A4 Privacy GDPR/CCPA/PIPA 상세 매핑 부재 → SOC2 readiness 너머 personal data 규제 customer 회의 | 중(EU/CA customer 진입 시) | 규제 customer 진입 차단 | 옵션 G compliance 진화. |
| multi-tenant epoch 분리 부재 + tenant 격리 e2e 검증 부재 → 멀티테넌시 customer 진입 시 보안 회의 | 중~낮(다수 tenant 진입 시) | 보안 audit 우려 | 옵션 H multi-tenant 강화. |

### 3.2 요구사항

| ID | 요구 | 측정 |
|---|---|---|
| R12-1 | Phase 12 1순위 epic은 외부 트랙 의존 0 또는 docs 우선 진입 가능 | D1 변리사/E36 hands-on/customer trigger/SOC2 감사 외부 의존 명시 |
| R12-2 | 1순위 epic 추정 ≤ 1.5개월 (보수적, Phase 11 평균 일관) | Stage 분해 합계 |
| R12-3 | 회귀 위험 ≤ 중급 | audit chain · 멀티테넌시 격리 · OpenTelemetry hot path 변경 표면 작음 |
| R12-4 | Phase 0~11 baseline 회귀 0 | Go test 50+ 패키지 + RBAC 통합 + Playwright e2e PASS + soc2-controls 61 check + v3 hash chain 유지 |
| R12-5 | design doc 우선 | 1일+ 임계 작업은 doc 먼저 (memory `feedback_design_doc_first.md`) |
| R12-6 | 보수적 추정 일관 | memory `feedback_design_doc_conservative.md` |

---

## 4. Phase 12 후보 8 옵션 비교

각 옵션마다 (a) 설계 요약 (b) 가치 (c) 노력 추정(보수적) (d) 전제·의존 (e) 리스크.

### 4.1 옵션 A — observability carryover 마감 (가설 6, v0.14.0 carryover)

**설계 요약**: v0.14.0 결선 위에 carryover 5종 마감 — (1) testcontainers e2e Jaeger smoke(CI docker pull + Jaeger all-in-one 1 container + scan flow 1 span emit 검증) (2) Lodestar Grafana dashboard panel 신규(trace volume + sampling effective rate + cross-region trace coverage + audit hash version + rotation_total + key_epoch 6 panel set) (3) Lodestar UI 안 trace_id 표시(scan detail + audit entry detail + advisor 결정 detail 3 위치 + Jaeger/Tempo deep link copy 버튼) (4) OTLP metric exporter plug(Prometheus federation 그대로 유지 + OTLP metric 양쪽 emit, D-P11A-5 carryover) (5) audit chain head sha mismatch metric(Phase 10.A-6 carryover).

**가치**:
- paying customer ★★★ (observability 사용자 진입 시 가치 가시화) / enterprise ★★★ / compliance ★★ / operational ★★★★★ (운영자 가치 직접) / 기술 부채 ★★★★★ (v0.14.0 carryover 5종 완전 마감)

**노력 추정 (보수적)**: **3~4주**. testcontainers Jaeger smoke 1주(docker-compose + 1 scan + Jaeger query API verify) + Grafana 6 panel 0.5~1주(JSON dashboard 갱신 + Prometheus query 검증) + UI trace_id 표시 1주(3 페이지 + deep link 컴포넌트 + i18n) + OTLP metric exporter 0.5주(otelmetric SDK + Prometheus federation 양쪽 emit) + head sha mismatch metric 0.5주(audit Repo 신규 metric).

**전제·의존**: 없음. v0.14.0 결선 활용. OpenTelemetry SDK + Prometheus 양쪽 결선 baseline 활용.

**리스크**: **낮음~중**. testcontainers는 CI docker 비용 추가 — Jaeger image cache로 회피. UI trace_id는 PWA 회귀 위험 표면 작음. Grafana panel은 dashboard JSON only — 코드 회귀 0.

### 4.2 옵션 B — enterprise 잔여 3 패키지 1차 구현 (가설 1, Phase 10·11 carryover)

**설계 요약**: 7 패키지 중 placeholder 잔여 3개(fleetxval · rostopo · selectdisclose) 1차 본체 구현. 후보 우선순위(R-D8 청구항):
- **selectdisclose**(D-2 R-D8 — selective disclosure / ZK redaction): customer가 외부 감사인에 evidence 일부만 공개 + 나머지 cryptographic commitment 유지. SHA256/Merkle commitment 기반 jsonpath redact + verify round-trip.
- **rostopo**(E-1 R-D8 — ROS2 그래프 cross-validation): scan 결과 graph topology(`ros2 node list`/`ros2 topic list`) 검증으로 ROS2 application layer 정밀.
- **fleetxval**(F-1 R-D8 — fleet 간 cross-validation): 여러 robot의 동시 scan 결과 일관성 검증(다수결 또는 cross-witness 알고리즘).

**가치**:
- paying customer ★★ / enterprise ★★★★ (영업 demo 회의 차단) / compliance ★★★ / operational ★ / 기술 부채 ★★★★ (placeholder 완전 마감) / IP 보호 ★★★★

**노력 추정 (보수적)**: **5~7주** (한 패키지당 ~2주). selectdisclose 알고리즘 + jsonpath redact + cryptographic commitment + 단위 테스트 + 통합 2주. rostopo는 ROS2 그래프 parsing + cross-validation 알고리즘 2주. fleetxval은 fleet 간 다수결 알고리즘 + cross-witness 호환성 2주. enterprise build tag boundary test 갱신 0.5~1주.

**전제·의존**: ★ **D1 변리사 출원 진행 상태에 따라 disclosure 시점 영향**. 출원 *전* 외부 PoC/blog 차단은 D8 결정 일관. 코드 자체는 enterprise build tag 안이므로 코어 코드베이스 disclosure 0(repo public 결선 후에도 build tag default off).

**리스크**: **중**. enterprise build tag 안에서만 컴파일되므로 코어 회귀 0. R-D8 청구권 disclosure 위험은 출원 후 진입 권장(★ D1 의존).

### 4.3 옵션 C — AI advisor 진화 (가설 3, Phase 11 carryover)

**설계 요약**: LLM provider 4종 + private deployment docs + v0.14.0 `llm.complete` span 결선 위에 advisor 진화 — reasoning trace UI 강화(advisor 결정에 step-by-step trace UI 표시 + audit emit `advisor.reasoning.recorded`) + multi-turn conversation persist 강화(`advisor_conversations` 테이블 신규 + 세션 컨텍스트 유지 + cleanup 정책) + advisor RBAC 정밀(`advisor:read` / `advisor:write` / `advisor:admin` 세분) + token budget cost guardrail UI(tenant daily token 통계 운영자 대시보드 + LLM 비용 trend).

**가치**:
- paying customer ★★★★ (advisor 가치 가시화 → upsell) / enterprise ★★★ / compliance ★★ (reasoning trace audit) / operational ★★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **5~7주**. reasoning trace UI(step-by-step 컴포넌트 + dict 키 + e2e) 1.5주 + multi-turn persist(0039 마이그레이션 + Repository + Cleanup 정책) 1.5주 + advisor RBAC 세분(3 권한 + middleware + boundary test) 1주 + token budget UI(통계 query + Grafana panel 어댑터 + i18n) 1주 + redaction 자동화 audit emit 1주.

**전제·의존**: 없음. 기존 LLM 4 provider + private docs 활용. customer가 LLM 옵트인 시점에 가치(default off 일관). v0.14.0 `llm.complete` span 자연 활용.

**리스크**: **중**. multi-turn persist는 PII 누설 위험 — `evidence.Redact` 자동 적용 필수. token budget UI는 read-heavy aggregation query(tenant 별 daily 통계) 성능 검증 필요. tenant_id 격리 멀티테넌시 원칙 일관.

### 4.4 옵션 D — customer experience + paying customer 진입 (가설 2, Phase 6 carryover)

**설계 요약**: R1+R2+R3 결선 위에 customer experience 진화 — customer health dashboard 페이지(`/customers` 운영자 한눈에 customer 별 login/scan/audit 빈도 + churn signal) + multi-tenant edition feature gate(plan 테이블 + middleware + feature flag UI 표시, community/pro/enterprise 3-tier) + 사용량 통계 API 확장(`/api/v1/usage/stats` 외 `/api/v1/usage/timeseries`·`/api/v1/usage/by-feature` 신규) + customer onboarding email 자동화(intake API 트리거).

**가치**:
- paying customer ★★★★ (영업 진입 직접) / enterprise ★★★ / compliance ★★ / operational ★★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **5~7주**. customer health dashboard(query aggregation + Grafana panel + i18n) 1.5주 + multi-tenant edition feature gate(plan 테이블 + middleware + feature flag UI) 2주 + 사용량 통계 API 확장 1.5주 + onboarding email 자동화(intake → email template trigger) 1주 + e2e 0.5~1주.

**전제·의존**: ★ **첫 paying customer trigger 후 가치 ROI 명확**. customer 진입 *전* 가설 단계로 priority 모호. memory `feedback_user_tracks.md` 일관 — paying customer 진입은 사용자 외부 트랙. billing integration은 Stripe/Toss 외부 결제 의존 ★.

**리스크**: **중**. feature gate 도입은 boundary 추가(community customer 격리 + enterprise feature 차단) → boundary test 갱신 부담. billing integration은 외부 결제 서비스 의존 → 외부 트랙(★).

### 4.5 옵션 E — scan 엔진 진화 (가설 4, Phase 5/6 carryover)

**설계 요약**: scanrun extras epic A·B·C·D 진입 — Pool size 동적(epic A) + per-tenant rate limit(epic B) + per-robot circuit breaker(epic C) + scan profile templating(profile 재사용 가능 templating) + scan history aggregation + cancellation graceful + observability metric 확장(epic D, v0.14.0 span 자연 활용). 50~100 robot scale e2e benchmark.

**가치**:
- paying customer ★★ (customer 부하 trigger 후 가치 증분) / enterprise ★★★ / compliance ★ / operational ★★★★ / 기술 부채 ★★★

**노력 추정 (보수적)**: **5~7주**. epic A 1주 + B 1주 + C 1.5주 + D 0.5주 + scan profile templating 1주 + scale benchmark e2e fixture(docker-compose 50~100 sshd) 1~2주.

**전제·의존**: ★ **customer 환경 부하 데이터**(가설 단계). customer 진입 *전*에는 부하 측정 데이터 부재. memory `feedback_user_tracks.md` 일관.

**리스크**: **중**. concurrent scan + circuit breaker 변경은 scanrun core path 영향 — 회귀 위험 큼. e2e benchmark는 CI 비용 추가(50~100 sshd container).

### 4.6 옵션 F — web UI 진화 + 다국어 확장 (가설 5, Phase 10·11 carryover)

**설계 요약**: 운영자별 dashboard 커스터마이징(panel 선택 + 배치 + saved layout) + 다국어 폭 확장(ko/en → ja/zh/es 3개 추가) + dark mode tuning(C5b-10 a11y palette contrast carryover 마감) + report builder UI(현재 PDF 단일 → JSON/CSV/XLSX export wizard).

**가치**:
- paying customer ★★★ (UX 가치 + 다국어 시장 확장) / enterprise ★★ / compliance ★ / operational ★★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **4~6주**. dashboard 커스터마이징(panel registry + drag-drop layout + saved layout IndexedDB persist) 2주 + 다국어 3개 추가(번역 + dict 동기 + RTL 미진행) 1.5~2주 + dark mode tuning(C5b-10 contrast) 0.5주 + report builder UI(JSON/CSV/XLSX export + 형식 선택 wizard + selftest) 1주 + e2e 0.5주.

**전제·의존**: 없음. PWA + RBAC + i18n 결선 baseline 활용. 번역 품질은 외부 검토 별 트랙(★ — 번역사 또는 native speaker review).

**리스크**: **낮음~중**. dashboard layout persist는 IndexedDB(PWA persist 결선 활용) — 회귀 표면 작음. 다국어는 dict 동기 부담 증가(번역 품질 외부 검토 별 트랙).

### 4.7 옵션 G — compliance 진화 (가설 8, v0.12.0 carryover)

**설계 요약**: SOC2 baseline 위에 추가 framework 진화 — A4 Privacy GDPR/CCPA/한국 PIPA 상세 매핑(별 framework 매트릭스 docs + 해당 통제 audit action 매핑) + IaaS cloud SOC2 inheritance(AWS/GCP attestation cross-reference docs CC6.4 + A1.3) + soc2-controls pack archive embed(PACKS_SOURCE Makefile 등록) + ISO 27001 / NIST 800-53 매핑 docs(SOC2 → ISO/NIST cross-walk, 별 framework but 외부 트랙 ★ 명시).

**가치**:
- paying customer ★★★ (EU/CA/KR customer 진입 가능) / enterprise ★★★ / compliance ★★★★★ / operational ★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **4~6주**. GDPR/CCPA/PIPA 상세 매핑 docs 2주(framework 한 set 약 30 sub-control × 3 framework) + IaaS cloud inheritance 0.5주 + soc2-controls pack archive embed 0.5주 + ISO/NIST cross-walk 1~1.5주(SOC2 cover 자산을 ISO/NIST 통제로 단순 alias 표) + e2e 0.5주.

**전제·의존**: ISO 27001 / NIST 800-53 정식 인증은 ★ 외부 감사 의존(본 옵션은 매핑 docs까지만, 정식 인증은 별 외부 트랙). GDPR/CCPA/PIPA 상세 매핑은 docs only — 외부 의존 0.

**리스크**: **낮음**. docs 위주 + pack archive embed 일부만 코드 — 회귀 표면 작음. ISO/NIST 단순 alias 정확성 검증 부담은 docs 검토 round로 흡수.

### 4.8 옵션 H — multi-tenant 강화 (가설 7, v0.10.0 carryover)

**설계 요약**: 단일 system tenant 운영 위주 baseline 위에 multi-tenant 강화 — multi-tenant epoch 분리(v0.10.0 carryover, audit chain epoch tenant 별 분리) + tenant별 quota(scan 횟수 + LLM token + storage byte 등) + tenant isolation 검증 e2e(cross-tenant 침투 시도 시 모든 endpoint reject 보장 + scan/evidence/audit/insight 4 도메인 격리 검증).

**가치**:
- paying customer ★★★ (다수 tenant 진입 시 가치) / enterprise ★★★ / compliance ★★★★ (tenant isolation audit) / operational ★★ / 기술 부채 ★★★ (v0.10.0 carryover 마감)

**노력 추정 (보수적)**: **4~6주**. multi-tenant epoch 분리(`audit_chain_keys` per-tenant 검증 + KeyRotator 분기) 1.5주 + tenant별 quota(테이블 + middleware + UI 운영자 quota 설정) 2주 + tenant isolation e2e(통합 test cross-tenant 침투 시도 6 도메인 × 4 endpoint = 24 case) 1~1.5주.

**전제·의존**: 없음. Phase 1 멀티테넌시 baseline 활용. v0.10.0 audit chain key rotation 결선 활용. customer 진입 *전*에도 가설 단계로 가치 있으나 customer 진입 후 ROI 명확.

**리스크**: **중**. audit chain epoch 분리는 v0.13.0 hash chain v3 영향 가능(per-tenant transitionSeq 관리) → fg-verify v3 호환성 검증 필수. quota middleware는 모든 hot path 영향 — 성능 검증 필요.

### 4.9 매트릭스 종합

| 옵션 | 가설 | 가치 종합 | 시간 | 위험 | 즉시 진입 | 외부 트랙 의존 |
|---|---|---|---|---|---|---|
| **A** observability carryover 마감 | 6 | ★★★★★ | 3~4주 | 낮음~중 | ✅ | 0 |
| **B** enterprise 잔여 3 패키지 | 1 | ★★★★ | 5~7주 | 중 | ⚠️(D1 출원 후 권장) | ★(D1) |
| **C** advisor 진화 | 3 | ★★★★ | 5~7주 | 중 | ✅ | 0 |
| **D** customer experience | 2 | ★★★★ | 5~7주 | 중 | ⚠️(customer trigger 후) | ★(customer + billing) |
| **E** scan 엔진 진화 | 4 | ★★★ | 5~7주 | 중 | ⚠️(customer trigger 후) | ★(customer) |
| **F** web UI 진화 + 다국어 | 5 | ★★★ | 4~6주 | 낮음~중 | ✅ | 0~★(번역) |
| **G** compliance 진화 | 8 | ★★★★ | 4~6주 | 낮음 | ✅ | 0~★(ISO/NIST 정식) |
| **H** multi-tenant 강화 | 7 | ★★★ | 4~6주 | 중 | ✅ | 0 |

---

## 5. Top 3 권장 + 권장 진입 순서

memory `feedback_design_doc_conservative.md` 일관 — 잠재 효과/시간 보수적.

### 5.1 1순위 — 옵션 A (observability carryover 마감)

**근거**:
- v0.14.0 결선 직후의 가장 자연스러운 후속 — 5 carryover(testcontainers Jaeger smoke + Grafana 6 panel + UI trace_id + OTLP metric exporter + audit head sha mismatch metric) 모두 v0.14.0 hot 상태에서 누적된 부산물. 시간 지날수록 conextual cost 증가.
- 외부 트랙 의존 0 + 추정 3~4주(어느 옵션보다 짧음) — R12-1·R12-2 모두 충족.
- 회귀 위험 낮음 + 운영자 가치 직접 — testcontainers는 CI cache 회피로 비용 흡수, Grafana panel 6은 dashboard JSON only, UI trace_id 3 페이지는 PWA 회귀 표면 작음.
- 기술 부채 ★★★★★ — v0.14.0 carryover 5종 + Phase 10.A-6 head sha mismatch 1종 = 6 항목 일괄 마감. CHANGELOG [Unreleased] 정리 동시 진행.
- Phase 11 옵션 A 마감의 자연 마감타.

**추정**: 3~4주 — R12-2 충족.

### 5.2 2순위 — 옵션 G (compliance 진화)

**근거**:
- v0.12.0 SOC2 baseline 직후의 자연 후속 — A4 Privacy GDPR/CCPA/PIPA + IaaS cloud inheritance + soc2-controls pack archive embed + ISO/NIST cross-walk = v0.12.0 carryover 4 항목 cover.
- 추정 4~6주 + 리스크 낮음(docs 위주 + pack archive embed 일부만 코드) — R12-2·R12-3 충족.
- enterprise/compliance 가치 ★★★★★ — EU/CA/KR customer 진입 시 critical, 한국 PIPA는 한국 customer baseline 진입.
- 외부 트랙 의존 0(docs 매핑까지) — ISO 27001/NIST 800-53 정식 인증은 ★ 외부, 본 옵션은 매핑 docs까지만.
- 1순위(옵션 A) 마감 후 자연 진입 — compliance + observability 양쪽 baseline 진척 시 영업 자산 강화.

**추정**: 4~6주 — R12-2 충족.

### 5.3 3순위 — 옵션 C (AI advisor 진화)

**근거**:
- v0.14.0 `llm.complete` span 결선 후 자연 후속 — reasoning trace UI/multi-turn persist/token budget UI 모두 advisor 가치 가시화에 직접.
- 외부 트랙 의존 0 + 추정 5~7주 — R12-1·R12-2 충족.
- paying customer 가치 ★★★★ — advisor 옵트인 시점에 ROI 명확. enterprise 영업 시 LLM private deployment + reasoning trace + token budget UI 묶음으로 노출 가능.
- 1·2순위 마감 후 자연 진입 — observability + compliance + advisor 세 축이 Phase 12 마감 자산.

**추정**: 5~7주 — R12-2 충족.

### 5.4 권장 보류 (Phase 12 default에서 제외)

#### 5.4.1 옵션 B (enterprise 잔여 3 패키지) — 보류, ★ D1 출원 의존

memory `feedback_user_tracks.md` 정책 — D1 변리사 출원은 사용자 외부 트랙. 출원 *전* 외부 disclosure 위험 + R-D8 v3 4 청구권은 이미 cover됨(crosswitness · multihash · wasmrt · robotid). 출원 마감 후 Phase 13 진입 권장.

#### 5.4.2 옵션 D (customer experience) — 보류, ★ customer trigger

memory `feedback_user_tracks.md` 일관 — 첫 paying customer 진입 후 ROI 명확. multi-tenant feature gate는 customer 진입 *전*에도 가치 있으나 billing은 외부 결제 의존 ★. customer 진입 후 재평가.

#### 5.4.3 옵션 E (scan 엔진 진화) — 보류, ★ customer trigger

memory `feedback_user_tracks.md` 일관 — customer 부하 데이터 부재한 가설 단계. customer 진입 후 부하 측정 → 우선순위 재평가.

#### 5.4.4 옵션 F (web UI 진화) — 보류, 우선순위 중

UX 가치 중급 + customer 명시 요구 *전* 우선순위 낮음. dark mode tuning(C5b-10 carryover)만 별도 micro epic 가능. **Phase 13 후보**.

#### 5.4.5 옵션 H (multi-tenant 강화) — 보류, 우선순위 중

v0.10.0 carryover이나 audit chain v3 영향 가능 + fg-verify v3 호환성 검증 추가 필요. 다수 tenant customer 진입 *전* 우선순위 중. **Phase 13 후보**.

### 5.5 권장 진입 순서 timeline (보수적)

| 순서 | 옵션 | 추정 누적 시간 | trigger 시점 |
|---|---|---|---|
| 1순위 | A observability carryover 마감 | 3~4주 | 본 design doc 채택 직후 |
| 2순위 | G compliance 진화 | 7~10주 누적 | 1순위 마감 |
| 3순위 | C advisor 진화 | 12~17주 누적 | 2순위 마감 |
| 보류 (★) | B enterprise 잔여 3 패키지 | — | D1 출원 완료 후 (Phase 13) |
| 보류 (★) | D customer experience | — | 첫 paying customer 진입 후 |
| 보류 (★) | E scan 엔진 진화 | — | 첫 paying customer 부하 측정 후 |
| 보류 | F web UI 진화 | — | Phase 13 또는 customer 요구 시 |
| 보류 | H multi-tenant 강화 | — | Phase 13 또는 다수 tenant customer 진입 후 |

**Phase 12 마감 추정**: 보수적 **12~17주 누적**(1·2·3순위 순차). 마감 시 OpenTelemetry carryover 완전 마감 + compliance multi-framework + advisor 진화 세 축 추가. 운영자 관측성 ROI 가시화 + EU/CA/KR customer 진입 가능 + advisor upsell 가시화.

---

## 6. Stage 분해 (1순위 옵션 A — observability carryover 마감)

memory `feedback_design_doc_first.md` 일관 — 1순위만 본 doc에서 Stage 분해, 2·3순위는 진입 시점에 별 design doc 위임.

### 6.1 Stage 12.A-1 — design doc 채택 + 본 doc

본 round (docs only, 코드 0).

### 6.2 Stage 12.A-2 — testcontainers e2e Jaeger smoke

추정 **1주**.
- `test/integration/otel_jaeger_e2e_test.go` 신규 — docker-compose Jaeger all-in-one(`jaegertracing/all-in-one:latest`) + rosshield-server `--otel-enabled --otel-endpoint=jaeger:4317` + 1 scan 요청 → Jaeger Query API(GET `/api/traces?service=rosshield`) 검증.
- CI docker image cache(`actions/cache@v4` Jaeger layer cache)로 비용 흡수.
- testcontainers Go SDK(`testcontainers-go`) 기존 결선 활용.
- 단위 test 1 + integration 1.

### 6.3 Stage 12.A-3 — Lodestar Grafana dashboard panel 6종 신규

추정 **0.5~1주**.
- `deploy/grafana/rosshield-dashboard.json` 갱신 — 6 신규 panel 추가:
  - trace volume(`sum(rate(otelcol_exporter_sent_spans[5m]))`)
  - sampling effective rate(`rate(spans_sampled[5m]) / rate(spans_received[5m])`)
  - cross-region trace coverage(`count by(region) (otelcol_processor_batch_batch_send_size)`)
  - audit chain hash version(`rosshield_audit_chain_hash_version`)
  - audit rotation total(`rosshield_audit_rotation_total`)
  - audit key epoch(`rosshield_audit_key_epoch`)
- `docs/operations/opentelemetry-setup.md` 6 panel 스크린샷 + 운영자 가이드 추가.
- Prometheus query 검증(`promtool query instant`).
- 회귀: dashboard JSON only — 코드 영향 0.

### 6.4 Stage 12.A-4 — Lodestar UI 안 trace_id 표시 + Jaeger/Tempo deep link

추정 **1주**.
- 3 페이지에 trace_id 표시 컴포넌트 — scans/$scanId detail + audit detail + advisor 결정 detail.
- `web/src/components/common/TraceIdLink.tsx` 신규 — trace_id + Jaeger/Tempo deep link copy 버튼(env `VITE_TRACE_DEEPLINK_BASE` 활용).
- i18n `trace.*` ko + en 신규 키 ~6.
- HTTP response header `traceparent` 활용(v0.14.0 HTTP middleware 결선) — 응답 schema 변경 0.
- 단위 vitest 3 + Playwright e2e 1.

### 6.5 Stage 12.A-5 — OTLP metric exporter plug

추정 **0.5주**.
- `internal/platform/otel/metric.go` 신규 — OTLP metric exporter(`go.opentelemetry.io/otel/exporters/otlp/otlpmetric`) Prometheus federation 그대로 유지 + OTLP metric 양쪽 emit.
- `Config.MetricExporter` field 추가(`grpc`/`http`/`disabled` 3-tier, default disabled로 Prometheus 단독 유지).
- `--otel-metric-enabled` flag 신규(default false — opt-in 일관).
- 단위 test 4 (양쪽 emit + disabled noop + endpoint validation + metric attribute).

### 6.6 Stage 12.A-6 — audit chain head sha mismatch metric (Phase 10.A-6 carryover)

추정 **0.5주**.
- `audit_chain_head_sha_mismatch{tenant,region}` Counter 신규 — replication 시 leader/follower head sha 비교에서 mismatch 발견 시 increment.
- `internal/platform/metrics/metrics.go` 등록 + `internal/api/handlers/replication.go` heartbeat 응답 처리에서 emit.
- Grafana panel 1 추가(Stage 12.A-3 dashboard에 통합).
- 단위 test 3.

### 6.7 Stage 12.A-7 — release notes + CHANGELOG

추정 0.5일.
- v0.14.1 patch 또는 v0.15.0 minor — Phase 12 진입 첫 release. 6 신규 기능 묶음으로 minor bump 권장(v0.15.0).
- release notes + CHANGELOG entry + [Unreleased] carryover 5 항목 정리.

**Stage 12.A-2~12.A-7 = ~3~4주** (보수적). 1순위 마감 시 v0.14.0 carryover 5종 + Phase 10.A-6 1종 = 6 항목 일괄 마감 + 첫 v0.15.0 release.

---

## 7. 결정 항목 (D-P12-1·2)

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시.

### 7.1 D-P12-1 — 본 design doc 채택 + Top 3 우선순위

- (1) **채택 + Top 3 순서 합의** — A(observability carryover 마감) → G(compliance 진화) → C(advisor 진화) (**권장 default**).
- (2) 채택 + 1순위 변경 — G 또는 C 또는 다른 옵션을 1순위로.
- (3) 채택 + 보류 옵션 진입 — B(enterprise 잔여) 또는 D(customer experience) 등 보류 권장 옵션을 1순위로.
- (4) 거부 — 본 doc 비채택, 별 backlog 접근.

**근거**: Phase 0~11 11 milestone 마감 + 8 후보 매트릭스 + 권장 default 명시되어 다음 round 즉시 진입 부담 0. 옵션 A는 v0.14.0 carryover 5종 + Phase 10.A-6 1종 = 6 항목 일괄 마감 + 외부 트랙 의존 0 + 추정 3~4주(어느 옵션보다 짧음) + 회귀 위험 낮음. 옵션 G는 v0.12.0 SOC2 carryover 후속으로 EU/CA/KR customer 진입 가치 + docs 위주로 외부 트랙 의존 0. 옵션 C는 v0.14.0 `llm.complete` span 결선 후 advisor 가치 가시화 + paying customer upsell.

### 7.2 D-P12-2 — 1 epic 진행 vs 병렬 2 epic

- (1) **1 epic 진행** — 옵션 A design doc 1개 → Stage 2·3·4·5·6·7 순차. context 안정 (**권장 default**).
- (2) 2 epic 병렬 — 옵션 A + 옵션 G 동시(worktree 2개). 두 epic 도메인 충돌 없음(observability vs compliance docs) + sub-agent 가능. 같은 round 압축.
- (3) 옵션 A만 우선, 마감 후 옵션 G 진입 (옵션 1 동등하나 명시).

**근거**: 옵션 A는 3~4주 단독 epic이라 1 epic 진행이 정합. 옵션 G(compliance 진화)는 docs 위주로 도메인 충돌 0(A는 otel/handler/dashboard, G는 docs/compliance + pack archive embed) + sub-agent dispatch 가능이라 병렬 옵션도 합리. 사용자 선택. memory `feedback_parallel_agents.md`는 매 stage 시작 시 병렬 가능성 재평가 의무 — 본 결정에서 default 1 epic이나 stage 진입 시 재평가.

---

## 8. Carryover 통합

### 8.1 v0.10~v0.14 carryover + Phase 5~9 잔여 carryover

| Carryover | 권장 default | trigger 조건 | 외부 트랙 |
|---|---|---|---|
| testcontainers e2e Jaeger smoke (v0.14.0) | **옵션 A 1순위 cover**(Stage 12.A-2) | Phase 12 진입 직후 | — |
| otel metric exporter plug (v0.14.0, D-P11A-5) | **옵션 A 1순위 cover**(Stage 12.A-5) | Phase 12 진입 직후 | — |
| Lodestar Grafana dashboard panel (v0.14.0) | **옵션 A 1순위 cover**(Stage 12.A-3) | Phase 12 진입 직후 | — |
| Lodestar UI 안 trace_id 표시 + Jaeger/Tempo deep link (v0.14.0) | **옵션 A 1순위 cover**(Stage 12.A-4) | Phase 12 진입 직후 | — |
| audit chain head sha mismatch metric (Phase 10.A-6) | **옵션 A 1순위 cover**(Stage 12.A-6) | Phase 12 진입 직후 | — |
| fg-verify v3 binary 외부 감사인 분배 (v0.13.0, D-P11C-4) | 별 ★ 외부 트랙 | release artifact 후 외부 감사인 환경 | ★ 외부 감사인 |
| A4 Privacy GDPR/CCPA/한국 PIPA 상세 매핑 (v0.12.0) | **옵션 G 2순위 cover** | 1순위 마감 후 | — |
| DC physical (CC6.4) + environmental (A1.3) IaaS cloud SOC2 inheritance (v0.12.0) | **옵션 G 2순위 cover** | 1순위 마감 후 | — |
| soc2-controls pack archive embed (PACKS_SOURCE Makefile, v0.12.0) | **옵션 G 2순위 cover** | 1순위 마감 후 | — |
| humble pack archive embed (PACKS_SOURCE Makefile, v0.11.0) | 보류 유지 | 옵션 G 진입 시 함께 cover 권장 | — |
| manual rotation endpoint (v0.10.0) | 보류 유지 | 옵션 A 마감 후 micro epic | — |
| multi-tenant epoch 분리 (v0.10.0) | 옵션 H에 흡수 | tenant 격리 epic 필요 시 | — |
| Grafana dashboard panel (rotation_total + key_epoch + hash_version, v0.13.0) | **옵션 A 1순위 cover**(Stage 12.A-3) | Phase 12 진입 직후 | — |
| SROS2 cert chain intermediate CA + OCSP responder (v0.11.0) | 보류 유지 | paying customer 명시 요구 또는 ROS2 Phase 진입 시 | — |
| SROS2 keystore 자동 enrollment workflow | 보류 유지 | paying customer 명시 요구 시 | — |
| DDS topic whitelist expansion (`/diagnostics` · `/rosout` · custom, v0.11.0) | 보류 유지 | paying customer ROS2 사용 시 | — |
| Manual fixture Stage 3 low 5건 | 보류 유지 | 첫 paying customer 진입 후 | — |
| E22-F BOOLEAN 회수 옵션 A | 보류 유지(영구) | Big bang driver-aware repo 별 epic | — |
| scanrun extras epic A·B·C·D | 보류 유지 (옵션 E에 흡수) | ★ customer 부하 측정 후 | — |
| C5b-10 a11y polish Tailwind palette contrast | 보류 유지 또는 옵션 F와 통합 | UI 진화 옵션 F 진입 시 | — |
| MR.T4 application restart integration | 보류 유지 | Phase 13 testcontainers e2e Patroni 3-node + etcd 진입 시 | — |
| Stage 4.5 BIND/PowerDNS Terraform sample | 보류 유지 | DNS routing customer 명시 요구 시 | — |
| Stage 5b 잔여 carryover (C5b-6/7/8/9) | 보류 유지 | UI 진화 옵션 F 진입 시 | — |
| Phase 9.5 testcontainers e2e Patroni 3-node | 보류 유지 | Patroni customer 진입 또는 Phase 13 | — |
| awareness training (★ HR internal) | 보류 유지(영구 ★) | HR/internal 트랙 | ★ HR |
| vendor inventory + pen test schedule | 보류 유지 | ★ 외부 감사 트랙 | ★ 외부 |
| formal IR program + risk register | 보류 유지 | internal governance 별 epic | — |

### 8.2 사용자 외부 트랙 (★ 표기, memory `feedback_user_tracks.md` 일관 — 본 doc 권장 default에서 제외)

- ★ **D1 변리사 의뢰** — R-D8 청구권 KR 우선출원. 옵션 B(enterprise 잔여 3 패키지) trigger.
- ★ **E36 레퍼런스 HW burn-in** — NUC + OptiPlex + TPM 봉인 + Secure Boot 측정. 사용자 hands-on.
- ★ **첫 paying customer 진입** — Phase 6 R1+R2+R3 결선 후 customer trigger 대기. 옵션 D + E + Manual fixture + scanrun extras carryover 모두 영향.
- ★ **SOC2 외부 감사인 firm 계약** — v0.12.0 baseline 완료. 90일 운영 효과성 측정 + 정식 감사 라운드는 외부 firm 결정.
- ★ **fg-verify v3 binary 외부 감사인 분배** — v0.13.0 release 후 외부 감사인 환경 분배(D-P11C-4).
- ★ **awareness training 콘텐츠** — HR/internal 트랙.

---

## 9. 비목표 / 거부

본 Phase 12에서 명시 거부:

### 9.1 단일 customer 의존 epic

옵션 D(customer experience) + E(scan 엔진 진화)는 customer trigger ★. 단일 customer 부하 데이터/요구로 일반화 위험 — 여러 customer 진입 후 패턴 분석 권장. memory `feedback_user_tracks.md` 일관.

### 9.2 nrobotcheck-style 자체 하드웨어 제조

nrobotcheck 전신에서도 거부 — Lodestar는 software-only. 어플라이언스 OS(snap + TPM) 결선은 reference HW(★ E36) customer가 직접 burn-in. nrobotcheck-style Electron 모놀리식 UI 복귀도 거부 — PWA + Tauri 일관.

### 9.3 ISO 27001 전용 인증 진입

설계서 §00 일관 — SOC2 Type II baseline 우선, ISO 27001 / NIST 800-53는 매핑 docs cross-walk까지(옵션 G). 정식 ISO 인증은 별 외부 트랙 ★, customer 명시 요구 시점에 재평가. ISO/NIST 전용으로 SOC2 baseline 회수 또는 우회는 거부.

### 9.4 에이전트 프레임워크화 또는 자율 공격

설계서 §12 비목표 명시 — CAI 영토 회피. advisor는 옵트인 + reasoning trace + 결정론 fallback 일관. 옵션 C(advisor 진화)에서도 옵트인 default 유지.

### 9.5 LLM 필수 경로 생성

설계서 §1.2 옵트인 원칙 일관 — Phase 12 어느 옵션도 LLM 필수 경로 도입 0. 옵션 A observability carryover에서 advisor span 활용은 가능하나 필수 0. 옵션 C(advisor 진화)도 default off 유지.

### 9.6 tenant_id 없는 신규 테이블

설계서 §1.4 멀티테넌시 원칙 일관 — 옵션 C `advisor_conversations` 테이블 신규 시 tenant_id 컬럼 필수. 옵션 H multi-tenant epoch 분리도 tenant scope 강제.

### 9.7 UPDATE/DELETE 가능한 audit 테이블

설계서 §1.9 불변성 원칙 — append-only 강제. 옵션 A observability carryover는 metric Counter only(audit 변경 0). 옵션 H multi-tenant epoch 분리는 새 chain head부터 적용(과거 entry 변경 0).

### 9.8 Remote push 자동화

CLAUDE.md 일관 — local 커밋 OK, remote push 사용자 명시 요청 시에만. v0.15.0 minor release tag push도 사용자 결정.

### 9.9 단일 customer 의존 billing 통합

옵션 D feature gate 도입 시 billing integration(Stripe/Toss 등)은 ★ 외부 결제 의존으로 본 Phase 12에서 거부. customer 진입 후 외부 결제 서비스 결정 후 별 epic.

---

## 10. 회귀 위험 / 운영 고려

- **본 문서 자체 영향**: 0. docs only, 코드 0 / 마이그레이션 0 / pack 변경 0 / API 0.
- **Phase 0~11 baseline 회귀 0**: 본 doc은 후보 매트릭스 + 권장. 진입 epic은 별 Stage 분해에서 회귀 영향 평가.
- **다음 세션 진입 부담 0**: D-P12-1·2 모두 권장 default 명시 — 사용자 round 1회로 합의 가능.
- **carryover 보류 일관**: 22+ carryover + 6 사용자 외부 트랙 모두 default 보류/제외 또는 Top 3에 흡수 — Phase 12 진입에 영향 0.

---

## 11. 참조

### 11.1 직전 design doc 패턴

- `docs/design/notes/phase11-backlog-design.md` — Phase 11 진입 doc 패턴(본 doc 1차 모방).
- `docs/design/notes/phase10-backlog-design.md` — Phase 10 진입 doc 패턴.
- `docs/design/notes/soc2-readiness-design.md` — Phase 11 옵션 B 본체.
- `docs/design/notes/audit-hash-key-epoch-input-design.md` — Phase 11 옵션 C 본체.
- `docs/design/notes/opentelemetry-tracing-design.md` — Phase 11 옵션 A 본체.
- `docs/design/notes/audit-chain-rotation-automation-design.md` — Phase 10 옵션 D 본체.
- `docs/design/notes/ros2-humble-dds-sros2-design.md` — Phase 10 옵션 E 본체.
- `docs/design/notes/multi-region-ha-design.md` — Phase 8 epic 본체.
- `docs/design/notes/customer-onboarding-design.md` — Phase 6 1순위 doc.
- `docs/design/notes/llm-private-deployment-design.md` — LLM private deployment 결선.

### 11.2 release / CHANGELOG

- `docs/releases/v0.12.0.md` — Phase 11 옵션 B SOC2 readiness baseline.
- `docs/releases/v0.13.0.md` — Phase 11 옵션 C audit hash key_epoch + fg-verify v3.
- `docs/releases/v0.14.0.md` — Phase 11 옵션 A OpenTelemetry tracing 전면.
- `CHANGELOG.md` — [0.12.0]·[0.13.0]·[0.14.0] entry + [Unreleased] carryover 22+ 항목.

### 11.3 설계서

- `docs/design/01-principles.md` — 12 원칙.
- `docs/design/11-tech-stack-and-roadmap.md` — 로드맵 + 결정 로그.
- `docs/design/12-migration-and-non-goals.md` — 비목표.
- `docs/design/13-patent-strategy.md` — R-D8 청구권.

### 11.4 코드/디렉터리 fact-check 참조

- `internal/enterprise/{crosswitness,multihash,wasmrt,robotid,fleetxval,rostopo,selectdisclose}/` — 4 실 구현(8+8+13+17 files) + 3 placeholder 잔여(2+2+2 files).
- `internal/platform/llm/{noop,anthropic,ollama,vllm}/` — 4 provider 결선.
- `internal/platform/otel/{provider,sampler,resource,exporter,trace_context,llm}.go` — v0.14.0 OpenTelemetry SDK 결선.
- `internal/platform/httpclient/otel.go` — `WrapClient`/`WrapTransport` outbound 결선.
- `internal/api/handlers/otel_middleware.go` · `compliance.go` · `compliance_effectiveness.go` · `compliance_export.go` · `usage_stats.go` — v0.12.0+v0.14.0 결선.
- `internal/domain/audit/hash.go` — `ComputeEntryHash` v1 + `ComputeEntryHashV3` v3(canonicalMetaJSONv3 9 키 포함).
- `internal/domain/audit/audit.go` · `export.go` — Phase 10.D + 11.C KeyEpoch + LeaderEpoch + v3 bundle 결선.
- `internal/domain/compliance/{compliance,frameworks,mapping,soc2_mapping}.go` — v0.12.0 SOC2 14 카테고리 × 40 sub-control 매핑.
- `internal/app/advisorrun/{llm_client,orchestrator,tools}.go` — advisor 단발 요청-응답 + v0.14.0 `llm.complete` span 결선.
- `packs/{cis-ubuntu-2404,ros2-jazzy,ros2-humble,soc2-controls}/` — 4 pack(humble 29 + jazzy 29 + soc2 61 check).
- `web/src/i18n/dict.ts` — `Locale = 'ko' | 'en'` 2-language.
- `web/src/routes/_authenticated/` — 21 페이지(compliance.effectiveness · compliance.export · advisor · regions 포함).
- `docs/compliance/soc2/` — 14 매트릭스(cc1~cc9 + a1~a5).
- `docs/onboarding/{otel-collector-setup,llm-private-deployment,multi-region-ha-setup,sla-template,support-channels}.md` — 14 onboarding docs.
- `docs/operations/{opentelemetry-setup,audit-chain-key-rotation,audit-verify-cli,multi-region-failover-runbook,ros2-humble-deployment}.md` — 15 operations docs.
- `deploy/grafana/rosshield-dashboard.json` — Grafana 1 dashboard(panel 신규 micro epic carryover).

### 11.5 memory feedback

- `feedback_design_doc_first.md` — 1일+ 임계 design doc 우선.
- `feedback_design_doc_conservative.md` — 잠재 효과/시간 보수적.
- `feedback_parallel_agents.md` — 매 stage 시작 시 병렬 가능성 재평가.
- `feedback_user_tracks.md` — D1·E36·SOC2 감사·customer trigger 등 외부 트랙 제외(★ 표기).
- `feedback_no_rest_recommendation.md` — 휴식 옵션 자동 포함 X.
- `feedback_recommend_next_actions.md` — 다음 추천 작업 3~5건 명시.
- `feedback_skip_handoff.md` — handoff edit/commit/push 생략, CHANGELOG + release notes + commit 메시지로 trace.
- `feedback_naming_verification.md` — 공개 식별자 확정 전 WebSearch 검증.
- `feedback_go_commit_pipeline.md` — gofmt -w + go mod tidy + goimports + errcheck.

---
