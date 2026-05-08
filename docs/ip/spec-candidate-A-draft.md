# 명세서 초안 — 후보 A 결합 청구항

> **이 문서의 위치**: 본 명세서는 변리사 컨설팅에 넘기기 위한 raw draft다. KIPO 명세서 양식을 따르되 청구항·도면·심사 응답은 변리사가 최종 확정한다. 본 문서를 그대로 출원하지 말 것.
>
> **참조**: `docs/design/13-patent-strategy.md` §13.5 1순위 결합 청구항(D8-2)

---

## 발명의 명칭

ROS2 로봇 플릿의 보안 감사 결과를 하드웨어 식별자와 결합하고, 멀티테넌트 상호 증인 해시 체인을 통해 외부에서 검증할 수 있도록 하는 시스템 및 방법
(System and Method for Verifying Security Audit Results of ROS2 Robot Fleets Bound to Hardware Identifiers via Multi-Tenant Cross-Witness Hash Chain)

## 기술 분야

본 발명은 ROS2(Robot Operating System 2) 기반 로봇 플릿의 보안 구성 상태를 정해진 절차에 따라 결정론적으로 감사하고, 그 결과의 무결성을 외부 감사인이 별도의 계정이나 인터넷 연결 없이도 다시 확인할 수 있게 해 주는 시스템 및 방법에 관한 것이다.

## 용어의 정의

본 명세서에서 사용하는 주요 용어는 아래와 같이 정의한다. 이하 정의는 청구항을 해석할 때 본 명세서가 정한 의미에 따른다.

### 시스템 모델

- **로봇 플릿(Robot Fleet)**: 동일한 운영 주체가 관리하는 복수의 로봇 집합. 본 명세서에서는 ROS2 기반 로봇이 N대 단위로 묶여 같은 보안 정책 아래 운영되는 단위를 가리킨다.

- **멀티테넌트(Multi-tenant)**: 하나의 시스템 인스턴스가 복수의 고객 조직을 데이터와 권한 측면에서 격리하여 동시에 서비스하는 구조. 본 명세서에서 "테넌트"는 그러한 격리 단위 하나를 의미한다.

- **PeerGroup**: 같은 OS 배포판, 같은 ROS distribution, 같은 운영 역할 등으로 묶인 비교 가능한 로봇의 집합. 본 명세서에서는 (osDistro, rosDistro, role) 튜플을 기본 분류 키로 사용한다.

- **스코프(scope)**: 토큰 또는 권한이 적용되는 범위. 본 명세서의 검증 토큰은 시간 범위·액션 카테고리·만료 시각으로 스코프를 한정한다.

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

(5) ROS2 토픽·노드·QoS처럼 도메인 특화된 보안 표면은 다루지 못한다.

[0003] 한편 Sigstore Rekor, Certificate Transparency 같은 변조 방지 로그 시스템은 전 세계가 공유하는 단일 로그를 전제로 설계되어 있어, 다중 테넌트로 격리된 환경에 그대로 적용하기 어렵다. 또한 외부 감사인이 스코프와 만료가 한정된 토큰만으로 계정 없이 검증하는 흐름도 지원하지 않는다.

## 해결하려는 과제

[0004] 본 발명이 풀고자 하는 과제는 다음과 같다.

(가) ROS2 로봇 플릿의 보안 감사 결과를 해당 로봇의 하드웨어 식별자에 정해진 방식으로 묶어, 다른 로봇이 결과를 위조할 수 없도록 한다.

(나) 평가 규칙을 격리된 실행 환경에서 돌려, 정책 팩이 호스트의 권한을 침해하는 일을 막으면서도 하나의 인터페이스로 여러 평가 규칙을 받아들인다.

(다) 감사 결과의 증거 출력에 대해 전체와 부분 단위 해시를 함께 산출하여, 일부만 바뀌었을 때 바뀐 영역만 다시 평가하면서도 감사 무결성을 그대로 유지한다.

(라) 다중 테넌트 환경에서 운영자가 한 테넌트의 체인을 위조하더라도, 다른 테넌트가 증인이 되어 그 위조 사실을 잡아낼 수 있도록 한다.

(마) 외부 감사인이 운영자의 계정이나 인터넷 연결 없이도 감사 결과의 무결성을 다시 확인할 수 있도록 한다.

## 과제의 해결 수단

[0005] 본 발명의 시스템은 다음 구성을 포함한다.

(a) **로봇 식별 결합부**는 각 로봇의 TPM(Trusted Platform Module) Endorsement Key 인증서, 하나 이상의 네트워크 인터페이스 MAC 주소, CPU 시리얼 번호를 입력으로 받는다. 이를 결합 함수 H_id = SHA-256(TPM_EK_pubkey ‖ sorted(MACs) ‖ CPU_serial)에 통과시켜 하드웨어 식별자를 만든다.

