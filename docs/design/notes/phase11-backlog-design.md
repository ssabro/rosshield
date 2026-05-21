# Phase 11 Backlog — Phase 10 마감 직후 차기 milestone 8 후보 매트릭스 + Top 3 권장 — Design

> **상태**: Phase 10 옵션 A·D·E Top 3 모두 마감(v0.9.0 + v0.10.0~v0.10.2 + v0.11.0) 직후 Phase 11 진입 합의용 design doc. 본 문서는 코드 0줄 / 마이그레이션 0건 / pack 변경 0 — Phase 0~10 회고 + Phase 11 후보 8종 매트릭스 + Top 3 권장 + 결정 항목 권장 default까지만 마감합니다.
> **참조**:
> - 직전 design doc 패턴: `notes/phase10-backlog-design.md`(Phase 10 진입 doc, 본 doc 1차 모방) · `notes/audit-chain-rotation-automation-design.md`(옵션 D 본체) · `notes/ros2-humble-dds-sros2-design.md`(옵션 E 본체).
> - 마감 release: `docs/releases/v0.11.0.md`(2026-05-21, Phase 10 옵션 E ros2-humble + DDS/SROS2 깊이 확장) — head `ebbfdf8` 기준.
> - CHANGELOG: `CHANGELOG.md` [0.9.0]·[0.10.0]·[0.10.1]·[0.10.2]·[0.11.0] entry.
> - 설계서: `docs/design/11-tech-stack-and-roadmap.md` 로드맵 + `docs/design/01-principles.md` 12 원칙 + `docs/design/13-patent-strategy.md` D8 청구권.
> **R 식별자**: R-PHASE11-1(본 doc 전체) — 결정 항목은 D-P11-1·2.
> **본 문서 작성 위치**: main(head `ebbfdf8`), 단독 sub-agent.

---

## 1. 상태 / 배경

### 1.1 Phase 0~10 마감 요약 (한 줄씩)

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

### 1.2 Phase 11 진입 가치

Phase 10 마감 시점에 baseline은 다음과 같이 확정되었습니다:
- **HA·DR·UI**: single-region E25 + cross-region replication + Patroni 자동 failover + `/regions` 운영자 가시성 표면화 결선. RTO ≤ 60초 + 운영자 카드 + alert + runbook 모두 cover.
- **audit chain 자동 운영**: signer key rotation quarterly cron + SwappableSigner Queue + emergency override + fg-verify v2 epoch backward compat 결선. 외부 감사인 호환성 보존.
- **ROS2 baseline 양분기**: Jazzy(LTS 2024-05~2029-05) + Humble(LTS 2022-05~2027-05) 양쪽 29 check 동기(8/8 카테고리 cover). SROS2 cert chain expiry/CA trust + DDS topic ACL 정밀화 깊이 확장 완료.
- **enterprise scaffold**: 7 패키지 중 4 패키지(crosswitness · multihash · wasmrt · robotid) 실 구현 진행, **3 패키지(fleetxval · rostopo · selectdisclose) placeholder 잔여**(R-D8 v3 4 청구권은 4 실 구현 cover).
- **LLM private 결선**: 4 provider(noop · anthropic · ollama · vllm) + private deployment docs 결선.
- **release infra**: v0.3.0~v0.11.0 = 38 release(cosign keyless 서명 + Sigstore Rekor 등록) + amd64+arm64 snap.

차기 milestone은 (a) **compliance gate 통과**(SOC2 Type II readiness) (b) **observability 진화**(OpenTelemetry distributed tracing) (c) **audit chain epoch input 완전 결합**(v0.10.0 carryover) (d) **enterprise 잔여 3 패키지 본체**(D1 출원 의존 ★) 네 축이 후보 풀입니다. memory `feedback_design_doc_first.md` 일관 — Phase 11 진입 시 1일+ 임계 작업은 design doc 우선.

---

## 2. 현재 상태 fact-check

본 §은 코드/디렉터리 직접 grep 결과 — 추측 0, fact만 명시.

### 2.1 OpenTelemetry 코드 부재 사실 (가설 1)

repo-wide `opentelemetry|otel|otelhttp|trace.Tracer` grep 결과(2026-05-21 head `ebbfdf8`):
- `docs/design/notes/phase10-backlog-design.md` — Phase 10 옵션 F 후보 언급만.
- `go.mod` · `go.sum` — transitive dep(다른 lib이 가져온 indirect)에 한정. 자체 `import` 0.
- `web/pnpm-lock.yaml` — front transitive.
- `internal/enterprise/robotid/quote_linux_test.go` — TPM quote의 `traceability` 키워드(otel과 무관).
- `packs/cis-ubuntu-2404/checks/5.1.4.yaml` · `docs/operations/cis-ubuntu-2404-degraded.md` — auditd `audit log path` 맥락(otel과 무관).

→ **OpenTelemetry SDK 통합 0**. `internal/platform/metrics/`(Prometheus + Grafana + eventbridge)만 결선. scan flow(SSH connect → check exec → evidence write → audit emit) trace · multi-region request trace · advisor LLM call trace 모두 부재. log aggregation pipeline(structured slog → Loki/ELK) 미존재.

### 2.2 docs/compliance/ 디렉터리 부재 사실 (가설 2)

`docs/` 트리(2026-05-21):
- `appliance/` · `design/` · `ip/` · `onboarding/` · `operations/` · `releases/` + 루트 PHASE2_EXIT_DEMO.md · PHASE3_EXIT_DEMO.md.

→ **`docs/compliance/` 디렉터리 없음**. SOC2 CC1~CC9 + A1~A6 control mapping · 외부 감사인 access wizard(read-only role + export) · effectiveness dashboard(통제별 audit event 집계) · `soc2-controls` benchmark pack 모두 미존재. compliance 키워드 검색은 release notes/CHANGELOG/cosign 서명 verification 맥락만.

### 2.3 enterprise placeholder 3 패키지 현황 (가설 3)

`internal/enterprise/` 디렉터리(head `ebbfdf8`):

