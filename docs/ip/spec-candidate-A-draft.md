# 명세서 초안 — 후보 A 결합 청구항

> **이 문서의 위치**: 본 명세서는 변리사 컨설팅에 넘기기 위한 raw draft다. KIPO 명세서 양식을 따르되 청구항·도면·심사 응답은 변리사가 최종 확정한다. 본 문서를 그대로 출원하지 말 것.
>
> **참조**: `docs/design/13-patent-strategy.md` §13.5 1순위 결합 청구항(D8-2). 외부 검토 의견과 반영 매핑은 `docs/ip/spec-A-review-and-revision-plan.md` 참조.

---

## 발명의 명칭

ROS2 로봇 플릿의 보안 감사 결과를 하드웨어 식별자와 결합하고, 멀티테넌트 상호 증인 해시 체인을 통해 외부에서 검증할 수 있도록 하는 시스템 및 방법
(System and Method for Verifying Security Audit Results of ROS2 Robot Fleets Bound to Hardware Identifiers via Multi-Tenant Cross-Witness Hash Chain)

## 기술 분야

본 발명은 ROS2(Robot Operating System 2) 기반 로봇 플릿의 보안 구성 상태를 정해진 절차에 따라 결정론적으로 감사하고, 그 결과의 무결성을 외부 감사인이 스코프가 한정된 검증 번들을 사전에 받은 뒤 별도의 계정이나 인터넷 연결 없이도 다시 확인할 수 있게 해 주는 시스템 및 방법에 관한 것이다.

## 용어의 정의

본 명세서에서 사용하는 주요 용어는 아래와 같이 정의한다. 이하 정의는 청구항을 해석할 때 본 명세서가 정한 의미에 따른다.

### 시스템 모델

- **로봇 플릿(Robot Fleet)**: 동일한 운영 주체가 관리하는 복수의 로봇 집합. 본 명세서에서는 ROS2 기반 로봇이 N대 단위로 묶여 같은 보안 정책 아래 운영되는 단위를 가리킨다.

- **멀티테넌트(Multi-tenant)**: 하나의 시스템 인스턴스가 복수의 고객 조직을 데이터와 권한 측면에서 격리하여 동시에 서비스하는 구조. 본 명세서에서 "테넌트"는 그러한 격리 단위 하나를 의미한다.

- **PeerGroup**: 같은 OS 배포판, 같은 ROS distribution, 같은 운영 역할 등으로 묶인 비교 가능한 로봇의 집합. 본 명세서에서는 (osDistro, rosDistro, role) 튜플을 기본 분류 키로 사용한다.

- **스코프(scope)**: 토큰 또는 권한이 적용되는 범위. 본 명세서의 검증 토큰은 시간 범위·액션 카테고리·만료 시각으로 스코프를 한정한다.

- **검증 번들(Verification Bundle)**: 검증 토큰의 스코프에 맞춰 운영자가 사전에 생성하여 외부 감사인에게 전달하는 자급형 데이터 묶음. 토큰 자체, 스코프 안의 감사 엔트리, 참조되는 체크포인트, 공개키 번들, redaction manifest, evidence 해시 집합을 포함한다.

### 하드웨어 신뢰 루트

- **TPM(Trusted Platform Module)**: 호스트 시스템과 분리된 보안 칩. 키 봉인, 디바이스 증명, 암호 연산을 호스트 운영체제와 격리된 영역에서 수행한다.

- **Endorsement Key(EK)**: TPM 제조 시점에 봉인되어 사후에 변경할 수 없는 고유 키 쌍. 인증서 형태로 발급되며, 디바이스 무결성 증명의 신뢰 루트(root of trust)로 사용된다.

### 알고리즘 성질

- **결정론적(deterministic)**: 같은 입력에 대해 시점, 실행 환경, 실행 횟수와 관계없이 항상 같은 결과를 내는 성질. 본 명세서에서는 해시 산출, redaction 처리, 식별자 결합에 적용된다.

- **append-only**: 데이터를 추가만 할 수 있고 수정이나 삭제가 불가능한 저장 모델. 본 명세서의 감사 엔트리는 모두 이 모델을 따른다.

- **redaction**: 출력에 포함된 비밀 정보(예: 비밀번호, 개인키, API 키)를 식별해 가리거나 다른 토큰으로 치환하는 처리. 본 명세서에서는 같은 입력에 대해 같은 위치와 같은 토큰으로 치환되는 결정론적 redaction을 사용한다.

### 데이터 구조

- **체크포인트(checkpoint)**: 해시 체인의 특정 시점 상태를 외부에서 인증할 수 있도록 서명된 스냅샷. 본 명세서에서는 (tenantId, seq, hash, signedAt, signature) 구조의 엔트리를 가리킨다.

- **fold-in**: 복수의 출처에서 나온 해시 값을 하나의 새 엔트리에 함께 담아, 특정 출처가 사후에 변조되었을 때 다른 출처가 변조 사실을 증명하도록 하는 결합 기법. 본 명세서에서는 다른 테넌트의 checkpoint 해시를 자기 체인의 새 엔트리에 fold-in한다.

- **외부 anchoring(External Anchoring)**: 자기 체인의 checkpoint 해시를 외부 신뢰점(타임스탬프 권한자, 공개 transparency log, 고객사의 외부 저장소, 사전 등록된 webhook 수신처 등)에 추가로 기록하는 보강 기법. 운영자가 자신이 통제하는 모든 테넌트 체인을 동시에 사후 재작성하는 시나리오에 대한 방어 수단으로 사용된다.

- **JSONPath**: JSON 문서에서 특정 경로를 선택하기 위한 표현식 언어(예: `$.status.code`). XPath의 JSON 변형에 해당한다.

### 실행 환경

- **WebAssembly(WASM)**: 격리된 가상 머신 환경에서 실행되는 이식 가능한 바이트코드 형식. 호스트 시스템 콜에 대한 접근을 화이트리스트 단위로 제한할 수 있어, 신뢰할 수 없는 코드를 격리해 실행하는 데 적합하다.

### 비교 대상이 되는 선행 시스템

- **Sigstore Rekor**: Linux Foundation의 Sigstore 프로젝트가 운영하는 공개 변조 방지 로그(transparency log). 소프트웨어 아티팩트의 서명 기록을 append-only로 보관한다. 본 명세서에서는 멀티테넌트 격리를 전제하지 않는 단일 글로벌 로그의 대표 예로 인용한다.

- **Certificate Transparency(CT)**: TLS 인증서 발급을 공개 로그에 기록하여 부정 발급을 탐지할 수 있게 하는 표준(RFC 6962). 본 명세서에서는 글로벌 단일 로그 모델의 또 다른 대표 예로 인용한다.

## 발명의 배경이 되는 기술

