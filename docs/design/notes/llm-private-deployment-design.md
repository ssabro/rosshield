# LLM Private Deployment Design (On-Prem Inference)

> **Status**: 설계 (Phase 5~7 carryover) · **Owner**: Platform/LLM
> **작성일**: 2026-05-19 · **참조**: §08 §01-P2/P6/P11 · `internal/platform/llm/` · v0.6.2 advisor opt-in badge
> **목표**: paying customer (enterprise/공공/금융/의료) on-prem inference 옵션 정립. 본 문서는 **코드 0줄 / 마이그레이션 0건** — 옵션 비교 + Stage 분해 + 결정 항목까지.

---

## 1. 상태 / 배경

### 1.1 현재 stack

`internal/platform/llm/`에 3 driver, **default `noop`** (R14-1 옵트인):

| Driver | endpoint | 비용 | privacy |
|---|---|---|---|
| `noop` | — (`ErrLLMDisabled` 즉시 반환) | 0 | 완전 |
| `ollama` | `http://localhost:11434` (NDJSON) | 0 | local-only (단, 모델 가중치는 customer가 직접 pull) |
| `anthropic` | `https://api.anthropic.com/v1/messages` | Anthropic 가격표 | **외부 vendor 전송** |

bootstrap (`cmd/rosshield-server/bootstrap.go:119,1445`):
- `cfg.LLMProvider ∈ {"", "ollama", "anthropic"}` → noop|ollama|anthropic 선택.
- v0.6.2: advisor 페이지 LLM opt-in badge 노출 (사용자가 명시 활성화 전 호출 0).

### 1.2 규제 압력 (paying customer 진입 차단 risk)

- **HIPAA (의료)**: PHI(Protected Health Information)가 외부 vendor API에 transit·rest 금지. BAA(Business Associate Agreement) 체결한 vendor만 허용. Anthropic은 2024년 HIPAA BAA 제공 시작이나 customer 별 negotiation 필수.
- **GDPR (EU)**: data subject 동의 없이 EU 밖으로 personal data 이전 금지 (Schrems II). 외부 vendor가 US 기반이면 SCC + TIA 별도 필요.
- **한국 개인정보보호법 (KISA)**: 국외 이전 사전 동의 + ISMS-P 인증 vendor. 공공/금융은 사실상 차단.
- **금융 (FSC/FSB)**: 망분리 (인터넷-내부망 분리) 정책 → 외부 vendor API 호출 자체 불가.

### 1.3 시장 수요 (carryover 5 중 하나로 식별된 이유)

- **enterprise SI** (SK·삼성·LG): 보안 review에서 "LLM 호출이 외부 vendor 가나" 첫 질문. on-prem 옵션 부재 시 PoC 진입 자체 거부.
- **공공/방산**: 망분리 환경. Ollama 단일 driver만으로는 production throughput 부족.
- **의료/금융**: HIPAA·FSC 요건상 air-gapped inference 필수.

→ paying customer 진입 prerequisite. Phase 5 (E32 enterprise pilot) 이전 정립 필요.

### 1.4 기존 설계서 표명 (§8.3)

`docs/design/08-intelligence-and-compliance.md` §8.3:
> **기본 구현**: noop · ollama · **vllm** (자체 호스팅 vLLM 엔드포인트) · anthropic · openai · azure-openai · openrouter

→ vLLM driver는 **이미 §8.3에 명시**되어 있으나 미구현. 본 design은 그 구현 spec.

---

## 2. 위협 모델 / 요구사항

### 2.1 leak surface (현 anthropic driver 사용 시)

LLM 호출 prompt에 들어갈 가능성 있는 sensitive data:

| data | source | sensitivity |
|---|---|---|
| robot fleet inventory (IP·hostname·serial) | scan target meta | high (network topology 추론) |
| scan evidence (file path·process list·sysctl) | evidence store | high (OS config leak) |
| audit chain hash · tenant context | audit log | medium (체인 무결성 trace) |
| CIS/STIG check failure detail | check result | medium (취약점 노출) |
| LLM advisor user query | UI input | low~high (user가 입력 통제) |

현 redaction (`evidence.Redact`)이 IP/hostname 마스킹은 하지만 — **외부 vendor 전송 자체를 금지**해야 하는 규제 요건이 존재.

### 2.2 요구사항