| 패키지 | 파일 수 | 상태 | 산출 |
|---|---|---|---|
| `crosswitness/` | 8 files | **실 구현 중** | `anchor.go`(WebhookAnchor + FilesystemDumpAnchor) + `fold.go` + `scheduler.go` + 단위 테스트. A-1 cross-witness 외부 anchoring R-D8 청구권 cover. |
| `multihash/` | 8 files | **실 구현 중** | `compute.go` + `verify.go` + `jsonpath.go` + 단위 테스트. B-1 multi-hash evidence R-D8 청구권 cover. |
| `wasmrt/` | 13 files | **실 구현 중** | `runtime.go` + `cosign.go` + `policy.go` + `sigstore.go` + `limits.go` + 단위 테스트. C-1 WASM sandboxed evaluator R-D8 청구권 cover. |
| `robotid/` | 17 files | **실 구현 중** | `collector.go`(linux + other) + `fingerprint.go` + `quote_attestation.go` + `tpm_linux.go` + simulator test. D-3 robot identity binding R-D8 청구권 cover. |
| `fleetxval/` | **2 files** | **placeholder 잔여** | `doc.go` + `enterprise.go`(`EditionTag = "enterprise"`만). |
| `rostopo/` | **2 files** | **placeholder 잔여** | `doc.go` + `enterprise.go`(`EditionTag = "enterprise"`만). |
| `selectdisclose/` | **2 files** | **placeholder 잔여** | `doc.go` + `enterprise.go`(`EditionTag = "enterprise"`만). |

7 패키지 중 4 실 구현(R-D8 v3 청구권 4 cover) + **3 placeholder 잔여**. 우선순위 후보(R-D8 청구항):
- **selectdisclose**(D-2 R-D8) — selective disclosure / ZK redaction. 외부 감사인에 evidence 일부만 공개 + 나머지 cryptographic commitment 유지.
- **rostopo**(E-1 R-D8) — ROS2 그래프 cross-validation. ROS2 application layer 정밀화.
- **fleetxval**(F-1 R-D8) — fleet 간 cross-validation. 다수결/cross-witness 알고리즘.

### 2.4 web UI i18n 현황 (가설 4)

`web/src/i18n/`:
- `dict.ts`(ko + en 2 language) + `t.ts` + `store.ts` + `dict.test.ts` + `t.test.ts`.
- ko 기본, en fallback. dict 키 동기 강제(누락 시 CI fail).
- 다국어 추가 없음 — ja/zh/es/de/fr 등 부재.
- dark mode 토글은 PWA에 일부 cover되었으나 C5b-10 a11y polish Tailwind palette contrast carryover 존재.
- report builder UI(PDF 외 JSON/CSV export wizard) 부재 — 현재 PDF 단일 형식.

### 2.5 audit canonicalMetaJSON 현재 input (가설 5)

`internal/domain/audit/hash.go` 직접 Read 결과(head `ebbfdf8`):

```go
// hash_i = sha256( prevHash[32] ‖ payloadDigest[32] ‖ canonicalMetaJSON )
// meta 필드: tenantId, seq, occurredAt(RFC3339Nano UTC), actor, action, target, outcome.
```

`canonicalMetaJSON`은 알파벳순 7 키(action · actor · occurredAt · outcome · seq · target · tenantId)를 직렬화. **`keyEpoch` 미포함**.

Phase 10.D-2~D-5에서 `internal/domain/audit/audit.go::Entry.KeyEpoch *int64` 추가 + `internal/domain/audit/export.go::ExportEntryLine.KeyEpoch` + v2 bundle `_bundleVersion: "v2"` + `_chainKeyEpochs` 도입은 결선이나 **hash chain input은 변경 0**(v1 backward compat 일관).

→ **audit hash chain의 key_epoch input 포함은 v0.10.0 carryover 명시**(CHANGELOG.md [Unreleased] entry — "audit hash chain key_epoch+leader_epoch input 포함"). fg-verify v3 도입 시점에 v1/v2/v3 backward compat 필요.

### 2.6 LLM provider 4종 + private docs 사실 (가설 6)

`internal/platform/llm/`:
- `llm.go` — `Adapter` interface + `LlmTrace` + `ErrLLMDisabled` 등.
- `noop/` — 기본값(R14-1).
- `anthropic/` — cloud HTTPS.
- `ollama/` — 로컬 daemon(self-hosted, CPU/GPU).
- `vllm/` — OpenAI-compatible(GPU + continuous batching, self-hosted production).

`docs/design/notes/llm-private-deployment-design.md` + `docs/onboarding/llm-private-deployment.md` 결선 — ollama·vLLM 양쪽 운영자 가이드 + CPU/GPU 시나리오 매트릭스 cover.

미진행 영역(`internal/` grep 결과):
- **reasoning trace UI** — `Advisor` 결정에 step-by-step trace 표시 미존재(ReasoningTrace 키워드 0).
- **multi-turn conversation persist** — `ConversationHistory` 키워드 0(현 단발 요청-응답만).
- **redaction 자동화 audit emit** — advisor orchestrator tool 호출 trace의 audit emit 자동화 부재.
- **token budget cost guardrail UI** — R14-6 tenant daily token limit 통계 운영자 UI 미존재.
- **advisor RBAC 정밀** — 현 `advisor:read` 단일 권한.

### 2.7 Phase 6 customer onboarding 결선 사실 (가설 7)

`docs/onboarding/`:
- `customer-info-template.md` · `walkthrough.md` · `quickstart.md` · `demo-script.md` · `cis-customer-policy.md` · `multi-region-ha-setup.md` · `llm-private-deployment.md` · `audit-rotation-cosign.md` · `audit-rotation-s3.md` · `audit-rotation-verify.md` · README.md.

Phase 6 R1+R2+R3(intake API + walkthrough script + SLA template) 결선. 미진행 영역:
- **customer health dashboard** — 운영자가 customer 별 health 한눈에 확인 UI 부재.
- **billing integration** — 사용량/계약 별 결제 통합 0.
- **multi-tenant edition feature gate** — 현 build tag `enterprise`만, tenant 별 plan(community/pro/enterprise) gate 부재.
- **customer churn signal** — login 빈도/scan 빈도 등 paying customer health signal 부재.

### 2.8 scanrun extras 현황 (가설 8)

- `internal/app/scanrun/scanrun.go` · `scanrun_test.go` · `test/integration/sshd_e2e_test.go` 결선.
- `internal/platform/sshpool/` — Pool idle 재사용 + keepalive + 5 metrics(Phase 5 Stage 5b 마감).
- per-robot HealthFailureThreshold=3 결선.
- **scanrun extras epic A·B·C·D 보류**(Phase 6 + 10 carryover) — Pool size 동적 · per-tenant rate limit · per-robot circuit breaker · observability metric 확장. customer trigger 대기.
- 실 fleet scale 50~100 robot 검증 부재.

---

## 3. 위협 모델 / 요구사항 (Phase 11 진입 시)

### 3.1 신규/잔여 위협

