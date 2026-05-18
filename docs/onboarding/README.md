# Onboarding — 첫 paying customer 진입 가이드

> **대상**: rosshield Phase 5(E38) 첫 paying customer 또는 PoC 파트너.
> **상태**: v0.2.0 release(`2026-05-08`) 기준. 자료는 best-effort, 정식 SLA는 첫 enterprise 계약과 함께 확정.
> **제품 브랜드**: **Lodestar** 확정(2026-05-18, D-P7-1). 코드 네임스페이스는 `rosshield`(Go 모듈·CLI) 유지.

이 디렉터리는 첫 customer가 **다운로드 → 첫 로그인 → 첫 스캔 → 첫 리포트**까지 30분 내 도달하도록 돕는 자료 모음입니다.

---

## 자료 인덱스

| 파일 | 목적 | 사용 시점 |
|---|---|---|
| [`README.md`](./README.md) | 이 문서. 절차·SLA·연락처 개요. | T-7일 (계약 직전 송부) |
| [`quickstart.md`](./quickstart.md) | 30분 내 첫 admin login + 첫 scan. step-by-step. | T+0 (kickoff 당일) |
| [`walkthrough.md`](./walkthrough.md) | PoC 단계별 명령 + 예상 결과 + 검증 + 트러블슈팅 12개. | T+0 ~ T+7 (1주차 동행 가이드) |
| [`customer-info-template.md`](./customer-info-template.md) | customer가 채워서 보내줄 intake yaml. | T-7일 (계약 직전 수집) |
| [`demo-script.md`](./demo-script.md) | 시연 시나리오·내레이션 스크립트(11 단계 30분, 영상 미제작·텍스트만). | T+14일 (run-through 직전) 또는 sales pitch |

---

## 마일스톤

### 1주차 (T+0 ~ T+7일) — Day-1 가동

| Day | 활동 | 산출물 |
|---|---|---|
| T-7 | intake yaml 회수 (`customer-info-template.md`) | 채워진 yaml 1부 |
| T-7 ~ T-1 | SKU·storage·SSO 결정 확정, license token 발급(community / pro / enterprise) | license token 1개 |
| T+0 | kickoff 미팅(원격 30분) — 환경 요구사항·다운로드·검증 안내 | 회의록 + 다운로드 링크 전달 |
| T+0 | customer 측 자체 설치 — `quickstart.md` 따라 진행 | admin login + 첫 robot 등록 |
| T+1 ~ T+3 | 첫 robot 1대 SSH 연결 + 첫 scan 실행 | scan session 1건 + report PDF 1부 |
| T+5 | 1주차 check-in(원격 15분) — 막힌 지점·fail check 회수 | 이슈 트리아지 |
| T+7 | 1주차 종료 보고 — 등록된 robot 수·완료 scan 수·remediation 진척 | 주간 status mail |

**1주차 성공 기준**: 첫 robot 1대 + 첫 scan 1건 + 첫 PDF report 1부.

### 2주차 (T+8 ~ T+14일) — 운영 안착

| Day | 활동 | 산출물 |
|---|---|---|
| T+8 ~ T+10 | 전체 robot fleet 등록(목표: intake yaml의 `expected_robots` 수) | fleet view 갱신 |
| T+10 | SSO 결선(OIDC 또는 SAML, intake에 명시된 경우) — `/sso` 페이지 | provider 1개 활성 |
| T+10 ~ T+12 | 추가 사용자 초대(`/users` + `/invitations`) — 이메일 또는 수동 token 전달 | 사용자 N명 활성 |
| T+12 | 컴플라이언스 프레임워크 활성화(ISMS-P / ISO27001 / NIST 800-53 중 선택) | 첫 framework snapshot |
| T+13 | webhook 1건 결선(SIEM·Slack·웹훅 endpoint) — `/integrations` | webhook 활성 1건 |
| T+14 | 2주차 종료 보고 + 30일차 마일스톤 합의 | 운영 합의서 |

**2주차 성공 기준**: 전 fleet 등록 + 사용자 ≥ 3명 + framework snapshot 1건 + webhook 1건.

### 30일차 (T+30) — 정착 점검