- **R-LLM-PRIV-1**: customer 환경 (망분리/air-gap) 내부에서 inference 완결.
- **R-LLM-PRIV-2**: audit chain에 LLM 호출 자체 기록 — provider + token count + cost 0. **prompt/response 원문 0**.
- **R-LLM-PRIV-3**: 기존 `Adapter` interface (`Complete` + `CompleteStream`) 호환 — caller 도메인 (Insight/Advisor) 변경 0.
- **R-LLM-PRIV-4**: latency p99 ≤ 5s (advisor 페이지 UX 임계).
- **R-LLM-PRIV-5**: customer pilot시 GPU 부재 환경 fallback — Ollama CPU mode 또는 noop.
- **R-LLM-PRIV-6**: model license가 commercial use 허용 (Llama 3.1 Community License · Gemma 2 Gemma License OK · Mistral 일부 제한).

---

## 3. 옵션 비교 (≥4)

| # | 옵션 | 접근 | dep · 비용 | privacy | latency p99 | 강점 | 약점 |
|---|---|---|---|---|---|---|---|
| **A** | **Ollama 단일 driver 강화** (기존 활용) | 기존 ollama dep · CPU/GPU 양쪽 | local-only | 1~10s (모델·HW 의존) | 가벼움 + 기존 코드 재사용 + edge 배포 OK | model 선택 manual + GPU 미활용 시 느림 + production throughput 한계 |
| **B** | **vLLM + GPU 서버 deploy 옵션** (별 컨테이너) | vLLM + CUDA + 별 컨테이너 | 완전 격리 | 100ms~2s (GPU) | production-grade throughput (continuous batching) + OpenAI API 호환 | GPU infra 부담 + cost + ops 복잡 |
| **C** | **Ollama + vLLM 둘 다 driver** (customer 선택) | 양쪽 dep | 양쪽 cover | 양쪽 cover | 유연성 max — edge=Ollama / data center=vLLM | 복잡 + 유지 부담 + 테스트 matrix 2배 |
| **D** | **Cloud LLM proxy** (PII 마스킹 + 외부 vendor 호출) | proxy 별 서비스 + Anthropic/OpenAI dep | 부분 — 마스킹 quality 의존 | 외부 API call latency | 외부 vendor latest 모델 활용 (Opus 4.x 등) | **진짜 private 아님** → HIPAA/금융 망분리 충족 0 |
| **E** | **llama.cpp 직접 임베드** (Go cgo) | llama.cpp + cgo | local-only | CPU 위주 (느림) | binary 단일화 + Ollama daemon 의존 0 | cgo 빌드 복잡 + Windows 빌드 risk + 유지 부담 |

### 3.1 권장 default: **옵션 C** (Ollama + vLLM 둘 다)

근거:
- **R-LLM-PRIV-1**: 둘 다 customer 내부 deploy 가능.
- **R-LLM-PRIV-4**: vLLM이 GPU 환경에서 p99 ≤ 5s 안정. Ollama는 edge fallback.
- **R-LLM-PRIV-5**: GPU 부재 customer는 Ollama로, 갖춘 customer는 vLLM로.
- 시장 segment split (edge vs data center)이 사실상 driver split과 1:1 매핑.
- §8.3 설계서가 이미 둘 다 명시 — 일관성.

옵션 D(proxy)는 본 task scope 외 (별 RFC). 옵션 E(cgo)는 Windows·Tauri 빌드 risk로 보류 (Phase 8+ 재논의).

### 3.2 보수적 추정 (메모리 [[feedback_design_doc_conservative]])

vLLM driver scaffold는 ollama driver와 ~70% 코드 재사용 가능 (HTTP client + NDJSON parser + LlmTrace 매핑). OpenAI 호환 endpoint이므로 response 스키마는 anthropic driver와 ~50% 재사용 (messages array + usage 객체).

→ 실 구현 부담은 spec 추정 (`~150줄 driver` + `~120줄 test`)보다 적을 가능성 높음. 운영 docs는 별 부담.

---

## 4. 아키텍처

### 4.1 driver 추가 (interface 변경 0)

```
internal/platform/llm/
├─ llm.go            (Adapter interface, 변경 0)
├─ noop/
├─ ollama/
├─ anthropic/
└─ vllm/             ← 신규
   ├─ vllm.go
   └─ vllm_test.go
```

`Adapter` interface는 §1.1 (`Complete` + `CompleteStream` + `Provider`) 그대로 유지. caller 도메인 (Insight/Advisor) 코드 변경 0.

### 4.2 vLLM HTTP API