| 위협 | 가능성 | 영향 | Phase 11 cover 영역 |
|---|---|---|---|
| SOC2 Type II 인증 실패 → enterprise 영업 critical gate 차단 | 높음(enterprise 영업 critical) | 영업 기회 손실 | 옵션 B docs/compliance/ + control mapping. |
| distributed system 디버깅 시 scan flow trace 부재로 latency 원인 파악 지연 | 중(고부하 customer 진입 시) | 운영자 대응 지연 + customer churn 위험 | 옵션 A OpenTelemetry 전면. |
| audit chain key_epoch input 미포함 → fg-verify v3 호환 위험 + 외부 검증 도구 cross-reference 회의 | 낮음(현 backward compat OK이나 미래 strict 검증 시) | 외부 감사인 회의 + 마이그레이션 부담 증가 | 옵션 C audit hash chain epoch input. |
| enterprise 잔여 3 패키지 placeholder → enterprise 영업 시 "어차피 4개만 실제 작동" 회의 | 중(enterprise demo 시점) | 영업 기회 손실 | 옵션 D enterprise 잔여 3 패키지 본체. |
| LLM advisor 가치 검증 부재 → paying customer "ROI 모름" | 중(advisor 옵트인 시점) | upsell 실패 | 옵션 E advisor 진화. |
| multi-tenant edition feature gate 부재 → community/pro/enterprise plan 차별화 부재 | 중(영업 진입 시 가격 책정 표면) | 가격 책정 불명 | 옵션 G customer experience. |

### 3.2 요구사항

| ID | 요구 | 측정 |
|---|---|---|
| R11-1 | Phase 11 1순위 epic은 외부 트랙 의존 0 또는 docs 우선 진입 가능 | D1 변리사/E36 hands-on/customer trigger/SOC2 감사 외부 의존 명시 |
| R11-2 | 1순위 epic 추정 ≤ 1.5개월 (보수적, Phase 10 평균보다 약간 길게 허용) | Stage 분해 합계 |
| R11-3 | 회귀 위험 ≤ 중급 | audit chain · 멀티테넌시 격리 변경 표면 작음 |
| R11-4 | Phase 0~10 baseline 회귀 0 | 기존 Go test 패키지 + RBAC 통합 + Playwright e2e PASS + ros2-jazzy/humble 29 check 유지 |
| R11-5 | design doc 우선 | 1일+ 임계 작업은 doc 먼저 (memory `feedback_design_doc_first.md`) |
| R11-6 | 보수적 추정 일관 | memory `feedback_design_doc_conservative.md` |

---

## 4. Phase 11 후보 8 옵션 비교

각 옵션마다 (a) 설계 요약 (b) 가치 (c) 노력 추정(보수적) (d) 전제·의존 (e) 리스크.

### 4.1 옵션 A — OpenTelemetry distributed tracing 전면 (가설 1, Phase 10 옵션 F 이월)

**설계 요약**: Prometheus metrics 결선 위에 OpenTelemetry SDK 통합 — scan flow(SSH connect → check exec → evidence write → audit emit) trace + multi-region request trace + advisor LLM call trace + Patroni failover trace. Jaeger/Tempo export + air-gap customer 비활성 옵션. log aggregation pipeline(structured slog → Loki/ELK 호환 어댑터).

**가치**:
- paying customer ★★ / enterprise ★★★★ (관측성 영업 도움) / compliance ★★★ (cross-system audit trace) / operational ★★★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **5~7주**. otel SDK 통합 + 4 hot path(scan/advisor/audit/replication) tracing 2주 + multi-region trace propagation(W3C tracestate · region tag) 1주 + customer Prometheus federation + Tempo/Jaeger export adapter 1주 + log aggregation pipeline(Loki/ELK 호환) 1.5주 + Grafana dashboard 갱신 + docs + e2e 0.5~1주.

**전제·의존**: 없음. 기존 Prometheus metrics 회수 없음(parallel 운영). air-gap customer 비활성 옵션 필수(R14-1 옵트인 원칙 적용 — `--otel-disabled` flag).

**리스크**: **중**. otel SDK 추가 dep + 모든 hot path span emit으로 인한 latency 영향 측정 필요(sample rate 0.1 default 권장). air-gap customer otel 비활성 옵션 필수 + dep 추가 표면 큼.

### 4.2 옵션 B — SOC2 Type II readiness (가설 2, Phase 10 옵션 B 이월)

**설계 요약**: `docs/compliance/` 신규 트리 — SOC2 CC1~CC9(보안 · 가용성 · 처리 무결성 · 기밀성 · 프라이버시) + A1~A6 control mapping(Lodestar 결선 자산을 SOC2 통제로 매핑 — audit chain · cosign 서명 · RBAC · TPM 봉인 · SSO · multi-region 등). 외부 감사인 접근 절차(read-only role + 감사 사본 export wizard) + 통제 effectiveness 측정 dashboard(통제별 audit event 집계) + 신규 benchmark pack `soc2-controls`(yaml 구조, SOC2 통제 단위 자동 evidence).

**가치**:
- paying customer ★★★★★ (enterprise 영업 critical gate) / enterprise ★★★★★ / compliance ★★★★★ / operational ★★★ / 기술 부채 ★

**노력 추정 (보수적)**: **6~9주**. control mapping 매트릭스 docs 2~3주 + 외부 감사인 access wizard(read-only role + export) 1주 + effectiveness dashboard(통제별 audit event 집계 + Grafana) 2주 + `soc2-controls` pack 신규(~80~120 check) 2~3주 + 외부 SOC2 감사인 검토 라운드(외부 트랙 ★).

**전제·의존**: 외부 SOC2 감사인 컨설팅(파트너 + 실 감사 라운드)은 별 외부 트랙(★ 표기). docs/control mapping은 외부 트랙 의존 0이나 실 SOC2 Type II 인증은 외부 감사인 90일 운영 측정 후 가능.

**리스크**: **중**. 신규 docs 트리 + 신규 pack은 회귀 위험 낮으나 effectiveness dashboard가 audit event 집계로 read-heavy query 추가 → 성능 영향 검증 필요. soc2-controls pack 80~120 check는 selftest fixture 작성 부담.

### 4.3 옵션 C — audit hash chain key_epoch input 포함 (가설 5, v0.10.0 carryover)

**설계 요약**: 현 audit chain `canonicalMetaJSON`에 `keyEpoch`(`*int64`, nil 시 미포함 — backward compat) 추가. v0.11.0+ bundle은 hash input에 epoch 포함 + v0.10.x 이하 backward compat 유지. `fg-verify` v3 도입 — v1(epoch 없음, v0.9.0 이하) / v2(epoch field but not in hash, v0.10.0~v0.11.0) / v3(epoch in hash, v0.12.0+) 세 모드 backward compat 자동 감지. `_bundleVersion: "v3"` 추가 + `audit.chain.epoch_input_activated` 이벤트.

