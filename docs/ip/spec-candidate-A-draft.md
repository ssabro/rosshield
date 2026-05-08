# 명세서 초안 — 후보 A 결합 청구항

> **이 문서의 위치**: 본 명세서는 변리사 컨설팅 입력용 raw draft입니다. KIPO 명세서 양식을 따르되 청구항·도면·심사 응답은 변리사가 최종화합니다. 본 문서를 그대로 출원하지 마십시오.
>
> **참조**: `docs/design/13-patent-strategy.md` §13.5 1순위 결합 청구항 (D8-2)

---

## 발명의 명칭

ROS2 로봇 플릿의 보안 감사 결과를 하드웨어 식별자에 결합하고 멀티테넌트 상호 증인 해시 체인으로 외부 검증 가능하게 하는 시스템 및 방법
(System and Method for Verifying Security Audit Results of ROS2 Robot Fleets Bound to Hardware Identifiers via Multi-Tenant Cross-Witness Hash Chain)

## 기술 분야

본 발명은 ROS2(Robot Operating System 2) 기반 로봇 플릿의 보안 구성 상태를 결정론적으로 감사하고, 그 결과의 무결성을 외부 감사인이 별도 계정·인터넷 접속 없이 재검증할 수 있도록 하는 시스템 및 방법에 관한 것이다.

## 발명의 배경이 되는 기술

[0001] 산업 자동화·물류·의료·방산 분야에서 ROS2 기반 로봇 플릿의 도입이 확산됨에 따라 해당 플릿이 정의된 보안 기준(예: CIS Benchmark, ISMS-P, IEC 62443)을 지속적으로 준수함을 감사인·규제기관에 증명할 필요성이 증대되고 있다.

[0002] 종래의 보안 감사 도구(OpenSCAP, CIS-CAT 등)는 단일 호스트 또는 IT 자산 중심으로 설계되어 있고, 다음과 같은 한계를 갖는다:

(1) 감사 결과를 특정 하드웨어 식별자에 결합하지 않아 결과 위조 가능성이 존재한다.

(2) 감사 결과의 무결성을 단일 운영자 환경에서 보장하는 해시 체인은 운영자 자신에 의한 위조를 방어하지 못한다.

(3) 외부 감사인이 결과를 재검증하기 위해 운영자의 계정·API 접속이 필요하여 독립 검증성이 약하다.

(4) 평가 규칙을 호스트 코드와 동일 권한으로 실행하여 악의적 정책 팩에 의한 호스트 침해 위험이 있다.

(5) ROS2 토픽·노드·QoS 같은 도메인 특화 보안 표면을 다루지 못한다.

[0003] 한편 Sigstore Rekor·Certificate Transparency 등 변조 방지 로그 시스템은 글로벌 단일 로그를 전제로 하여 다중 테넌트 격리 환경에 직접 적용하기 어렵고, 외부 감사인 워크플로(스코프·만료가 있는 무계정 검증)를 지원하지 않는다.

## 해결하려는 과제

[0004] 본 발명은 다음 과제를 해결하는 것을 목적으로 한다:

(가) ROS2 로봇 플릿의 보안 감사 결과를 해당 로봇의 하드웨어 식별자에 결정론적으로 결합하여, 다른 로봇이 결과를 위조할 수 없도록 한다.

(나) 평가 규칙을 격리된 실행 환경에서 수행하여 정책 팩에 의한 호스트 권한 침해를 차단하면서도 동일 인터페이스로 다양한 평가 규칙을 수용한다.

(다) 감사 결과의 증거 출력에 대해 다중 해시(전체 + 부분)를 산출하여 부분 변경 시 변경 영역만 재평가하면서도 감사 무결성을 유지한다.

(라) 다중 테넌트 환경에서 단일 운영자가 단일 테넌트 체인을 위조하더라도 다른 테넌트가 상호 증인이 되어 위조를 검출할 수 있도록 한다.

(마) 외부 감사인이 운영자 계정·인터넷 접속 없이도 감사 결과의 무결성을 재검증할 수 있도록 한다.

## 과제의 해결 수단

[0005] 상기 과제를 해결하기 위한 본 발명의 시스템은 다음 구성부를 포함한다:

(a) **로봇 식별 결합부**: 각 로봇의 TPM(Trusted Platform Module) Endorsement Key 인증서, 하나 이상의 네트워크 인터페이스 MAC 주소, CPU 시리얼 번호를 입력으로 하여 결정론적 결합 함수 H_id = SHA-256(TPM_EK_pubkey ‖ sorted(MACs) ‖ CPU_serial)로 하드웨어 식별자를 산출한다.