(b) **격리 평가 실행부**는 정책 팩에 들어 있는 평가 규칙을 WebAssembly(WASM) 런타임에서 실행한다. 이때 호스트 시스템 콜에 대한 접근은 화이트리스트(예: includes/match/length/trim/json.parse/semver.gte)로 제한하고, 평가 결과는 (a)에서 만든 식별자와 함께 출력한다.

(c) **다중 해시 증거 저장부**는 평가 결과의 표준 출력(stdout), 표준 오류(stderr), 종료 코드를 입력으로 받아 두 가지 해시를 산출한다. 첫째는 전체에 대한 sha256 해시이고, 둘째는 라인 또는 JSONPath 표현식으로 지정된 의미 단위별 부분 해시 집합이다. redaction을 거친 출력의 해시가 흔들리지 않도록, 같은 입력에는 같은 위치·같은 토큰으로 치환되는 결정론적 redaction을 적용한다.

(d) **상호 증인 감사 체인부**는 테넌트마다 시퀀스가 단조 증가하는 append-only 감사 엔트리를 hash_i = SHA-256(hash_{i-1} ‖ payloadDigest ‖ meta) 식으로 연결한다. 정해진 주기 또는 이벤트 시점이 되면, 다른 테넌트들의 최신 checkpoint 해시 집합을 자기 체인의 새 엔트리에 fold-in한다. 이렇게 해 두면 운영자가 한 테넌트의 체인을 사후에 위조하더라도, 다른 테넌트가 가진 fold-in 엔트리의 checkpoint hash와 어긋나면서 위조 사실이 그대로 드러난다.

(e) **선택적 공개 검증부**는 외부 감사인에게 발급되는 검증 토큰에 (i) 시간 범위 스코프, (ii) 액션 카테고리 스코프(예: scan·report만 허용하고 사용자 관리는 비공개), (iii) 만료 시각을 담는다. 토큰을 가진 사람은 별도의 계정 없이도 스코프 안의 엔트리·체크포인트·공개키 번들을 조회하여 (d)의 무결성을 직접 다시 계산할 수 있다.

[0006] 본 발명의 방법은 (a)~(e)를 다음 순서로 수행한다.

[0007] 1단계: 로봇을 등록할 때 (a)로 하드웨어 식별자 H_id를 산출하여 로봇 엔터티에 함께 저장한다.

[0008] 2단계: 정책 팩을 설치할 때 팩의 서명을 검증하고, (b)의 WASM 런타임에 평가 규칙을 로드한다.

[0009] 3단계: 감사 세션이 시작되면 체크마다 SSH 명령을 실행하고, (c)로 다중 해시 증거를 저장한 뒤 (b)에서 격리 평가를 수행한다. 마지막으로 결과를 H_id와 결합한 페이로드를 만든다.

[0010] 4단계: 세션 결과를 (d)의 감사 체인에 추가한다. fold-in 주기가 되면, 다른 테넌트의 checkpoint 해시 집합을 담은 cross-witness 엔트리를 따로 추가한다.

[0011] 5단계: 외부 감사인이 검증할 때는 (e)의 검증 토큰을 받아, 별도의 OSS 검증 도구로 체인 재계산·fold-in 상호 증인 검증·공개키 서명 검증을 인터넷 연결 없이 수행한다.

## 발명의 효과

[0012] 본 발명은 다음과 같은 효과가 있다.

(가) 감사 결과가 하드웨어 식별자에 정해진 방식으로 묶여 있기 때문에, 다른 로봇이 결과를 위조할 수 없다.

(나) WASM으로 격리해 실행하므로 악의적인 평가 규칙이 호스트를 침해하는 일을 막으면서도, 정책 팩 생태계의 확장성은 그대로 유지된다.

(다) 다중 해시 증거 덕분에 일부만 바뀌었을 때 바뀐 영역만 다시 평가할 수 있다. 대규모 플릿의 운영 부담을 줄이면서도 감사 무결성은 그대로 지킨다.

(라) 테넌트 간 cross-witness fold-in을 두기 때문에, 운영자가 한 테넌트만 골라 위조하려는 시도를 다른 테넌트가 잡아낸다.

(마) 외부 감사인이 운영자의 계정이나 인터넷 연결 없이도 별도의 OSS 도구로 무결성을 다시 검증할 수 있어, 규제 감사 흐름과 잘 맞는다.

(바) 토픽 QoS·노드 권한·서비스 인증 같은 ROS2 특유의 평가도 같은 격리 실행 틀에서 받아들이므로, 일반 IT 보안 감사 도구로는 다룰 수 없던 영역까지 감사 대상에 포함시킬 수 있다.

## 도면의 간단한 설명

