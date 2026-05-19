# LLM Private Deployment 가이드

> Lodestar는 **옵트인 지능화**(P2) 원칙에 따라 LLM 어댑터가 기본 비활성(`noop`)입니다.
> 본 문서는 customer가 자체 인프라에 LLM을 띄워 Lodestar를 통합하는 방법을 안내합니다.
> 외부 SaaS LLM(Anthropic)은 `docs/onboarding/quickstart.md`의 별 섹션 참조.

## TL;DR

| 시나리오 | 추천 driver | 인프라 | 비용 |
|---|---|---|---|
| edge·데스크톱·소규모 robot fleet (CPU OK) | **ollama** | 로컬 daemon 1개 | $0 (로컬) |
| data center·다수 동시 호출·GPU 보유 | **vLLM** | docker + GPU | $0 (self-hosted) |
| 옵트인 차단 (기본) | `noop` | 없음 | $0 |
| 완전 외부 (cloud) | `anthropic` | API key | per-token |

본 가이드는 **ollama·vLLM** 두 옵션만 다룹니다 (옵션 C — 둘 다 driver로 지원, D-LLM-1).

---

## 옵션 비교

| 항목 | Ollama | vLLM |
|---|---|---|
| GPU 필수 | ❌ (CPU 가능, D-LLM-5) | ✅ (CUDA 권장) |
| 처리량 | 1~5 tok/s (CPU) ~20 tok/s (GPU) | 50~200 tok/s (GPU + batching) |
| 동시 요청 | 1 (serialize) | 다수 (continuous batching) |
| 설치 난도 | 매우 쉬움 (single binary) | 보통 (docker + cuda toolkit) |
| 모델 형식 | GGUF (양자화) | safetensors (HuggingFace) |
| API 형식 | 자체 NDJSON (`/api/generate`) | OpenAI-compatible (`/v1/chat/completions`) |
| 모델 자동 다운로드 | ✅ `ollama pull` | ❌ (수동 HF download) |
| 추천 사용처 | edge·desktop·offline demo | production·multi-tenant·SLA |

**선택 규칙**:
- 데모·소규모 PoC·offline appliance → **ollama**
- 다수 customer 동시 처리·낮은 latency 요구 → **vLLM**

---

## D-LLM 결정 사항 요약

| # | 항목 | 결정 |
|---|---|---|
| D-LLM-1 | inference 엔진 | vLLM (production) + Ollama (edge) 둘 다 driver |
| D-LLM-2 | default 모델 | Llama 3.1 8B Instruct (vllm) / llama3.2 (ollama) |
| D-LLM-3 | audit 범위 | **prompt 미기록** — provider+token+cost+error만 |
| D-LLM-4 | PII 마스킹 | middleware (caller인 advisor/insight가 evidence.Redact로 prompt 처리) |
| D-LLM-5 | GPU 부재 fallback | Ollama (CPU OK) |
| D-LLM-6 | streaming | 비활성 (vllm driver는 Complete만 지원, CompleteStream은 단일 chunk shim — Phase 8+ 별 epic) |
| D-LLM-7 | 모델 download | customer 본인 (offline tarball 옵션 — AutoPull은 명시 활성 시만) |

---

## 옵션 1 — Ollama (CPU/GPU edge)

### 설치

```bash
# Linux
curl -fsSL https://ollama.com/install.sh | sh

# macOS / Windows: https://ollama.com/download
```

### 모델 다운로드 (D-LLM-7)

```bash
# 8B 모델 — 8GB RAM 권장, GGUF Q4 양자화
ollama pull llama3.2:8b

# 또는 더 작은 3B (4GB RAM)
ollama pull llama3.2:3b
```

에어갭 환경에선 다른 머신에서 받아 모델 디렉토리(`~/.ollama/models`)를 tarball로 옮깁니다.

### Lodestar 설정 (환경 변수)

```bash
export ROSSHIELD_LLM_PROVIDER=ollama
export ROSSHIELD_LLM_BASE_URL=http://localhost:11434
export ROSSHIELD_LLM_MODEL=llama3.2:8b
export ROSSHIELD_LLM_TIMEOUT_SEC=120        # CPU 환경은 ↑
export ROSSHIELD_LLM_KEEP_ALIVE_SEC=1800    # 30분 메모리 유지 (load 비용 회피)
# 에어갭이면 false 유지 (default), 인터넷 OK면 true도 가능
export ROSSHIELD_LLM_AUTO_PULL=false
```

### 또는 CLI flag

```bash
./rosshield-server \
  --llm-provider=ollama \
  --llm-base-url=http://localhost:11434 \
  --llm-model=llama3.2:8b \
  --llm-timeout=120s \
  --llm-keep-alive=30m
```

### systemd EnvironmentFile 예시 (`/etc/rosshield/env`)