(b) **격리 평가 실행부**: 정책 팩이 동봉한 평가 규칙을 WebAssembly(WASM) 런타임에서 실행하되, 호스트 시스템 콜 접근을 화이트리스트 함수(includes/match/length/trim/json.parse/semver.gte 등)로 제한하고, 평가 결과를 (a)의 식별자와 함께 출력한다.

(c) **다중 해시 증거 저장부**: 평가 결과의 표준 출력(stdout)·표준 오류(stderr)·종료 코드를 입력으로 하여 (i) 전체 sha256 해시, (ii) 의미 단위(line 또는 JSONPath 표현식으로 지정된 영역)별 부분 해시 집합을 산출하고, redaction 처리된 출력의 해시 안정성을 보장하기 위해 결정론적 redaction 알고리즘(동일 입력에 동일 위치·동일 토큰으로 치환)을 적용한다.

(d) **상호 증인 감사 체인부**: 테넌트별 단조 증가 시퀀스의 append-only 감사 엔트리를 hash_i = SHA-256(hash_{i-1} ‖ payloadDigest ‖ meta) 식으로 연결하되, 미리 정해진 주기 또는 이벤트 시점에 다른 테넌트의 최신 checkpoint 해시 집합을 자기 체인의 새 엔트리에 fold-in하여, 단일 운영자가 단일 테넌트 체인을 위조할 경우 다른 테넌트의 해당 fold-in 엔트리가 위조 사실을 증명한다.

(e) **선택적 공개 검증부**: 외부 감사인에게 발급되는 검증 토큰에 (i) 시간 범위 스코프, (ii) 액션 카테고리 스코프(예: scan·report만 허용, 사용자 관리 비공개), (iii) 만료 시각을 박아 두고, 토큰 소지자가 별도 계정 없이 해당 스코프 내 엔트리·체크포인트·공개키 번들을 조회하여 (d)의 무결성을 재계산할 수 있도록 한다.

[0006] 본 발명의 방법은 (a)~(e)의 동작을 다음 순서로 수행한다:

[0007] 1단계: 로봇 등록 시 (a)를 통해 하드웨어 식별자 H_id를 산출하여 로봇 엔터티에 결합 저장한다.

[0008] 2단계: 정책 팩 설치 시 팩의 서명을 검증하고 (b)의 WASM 런타임에 평가 규칙을 로드한다.

[0009] 3단계: 감사 세션이 시작되면 각 체크별로 SSH 명령 실행 → (c)의 다중 해시 증거 저장 → (b)의 격리 평가 실행 → 결과를 H_id와 결합한 페이로드 생성 순으로 처리한다.

[0010] 4단계: 세션 결과를 (d)의 감사 체인에 append하고, fold-in 주기에 도달한 경우 다른 테넌트의 checkpoint 해시 집합을 포함하는 cross-witness 엔트리를 추가한다.

[0011] 5단계: 외부 감사인 검증 시 (e)의 검증 토큰을 발급받아 별도 OSS 검증 도구로 체인 재계산, fold-in cross-witness 검증, 공개키 서명 검증을 인터넷 접속 없이 수행한다.

## 발명의 효과

[0012] 본 발명에 의하면 다음 효과를 얻을 수 있다:

(가) 감사 결과가 하드웨어 식별자에 결정론적으로 결합되므로 다른 로봇이 결과를 위조할 수 없다.

(나) WASM 격리 실행으로 악의적 평가 규칙에 의한 호스트 침해를 차단하면서 정책 팩 생태계의 확장성을 유지한다.

(다) 다중 해시 증거로 부분 변경 시 변경 영역만 재평가하여 대규모 플릿 운영 부담을 절감하면서도 감사 무결성을 보존한다.

(라) 테넌트 간 cross-witness fold-in으로 단일 운영자에 의한 단일 테넌트 위조를 다른 테넌트가 검출할 수 있다.

(마) 외부 감사인이 운영자 계정·인터넷 접속 없이도 별도 OSS 도구로 무결성을 재검증할 수 있어 규제 감사 워크플로에 부합한다.

(바) ROS2 도메인 특화 평가(토픽 QoS·노드 권한·서비스 인증)를 동일 격리 실행 프레임으로 수용하여 일반 IT 보안 감사 도구로 다룰 수 없는 표면을 감사 대상에 포함한다.

## 도면의 간단한 설명

- 도 1: 본 발명의 시스템 전체 구성도. 로봇 식별 결합부(110), 격리 평가 실행부(120), 다중 해시 증거 저장부(130), 상호 증인 감사 체인부(140), 선택적 공개 검증부(150)의 결합 관계.

- 도 2: 하드웨어 식별자 H_id 산출 흐름도. TPM EK·MAC·CPU serial → 결정론적 결합 함수 → 64자 hex 출력.

- 도 3: 다중 해시 증거 저장 흐름도. raw stdout → 결정론적 redaction → 전체 sha256 + 부분 sha256 집합.