[0001] 산업 자동화·물류·의료·방산 분야에서 ROS2 기반 로봇 플릿의 도입이 빠르게 확산되고 있다. 이에 따라 해당 플릿이 CIS Benchmark, ISMS-P, IEC 62443 등 정해진 보안 기준을 꾸준히 지키고 있음을 감사인과 규제기관에 증명해야 할 필요가 점점 커지고 있다.

[0002] 종래의 보안 감사 도구(OpenSCAP, CIS-CAT 등)는 단일 호스트 또는 일반 IT 자산을 중심으로 설계되어 있어 다음과 같은 한계가 있다.

(1) 감사 결과를 특정 하드웨어 식별자에 묶어두지 않기 때문에 결과를 위조할 여지가 남는다.

(2) 단일 운영자 환경에서 무결성을 보장하는 해시 체인은, 정작 운영자 본인이 사후에 위조하는 경우는 막지 못한다.

(3) 외부 감사인이 결과를 다시 검증하려면 운영자의 계정이나 API 접근 권한이 필요하므로 독립적으로 검증하기가 어렵다.

(4) 평가 규칙이 호스트 코드와 같은 권한으로 실행되기 때문에, 악의적인 정책 팩이 호스트를 침해할 위험이 있다.

(5) ROS2 토픽·노드·QoS·DDS Security 설정처럼 도메인 특화된 보안 표면은 다루지 못한다.

[0003] 한편 Sigstore Rekor, Certificate Transparency 같은 변조 방지 로그 시스템은 전 세계가 공유하는 단일 로그를 전제로 설계되어 있어, 다중 테넌트로 격리된 환경에 그대로 적용하기 어렵다. 또한 외부 감사인이 스코프와 만료가 한정된 토큰으로 검증 번들을 사전에 받은 뒤 계정 없이 검증하는 흐름도 지원하지 않는다.

## 해결하려는 과제

[0004] 본 발명이 풀고자 하는 과제는 다음과 같다.

(가) ROS2 로봇 플릿의 보안 감사 결과를 해당 로봇의 하드웨어 식별자에 정해진 방식으로 묶어, 다른 로봇이 결과를 위조할 수 없도록 한다.

(나) 평가 규칙을 격리된 실행 환경에서 돌려, 정책 팩이 호스트의 권한을 침해하는 일을 막으면서도 하나의 인터페이스로 여러 평가 규칙을 받아들인다.

(다) 감사 결과의 증거 출력에 대해 전체와 부분 단위 해시를 함께 산출하여, 일부만 바뀌었을 때 바뀐 영역만 다시 평가하면서도 감사 무결성을 그대로 유지한다.

(라) 다중 테넌트 환경에서 운영자가 한 테넌트의 체인을 위조하더라도, 다른 테넌트가 증인이 되어 그 위조 사실을 잡아낼 수 있도록 한다. 운영자가 모든 테넌트를 동시에 통제하는 시나리오에 대해서도 외부 anchoring으로 보강한다.

(마) 외부 감사인이 스코프가 한정된 검증 번들을 사전에 받은 뒤, 운영자의 계정이나 인터넷 연결 없이도 감사 결과의 무결성을 다시 확인할 수 있도록 한다.

## 과제의 해결 수단

[0005] 본 발명의 시스템은 다음 구성을 포함한다.

(a) **로봇 식별 결합부**는 각 로봇의 보안 모듈로부터 유래한 암호학적 식별 정보(예: TPM Endorsement Key 인증서 또는 그 공개키)와 하나 이상의 장치 식별 정보(예: 네트워크 인터페이스 MAC 주소, CPU 시리얼 번호, 메인보드 시리얼 번호)를 입력으로 받는다. 이를 정규화한 뒤 결합 함수 H_id = SHA-256(TPM_EK_pubkey ‖ sorted(MACs) ‖ CPU_serial)에 통과시켜 하드웨어 식별자를 만든다. 결합 함수와 입력 항목의 구체적 선택은 실시례에서 한정한다.

(b) **격리 평가 실행부**는 정책 팩에 들어 있는 평가 규칙을 격리된 실행 환경에서 실행한다. 본 명세서의 대표 실시례는 WebAssembly(WASM) 런타임이며, 이때 호스트 시스템 콜에 대한 접근은 화이트리스트(예: includes/match/length/trim/json.parse/semver.gte)로 제한하고, 평가 결과는 (a)에서 만든 식별자와 함께 출력한다.

(c) **다중 해시 증거 저장부**는 평가 결과의 표준 출력(stdout), 표준 오류(stderr), 종료 코드를 입력으로 받아 두 가지 해시를 산출한다. 첫째는 전체에 대한 sha256 해시이고, 둘째는 라인 또는 JSONPath 표현식으로 지정된 의미 단위별 부분 해시 집합이다. redaction을 거친 출력의 해시가 흔들리지 않도록, 같은 입력에는 같은 위치·같은 토큰으로 치환되는 결정론적 redaction을 적용한다. 또한 redaction manifest를 함께 보관하여, 외부 감사인이 원문 비밀을 복원하지 않고도 redaction이 정합하게 이루어졌는지 검증할 수 있다.

(d) **상호 증인 감사 체인부**는 테넌트마다 시퀀스가 단조 증가하는 append-only 감사 엔트리를 hash_i = SHA-256(hash_{i-1} ‖ payloadDigest ‖ canonical meta) 식으로 연결한다. 정해진 주기 또는 이벤트 시점이 되면, 다른 테넌트들의 최신 checkpoint 해시 집합을 자기 체인의 새 엔트리에 fold-in한다. 이렇게 해 두면 운영자가 한 테넌트의 체인을 사후에 위조하더라도, 다른 테넌트가 가진 fold-in 엔트리의 checkpoint hash와 어긋나면서 위조 사실이 그대로 드러난다. 운영자가 자신이 통제하는 모든 테넌트 체인을 동시에 사후 재작성하는 시나리오에 대해서는 외부 anchoring(타임스탬프 권한자, 공개 transparency log, 고객사 외부 저장소, 감사인 webhook 수신처 등) 중 하나 이상을 추가로 적용한다.

(e) **선택적 공개 검증부**는 외부 감사인에게 발급되는 검증 토큰에 (i) 시간 범위 스코프, (ii) 액션 카테고리 스코프(예: scan·report만 허용하고 사용자 관리는 비공개), (iii) 만료 시각을 담는다. 검증 토큰에 기초하여 스코프가 한정된 검증 번들(엔트리, 체크포인트, 공개키 번들, redaction manifest, evidence 해시 집합 포함)이 운영자에 의해 사전에 생성되어 외부 감사인에게 전달된다. 검증 번들을 받은 감사인은 별도 계정과 인터넷 연결 없이도 외부 검증 도구로 (d)의 무결성을 직접 다시 계산할 수 있다.

