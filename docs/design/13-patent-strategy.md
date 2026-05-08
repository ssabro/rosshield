# 13. 특허 전략 (Patent Strategy)

> 본 문서는 설계서 본체와는 다른 결의 문서입니다. **사업·IP 전략 + 설계 보강안 + 출원 워크플로**를 한곳에 모아 변리사 협업과 코드 의사결정의 단일 진실 원천으로 둡니다.

## 13.1 목적과 범위

1. 본 제품에서 **특허로 보호 가능한 기술 후보**를 식별·우선순위화한다.
2. 특허 가치를 끌어올리기 위해 **설계·구현에 어떤 보강을 어디에 반영할지**를 명세한다.
3. **Open-core(D5) 결정**과 **GitHub private 유지(D6)** 의 제약 안에서 청구 범위가 OSS 특허 grant로 무력화되지 않도록 **청구 분배 원칙**을 정한다.
4. KR 우선출원 → PCT/해외 진입의 **출원 워크플로**와 **출원 전 잠금 사항**을 둔다.

## 13.2 결정 요약 (2026-05-08, D8)

| 결정 | 내용 |
|---|---|
| **D8-1 보강안 채택 범위** | 후보 A 강화 + 후보 B 강화 + 후보 C 강화 + **후보 D 전체 신규** + 결합 청구항. 후보 E는 Phase 4 어플라이언스 작업 시 함께 검토(보류). |
| **D8-2 1순위 결합 청구항** | A-1(cross-witness) + B-1(multi-hash evidence) + C-1(WASM evaluator) + D-3(robot identity binding) — "ROS2 로봇 플릿의 보안 상태를 하드웨어 ID에 묶어 변조 불가하게 외부 검증 가능한 멀티테넌트 시스템" |
| **D8-3 enterprise build tag 가속** | D5 결정 원안은 "첫 paying customer 직전" 분리. **특허 출원 명세서와 동기화하기 위해 Phase 2 후반 또는 Phase 3 진입 시 build tag 골격 선반영**. 실제 enterprise-only 코드 분리는 D5 원안대로 진행하되, 빌드 시스템·디렉터리·import 가드는 미리 구축. |
| **D8-4 출원 전 잠금** | KR 우선출원 완료 전까지 다음 금지: (a) GitHub repo public 전환, (b) OSS 라이선스 적용·LICENSE 파일 추가, (c) 외부 컨퍼런스·블로그·논문 공개, (d) 비공개 PoC라도 NDA 없는 잠재 고객 제공. **D6(private 유지)** 와 **D5(라이선스 보류)** 결정이 이미 이 잠금에 부합. |
| **D8-5 출원 단계** | (1) 본 문서 + 후보 A 명세서 초안 완성 → (2) 변리사 컨설팅으로 청구 범위·진보성 확정 → (3) 선행기술 조사 위임 → (4) KR 출원 → (5) 12개월 내 PCT 또는 해외 직접 진입 평가. |

## 13.3 Open-core 제약과 청구 분배 원칙

D5(Apache-2.0 코어 + enterprise 별 라이선스)에서 **Apache-2.0 §3 patent grant**는 OSS 사용자에게 출원된 특허의 무상 사용권을 자동 부여합니다. 즉 **코어에 들어간 알고리즘은 특허로 출원해도 OSS 사용자가 합법적으로 그대로 쓸 수 있습니다**. 실질 방어가 가능한 영역은 **enterprise build tag 뒤로 가는 코드**입니다.

### 청구 분배 표

| 청구 요소 | 둘 곳 | 이유 |
|---|---|---|
| 해시 체인 기본 연산자 (`hash_i = sha256(prev ‖ digest ‖ meta)`) | 코어 (Apache-2.0) | 외부 감사인 verify CLI(E30)와 호환되어야 함. 표준 인터페이스. |
| `verify` CLI 본체 | 코어 (Apache-2.0) | 감사인 무인증 검증이 P1(외부 검증 가능성) 핵심 가치. OSS여야 신뢰. |
| **Cross-witness fold-in (A-1)** | enterprise | 단일 인스턴스 운영자가 단일 테넌트 체인을 위조해도 상호 증인이 됨. 차별화 핵심. |
| **Selective disclosure verification token (A-3)** | enterprise | 토큰에 액션 카테고리 스코프 박는 capability 모델. |
| **Multi-hash evidence + 차분 신뢰도 (B-1, B-2)** | enterprise | 운영 효율 차별화. 대규모 플릿에서만 가치 발생. |
| **WASM sandboxed evaluator (C-1)** | 인터페이스는 코어, **호스트 격리·서명 검증 정책은 enterprise** | 팩 생태계 호환을 위해 인터페이스는 OSS, 보안 격리·증명 정책은 enterprise. |
| **Robot identity binding (D-3, TPM EK + MAC + CPU serial fingerprint)** | enterprise | 어플라이언스·고가치 환경 차별. |
| **ROS2 graph topology audit (D-1)** | 인터페이스는 코어 plugin check type, **고급 그래프 평가 알고리즘은 enterprise** | OSS 생태계 확장과 enterprise 차별의 양립. |
| **Fleet cross-validation (D-2, peer evidence hash 일치 자동 탐지)** | enterprise | 결정론적 anomaly detection. |
| **n-of-m air-gap update (E-1, 보류)** | enterprise (Phase 4) | 어플라이언스 사회공학 방어. |

