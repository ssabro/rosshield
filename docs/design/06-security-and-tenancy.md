# 06. 보안 및 테넌시

## 6.1 위협 모델 (STRIDE 요약)

| 위협 | 예시 | 대응 |
|---|---|---|
| **Spoofing** | 가짜 사용자가 관리자 행세 | 강한 인증(SSO·2FA), JWT 서명, API Key 해시 |
| **Tampering** | 감사 로그 조작 | append-only + 해시 체인 + 외부 검증 |
| **Repudiation** | 관리자가 행위 부인 | 감사 로그(행위자·target·payloadDigest) |
| **Information Disclosure** | Evidence의 비밀 유출 | 레덕션, blob 접근 제어, 암호화 |
| **Denial of Service** | 대량 스캔 트리거 | Rate limit, 쿼터, backpressure |
| **Elevation of Privilege** | 사용자가 다른 테넌트 접근 | Tenant middleware + 저장소 레벨 격리 |

## 6.2 보안 아키텍처 계층

```
(1) Network                 TLS 1.3, mTLS(선택), 내부 네트워크 분리
(2) Identity                SSO·2FA·API Key·Session
(3) Authorization           Tenant scope + RBAC
(4) Data at rest            KEK/DEK 암호화, TPM 활용(어플라이언스)
(5) Data in transit         TLS + WebSocket Secure
(6) Application             입력 검증, SQL 파라미터화, CSRF, SSRF 방지
(7) Secrets                 Secret Store, 로테이션
(8) Audit                   append-only 해시 체인, 외부 검증
(9) Supply chain            서명된 팩, SBOM, 의존성 감사
(10) Physical (어플라이언스) Secure Boot, 디스크 암호화
```

## 6.3 테넌시 격리

### 세 가지 격리 수준

| 수준 | 기법 | 적용 |
|---|---|---|
| **Row-level** (기본) | 모든 쿼리 `tenant_id` 필수 | 온프렘·데스크톱 대부분 |
| **Schema-level** (옵션) | PostgreSQL schema per tenant | 대형 엔터프라이즈 |
| **Database-level** | 테넌트별 DB 또는 어플라이언스 1대 | 초민감 고객 (전용 물리 박스) |

### Row-level 강제

- 저장소 기본 클래스가 생성 시 `tenantId`를 주입받고 모든 쿼리에 자동 포함.
- **tenant 누락 쿼리는 컴파일/린트 레벨에서 차단**. raw SQL 사용 시 `-- allow:no-tenant` 주석 필요, 이 주석이 있는 라인은 모두 코드 리뷰 필수.
- 런타임에도 쿼리 파서가 `WHERE` 절에 `tenant_id` 참조가 없으면 경고 + 감사 로그.

### 테넌트 컨텍스트 전파

```
Request
  → Auth middleware → principal.tenantId 결정
  → TenantContext 생성 (async local storage / goroutine-local)
  → 모든 Repository/Service가 컨텍스트에서 tenant 추출
```

## 6.4 RBAC (Role-Based Access Control)

### 기본 역할 (시스템 제공)

| 역할 | 권한 |
|---|---|
| `owner` | 전부 (Tenant 삭제 포함) |
| `admin` | Tenant 관리 외 전부, 사용자 초대·역할 부여 |
| `auditor` | 읽기 전용 + 리포트 서명 |
| `operator` | 스캔 실행·로봇 CRUD, 컴플라이언스 읽기 |
| `viewer` | 읽기 전용 |
| `api` | API Key 사용자, 명시적으로 지정된 scope만 |

### 커스텀 역할

- 테넌트 관리자가 권한 조합으로 생성.
- 시스템 역할은 수정·삭제 불가.

### ABAC 확장 (v2)

- 리소스 레이블 기반 조건(`fleet.tag=production` 인 경우만 write) — 초기 버전엔 포함 안 함.

## 6.5 자격 증명 관리 (Secrets)

### Secret Store 추상화

```
SecretStore interface {
  Put(id, value, meta) 
  Get(id) → (value, meta)
  Rotate(id, newValue)
  Delete(id)              // 소프트: 일정 기간 tombstone
  List(tenantId)
}
```

### 구현체

| 구현 | 사용처 |
|---|---|
| **OS Keychain** | 데스크톱 (macOS Keychain / Windows Credential Manager / libsecret) |
| **File + KEK** | 온프렘 기본. KEK는 환경변수 또는 KMS. |
| **HashiCorp Vault** | 엔터프라이즈 (Enterprise SKU, 옵션) |
| **TPM-sealed** | 어플라이언스 (기기 고유 키) |