[0006] 본 발명의 방법은 (a)~(e)를 다음 순서로 수행한다.

[0007] 1단계: 로봇을 등록할 때 (a)로 하드웨어 식별자 H_id를 산출하여 로봇 엔터티에 함께 저장한다.

[0008] 2단계: 정책 팩을 설치할 때 팩의 서명을 검증하고, (b)의 격리 실행 환경(예: WASM 런타임)에 평가 규칙을 로드한다.

[0009] 3단계: 감사 세션이 시작되면 체크마다 SSH 명령을 실행하고, (c)로 다중 해시 증거와 redaction manifest를 함께 저장한 뒤 (b)에서 격리 평가를 수행한다. 마지막으로 결과를 H_id와 결합한 페이로드를 만든다.

[0010] 4단계: 세션 결과를 (d)의 감사 체인에 추가한다. fold-in 주기가 되면, 다른 테넌트의 checkpoint 해시 집합을 담은 cross-witness 엔트리와 외부 anchoring 정보를 함께 추가한다.

[0011] 5단계: 외부 감사인이 검증할 때는 운영자가 (e)의 검증 토큰에 기초해 사전에 생성한 스코프 한정 검증 번들을 이동식 저장매체 또는 사전 협의된 채널로 전달받는다. 그 후 별도의 OSS 검증 도구로 체인 재계산, fold-in 상호 증인 검증, 외부 anchoring 검증, 공개키 서명 검증, redaction manifest 검증을 인터넷 연결 없이 수행한다.

## 발명의 효과

[0012] 본 발명은 다음과 같은 효과가 있다.

(가) 감사 결과가 하드웨어 식별자에 정해진 방식으로 묶여 있기 때문에, 다른 로봇이 결과를 위조할 수 없다.

(나) 격리된 실행 환경(예: WASM)에서 평가 규칙을 실행하므로 악의적인 평가 규칙이 호스트를 침해하는 일을 막으면서도, 정책 팩 생태계의 확장성은 그대로 유지된다.

(다) 다중 해시 증거와 redaction manifest 덕분에 일부만 바뀌었을 때 바뀐 영역만 다시 평가할 수 있고, 외부 감사인이 원문 비밀 없이도 redaction의 정합성을 확인할 수 있다. 대규모 플릿의 운영 부담을 줄이면서도 감사 무결성은 그대로 지킨다.

(라) 테넌트 간 cross-witness fold-in과 외부 anchoring을 두기 때문에, 운영자가 한 테넌트만 골라 위조하려는 시도뿐 아니라 모든 테넌트를 동시에 재작성하려는 시도까지 외부에 남은 사본과 충돌하면서 잡힌다.

(마) 외부 감사인이 스코프가 한정된 검증 번들을 사전에 받은 뒤에는 운영자의 계정이나 인터넷 연결 없이도 별도 OSS 도구로 무결성을 다시 검증할 수 있어, 규제 감사 흐름과 잘 맞는다.

(바) 토픽 QoS·노드 권한·서비스 인증·DDS Security·SROS2 enclave 같은 ROS2 특유의 평가도 같은 격리 실행 틀에서 받아들이고, ROS graph fingerprint를 통해 같은 PeerGroup 안에서 이상 로봇을 결정론적으로 식별할 수 있어, 일반 IT 보안 감사 도구로는 다룰 수 없던 영역까지 감사 대상에 포함시킬 수 있다.

## 도면의 간단한 설명

- 도 1: 본 발명의 시스템 전체 구성도. 로봇 식별 결합부(110), 격리 평가 실행부(120), 다중 해시 증거 저장부(130), 상호 증인 감사 체인부(140), 선택적 공개 검증부(150) 사이의 결합 관계를 보여 준다.

- 도 2: 하드웨어 식별자 H_id 산출 흐름도. 보안 모듈 식별 정보와 장치 식별 정보를 정규화한 뒤 결합 함수에 넣어 64자 hex가 나오기까지의 흐름.

- 도 3: 다중 해시 증거 저장 흐름도. raw stdout이 결정론적 redaction을 거쳐 전체 sha256과 부분 sha256 집합으로 나뉘고, redaction manifest가 함께 산출되는 과정.

- 도 4: 상호 증인 감사 체인 구조. 테넌트 T1의 체인에 T2·T3의 checkpoint hash가 fold-in되는 모습과, 외부 anchoring(타임스탬프 권한자·공개 transparency log·webhook 수신처)이 함께 표시된다.

- 도 5: 외부 감사인 검증 토큰의 발급·사용 시퀀스. 운영자가 토큰을 발급하고 검증 번들을 사전에 생성하여 감사인에게 전달한 뒤, 감사인이 OSS 검증 도구로 체인을 재계산해 검증 결과를 받기까지의 흐름.

- 도 6: 격리 평가 실행부의 WASM 런타임 보안 경계. 호스트 시스템 콜에 대한 화이트리스트 구조.

- 도 7: 외부 검증 번들의 구조도. 검증 토큰, 스코프 한정 감사 엔트리, 참조되는 테넌트 체크포인트, cross-witness 참조, 공개키 번들(현행·폐기), redaction manifest, evidence 해시 집합, 외부 anchoring 증거가 하나의 번들로 묶이는 모습을 보여준다.

## 발명을 실시하기 위한 구체적인 내용

### [실시례 1] 하드웨어 식별자 산출 (D-3)

[0013] 로봇 식별 결합부(110)는 다음과 같은 의사 코드로 구현된다.

```
function computeHardwareFingerprint(robot):
    tpmEkPub  = readTPMEKCertificate(robot)  // X.509 인증서의 공개키 부분
    macList   = sorted(readNetworkInterfaceMACs(robot))  // 정렬해 결정론성을 보장
    cpuSerial = readCPUSerial(robot)  // /proc/cpuinfo 또는 dmidecode

    canonical = tpmEkPub + "|" + join(macList, ",") + "|" + cpuSerial
    return sha256_hex(canonical)
```

[0014] MAC 주소를 정렬한 뒤 구분자를 끼워 넣기 때문에, 같은 로봇이라면 시점이나 실행 환경이 달라도 항상 같은 식별자가 나온다. TPM이 없는 로봇은 EK 부분을 빈 문자열로 두는 대신, 그 사실을 메타데이터에 함께 남긴다.

[0014-1] 본 실시례의 변형으로, 보안 모듈로부터 유래한 식별 정보로 TPM EK 인증서 외에 TPM Attestation Key를 사용할 수 있고, 장치 식별 정보로 MAC 주소·CPU 시리얼 외에 메인보드 시리얼 번호, BIOS UUID, 디바이스 인증서의 공개키 fingerprint 등을 단독 또는 조합하여 사용할 수 있다. 결합 함수도 SHA-256 외에 SHA-3, BLAKE3, HMAC, 또는 디지털 서명을 적용할 수 있다. 이러한 변형은 모두 본 명세서가 정의하는 하드웨어 식별자 결합의 범주에 속한다.

