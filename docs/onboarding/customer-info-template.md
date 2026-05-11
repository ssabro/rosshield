# Customer intake template

> **목적**: 첫 paying customer 또는 PoC 파트너가 채워서 보내줄 onboarding 정보 양식.
> **반환 채널**: 1차 contact 이메일 (TBD — `README.md` 연락처 참조).
> **반환 시점**: 계약 직전(T-7일) 또는 PoC kickoff 직전(T-3일).
> **포맷**: 아래 yaml 블록을 그대로 복사 → 수정 → 첨부 또는 본문 붙여넣기. 비어 있는 필드는 빈 문자열·빈 배열·`0`·`false` 그대로 두지 말고 **미정 시 `TBD`** 명시.

```yaml
# rosshield onboarding intake (v1)
# 작성일: YYYY-MM-DD
# 작성자: <이름> <이메일>

customer:
  # 조직 공식 명칭 (계약서·license token에 사용)
  organization: "Acme Robotics"

  contact_admin:
    # rosshield admin user로 시드될 사람. 1차 운영 책임자.
    name: ""
    email: ""
    # IANA timezone (예: Asia/Seoul, America/Los_Angeles, UTC).
    # 스케줄 스캔·리포트 timestamp 표시에 사용.
    timezone: "Asia/Seoul"

deployment:
  # 배포 SKU 결정. 미결정이면 TBD — 1주차 안에 합의.
  sku: ""              # desktop | onprem | appliance (TBD)

  # 영속 저장소. SQLite는 단일 인스턴스·소규모, Postgres는 다중 인스턴스·HA·대규모.
  # desktop SKU는 SQLite 강제, appliance는 둘 다 가능, onprem은 Postgres 권장.
  storage: ""          # sqlite | postgres

  # 등록 예정 robot 수 (license quota 산정용).
  # 정확한 수가 없으면 상한값으로 답변 (예: 50).
  expected_robots: 0

  # 등록 예정 사용자 수(admin + auditor + operator 합계).
  expected_users: 0

sso:
  # SSO 활성 여부. false면 로컬 계정 + invitation token만 사용.
  enabled: false

  # provider 종류 — enabled=true 시 필수.
  provider: ""         # oidc | saml

  # IdP metadata URL.
  # OIDC: discovery URL (예: https://login.example.com/.well-known/openid-configuration)
  # SAML: IdP metadata XML URL 또는 첨부 파일 경로
  idp_metadata_url: ""

  # 자동 프로비저닝 허용 이메일 도메인 화이트리스트.
  # 예: ["acme.com", "acme-robotics.com"] — 이 도메인의 SSO 사용자만 자동 user 생성.
  email_domains: []

license:
  # License edition. quota·feature 게이트에 사용.
  # community = free, 기능 일부 제한 / pro = 단일 tenant + 전 기능 / enterprise = 다중 tenant + 우선 지원
  edition: ""          # community | pro | enterprise

  # 라이선스 quota — license token 발급 시 인입.
  # 0이면 unlimited(enterprise만 가능). community/pro는 양수값 필수.
  expected_quota:
    robots: 0                # 동시 등록 robot 수 상한
    scans_per_day: 0         # 하루 scan 실행 수 상한 (각 robot 1회 = scan 1건)
    llm_tokens_per_day: 0    # advisor LLM 사용량 상한 (LLM 옵트인 시만 의미)

network:
  # 에어갭 환경 여부. true면 pack mirror·telemetry·LLM cloud provider가 강제 off.
  # 본인 LLM(Ollama 로컬)은 가능.
  airgap: false

  # public 접근 base URL (HTTPS 권장).
  # 초대 이메일의 accept URL용 (예: https://rosshield.acme.com).
  # 에어갭 환경이거나 단일 머신 데스크톱이면 빈 문자열 — invite token 수동 전달 모델.
  public_base_url: ""

  # SMTP — 옵션. 미설정 시 invite는 stdout JSON으로 token만 표출되고 admin이 수동 전달.
  smtp:
    enabled: false
    host: ""              # 예: smtp.gmail.com, smtp.acme.com
    port: 587             # 587 (STARTTLS, 권장) 또는 465 (TLS) 또는 25 (cleartext, non-prod)
    user: ""              # 예: noreply@acme.com
    # 패스워드는 ROSSHIELD_SMTP_PASSWORD env로 별도 전달. 본 yaml에 평문 기재 금지.

notes: |
  # 자유 기술. 특수 요구사항·기존 인프라 제약·계약상 추가 의무 등.
  # 예시:
  # - SIEM은 Splunk Cloud, webhook URL은 추후 별도 송부
  # - 에어갭이지만 월 1회 USB로 pack bundle 반입 가능
  # - 첫 30일은 PoC 무상 → 첫 갱신 시 정식 enterprise 계약 전환 합의
```

---

## 작성 가이드

### 필수 vs 선택

| 필드 | 필수 여부 | 비고 |
|---|---|---|
| `customer.organization` | 필수 | license token에 인입 |
| `customer.contact_admin.{name,email,timezone}` | 필수 | admin seed 명령 인자 |
| `deployment.sku` | 필수 | TBD 가능 (1주차 결정) |
| `deployment.storage` | 필수 | SKU에 따라 자동 결정 가능 |
| `deployment.expected_{robots,users}` | 필수 | 정확값 없으면 상한 추정 |
| `sso.*` | `enabled=true` 시만 필수 | false면 무시됨 |
| `license.edition` | 필수 | community 시작 → pro/enterprise 승급 가능 |
| `license.expected_quota.*` | edition별 필수 | enterprise는 0=unlimited 허용 |
| `network.airgap` | 필수 (boolean) | true 시 pack/telemetry/LLM 자동 잠금 |
| `network.public_base_url` | 권장 | invite email accept URL용. 빈값 시 token 수동 전달 |
| `network.smtp.*` | 선택 | 없으면 invite stdout JSON 모델 |

### 보안 주의

- **SMTP 패스워드**는 본 yaml에 평문 기재 **금지**. `ROSSHIELD_SMTP_PASSWORD` 환경변수로 별도 전달(가능하면 PGP 암호화 메일 또는 1Password 공유 vault).
- **License token** 자체도 본 yaml에 포함하지 않습니다. license token은 intake 회수 후 별도로 발급 → 별도 secure 채널로 전달.
- **admin 패스워드**는 customer가 첫 부팅 시 직접 설정합니다(intake에 평문 기재 금지). quickstart §1.1 참조.

### 변경 이력

| 날짜 | 변경 | 작성자 |
|---|---|---|
| 2026-05-11 | 초판 v1 (E38 사전 준비) | rosshield core team |