- 도 4: 상호 증인 감사 체인 구조. 테넌트 T1의 체인에 T2·T3 checkpoint hash가 fold-in되는 엔트리 표시.

- 도 5: 외부 감사인 검증 토큰 발급·사용 시퀀스. 운영자 → 토큰 발급 → 감사인 → OSS 검증 도구 → 체인 재계산 결과 출력.

- 도 6: 격리 평가 실행부의 WASM 런타임 보안 경계. 호스트 시스템 콜 화이트리스트.

## 발명을 실시하기 위한 구체적인 내용

### [실시례 1] 하드웨어 식별자 산출 (D-3)

[0013] 로봇 식별 결합부(110)는 다음 의사 코드로 구현된다:

```
function computeHardwareFingerprint(robot):
    tpmEkPub  = readTPMEKCertificate(robot)  // X.509 인증서의 공개키 부분
    macList   = sorted(readNetworkInterfaceMACs(robot))  // 정렬로 결정론성 보장
    cpuSerial = readCPUSerial(robot)  // /proc/cpuinfo 또는 dmidecode

    canonical = tpmEkPub + "|" + join(macList, ",") + "|" + cpuSerial
    return sha256_hex(canonical)
```

[0014] 정렬·구분자 사용으로 동일 로봇은 시점·실행 환경에 무관하게 항상 동일 식별자를 산출한다. TPM이 부재한 로봇은 EK 부분을 빈 문자열로 두되 그 사실을 메타데이터에 기록한다.

### [실시례 2] WASM 격리 평가 실행부 (C-1)

[0015] 격리 평가 실행부(120)는 Wasmtime 또는 Wasmer 런타임을 임베딩하여 다음 인터페이스를 제공한다:

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

[0016] 호스트 함수 화이트리스트는 `includes`, `match`, `length`, `trim`, `json.parse`, `semver.gte` 등으로 한정하며, 파일 시스템·네트워크·시스템 콜 접근은 모두 차단된다.

### [실시례 3] 다중 해시 증거 저장 (B-1)

[0017] 다중 해시 증거 저장부(130)는 다음 절차로 동작한다:

