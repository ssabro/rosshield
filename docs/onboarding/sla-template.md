# SLA 합의 Template — rosshield Lodestar

> **목적**: rosshield(`Lodestar`) 운영자와 customer 사이의 서비스 수준(SLA) 합의용 markdown template입니다. customer가 본 파일을 fork·복사해 placeholder를 채우고 양측 서명으로 합의를 확정합니다.
> **상태**: paying customer 0인 현 단계의 **draft**입니다. 모든 수치는 보수적 default이며 첫 enterprise 계약 체결 시점에 정식 SLA로 승격됩니다.
> **참조**:
> - `docs/onboarding/README.md` — onboarding 전체 흐름 + Phase 5 best-effort 표.
> - `docs/onboarding/support-channels.md` — P0/P1/P2/P3 정의 + 첫 응답 SLA + 보고 양식.
> - `docs/onboarding/license-lifecycle.md` (R1 Stage 3 산출 예정) — license 만료·갱신 일정.
> - 설계서 `docs/design/01-principles.md` — 결정성·에어갭·불변성 원칙(SLA 면제 조건과 직결).

---

## 1. SLA 정의

### 1.1 적용 범위

본 SLA는 다음을 cover합니다:

- rosshield server(`rosshield-server`)의 가용성(uptime).
- HTTP API(`/api/v1/*`)의 응답 시간(p95).
- Audit chain의 무결성 보장(외부 검증 가능).
- Critical 사고(P0/P1)에 대한 1차 응답·복구 시간.

본 SLA는 다음을 cover하지 않습니다:

- customer 자가 운영 인프라(SSH 대상 robot, 네트워크, storage volume).
- 외부 의존(SMTP·SAML IdP·LLM provider — opt-in 시).
- pack 컨텐츠 자체의 정확성(컨텐츠/코드 분리 원칙 §8 — pack은 별 라이프사이클).

### 1.2 Edition별 약속 수치 (보수 default)

| 항목 | community | pro | enterprise |
|---|---|---|---|
| **Uptime SLO (월)** | 명시 X (best-effort) | `<99.0% / 협상>` | `<99.5% / 협상>` |
| **HTTP API 응답 p95** | best-effort | `<2.0초 / 협상>` | `<1.0초 / 협상>` |
| **MTTR P0** | 명시 X | `<영업일 8시간 / 협상>` | `<24/7 4시간 / 협상>` |
| **MTTR P1** | 명시 X | `<영업일 24시간 / 협상>` | `<영업일 8시간 / 협상>` |
| **응답 시간 P0** (1차 응답까지) | best-effort | `<4시간 / 협상>` | `<1시간 / 협상>` |
| **응답 시간 P1** (1차 응답까지) | best-effort | `<영업일 8시간 / 협상>` | `<영업일 4시간 / 협상>` |
| **Audit chain 검증 보장** | 모두 (외부 SDK) | 모두 | 모두 + 분기별 chain head 합의 |
| **데이터 손실 RPO** | best-effort | `<24시간 (daily backup) / 협상>` | `<4시간 (4시간 cron) / 협상>` |
| **계획된 유지보수 windows** | 미정 | `<월 1회 4시간 / 협상>` | `<분기 1회 2시간 + 사전 7일 통보 / 협상>` |
| **SLA 보상 조항** | 없음 | `<uptime 99.0% 미달 시 다음 달 10% credit / 협상>` | `<uptime 99.5% 미달 시 다음 달 20% credit / 협상>` |
| **계약 변경 통보** | EULA 변경만 | 30일 통보 | 90일 통보 |

> **수치 해석**: 모든 수치는 **paying customer 0인 현 단계 보수 default**입니다. 첫 enterprise 계약 체결 시점에 customer 인프라(on-prem vs appliance vs desktop), founder-led 운영 capacity, on-call rotation 가능 여부에 맞춰 합의로 확정합니다.

### 1.3 customer 합의용 placeholder

customer는 다음을 채워서 양측 서명합니다.

```
조직명: <customer.organization>
계약 edition: <community / pro / enterprise>
계약 시작일: <YYYY-MM-DD>
계약 종료일: <YYYY-MM-DD>
SLA 유효 기간: <계약 시작일과 동일 / 별도 협상>

------- 합의 수치 (1.2 표 default 또는 협상값) -------
Uptime SLO (월): <수치>
HTTP API 응답 p95: <수치>
MTTR P0: <수치>
MTTR P1: <수치>
응답 시간 P0: <수치>
응답 시간 P1: <수치>
데이터 손실 RPO: <수치>
계획된 유지보수: <빈도 + 통보 시한>
SLA 보상 조항: <조건 + credit %>

------- 양측 서명 -------
rosshield 측: <담당자 이름·이메일·서명·날짜>
customer 측:  <담당자 이름·이메일·서명·날짜>
```

---

## 2. 측정 방법

SLA 위반 여부는 다음 결정론적 지표로만 판정합니다(원칙 §1 결정성 일관). customer는 본 지표를 자가 검증할 수 있어야 합니다.

### 2.1 Uptime 측정