```
ROSSHIELD_LLM_PROVIDER=ollama
ROSSHIELD_LLM_BASE_URL=http://localhost:11434
ROSSHIELD_LLM_MODEL=llama3.2:8b
ROSSHIELD_LLM_TIMEOUT_SEC=120
ROSSHIELD_LLM_KEEP_ALIVE_SEC=1800
```

```ini
# /etc/systemd/system/rosshield.service
[Service]
EnvironmentFile=/etc/rosshield/env
ExecStart=/usr/local/bin/rosshield-server --data-dir=/var/lib/rosshield
```

---

## 옵션 2 — vLLM (GPU data center)

### 사전 요구

- NVIDIA GPU (Compute Capability ≥ 7.0 권장 — V100/A100/H100/RTX 30·40 시리즈)
- CUDA Toolkit 12.x
- Docker + NVIDIA Container Toolkit
- 모델 weights (HuggingFace에서 직접 다운로드 — `meta-llama/Llama-3.1-8B-Instruct` 등)

### 모델 다운로드 (D-LLM-7)

```bash
# HuggingFace CLI 권장 — 토큰 발급 후
pip install -U "huggingface_hub[cli]"
huggingface-cli login
huggingface-cli download meta-llama/Llama-3.1-8B-Instruct \
  --local-dir /opt/models/Llama-3.1-8B-Instruct
```

에어갭 환경: 다른 머신에서 받아 `/opt/models/`로 rsync.

### 서버 기동 (docker)

```bash
docker run --rm --gpus all \
  -v /opt/models:/models \
  -p 8000:8000 \
  --ipc=host \
  vllm/vllm-openai:latest \
  --model /models/Llama-3.1-8B-Instruct \
  --served-model-name meta-llama/Llama-3.1-8B-Instruct \
  --max-model-len 8192 \
  --gpu-memory-utilization 0.85
```

`/v1/chat/completions` 엔드포인트가 노출됩니다 (OpenAI-compatible).

선택: API key 인증 추가 (`--api-key sk-mysecret`).

### Lodestar 설정 (환경 변수)

```bash
export ROSSHIELD_LLM_PROVIDER=vllm
export ROSSHIELD_LLM_BASE_URL=http://vllm-host:8000
export ROSSHIELD_LLM_MODEL=meta-llama/Llama-3.1-8B-Instruct
export ROSSHIELD_LLM_TIMEOUT_SEC=60
export ROSSHIELD_LLM_MAX_TOKENS=1024
# vllm 서버가 --api-key를 설정한 경우만:
export ROSSHIELD_LLM_API_KEY=sk-mysecret
```

### 또는 CLI flag

```bash
./rosshield-server \
  --llm-provider=vllm \
  --llm-base-url=http://vllm-host:8000 \
  --llm-model=meta-llama/Llama-3.1-8B-Instruct \
  --llm-timeout=60s \
  --llm-max-tokens=1024 \
  --llm-api-key=sk-mysecret
```

---

## 환경 변수 전체 목록

| 변수 | flag | 의미 |
|---|---|---|
| `ROSSHIELD_LLM_PROVIDER` | `--llm-provider` | `noop` / `ollama` / `vllm` / `anthropic` |
| `ROSSHIELD_LLM_BASE_URL` | `--llm-base-url` | endpoint URL |
| `ROSSHIELD_LLM_MODEL` | `--llm-model` | 모델 식별자 (provider별) |
| `ROSSHIELD_LLM_API_KEY` | `--llm-api-key` | vllm/anthropic용 (Bearer 또는 x-api-key) |
| `ROSSHIELD_LLM_TIMEOUT_SEC` | `--llm-timeout` | 요청 wall-clock 상한 (초) |
| `ROSSHIELD_LLM_MAX_TOKENS` | `--llm-max-tokens` | vllm 응답 토큰 상한 (default 1024) |
| `ROSSHIELD_LLM_KEEP_ALIVE_SEC` | `--llm-keep-alive` | ollama 모델 메모리 유지 (default 300초 = 5분, 음수 = 즉시 unload) |
| `ROSSHIELD_LLM_AUTO_PULL` | `--llm-auto-pull` | ollama AutoPull (default false — 에어갭 안전) |

**우선순위**: CLI flag → 환경 변수 → 어댑터 default.

**legacy**: `ANTHROPIC_API_KEY`는 anthropic용으로 계속 인식 (backward compat).

---

## Audit 정책 (D-LLM-3)

Lodestar는 LLM 호출의 **prompt 본문을 audit에 기록하지 않습니다**. customer의 robot 환경·민감정보가 audit log로 유출되는 위험을 차단합니다.

audit entry에 남는 메타데이터:
- `llmProvider` — `ollama` / `vllm` / `anthropic`
- `llmModel` — 모델 식별자
- `inputTokens` / `outputTokens` — 토큰 카운트
- `costUsd` — anthropic만 정확, ollama/vllm은 0 (self-hosted)
- `error` — 실패 시에만

