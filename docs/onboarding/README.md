# Onboarding — 첫 paying customer 진입 가이드

> **대상**: rosshield Phase 5(E38) 첫 paying customer 또는 PoC 파트너.
> **상태**: v0.2.0 release(`2026-05-08`) 기준. 자료는 best-effort, 정식 SLA는 첫 enterprise 계약과 함께 확정.
> **제품 브랜드**: 미확정(D1 연기). 본 문서의 `<ProductName>` placeholder는 코드네임 `rosshield`로 대체해 읽으세요.

이 디렉터리는 첫 customer가 **다운로드 → 첫 로그인 → 첫 스캔 → 첫 리포트**까지 30분 내 도달하도록 돕는 자료 모음입니다.

---

## 자료 인덱스

| 파일 | 목적 | 사용 시점 |
|---|---|---|
| [`README.md`](./README.md) | 이 문서. 절차·SLA·연락처 개요. | T-7일 (계약 직전 송부) |
| [`quickstart.md`](./quickstart.md) | 30분 내 첫 admin login + 첫 scan. step-by-step. | T+0 (kickoff 당일) |
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