| 항목 | 합격 기준 |
|---|---|
| 정기 scan 스케줄 | weekly 또는 daily 등록 |
| Audit 체인 head 검증 | 외부 검증 SDK(`rosshield-audit-verify`)로 customer가 자가 검증 가능 |
| Backup 운영 | `rosshield-server backup`을 cron으로 daily, S3 또는 별도 disk로 off-site |
| Findings 처리율 | High 이상 finding 미처리율 ≤ 30% (목표·계약별 조정) |
| 만족도 인터뷰 | 30분 retro — 추천 가능 여부 확인(NPS 대용) |

30일차 점검은 **첫 갱신 의향**과 직결됩니다. 결과는 `phase5-backlog.md`의 E38 진척 노트에 기록.

---

## Support 채널 SLA (Phase 5 best-effort)

> Phase 5는 founder-led 운영입니다. 다음은 정식 SLA가 아닌 **best-effort 약속**입니다. 첫 enterprise 계약 시점에 정식 SLA로 승격합니다.

| 채널 | 응답 약속 | 비고 |
|---|---|---|
| **이메일** (`support@TBD`) | 영업일 24시간 내 1차 응답 | KST 09-18 우선, 그 외 best-effort |
| **Slack 공유 채널** (TBD) | 영업일 4시간 내 1차 응답 | enterprise 계약 고객 한정 (Phase 5 후반 도입) |
| **Discord 커뮤니티** (TBD) | 비공식, 응답 보장 없음 | community edition 사용자 대상 |
| **Critical 사고** (P0/P1) | best-effort 즉시 대응 | "audit 체인 검증 실패" · "데이터 손실" · "전체 인스턴스 down" 만 P0 |

**P0/P1 정의**:
- **P0** — 데이터 손상·audit 체인 깨짐·전 사용자 로그인 불가. 즉시 대응 + RCA 7일 내.
- **P1** — 일부 기능 불가(scan 실행 불가·리포트 생성 실패) + workaround 부재. 영업일 24시간 내 fix 또는 workaround.
- **P2** — 일부 기능 불가 + workaround 존재. 다음 release 포함.
- **P3** — UI 폴리시·문구 등. backlog 등록.

---

## 연락처 (TBD — 첫 customer 직전 확정)

| 역할 | 채널 | 값 |
|---|---|---|
| 1차 contact | 이메일 | `TBD@TBD` |
| 1차 contact | Slack DM | TBD |
| 긴급(P0) | 휴대폰 | TBD (계약 시 NDA 후 전달) |
| 커뮤니티 | Discord 서버 | TBD |
| 보안 신고 | 이메일(별도) | `security@TBD` (PGP key 별 첨부 예정) |

> **정식 도메인 확정 전 우회**: 첫 customer는 임시로 founder의 개인 이메일(`ssabro_k@naver.com`)을 1차 contact로 사용합니다. D1(제품명·도메인) 확정 후 30일 내 정식 채널로 마이그레이션.

---

## Intake API (Phase 6 R1) — yaml 회수 후 자동 provisioning

> **상태**: Phase 6 후보 1 R1 epic 5/5 마감(2026-05-18). intake REST API + auto-provisioning wrap + 운영자 docs 모두 cover. paying customer 0 단계 — license token 발급은 placeholder(별 CLI Ed25519 서명).

### 한계 (paying customer 0 단계)

- **license token 발급은 placeholder** — wrap adapter는 tenant + admin user 시드만 자동화, license token은 별 CLI(`rosshield-pack-tools keygen` 패턴 — 미구현)로 운영자가 별도 발급 후 customer에 secure 채널 전달.
- **admin user 초기 password는 cryptographic random 32B**(base64url) — adapter 내부 일시 보유 후 argon2id 해시로 저장. customer는 별 채널(password reset 또는 invitation token — R4 후속)로 첫 로그인 token 회수. 본 R1 단계에서는 운영자가 customer에 password reset 메일을 수동 발송하거나 `rosshield-server seed admin --reset` 패턴으로 직접 갱신.
- **web admin UI(intake CRUD 페이지)는 미제공** — R4 후속. 본 R1 단계는 REST API 직접 호출만.

### 사용 흐름 (REST API 3단계)

```
1. customer가 customer-info-template.md (yaml) 작성 → 운영자 이메일 송부.
2. 운영자가 yaml → JSON 변환 → POST /api/v1/customers/intake.
3. 운영자가 검토 후 POST /api/v1/customers/intakes/{id}:accept (자동 provisioning) 또는
   POST /api/v1/customers/intakes/{id}:reject (rejection_reason 필수).
```

