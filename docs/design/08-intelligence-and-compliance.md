# 08. 지능화 기능 및 컴플라이언스

## 8.1 범위

이 문서는 **두 개의 축**을 함께 다룹니다. 분리하지 않는 이유는 **컴플라이언스 매핑 품질이 LLM 보조로 크게 향상**되고, 반대로 **LLM 기능의 가장 설득력 있는 유스케이스가 컴플라이언스 해석**이기 때문입니다.

1. **지능화 기능** (Insight · Advisor · LLM Adapter)
2. **컴플라이언스 엔진** (프레임워크 매핑 · 점수 · 서명된 리포트)

둘 다 **옵트인(P2)** 이며 **결정론적 fallback(P6)** 을 가집니다.

---

## Part A — 지능화

## 8.2 기능 맵

```
Scan/Result → Insight 후처리 파이프라인
   ├─ Drift Detection         (규칙 기반: prev vs current)
   ├─ Anomaly Detection       (규칙 + 통계, 옵션 LLM 설명)
   ├─ Peer Comparison         (규칙 기반: PeerGroup 대비)
   ├─ Root Cause              (규칙 기반 + 옵션 LLM 설명)
   ├─ Attack Path             (규칙 기반 그래프, 옵션 LLM 해설)
   └─ Predictive Maintenance  (시계열 트렌드, 옵션 LLM 해설)

Advisor (LLM)
   ├─ Conversational Debugger (대화형, 증거 인용)
   ├─ Remediation Suggest     (조치 추천, 근거 포함)
   ├─ NL Query                (자연어 → 결과 필터/리포트)
   └─ Change Review Assist    (벤치마크 변경 리뷰)

Compliance LLM 옵션
   ├─ Auto Mapping Suggest    (check ↔ control 매핑 제안)
   └─ Report Narrative        (리포트 서술부 초안)
```

## 8.3 LLM Adapter 계층

### 목적

공급자에 중립적인 호출 인터페이스. Anthropic·OpenAI·Ollama·vLLM·사내 LLM을 동일 코드로.

### 인터페이스 (개념)

```
LlmProvider {
  name: string
  capabilities: { streaming, toolUse, embeddings, maxContextTokens }
  
  complete(request: CompletionRequest) → CompletionResponse
  stream(request) → AsyncIterator<CompletionChunk>
  embed(texts: string[]) → number[][]
  health() → HealthStatus
}

CompletionRequest {
  system: string
  messages: Message[]
  tools?: ToolSpec[]
  responseFormat?: 'text' | 'json' | JsonSchema
  temperature, maxTokens, ...
  timeoutMs
}
```

### 기본 구현

- `noop` — 항상 실패(결정론적 fallback 경로 강제 발동). 테스트·기본 프로필.
- `ollama` — 로컬. 기본 모델 `llama3.1:8b` 또는 `qwen2.5:7b`.
- `vllm` — 자체 호스팅 vLLM 엔드포인트.
- `anthropic` — Claude API.
- `openai` — OpenAI API.
- `azure-openai` — Azure 배포.
- `openrouter` — 브리지.

### 호출 프로토콜

1. 설정에서 primary + fallback 공급자 체인 지정.
2. primary timeout/실패 → fallback → fallback-of-fallback.
3. 모든 공급자 실패 → **도메인 규칙 기반 응답**(`deterministicFallback`).
4. 결과는 `LlmTrace`에 기록(`Insight.llmTrace`, `Recommendation.llmTrace` 등).

### 비용·토큰 보호

- 요청당 최대 토큰 제한 (기본 8K 입력, 2K 출력).
- 테넌트별 일일 토큰 쿼터.
- 초과 시 fallback(규칙 기반)으로 자동 전환, 감사 엔트리.

### 민감 정보 보호

- 요청 전 **2차 레덕션** 파이프라인 실행 (Evidence에서 이미 1차 적용되었어도).
- 사용자가 LLM 요청 전 프리뷰를 볼 수 있는 UI 토글.

## 8.4 Insight 파이프라인

### 트리거

