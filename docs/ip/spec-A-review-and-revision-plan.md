# 명세서 후보 A — 외부 검토 의견과 반영 계획

> **이 문서의 위치**: `docs/ip/spec-candidate-A-draft.md`(명세서 raw draft)에 대한 외부 검토 의견을 정리하고, 어떤 의견을 명세서 본문에 즉시 반영했는지·어떤 의견을 변리사 컨설팅 단계로 위임했는지 매핑한 작업 계획서다. 변리사 컨설팅 시 명세서와 함께 입력 자료로 전달한다.
>
> **참조**: `docs/design/13-patent-strategy.md` §13.5 1순위 결합 청구항(D8-2), `docs/ip/spec-candidate-A-draft.md`

---

## 1. 검토 의견 총평

검토자는 본 명세서의 결합 발명 방향(ROS2 로봇 플릿 보안 감사 + 하드웨어 식별자 결합 + WASM 격리 평가 + 다중 해시 evidence + 멀티테넌트 cross-witness + 오프라인 외부 검증 토큰)에 동의했다. 다만 다음 세 가지 보강이 필요하다고 지적했다.

1. 청구항 1이 너무 많은 핵심 요소를 한 번에 담고 있어 권리 범위가 좁아질 위험
2. "인터넷 연결 없이 검증"과 "토큰으로 엔트리 조회" 사이의 표현상 충돌
3. cross-witness fold-in의 구체적 검증 방식, 실패 조건, 보안 경계가 더 명확해야 함

추가로 명세서 본문 차원에서 canonicalization·redaction manifest·키 관리·검증 실패 유형·ROS2 특화성·도면 보강이 권장되었다.

## 2. 의견 분류와 처리 방침

| 영역 | 검토 의견 항목 | 처리 방침 |
|---|---|---|
| **명세서 본문 보강** | canonicalization 규칙, redaction manifest, append-only 구현 옵션, 키 관리·회전, 검증 실패 유형 enum, fold-in 알고리즘, 외부 anchoring, ROS2 특화 평가, ROS graph fingerprint, 도면 7 추가 | **본 라이팅 단계에서 명세서 본문에 즉시 반영** (실시례 보강 + 신규 실시례 7~10) |
| **표현 정합성** | "계정 없이 + 인터넷 없이"의 단계 분리 | **본 라이팅 단계에서 명세서 본문에 즉시 반영** |
| **진보성 논거** | 선행기술 대비 차이 표 + 핵심 진보성 문장 | **본 라이팅 단계에서 명세서 끝의 변리사 입력용 보조 정보에 추가** |
| **청구항 추상화** | "TPM EK + MAC + CPU serial" → "보안 모듈 식별 정보 + 장치 식별 정보", WASM → "격리 실행 환경" | **변리사 위임** — 다만 명세서 실시례 [0014-1], [0016-1]에 변형 가능성을 명시해 두어 변리사가 추상화할 때 근거 자료 확보 |
| **독립항 분할 vs 결합** | 5개 독립항으로 분할할지, 1+4 종속으로 갈지 | **변리사 위임** — 본 문서 §5의 옵션 A·B·C로 정리해 입력 자료로 제공 |
| **방법 청구항 단계 분해** | 청구항 8을 10단계 내외로 명시적 분해 | **변리사 위임** — 명세서 raw draft 청구항 8에 단계 (i)~(xi)로 골격을 두고, 변리사가 단계별 분해 청구항으로 전개 |
| **차분 평가 재사용 조건** | 동일 policy pack version, evaluator version, 분할 규칙, redaction 규칙 등 충족 조건 명시 | **본 라이팅에서 청구항 4에 즉시 반영** |
| **PeerGroup 다수결 결정론** | "결정론적 비교" 강조, 통계적 anomaly detection과 구분 | **본 라이팅에서 청구항 5와 실시례 10에 즉시 반영** |

## 3. 명세서 본문 반영 매핑

### 3.1 표현 정합성 — "계정 없이 + 인터넷 없이"의 단계 분리

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| 검증 번들을 사전에 받은 뒤 인터넷 없이 재계산이라는 단계 명확화 | 기술 분야 / [0004] (마) / [0005] (e) / [0011] 5단계 / [0012] (마) / 청구항 2 / 실시례 6 단계 5·6 | 모든 등장 위치에서 "스코프가 한정된 검증 번들을 사전에 받은 뒤 별도 계정과 인터넷 연결 없이"로 일관 통일. 토큰 발급 → 검증 번들 생성 → 사전 전달 → 오프라인 검증의 4단계 흐름이 명세서 전반에 일관 적용되었다. |

