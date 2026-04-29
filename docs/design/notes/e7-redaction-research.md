# E7 Redaction 엔진 — 외부 도구 정찰 및 권장 설계

> 작성일: 2026-04-29 · Phase 1 · Epic E7 · Stage 사전 조사
> 범위: SSH stdout/stderr (R4 캡 10MiB, 보통 ≤1MB)에서 비밀 마스킹
> 산출 형식: `redacted []byte` + `marks []RedactionMark{ Offset, Length, Type }`

---

## 1. 권장 라이브러리 결론

### 1.1 후보 정찰 결과

| 도구 | Go import 가능 | 라이선스 | 우리 모델(Apache-2.0 open-core) 호환 | 비고 |
|---|---|---|---|---|
| **gitleaks v8** (`github.com/zricethezav/gitleaks/v8`) | ✅ pkg.go.dev 등록됨 | **MIT** | ✅ OK | 24.4k stars, 200+ 룰 TOML, regex+entropy 결합 |
| **trufflehog v3** (`github.com/trufflesecurity/trufflehog/v3`) | ✅ | **AGPL-3.0** | ❌ **거부** | open-core 코어가 AGPL에 오염되면 엔터프라이즈 클로즈 산출물에 라이선스 호환 불가 |
| 자체 stdlib `regexp` | n/a | n/a | ✅ | 의존 0, 컨트롤 100%, 패턴 8~10개 손맛 작성 |

### 1.2 결정 권장

**A. 자체 구현 + gitleaks의 "패턴 카탈로그를 참고"하되 코드는 import 하지 않는다.**

이유:
1. **라이선스 청결도** — gitleaks MIT는 import해도 무방하지만, 그 의존 트리(go-git, fatih/semgroup 등)와 200+ 룰 TOML 로더까지 끌고 오면 바이너리·테스트 시작 시간이 부풀고, 우리가 필요한 건 **정확히 8~10개 패턴**이다. ROI 음수.
2. **결정론 원칙(§01-1)** — 외부 룰 팩이 업데이트되면 같은 입력에 다른 결과. Phase 1 증거 hash chain의 안정성 위협. 패턴은 우리 리포 안 `internal/redact/patterns.go`로 고정해야 evidence가 재현된다.
3. **trufflehog AGPL** — 라이브러리로 import 시 우리 서버 바이너리 전체가 AGPL 의무에 묶일 위험(특히 네트워크 서비스이므로 §13 "remote network interaction" 절의 SaaS clause). 어떤 형태로도 코드 차용 금지. 단, **패턴 자체는 저작권 보호를 받지 않는 사실(facts)**이므로 정규식 텍스트 참고 OK.
4. **성능** — RE2(Go stdlib) 단일 alternation으로 8~10 패턴 OR 합치기는 후보 1MB 입력 처리에 충분. 30~40배 빠른 hyperscan/wasilibs/go-re2 옵션은 cgo/wasm 의존을 추가하므로 단일 바이너리 원칙(§01-7) 위배. 보류.

**Phase 1 산물**:
- `internal/redact/` 신규 패키지 (코드 ≤ 400줄 권장)
- 외부 의존: 0
- 테스트 fixture: 룰별 positive 1+ negative 1, 추가 코너 케이스 6종 (§4)

---

## 2. Phase 1 패턴 선정 (8~10개)

### 2.1 패턴 카탈로그

번호별 우선순위. 모든 정규식은 Go RE2 문법이며 `(?i)` 케이스 무시는 키워드 부분에만, 토큰 본문은 case-sensitive로 둔다.