- `ScanCompleted` 이벤트 → Insight 후처리 큐.
- Drift/Anomaly/Peer/Root-cause/Attack-path/Prediction 각각 **독립 옵트인 플래그**.

### Drift Detection

- **입력**: 같은 robot × check의 최근 N개 세션 결과.
- **출력**: `pass→fail` 전환 또는 `fail→pass` 전환 탐지.
- **규칙 기반**: 결과 outcome 비교. evidence sha256 차이가 있으면 근거로 첨부.
- **LLM 옵션**: 변화 원인에 대한 서술(근거 필수).

### Anomaly Detection

- **입력**: 같은 PeerGroup 안에서 robot 한 대만 다른 결과를 보임.
- **규칙 기반**: 다수결에서 이탈한 로봇·체크 pair.
- **LLM 옵션**: 왜 특정 로봇만 다른지 설명 (환경 차이 추정).

### Peer Comparison

- PeerGroup은 `(osDistro, rosDistro, role)` 튜플로 자동 그루핑 + 사용자 태그 보정.
- 통계: fail rate, outlier, trend.

### Root Cause

- 규칙 기반: 공유 증거(같은 sha256 evidence)를 기준으로 fail 체크들을 묶음.
- LLM 옵션: "이 실패들은 `/etc/ssh/sshd_config` 하나의 설정이 원인"처럼 자연어 요약.

### Attack Path

- fail 체크들을 **ATT&CK MITRE Technique**로 매핑 (팩에서 제공).
- 간단한 그래프 구성: 초기 접근 → 권한 상승 → 횡적 이동 추정.
- LLM 옵션: 시나리오 서술.
- **주의**: 자율 공격 실행 없음 (비목표). 자체 탐색도 안 함.

### Predictive Maintenance

- 시계열 트렌드 → 특정 체크가 곧 fail로 갈 가능성.
- 규칙: rolling window에서 경향 감지.
- LLM 옵션: 서술·권장 조치.

## 8.5 Advisor (대화형)

### 대화 모델

- `Conversation` 엔터티: tenant·user·scope(session/robot/report) 연결.
- 메시지: system prompt + tool calls + 증거 인용.
- **증거 강제 인용**: 모델이 "문제가 있다"고 답할 때 반드시 `evidenceRefs`를 반환하도록 tool use 강제. 인용 없는 답은 "확실하지 않음"으로 대체.

### 툴 (LLM 호출)

- `queryResults(filter)`
- `getEvidence(evidenceId)`
- `compareSessions(sessionA, sessionB)`
- `listChecks(filter)`
- `suggestRemediation(checkId)`

### 안전 장치

- **읽기 전용 툴만**. 시스템 상태 변경·스크립트 실행은 별도 명시적 사용자 승인 흐름(UI에서 확인 → `POST /remediation/execute`).
- Prompt Injection 방어: 사용자·증거 텍스트를 **구분된 컨텍스트**로 주입, 시스템 지시와 섞이지 않도록.

## 8.6 결정론적 Fallback 예시

- Root Cause LLM 실패 → 규칙 기반(공유 evidence 그루핑)으로 "같은 파일을 참조하는 check 3개가 함께 fail" 같은 사실적 진술만.
- Anomaly LLM 실패 → "로봇 X는 체크 Y·Z에서 peer 다수(n=12)와 다른 결과"라는 수치 진술만.
- Advisor LLM 실패 → "현재 AI 설명을 사용할 수 없습니다. 수동 조치 가이드: [remediationDescription]"

모든 fallback은 **코드 패스 테스트** 필수.

## 8.7 LlmTrace

```
LlmTrace {
  provider: string
  model: string
  promptSha256: string        // prompt 자체는 저장, 필요 시 재현
  tokenUsage: { input, output, total }
  latencyMs: number
  outcome: 'success' | 'timeout' | 'schema_violation' | 'upstream_error' | 'fallback'
  safetyFlags: string[]       // 모델의 safety block, redaction 횟수 등
}
```

## 8.8 프롬프트 관리