vLLM은 **OpenAI 호환** endpoint 노출 (기본 `http://localhost:8000`):
- `POST /v1/chat/completions` (sync · streaming SSE)
- 응답 스키마: OpenAI 형식 (`choices[0].message.content` + `usage.prompt_tokens`/`completion_tokens`)

→ 향후 OpenAI driver 추가 시 same parser 재사용 가능 (Phase 8+ enterprise 옵션).

### 4.3 bootstrap config 확장

`cmd/rosshield-server/bootstrap.go`:

```
cfg.LLMProvider ∈ {"", "ollama", "anthropic", "vllm"}
                                                ^^^^^^
```

신규 환경변수:
- `ROSSHIELD_LLM_VLLM_ENDPOINT` (기본 `http://localhost:8000`)
- `ROSSHIELD_LLM_VLLM_MODEL` (기본 `meta-llama/Llama-3.1-8B-Instruct`)
- `ROSSHIELD_LLM_VLLM_TIMEOUT_SEC` (기본 60)

`buildLLMAdapter` switch case 추가 — error 메시지도 `allowed: noop|ollama|anthropic|vllm`로 갱신.

### 4.4 LLM 호출 호출 site 패턴 유지 (P6 결정론적 fallback)

```
if cfg.LLMProvider != "noop" {
    resp, err := adapter.Complete(ctx, req)
    if err != nil || resp.Content == "" {
        return ruleFallback()   // ← P6 일관
    }
    return resp.Content
} else {
    return ruleFallback()
}
```

→ vllm 호출 실패시도 동일하게 규칙 fallback. customer가 GPU 장애·OOM 등으로 vLLM 다운돼도 service 영향 0.

### 4.5 audit chain 기록 (R-LLM-PRIV-2)

기존 audit emit (`audit.Emit("llm.complete", ...)`)에 다음 필드만 기록:
- `provider` (예: "vllm")
- `model` (예: "meta-llama/Llama-3.1-8B-Instruct")
- `input_tokens` / `output_tokens`
- `duration_ms`
- `cost` (vLLM은 0)
- `error` (있을 시)

**prompt / response 원문은 기록 0** — privacy 요건상 chain에 들어가면 안 됨.

### 4.6 advisor 페이지 안내 갱신 (v0.6.2 badge)

advisor 페이지 LLM provider badge에 vllm 옵션 추가:
- "Ollama (local CPU)" · "vLLM (on-prem GPU)" · "Anthropic Cloud" 표시.
- 기본은 "Disabled (noop)".

---

## 5. TDD 진입

### 5.1 단위 (Red → Green)

`vllm_test.go`:
- **TestVLLM_Complete_Success**: mock HTTP server (`httptest.NewServer`) — OpenAI 응답 JSON 반환 → `Adapter.Complete` content 정확 추출.
- **TestVLLM_Complete_Timeout**: mock server `time.Sleep(2s)` + Adapter timeout 1s → `ErrTimeout` 반환.
- **TestVLLM_Complete_500Error**: mock server `http.StatusInternalServerError` → `error` 반환 + LlmTrace.Error 채워짐.
- **TestVLLM_Complete_Unauthorized**: mock server 401 → `ErrUnauthorized` 반환.
- **TestVLLM_CompleteStream_SSE**: mock server `text/event-stream` chunks 송신 → StreamChunk 채널 token 순서 정확.
- **TestVLLM_CompleteStream_DoneTrace**: stream 종료 후 Done=true chunk의 Trace.OutputTokens 정확.
- **TestVLLM_Provider**: `Provider()` == "vllm".

### 5.2 bootstrap 결선 단위

`bootstrap_test.go` (기존 파일 확장):
- **TestBuildLLMAdapter_VLLM**: `cfg.LLMProvider="vllm"` → `*vllm.Adapter` 반환.
- **TestBuildLLMAdapter_Unknown**: `cfg.LLMProvider="invalid"` → error 메시지에 `vllm` 포함.

### 5.3 e2e (Stage 4)

`internal/platform/llm/vllm/e2e_test.go` (build tag `e2e`):
- testcontainers-go로 vLLM CPU 이미지 (`vllm/vllm-openai:latest`) 실행.
- `Adapter.Complete` end-to-end 1회 → content 비어있지 않음 확인.
- CI는 기본 skip (GPU·time cost). 주간 nightly에서만 실행 (옵션).

---

## 6. Stage 분해 (5 stage)

### Stage 1: vllm driver scaffold (~1일)