| # | Type 라벨 | 정규식 (Go raw string) | 비고 |
|---|---|---|---|
| R1 | `password_kv` | `(?i)(password\|passwd\|pwd)\s*[:=]\s*["']?([^\s"'&;]{4,128})` | group(2) 마스킹. 키 자체는 보존 |
| R2 | `bearer_token` | `(?i)Authorization\s*:\s*(Bearer\|Basic)\s+([A-Za-z0-9._\-+/=]{8,})` | group(2) 마스킹. HTTP 헤더 |
| R3 | `pem_block` | `(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----` | `(?s)` dot-matches-newline. RSA·EC·OPENSSH·DSA·일반 PRIVATE 모두 커버 |
| R4 | `aws_access_key` | `\b(?:A3T[A-Z0-9]\|AKIA\|ASIA\|ABIA\|ACCA)[A-Z0-9]{16}\b` | gitleaks의 검증된 prefix 집합. entropy 검사 불필요 (구조만으로 충분히 specific) |
| R5 | `github_token` | `\b(gh[posur]_[A-Za-z0-9_]{36,255})\b` | ghp_, gho_, ghs_, ghu_, ghr_ 모두 커버. 길이 범위 PAT 변천사 반영 |
| R6 | `slack_token` | `\bxox[abprs]-[A-Za-z0-9-]{10,}\b` | bot/user/refresh/legacy. 운영팀 webhook 누락 사고 대비 |
| R7 | `jwt` | `\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b` | 3-segment base64url. 헤더가 `eyJ`로 시작 (대부분 `{"alg":...` base64) |
| R8 | `db_url` | `(?i)\b(postgres\|postgresql\|mysql\|mongodb\|redis)://[^\s:]+:([^@\s]+)@` | group(2) 마스킹. DSN 비밀번호 |
| R9 | `ssh_password_url` | `(?i)\bssh://[^\s:]+:([^@\s]+)@` | R8과 분리 (스캔 컨텍스트상 흔함) |
| R10 | `high_entropy_b64` | `\b([A-Za-z0-9+/]{40,}={0,2})\b` + Shannon entropy ≥ 4.5 후처리 | 마지막에 적용. 위 R1~R9에 이미 매칭된 구간은 스킵 (중복 마크 방지) |

### 2.2 entropy 보조 알고리즘 (R10)

```
shannon(s) = -Σ p_i * log2(p_i)   // p_i = 문자 i의 빈도/길이
threshold = 4.5   // gitleaks 기본 4.3~4.5 관행. trufflehog issue #168 토론 참조
min_len   = 40    // 짧은 문자열은 우연히 high entropy
char_set  = base64 alphabet only
```

엔트로피 한계:
- 영어 문장 평균 ≈ 4.0~4.5 (특히 Korean·UTF-8 mixed 시 더 높을 수 있음 → ASCII-only 게이팅)
- 4.5는 **PHP webshell 탐지 연구 결과의 경험적 임계**(보안 연구 컨센서스)
- false positive 다수 → R10은 **opt-in 플래그**(`--redact-entropy`)로만 활성화 권장. Phase 1 기본은 R1~R9.

### 2.3 패턴 컴파일 전략

**단일 alternation vs 다중 컴파일** — RE2는 alternation에서 NFA를 공유하므로 이론상 OR 합치기가 빠르나, 실측 결과 5~10 패턴 규모에서 **각각 컴파일 후 순회가 디버깅·라벨링·캡처 그룹 관리에 압도적으로 단순**. Phase 1은 **다중 컴파일**, Type 라벨을 패턴별 메타로 들고 다닌다. 성능 문제 시 E11 최적화에서 재검토.

---

## 3. 알고리즘 (의사코드)

### 3.1 핵심 함수 시그니처

```go
package redact

type Mark struct {
    Offset int    // 원본 바이트 오프셋
    Length int    // 원본 바이트 길이
    Type   string // "password_kv", "pem_block", ...
}

type Engine struct {
    rules []rule  // {name string; re *regexp.Regexp; group int}
    enableEntropy bool
}

func (e *Engine) Redact(input []byte) (out []byte, marks []Mark)
```

### 3.2 의사코드

