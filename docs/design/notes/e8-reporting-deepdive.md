# E8 Reporting Epic — 전신(nrobotcheck) 자산 조사 및 설계

## 1. 한 줄 결론

**전신은 Markdown/PDF/Excel 내보내기와 해시 체인 기반 감사 로그를 갖추었으나, 서명된 PDF 검증 구조와 표준 컴플라이언스 프레임워크 매핑은 미구현. rosshield는 이를 기반으로 EdDSA 서명·검증 API·외부 번들 증명을 구현해야 함.**

---

## 2. 발견된 자산

### 2.1 nrobotcheck의 Reporting 도메인

**경로**: `src/domains/reporting/`

#### 핵심 구성 (7개 파일, 736줄)

- `types/Reporting.ts` (92줄): ReportContext, ReportSummary 타입 정의 - 순수 인터페이스
- `services/ReportSummarizer.ts` (317줄): LLM 또는 규칙 기반 요약 생성 (fallback 보증)
- `services/ReportContextBuilder.ts` (200줄): DB → 통계·위험·컴플라이언스 데이터 구성
- `services/MarkdownRenderer.ts` (127줄): ReportSummary를 Markdown으로 렌더 (KO/EN)

**특징**:
- 세션 스코프: 하나의 scan session만 다룸
- 다언어 지원: HEADINGS 맵으로 한국어/영어 전환
- Deterministic Fallback: LLM 실패 시 규칙 기반 응답 보증
- 순수 데이터: ReportContext는 불변 객체

#### ReportContext 모델

`ReportScanStats`: totalChecks, pass/fail/error/skipCount, failureBySeverity
`ReportRisk`: checkId, checkName, severity(high/medium/low), robotId, actualValue, checkedAt (상위 5개)
`ReportComplianceEntry`: frameworkId, frameworkName, score(0~1), passingControls, totalControls
`ReportDrift`: robotId, checkId, fromStatus, toStatus, detectedAt (최신 5개)

### 2.2 내보내기 엔진 (533줄)

**경로**: `src/main/services/export/ExportService.ts`

#### 3가지 포맷