- `internal/platform/llm/vllm/vllm.go` 신규 (~150줄)
- `internal/platform/llm/vllm/vllm_test.go` 신규 (~120줄)
- mock HTTP server 기반 단위 7개 PASS
- ollama driver 패턴 70% 재사용 (HTTP client + LlmTrace 매핑)

**완료 기준**: `go test ./internal/platform/llm/vllm/...` GREEN.

### Stage 2: bootstrap config + advisor 안내 (~0.5일)

- `bootstrap.go` `buildLLMAdapter` switch case 추가 (+10줄)
- `bootstrap.go` 에러 메시지 갱신 (`allowed: noop|ollama|anthropic|vllm`)
- `bootstrap_test.go` 2개 추가
- advisor 페이지 badge UI (TS) — provider 라벨 매핑 추가 (+10줄)

**완료 기준**: `go test ./cmd/...` GREEN + UI 빌드 GREEN.

### Stage 3: 운영 docs (~0.5일)

`docs/operations/llm-on-prem-deployment.md` 신규 (~300줄):
- vLLM 설치 가이드 (Docker · Kubernetes · bare-metal)
- 모델 가중치 download (HuggingFace · 사내 mirror)
- GPU 사양 권장 (Llama 3.1 8B → 16GB VRAM 최소 · Gemma 2 9B → 20GB)
- Ollama vs vLLM 선택 가이드 (edge vs data center)
- audit chain 영향 (prompt/response 기록 0 명시)
- HIPAA/GDPR/KISA 별 customer 가이드

**완료 기준**: docs lint GREEN + 외부 검토 OK.

### Stage 4: testcontainers e2e (옵션, ~1일)

- `vllm/e2e_test.go` 신규 (build tag `e2e`)
- CI workflow에 옵션 job 추가 (수동 trigger 또는 nightly)
- GPU 부재 CI에서는 vLLM CPU 모드 (느림 — timeout 5분 설정)

**완료 기준**: `go test -tags=e2e ./internal/platform/llm/vllm/` GREEN.

### Stage 5: customer pilot (~1주, 외부)

- enterprise pilot customer 1곳 (예: 공공 R&D 기관) 환경 deploy
- 1주간 사용 후 latency · throughput · 만족도 측정
- feedback 반영 (model 변경 · timeout 조정 등)

**완료 기준**: pilot 1개 PASS + 양적 지표 수집.

### Stage 추정 총합

| Stage | 추정 (보수적) |
|---|---|
| 1 driver scaffold | 1일 |
| 2 bootstrap+UI | 0.5일 |
| 3 운영 docs | 0.5일 |
| 4 e2e (옵션) | 1일 |
| 5 customer pilot | 1주 (외부) |
| **합 (Stage 1~4)** | **3일** |

→ Stage 5는 customer 의존이므로 schedule 외. 본 task는 Stage 1~3이 critical path.

---

## 7. 결정 항목 (권장 default)

| # | 항목 | 옵션 | 권장 default | 근거 |
|---|---|---|---|---|
| **D-LLM-PRIV-1** | inference 엔진 | vLLM · llama.cpp · TGI (Text Generation Inference) | **vLLM** | production-grade throughput (continuous batching) + OpenAI 호환 + Apache 2.0 + active community |
| **D-LLM-PRIV-2** | 추천 default 모델 | Llama 3.1 8B · Gemma 2 9B · Qwen 2.5 7B · Mistral 7B | **Llama 3.1 8B Instruct** | 한국어/영어 cover + Llama Community License (상용 OK · MAU 7억 미만) + 16GB VRAM 가능 + ecosystem 최대 |
| **D-LLM-PRIV-3** | audit 기록 범위 | provider만 · provider+token · full prompt/response | **provider + token count + cost + error** | R-LLM-PRIV-2 — prompt/response는 privacy risk |
| **D-LLM-PRIV-4** | PII 마스킹 layer | 별 layer · driver 내부 · caller (Insight/Advisor) 책임 | **caller 책임 (현재와 동일)** | evidence.Redact 기존 코드 재사용 + driver는 transport만 |
| **D-LLM-PRIV-5** | GPU 부재 fallback | Ollama CPU · vLLM CPU · noop | **Ollama CPU (R-LLM-PRIV-5)** | Ollama가 CPU mode 안정 + 모델 호환 (gguf) + customer 부담 최소 |
| **D-LLM-PRIV-6** | streaming 지원 | 동기만 · SSE streaming · gRPC streaming | **동기 + SSE streaming (현 Adapter interface 따름)** | advisor 페이지 UX 위해 streaming 필요 + vLLM SSE OpenAI 호환 |
| **D-LLM-PRIV-7** | 모델 download 방식 | bundle · runtime download · customer pre-stage | **customer pre-stage** | air-gap 환경 + 모델 라이선스 별도 동의 필요 + binary 크기 폭주 회피 |