- **방법**: customer 측에서 `/healthz` endpoint를 1분 간격으로 polling. 200 OK 응답 비율로 산정.
- **측정 도구 예**: Prometheus blackbox exporter, Pingdom, UptimeRobot, customer 자체 cron + curl.
- **down 판정 기준**: 5분 연속 503 또는 timeout(>10초). 단발 5xx는 down으로 간주하지 않습니다.
- **계산식**: `uptime_월 = 1 - (down_초 / 월_초)` × 100. 월=30일=2,592,000초로 통일(31일·28일 무시).
- **예외**: §3.2 면제 조건에 해당하는 계획된 유지보수·force majeure는 제외.

### 2.2 응답 시간 측정 (HTTP API p95)

- **방법**: customer 측 메트릭 또는 server 측 `/metrics`(Prometheus 호환, opt-in `metrics.enabled=true`)에서 `http_request_duration_seconds{path="/api/v1/*"}` p95 추출.
- **측정 단위**: 분 단위 또는 시간 단위 윈도우(customer 합의로 결정, default 1시간).
- **위반 판정**: 1시간 윈도우 p95가 합의값 초과 + 24시간 누적 위반 시간 ≥ 1시간 시 위반.

### 2.3 Audit chain 무결성 검증

- **방법**: customer가 정기(권장: 일 1회)로 `rosshield-audit-verify` CLI(외부 검증 SDK, E30 산출) 실행. 모든 chain head가 일관 + 서명 검증 통과여야 합니다.
- **위반 판정**: 1건이라도 검증 실패 시 즉시 P0 — §3.1 응답 시간 SLA 적용.
- **enterprise 추가**: 분기별 1회 customer·rosshield 양측이 chain head를 합의 비교(외부 timestamp authority 또는 양측 서명).

### 2.4 데이터 손실(RPO) 측정

- **방법**: customer 측 backup 정책(권장: `rosshield-server backup` cron daily 또는 시간 단위)의 마지막 backup 시각으로부터 사고 발생 시각까지의 간격.
- **위반 판정**: 사고 시점 - 마지막 backup 시각 > 합의 RPO 초과.
- **전제**: customer 측 backup 운영이 정상이어야 합니다(rosshield 측은 backup 도구 제공만, 실 운영은 customer 책임).

### 2.5 1차 응답 시간 측정

- **방법**: customer가 §1.3 합의 채널(이메일·Slack 등)로 사고 보고를 보낸 시각 ~ rosshield 측이 1차 응답(자동 acknowledgment 제외, 사람이 보낸 답신)을 보낸 시각까지의 경과 시간.
- **합의 채널**: `docs/onboarding/support-channels.md` §1 우선순위 표 참조.

---

## 3. 위반 시 정책

### 3.1 위반 판정·보고 절차

1. **customer 보고**: customer가 §2 측정 결과로 위반을 인지하면 `support-channels.md` §4 보고 양식으로 P0 또는 P1 채널 보고.
2. **rosshield 1차 확인**: rosshield 측이 합의된 1차 응답 시간 내 acknowledgment + 자체 측정으로 위반 여부 확인.
3. **합의**: 위반 확정 시 양측이 사고 매트릭스(시작 시각·종료 시각·영향 범위·근본 원인 RCA)에 합의.
4. **보상 적용**: §1.2 SLA 보상 조항에 따라 다음 달 청구서에 credit 반영.

### 3.2 보상 조항 (placeholder)

본 보상은 **첫 enterprise 계약 체결 시점에 customer와 합의로 확정**합니다. 현 default:

| 위반 유형 | 영향 | 보상 (default placeholder) |
|---|---|---|
| Uptime SLO 미달(월 단위) | uptime 측정 결과 < 합의값 | `<다음 달 청구액의 X% credit / customer 협상>` |
| MTTR P0 위반 | P0 사고 복구 시간이 합의값 초과 | `<해당 월 청구액의 Y% credit / customer 협상>` |
| Audit chain 위반 | chain 검증 실패 1건 이상 | `<rollback + 데이터 무결성 RCA 30일 내 + 청구액의 Z% credit / customer 협상>` |
| 데이터 손실 RPO 위반 | 사고 시점 - 마지막 backup > 합의 RPO | `<rollback + 손실 데이터 재구성 best-effort + 청구액의 W% credit / customer 협상>` |

> **paying customer 0 단계 명시**: 본 보상 항목은 첫 enterprise 계약 시점에 사용자(rosshield team)와 customer가 합의로 확정합니다. 현 placeholder는 SaaS 표준 패턴(10~25% credit) 수준의 보수적 default입니다.

### 3.3 escalation flow

1. **1차** — customer가 합의 채널로 보고 → rosshield 1차 응답 + 자체 측정.
2. **2차** — 1차 응답 후 합의 MTTR 50% 경과 + 미해결 시 rosshield 측 escalation 담당자(founder)에게 자동 escalation.
3. **3차** — MTTR 100% 경과 + 미해결 시 customer 담당 임원·rosshield team 동시 호출(enterprise 한정 — Slack 공유 채널 + 전화).

> escalation 담당자 정보는 `support-channels.md` §3 escalation flow 참조.

---

## 4. 면제 조건