### 단계별 명령 (curl 예시)

#### 1) 운영자 admin login → JWT 발급

```bash
curl -X POST https://rosshield.example/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ops-admin@rosshield.example","password":"<your-password>"}' \
  | jq -r '.accessToken'
# → eyJhbGciOiJFZERTQSI...  (Bearer token, 별 변수 export 권장: export TOKEN=...)
```

#### 2) POST /api/v1/customers/intake — intake row INSERT (pending)

```bash
curl -X POST https://rosshield.example/api/v1/customers/intake \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "organizationName": "Acme Robotics Corp",
    "primaryContactEmail": "customer-admin@acme.example",
    "primaryContactName": "Acme Admin",
    "planRequest": "pro",
    "intendedUse": "ROS2 fleet 보안 감사 PoC — warehouse-a (50대)."
  }'
```

**응답 (201 Created)**:

```json
{
  "id": "ci_01HV...",
  "organizationName": "Acme Robotics Corp",
  "primaryContactEmail": "customer-admin@acme.example",
  "primaryContactName": "Acme Admin",
  "planRequest": "pro",
  "intendedUse": "ROS2 fleet 보안 감사 PoC — warehouse-a (50대).",
  "status": "pending",
  "createdAt": "2026-05-18T09:00:00.000Z"
}
```

`primaryContactEmail`은 lowercase normalize 후 저장 (`Customer-Admin@Acme.Example` → `customer-admin@acme.example`).

**검증 실패 예** (422 Unprocessable Entity):

- `organizationName` 누락 → `{"error":"intake: OrganizationName is required"}`
- email 형식 오류 → `{"error":"intake: PrimaryContactEmail format invalid"}`
- `planRequest` ∉ {community, pro, enterprise} → `{"error":"intake: PlanRequest is not a known value"}`

#### 3a) POST /customers/intakes/{id}:accept — auto-provisioning

```bash
INTAKE_ID="ci_01HV..."
curl -X POST "https://rosshield.example/api/v1/customers/intakes/${INTAKE_ID}:accept" \
  -H "Authorization: Bearer $TOKEN"
```

**응답 (200 OK)**:

```json
{
  "id": "ci_01HV...",
  "tenantId": "tn_01HV...",
  "organizationName": "Acme Robotics Corp",
  "primaryContactEmail": "customer-admin@acme.example",
  "status": "accepted",
  "createdAt": "2026-05-18T09:00:00.000Z",
  "acceptedAt": "2026-05-18T09:30:00.000Z",
  "acceptedByUserId": "us_01HV..."
}
```

**Accept 시점에 같은 Tx로 자동 실행**:

1. `tenants` 테이블에 새 row INSERT — `name = organizationName`, `plan = mapPlanRequest(planRequest)` (community → `desktop_free`, pro → `desktop_pro`, enterprise → `enterprise`).
2. `users` 테이블에 새 admin user INSERT — `email = primaryContactEmail`, `display_name = primaryContactName`, password = cryptographic random 32B (argon2id 해시).
3. `roles` 테이블에 시스템 역할 3종 시드 (admin·auditor·operator) + admin role을 새 user에 할당.
4. `customer_intakes` 테이블 row UPDATE — `status = 'accepted'`, `tenant_id`, `accepted_at`, `accepted_by_user_id` 채움.

실패 시(예: `tenant.Create`의 ErrInvalidEmail) **Tx rollback** — intake row는 `pending` 유지, tenant 부분 생성 차단.

#### 3b) POST /customers/intakes/{id}:reject — rejection_reason 필수

```bash
curl -X POST "https://rosshield.example/api/v1/customers/intakes/${INTAKE_ID}:reject" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"reason":"SKU desktop_free 외 quota 명시 부족 — re-submit 필요"}'
```

**응답 (200 OK)**:

```json
{
  "id": "ci_01HV...",
  "status": "rejected",
  "createdAt": "2026-05-18T09:00:00.000Z",
  "rejectedAt": "2026-05-18T09:30:00.000Z",
  "rejectionReason": "SKU desktop_free 외 quota 명시 부족 — re-submit 필요"
}
```

Reject는 tenant 미생성 (`tenantId` 응답 필드 없음). reason 빈 값 시 422 `{"error":"intake: RejectionReason is required"}`.

### List·Get (검토 보조)