→ **모두 사용자 합의 후 §12 결정 로그에 기록** (CLAUDE.md `## 결정 현황` 패턴 일관).

---

## 8. 변경 사항 outline

### 8.1 신규 파일

| 경로 | 추정 줄수 |
|---|---|
| `internal/platform/llm/vllm/vllm.go` | ~150 |
| `internal/platform/llm/vllm/vllm_test.go` | ~120 |
| `internal/platform/llm/vllm/e2e_test.go` (옵션) | ~80 |
| `docs/operations/llm-on-prem-deployment.md` | ~300 |
| **합** | **~650** |

### 8.2 수정 파일

| 경로 | +/- |
|---|---|
| `cmd/rosshield-server/bootstrap.go` | +15 / -3 |
| `cmd/rosshield-server/bootstrap_test.go` | +30 |
| `ui/src/pages/Advisor.tsx` (or 유사) | +10 |
| `docs/design/08-intelligence-and-compliance.md` | +20 (§8.3 vllm 구현 표기) |
| `README.md` 또는 `docs/operations/README.md` | +5 (link) |
| **합 (수정)** | **~80** |

### 8.3 전체 변경 추정 (보수적)

신규 ~650 + 수정 ~80 = **~730줄** (구현 ~200 + 테스트 ~200 + 운영 docs ~300 + 잡일 ~30)

> 보수적 추정 — Stage 5 customer pilot에서 추가 발견 시 fallback 로직·재시도·circuit breaker 등 +200줄 가능 (Phase 8+).

### 8.4 마이그레이션 영향

- DB schema 변경 0 — `audit_log.llm_provider` column 이미 존재.
- API surface 변경 0 — `LLMProvider` enum만 vllm 1개 확장.
- 기존 customer 영향 0 — default `noop` 유지.

---

## 9. 검증

### 9.1 회귀

- `go test ./...` GREEN (race 없이 Windows · race 있어 Linux)
- `go vet ./...` clean
- `make ci` GREEN

### 9.2 통합 (Stage 4)

- testcontainers vLLM CPU 이미지 e2e PASS (옵션 nightly)
- mock HTTP server 기반 driver 단위 100% PASS

### 9.3 customer pilot (Stage 5)

- latency p99 ≤ 5s (R-LLM-PRIV-4)
- 1주 운영 후 OOM·crash 0
- audit chain 무결성 검증 (`rosshield audit verify`) PASS
- prompt/response 외부 leak 0 (network trace 확인)

---

## 10. 비즈니스 / 라이선스 영향

### 10.1 vLLM 라이선스

- **vLLM**: Apache 2.0 → 코어 (Apache 2.0) 영역 통합 OK.
- enterprise BSL 1.1 영역과 무관 (vllm driver는 코어 platform).

### 10.2 모델 라이선스

| 모델 | 라이선스 | 상용 사용 | 비고 |
|---|---|---|---|
| Llama 3.1 8B Instruct | Llama Community License | OK (MAU 7억 미만 customer) | default 추천 |
| Gemma 2 9B | Gemma Terms of Use | OK | Google Cloud 통합 별 |
| Qwen 2.5 7B | Apache 2.0 (대부분 변형) | OK | 한국어 약함 |
| Mistral 7B v0.3 | Apache 2.0 | OK | 영어 우수, 한국어 약함 |

→ **customer가 직접 모델 license 동의** (D-LLM-PRIV-7 customer pre-stage 결정과 일관). Lodestar는 모델 가중치 미배포.

### 10.3 paying customer 진입 영향

- **HIPAA**: vLLM on-prem + 모델 가중치 customer 자체 보관 = BAA 불필요. 의료 customer 진입 가능.
- **GDPR**: EU customer 환경 deploy → data transfer 0 = SCC 불필요.
- **KISA / 망분리**: 인터넷 호출 0 = ISMS-P 인증 vendor 요건 무관.
- **Anthropic Cloud (기존)**: enterprise는 BAA·DPA·SCC 별 negotiate. paying customer pilot 진입 후 옵션 유지.

### 10.4 enterprise SI 입찰 영향

- RFP "on-prem AI 가능?" 항목에 **YES** 답변 가능 → PoC 진입 차단 해제.
- 공공/방산 입찰 자격 요건 충족.