다음 사유로 인한 down·지연은 SLA 위반으로 간주하지 않습니다.

### 4.1 계획된 유지보수(planned maintenance)

- §1.2 표 합의 windows 안에서 진행되는 유지보수.
- **사전 통보 의무**: pro = 7일 전 / enterprise = 14일 전 이메일·Slack 공지. 통보 누락 시 면제 미적용.
- 통보 양식: 시작 시각·종료 예상 시각·영향 범위(전체 down vs partial degrade)·롤백 전략.

### 4.2 Force majeure

- 자연재해(지진·홍수·화재).
- 광역 통신 장애(ISP·DNS root·CA failure).
- 정부 규제·법적 강제(긴급 명령 등).
- pandemic·전쟁·사보타지 등 합리적 통제 외 사유.

### 4.3 customer 측 인프라 사유

- customer 자가 운영 storage(postgres·sqlite)의 장애.
- customer 자가 운영 네트워크(VPN·방화벽·load balancer)의 장애.
- customer가 명시적으로 비활성한 옵션 기능(opt-in 미설정으로 인한 기능 부재).
- customer 측 OS·container runtime의 알려진 버그·EOL 사유.

### 4.4 외부 의존 장애

- SMTP provider(Gmail·SES·SendGrid 등)의 장애.
- SAML/OIDC IdP(Okta·Azure AD·Google Workspace 등)의 장애.
- LLM provider(Anthropic·OpenAI 등 — opt-in `intelligence.enabled=true` 시) API 장애·rate limit.
- pack mirror(에어갭이 아닌 경우) CDN 장애.

### 4.5 customer 응답 의무 미이행

- §3.1 보고 절차에서 customer가 재현 정보·로그·환경 정보 회수 요청에 합의 시한(default: P0 4시간 / P1 영업일 8시간) 내 응답하지 않은 경우, 응답 지연 시간만큼 MTTR 측정에서 제외합니다.

---

## 5. 양측 책임 (responsibility matrix)

| 항목 | rosshield 측 | customer 측 |
|---|---|---|
| `rosshield-server` 코드 품질·보안 패치 | ✅ | — |
| Pack 컨텐츠(benchmark·매핑) 서명·배포 | ✅ | — |
| License token 발급·갱신·회수 | ✅ | — |
| 1차 사고 응답 + 진단 + 패치 | ✅ | — |
| Audit chain 외부 검증 SDK 제공 | ✅ | — |
| Storage(sqlite·postgres) 운영·backup | — | ✅ |
| 네트워크 인프라(VPN·LB·방화벽) 운영 | — | ✅ |
| OS·container runtime 패치 | — | ✅ |
| `rosshield-audit-verify` 정기 실행 | — | ✅ |
| 사고 보고 시 재현 정보·로그·환경 정보 제공 | — | ✅ |
| SAML/OIDC IdP·SMTP provider 운영 | — | ✅ |
| LLM provider 결제·API key 관리(opt-in 시) | — | ✅ |

---

## 6. SLA 변경·갱신

### 6.1 변경 절차

- rosshield 측 변경 제안 → customer 측 합의 → 양측 서명 → 합의 효력 발생.
- enterprise edition은 §1.2 default에 따라 90일 사전 통보. pro는 30일.
- community는 EULA 변경 통보만(별도 SLA 합의 없음).

### 6.2 갱신 주기

- 본 SLA는 계약 기간(`<계약 종료일>`)과 동일 유효 기간을 갖습니다.
- 갱신 시점에 양측이 §1.2·§3.2 수치를 재협상할 수 있습니다.
- 운영 통계(이전 계약 기간 동안의 실 uptime·MTTR·사고 횟수)를 근거로 합의값을 조정합니다.

---

## 7. 한계 (paying customer 0 단계 명시)

본 SLA template는 다음 한계를 안고 있습니다.

1. **수치 모두 placeholder**: §1.2 default는 SaaS 표준 패턴 수준의 보수 추정입니다. 첫 enterprise 계약 체결 시점에 customer 인프라·운영 capacity·on-call rotation 가능 여부에 맞춰 확정합니다.
2. **on-call rotation 0 단계**: 현재 founder-led 운영으로 24/7 대응이 보장되지 않습니다. enterprise edition default 24/7 4시간 MTTR P0는 첫 enterprise customer 진입 시점에 별도 on-call 인력 확보 또는 escalation 자동화가 전제입니다.
3. **보상 조항 정량 미확정**: §3.2 credit % 는 첫 customer 협상으로 확정합니다. 현 단계는 `<X%>` placeholder.
4. **외부 검증 인프라 미완**: §2.3 enterprise 추가 "분기별 chain head 합의" 항목은 외부 timestamp authority 통합 후속(P1 backlog) 필요.
5. **법적 검토 미완**: 본 SLA template은 변리사·법무 검토를 거치지 않은 draft입니다. 첫 enterprise 계약 체결 *전*에 법무 검토 필수입니다.

---

## 8. 변경 이력

| 날짜 | 변경 | 작성자 |
|---|---|---|
| 2026-05-15 | 초판 (R3 Stage 1, paying customer 합의용 placeholder template) | rosshield core team |