### [실시례 2] WASM 격리 평가 실행부 (C-1)

[0015] 격리 평가 실행부(120)는 Wasmtime 또는 Wasmer 런타임을 임베딩하여 다음과 같은 인터페이스를 제공한다.

```
type Evaluator struct {
    runtime  WasmRuntime
    allowed  []string  // 화이트리스트 함수 목록
}

function evaluate(checkDef, evidence):
    module = runtime.compile(checkDef.wasmBytes)
    instance = runtime.instantiate(module, hostFunctions=allowed)
    result = instance.call("eval", evidence)
    return parseOutcome(result)  // pass | fail | error | NA | manual
```

[0016] 호스트 함수 화이트리스트는 includes, match, length, trim, json.parse, semver.gte 등으로 제한된다. 파일 시스템·네트워크·시스템 콜에는 일체 접근할 수 없다.

[0016-1] WASM 런타임은 본 발명의 대표적인 격리 실행 환경 실시례이지만, 컨테이너 기반 샌드박스, eBPF 기반 격리, 별도 프로세스 격리 같은 다른 격리 기술도 호스트 시스템 콜의 화이트리스트 제한이라는 같은 성질을 갖추는 한 본 발명의 격리 실행 환경에 해당한다.

### [실시례 3] 다중 해시 증거 저장 (B-1)

[0017] 다중 해시 증거 저장부(130)는 다음과 같이 동작한다.

```
function storeEvidence(rawStdout, rawStderr, exitCode, partitionRules, redactionRuleVersion):
    redacted, manifest = deterministicRedact(rawStdout, redactionRuleVersion)
    fullHash = sha256_hex(redacted + rawStderr + exitCode)

    partHashes = {}
    for rule in partitionRules:
        if rule.type == "line":
            for i, line in enumerate(redacted.split("\n")):
                partHashes["line:" + i] = sha256_hex(line)
        elif rule.type == "jsonpath":
            for path in rule.paths:
                value = jsonpath.extract(redacted, path)
                partHashes["jsonpath:" + path] = sha256_hex(canonicalize(value))

    return { full: fullHash, parts: partHashes, manifest: manifest }
```

[0018] 결정론적 redaction은 다음 세 단계로 이루어진다. 먼저 정규식과 엔트로피 기반 패턴 매칭으로 비밀로 보이는 부분을 찾아낸다. 다음으로 비밀의 유형(PEM, JWT, API key 등)별로 같은 길이의 식별자(예: `<REDACTED:PEM:0001>`)로 치환한다. 마지막으로, 같은 입력이라면 같은 식별자 시퀀스가 나오도록 결정론성을 보장한다.

[0018-1] redaction manifest는 치환된 비밀 후보별로 다음 항목을 담되, **원문 비밀 값 자체는 포함하지 않는다.**

```
type RedactionManifestEntry {
    offset                int      // 원문에서의 시작 위치
    length                int      // 원문에서의 길이
    secretType            string   // 'pem' | 'jwt' | 'apikey' | 'password' | 'high-entropy' 등
    placeholder           string   // 치환된 식별자 (예: <REDACTED:PEM:0001>)
    saltedDigest          string   // HMAC-SHA256(secret, manifestSalt)
    redactionRuleVersion  string   // 사용된 redaction 규칙 버전
}

type RedactionManifest {
    redactionRuleVersion  string
    manifestSaltDigest    string   // sha256(manifestSalt) — salt 자체는 별도 보관
    entries               []RedactionManifestEntry
}
```

[0018-2] 외부 감사인은 redaction manifest를 통해 (i) 동일한 redaction 규칙으로 재현했을 때 같은 자리에 같은 placeholder가 나오는지 확인하고, (ii) saltedDigest를 비교해 manifest와 evidence가 같은 원문에서 비롯된 것인지 검증한다. saltedDigest는 manifest에만 보관되는 salt와 결합한 HMAC이므로 원문 복원에는 사용되지 않는다. 검증 도구는 saltedDigest 불일치 시 REDACTION_DIGEST_MISMATCH 오류를 반환한다.

### [실시례 4] 상호 증인 감사 체인 (A-1)

[0019] 상호 증인 감사 체인부(140)는 다음과 같은 구조의 엔트리를 관리한다.

```
type AuditEntry {
    tenantId        string
    seq             uint64       // tenant 안에서 단조 증가
    occurredAt      timestamp
    actor           Actor
    action          string
    target          Target
    payloadDigest   string       // canonical JSON에 대한 sha256
    crossWitness    []TenantCheckpoint  // fold-in 시점에만 채워짐
    externalAnchor  *ExternalAnchor     // 외부 anchoring 시점에만 채워짐
    prevHash        string
    hash            string       // sha256(prevHash || payloadDigest || meta || crossWitness || externalAnchor)
}

type TenantCheckpoint {
    tenantId  string
    seq       uint64
    hash      string
    signedAt  timestamp
}

type ExternalAnchor {
    kind      string   // 'tsa-rfc3161' | 'transparency-log' | 'external-storage' | 'webhook'
    target    string   // TSA URL, Rekor 인덱스, 저장소 ARN, webhook URI 등
    proof     bytes    // TSA 서명 응답, transparency log inclusion proof, 저장소 응답 영수증 등
    anchoredAt timestamp
}
```

[0020] fold-in 정책은 다음 세 시점 중 하나에서 발동한다 — 매시간 도래, 엔트리 100개 누적, 사용자가 정의한 트리거. 이 시점에 활성 상태의 다른 모든 테넌트로부터 최신 checkpoint를 모아 새 엔트리에 함께 담는다.

[0020-1] crossWitness 배열은 다음 규칙에 따라 결정론적으로 구성된다.

(i) **대상 선택**: fold-in 시점에 활성 상태이며 1개 이상의 엔트리를 가진 다른 모든 테넌트를 후보로 한다. "활성"이란 직전 N(예: 24)시간 안에 새 엔트리가 추가된 상태를 말하며, 이 정의는 메타데이터에 함께 기록된다.

(ii) **정렬**: 후보 테넌트의 최신 checkpoint를 tenantId의 사전식(lexicographic) 정렬 순서로 배열한다. 동일 tenantId가 중복되지 않으며, 각 항목은 (tenantId, seq, hash, signedAt) 형태의 canonical JSON(실시례 8 참조)으로 직렬화된다.

(iii) **canonical 직렬화**: crossWitness 배열 전체도 RFC 8785 JSON Canonicalization Scheme에 따라 직렬화되며, 그 직렬 결과가 hash 계산의 입력에 포함된다.

[0020-2] 검증자는 다음 절차로 fold-in 무결성을 확인한다.

