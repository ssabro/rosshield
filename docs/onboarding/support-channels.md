# 지원 채널 정책 — rosshield <ProductName>

> **대상**: rosshield(`<ProductName>`) customer(community / pro / enterprise edition)의 사고·문의 보고 채널 가이드입니다.
> **상태**: paying customer 0인 현 단계의 **draft**입니다. 모든 채널 식별자(이메일·Slack URL·휴대폰)는 D1(브랜드·도메인 확정) 트랙 마감 후 정식 값으로 교체합니다. 본 epic(R3) 시점에는 placeholder를 유지하고 `support@<rosshield-domain-TBD>` 형식으로 표기합니다.
> **참조**:
> - `docs/onboarding/README.md` — onboarding 전체 흐름 + Phase 5 best-effort 표 + 연락처 TBD.
> - `docs/onboarding/sla-template.md` — SLA 정의·측정·위반 정책·면제 조건(본 docs와 cross-reference).
> - `docs/design/notes/customer-onboarding-design.md` §3.4 — D-CUSTONB-5 권장 default(이메일 1차 + GitHub P2/P3 + Slack enterprise만).

---

## 1. 채널 우선순위

문제 유형에 따라 다음 채널 우선순위를 따라주세요. customer는 우선순위가 명확하지 않을 때 **이메일을 default 1차 채널**로 사용합니다.

| 우선순위 | 채널 | 정의 | edition 제한 |
|---|---|---|---|
| **P0** (production down) | 이메일 `support@<rosshield-domain-TBD>` **+** 휴대폰 SMS | 데이터 손상·audit 체인 깨짐·전 사용자 로그인 불가·전 인스턴스 down. | 모두 (휴대폰은 enterprise + 계약 시 별도 전달) |
| **P1** (critical bug) | 이메일 `support@<rosshield-domain-TBD>` | 일부 기능 불가 + workaround 부재. scan 실행 불가·report 생성 실패 등. | 모두 |
| **P2** (feature request / minor bug) | GitHub issues `https://github.com/ssabro/rosshield/issues` (D6 public 전환 후) **또는** 이메일 | 일부 기능 불가 + workaround 존재. UI 폴리시·문구·문서 오류. | 모두 |
| **P3** (question / discussion) | GitHub discussions (D6 후) **또는** 이메일 | 운영 질문·best-practice 공유·feature 토론. | 모두 |
| **enterprise dedicated** | Slack 공유 채널 (계약 시 별도 초대) | enterprise 계약 customer의 1차 채널. P0·P1 모두 Slack 우선. | enterprise만 |
| **보안 신고** | 이메일 `security@<rosshield-domain-TBD>` **+** PGP 암호화 | 취약점·CVE 신고·악용 가능 결함. **public issue 금지**. | 모두 |

### 1.1 우선순위 분류 가이드

customer가 우선순위를 잘 모를 때 다음 트리를 따라주세요.

1. **데이터·audit 체인 무결성에 영향?** → P0.
2. **전 사용자 로그인 불가 또는 전 인스턴스 down?** → P0.
3. **일부 기능 불가 + workaround 없음?** → P1.
4. **일부 기능 불가 + workaround 있음?** → P2.
5. **취약점·보안 결함?** → 보안 신고 채널(public issue 금지).
6. **기능 요청·UI 폴리시·문서 오류?** → P2 (GitHub issues 우선, 비공식 가능 시 이메일).
7. **운영 질문·best-practice?** → P3.

운영자가 customer 보고 접수 후 우선순위가 부적절하다고 판단하면 1차 응답에서 우선순위 재분류를 통보합니다.

### 1.2 D-CUSTONB-5 default 근거

- **이메일 1차** 원칙은 founder-led 운영 capacity 한계에서 다채널 동시 모니터링 회피를 위함입니다.
- **GitHub issues는 P2/P3만**: D6(GitHub public 전환)는 첫 enterprise customer 진입 후 또는 Phase 5 진입 시 재논의(CLAUDE.md D6). 현 단계는 private repo로 issues 비공개 → P2/P3 issues는 D6 마감 *후* 실 사용.
- **Slack enterprise만**: 공유 채널 운영은 enterprise edition의 차별 가치이자 운영자 channel sprawl 회피.

---

## 2. 첫 응답 SLA per 우선순위

본 표는 `sla-template.md` §1.2 default와 일관합니다. customer가 계약한 edition에 따라 다음 1차 응답을 약속합니다.

| 우선순위 | community default | pro default | enterprise default |
|---|---|---|---|
| **P0** | best-effort | 4시간 (24/7) | 1시간 (24/7) |
| **P1** | best-effort | 영업일 8시간 | 영업일 4시간 |
| **P2** | best-effort | 영업일 2일 | 영업일 1일 |
| **P3** | 응답 보장 없음 | best-effort 영업일 5일 | 영업일 2일 |
| **보안 신고** | 24시간 (모든 edition 동일) | 24시간 | 24시간 |