prompt·response 본문 자체는 `Insight`/`Advisor` 도메인의 `LlmTrace` 옆 별 컬럼에 저장될 수 있으나(개발 옵션), **default는 미기록**. 운영에서 본문을 보관해야 한다면 caller가 명시적으로 `evidence.Redact`로 PII 제거 후 저장.

---

## PII 마스킹 가이드 (D-LLM-4)

PII 마스킹은 **middleware 계층**이 담당합니다. 본 driver 패키지(`internal/platform/llm/*`)는 prompt를 그대로 송신하며, 마스킹은 caller(`internal/app/advisorrun`, `internal/app/insightautorun` 등)가 호출 직전에 적용합니다.

표준 흐름:
1. caller(advisor)가 evidence·scan 결과에서 prompt를 합성
2. `evidence.Redact(prompt)`로 IP·hostname·token·email 등 자동 마스킹
3. 마스킹된 prompt를 `llm.Adapter.Complete(ctx, req)`에 전달
4. driver는 그대로 송신 → LLM 응답 수신 → caller에게 반환
5. caller는 응답을 advisor turn으로 저장 (DB에는 마스킹된 형식만)

**driver 책임 외**: customer가 추가 마스킹 규칙(자체 reg-ex, 사내 자원명 패턴)을 적용하려면 caller 도메인의 redaction 규칙을 확장 — driver 수정 불필요.

---

## Troubleshooting

### 1. `llm: request timed out`

**원인**: ollama CPU 환경에서 8B 모델 응답이 60초를 초과.

**해결**:
```bash
export ROSSHIELD_LLM_TIMEOUT_SEC=300   # 5분으로 ↑
```

또는 더 작은 모델로 교체 (`llama3.2:3b`).

### 2. vLLM `CUDA out of memory`

**원인**: `--max-model-len` 또는 `--gpu-memory-utilization`이 GPU 메모리 초과.

**해결**:
```bash
docker run ... vllm/vllm-openai:latest \
  --max-model-len 4096 \                # 8192 → 4096
  --gpu-memory-utilization 0.7          # 0.85 → 0.7
```

또는 양자화 모델 사용 (`--quantization awq`, `--quantization gptq`).

### 3. `vllm: http 404 ... model not found`

**원인**: `ROSSHIELD_LLM_MODEL`이 vLLM의 `--served-model-name`과 불일치.

**해결**: vllm 기동 로그에서 정확한 모델 ID 확인 후 환경 변수 일치시킴.

### 4. ollama `model "llama3.2:8b" not found, try pulling it first`

**원인**: 모델을 `ollama pull` 하지 않은 상태.

**해결**:
```bash
ollama pull llama3.2:8b
```

또는 `--llm-auto-pull=true` (인터넷 가능 환경만 — 에어갭 환경은 사전에 tarball 배포).

### 5. `vllm: http 401`

**원인**: vllm 서버가 `--api-key`를 요구하지만 Lodestar에 키 미설정.

**해결**:
```bash
export ROSSHIELD_LLM_API_KEY=sk-mysecret
```

### 6. `unknown LLMProvider "xxx"`

**원인**: typo. 허용 값은 `noop` / `ollama` / `vllm` / `anthropic`만.

### 7. 첫 호출이 느리고 두 번째부터 빠름 (ollama)

**원인**: 모델이 메모리에서 unload된 상태에서 로드 비용.

**해결**: `ROSSHIELD_LLM_KEEP_ALIVE_SEC`를 batch 주기보다 길게 설정.
```bash
export ROSSHIELD_LLM_KEEP_ALIVE_SEC=3600   # 1시간 유지
```

---

## 검증

기본 health check로 LLM 상태 확인 (v0.7+ 예정):
```bash
curl http://localhost:8080/healthz | jq '.components.llm'
```

수동 호출:
```bash
# vLLM
curl http://localhost:8000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"meta-llama/Llama-3.1-8B-Instruct","messages":[{"role":"user","content":"ping"}],"max_tokens":10}'

# Ollama
curl http://localhost:11434/api/generate \
  -d '{"model":"llama3.2:8b","prompt":"ping","stream":false}'
```

---

## 참조

- 설계: `docs/design/notes/llm-private-deployment-design.md`
- 어댑터 코드: `internal/platform/llm/{vllm,ollama,anthropic,noop}/`
- bootstrap 매핑: `cmd/rosshield-server/bootstrap.go` (`buildLLMAdapter`)
- 원칙: `docs/design/01-principles.md` P2 (옵트인 지능화), P3 (에어갭 1급), P11 (설명 가능성)
- 인텔리전스 레이어: `docs/design/08-intelligence-and-compliance.md`