**가치**:
- paying customer ★★ / enterprise ★★★★ (key rotation event 위변조 완전 차단) / compliance ★★★★ / operational ★★ / 기술 부채 ★★★★ (Phase 10.D carryover 마감)

**노력 추정 (보수적)**: **2~3주**. canonicalMetaJSON v2 신규(`keyEpoch` 알파벳순 추가) 0.5주 + Entry pipeline `KeyEpoch` propagation 검증 0.5주 + ComputeEntryHash 시그니처 갱신 0.5주 + fg-verify v3(v1/v2/v3 자동 감지) 1주 + 마이그레이션 0 (DB 스키마 변경 없음, 새 chain head부터 적용) + e2e 0.5주.

**전제·의존**: 없음. Phase 10.D-2~D-5 결선 활용. 외부 fg-verify 사용 customer는 v3 자동 감지로 회귀 없음.

**리스크**: **중**. hash input 변경은 critical — 활성 시점 명확 표기(`audit.chain.epoch_input_activated` event seq + timestamp) 필수. 외부 검증 도구가 v3 미지원 시 false negative 위험 → fg-verify v3 우선 release 후 활성 전환 권장(2-step migration).

### 4.4 옵션 D — enterprise 잔여 3 패키지 1차 구현 (가설 3, Phase 10 옵션 C 이월)

**설계 요약**: 7 패키지 중 placeholder 잔여 3개(fleetxval · rostopo · selectdisclose) 1차 본체 구현. 후보 우선순위(R-D8 청구항):
- **selectdisclose**(D-2 R-D8 — selective disclosure / ZK redaction): customer가 외부 감사인에 evidence 일부만 공개 + 나머지 cryptographic commitment 유지. SHA256/Merkle commitment 기반 jsonpath redact + verify round-trip.
- **rostopo**(E-1 R-D8 — ROS2 그래프 cross-validation): scan 결과 graph topology(`ros2 node list` · `ros2 topic list` 등) 검증으로 ROS2 application layer 정밀.
- **fleetxval**(F-1 R-D8 — fleet 간 cross-validation): 여러 robot의 동시 scan 결과 일관성 검증(다수결 또는 cross-witness 알고리즘).

**가치**:
- paying customer ★★ / enterprise ★★★★ (영업 demo 회의 차단) / compliance ★★★ / operational ★ / 기술 부채 ★★★★ (placeholder 완전 마감) / IP 보호 ★★★★

**노력 추정 (보수적)**: **5~7주** (한 패키지당 ~2주). selectdisclose 알고리즘 + jsonpath redact + cryptographic commitment(SHA256 또는 Merkle commitment) + 단위 테스트 + 통합 2주. rostopo는 ROS2 그래프 parsing + cross-validation 알고리즘 2주. fleetxval은 fleet 간 다수결 알고리즘 + cross-witness 호환성 2주. enterprise build tag boundary test 갱신 0.5~1주.

**전제·의존**: ★ **D1 변리사 출원 진행 상태에 따라 disclosure 시점 영향**. 출원 *전* 외부 PoC/blog 차단은 D8 결정 일관. 코드 자체는 enterprise build tag 안이므로 코어 코드베이스 disclosure 0(repo public 마감 후에도 build tag default off).

**리스크**: **중**. enterprise build tag 안에서만 컴파일되므로 코어 회귀 0. R-D8 청구권 disclosure 위험은 출원 후 진입 권장(★ D1 의존).

### 4.5 옵션 E — AI advisor 진화 (가설 6, paying customer-facing)

**설계 요약**: LLM provider 4종 + private deployment docs 결선 위에 advisor 진화 — reasoning trace UI 강화(advisor 결정에 step-by-step trace UI 표시 + audit emit `advisor.reasoning.recorded`) + multi-turn conversation persist 강화(`advisor_conversations` 테이블 신규 + 세션 컨텍스트 유지) + advisor RBAC 정밀(`advisor:read` / `advisor:write` / `advisor:admin` 세분) + token budget cost guardrail UI(R14-6 tenant daily token 통계 운영자 대시보드).

**가치**:
- paying customer ★★★★ (advisor 가치 가시화 → upsell) / enterprise ★★★ / compliance ★★ (reasoning trace audit) / operational ★★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **5~7주**. reasoning trace UI(step-by-step 컴포넌트 + dict 키 + e2e) 1.5주 + multi-turn persist(0039 마이그레이션 + Repository + Cleanup 정책) 1.5주 + advisor RBAC 세분(3 권한 + middleware + boundary test) 1주 + token budget UI(통계 query + Grafana panel 어댑터 + i18n) 1주 + redaction 자동화 audit emit 1주.

**전제·의존**: 없음. 기존 LLM 4 provider + private docs 활용. customer가 LLM 옵트인 시점에 가치(default off 일관).

**리스크**: **중**. multi-turn persist는 PII 누설 위험 — `evidence.Redact` 자동 적용 필수. token budget UI는 read-heavy aggregation query(tenant 별 daily 통계) 성능 검증 필요.

### 4.6 옵션 F — web UI 진화 + customer experience (가설 4, Phase 10 옵션 G 이월)

**설계 요약**: 운영자별 dashboard 커스터마이징(panel 선택 + 배치 + saved layout) + 다국어 폭 확장(ko/en → ja/zh/es 3개 추가) + dark mode tuning(C5b-10 a11y palette contrast carryover) + report builder UI(현재 PDF 단일 → JSON/CSV/XLSX export wizard).

**가치**:
- paying customer ★★★ (UX 가치 + 다국어 시장 확장) / enterprise ★★ / compliance ★ / operational ★★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **4~6주**. dashboard 커스터마이징(panel registry + drag-drop layout + saved layout IndexedDB persist) 2주 + 다국어 3개 추가(번역 + dict 동기 + RTL 미진행) 1.5~2주 + dark mode tuning(C5b-10 contrast) 0.5주 + report builder UI(JSON/CSV/XLSX export + 형식 선택 wizard + selftest) 1주 + e2e 0.5주.

**전제·의존**: 없음. PWA + RBAC + i18n 결선 baseline 활용. 번역 품질은 외부 검토 별 트랙(★ — 번역사 또는 native speaker review).

**리스크**: **낮음~중**. dashboard layout persist는 IndexedDB(PWA persist 결선 활용) — 회귀 표면 작음. 다국어는 dict 동기 부담 증가(번역 품질 외부 검토 별 트랙).

### 4.7 옵션 G — customer experience + paying customer 진입 (가설 7, Phase 6 carryover)