> **1차 응답의 정의**: 자동 acknowledgment(이메일 자동 회신·Slack bot 응답)는 포함되지 않습니다. 사람이 보낸 답신을 1차 응답으로 산정합니다.

> **paying customer 0 단계 명시**: 위 default 수치는 첫 enterprise 계약 체결 시점에 양측 합의로 확정합니다. 현재 founder-led 운영으로 24/7 대응 capacity가 보장되지 않습니다 — enterprise 4시간 MTTR P0는 별도 on-call 인력 확보 또는 escalation 자동화 전제 placeholder입니다.

### 2.1 영업일·시간대 정의

- **영업일**: 월~금 (한국 공휴일·연말연시 제외).
- **영업 시간**: KST 09:00 ~ 18:00 (한국 표준시 UTC+9).
- **24/7 적용 채널**: P0 채널만(enterprise 휴대폰 SMS). P1/P2/P3는 영업일·영업 시간 기준.
- **on-call rotation**: paying customer 0 단계에서 미운영. 첫 enterprise customer 진입 시점에 도입(R3 후속 carryover).

---

## 3. Escalation flow

1차 응답 후 합의 MTTR 안에 해결되지 않거나 customer가 추가 escalation을 요청하면 다음 흐름을 따릅니다.

### 3.1 표준 escalation

```
[1차] customer 보고
   ↓ (1차 응답 SLA 내)
[1차 응답] rosshield support
   ↓ (MTTR 50% 경과 + 미해결 시 자동 escalation)
[2차] rosshield escalation 담당 (founder / engineering lead)
   ↓ (MTTR 100% 경과 + 미해결 시)
[3차] customer 담당 임원 + rosshield team 동시 호출
       (enterprise 한정 — Slack 공유 채널 + 전화)
```

### 3.2 담당자 매트릭스 (placeholder)

| 단계 | 담당 (rosshield 측) | 연락 채널 |
|---|---|---|
| 1차 응답 | support 담당 (founder-led 단계는 founder 직접) | 이메일 `support@<rosshield-domain-TBD>` |
| 2차 escalation | engineering lead | Slack 공유 채널 (enterprise) 또는 이메일 직접 호출 |
| 3차 escalation | founder / CEO | 휴대폰 SMS (enterprise 계약 시 별도 전달) |
| 보안 escalation | security lead | 이메일 `security@<rosshield-domain-TBD>` + PGP |

> **현 단계 현실**: paying customer 0 + founder-led 운영이므로 1차·2차·3차 모두 founder 단일 담당입니다. 첫 enterprise customer 진입 시점에 인력 추가 + escalation 분리(R3 후속 carryover).

### 3.3 customer 측 escalation 의무

customer는 escalation 단계에서 다음을 제공할 의무가 있습니다.

- 사고 보고 시점부터 escalation 시점까지의 추가 진단 정보(로그·재현 절차 갱신).
- customer 측 담당 임원·기술 책임자의 연락처(enterprise만, 계약 시 별도 합의).
- escalation 결과 합의된 RCA 보고서의 customer 측 검토·서명.

---

## 4. 사고 보고 양식

이메일·GitHub issues·Slack 모두 다음 양식을 사용해주세요. 양식 누락 시 운영자가 추가 정보 회수를 위해 핑퐁이 발생합니다.

### 4.1 표준 양식

```
## 우선순위

P0 / P1 / P2 / P3 중 하나 + 분류 근거 한 줄.

## 환경

- rosshield 버전: vX.Y.Z (`rosshield-server --version` 또는 UI 우측 하단)
- SKU: desktop / onprem / appliance
- License edition: community / pro / enterprise
- Storage: sqlite / postgres (버전 명시)
- OS: Ubuntu 22.04 / RHEL 9 / ... (배포 + 커널 버전)
- Container runtime: docker / containerd / 직접 binary 실행

## 재현 절차

1. ...
2. ...
3. ...

## 기대 결과

(정상 동작 시 어떻게 되어야 하는지)

## 실제 결과

(실제로 어떻게 되었는지)

## 첨부

- [ ] 서버 로그 마지막 200줄 (민감 정보 제거 후)
- [ ] Audit chain 검증 결과 (`rosshield-audit-verify --bundle <path>`)
- [ ] 스크린샷 (선택)
- [ ] customer-info-template.md 회수 본 (1차 보고 시만)

## 영향 범위

- 영향 받은 tenant 수:
- 영향 받은 사용자 수:
- 영향 받은 robot/scan 수:
- 데이터 손실 의심: yes / no
```

### 4.2 이메일 제목 prefix

운영자가 우선순위·검색·자동 분류를 위해 다음 prefix를 사용해주세요.