1. fold-in 엔트리에 박힌 각 TenantCheckpoint에 대해, 해당 테넌트 체인을 재계산한 결과의 (seq, hash)와 일치하는지 확인한다.
2. 하나 이상의 TenantCheckpoint가 재계산 결과와 어긋나면 변조로 판단하고 CROSS_WITNESS_MISMATCH 오류를 반환한다.
3. crossWitness 배열의 정렬·중복·canonical 직렬화 규칙을 위반하면 BUNDLE_INCOMPLETE 또는 ENTRY_HASH_MISMATCH로 판단한다.

[0020-3] 운영자가 자신이 통제하는 모든 테넌트 체인을 동시에 사후 재작성하는 시나리오에 대해서는, 다음 외부 anchoring 방식 중 하나 이상을 함께 적용한다(선택 실시례).

(i) **TSA**: 외부 타임스탬프 권한자(RFC 3161)에게 주기적으로 checkpoint hash를 서명받아 보관한다.

(ii) **공개 transparency log**: Sigstore Rekor, Certificate Transparency 같은 공개 로그에 checkpoint hash를 anchoring하고 inclusion proof를 보관한다.

(iii) **외부 저장소**: 고객사의 별도 외부 저장소(예: 고객 S3·KMS·HSM)에 checkpoint를 push하고 응답 영수증을 보관한다.

(iv) **감사인 webhook**: 사전 등록된 감사인 또는 webhook 수신처에 주기적으로 checkpoint를 송신해 외부 사본을 확보한다.

이 anchoring을 통해, 운영자가 자신이 통제하는 모든 테넌트 체인을 일관되게 재작성하더라도 외부에 남은 사본과 충돌하므로 변조 사실이 드러난다. 검증자는 ANCHOR_VERIFICATION_FAILED 오류로 그 충돌을 보고한다.

[0020-4] fold-in 대상 테넌트가 0개인 경우, 해당 fold-in 엔트리의 crossWitness는 빈 배열이 되며, 그 fold-in 엔트리는 [0020-3]의 외부 anchoring 중 하나 이상을 반드시 동반한다. fold-in 대상 테넌트가 1개인 경우에도 같은 외부 anchoring 보강을 적용해 단일 증인의 한계를 보완한다.

### [실시례 5] 선택적 공개 검증과 검증 번들 (A-3)

[0021] 선택적 공개 검증부(150)는 다음과 같은 토큰 구조를 사용한다.

```
type VerificationToken {
    tokenId          string
    tenantId         string
    scope: {
        timeFrom     timestamp
        timeTo       timestamp
        actionTypes  []string   // 예: ["scan.execute", "report.sign"]
        targetTypes  []string   // 예: ["robot", "scan"]
    }
    expiresAt        timestamp
    signature        bytes      // Ed25519
}
```

[0022] 검증 토큰을 받은 외부 감사인은 먼저 운영자가 발급한 스코프 한정 검증 번들을 사전에 획득한다. 검증 번들은 다음을 포함한다.

```
type VerificationBundle {
    token             VerificationToken
    entries           []AuditEntry            // 스코프 안의 감사 엔트리(NDJSON 또는 동등 형식)
    referencedCheckpoints  []CheckpointSignature
    crossWitnessCheckpoints []TenantCheckpoint  // crossWitness가 참조하는 다른 테넌트의 체크포인트
    publicKeyBundle   PublicKeyBundle         // 현행 키 + 폐기 키 목록
    redactionManifest RedactionManifest
    evidenceHashes    []EvidenceHashRecord    // 전체·부분 해시 집합
    externalAnchorProofs []ExternalAnchor     // 적용된 anchoring 증거
}
```

[0022-1] 외부 감사인은 별도 OSS 검증 도구(`rosshield-verify`)에 검증 번들을 입력한다. 도구는 인터넷 연결 없이 다음을 차례로 수행한다.

1. 토큰 서명 검증
2. 스코프 안에 들어오는 엔트리만 추출
3. 체인 재계산(prevHash → hash)
4. cross-witness fold-in 검증(실시례 4 [0020-2])
5. 외부 anchoring 검증(있는 경우, [0020-3])
6. 공개키 서명 검증(테넌트·체크포인트·토큰·폐기 목록 대조)
7. redaction manifest 검증(saltedDigest 일치 여부, [0018-2])

[0022-2] 검증 결과는 단일 OK가 아니라 다음 유형 중 하나로 보고된다.

```
type VerificationOutcome {
    status   string   // 'ok' | 'failed'
    failures []FailureCode
}

const FailureCode = {
    TOKEN_SIGNATURE_INVALID,
    TOKEN_EXPIRED,
    SCOPE_VIOLATION,
    ENTRY_HASH_MISMATCH,
    PREV_HASH_MISMATCH,
    CHECKPOINT_SIGNATURE_INVALID,
    CROSS_WITNESS_MISMATCH,
    ANCHOR_VERIFICATION_FAILED,
    PUBLIC_KEY_REVOKED,
    BUNDLE_INCOMPLETE,
    REDACTION_DIGEST_MISMATCH,
}
```

스코프를 벗어난 엔트리에 대해서는 도구가 SCOPE_VIOLATION을 보고하거나 출력에서 마스킹하여 가린다.

### [실시례 6] 동작 시나리오

[0023] 예를 들어 어느 로봇 운영사가 보유한 12대의 ROS2 로봇 플릿을 ISMS-P 컴플라이언스 기준으로 감사하는 상황을 가정해 보자.

1. 각 로봇을 등록할 때 H_id를 산출하여 함께 저장한다(실시례 1).
2. CIS Ubuntu 24.04 팩과 ROS2 Jazzy 팩을 설치하고, 평가 규칙은 WASM 모듈 형태로 올린다(실시례 2). ROS2 특화 plugin check type(실시례 10)도 포함한다.
3. 감사 세션을 실행하면서 체크마다 나오는 evidence를 다중 해시와 redaction manifest로 함께 저장한다(실시례 3).
4. 결과를 audit chain에 추가하고, 매시간 다른 테넌트의 checkpoint를 fold-in한다(실시례 4). 동시에 사전 등록된 TSA에 checkpoint hash를 anchoring한다([0020-3] (i)).
5. 분기말 외부 감사 시점이 오면, 감사인에게 7일 후 만료되며 scan·report 카테고리만 허용하는 검증 토큰을 발급하고, 운영자는 그 토큰의 스코프에 맞춘 검증 번들을 함께 생성한다(실시례 5).
6. 감사인은 USB 같은 이동식 저장매체로 검증 번들을 받아, 인터넷 연결 없이 검증 도구를 돌린다. 결과는 "ok" 또는 [0022-2]에 정의된 실패 유형(예: ENTRY_HASH_MISMATCH at seq N) 중 하나로 보고된다.