### 빌드 시스템 골격 (D8-3 가속 결과)

`internal/` 하위에 다음 디렉터리 컨벤션을 Phase 2 후반에 도입한다:

```
internal/
  enterprise/        // build tag: rosshield_enterprise
    crosswitness/    // A-1
    selectdisclose/  // A-3
    multihash/       // B-1, B-2
    wasmrt/          // C-1 host
    robotid/         // D-3
    rostopo/         // D-1 advanced
    fleetxval/       // D-2
  ...
```

각 enterprise 패키지는 빌드 태그 `//go:build rosshield_enterprise` 보호. 코어 빌드는 인터페이스만 import, enterprise 빌드는 구현 결선. 라이선스 텍스트도 디렉터리별 LICENSE 파일로 명시(`internal/enterprise/LICENSE.enterprise`).

## 13.4 후보 보강안 — 적용 위치와 검증 기준

### 후보 A — 감사 체인 강화 (설계서 §10 보강)

| ID | 내용 | 적용 위치 | 검증 기준 |
|---|---|---|---|
| **A-1** | 테넌트 간 cross-witness — 다른 테넌트 checkpoint hash를 자기 체인에 fold-in | `10-audit-and-observability.md` §10.4·10.5 신설 항목 + `internal/enterprise/crosswitness/` | 단일 인스턴스 위조 공격 시뮬레이션에서 다른 테넌트가 위조 검출 |
| **A-2** | TPM/HSM 봉인 키로 checkpoint 서명 | §10.5 보강 + 어플라이언스 결선 | 키 추출 공격 시 서명 자동 무효화 |
| **A-3** | Verification token의 selective disclosure (액션 카테고리 스코프) | §10.6 보강 + `internal/enterprise/selectdisclose/` | 스코프 외 entry 조회 시 403 |
| **A-4** | 체인 단절 즉시 외부 witness 통보 + audit entry로 흡수 | §10.8 보강 | 백업 복원·DB 직접 조작 시뮬레이션에서 즉시 감지 |

### 후보 B — 차분 스캔 강화 (설계서 §7 보강)

| ID | 내용 | 적용 위치 | 검증 기준 |
|---|---|---|---|
| **B-1** | Multi-hash evidence — 전체 sha256 + JSONPath/line 단위 sub-hash | `07-scan-engine-and-benchmarks.md` §7.8·7.9 보강 + `internal/enterprise/multihash/` | 부분 변경 시 변경 영역만 재평가됨 |
| **B-2** | 차분 신뢰도 점수 — 연속 동일 → 신뢰도↑, 변경 → 다음 K회 강제 전체 평가 | §7.9 보강 | 신뢰도 0.95 이상에서 전체 평가 비율 5% 이하 |
| **B-3** | `scan.reuse` audit entry — 재사용 시 원본 세션 ref를 chain entry로 명시 | §7.9 + §10.2 추가 카테고리 | 재사용 결과를 verify CLI가 원본까지 따라감 |

### 후보 C — 정책 팩 강화 (설계서 §7 보강)

| ID | 내용 | 적용 위치 | 검증 기준 |
|---|---|---|---|
| **C-1** | WASM sandboxed evaluator를 v1로 끌어올림 (현재 v2) | §7.3 plugin check type 1순위 + `internal/enterprise/wasmrt/` | 악성 평가 모듈의 호스트 escape 차단(WASM CFI) |
| **C-2** | SLSA-style provenance + self-test 결합 서명 | §7.5 보강 | 빌드 환경 attestation 변조 시 설치 거부 |
| **C-3** | Plugin도 fixture 동봉 의무화 | §7.3·7.6 강제력 추가 | fixture 없는 plugin 설치 시 거부 |

### 🆕 후보 D — ROS2 도메인 특화 (설계서 §7·§8 신설)