```
function Redact(input []byte) -> (out []byte, marks []Mark):
    raw := []Mark{}

    // 1. 모든 룰을 입력 전체에 대해 한 번씩 매칭
    for rule in rules:
        for match in rule.re.FindAllSubmatchIndex(input, -1):
            // group=0 이면 전체, group=N이면 캡처 N의 (start, end) 사용
            start, end := match[2*rule.group], match[2*rule.group+1]
            if start < 0: continue   // optional group 미매칭
            raw.append(Mark{Offset: start, Length: end-start, Type: rule.name})

    // 2. opt-in entropy 패스 (R10)
    if e.enableEntropy:
        for cand in highEntropyB64Re.FindAllIndex(input, -1):
            s, e2 := cand[0], cand[1]
            if shannon(input[s:e2]) >= 4.5 && !overlapsAny(s, e2, raw):
                raw.append(Mark{s, e2-s, "high_entropy_b64"})

    // 3. 정렬 (Offset 오름차순, 동률이면 Length 내림차순 — 큰 마크 우선)
    sort(raw)

    // 4. 병합: 겹치거나 인접(< 1바이트 간격)인 마크는 합치되,
    //    Type은 "여러 룰 동시 hit" 신호로 가장 specific한 것을 선택 (R3 PEM > R5 token > R10 entropy)
    merged := mergeOverlaps(raw)

    // 5. 단일 패스 substitution: out 버퍼에 [non-mark][REDACTED:type:N][non-mark]... 로 쓴다
    //    placeholder 형식: [REDACTED:<type>:<len>] (예: [REDACTED:pem_block:1704])
    //    원본 길이 보존 X (성능·가독성 우선). marks에 원본 (Offset, Length) 기록되어 추적 가능
    out := bytes.Buffer
    cursor := 0
    for m in merged:
        out.Write(input[cursor:m.Offset])
        out.WriteString(fmt.Sprintf("[REDACTED:%s:%d]", m.Type, m.Length))
        cursor = m.Offset + m.Length
    out.Write(input[cursor:])

    return out.Bytes(), merged
```

### 3.3 병합 규칙 상세

```
function mergeOverlaps(marks []Mark) -> []Mark:
    // 입력은 Offset 오름차순 정렬되어 있다 가정
    if len(marks) == 0: return []
    result := [marks[0]]
    for i := 1; i < len(marks); i++:
        prev := &result[last]
        cur  := marks[i]
        if cur.Offset <= prev.Offset + prev.Length:   // 겹침 또는 인접
            // 더 specific한 Type으로 교체 (priorityRank 표 참조)
            if priorityRank(cur.Type) > priorityRank(prev.Type):
                prev.Type = cur.Type
            // 더 멀리 뻗는 끝점으로 확장
            end := max(prev.Offset+prev.Length, cur.Offset+cur.Length)
            prev.Length = end - prev.Offset
        else:
            result.append(cur)
    return result
```

우선순위 표 (높을수록 specific):
```
pem_block          : 100
aws_access_key     :  90
github_token       :  90
slack_token        :  90
jwt                :  85
bearer_token       :  80
db_url             :  75
ssh_password_url   :  75
password_kv        :  60
high_entropy_b64   :  10  (가장 약함)
```

### 3.4 복잡도

- 시간: O(R · N) (R = 룰 수 ≤ 10, N = 입력 바이트). RE2 NFA 단일 패스 보장. 1MB 입력 기준 ~수 ms.
- 공간: 입력 1.0~1.5배 (output buffer + marks 배열). 10MiB 캡 입력에서 ~15MiB peak. 허용 범위.

---

## 4. 함정 및 테스트 케이스 후보

### 4.1 멀티라인 PEM
- 함정: `(?s)` 플래그 필수. 빠뜨리면 `.`이 `\n`을 매치 안 해서 PRIVATE KEY 본문 통째로 누락.
- 테스트: 정상 OPENSSH 키, BEGIN만 있고 END 없는 잘린 키 (매치 X 확인), 여러 키가 연달아 등장 (각각 매치).