### [실시례 7] append-only 저장 구현 옵션

[0024] 감사 엔트리의 append-only 성질은 다음 중 하나 이상으로 보장된다.

(i) **데이터베이스 트리거**: UPDATE/DELETE를 차단하는 append-only 테이블. INSERT만 허용하는 데이터베이스 역할(role)을 별도로 두어 애플리케이션 사용자에게 부여하고, 데이터베이스 관리자(DBA) 권한 사용은 별도 감사 플러그인으로 추적한다.

(ii) **객체 스토리지의 versioning + object lock**: 한 번 기록된 객체의 덮어쓰기·삭제를 차단한다(WORM 모드).

(iii) **WORM 저장소**: 한 번 쓰면 다시 쓸 수 없는 전용 저장소에 직접 기록한다.

(iv) **권한 분리**: 감사 엔트리 작성 권한과 체크포인트 서명 권한을 서로 다른 자격증명에 부여하여, 한 권한만으로는 위조가 불가능하게 한다.

(v) **Merkle tree inclusion proof**: 체인을 Merkle tree로 구성하고, 각 엔트리에 대해 inclusion proof를 별도 보관한다.

[0025] 위 구현은 단독 또는 결합으로 적용할 수 있다. 어떤 구현을 선택하든, 각 엔트리의 hash는 직전 엔트리 hash, 현재 payloadDigest, canonical meta(crossWitness·externalAnchor 포함)를 함께 입력으로 산출되며, 체크포인트는 서비스 서명키 또는 HSM/KMS에 보관된 키로 서명된다.

### [실시례 8] canonical 형식 및 해시 입력 규칙

[0026] 본 발명에서 모든 해시 입력은 다음 canonical 규칙을 따른다.

(i) 문자열은 UTF-8로 인코딩한다.

(ii) JSON 직렬화는 RFC 8785 JSON Canonicalization Scheme(JCS) 또는 그에 준하는 결정론적 직렬화를 사용한다. 키는 UCS 코드 포인트 사전식 순서로 정렬되고, 공백·들여쓰기는 제거되며, 숫자는 RFC 8259 표현으로 정규화된다.

(iii) 시각은 RFC 3339/ISO-8601 UTC 형식(예: 2026-05-08T03:14:15.123Z) 또는 epoch milliseconds 정수 중 하나로 일관 사용한다. 한 시스템 안에서 형식이 혼용되지 않는다.

(iv) 줄 끝은 LF(0x0A)로 통일된다.

(v) null과 빈 문자열·빈 배열은 서로 다른 값으로 구분된다.

(vi) 배열의 정렬 기준이 의미상 중요하지 않은 경우(예: crossWitness)는 사전식 정렬 등 명시된 기준을 적용한다.

(vii) 결합 연산에서 사용하는 구분자는 입력에 등장할 수 없는 바이트(예: 0x1F unit separator) 또는 충분한 길이의 ASCII 문자열을 사용해 구분자 충돌을 방지한다.

[0027] 위 규칙을 위반한 입력으로 산출된 해시는 외부 검증 도구가 ENTRY_HASH_MISMATCH로 판정한다.

### [실시례 9] 키 관리 및 키 회전

[0028] 본 발명에서 사용되는 서명 키는 역할별로 분리된다.

- **테넌트 서명키**: 각 테넌트의 감사 엔트리·체크포인트에 서명하는 키. 서비스가 보관하되 테넌트 단위로 분리된다.
- **체크포인트 서명키**: 체크포인트 별도 키 또는 테넌트 서명키와 동일한 키 중 선택할 수 있다. 어플라이언스 모드에서는 TPM·HSM에 봉인된 별도 키 사용을 권장한다.
- **검증 토큰 서명키**: 외부 감사인용 토큰을 서명하는 키. 일반적으로 테넌트 서명키와 분리해 권한·노출 면을 최소화한다.

[0029] 키 회전 절차는 다음과 같다.

1. 새 키쌍을 생성하고 공개키를 공개키 번들에 추가한다(이전 키도 함께 유지).
2. 회전 시점부터 새 엔트리·체크포인트는 새 키로 서명한다.
3. 일정 유예기간이 지난 뒤, 폐기된 구 키는 폐기 키 목록(revoked key list)에 추가된다. 폐기 후에도 구 키로 서명된 과거 엔트리의 검증은 그대로 가능하다.

[0030] 검증 도구는 공개키 번들과 폐기 키 목록을 함께 입력으로 받아, (i) 서명 시점에 키가 유효했는지, (ii) 검증 시점에 키가 폐기되어 있는지, (iii) 폐기 사유가 변조 의심인 경우 PUBLIC_KEY_REVOKED 오류를 반환한다.

[0031] 모든 서명 키는 HSM 또는 KMS에 보관할 수 있다. 어플라이언스 모드에서는 TPM 봉인을 권장하며, 키 추출 공격이 발생하면 TPM 정책에 의해 자동 무효화된다.

### [실시례 10] ROS2 특화 평가 항목 및 ROS graph fingerprint

[0032] 본 발명의 격리 평가 실행부는 일반 OS 보안 체크에 더하여, 다음과 같은 ROS2 특화 평가 항목을 plugin check type으로 수용한다.

(i) ROS_DOMAIN_ID별 감사 범위 분리. 한 도메인 안의 노드·토픽·서비스만을 평가 대상으로 한정한다.

(ii) DDS Security 활성화 여부. Authentication, AccessControl, Cryptography 플러그인의 로드 상태와 SROS2 enclave 적용 여부를 검증한다.

(iii) 토픽별 QoS profile(reliability, durability, history, depth, deadline, lifespan, liveliness)의 기대값 비교.

(iv) 토픽별 publisher·subscriber 수의 기대 범위 검증(예: `/cmd_vel`은 publisher 1개·subscriber 1개).

(v) 기대 목록에 없는 비인가 node 탐지.

(vi) service·action endpoint의 노출 여부와 권한 정책.

(vii) namespace별 SROS2 permissions.xml 정합성.

(viii) SROS2 keystore의 enclave·permissions·governance 파일 일관성.

[0033] ROS graph fingerprint는 다음과 같이 산출된다.

```
function computeRosGraphFingerprint(robot):
    nodes      = sorted(listNodes(robot))                    // (namespace, name)
    topics     = sorted(listTopics(robot))                   // (name, type)
    services   = sorted(listServices(robot))                 // (name, type)
    actions    = sorted(listActions(robot))                  // (name, type)
    qosByTopic = sorted(listQosByTopic(robot))               // (topic, qos canonical JSON)
    domainId   = readRosDomainId(robot)
    sec        = readDdsSecurityState(robot)                 // enabled/disabled, plugin set, enclave id

    canonical = canonicalJSON({
        nodes, topics, services, actions, qosByTopic, domainId, sec
    })
    return sha256_hex(canonical)
```