- 도 1: 본 발명의 시스템 전체 구성도. 로봇 식별 결합부(110), 격리 평가 실행부(120), 다중 해시 증거 저장부(130), 상호 증인 감사 체인부(140), 선택적 공개 검증부(150) 사이의 결합 관계를 보여 준다.

- 도 2: 하드웨어 식별자 H_id 산출 흐름도. TPM EK·MAC·CPU serial을 결정론적 결합 함수에 넣어 64자 hex가 나오기까지의 흐름.

- 도 3: 다중 해시 증거 저장 흐름도. raw stdout이 결정론적 redaction을 거쳐 전체 sha256과 부분 sha256 집합으로 나뉘는 과정.

- 도 4: 상호 증인 감사 체인 구조. 테넌트 T1의 체인에 T2·T3의 checkpoint hash가 fold-in되는 모습.

- 도 5: 외부 감사인 검증 토큰의 발급·사용 시퀀스. 운영자가 토큰을 발급하고, 감사인이 OSS 검증 도구로 체인을 재계산하여 결과를 받기까지의 흐름.

- 도 6: 격리 평가 실행부의 WASM 런타임 보안 경계. 호스트 시스템 콜에 대한 화이트리스트 구조.

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

### [실시례 3] 다중 해시 증거 저장 (B-1)

[0017] 다중 해시 증거 저장부(130)는 다음과 같이 동작한다.

```
function storeEvidence(rawStdout, rawStderr, exitCode, partitionRules):
    redacted = deterministicRedact(rawStdout)  // 같은 입력이면 같은 출력
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

    return { full: fullHash, parts: partHashes, redactions: [...] }
```

[0018] 결정론적 redaction은 다음 세 단계로 이루어진다. 먼저 정규식과 엔트로피 기반 패턴 매칭으로 비밀로 보이는 부분을 찾아낸다. 다음으로 비밀의 유형(PEM, JWT, API key 등)별로 같은 길이의 식별자(예: `<REDACTED:PEM:0001>`)로 치환한다. 마지막으로, 같은 입력이라면 같은 식별자 시퀀스가 나오도록 결정론성을 보장한다.

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
    prevHash        string
    hash            string       // sha256(prevHash || payloadDigest || meta || crossWitness)
}