이번 결정의 핵심. 일반 Linux 보안 감사 도구와의 결정적 차별 영역.

| ID | 내용 | 적용 위치 | 검증 기준 |
|---|---|---|---|
| **D-1** | ROS2 토폴로지 기반 평가 — 토픽 QoS·publisher/subscription count·암호화 그래프 구조 | `07-*` §7.3 plugin check type 1순위 + `internal/enterprise/rostopo/` | 동일 OS 동일 ROS distro에서 토폴로지 차이 검출 |
| **D-2** | 로봇 플릿 cross-validation — 같은 PeerGroup의 evidence 해시 일치 여부로 outlier 결정론적 탐지 | `08-*` §8.4 anomaly 보강 + `internal/enterprise/fleetxval/` | 12대 중 1대만 다른 설정 → 5분 이내 자동 탐지 |
| **D-3** | Robot identity binding — 감사 결과에 robot 하드웨어 ID(TPM EK + MAC + CPU serial 결합 fingerprint) 결합 | `04-*` Robot 엔터티 + `07-*` §7.7 + `internal/enterprise/robotid/` | 다른 로봇이 결과 위조 시 fingerprint 불일치로 검출 |
| **D-4** | Plugin check type 1차 시민화 — `ros2_topic_audit`·`ros2_node_audit`·`ros2_service_audit` | `07-*` §7.3 보강(현재 v1.1+ → v1) | 코어 팩에 최소 3종 plugin check 포함하여 출시 |

### 후보 E — 에어갭·다중 서명 (Phase 4 검토)

| ID | 내용 | 비고 |
|---|---|---|
| E-1 | n-of-m 다중 admin 서명으로 USB 반입 팩 설치 | Phase 4 어플라이언스 진입 시 검토 |
| E-2 | 시간 윈도우 + nonce — 동일 팩 재사용 공격 차단 | Phase 4 |
| E-3 | 에어갭 모드 텔레메트리 함수 빌드 타임 noop attestation | Phase 4 |

## 13.5 1순위 결합 청구항 (D8-2)

> **시스템 청구항 초안** (변리사 손보기 전 raw 형태):
>
> ROS2 로봇 플릿의 보안 상태를 외부 감사인이 검증할 수 있는 시스템으로서, 다음을 포함한다:
>
> (a) 각 로봇의 하드웨어 식별자(TPM EK 증명서·MAC 주소·CPU 시리얼의 결합 해시)를 결정론적으로 산출하는 **로봇 식별 결합부(D-3)**,
>
> (b) 평가 규칙을 격리된 WebAssembly 런타임에서 실행하고 결과를 (a)의 식별자에 결합하는 **격리 평가 실행부(C-1)**,
>
> (c) 평가 결과의 증거 출력에 대해 전체 해시 + 의미 단위 부분 해시(JSONPath/라인) 다중 해시를 산출하는 **다중 해시 증거 저장부(B-1)**,
>
> (d) 테넌트별 단조 증가 시퀀스의 append-only 감사 체인에 (a)·(b)·(c)를 기록하고, 다른 테넌트의 checkpoint 해시를 자기 체인에 fold-in하여 단일 운영자에 의한 위조를 상호 증명하는 **상호 증인 감사 체인부(A-1)**,
>
> (e) 외부 감사인에게 발급되는 스코프·만료 한정 검증 토큰으로 (d)의 일부를 무인증·무계정으로 재검증할 수 있는 **선택적 공개 검증부(A-3, 선택적 종속항)**.

이 청구항은 단일 요소(해시 체인·WASM·content-addressable storage)는 모두 선행기술이 있지만 **결합과 도메인(ROS2 로봇 플릿 + 하드웨어 결합 + 멀티테넌트 cross-witness)** 의 비자명성을 노립니다.

## 13.6 출원 워크플로

### 단계

1. **본 문서 + 후보 A 명세서 초안** (현재 단계)
2. **변리사 컨설팅** — 청구항 1~N항 설계, 진보성 논거 수립, 도면 설계 보조 (2~4주)
3. **선행기술 조사** — 변리사 또는 전문 조사기관 위임 (2~4주, 비용 별도)
4. **명세서 최종화** — 도면 + 실시례 + 발명 효과 + 청구항 (2주)
5. **KR 출원** — KIPO e-출원 (출원료 기본 약 46,000원 + 변리사 수수료 ~300만원/건)
6. **공개 잠금 해제 가능 시점** — 출원일 익일부터 신규성 상실 위험 없음. 단 해외 진입 위해서는 **출원일로부터 12개월 내 PCT 출원** 또는 **개별 국가 직접 출원**.
7. **PCT/해외 진입 평가** — 미국·유럽·일본 우선. 국가당 추가 비용 수백만~천만 원대.