- 모든 프롬프트 템플릿은 **버전 관리된 파일**(`prompts/*.yaml`)에 저장.
- 변경 시 리그레션 테스트 (골든 입출력 스위트) 통과해야 배포.
- 템플릿에는 `{locale}` 슬롯 — 한국어/영어 출력.

## 8.9 모델 선택 가이드

| 용도 | 권장 모델 유형 |
|---|---|
| Drift/Anomaly 서술 | 소규모(7B) 로컬 가능 |
| Root Cause 분석 | 중규모(70B) 또는 클라우드 |
| Advisor 대화 | 중~대규모, tool use 지원 |
| 자동 매핑 제안 | 대규모(클라우드 권장), JSON 모드 |
| 요약/서술부 | 소~중규모 |

**기본 프로필**: 에어갭/온프렘은 중규모 로컬(예: qwen2.5-32B 또는 llama3.1-70B-Instruct), 클라우드 옵트인은 Claude Sonnet/Opus 또는 GPT-4o 계열.

---

## Part B — 컴플라이언스 엔진

## 8.10 모델

```
ComplianceProfile ─┬─ FrameworkVersion
                   ├─ enabled: bool
                   └─ customizations: ControlCustomization[]

FrameworkSnapshot (세션별 실행 결과)
                   ├─ controlStatuses: ControlStatus[]
                   ├─ overallScore (가중 평균)
                   └─ createdAt
```

## 8.11 지원 프레임워크

| 프레임워크 | 버전 | 비고 |
|---|---|---|
| CIS Benchmarks | 24.04·22.04 | 벤치마크 ↔ 자기 자신 매핑(기본) |
| NIST 800-53 | rev. 5 | 통제 그룹 |
| ISO/IEC 27001 | 2022 | Annex A |
| IEC 62443 | 4-2 | 산업 제어 시스템 |
| ISMS-P | 2023 개정판 | 국내 |
| 주요정보통신기반시설 | 최신 고시 | 국내 |
| CC Common Criteria | PP 기반 | 사용자 요구 시 |
| KISA/K-CMVP | — | 암호 요구사항 |

각 프레임워크는 **mappings 팩**으로 제공됩니다 (벤치마크 팩과 분리). 컴플라이언스 팩은 **체크가 특정 통제에 매핑되는지**를 정의.

## 8.12 매핑 포맷

```yaml
apiVersion: fleetguard.dev/mapping/v1
kind: ControlMapping
metadata:
  framework: iso27001
  version: "2022"
spec:
  controls:
    - id: "A.8.9"
      name: { ko: "구성 관리", en: "Configuration Management" }
      mappedChecks:
        - packId: cis-ubuntu-24.04
          checkCode: CIS-1.1.1.1
          weight: 1.0
        - packId: cis-ubuntu-24.04
          checkCode: CIS-1.1.1.2
          weight: 1.0
        ...
      required: true
    - id: "A.9.1"
      ...
```

- `weight`로 부분 점수 가중.
- `required`로 "이 통제가 하나라도 fail이면 전체 부적합"을 표현.

## 8.13 점수 계산

### 기본 알고리즘

```
controlStatus(C) =
  let mapped = mappedChecks of C
  if mapped == ∅: return 'unmapped'
  let results = latest result per (robot, check) in session scope
  let passW = Σ result.pass ? check.weight : 0
  let totW  = Σ check.weight
  if passW == 0: 'fail'
  else if passW == totW: 'pass'
  else: 'partial'
  score(C) = passW / totW

overallScore(framework, session) = 
  weighted mean of score(C) for all enabled controls,
  with penalty for 'required' controls in non-pass status
```

- 단일 세션 기준과 **시간 구간(30/90일)** 기준 두 모드.
- Robot별 드릴다운 점수도 산출.

## 8.14 LLM 자동 매핑 제안 (옵트인)

### 흐름