```bash
# 전체 intake list (DESC 정렬, 최근 50건)
curl -H "Authorization: Bearer $TOKEN" https://rosshield.example/api/v1/customers/intakes | jq

# status 필터
curl -H "Authorization: Bearer $TOKEN" \
  'https://rosshield.example/api/v1/customers/intakes?status=pending' | jq

# 단건 조회 (404 if not found)
curl -H "Authorization: Bearer $TOKEN" \
  "https://rosshield.example/api/v1/customers/intakes/${INTAKE_ID}" | jq
```

잘못된 status 값(`?status=foo`) → 400 `{"error":"invalid status query"}`.

### RBAC

intake 5 endpoint 모두 `ResourceTenantAdmin.Admin` 권한 게이트 — 운영자 admin role 필수. operator·auditor 등 다른 role은 403. 인증 누락 시 401(AuthMiddleware).

intake row 자체는 *tenant 생성 전* 단계 글로벌 데이터(P10 프라이버시 — contact email·use case 포함)이라 read도 admin 전용으로 격리. customer self-service signup은 본 R1 범위 외(별 epic).

### 호환성·후속

- **back-compat**: 본 API 없이 기존 `customer-info-template.md` 수동 회수 → `rosshield-server seed admin` CLI 경로(`admin_seed.go`)도 그대로 동작 — R1은 추가 path.
- **license token 자동 발급**: 첫 paying customer 진입 직전 별 epic(R2 또는 R3 — `phase6-backlog-design.md` 참조).
- **web admin UI**: R4 또는 후속 — 본 R1은 REST API only.

---

## 필수 customer 정보 수집 체크리스트

계약 직전(T-7일)에 [`customer-info-template.md`](./customer-info-template.md) yaml을 회수합니다. 최소 항목:

- [ ] **조직명** (`customer.organization`)
- [ ] **관리자 이름·이메일·timezone** (`customer.contact_admin.*`)
- [ ] **SKU 선택** (`deployment.sku`) — desktop / onprem / appliance
- [ ] **Storage 선택** (`deployment.storage`) — sqlite (단일 인스턴스) / postgres (HA·다중 인스턴스)
- [ ] **예상 robot 수** (`deployment.expected_robots`) — license quota 산정용
- [ ] **예상 사용자 수** (`deployment.expected_users`)
- [ ] **SSO 사용 여부** (`sso.enabled`) + 사용 시 provider·idp_metadata_url·email_domains
- [ ] **License edition** (`license.edition`) — community / pro / enterprise
- [ ] **License quota** (`license.expected_quota.*`) — robots / scans_per_day / llm_tokens_per_day
- [ ] **에어갭 여부** (`network.airgap`) — true 시 pack mirror·telemetry·LLM cloud 강제 off
- [ ] **Public base URL** (`network.public_base_url`) — 초대 이메일 accept URL용
- [ ] **SMTP 설정** (`network.smtp.*`) — 옵션, 미설정 시 invite token 수동 전달 모델

intake yaml 회수 → kickoff 24시간 전 license token 발급 → kickoff에서 token + quickstart link 전달.

---

## 다음 단계

1. **이 문서 송부** (T-7일) — customer 측 1차 contact에게 README + intake template 전달.
2. **intake 회수** (T-3일) — 채워진 yaml 회수 + 누락 항목 phone/mail로 회수.
3. **kickoff 준비** (T-1일) — license token 발급 + quickstart 링크 + 영상 URL(없음·TBD).
4. **kickoff** (T+0) — `quickstart.md`를 화면 공유로 함께 따라가며 첫 admin login까지 동행.

---

## 변경 이력

| 날짜 | 변경 | 작성자 |
|---|---|---|
| 2026-05-11 | 초판 (E38 사전 준비) | rosshield core team |
| 2026-05-13 | demo-script.md 신규(11 단계 30분 시연 시나리오 — TBD 해소) + CIS pack 자동 변환 77.6% 마감 반영(quickstart 시연 단계에서 활용) | rosshield core team |
| 2026-05-15 | walkthrough.md 신규 (Phase 6 후보 1 R2 — PoC 단계별 명령 + 예상 결과 + 트러블슈팅 12개) | rosshield core team |
| 2026-05-18 | §Intake API 섹션 신규 — yaml → REST API → auto-provisioning 절차 + curl 예시 (Phase 6 후보 1 R1 epic 5/5 마감) | rosshield core team |