**설계 요약**: R1+R2+R3 결선 위에 customer experience 진화 — customer health dashboard(login/scan 빈도 + 첫 paying customer churn signal) + multi-tenant edition feature gate(community/pro/enterprise plan 별 feature flag + UI 표시) + 사용량 통계 API(billing integration 전 단계) + customer onboarding email 자동화(intake API 트리거).

**가치**:
- paying customer ★★★★ (영업 진입 직접) / enterprise ★★★ / compliance ★★ / operational ★★★ / 기술 부채 ★★

**노력 추정 (보수적)**: **5~7주**. customer health dashboard(query aggregation + Grafana panel + i18n) 1.5주 + multi-tenant edition feature gate(plan 테이블 + middleware + feature flag UI) 2주 + 사용량 통계 API(`/api/v1/usage/*` endpoint + tenant 별 집계) 1.5주 + onboarding email 자동화(intake → email template trigger) 1주 + e2e 0.5~1주.

**전제·의존**: ★ **첫 paying customer trigger 후 가치 ROI 명확**. customer 진입 *전* 가설 단계로 priority 모호. memory `feedback_user_tracks.md` 일관 — paying customer 진입은 사용자 외부 트랙.

**리스크**: **중**. feature gate 도입은 boundary 추가(community customer 격리 + enterprise feature 차단) → boundary test 갱신 부담. billing integration은 외부 결제 서비스 의존(Stripe/Toss 등) → 외부 트랙(★).

### 4.8 옵션 H — scan 엔진 진화(scanrun extras, 가설 8, Phase 5/6 carryover)

**설계 요약**: scanrun extras epic A·B·C·D 진입 — Pool size 동적(epic A) + per-tenant rate limit(epic B) + per-robot circuit breaker(epic C) + scan profile templating(scan profile 재사용 가능 templating) + scan history aggregation + cancellation graceful + observability metric 확장(epic D). 50~100 robot scale e2e benchmark.

**가치**:
- paying customer ★★ (customer 부하 trigger 후 가치 증분) / enterprise ★★★ / compliance ★ / operational ★★★★ / 기술 부채 ★★★

**노력 추정 (보수적)**: **5~7주**. epic A 1주 + B 1주 + C 1.5주 + D 0.5주 + scan profile templating 1주 + scale benchmark e2e fixture(docker-compose 50~100 sshd) 1~2주.

**전제·의존**: ★ **customer 환경 부하 데이터**(가설 단계). customer 진입 *전*에는 부하 측정 데이터 부재. memory `feedback_user_tracks.md` 일관.

**리스크**: **중**. concurrent scan + circuit breaker 변경은 scanrun core path 영향 — 회귀 위험 큼. e2e benchmark는 CI 비용 추가(50~100 sshd container).

### 4.9 매트릭스 종합

| 옵션 | 가설 | 가치 종합 | 시간 | 위험 | 즉시 진입 | 외부 트랙 의존 |
|---|---|---|---|---|---|---|
| **A** OpenTelemetry 전면 | 1 | ★★★★ | 5~7주 | 중 | ✅ | 0 |
| **B** SOC2 Type II readiness | 2 | ★★★★★ | 6~9주 | 중 | ✅(docs 우선) | ★(실 감사 외부) |
| **C** audit hash chain epoch input | 5 | ★★★★ | 2~3주 | 중 | ✅ | 0 |
| **D** enterprise 잔여 3 패키지 | 3 | ★★★★ | 5~7주 | 중 | ⚠️(D1 출원 후 권장) | ★(D1) |
| **E** advisor 진화 | 6 | ★★★ | 5~7주 | 중 | ✅ | 0 |
| **F** web UI 진화 | 4 | ★★★ | 4~6주 | 낮음~중 | ✅ | 0~★(번역) |
| **G** customer experience | 7 | ★★★★ | 5~7주 | 중 | ⚠️(customer trigger 후) | ★(customer + billing) |
| **H** scan 엔진 진화 | 8 | ★★★ | 5~7주 | 중 | ⚠️(customer trigger 후) | ★(customer) |

---

## 5. Top 3 권장 + 권장 진입 순서

memory `feedback_design_doc_conservative.md` 일관 — 잠재 효과/시간 보수적.

### 5.1 1순위 — 옵션 B (SOC2 Type II readiness)

**근거**:
- enterprise 영업 critical gate — SOC2 통과 없이는 enterprise customer 진입 불가능한 시장이 큼(금융 · 의료 · 정부).
- docs/control mapping은 외부 트랙 의존 0 — 1차 작업(docs/compliance/ 트리 + control mapping + access wizard + soc2-controls pack)은 사내 진행 가능. 실 SOC2 Type II 인증은 외부 감사인 90일 운영 측정(★ 외부 트랙).
- R-D8 v3 + multi-region UI + audit key rotation 등 Phase 7~10에서 결선된 자산이 SOC2 통제 그대로 매핑 가능(audit chain · cosign · RBAC · TPM · SSO 등) → control mapping에 자연 cover.
- Phase 10에서 옵션 B로 권장되었으나 carryover 처리(옵션 A·D·E 마감) — Phase 11에서 자연 재진입.
- 가치 종합 ★★★★★ — Phase 11 어느 옵션보다 영업 임팩트 큼.

**추정**: 6~9주 — R11-2(≤ 1.5개월) 약간 초과하나 docs/pack 작업 비중이 커서 외부 트랙 의존 부재로 충분히 분할 가능.

### 5.2 2순위 — 옵션 C (audit hash chain key_epoch input 포함)

**근거**:
- Phase 10.D 옵션 결선 시 명시 carryover — v0.10.0+ bundle은 epoch field 노출하나 hash input 미포함 → fg-verify v3 도입 시점에 마감 필요.
- 기술 부채 ★★★★ + Phase 10.D 마감의 자연 후속 → 다음 분기 도래 전 마감 권장.
- 추정 2~3주 — Phase 11 어느 옵션보다 짧은 가치 회수.
- 외부 트랙 의존 0 + customer 회귀 0(fg-verify v3 자동 감지 v1/v2/v3 backward compat).
- 1순위(옵션 B) 마감 후 짧은 round로 마감 가능 — Phase 11 마감 timeline 안에서 안정적.

**추정**: 2~3주 — R11-2 충족.

### 5.3 3순위 — 옵션 A (OpenTelemetry distributed tracing 전면)

**근거**:
- Phase 10 옵션 F로 권장되었으나 carryover 처리 — Phase 11에서 자연 재진입.
- enterprise 가치 ★★★★ + operational 가치 ★★★★ — multi-region 운영 진입 customer가 trace 없이 디버깅하기 어려움.
- 외부 트랙 의존 0 + air-gap customer 비활성 옵션(--otel-disabled flag) — R14-1 옵트인 원칙 일관.
- 1·2순위 마감 후 자연 진입 — multi-region UI(Phase 10.A) + audit chain epoch input(2순위) 결선 후 trace propagation 자연 cover.
- 추정 5~7주 — R11-2 약간 초과하나 분할 가능.