### 4.2 CRLF vs LF
- 함정: Windows 호스트 stdout에서 CRLF 도달. `[^\s]` 류는 CR/LF 모두 stop이라 영향 없으나 PEM 블록 안 base64 라인 끝에 CR이 끼면 R3의 dot-all로 무관.
- 테스트: 같은 PEM을 LF·CRLF·CR-only 3종으로 입력 → 동일 마크 길이 ±1 허용 (또는 정규화 후 비교).

### 4.3 UTF-8 invalid sequences
- 함정: Go `regexp`는 invalid UTF-8을 panic 없이 RuneError(U+FFFD)로 처리. 그러나 **바이트 인덱스는 원본 기준** 유지되므로 마크 오프셋은 정확. 단, 출력 대체 문자열을 UTF-8 string으로 저장하면 안 되고 `[]byte`로 다뤄야 한다.
- 테스트: `\xff\xfe` 중간에 패스워드 fragment 삽입 → 마크 오프셋 정확히 일치.

### 4.4 거대 문자열 메모리 spike
- 함정: 10MiB stdout에서 `FindAllSubmatchIndex(-1)` 매치가 수천 개 발생하면 [][]int 슬라이스가 수십 MB 점유. 추가로 output buffer까지 합쳐 peak 60MB 가능.
- 완화: (a) 출력 버퍼 `bytes.Buffer.Grow(len(input))`로 사전 할당 (b) marks 슬라이스 capacity 추정 후 `make([]Mark, 0, estimate)` (c) 룰별 매치 수 상한 (예: 10000) 두고 초과 시 룰 단위로 스킵 + 경고 marks.
- 테스트: 1KB 패스워드 라인을 10000번 반복한 10MB 입력 → peak heap < 80MB, p95 latency < 500ms 목표.

### 4.5 겹침·인접 마크
- 함정: `Authorization: Bearer eyJ...` 는 R2(bearer)와 R7(jwt) 동시 매치. 단순 substitution 시 `[REDACTED][REDACTED]` 중첩 출력.
- 완화: §3.3 mergeOverlaps. R7이 R2의 group(2)와 정확히 겹치면 R7 우선(더 specific).
- 테스트: 위 케이스 → 단일 mark, Type=jwt.

### 4.6 password_kv false positive
- 함정: `password_required=true`, `password_min_length=8` 같은 설정 키는 R1에 hit하면 곤란.
- 완화: 후보 값에 영문자만 있고 모두 소문자 키워드(`true`, `false`, `null`, `none`, 숫자) 면 마크 제외하는 후처리. 또는 entropy 0.5 이하 짧은 매치 무시.
- 테스트: `password_required=true` 통과 확인.

### 4.7 base64 entropy false positive
- 함정: 한국어 ROS log·base64 thumbnail·git commit hash가 임계 초과.
- 완화: R10은 기본 비활성. 활성 시에도 길이 ≥ 40, 순수 ASCII base64 alphabet, 그리고 SHA1/SHA256 hex(`^[a-f0-9]{40}$`, `^[a-f0-9]{64}$`)는 화이트리스트.
- 테스트: git commit hash·image base64·random secret 3종 → 처음 2개 통과, 3번째만 마크.

### 4.8 zstd 압축 적용 시점 (참고)
- 함정: stdout 캡처를 zstd 압축 후 저장하고 retrieval 시 redaction 적용하면 (a) 파일 시스템·백업·RAM swap에 평문 비밀이 잔존 (b) sequence 함정 — 해시 체인이 평문 해시인지 redacted 해시인지 모호.
- **결정**: redaction은 **zstd 압축 직전, evidence hash 직전**에 적용. R7-X §3.2 참조 (보안 best practice 컨센서스: redact early at source).