type TenantCheckpoint {
    tenantId  string
    seq       uint64
    hash      string
    signedAt  timestamp
}
```

[0020] fold-in 정책은 다음 세 시점 중 하나에서 발동한다 — 매시간 도래, 엔트리 100개 누적, 사용자가 정의한 트리거. 이 시점에 활성 상태의 다른 모든 테넌트로부터 최신 checkpoint를 모아 새 엔트리에 함께 담는다. 만약 운영자가 테넌트 T1의 과거 엔트리를 나중에 손대면, T2·T3 등이 가진 fold-in 엔트리에 박혀 있던 T1의 checkpoint hash와 어긋나기 때문에 외부 검증자가 곧바로 위조 사실을 잡아낸다.

### [실시례 5] 선택적 공개 검증 (A-3)

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

[0022] 토큰을 받은 외부 감사인이 별도의 OSS 검증 도구(`rosshield-verify`)에 토큰과 export 번들을 넣으면, 도구는 인터넷 연결 없이 다음을 차례로 수행한다 — 토큰 서명 검증, 스코프 안의 엔트리만 추출, 체인 재계산, cross-witness fold-in 검증, 공개키 서명 검증. 스코프를 벗어난 엔트리에 대해서는 도구가 응답하지 않거나 마스킹 처리하여 가린다.

### [실시례 6] 동작 시나리오

[0023] 예를 들어 어느 로봇 운영사가 보유한 12대의 ROS2 로봇 플릿을 ISMS-P 컴플라이언스 기준으로 감사하는 상황을 가정해 보자.

1. 각 로봇을 등록할 때 H_id를 산출하여 함께 저장한다(실시례 1).
2. CIS Ubuntu 24.04 팩과 ROS2 Jazzy 팩을 설치하고, 평가 규칙은 WASM 모듈 형태로 올린다(실시례 2).
3. 감사 세션을 실행하면서 체크마다 나오는 evidence를 다중 해시로 저장한다(실시례 3).
4. 결과를 audit chain에 추가하고, 매시간 다른 테넌트의 checkpoint를 fold-in한다(실시례 4).
5. 분기말 외부 감사 시점이 오면 감사인에게 7일 후 만료되며 scan·report 카테고리만 허용하는 토큰을 발급한다(실시례 5).
6. 감사인은 USB로 export 번들과 토큰을 받아, 인터넷 연결 없이 검증 도구를 돌린다. 결과는 "무결성 OK" 또는 "체인 위반 at seq N" 같은 형태로 받는다.

## 청구의 범위 (raw draft — 변리사 최종화 필요)

> 청구항은 한국 KIPO 정형 문체를 따른다. 이하는 raw draft이며, 변리사가 청구 범위를 최종 확정한다.

**[청구항 1]** ROS2 로봇 플릿의 보안 감사 결과를 검증하는 시스템에 있어서,

각 로봇의 TPM Endorsement Key 인증서, 네트워크 인터페이스 MAC 주소 및 CPU 시리얼을 결정론적 결합 함수에 입력하여 하드웨어 식별자를 산출하는 로봇 식별 결합부;

정책 팩이 동봉한 평가 규칙을 WebAssembly 런타임에서 화이트리스트 호스트 함수로 제한하여 격리 실행하고, 그 결과를 상기 하드웨어 식별자와 결합하는 격리 평가 실행부;

상기 평가 결과의 출력에 결정론적 redaction을 적용한 후 전체 해시와 의미 단위 부분 해시 집합을 산출하는 다중 해시 증거 저장부; 및

테넌트별 단조 증가 시퀀스의 append-only 감사 엔트리를 해시 체인으로 연결하고, 주기적으로 다른 테넌트의 최신 체크포인트 해시를 자기 체인 엔트리에 fold-in하는 상호 증인 감사 체인부;

를 포함하는 것을 특징으로 하는 ROS2 로봇 플릿 보안 감사 결과 검증 시스템.

**[청구항 2]** 제1항에 있어서, 시간 범위·액션 카테고리·만료 시각이 담긴 검증 토큰을 외부 감사인에게 발급하고, 토큰 소지자가 별도 계정 및 인터넷 연결 없이 외부 검증 도구로 상기 감사 체인의 무결성을 재계산할 수 있도록 하는 선택적 공개 검증부를 더 포함하는 것을 특징으로 하는 시스템.

**[청구항 3]** 제1항에 있어서, 상기 격리 평가 실행부의 평가 규칙은 ROS2 토픽의 QoS, publisher 수, subscription 수 또는 암호화 여부 중 하나 이상을 입력으로 받는 ros2_topic_audit, ros2_node_audit, ros2_service_audit 중 적어도 하나의 plugin check type을 포함하는 것을 특징으로 하는 시스템.

**[청구항 4]** 제1항에 있어서, 상기 다중 해시 증거 저장부는 부분 해시가 직전 세션과 동일한 영역에 대해서는 평가를 재사용하고, 변경된 영역에 대해서만 다시 평가를 수행하는 차분 평가부를 더 포함하는 것을 특징으로 하는 시스템.

**[청구항 5]** 제1항에 있어서, 같은 PeerGroup으로 분류된 복수의 로봇에 대해, 동일한 체크의 evidence 부분 해시가 다수결에서 이탈한 로봇을 결정론적으로 검출하는 플릿 상호 검증부를 더 포함하는 것을 특징으로 하는 시스템.

**[청구항 6]** 제1항 내지 제5항 중 어느 한 항의 시스템을 구현하는 컴퓨터 프로그램이 저장된 비일시적 컴퓨터 판독 가능 매체.

**[청구항 7]** ROS2 로봇 플릿의 보안 감사 결과를 검증하는 방법에 있어서, [실시례 6]의 1~6단계를 컴퓨터가 수행하는 것을 특징으로 하는 방법. (변리사가 단계별로 분해 청구항으로 전개해야 한다.)

---

## 변리사 입력용 보조 정보

### 진보성 논거 핵심 키워드
- 결합 신규성: TPM 결합 + WASM 격리 + 다중 해시 + 멀티테넌트 cross-witness + 선택적 공개 검증 토큰. 단일 요소가 아닌 결합의 비자명성에 무게를 둔다.
- 도메인 특화: ROS2 그래프(토픽·노드·서비스). 일반 IT 보안 감사 도구가 다루지 못하는 표면이다.

### 검토할 선행기술
- Sigstore Rekor / Trillian (transparency log)
- Certificate Transparency RFC 6962
- AWS QLDB, Hyperledger Fabric (append-only ledger)
- AWS CloudTrail Log File Validation
- OpenSCAP / CIS-CAT / Wazuh syscheck
- Schneier-Kelsey "Secure Audit Logs" (1998)
- OPA Rego + `opa test`
- Sigstore cosign / policy-controller
- Helm chart provenance

### 도면 작도 우선순위
도 1 → 도 4 → 도 5 → 도 2 → 도 3 → 도 6. 전체 구성도와 cross-witness, 외부 검증 시퀀스를 먼저 그려야 차별성이 잘 드러난다.

### KIPO 분류 후보
- G06F 21/64 (데이터 무결성 검증)
- G06F 21/53 (격리 실행 환경)
- G06F 21/57 (소프트웨어 컴플라이언스 검증)
- H04L 9/32 (공개키 인증·서명)