### 3.2 fold-in 알고리즘 구체화

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| 다른 테넌트 checkpoint 선택 기준 | 실시례 4 [0020-1] (i) | "활성 상태이며 1개 이상 엔트리를 가진 다른 모든 테넌트, 직전 N(예: 24)시간 내 엔트리 추가 상태" |
| canonical 정렬 규칙 | 실시례 4 [0020-1] (ii)(iii) | tenantId 사전식 정렬 + RFC 8785 JCS 직렬화 |
| 검증 실패 조건 | 실시례 4 [0020-2] | 3단계 절차 + CROSS_WITNESS_MISMATCH / BUNDLE_INCOMPLETE / ENTRY_HASH_MISMATCH 매핑 |
| fold-in 대상 0~1개 처리 | 실시례 4 [0020-4] | 외부 anchoring을 반드시 동반 |
| 모든 테넌트 동시 재작성 방어 | 실시례 4 [0020-3] | 4가지 외부 anchoring 옵션 (TSA / 공개 transparency log / 외부 저장소 / webhook) |
| externalAnchor 데이터 구조 | 실시례 4 [0019] | AuditEntry에 `externalAnchor *ExternalAnchor` 필드 추가 |

### 3.3 redaction manifest 도입

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| redaction manifest 구조와 외부 검증 가능성 | 실시례 3 [0018-1], [0018-2] | RedactionManifestEntry 구조 (offset/length/secretType/placeholder/saltedDigest/redactionRuleVersion) + RedactionManifest 메타. 원문 비밀 값 미포함, HMAC-SHA256 기반 saltedDigest로 외부 검증 가능. REDACTION_DIGEST_MISMATCH 오류 매핑 |
| 검증 번들에 manifest 포함 | 실시례 5 [0022] | VerificationBundle에 redactionManifest 필드 추가 |
| 발명 효과 (다)에 반영 | [0012] (다) | "외부 감사인이 원문 비밀 없이도 redaction의 정합성을 확인할 수 있다" |

### 3.4 append-only 구현 옵션 (실시례 7 신규)

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| WORM, DB trigger, object lock, HSM, 권한 분리, Merkle inclusion proof | 실시례 7 [0024], [0025] | 5가지 구현 옵션을 단독 또는 결합 적용 가능. 청구항 1과 분리해 종속항(또는 변리사 판단으로 독립항)으로 활용 가능 |

### 3.5 canonical 형식 (실시례 8 신규)

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| UTF-8, JSON canonicalization, timestamp 정규화, line ending, null 구분, 배열 정렬, 구분자 충돌 방지 | 실시례 8 [0026] (i)~(vii) | RFC 8785 JCS 명시 + 7개 규칙 항목. ENTRY_HASH_MISMATCH로 위반 판정 |

### 3.6 키 관리 (실시례 9 신규)

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| 테넌트·체크포인트·토큰 서명키 분리, 회전, 폐기, HSM/KMS, TPM 봉인 | 실시례 9 [0028]~[0031] | 키 역할 3종 + 회전 절차 3단계 + 검증 도구의 폐기 키 처리 + PUBLIC_KEY_REVOKED 오류 매핑 |
| 검증 번들에 publicKeyBundle 포함 (현행 + 폐기) | 실시례 5 [0022] VerificationBundle | publicKeyBundle 필드 |

### 3.7 검증 실패 유형 enum

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| 단일 OK/KO가 아닌 11종 실패 유형 | 실시례 5 [0022-2] | TOKEN_SIGNATURE_INVALID, TOKEN_EXPIRED, SCOPE_VIOLATION, ENTRY_HASH_MISMATCH, PREV_HASH_MISMATCH, CHECKPOINT_SIGNATURE_INVALID, CROSS_WITNESS_MISMATCH, ANCHOR_VERIFICATION_FAILED, PUBLIC_KEY_REVOKED, BUNDLE_INCOMPLETE, REDACTION_DIGEST_MISMATCH |