**추정**: 5~7주.

### 5.4 권장 보류 (Phase 11 default에서 제외)

#### 5.4.1 옵션 D (enterprise 잔여 3 패키지) — 보류, ★ D1 출원 의존

memory `feedback_user_tracks.md` 정책 — D1 변리사 출원은 사용자 외부 트랙. 출원 *전* 외부 disclosure 위험 + R-D8 v3 4 청구권은 이미 cover됨(crosswitness · multihash · wasmrt · robotid). 출원 마감 후 Phase 12 진입 권장.

#### 5.4.2 옵션 E (advisor 진화) — 보류, 우선순위 중

paying customer LLM 옵트인 시점에 가치 — 1·2·3순위 마감 후 자연 진입. **Phase 12 후보**.

#### 5.4.3 옵션 F (web UI 진화) — 보류, 우선순위 중

UX 가치 중급 + customer 명시 요구 *전* 우선순위 낮음. dark mode tuning(C5b-10 carryover)만 별도 micro epic 가능. **Phase 12 후보**.

#### 5.4.4 옵션 G (customer experience) — 보류, ★ customer trigger

memory `feedback_user_tracks.md` 일관 — 첫 paying customer 진입 후 ROI 명확. multi-tenant feature gate는 customer 진입 *전*에도 가치 있으나 billing은 외부 트랙. customer 진입 후 재평가.

#### 5.4.5 옵션 H (scan 엔진 진화) — 보류, ★ customer trigger

memory `feedback_user_tracks.md` 일관 — customer 부하 데이터 부재한 가설 단계. customer 진입 후 부하 측정 → 우선순위 재평가.

### 5.5 권장 진입 순서 timeline (보수적)

| 순서 | 옵션 | 추정 누적 시간 | trigger 시점 |
|---|---|---|---|
| 1순위 | B SOC2 Type II readiness | 6~9주 | 본 design doc 채택 직후 |
| 2순위 | C audit hash chain epoch input | 8~12주 누적 | 1순위 마감 |
| 3순위 | A OpenTelemetry 전면 | 13~19주 누적 | 2순위 마감 |
| 보류 (★) | D enterprise 잔여 3 패키지 | — | D1 출원 완료 후 (Phase 12) |
| 보류 | E advisor 진화 | — | Phase 12 |
| 보류 | F web UI 진화 | — | Phase 12 또는 customer 요구 시 |
| 보류 (★) | G customer experience | — | 첫 paying customer 진입 후 |
| 보류 (★) | H scan 엔진 진화 | — | 첫 paying customer 부하 측정 후 |

**Phase 11 마감 추정**: 보수적 **13~19주 누적**(1·2·3순위 순차). 마감 시 SOC2 readiness + audit chain epoch input 완전 결합 + distributed tracing 세 축 추가. enterprise 영업 critical gate 통과 + 기술 부채 마감 + 운영자 관측성 진화.

---

## 6. Stage 분해 (1순위 옵션 B — SOC2 Type II readiness)

memory `feedback_design_doc_first.md` 일관 — 1순위만 본 doc에서 Stage 분해, 2·3순위는 진입 시점에 별 design doc 위임.

### 6.1 Stage 11.B-1 — design doc 채택 + 본 doc

본 round (docs only, 코드 0).

### 6.2 Stage 11.B-2 — `docs/compliance/` 트리 신규 + control mapping 매트릭스

추정 **2~3주**.
- `docs/compliance/README.md` — SOC2 Type II 개요 + Lodestar 결선 자산 매핑 표.
- `docs/compliance/cc1-control-environment.md` ~ `cc9-risk-mitigation.md` — CC1~CC9 9 control 매트릭스.
- `docs/compliance/a1-availability.md` ~ `a6-confidentiality.md` — Additional Criteria 6 control.
- 각 통제에 Lodestar 결선 자산(audit chain · cosign · RBAC · TPM · SSO · multi-region · key rotation 등) cross-reference.
- 통제 effectiveness 측정 방법(audit event aggregation query) 명시.
- 한국어 + 영어 양쪽 docs(en은 paying customer/감사인 대상).

### 6.3 Stage 11.B-3 — 외부 감사인 access wizard (read-only role + export)

추정 **1주**.
- `auditor` 신규 role — RBAC 매트릭스에 `auditor:read` 단일 권한 + 모든 audit endpoint read 허용 + write 차단.
- `internal/api/handlers/compliance/export.go` 신규 — `GET /api/v1/compliance/auditor-bundle?period=90d` endpoint(admin 권한 필요, auditor role에 export 허용 별 endpoint).
- bundle 구조: audit_entries + cosign signatures + chain_keys + 통제별 evidence index + README.
- 단위 + e2e + boundary test(auditor role write 차단).

### 6.4 Stage 11.B-4 — 통제 effectiveness dashboard (audit event 집계)

추정 **2주**.
- `internal/api/handlers/compliance/effectiveness.go` 신규 — `GET /api/v1/compliance/effectiveness?control=CC6.6` endpoint.
- 통제별 audit event 집계 query(CC6.6 → `audit.chain.key_rotated` count + 정기성 분석).
- Grafana dashboard 신규 — `deploy/grafana/dashboards/compliance-effectiveness.json`.
- web `/compliance` 페이지 신규(admin 권한) — 통제별 status card + 마지막 evidence 시각.
- i18n 키 + e2e + boundary test.

### 6.5 Stage 11.B-5 — `soc2-controls` benchmark pack 신규 (~80~120 check)

추정 **2~3주**.
- `packs/soc2-controls/pack.yaml` 신규 — apiVersion + compatibility 명시.
- `packs/soc2-controls/checks/` — CC1~CC9 + A1~A6 통제 단위 yaml check ~80~120건. 각 check는 Lodestar 결선 자산을 query하여 통제 충족 여부 자동 평가.
  - 예: `CC6.6-key-rotation-quarterly.yaml` → audit chain key rotation 90일 이내 검증.
  - 예: `CC5.2-rbac-least-privilege.yaml` → RBAC 매트릭스 의 admin 권한 사용자 수 임계 검증.
  - 예: `CC7.2-monitoring-anomaly.yaml` → multi-region alert rule 활성 검증.
- selftest fixture 1:1 매칭.
- `ValidatePackYAMLBytes` + `ParseCheckYAML` + `ParseSelfTestYAML` + `RunCheckSelfTest` PASS.

### 6.6 Stage 11.B-6 — testcontainers e2e + Playwright e2e