### 4.9 ANSI escape sequences
- 함정: ROS2 CLI는 색상 escape `\x1b[0;31m` 삽입 → `password=\x1b[31msecret\x1b[0m` 같은 패턴이 R1 정규식 `[^\s"'&;]` 단속에 걸려 일부만 매치.
- 완화: Phase 1은 알려진 한계로 문서화. Phase 2에서 ANSI strip 전처리 옵션 검토.
- 테스트: ANSI 포함/제외 동일 입력 → 결과 다를 수 있음 (회귀 가드만).

---

## 5. 결정 권장 (R9-X 형식)

| ID | 제목 | 권장 | 근거 |
|---|---|---|---|
| **R9-1** | 외부 라이브러리 차용 정책 | **자체 구현 only**. gitleaks·trufflehog 코드 import 금지. 패턴 텍스트(사실)만 참고 | 라이선스 청결도(trufflehog AGPL 거부), 결정론적 evidence(원칙 §01-1), 단일 바이너리(§01-7), 의존 0으로 빌드·테스트 단순 |
| **R9-2** | 패턴 카탈로그 | Phase 1에 **R1~R9의 9개 패턴** 포함. R10 entropy는 `--redact-entropy` opt-in 플래그로만 활성화 | gitleaks 200+ 룰 중 SSH stdout/stderr 컨텍스트에서 의미 있는 핵심만. false positive 위험은 entropy를 분리해 격리 |
| **R9-3** | placeholder 형식 | `[REDACTED:<type>:<원본_바이트수>]` 고정. 원본 길이 보존 안 함 | 가독성·디버깅 우선. 정확한 위치 추적은 `marks []RedactionMark`로 제공. 길이 보존이 필요한 외부 시스템(diff/UI alignment)은 marks로 후처리 |
| **R9-4** | redaction 시점 | **zstd 압축 직전, evidence hash 직전**. 캡처 직후 메모리 단계에서 즉시 적용. 평문 stdout는 디스크에 절대 쓰지 않는다 | §4.8. 보안 best practice 컨센서스. evidence chain은 redacted bytes의 해시를 기록 |
| **R9-5** | 성능·메모리 가드 | (a) `bytes.Buffer.Grow(N)` 사전 할당 (b) 룰당 매치 상한 10000 (c) 입력 > 10MiB는 R4 캡 위반으로 거부(스캔 단계에서 차단) (d) Phase 1 측정: 1MB 입력 < 50ms p95, 10MiB 입력 < 1s p95 | 안전한 메모리 상한 + 회귀 감지를 위한 명시 SLO |

---

## 6. 참고 자료

- [Gitleaks GitHub (MIT)](https://github.com/gitleaks/gitleaks)
- [Trufflehog GitHub (AGPL-3.0)](https://github.com/trufflesecurity/trufflehog)
- [Gitleaks DeepWiki — Rule System](https://deepwiki.com/gitleaks/gitleaks/4-rule-system)
- [Trufflehog Issue #168 — Improving Entropy Calculation](https://github.com/trufflesecurity/truffleHog/issues/168)
- [wasilibs/go-re2 — drop-in 30~40배 빠른 RE2(보류 옵션)](https://github.com/wasilibs/go-re2)
- [Best regexp alternative for Go — Benchmarks](https://itnext.io/best-regexp-alternative-for-go-be42abdc1fbb)
- [Grafana Alloy — How to redact secrets from logs](https://grafana.com/blog/2025/03/20/how-to-redact-secrets-from-logs-with-grafana-alloy-and-loki/)
- [Datadog — Observability Pipelines sensitive data redaction](https://www.datadoghq.com/blog/observability-pipelines-sensitive-data-redaction/)
- [Skyflow — 9 Best Practices for Sensitive Data in Logs](https://www.skyflow.com/post/how-to-keep-sensitive-data-out-of-your-logs-nine-best-practices)
- [Go regexp 공식 문서](https://pkg.go.dev/regexp)
- [Go issue #19173 — regexp invalid UTF-8 처리](https://github.com/golang/go/issues/19173)
- [LeetCode 56 — Merge Intervals (알고리즘 reference)](https://leetcode.com/problems/merge-intervals/)