### SSH 자격 증명

- **개인키 권장**, 패스워드 저장은 허용하지만 경고.
- Public key-only 모드 — 프라이빗 키는 로봇에 배포하지 않고 Core가 점검 시 SSH agent forward 없이 직접 사용.
- **자동 로테이션 지원**: Core가 주기적으로 새 키 생성 → 로봇 `authorized_keys` 교체 → 이전 키 폐기.

### LLM API 키

- Provider별로 별도 저장.
- 요청 시에만 복호화, 메모리에서 즉시 제로화.
- 감사 로그에는 **해시 prefix만** 남김.

## 6.6 암호화

### 데이터 분류

| 분류 | 예시 | 처리 |
|---|---|---|
| Public | 제품 버전, 공개 팩 목록 | 암호화 불필요 |
| Internal | Robot 이름, Fleet 구조 | 기본 저장(인가 필요) |
| Confidential | 스캔 Evidence | 레덕션 + 접근 감사 |
| Secret | SSH 키, 비밀번호, API 키 | 암호화 저장(DEK) + KEK 분리 |

### 키 계층

```
Master Key (KEK, TPM/Keychain에 봉인)
  └── Tenant Key (각 테넌트별, KEK로 wrap)
       └── Data Key (DEK, 레코드 단위, Tenant Key로 wrap)
            └── Ciphertext
```

- DEK는 레코드별 랜덤. 같은 DEK는 여러 레코드에 재사용하지 않음.
- Tenant Key 로테이션 시 DEK는 재wrap만, 본문 재암호화는 백그라운드로.

### 알고리즘

- 대칭: AES-256-GCM
- 비대칭: Ed25519(서명), X25519(교환)
- 해시: SHA-256(감사 체인), Argon2id(비밀번호·API Key)

## 6.7 전송 보안 (TLS)

- **TLS 1.3 우선**, 1.2 fallback(규제 요건).
- 자체 서명 인증서: 데스크톱 기본. 온프렘은 고객 PKI 또는 Let's Encrypt(인터넷 연결 시).
- mTLS 옵션: API Key 대신 클라이언트 인증서 사용 (엔터프라이즈).
- HSTS 헤더, 최소 취약한 cipher suite 비활성화.

## 6.8 SSH 풀 보안

- **Host key 고정**: 첫 접속 시 기록, 이후 불일치는 즉시 실패 + 경보.
- **명령어 실행은 구조화 AuditCommand만** — raw shell 입력 경로 없음 (플러그인도 `spawn(argv)` 형태로만).
- **쉘 메타문자 화이트리스트**: 벤치마크 팩에 선언된 argv만 허용.
- **출력 크기 제한**: 체크당 기본 10MB, 초과 시 잘라내고 경고.
- **타임아웃**: 체크당 기본 30초, 팩에서 오버라이드 가능.

## 6.9 입력 검증 · OWASP 대응

| 항목 | 대응 |
|---|---|
| SQL Injection | 파라미터화 쿼리만, ORM 또는 prepared. raw SQL은 테스트 시만. |
| XSS | Web UI 전부 React (기본 escape), `dangerouslySetInnerHTML` 사용 금지 |
| CSRF | SameSite=strict 쿠키 + double-submit token (세션 기반 요청만) |
| SSRF | 아웃바운드 HTTP 허용 목록 — LLM provider·Pack Mirror·Webhook만. 내부 IP 범위(10./172.16/192.168/169.254) 기본 차단. |
| Path traversal | 파일 경로는 ID 기반 조회로만, 사용자 입력 path 받지 않음 |
| Deserialization | JSON만 수용, YAML은 팩 로더에서 safe loader |
| ReDoS | 정규식은 사전 정의만, 사용자 입력 정규식은 길이·중첩 제한 + 타임아웃 |
| Open redirect | 리다이렉트 대상은 화이트리스트 |
| Insecure defaults | 기본 암호 비활성, 첫 로그인 강제 변경 |

## 6.10 감사 로그 보안

- **Append-only**: DB 트리거로 UPDATE/DELETE 차단 (P9).
- **해시 체인**: `hash_i = SHA256(hash_{i-1} || payloadDigest_i || meta_i)`
- **Checkpoint 서명**: 일정 주기마다 ChainHead에 기기 키 서명.
- **외부 검증 API**: `/audit/verify`로 체인 재계산·대조.
- **Export 서명**: 감사 로그 내보내기 시 `ndjson + signature.json` 묶음.