추정 **0.5~1주**.
- `test/integration/compliance_e2e_test.go` — 90일 운영 simulation + auditor bundle export + effectiveness aggregate.
- Playwright e2e — `/compliance` 페이지 + auditor 권한 게이트 + bundle export 흐름.

### 6.7 Stage 11.B-7 — release notes + CHANGELOG

추정 0.5일.
- v0.12.0 minor — Phase 11 진입 첫 minor.
- release notes + CHANGELOG entry.

**Stage 11.B-2~11.B-7 = ~6~9주** (보수적). 1순위 마감 시 SOC2 Type II readiness docs + auditor role + effectiveness dashboard + soc2-controls pack + 첫 v0.12.0 minor release.

---

## 7. 결정 항목 (D-P11-1·2)

memory `feedback_design_doc_first.md` 일관 — 모든 결정에 권장 default 명시.

### 7.1 D-P11-1 — 본 design doc 채택 + Top 3 우선순위

- (1) **채택 + Top 3 순서 합의** — B(SOC2 readiness) → C(audit hash chain epoch input) → A(OpenTelemetry 전면) (**권장 default**).
- (2) 채택 + 1순위 변경 — C 또는 A 또는 다른 옵션을 1순위로.
- (3) 채택 + 보류 옵션 진입 — D(enterprise 잔여) 또는 E(advisor) 등 보류 권장 옵션을 1순위로.
- (4) 거부 — 본 doc 비채택, 별 backlog 접근.

**근거**: Phase 0~10 10 milestone 마감 + 8 후보 매트릭스 + 권장 default 명시되어 다음 round 즉시 진입 부담 0. 옵션 B는 enterprise 영업 critical gate + docs 우선 진입 가능 + 외부 SOC2 감사는 90일 운영 측정 필요로 일찍 시작할수록 가치. 옵션 C는 Phase 10.D carryover 마감 + 짧은 round로 ROI 높음. 옵션 A는 multi-region + key rotation 결선 후 자연 진입.

### 7.2 D-P11-2 — 1 epic 진행 vs 병렬 2 epic

- (1) **1 epic 진행** — 옵션 B design doc 1개 → Stage 2·3·4·5·6·7 순차. context 안정 (**권장 default**).
- (2) 2 epic 병렬 — 옵션 B + 옵션 C 동시(worktree 2개). 두 epic 도메인 충돌 없음(docs/pack vs audit/fg-verify) + sub-agent 가능. 같은 round 압축.
- (3) 옵션 B만 우선, 마감 후 옵션 C 진입 (옵션 1 동등하나 명시).

**근거**: 옵션 B는 6~9주 단독 epic이라 1 epic 진행이 정합. 옵션 C(audit chain epoch input)는 audit 도메인 충돌 0(B는 docs/handler/pack, C는 hash core) + sub-agent dispatch 가능(hash + fg-verify 격리)이라 병렬 옵션도 합리. 사용자 선택. memory `feedback_parallel_agents.md`는 매 stage 시작 시 병렬 가능성 재평가 의무 — 본 결정에서 default 1 epic이나 stage 진입 시 재평가.

---

## 8. Carryover 통합

### 8.1 v0.10.x + v0.11.0 + Phase 5~9 잔여 carryover

| Carryover | 권장 default | trigger 조건 | 외부 트랙 |
|---|---|---|---|
| audit hash chain key_epoch+leader_epoch input 포함 (v0.10.0) | **옵션 C 1순위 cover**(권장 default 2순위) | Phase 11 진입 직후 | — |
| audit chain head sha mismatch metric (Phase 10.A-6) | 보류 유지 | 1·2·3순위 마감 후 micro epic | — |
| manual rotation endpoint (v0.10.0) | 보류 유지 | 옵션 C 마감 후 micro epic | — |
| multi-tenant epoch 분리 (v0.10.0) | 보류 유지 | tenant 격리 epic 필요 시 | — |
| Grafana dashboard panel (rotation_total + key_epoch) (v0.10.0) | 보류 유지 또는 옵션 A와 통합 | 옵션 A OpenTelemetry 진입 시 | — |
| humble pack archive embed (v0.11.0) | 보류 유지 | PACKS_SOURCE Makefile humble 등록 micro epic | — |
| SROS2 cert chain intermediate CA + OCSP responder (v0.11.0) | 보류 유지 | paying customer 명시 요구 또는 ROS2 Phase 진입 시 | — |
| SROS2 keystore 자동 enrollment workflow | 보류 유지 | paying customer 명시 요구 시 | — |
| DDS topic whitelist expansion (`/diagnostics` · `/rosout` · custom payload) (v0.11.0) | 보류 유지 | paying customer ROS2 사용 시 | — |
| Manual fixture Stage 3 low 5건 | 보류 유지 | 첫 paying customer 진입 후 | — |
| E22-F BOOLEAN 회수 옵션 A | 보류 유지(영구) | Big bang driver-aware repo 별 epic | — |
| scanrun extras epic A·B·C·D | 보류 유지 (옵션 H에 흡수) | ★ customer 부하 측정 후 | — |
| ROS2 Round 3 carryover | **Phase 10 옵션 E에서 자연 cover** | 마감(2026-05-21) | — |
| C5b-10 a11y polish Tailwind palette | 보류 유지 또는 옵션 F와 통합 | UI 진화 옵션 F 진입 시 | — |
| MR.T4 application restart integration | 보류 유지 | Phase 9.5 testcontainers e2e Patroni 3-node + etcd 진입 시 | — |
| Stage 4.5 BIND/PowerDNS Terraform sample | 보류 유지 | DNS routing customer 명시 요구 시 | — |
| Stage 5b 잔여 carryover (C5b-6/7/8/9) | 보류 유지 | UI 진화 옵션 F 진입 시 | — |
| Phase 9.5 testcontainers e2e Patroni 3-node | 보류 유지 | Patroni customer 진입 또는 Phase 12 | — |

### 8.2 사용자 외부 트랙 (★ 표기, memory `feedback_user_tracks.md` 일관 — 본 doc 권장 default에서 제외)

- ★ **D1 변리사 의뢰** — R-D8 청구권 KR 우선출원. 옵션 D(enterprise 잔여 3 패키지) trigger.
- ★ **E36 레퍼런스 HW burn-in** — NUC + OptiPlex + TPM 봉인 + Secure Boot 측정. 사용자 hands-on.
- ★ **첫 paying customer 진입** — Phase 6 R1+R2+R3 결선 후 customer trigger 대기. 옵션 G + H + Manual fixture + scanrun extras carryover 모두 영향.
- ★ **SOC2 외부 감사인 컨설팅** — 옵션 B 실 인증 부분(90일 운영 측정 후 정식 감사 라운드).