[0034] 플릿 상호 검증부는 같은 PeerGroup으로 분류된 로봇들의 ROS graph fingerprint 또는 그 부분 해시(노드 단위·토픽 단위·QoS 단위)를 비교하여, 다수결 또는 사전 정의된 기준 비율(예: 80% 이상)에서 벗어난 로봇을 이상 상태로 판정한다. 이 판정은 통계가 아니라 결정론적 비교로 이루어지므로, 같은 입력을 받은 외부 검증자는 동일한 판정 결과를 재현할 수 있다.

[0035] PeerGroup 결정에는 (osDistro, rosDistro, role)에 더하여 정책 팩 버전, evaluator 버전, redaction 규칙 버전 등을 함께 고려할 수 있다. 같은 PeerGroup 안에서 ROS graph fingerprint와 evidence 부분 해시를 모두 결합하여 비교하면, 설정 차이와 그래프 토폴로지 차이를 함께 잡아낼 수 있다.

## 청구의 범위 (raw draft — 변리사 최종화 필요)

> 청구항은 한국 KIPO 정형 문체를 따른다. 이하는 raw draft이며, 변리사가 권리 범위(독립항 분할, 추상화 수준, 종속항 배치)를 최종 확정한다. 추상화·분할 권고는 본 명세서 끝의 "변리사 입력용 보조 정보" 참조.

**[청구항 1]** ROS2 로봇 플릿의 보안 감사 결과를 검증하는 시스템에 있어서,

각 로봇의 TPM Endorsement Key 인증서, 네트워크 인터페이스 MAC 주소 및 CPU 시리얼을 결정론적 결합 함수에 입력하여 하드웨어 식별자를 산출하는 로봇 식별 결합부;

정책 팩이 동봉한 평가 규칙을 WebAssembly 런타임에서 화이트리스트 호스트 함수로 제한하여 격리 실행하고, 그 결과를 상기 하드웨어 식별자와 결합하는 격리 평가 실행부;

상기 평가 결과의 출력에 결정론적 redaction을 적용한 후 전체 해시와 의미 단위 부분 해시 집합 및 redaction manifest를 산출하는 다중 해시 증거 저장부; 및

테넌트별 단조 증가 시퀀스의 append-only 감사 엔트리를 해시 체인으로 연결하고, 주기적으로 다른 테넌트의 최신 체크포인트 해시를 자기 체인 엔트리에 fold-in하는 상호 증인 감사 체인부;

를 포함하는 것을 특징으로 하는 ROS2 로봇 플릿 보안 감사 결과 검증 시스템.

**[청구항 2]** 제1항에 있어서, 시간 범위·액션 카테고리·만료 시각이 담긴 검증 토큰을 외부 감사인에게 발급하고, 상기 검증 토큰에 기초하여 스코프가 한정된 검증 번들을 생성하며, 외부 검증 도구가 상기 검증 번들과 공개키 정보를 입력으로 받아 별도 계정 및 인터넷 연결 없이 상기 감사 체인의 무결성, fold-in 상호 증인 관계, 외부 anchoring 증거 및 redaction manifest를 재계산하여 검증할 수 있도록 하는 선택적 공개 검증부를 더 포함하는 것을 특징으로 하는 시스템.

**[청구항 3]** 제1항에 있어서, 상기 격리 평가 실행부의 평가 규칙은 ROS2의 node, topic, service, action, QoS profile, ROS_DOMAIN_ID, DDS Security 활성 상태, SROS2 enclave 또는 permissions policy 중 하나 이상을 canonical evidence로 수집하여 평가하는 plugin check type을 포함하는 것을 특징으로 하는 시스템.

**[청구항 4]** 제1항에 있어서, 상기 다중 해시 증거 저장부는 동일 정책 팩 버전, 동일 평가기(evaluator) 버전, 동일 evidence 분할 규칙, 동일 redaction 규칙 버전 중 하나 이상의 조건이 충족되는 직전 세션과 부분 해시가 동일한 영역에 대해서는 평가를 재사용하고, 변경된 영역에 대해서만 다시 평가를 수행하는 차분 평가부를 더 포함하는 것을 특징으로 하는 시스템.

**[청구항 5]** 제1항에 있어서, OS 배포판, ROS distribution, 로봇 역할 및 정책 팩 버전 중 하나 이상에 의해 결정되는 PeerGroup으로 분류된 복수의 로봇에 대해, 동일한 체크의 evidence 부분 해시 또는 ROS graph fingerprint가 다수결 또는 사전 정의된 기준 비율에서 이탈한 로봇을 결정론적으로 검출하는 플릿 상호 검증부를 더 포함하는 것을 특징으로 하는 시스템.

**[청구항 6]** 제1항에 있어서, 상기 상호 증인 감사 체인부는 자기 체인의 체크포인트 해시를 외부 타임스탬프 권한자(RFC 3161), 공개 transparency log, 고객사 외부 저장소 또는 사전 등록된 webhook 수신처 중 하나 이상에 추가로 anchoring하는 외부 anchoring부를 더 포함하는 것을 특징으로 하는 시스템.

**[청구항 7]** 제1항 내지 제6항 중 어느 한 항의 시스템을 구현하는 컴퓨터 프로그램이 저장된 비일시적 컴퓨터 판독 가능 매체.

**[청구항 8]** ROS2 로봇 플릿의 보안 감사 결과를 검증하는 방법에 있어서, 컴퓨터가 (i) 로봇 식별 정보를 수집하고, (ii) 결합 함수로 하드웨어 식별자를 산출하고, (iii) ROS2 보안 감사 evidence를 수집하고, (iv) 격리 실행 환경에서 평가 규칙을 실행하고, (v) 평가 결과와 하드웨어 식별자를 결합한 페이로드를 생성하고, (vi) 평가 결과의 출력에 결정론적 redaction을 적용한 뒤 전체 해시·부분 해시·redaction manifest를 산출하고, (vii) 감사 엔트리를 생성하여 해시 체인에 추가하고, (viii) 다른 테넌트의 체크포인트 해시를 자기 체인 엔트리에 fold-in하고, (ix) 외부 anchoring을 적용하고, (x) 검증 토큰에 기초하여 스코프 한정 검증 번들을 생성하며, (xi) 외부 검증 도구가 상기 검증 번들과 공개키 정보를 이용해 인터넷 연결 없이 체인 재계산·서명 검증·fold-in 검증·anchoring 검증·redaction manifest 검증을 수행하는 단계들을 수행하는 것을 특징으로 하는 방법. (변리사가 단계별 분해 청구항으로 추가 전개해야 한다.)

---

## 변리사 입력용 보조 정보