```
function storeEvidence(rawStdout, rawStderr, exitCode, partitionRules):
    redacted = deterministicRedact(rawStdout)  // 동일 입력 → 동일 출력
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

[0018] 결정론적 redaction은 (i) 정규식·엔트로피 기반 패턴 매칭으로 비밀 후보를 식별하고, (ii) 비밀의 유형(PEM·JWT·API key)별로 동일 길이의 식별자(예: `<REDACTED:PEM:0001>`)로 치환하며, (iii) 같은 입력은 같은 식별자 시퀀스로 치환되도록 결정론성을 보장한다.

### [실시례 4] 상호 증인 감사 체인 (A-1)

[0019] 상호 증인 감사 체인부(140)는 다음 구조의 엔트리를 관리한다:

```
type AuditEntry {
    tenantId        string
    seq             uint64       // tenant 내 단조 증가
    occurredAt      timestamp
    actor           Actor
    action          string
    target          Target
    payloadDigest   string       // sha256 of canonical JSON
    crossWitness    []TenantCheckpoint  // optional, fold-in 시에만
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

[0020] fold-in 정책: (i) 매시간 또는 (ii) 100개 엔트리마다 또는 (iii) 사용자 정의 트리거 시점에 다른 모든 활성 테넌트의 최신 checkpoint를 수집하여 새 엔트리에 포함한다. 단일 운영자가 테넌트 T1의 과거 엔트리를 사후 변조하면 T2·T3 등이 보유한 fold-in 엔트리의 T1 checkpoint hash와 불일치가 발생하여 외부 검증자가 위조 사실을 검출한다.

### [실시례 5] 선택적 공개 검증 (A-3)

[0021] 선택적 공개 검증부(150)는 다음 토큰 구조를 사용한다:

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

[0022] 토큰을 받은 외부 감사인은 별도 OSS 검증 도구(`rosshield-verify`)에 토큰과 export 번들을 입력하면 도구가 (i) 토큰 서명 검증, (ii) 스코프 내 엔트리만 추출, (iii) 체인 재계산, (iv) cross-witness fold-in 검증, (v) 공개키 서명 검증을 인터넷 없이 수행한다. 스코프 외 엔트리는 도구가 응답하지 않거나 마스킹 처리한다.

### [실시례 6] 동작 시나리오

[0023] 한 예로, 로봇 운영사가 자사 12대의 ROS2 로봇 플릿에 대해 ISMS-P 컴플라이언스 감사를 수행하는 경우:

1. 각 로봇 등록 시 H_id 산출·저장 (실시례 1).
2. CIS Ubuntu 24.04 팩 + ROS2 Jazzy 팩 설치, 평가 규칙은 WASM 모듈로 (실시례 2).
3. 감사 세션 실행, 각 체크별 evidence를 다중 해시로 저장 (실시례 3).
4. 결과를 audit chain에 append, 매시간 cross-witness fold-in (실시례 4).
5. 분기말 외부 감사 시 감사인에게 7일 만료, scan·report 카테고리만 허용하는 토큰 발급 (실시례 5).
6. 감사인이 USB로 export 번들 + 토큰을 받아 인터넷 없이 검증 도구 실행, "무결성 OK" 또는 "체인 위반 at seq N" 출력.

## 청구의 범위 (raw draft — 변리사 최종화 필요)

**[청구항 1]** ROS2 로봇 플릿의 보안 감사 결과를 검증하는 시스템에 있어서,

각 로봇의 TPM Endorsement Key 인증서·네트워크 인터페이스 MAC 주소·CPU 시리얼을 결정론적 결합 함수에 입력하여 하드웨어 식별자를 산출하는 로봇 식별 결합부;

정책 팩이 동봉한 평가 규칙을 WebAssembly 런타임에서 화이트리스트 호스트 함수로 제한하여 격리 실행하고 결과를 상기 하드웨어 식별자와 결합하는 격리 평가 실행부;

상기 평가 결과의 출력에 대해 결정론적 redaction을 적용한 후 전체 해시와 의미 단위 부분 해시 집합을 산출하는 다중 해시 증거 저장부;

테넌트별 단조 증가 시퀀스의 append-only 감사 엔트리를 해시 체인으로 연결하고, 주기적으로 다른 테넌트의 최신 체크포인트 해시를 자기 체인 엔트리에 fold-in하는 상호 증인 감사 체인부;

를 포함하는 것을 특징으로 하는 ROS2 로봇 플릿 보안 감사 결과 검증 시스템.

**[청구항 2]** 제1항에 있어서, 시간 범위·액션 카테고리·만료 시각이 박힌 검증 토큰을 외부 감사인에게 발급하고, 토큰 소지자가 별도 계정·인터넷 접속 없이 외부 검증 도구로 상기 감사 체인의 무결성을 재계산할 수 있도록 하는 선택적 공개 검증부를 더 포함하는 것을 특징으로 하는 시스템.

**[청구항 3]** 제1항에 있어서, 상기 격리 평가 실행부의 평가 규칙은 ROS2 토픽의 QoS·publisher 수·subscription 수·암호화 여부 중 하나 이상을 입력으로 받는 ros2_topic_audit, ros2_node_audit, ros2_service_audit 중 적어도 하나의 plugin check type을 포함하는 것을 특징으로 하는 시스템.

**[청구항 4]** 제1항에 있어서, 상기 다중 해시 증거 저장부는 부분 해시가 직전 세션과 동일한 영역에 대해 평가를 재사용하고 변경된 영역만 재평가하는 차분 평가부를 더 포함하는 것을 특징으로 하는 시스템.

**[청구항 5]** 제1항에 있어서, 같은 PeerGroup으로 분류된 복수의 로봇에 대해 동일 체크의 evidence 부분 해시가 다수결에서 이탈한 로봇을 결정론적으로 검출하는 플릿 상호 검증부를 더 포함하는 것을 특징으로 하는 시스템.

**[청구항 6]** 제1항 내지 제5항 중 어느 한 항의 시스템을 구현하는 컴퓨터 프로그램이 저장된 비일시적 컴퓨터 판독 가능 매체.

**[청구항 7]** ROS2 로봇 플릿의 보안 감사 결과를 검증하는 방법에 있어서, [실시례 6]의 1~6단계를 컴퓨터가 수행하는 것을 특징으로 하는 방법. (변리사가 단계별로 분해 청구항 작성 필요)

---

## 변리사 입력용 보조 정보

### 진보성 논거 핵심 키워드
- 결합 신규성: TPM 결합 + WASM 격리 + 다중 해시 + 멀티테넌트 cross-witness + 선택적 공개 검증 토큰 — 단일 요소가 아닌 결합의 비자명성
- 도메인 특화: ROS2 그래프(토픽·노드·서비스) — 일반 IT 보안 감사 도구가 다루지 못하는 표면

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
도 1 > 도 4 > 도 5 > 도 2 > 도 3 > 도 6 (전체 시스템도와 cross-witness·외부 검증 시퀀스가 가장 차별성 가시화)

### KIPO 분류 후보
- G06F 21/64 (데이터 무결성 검증)
- G06F 21/53 (격리 실행 환경)
- G06F 21/57 (소프트웨어 컴플라이언스 검증)
- H04L 9/32 (공개키 인증·서명)