1. **Excel** (exceljs)
   - 시트: 요약 | 전체 결과 | 실패 항목 상세
   - 칼라: PASS(#92D050) / FAIL(#FF6B6B) / ERROR(#FFC000) / SKIP(#C0C0C0)
   - 심각도: HIGH(빨강, 굵음) / MEDIUM(주황) / LOW(회색)

2. **HTML** (함수명: exportToPdf, 실제는 HTML)
   - Electron dialog.showSaveDialog() + fs.writeFileSync()
   - 3페이지: 세션정보 → 실패상세 → 전체테이블
   - 600줄 CSS (Tailwind 유사, 인라인)

3. **CSV** (내장)
   - BOM + UTF-8
   - 18 컬럼 (점검ID~점검일시)

#### 칼러팔레트 (재사용 권장)

- Pass: #22c55e 텍스트, #dcfce7 배경
- Fail: #ef4444 텍스트, #fee2e2 배경
- Error: #eab308 텍스트, #fef3c7 배경
- Skip: #9ca3af 텍스트, #f3f4f6 배경
- Severity High: #dc2626 (굵음)
- 테이블 헤더: #1e40af 배경
- 주 제목: #2563eb

### 2.3 감사 로그 체인 (AuditLogger + AuditLogVerifier)

**메커니즘**:
- 각 entry: sequence (1부터), eventType, actor, targetType, targetId, action, detailsJson, occurredAt, prevHash
- 해시 계산: SHA256([sequence, eventType, actor, targetType, targetId, action, detailsJson, occurredAt, prevHash].join(''))
- 체인: entry[i].thisHash == entry[i+1].prevHash
- 시작: entry[0].prevHash == GENESIS_HASH ('0' × 64)

**검증**:
- sequence 연속성 (1부터)
- prevHash 체인 확인
- thisHash 재계산 비교
- 실패 시 { ok: false, firstBadSequence, reason }

**제약**: 단일 노드만 검증, 외부 검증(공개키) 미구현

### 2.4 IPC 라우터 (2개 채널)

**경로**: `src/main/ipc/routes/v2/reporting.router.ts`

1. `v2:reporting:generateSummary` (sessionId, language?) → ReportSummary (텍스트)
2. `v2:reporting:exportSummaryMarkdown` (sessionId, language?) → {filePath, cancelled}

특징: Electron/fs lazy-load (테스트 격리), 파일명: report-summary-{sessionName}-{ISO}.md

### 2.5 의존성

`package.json`:
- `pdfmake`: 의존만 있고 코드 미사용
- `exceljs`: 활발히 사용
- `papaparse`: 의존만 있음

---

## 3. 주요 교훈

### 3.1 PDF 생성이 실제로는 HTML

pdfmake를 의존에 추가했으나 사용하지 않음. 실제는 HTML 문자열 → writeFileSync → 사용자가 브라우저 인쇄

**rosshield 영향**: Go 라이브러리 필요 (go-pdf/fpdf 또는 unidoc/unidoc)

### 3.2 서명 구조 미완성

- 해시 체인은 있음 (SHA256)
- **서명은 구현 안 함**
- ReportSummary에 signature 필드 없음
- 검증은 체인 내부만

**rosshield 요구**: Ed25519 detached signature + 검증 API + 외부 번들

### 3.3 컴플라이언스 점수 계산이 분리됨

ReportComplianceEntry 타입만 정의, 실제 점수는 compliance 도메인
**우수 설계**: 의존성 역전 (보고 도메인은 데이터 주입)

### 3.4 템플릿이 문서화되었지만 코드 미발견

문서에 `resources/report_templates/*.md.jinja` 언급
실제 코드에서는 MarkdownRenderer 하드코드 사용

### 3.5 다언어 패턴 우수

HEADINGS 맵으로 모든 라벨 전환 - 재사용 권장

---

## 4. rosshield E8 구현 방향

### 4.1 PDF 렌더링

**선택**: go-pdf/fpdf (순수 Go, 경량) 또는 unidoc/unidoc (한글)
**초기**: fpdf + TrueType 임베드 또는 다음 단계로 연기

### 4.2 Report 모델 및 서명

`Report`: ID, SessionID, Format, Content (바이너리), Signature, ChainHead, CreatedAt
`ReportSignature`: KeyID, Timestamp, Signature (Ed25519 detached)

### 4.3 서명 로직

1. Report.Content SHA256
2. audit.Head() → 체인 최상단 해시
3. payload = SHA256(reportHash || chainHeadHash)
4. signer.Sign() → signature, keyId
5. 감사 로그: "reporting.sign" 이벤트

### 4.4 검증 API

`POST /api/v1/reports/{id}:verify` → VerificationResult
- signatureValid (Ed25519)
- chainAnchorValid (체인 해시)
- verificationUrl (토큰)

### 4.5 템플릿 (Phase 1 MVP)

- 팩에 template 포함 (§07.1과 동일 생명주기)
- 초기: 하드코드된 HTML (nrobotcheck 팔레트 재사용)
- 슬롯: {locale}, {organizationName}, {reportId} 등

### 4.6 외부 검증 번들

`report-bundle-<id>.tar.gz`:
- report.pdf
- report.pdf.sig
- audit-chain-head.json
- public-key.pem

CLI: `rosshield report verify <bundle>`

---

## 5. E8 태스크 분해

| E8.T | 설명 | 기간 | 의존 |
|---|---|---|---|
| T1 | ReportBuilder (통계) | 1일 | E6 (Scan) |
| T2 | PDF 생성 + 서명 메타 | 2일 | E1 (Signer) |
| T3 | Signature 검증 | 1일 | E1 |
| T4 | 손상된 PDF 거부 | 1일 | E8.T2 |
| T5 | Audit Chain 앵커 | 1일 | E2 (Audit) |
| T6 | 감사 로그 | 1일 | E2 |

총 1주, 모든 선행 조건 완료

---

## 6. 핵심 요약

**차용 자산**:
1. 칼러팔레트 (Pass/Fail/Error/Severity)
2. 다언어 HEADINGS 맵 구조
3. 감시 로그 해시 함수 (구분자 활용)

**신규 구현**:
1. Go PDF 렌더 (gofpdf 또는 unidoc)
2. Ed25519 서명 (detached + Report 메타)
3. 검증 API + 외부 번들
4. 컴플라이언스 통합

**Phase 1 Exit**: 로봇 3대 감사 → 서명 PDF → 감사 체인 외부 검증 성공