### 출원 전 잠금 사항 (D8-4 재명시)

- ❌ GitHub repo public 전환 금지
- ❌ LICENSE 파일 추가 금지 (Apache-2.0 헤더도)
- ❌ 외부 컨퍼런스·블로그·SNS·논문 공개 금지
- ❌ NDA 없는 잠재 고객·파트너에게 데모 금지 (UI 시연은 가능하되 본 문서 후보 A·D-3 알고리즘 노출 금지)
- ✅ 비공개 GitHub repo 내부 commit·CI 동작은 OK (자기 공개 아님)
- ✅ NDA 체결 잠재 고객에게 PoC 가능

## 13.7 설계서 본체 갱신 후속 (별도 단계)

본 문서가 단일 진실 원천이므로 **본체 설계서는 한 줄 hook + 본 문서 참조**만으로 충분. 단 1순위 결합 청구항이 의존하는 4개 설계서는 명세서 작성 시점에 함께 갱신 필요:

- `04-domain-and-data-model.md` Robot 엔터티에 `hardwareFingerprint` 필드 추가 (D-3)
- `07-scan-engine-and-benchmarks.md` §7.3 plugin check type을 v1로 승격, §7.8 multi-hash, §7.9 차분 신뢰도 (B-1·B-2·C-1·D-4)
- `08-intelligence-and-compliance.md` §8.4 anomaly에 fleet cross-validation 결정론 변종 추가 (D-2)
- `10-audit-and-observability.md` §10.4 cross-witness fold-in, §10.5 TPM 봉인, §10.6 selective disclosure (A-1·A-2·A-3)
- `11-tech-stack-and-roadmap.md` Phase 2~3에 enterprise build tag 골격 항목, §11.16 D8 기록

이 갱신은 **변리사 컨설팅 결과로 청구 범위가 확정된 직후** 수행. 그 전에 본체 설계서를 미리 흩뜨리면 청구항 변경 시 동기 비용 발생.

## 13.8 리스크 (Patent-specific)

| # | 리스크 | 가능성 | 영향 | 완화 |
|---|---|:---:|:---:|---|
| P1 | 선행기술 강함(Sigstore Rekor·CT·OpenSCAP)으로 핵심 청구항 거절 | 중 | 고 | 결합 청구항 + 후보 D 도메인 특화로 진보성 보강 |
| P2 | Apache-2.0 코어로 출원된 특허 grant 발생, 경쟁자가 OSS 그대로 사용 | 고 | 중 | 청구 분배 원칙으로 enterprise tag 코드에만 핵심 알고리즘 배치 |
| P3 | 자기 공개로 신규성 상실 | 저 | 치명 | D8-4 잠금. CI·CD 외부 노출 금지 |
| P4 | 변리사·출원 비용 부담 | 중 | 중 | 1순위 결합 청구항 1건 우선, 후속은 자금 상황 보고 결정 |
| P5 | 청구항 회피 설계로 경쟁자 우회 | 중 | 중 | 종속항 다층화, 컨텐츠 팩·Self-Test fixture로 운영 해자 보강 |
| P6 | 한국·미국 SW 특허 적격성 거절 | 중 | 중 | 시스템·방법 청구항 + 컴퓨터 프로그램이 저장된 매체 청구항 병행 |

## 13.9 Non-goals (특허 측면)

1. **방어적 특허 다량 출원하지 않는다** — 자금·관리 부담 대비 가치 낮음. 1~3건 핵심에 집중.
2. **특허 라이선싱 사업화하지 않는다** — 본 제품 보호용 (defensive use). 패밀리 내 cross-license는 케이스별.
3. **표준 필수 특허(SEP) 등재 시도하지 않는다** — ROS2·CIS Benchmark 표준화에 깊이 관여하지 않음.

## 13.10 결정 로그 (특허 전용)

- **2026-05-08 · D8 — 특허 전략 수립**: 본 문서 신설. 후보 D(ROS2 도메인 특화) 전체 신규 채택, 후보 A·B·C는 강화안 채택, 후보 E는 Phase 4 보류. 1순위 결합 청구항(A-1+B-1+C-1+D-3) 확정. enterprise build tag 골격을 Phase 2 후반/Phase 3 진입 시 가속 도입(D5 원안 대비 가속). 출원 전 GitHub public 전환·OSS 라이선스 적용 잠금. 다음 단계: 후보 A 명세서 초안 → 변리사 컨설팅.