### 3.8 ROS2 특화 보강 (실시례 10 신규)

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| ROS_DOMAIN_ID, DDS Security, QoS profile, publisher/subscriber count, 비인가 node, service/action endpoint, namespace, SROS2 keystore/enclave/permissions.xml | 실시례 10 [0032] (i)~(viii) | 8개 ROS2 특화 평가 항목 |
| ROS graph fingerprint | 실시례 10 [0033] | computeRosGraphFingerprint 의사 코드 + canonical JSON 직렬화 |
| PeerGroup 결정론 비교 + 기준 비율 | 실시례 10 [0034], [0035] | "결정론적 비교"와 사전 정의된 기준 비율(예: 80%) 명시. 청구항 5와 정합 |
| 청구항 3 보강 | 청구항 3 | ROS_DOMAIN_ID, DDS Security 활성 상태, SROS2 enclave, permissions policy 추가 |

### 3.9 도면 추가

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| 도 7 — 검증 번들 구조도 | 도면의 간단한 설명 | 검증 토큰·엔트리·체크포인트·cross-witness·공개키 번들·redaction manifest·evidence 해시·외부 anchoring 증거가 한 번들로 묶이는 모습 |
| 도면 작도 우선순위에 도 7 추가 | 변리사 입력용 보조 정보 | 도 1 → 도 4 → 도 7 → 도 5 → 도 2 → 도 3 → 도 6 |

### 3.10 진보성 논거 표

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| 선행기술 대비 5종 비교 표 + 핵심 진보성 문장 | 변리사 입력용 보조 정보 | 5+1행 표(OpenSCAP·CloudTrail·CT/Rekor·QLDB/Fabric·OPA·WORM) + 핵심 진보성 문장 1단락 |

### 3.11 차분 평가 재사용 조건

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| 동일 policy pack version, evaluator version, 분할 규칙, redaction 규칙 등 조건 명시 | 청구항 4 | "동일 정책 팩 버전, 동일 평가기 버전, 동일 evidence 분할 규칙, 동일 redaction 규칙 버전 중 하나 이상의 조건이 충족되는 직전 세션과" |

### 3.12 외부 anchoring 청구항 (청구항 6 신규)

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| 외부 anchoring 자체를 별도 종속항으로 | 청구항 6 (신규) | "TSA, 공개 transparency log, 고객사 외부 저장소, 사전 등록된 webhook 수신처 중 하나 이상에 추가로 anchoring하는 외부 anchoring부" |

### 3.13 청구항 1 추상화 단서를 명세서에 심기

| 검토 의견 | 반영 위치 | 반영 내용 |
|---|---|---|
| 하드웨어 식별자 결합 함수 변형 가능성 | 실시례 1 [0014-1] | TPM Attestation Key, 메인보드 시리얼, BIOS UUID, 디바이스 인증서 fingerprint, SHA-3·BLAKE3·HMAC·디지털 서명 등을 변형으로 명시 |
| 격리 실행 환경 변형 가능성 | 실시례 2 [0016-1] | WASM 외에 컨테이너 샌드박스, eBPF 격리, 별도 프로세스 격리도 같은 성질을 갖추면 본 발명의 격리 실행 환경에 해당 |

이 두 보강은 변리사가 청구항 1을 추상화할 때 명세서가 그 추상화를 뒷받침하도록 하기 위한 사전 작업이다.

## 4. 변리사 컨설팅으로 위임된 항목

명세서 raw draft에 반영하지 않고, 권리 범위 결정은 변리사에게 위임한다.

| 항목 | 위임 이유 |
|---|---|
| 청구항 1 본문의 추상화 (예: "TPM EK + MAC + CPU serial" → "보안 모듈 식별 정보 + 장치 식별 정보") | 권리 범위 직접 결정. 명세서 raw draft에는 보수적으로 좁게 두고, 명세서 실시례에서 변형을 인정하는 단서만 심어두었음([0014-1], [0016-1]). |
| 독립항 분할 vs 결합 구조 결정 (옵션 A·B·C 중 선택) | 선행기술 조사 결과에 따라 진보성 논거 분산 위험을 평가해 결정. 본 문서 §5에서 옵션 비교 제공. |
| 방법 청구항(청구항 8) 단계별 분해 | 명세서 raw draft에서는 (i)~(xi) 골격만 두고, 변리사가 단계별 청구항으로 전개. |
| 종속항 다층화 전략 | 변리사 영역. |
| 청구항 표현의 정형 문체 다듬기 | KIPO 정형 문체. |

## 5. 청구 범위 옵션 비교 (변리사 입력용)

검토 의견에서 권한 분할 구조가 거론되었으므로 세 가지 옵션을 비교 제공한다. 변리사가 선행기술 조사 결과를 본 뒤 최종 선택한다.