---

## 11. 리스크

| # | 리스크 | 가능성 | 영향 | 완화 |
|---|---|---|---|---|
| **R1** | GPU 부재 customer 비율 (>50% 예상) | 높음 | 중 | Ollama CPU fallback (D-LLM-PRIV-5) + 운영 docs 가이드 |
| **R2** | model 선택 manual — onboarding 부담 | 중 | 중 | docs default 모델 표 + customer SE 지원 |
| **R3** | vLLM OpenAI 호환이지만 일부 endpoint 차이 (function call · embeddings) | 중 | 낮음 | driver는 `/v1/chat/completions`만 지원 (embeddings는 §8.3 future) |
| **R4** | vLLM CUDA 버전·driver 호환성 | 중 | 중 | 운영 docs CUDA 12.x 권장 + Docker 이미지 pin |
| **R5** | model 가중치 size (Llama 3.1 8B → 16GB+) — air-gap customer download 부담 | 높음 | 낮음 | customer 사내 HuggingFace mirror 권장 + 사전 download 가이드 |
| **R6** | latency p99 > 5s (R-LLM-PRIV-4 위반) | 낮음 | 높음 | Stage 5 pilot로 검증 + 실패 시 timeout 조정 또는 smaller model (7B) |
| **R7** | vLLM crash·OOM → service 영향 | 낮음 | 중 | P6 결정론적 fallback (`ruleFallback`) — service 자체 영향 0 |
| **R8** | testcontainers e2e CI cost (GPU 부재 시 매우 느림) | 중 | 낮음 | 옵션 nightly job · default skip |
| **R9** | 라이선스 변경 (Llama 4.x · Meta 정책 변경) | 낮음 | 중 | Gemma 2 · Qwen 2.5 대안 docs 명시 |
| **R10** | Phase 8 OpenAI driver 추가 시 코드 중복 | 중 | 낮음 | 공통 OpenAI response parser 추출 (refactor Stage 6+) |

---

## 12. 결정 로그

- 2026-05-19 — 본 design doc 작성 (carryover 5 중 LLM private deployment).
- (이하 사용자 결정 후 갱신)

| 결정 | 일자 | 옵션 | 근거 |
|---|---|---|---|
| D-LLM-PRIV-1 | TBD | 권장 vLLM | §3.1 / §7 |
| D-LLM-PRIV-2 | TBD | 권장 Llama 3.1 8B Instruct | §7 / §10.2 |
| D-LLM-PRIV-3 | TBD | 권장 provider+token+cost+error | §4.5 / §2.2 |
| D-LLM-PRIV-4 | TBD | 권장 caller 책임 (기존) | §7 |
| D-LLM-PRIV-5 | TBD | 권장 Ollama CPU fallback | §7 / §11 R1 |
| D-LLM-PRIV-6 | TBD | 권장 동기 + SSE streaming | §7 / §4.4 |
| D-LLM-PRIV-7 | TBD | 권장 customer pre-stage | §7 / §10.2 |

---

## 부록 A — 참조 코드 위치

- `internal/platform/llm/llm.go` — Adapter interface (변경 0)
- `internal/platform/llm/ollama/ollama.go` — vllm driver scaffold 패턴 참조 (HTTP + NDJSON → SSE 변환)
- `internal/platform/llm/anthropic/anthropic.go` — usage 객체 매핑 패턴 참조
- `cmd/rosshield-server/bootstrap.go:119,1445` — `LLMProvider` switch case
- `docs/design/08-intelligence-and-compliance.md` §8.3 — vllm 명시 (기존)
- `docs/design/01-principles.md` P2/P6/P11 — 옵트인 · 결정론적 fallback · 설명 가능성

## 부록 B — 향후 (out of scope)

- **OpenAI driver** (Phase 8+ enterprise option) — vLLM driver OpenAI parser 재사용
- **Azure OpenAI driver** (Phase 8+ enterprise option) — endpoint·auth만 차이
- **OpenRouter bridge** (Phase 8+) — 다중 vendor 라우팅
- **vLLM embeddings endpoint** (Phase 8+) — RAG·semantic search 위해
- **LLM function call / tool use** (Phase 9+) — agentic advisor 위해 (P1 deterministic evidence 원칙과 충돌 review 필요)
- **GPU pool scheduling** — multi-tenant 환경 GPU sharing (Phase 9+)
- **PII 마스킹 proxy** (옵션 D) — Anthropic Cloud 호출 시 prompt scrubber (별 RFC)