| 우선순위 | 제목 prefix 예시 |
|---|---|
| P0 | `[P0][rosshield] 전 사용자 로그인 불가 — <간단 한 줄>` |
| P1 | `[P1][rosshield] scan 실행 불가 — <간단 한 줄>` |
| P2 | `[P2][rosshield] <기능 요청 또는 minor bug 한 줄>` |
| P3 | `[P3][rosshield] <운영 질문 한 줄>` |
| 보안 신고 | `[SECURITY][rosshield] <취약점 카테고리 + 영향>` (본문은 PGP 암호화 권장) |

### 4.3 민감 정보 처리

다음을 보고 *전*에 반드시 제거해주세요.

- License token (`ROSSHIELD_LICENSE_TOKEN` 환경변수 값).
- SMTP 패스워드·API key.
- SAML IdP private key·certificate.
- SSH 대상 robot 자격 증명.
- 사용자 이메일·실명 등 PII(필요한 경우 마스킹: `user-<id>@<domain>` 형식).

> 운영자는 customer가 제공한 로그·첨부에서 민감 정보를 발견하면 즉시 회수 + 안전 채널(PGP·임시 secure 링크)로 재전송을 요청합니다.

---

## 5. 채널별 운영 시간

### 5.1 채널·시간대 매트릭스

| 채널 | 운영 시간 | 응답 약속 |
|---|---|---|
| 이메일 `support@<TBD>` | 영업일 KST 09-18 | 1차 응답 SLA(§2)에 따름 |
| Slack 공유 채널 (enterprise) | 영업일 KST 09-18 + P0 24/7 | P0 1시간(enterprise default), P1 영업일 4시간 |
| 휴대폰 SMS (enterprise P0) | 24/7 | P0 1시간 (enterprise default) |
| GitHub issues (D6 후) | best-effort, KST 09-18 | P2 영업일 2일 / P3 영업일 5일 |
| 보안 신고 이메일 `security@<TBD>` | 24/7 acknowledgment, 분석은 영업일 | 24시간 1차 응답(모든 edition) |

### 5.2 on-call rotation

- **현 단계**: paying customer 0 + founder-led → on-call rotation 미운영. P0 24/7 약속은 enterprise 진입 *전*까지 best-effort.
- **첫 enterprise customer 진입 시**: founder + 1명 이상 인력 추가 후 24/7 on-call rotation 도입. escalation 자동화(PagerDuty·Opsgenie 등) 옵션.
- **R3 후속 carryover**: on-call rotation 설계 + 인력 확보는 본 epic 외 후속 작업.

### 5.3 연말연시·공휴일 처리

- 한국 공휴일 + 연말연시(12/30 ~ 1/2): 영업일에서 제외.
- 사전 통보: rosshield 측이 영업 일정 변경 시 7일 전 이메일·Slack 공지 의무.
- P0 채널: 공휴일·연말연시도 24/7 적용(enterprise 한정, customer 합의 시).

---

## 6. 한계 (paying customer 0 단계 명시)

본 지원 채널 정책은 다음 한계를 안고 있습니다.

1. **채널 식별자 모두 placeholder**: `support@<rosshield-domain-TBD>`·`security@<rosshield-domain-TBD>`·Slack URL·휴대폰 모두 D1(브랜드·도메인) 트랙 마감 후 실 값으로 교체합니다.
2. **현 단계 우회**: 첫 customer 진입 *전*에는 founder의 개인 이메일(`ssabro_k@naver.com`)을 임시 1차 채널로 사용합니다. D1 확정 후 30일 내 정식 채널로 마이그레이션합니다.
3. **on-call rotation 0**: 24/7 P0 응답 약속은 첫 enterprise customer 진입 시점에 인력 추가·자동화 도구 도입을 전제로 합니다. 현 단계는 founder-led best-effort.
4. **GitHub issues 비활성**: P2/P3 GitHub issues 채널은 D6(GitHub public 전환) 마감 후 활성화합니다. 현 단계는 이메일이 P2/P3 백업 채널입니다.
5. **PGP key 미발급**: 보안 신고용 PGP key는 D1 확정 + 정식 도메인 마련 후 발급·게시합니다. 현 단계는 보안 신고도 일반 이메일 우선, 민감 첨부는 임시 secure 링크(예: end-to-end encrypted notebook) 사용.
6. **법적 검토 미완**: 본 지원 채널 정책은 변리사·법무 검토를 거치지 않은 draft입니다. 첫 enterprise 계약 체결 *전*에 법무 검토 필수입니다(고객 데이터 처리·로그 retention 관련 GDPR·PIPA 적합성 포함).

---

## 7. 변경 이력

| 날짜 | 변경 | 작성자 |
|---|---|---|
| 2026-05-15 | 초판 (R3 Stage 2, customer-facing 지원 채널 정책 + 보고 양식 + escalation flow) | rosshield core team |