### 옵션 A — 결합 청구항 단일 (현재 raw draft)

```
청구항 1: 하드웨어 식별자 + WASM 격리 + 다중 해시 + cross-witness 결합
청구항 2~6: 각 요소를 종속항으로 구체화·확장
청구항 7: 매체 청구항
청구항 8: 방법 청구항
```

장점: 결합 신규성을 그대로 청구. 진보성 논거 집중.
단점: 권리 범위가 좁음. 회피 설계 가능성.

### 옵션 B — 5개 독립항 병렬

```
청구항 1: 하드웨어 식별자 결합 + 감사 체인 (가장 추상화된 골격)
청구항 2: WASM 격리 평가 (또는 격리 실행 환경)
청구항 3: 다중 해시 + 차분 평가
청구항 4: 멀티테넌트 cross-witness + 외부 anchoring
청구항 5: 오프라인 검증 번들
청구항 6~: 매체·방법
```

장점: 권리 범위 최대. 어느 한 요소만 사용해도 침해 주장 가능.
단점: 진보성 논거가 분산되어 일부 독립항이 거절될 위험. 출원료·심사료 증가.

### 옵션 C — 1+4 종속 구조

```
청구항 1: 하드웨어 식별자 + 감사 체인 (가장 추상화된 골격)
청구항 2: 제1항 + WASM 격리 종속
청구항 3: 제1항 + 다중 해시 + 차분 종속
청구항 4: 제1항 + cross-witness + 외부 anchoring 종속
청구항 5: 제1항 + 오프라인 검증 번들 종속
청구항 6~: 매체·방법
```

장점: 청구항 1의 진보성 논거가 모든 종속항에 상속. 회피 시 청구항 1로 대응.
단점: 청구항 1의 추상화 수준이 핵심. 너무 넓으면 선행기술과 충돌, 너무 좁으면 옵션 A와 동등.

### 권고
변리사 선행기술 조사 결과에 따라 결정. 본 명세서 raw draft는 옵션 A 형태이지만, **옵션 C로의 전환을 가장 우선 검토하길 권한다**. 옵션 B는 진보성 논거 분산 위험이 커 본 발명의 결합 신규성에 적합하지 않을 수 있다.

## 6. 추가 보완 메모

### 6.1 redaction의 검증 가능성 강화 옵션
실시례 3에 manifest를 도입했지만, 다음 보강도 변리사 검토 시 함께 다룰 수 있다.

- 원문 비공개 저장: 비밀 자체는 별도 vault에 keyed digest로만 저장.
- redaction rule 자체의 버전 서명: redaction 규칙 변경 이력을 audit chain에 남기기.
- redaction 전 원문 hash와 redaction 후 hash 분리 보관: 외부 감사인은 redaction 후만 받지만, 운영자 내부 검증은 둘 다 사용.

### 6.2 도 7의 작도 시 강조점
검증 번들 구조도(도 7)에는 다음 요소가 명확히 표시되어야 한다.

- 검증 토큰
- 스코프 한정 감사 엔트리
- 참조되는 테넌트 체크포인트
- crossWitness 참조와 다른 테넌트의 체크포인트
- 공개키 번들 (현행 키 + 폐기 키)
- redaction manifest
- evidence 해시 집합
- 외부 anchoring 증거 (TSA 응답·CT inclusion proof 등)
- 검증 결과 (FailureCode 11종 중 하나)

이 도면이 있으면 "오프라인 검증" 구조의 자급성이 한눈에 드러난다.

### 6.3 출원 타이밍과 R&D 일정의 동기
본 명세서는 명세서 raw draft 단계이며, 변리사 컨설팅 → 청구 범위 확정 → 선행기술 조사 → KR 출원 순서로 진행된다(D8-5). 출원 전에는 GitHub public 전환·LICENSE 추가·외부 공개 모두 금지된다(D8-4).

명세서가 가리키는 enterprise build tag 골격(`internal/enterprise/{crosswitness,selectdisclose,multihash,wasmrt,robotid,rostopo,fleetxval}`)은 Phase 4에 도입 예정이며, 그 시점에 본 명세서의 실시례 4·5·7·8·9·10이 실제 코드로 구현된다.

---

## 7. 변경 이력

- **2026-05-08 · v1**: 외부 검토 의견을 받아 본 문서 초안 작성. 명세서 본문 보강(실시례 7~10 신설 + 표현 정합성 + 진보성 논거 표)과 변리사 위임 항목을 분리하여 정리.