1. 관리자가 새 벤치마크 팩 설치 → 매핑이 비어있거나 일부만 있음.
2. "매핑 제안" 실행 → LLM이 체크 정의 + 통제 정의를 보고 **추천 매핑 리스트** 반환.
3. 각 제안은 `{checkCode, controlId, weight, reasoning, confidence}`.
4. 관리자 UI에서 선택적으로 **승인/거절/수정** → `insertMany`.
5. 승인된 매핑은 **사용자 커스터마이징 팩**으로 별도 기록(시스템 팩은 수정 안 함).

### 프롬프트 구조

- 시스템: "너는 ISO27001 감사 전문가다. 주어진 체크 정의를 통제 항목에 매핑하라."
- 사용자: 체크 YAML + 통제 목록 + 기존 매핑 예시.
- 응답 스키마: JSON, 엄격 검증.

### 품질 가드

- LLM 응답이 스키마 위반이면 fallback: **문자열 유사도 + TF-IDF 기반** 추천.
- 신뢰도 0.6 미만은 UI에서 자동으로 "검토 필요" 배지.

## 8.15 컴플라이언스 리포트

### 종류

| 리포트 | 대상 | 형식 |
|---|---|---|
| Session Summary | 이번 세션 결과 | Markdown / HTML |
| Framework Report | 특정 프레임워크 상세 | **서명된 PDF** |
| Executive Summary | 경영진용 요약 | PDF / PPT |
| Audit Trail Export | 감사인용 증거 묶음 | NDJSON + blob 번들, 서명 |

### 서명된 PDF 구조

- 본문: 점수, 통제별 상태, 근거 Evidence 요약, 증거 해시.
- 뒷면: `ChainHead.hash` 포함.
- 서명: 기기 키 또는 조직 키(Enterprise).
- 검증: 제품이 제공하는 검증 페이지 또는 `openssl`로 수동 검증 가능.

## 8.16 커스터마이징

- 조직별 정책: 특정 통제 비활성, 가중치 조정, 통제 추가.
- 커스터마이징은 **사용자 팩**으로 저장, 시스템 팩과 병합.
- 모든 커스터마이징 변경은 감사 로그.

## 8.17 증거 번들 (Audit Export)

감사인 제출용 완성품.

```
audit-export-tn_...-2026Q1.tar.gz
  ├─ manifest.json
  ├─ chain/
  │   ├─ entries.ndjson
  │   └─ head.sig
  ├─ scans/
  │   └─ ss_.../results.json
  ├─ evidence/
  │   └─ ev_<sha256>.zst
  ├─ reports/
  │   └─ report-<id>.pdf
  └─ SIGNATURE
```

- 외부 검증 도구(별도 OSS 공개 예정)로 전체 번들 무결성 확인 가능.
- 감사인은 인터넷 없이도 번들만으로 검증 가능.

## 8.18 규제·인증 정렬

- 국내 ISMS-P·주요정보통신기반시설 인증 심사원 인터뷰를 **팩·리포트 포맷**에 반영 (필드명·증거 형식).
- 매핑 갱신은 **규제 개정 시 72시간 내 패치** 목표.

## 8.19 책임 경계

- 본 제품은 **감사 증거 생성 도구**입니다. "인증 통과 여부"를 판정하지 않습니다.
- 리포트에 "이 결과는 심사 보조 자료이며 최종 판단은 심사원에게 있음" 명시.
- 법적 면책 조항을 리포트 템플릿에 포함.

## 8.20 이 문서의 핵심 결정

1. **LLM은 모든 도메인 기능 위에 얇게 얹는다** — 결정론적 경로가 본체.
2. **증거 인용 없이는 LLM 답을 보여주지 않는다** — Prompt Injection과 환각 동시 방어.
3. **컴플라이언스 매핑도 "팩"** — 벤치마크와 동일한 서명·업데이트·커스터마이징 모델.
4. **서명된 PDF + 외부 검증 가능 번들**이 제품 가치의 핵심.
5. **국내 규제 매핑**에 투자 — ISMS-P, 주요정보통신기반시설. 해외 경쟁자 대비 해자.

다음 문서: [09-ui-and-clients.md](./09-ui-and-clients.md)