---

## 9. 비목표 / 거부

본 Phase 11에서 명시 거부:

### 9.1 단일 customer 의존 epic

옵션 G(customer experience) + H(scan 엔진 진화)는 customer trigger ★. 단일 customer 부하 데이터/요구로 일반화 위험 — 여러 customer 진입 후 패턴 분석 권장.

### 9.2 nrobotcheck-style 자체 하드웨어 제조

nrobotcheck 전신에서도 거부 — Lodestar는 software-only. 어플라이언스 OS(snap + TPM) 결선은 reference HW(★ E36) customer가 직접 burn-in. nrobotcheck-style Electron 모놀리식 UI 복귀도 거부 — PWA + Tauri 일관.

### 9.3 에이전트 프레임워크화 또는 자율 공격

설계서 §12 비목표 명시 — CAI 영토 회피. advisor는 옵트인 + reasoning trace + 결정론 fallback 일관. 옵션 E(advisor 진화)에서도 옵트인 default 유지.

### 9.4 LLM 필수 경로 생성

설계서 §1.2 옵트인 원칙 일관 — Phase 11 어느 옵션도 LLM 필수 경로 도입 0. 옵션 B SOC2 control mapping에서 advisor reasoning trace 옵션 활용은 가능하나 필수 0. 옵션 E(advisor 진화)도 default off 유지.

### 9.5 tenant_id 없는 신규 테이블

설계서 §1.4 멀티테넌시 원칙 일관 — 옵션 B effectiveness dashboard 신규 query는 tenant scope 필수. 옵션 E `advisor_conversations` 테이블 신규 시 tenant_id 컬럼 필수.

### 9.6 UPDATE/DELETE 가능한 audit 테이블

설계서 §1.9 불변성 원칙 — append-only 강제. 옵션 B `audit.chain.epoch_input_activated` event 신규는 append만, 기존 audit_entries 변경 0. 옵션 C ComputeEntryHash 시그니처 갱신은 새 chain head부터 적용(과거 entry 변경 0).

### 9.7 Remote push 자동화

CLAUDE.md 일관 — local 커밋 OK, remote push 사용자 명시 요청 시에만. v0.12.0 minor release tag push도 사용자 결정.

---

## 10. 회귀 위험 / 운영 고려

- **본 문서 자체 영향**: 0. docs only, 코드 0 / 마이그레이션 0 / pack 변경 0 / API 0.
- **Phase 0~10 baseline 회귀 0**: 본 doc은 후보 매트릭스 + 권장. 진입 epic은 별 Stage 분해에서 회귀 영향 평가.
- **다음 세션 진입 부담 0**: D-P11-1·2 모두 권장 default 명시 — 사용자 round 1회로 합의 가능.
- **carryover 보류 일관**: 18 carryover + 4 사용자 외부 트랙 모두 default 보류/제외 — Phase 11 진입에 영향 0.

---

## 11. 참조

### 11.1 직전 design doc 패턴

- `docs/design/notes/phase10-backlog-design.md` — Phase 10 진입 doc 패턴(본 doc 1차 모방).
- `docs/design/notes/audit-chain-rotation-automation-design.md` — Phase 10 옵션 D 본체.
- `docs/design/notes/ros2-humble-dds-sros2-design.md` — Phase 10 옵션 E 본체.
- `docs/design/notes/auto-failover-research.md` — Phase 9 진입 doc 패턴.
- `docs/design/notes/multi-region-ha-design.md` — Phase 8 epic 본체.
- `docs/design/notes/customer-onboarding-design.md` — Phase 6 1순위 doc.
- `docs/design/notes/llm-private-deployment-design.md` — LLM private deployment 결선.

### 11.2 release / CHANGELOG

- `docs/releases/v0.9.0.md` — Phase 10 옵션 A multi-region UI 표면화.
- `docs/releases/v0.10.0.md` — Phase 10 옵션 D audit chain signer key rotation 자동화.
- `docs/releases/v0.10.1.md` · `v0.10.2.md` — v0.10.0 lint hot fix + 추가 gofmt 마감.
- `docs/releases/v0.11.0.md` — Phase 10 옵션 E ros2-humble + DDS/SROS2 깊이 확장.
- `CHANGELOG.md` — [0.9.0]·[0.10.0]·[0.10.1]·[0.10.2]·[0.11.0] entry + [Unreleased] carryover.

### 11.3 설계서

- `docs/design/01-principles.md` — 12 원칙.
- `docs/design/11-tech-stack-and-roadmap.md` — 로드맵 + 결정 로그.
- `docs/design/12-migration-and-non-goals.md` — 비목표.
- `docs/design/13-patent-strategy.md` — R-D8 청구권.

### 11.4 코드/디렉터리 fact-check 참조

- `internal/enterprise/{crosswitness,multihash,wasmrt,robotid,fleetxval,rostopo,selectdisclose}/` — 4 실 구현 + 3 placeholder 잔여.
- `internal/platform/llm/{noop,anthropic,ollama,vllm}/` — 4 provider 결선.
- `internal/domain/audit/hash.go` — canonicalMetaJSON에 keyEpoch 미포함 fact.
- `internal/domain/audit/audit.go` · `export.go` — Phase 10.D-2~D-5 KeyEpoch field + v2 bundle 결선.
- `internal/api/handlers/replication.go` — multi-region 4 endpoint.
- `internal/platform/metrics/` — Prometheus + Grafana(OpenTelemetry 0).
- `packs/{cis-ubuntu-2404,ros2-jazzy,ros2-jazzy-baseline,ros2-humble}/` — 4 pack(humble 29 + jazzy 29 동기).
- `web/src/i18n/dict.ts` — ko/en 2 language.
- `docs/onboarding/*` · `docs/operations/*` — 21 markdown 결선(docs/compliance/ 부재 fact).

### 11.5 memory feedback

- `feedback_design_doc_first.md` — 1일+ 임계 design doc 우선.
- `feedback_design_doc_conservative.md` — 잠재 효과/시간 보수적.
- `feedback_parallel_agents.md` — 매 stage 시작 시 병렬 가능성 재평가.
- `feedback_user_tracks.md` — D1·E36·SOC2 감사·customer trigger 등 외부 트랙 제외(★ 표기).
- `feedback_no_rest_recommendation.md` — 휴식 옵션 자동 포함 X.
- `feedback_recommend_next_actions.md` — 다음 추천 작업 3~5건 명시.
- `feedback_skip_handoff.md` — handoff edit/commit/push 생략, CHANGELOG + release notes + commit 메시지로 trace.