상세는 `10-audit-and-observability.md`.

## 6.11 에어갭 시나리오

### 사전 준비

1. 고객 환경에 **팩 미러**(내부 HTTPS 서버) 구축 또는 USB 전달 채널 합의.
2. TLS 인증서: 자체 CA 또는 고객 PKI.
3. 관리자 계정·역할 초기화: USB에 든 bootstrap 스크립트.

### 운영

1. 제품 업데이트: 서명된 OCI 이미지 번들을 USB로 반입 → 내부 레지스트리 → 롤링 업데이트.
2. 팩 업데이트: 서명된 팩 번들(tar.sig) → 내부 미러 → 자동 설치.
3. LLM 사용 원하면 **Ollama + 로컬 모델 이미지**도 USB로 반입.

### 제약

- Webhook 대상이 내부망에 있어야 함.
- 텔레메트리 완전 off.
- CRL/OCSP 접근 불가 → 인증서 만료 모니터링 대체 수단 필요.

## 6.12 공급망 보안 (Supply Chain)

- **의존성 감사**: `npm audit` / `govulncheck` CI 강제.
- **SBOM 생성**: CycloneDX·SPDX 형식 릴리스마다 동봉.
- **서명된 릴리스**: GitHub Releases sigstore 서명 또는 자체 키.
- **Reproducible build** 목표: 동일 소스 → 동일 바이너리 해시.
- **팩 서명**: 공급자 개인키로 서명, 제품은 공개키 번들로 검증.

## 6.13 비밀 노출 사고 대응

### 탐지

- 로그·감사 엔트리에 민감 패턴이 들어가지 않도록 로거에 redaction 필터.
- Evidence 저장 시 이미 레덕션. 사후 발견 시 tombstone.

### 대응 절차

1. 관련 credential 즉시 revoke (ApiKey·SSH 키·세션).
2. 로봇 쪽 `authorized_keys` 교체 가이드 제공.
3. 감사 로그에 사건 기록 (`security.incident`).
4. 영향받은 테넌트 관리자에게 알림 (이메일·Webhook).

## 6.14 페넌트레이션 테스트 준비

- 릴리스 전 **3rd-party pentest** 권장 (Enterprise SKU 출시 전 필수).
- OSS 공개 시 **security.md**에 취약점 제보 경로 명시.
- CVE 발급 프로세스 정립.

## 6.15 컴플라이언스·규제 (제품 자신의 준수)

| 규제 | 대응 |
|---|---|
| ISMS-P | 감사 로그·접근 통제·암호화·취약점 관리 체크 |
| GDPR | 개인정보 처리자 역할, DPA 템플릿, 삭제 요청 지원 |
| SOC 2 | 로그·변경관리·접근 통제·감사 |
| Korean 개인정보보호법 | 동의 기반 수집, 국외 이전 제한, 처리 위탁 고지 |
| CCPA | 판매·공유 옵트아웃 (B2B 제품 특성상 제한적) |

## 6.16 Secure Development Lifecycle

| 단계 | 활동 |
|---|---|
| Design | 위협 모델링 문서(각 feature마다 섹션) |
| Code | SAST (GoSec/Semgrep), 린트 규칙(raw SQL·unsafe·dangerouslySetInnerHTML 금지) |
| Review | 보안 민감 변경은 2인 승인 |
| Test | 단위·통합·E2E + fuzz 테스트(API 파서, 팩 로더) |
| Release | SBOM, 서명, 릴리스 노트에 보안 fix 명시 |
| Deploy | 최소 권한, 기본 secure configuration |
| Operate | 로그 모니터링, 이상 탐지(내부), 정기 3rd-party 감사 |

## 6.17 이 문서의 핵심 결정

1. **테넌시는 저장소 레벨에서 강제**, 애플리케이션 레이어의 실수로 깨질 수 없게.
2. **KEK/DEK 2계층 암호화**, 어플라이언스는 TPM 봉인.
3. **감사 로그는 append-only + 해시 체인 + 외부 검증**.
4. **SSH 명령은 구조화된 argv만**, raw shell 없음.
5. **에어갭 프로필**이 일등급 시나리오로 설계.
6. **공급망 보안**: 서명된 팩·SBOM·Reproducible build 목표.

다음 문서: [07-scan-engine-and-benchmarks.md](./07-scan-engine-and-benchmarks.md)