### 진보성 논거 핵심 키워드
- 결합 신규성: TPM 결합 + WASM 격리 + 다중 해시 + 멀티테넌트 cross-witness + 외부 anchoring + 선택적 공개 검증 토큰. 단일 요소가 아닌 결합의 비자명성에 무게를 둔다.
- 도메인 특화: ROS2 그래프(토픽·노드·서비스)와 DDS Security·SROS2. 일반 IT 보안 감사 도구가 다루지 못하는 표면이다.

### 선행기술 대비 진보성 논거 표

| 선행기술 유형 | 한계 | 본 발명의 차이 |
|---|---|---|
| OpenSCAP / CIS-CAT | 일반 호스트 중심, ROS2 그래프·DDS Security 부재 | ROS2 topic·node·service·QoS·DDS Security 특화 evidence + ROS graph fingerprint(실시례 10) |
| AWS CloudTrail Log File Validation | 클라우드 이벤트 로그, 단일 운영자 모델 | 로봇 플릿 감사 결과를 하드웨어 식별자에 결합 + cross-witness fold-in + 외부 anchoring(실시례 4) |
| Certificate Transparency / Sigstore Rekor | 글로벌 단일 로그 모델, 멀티테넌트 격리 부재 | 테넌트 격리 + cross-witness fold-in + 스코프 한정 검증 번들(실시례 5) |
| AWS QLDB / Hyperledger Fabric | 범용 원장, 외부 감사인 워크플로 부재 | 스코프 한정 검증 토큰 + 검증 번들 + 오프라인 OSS 도구 검증(실시례 5) |
| OPA / Rego + opa test | 정책 평가에 한정, 실행 격리·증거 결합 부재 | WASM 격리 + 다중 해시 evidence + redaction manifest + 하드웨어 식별자 결합 |
| AWS S3 Object Lock / WORM 저장소 | 저장 단계 무결성에 한정, 증거 결합·외부 검증 부재 | append-only는 보강 옵션의 하나일 뿐, 본 발명은 그 위에 결합 발명을 구성 |

### 핵심 진보성 문장 (변리사용)

본 발명은 단순한 감사 로그 저장이나 일반적인 transparency log가 아니라, ROS2 로봇 플릿의 보안 감사 결과를 로봇 하드웨어 식별자에 결합하고, 격리된 실행 환경에서 평가된 결과와 다중 해시 evidence를 테넌트별 append-only 감사 체인에 기록하며, 다른 테넌트의 체크포인트를 상호 fold-in하고 외부 anchoring으로 추가 보강함으로써 멀티테넌트 환경에서 운영자 자신의 사후 변조 가능성(단일 테넌트 변조 및 모든 테넌트 동시 재작성 모두)을 낮추고, 스코프가 한정된 검증 번들을 통해 외부 감사인이 별도 계정과 인터넷 연결 없이 무결성을 재검증할 수 있게 하는 결합 구조이다.

### 청구 범위 추상화·분할 권고 (변리사 검토 영역)

본 명세서의 청구항 raw draft는 핵심 결합을 청구항 1로 두고 cross-witness·외부 anchoring·검증 번들·차분 평가·PeerGroup 다수결 검출을 종속항으로 분해해 두었다. 변리사는 권리 범위 결정 시 다음을 함께 검토한다.

1. **청구항 1 핵심 요소 추상화**:
   - "TPM Endorsement Key 인증서, MAC 주소, CPU 시리얼" → "보안 모듈 식별 정보 및 하나 이상의 장치 식별 정보". 종속항에서 구체화.
   - "WebAssembly 런타임" → "격리 실행 환경"으로 일반화. 종속항에서 WASM·컨테이너 샌드박스·eBPF 격리 등 구체화.
   - "결정론적 결합 함수" → SHA-256·SHA-3·BLAKE3·HMAC·디지털 서명 등 다양한 변형을 포괄.

2. **독립항 분할 옵션**:
   - 옵션 A — 결합 청구항 단일: 모든 핵심 요소를 청구항 1에 두고 종속항으로 구체화 (현재 raw draft).
   - 옵션 B — 5개 독립항 병렬: 하드웨어 식별자 결합 / 격리 평가 / 다중 해시 + 차분 / cross-witness + 외부 anchoring / 오프라인 검증 번들을 각각 독립항으로.
   - 옵션 C — 1+4 종속 구조: 가장 추상화된 골격(하드웨어 식별자 + 감사 체인)을 청구항 1로, 나머지는 종속항.

   본 명세서의 1순위 결합 청구항(D8-2)은 옵션 A로 작성되어 있다. 옵션 B는 권리 범위가 넓어지나 진보성 논거 분산 위험. 옵션 C는 진보성 논거 집중도가 높으나 회피 가능성 증가. 변리사가 선행기술 조사 결과를 본 뒤 옵션 결정.

3. **방법 청구항(청구항 8) 단계별 분해**:
   본 명세서 [실시례 6] 1~6단계 + [0020-2]의 검증 절차 + [0022-1]의 검증 도구 절차에서 도출되는 단계 10~12개를 명시적 단계 청구항으로 전개해야 한다.

4. **차분 평가 재사용 조건(청구항 4)**:
   동일 정책 팩 버전·동일 평가기 버전·동일 분할 규칙·동일 redaction 규칙 버전 중 하나 이상이 충족될 때로 한정된다. 추가로 동일 하드웨어 식별자 또는 동일 PeerGroup 조건을 결합하면 부정 재사용 위험을 더 줄일 수 있다.

5. **PeerGroup 다수결 검출(청구항 5)**:
   결정론적 비교를 강조하는 표현이 필요하다. 통계적 anomaly detection으로 해석되면 OPA·기존 anomaly 도구와의 차별이 약화된다.

### 검토할 선행기술
- Sigstore Rekor / Trillian (transparency log)
- Certificate Transparency RFC 6962
- AWS QLDB, Hyperledger Fabric (append-only ledger)
- AWS CloudTrail Log File Validation
- AWS S3 Object Lock / WORM
- OpenSCAP / CIS-CAT / Wazuh syscheck
- Schneier-Kelsey "Secure Audit Logs" (1998)
- OPA Rego + `opa test`
- Sigstore cosign / policy-controller
- Helm chart provenance
- RFC 3161 TSA (외부 anchoring 비교)

### 도면 작도 우선순위
도 1 → 도 4 → 도 7 → 도 5 → 도 2 → 도 3 → 도 6. 전체 구성도와 cross-witness/외부 anchoring 구조, 검증 번들 구조도(도 7), 외부 검증 시퀀스를 먼저 그려야 차별성이 가장 잘 드러난다.

### KIPO 분류 후보
- G06F 21/64 (데이터 무결성 검증)
- G06F 21/53 (격리 실행 환경)
- G06F 21/57 (소프트웨어 컴플라이언스 검증)
- G06F 21/60 (비밀 정보 보호)
- H04L 9/32 (공개키 인증·서명)
