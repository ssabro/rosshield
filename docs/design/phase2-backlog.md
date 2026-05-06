# rosshield Phase 2 Backlog — Intelligence & Compliance

**범위**: 8~10주 (`docs/design/11-tech-stack-and-roadmap.md` §11.13).

**전제**: Phase 1 12 epic 모두 완료(`docs/design/archive/phase1-backlog.md`). 단일 바이너리 `rosshield-server`가 API + Web Console + 정적 자산 + 서명 PDF + audit chain + offline verify + Docker compose 모두 동작.

**Exit 기준**: ISMS-P 통제 기준 점수·리포트 생성, 감사 체인 포함 외부 검증 성공.

---

## Phase 1 Carryover (deferred — Phase 2 합류 후보)

Phase 1에서 명시적으로 deferred한 항목 — Phase 2 진입 시 우선 처리할지 결정 필요.

| ID | 출처 | 내용 | 추정 |
|---|---|---|---|
| C1 | E9.T2 + E10.T3 | WebSocket scan progress streaming (`coder/websocket` + 서버 WS + CLI `scan watch` + Web ScanDetailPage) | 1~2일 |
| C2 | R12-9 | Tauri 2.x 데스크톱 셸 (Rust 툴체인 + Web Console 번들) | 1주 |
| C3 | R12-1 | Web UI 추가 페이지 (Overview·Findings·Audit·Settings — Compliance·Advisor는 Phase 2 epic E13/E14에서 신규) | 3~5일 |
| C4 | E10.T4 | Playwright E2E (docker-compose harness) | 2~3일 |
| C5 | E10.T5 | i18next ko/en 번들 | 2일 |
| C6 | R12-12 | localStorage → HttpOnly cookie + CSRF (XSS 강화) | 1일 |
| C7 | E3 | Refresh token reuse detection (Phase 1 미구현, API 미들웨어 도입 시) | 0.5일 |

**권장 우선순위**: C1(데모 UX 핵심) → C3(Overview/Audit는 컴플라이언스 화면 기반) → C5(국내 B2B 영문 병행) → C2(상용 데스크톱 SKU). C4·C6·C7은 운영 도입 시점.

---

## Phase 2 신규 Epic

`docs/design/08-intelligence-and-compliance.md` §8.x 와 `11-tech-stack-and-roadmap.md` §11.13 Phase 2 spec 기반.

### E13. LLM Adapter (1주)

**왜**: 옵트인 지능화(P2). 결정론적 fallback이 기본, LLM은 보조(§01-6).

#### 스코프

```
internal/platform/llm/
  ├─ llm.go              # Adapter 인터페이스 + LlmTrace 구조체
  ├─ noop/               # 기본값 — LLM 호출 0
  ├─ ollama/             # 로컬 Ollama 어댑터 (옵트인, 에어갭 친화)
  └─ anthropic/          # 클라우드 Anthropic 어댑터 (옵트인)
```

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E13.T1 | `TestNoopReturnsErrLLMDisabled` | 기본 어댑터 — Insight 파이프라인이 LLM 실패에 결정론적 fallback 사용함을 보장 |
| E13.T2 | `TestOllamaAdapterStreamsTokens` (mock HTTP) | `/api/generate` 스트리밍 + LlmTrace 캡처 |
| E13.T3 | `TestAnthropicAdapterRespectsTimeout` | claude-3 messages API + 30s 타임아웃 + retry 1회 |
| E13.T4 | `TestLlmTraceCapturesPromptAndResponse` | 모든 호출이 audit emit (`llm.invoke`)·DB 영속 |
| E13.T5 | `TestRedactionAppliedToPromptsAndResponses` | E7 redaction을 LLM 입력·출력에 적용(prompt injection·secret leak 방지) |

#### Exit 기준

- noop이 Phase 2 기본값. ollama/anthropic은 명시 활성 후 동작.
- LLM 응답 모두 LlmTrace 영속 + audit chain anchor.

#### 설계 참조

§8.3 LLM Adapter 계층, §8.7 LlmTrace.

---

### E14. Insight 파이프라인 (1주)

**왜**: drift·anomaly·peer comparison을 결정론적으로 산출 → 감사인이 "왜 이렇게 변했나"에 답.

#### 스코프

```
internal/domain/insight/
  ├─ insight.go            # Insight 모델 + Severity + Kind enum
  ├─ drift.go              # 직전 N session 대비 변화 탐지
  ├─ anomaly.go            # statistical outlier (IQR·z-score)
  ├─ peer.go               # 같은 fleet 내 비교
  └─ sqliterepo/           # insights 테이블 (마이그레이션 0014)
```

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E14.T1 | `TestDriftDetectsCheckOutcomeChange` — pass→fail 전이 감지 | 직전 session 대비 diff |
| E14.T2 | `TestAnomalyFlagsOutlierLatency` — duration_ms IQR 1.5× 초과 | 통계 기반 |
| E14.T3 | `TestPeerComparisonFlagsRobotBelowFleetAvg` | 같은 fleet 내 pass 비율 평균 - σ |
| E14.T4 | `TestInsightAuditEmit` | 새 Insight 생성 시 `insight.created` audit |
| E14.T5 | `TestInsightCrossTenantIsolated` | tenant scope 격리 |

#### Exit 기준

- 3 Kind(drift·anomaly·peer) 모두 결정론적으로 산출. LLM 호출 0(설명만 옵트인).

#### 설계 참조

§4.2 Insight, §8.4 Insight 파이프라인.

---

### E15. Compliance 도메인 (1.5주)

**왜**: ISMS-P·ISO 27001·NIST 800-53·CIS·IEC-62443 통제 매핑. ISMS-P 점수가 Phase 2 Exit.

#### 스코프

```
internal/domain/compliance/
  ├─ compliance.go         # ComplianceProfile · FrameworkSnapshot · ControlStatus
  ├─ frameworks/           # 통제 정의 YAML (isms-p.yaml, iso27001.yaml, ...)
  ├─ mapping.go            # CheckDefinition.controlMappings → ControlStatus 집계
  └─ sqliterepo/           # compliance_profiles · framework_snapshots 마이그레이션 0015
```

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E15.T1 | `TestISMSPProfileLoadsFromYAML` — 통제 코드 100+ 개 | YAML 파서 + JSON Schema |
| E15.T2 | `TestFrameworkSnapshotAggregatesScanResults` — pass/fail/partial 집계 | 매핑 룰 → ControlStatus |
| E15.T3 | `TestComplianceScoreIsAuditAnchored` | 스냅샷 생성 시 audit chain head 캡처 |
| E15.T4 | `TestUnmappedChecksAreReported` | 매핑 안 된 check_id 목록 반환 |
| E15.T5 | `TestComplianceCrossTenantIsolated` | tenant scope |

#### Exit 기준

- ISMS-P · ISO 27001 두 프로필 점수·리포트 생성 가능.
- Unmapped controls 명시(LLM 자동 매핑은 E17).

#### 설계 참조

§4.2 ComplianceProfile/FrameworkSnapshot/ControlStatus, §8.2 기능 맵.

---

### E16. Advisor 대화 오케스트레이터 (1주, 옵트인)

**왜**: 사용자가 "이 fail은 왜?"를 자연어로 물으면 LLM이 evidence·rationale·fix를 종합해 답변.

#### 스코프

```
internal/domain/advisor/
  ├─ advisor.go            # Conversation·Turn·ToolCall 모델
  ├─ tools.go              # LLM이 호출 가능한 read-only tool 7종 (get_check, list_evidence, ...)
  ├─ orchestrator.go       # E13 LLM Adapter + tool dispatch
  └─ sqliterepo/           # advisor_conversations 마이그레이션 0016
```

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E16.T1 | `TestConversationPersistsTurns` | 대화 영속(쿼리 가능) |
| E16.T2 | `TestToolCallsExecuteReadOnly` | LLM이 write API 호출 X — read-only tool만 |
| E16.T3 | `TestToolResultsRedacted` | 응답에 redaction(E7) 적용 |
| E16.T4 | `TestAdvisorDisabledByDefault` | LLM Adapter=noop이면 Advisor도 disabled — UI에서 미노출 |

#### Exit 기준

- "왜 이 check가 fail?" 질문에 evidence·rationale 인용한 답변 생성 가능.
- 모든 tool call audit emit(`advisor.tool_called`).

#### 설계 참조

§8.5 Advisor (대화형).

---

### E17. LLM 자동 매핑 제안 (3일)

**왜**: 새 통제 도입 시 수백 개 check를 수동 매핑하는 부담 → LLM이 후보 제안.

#### 스코프

- E13 LLM Adapter + E15 Compliance 결합
- `compliance/llm_mapper.go` — check 메타·rationale을 prompt 컨텍스트로 → 후보 control 출력
- 자동 적용은 X — 사용자 검토 UI 후 수동 confirm

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E17.T1 | `TestSuggestMappingsReturnsTopN` (mock LLM) | confidence 0~1 + reasoning |
| E17.T2 | `TestSuggestionsAreAuditedNotAutoApplied` | suggestion 영속 + audit, 매핑은 별도 confirm 흐름 |

#### Exit 기준

- 미매핑 check 100개 → LLM 제안 → 사용자 confirm → ControlStatus에 반영.

---

### E18. Framework 리포트 PDF (3일)

**왜**: 컴플라이언스 점수만으로 부족 — 프레임워크별 통제·증거·점수를 PDF로.

#### 스코프

- E8 Reporting 도메인 확장
- `reporting/pdf/framework_builder.go` — 통제별 row + 점수 게이지 + audit anchor (기존 PDF builder 재사용)
- `Service.GenerateFramework(profileID, snapshotID)` 신규

#### TDD 태스크

| ID | 테스트 | 구현 |
|---|---|---|
| E18.T1 | `TestFrameworkReportPDFContainsControlList` | 모든 통제 row 출력 |
| E18.T2 | `TestFrameworkReportSignatureMatchesAuditAnchor` | E8 흐름 재사용 |

#### Exit 기준

- ISMS-P 통제 별 PDF 1개 생성·서명·외부 검증 통과.

---

### E19. Web UI Compliance·Findings·Advisor 페이지 (1주) — ✅ 완료 (2026-05-06)

**왜**: 백엔드 도메인 결선 후 UX 표면. Phase 1에서 deferred(R12-1)된 페이지 + Phase 2 신규.

#### 스코프 (실행 결과)

- ✅ `web/src/routes/_authenticated/findings.tsx` — Insight 목록 (kind/severity/robotId 필터 + Dismiss)
- ✅ `web/src/routes/_authenticated/compliance.tsx` — 프로필 추가/목록 + 선택 프로필의 snapshot 목록·생성 (Score Badge variant)
- ✅ `web/src/routes/_authenticated/advisor.tsx` — 좌측 대화 목록 + 우측 turn 렌더링 + Ask form. 503 응답 시 옵트인 활성화 안내
- ✅ `internal/api/handlers/advisor.go` (E19-3-A 추가 작업) — Ask/List/Get 3 endpoint chi 직접 mount, advisorErrorStatus(Disabled→503·EmptyQ→400·NotFound→404), 6 통합 테스트
- ✅ `web/src/components/layout/Sidebar.tsx` — Findings·Compliance·Advisor 3 메뉴 추가
- ✅ `web/src/api/hooks.ts` — Insight·Compliance·Advisor 훅 (raw fetch 패턴 또는 openapi-fetch)

#### 실행 단계

| 하위 단계 | 내용 |
|---|---|
| E19-1 | Findings 페이지 |
| E19-2 | Compliance 페이지 |
| E19-3-A | Advisor 백엔드 HTTP 표면 (openapi spec 미등록 — chi 직접 mount, **후속 정리 필요**) |
| E19-3-B | Advisor 웹 페이지 |

#### Exit 기준

- ✅ 3 페이지 빌드 통과 + 라우터 통합
- ✅ Go 전체 테스트 통과 (advisor 핸들러 6 신규)
- ✅ tsc --noEmit 통과
- ⚠ Vitest 단위 테스트 — 본 라운드는 작성하지 않음 (R12-7로 Playwright deferred 정합)
- ⚠ openapi spec advisor 표면 누락 — 후속 정리 (oapi-codegen 재생성 필요)

---

## Phase 2 Wiring & Operationalization 트랙 (W1~W3)

E13~E15 도메인 작성 후 발견된 결선·HTTP·자동화 작업. 원래 backlog에 명시 항목이 없어 별도 트랙으로 기록(2026-04-30).

| ID | 내용 | 추정 | 상태 | Commit |
|---|---|---|---|---|
| W1 | Bootstrap 결선 — Platform에 Insight·Compliance·LLM 통합. auditEmitterAdapter에 4 메서드 추가, Scan/Audit 어댑터 3종(insight/compliance ScanReader·AuditReader). LLMProvider 선택기(noop/ollama/anthropic). | 0.5일 | ✅ | `4601b55` |
| W2 | API 핸들러 — OpenAPI spec에 7 엔드포인트(insight 3 + compliance 4). oapi-codegen 재생성. handlers.Deps 확장 + Insight·Compliance handler + complianceErrorStatus(409/400 매핑). 통합 테스트 9건. | 1일 | ✅ | `85a0974` |
| W3 | scan.completed 자동 구독 — `internal/app/insightautorun` 패키지로 분리. payload 디코딩 → GetSession → Insight.RunForFleet best-effort. failed/cancelled는 skip. bootstrap에서 Subscriber 결선 + Shutdown subscription cancel. | 0.5일 | ✅ | `3e17e86` |

**관계**: W1~W3는 E14·E15의 자연스런 후속이며 E16(Advisor)·E17(LLM 자동 매핑)·E19(Web UI)와 독립적. backlog의 E16/E17/E19는 그대로 유효(LLM 활용·Web UI 본 작업).

---

## 의존 그래프

```
E13 LLM Adapter ──┬─→ E16 Advisor ──┐
                  └─→ E17 자동 매핑 │
                                   ├─→ E19 Web UI 페이지
E14 Insight ──┬───────────────────┘
              └─→ W3 자동 구독
E15 Compliance ──→ E18 Framework PDF ──→ E19
E14·E15 ──────→ W1 결선 → W2 API
```

병렬 가능: E13·E14·E15는 독립(평행 진행 가능). E16·E17·E18는 후속. W1·W2·W3는 E14·E15 직후 결선/표면 단계.

---

## 추정 (병렬 + 1인 운영 가정)

| Epic | 단독 추정 | 병렬 단축 | 상태 |
|---|---|---|---|
| E13 LLM Adapter | 1주 | (병렬 진입) | ✅ `d11b3cb` |
| E14 Insight | 1주 | (병렬 진입) | ✅ `5bcb741` |
| E15 Compliance | 1.5주 | (병렬 진입) | ✅ `5bcb741` |
| W1 Bootstrap 결선 | 0.5일 | E14·E15 후 | ✅ `4601b55` |
| W2 API 핸들러 | 1일 | W1 후 | ✅ `85a0974` |
| W3 scan.completed 자동 구독 | 0.5일 | W1 후 | ✅ `3e17e86` |
| E16 Advisor | 1주 | E13 후 | ✅ `2295005`+`5c2ee41`+`f8e8a14` |
| E17 자동 매핑 | 3일 | E13·E15 후 | ✅ `b3d9730`+`495c3a0` |
| E18 Framework PDF | 3일 | E15 후 | ✅ `5984b43`+`0ded8a6`+`d911ac5` |
| E19 Web UI | 1주 | E15·E16·E17 후 | ✅ Findings·Compliance·Advisor 3 페이지 + Advisor 백엔드 HTTP 표면 (E19-3-A) |
| **합계** | **6.5주 + 2일 W** | **~5주** | |

Carryover(C1~C7) 추가 시 +1~2주.

---

## 리스크 (Phase 2 한정)

| 리스크 | 완화 |
|---|---|
| LLM 응답 비결정성 → 감사 신뢰 저하 | 모든 LLM 호출에 LlmTrace + 결정론적 fallback 의무화 |
| Compliance 매핑 데이터 품질 | Phase 2 초기에 ISMS-P·ISO 27001 통제 데이터 수집 1주 별도 |
| Advisor가 write API 호출하는 prompt injection | tool 화이트리스트 read-only만, 도메인 service interface 우회 차단 |
| LLM cost 통제 | tenant당 일일 토큰 한도 + audit alarm |
| Multi-tenant LLM 컨텍스트 누수 | tenant scope tx + 프롬프트 헤더에 tenant_id 명시 + redaction(E7) |

---

## Phase 2 Exit 체크리스트 (`12-*` §12.9 재확인)

- [ ] ISMS-P·ISO 27001 두 프로필 통제 점수 산출
- [ ] Framework 리포트 PDF 외부 검증 성공
- [ ] LLM Adapter noop 기본값 + ollama/anthropic 옵트인 동작
- [ ] Insight 3 Kind(drift·anomaly·peer) 결정론적 산출
- [x] Web Console 추가 3 페이지(Compliance·Findings·Advisor) 동작 — E19 완료 (2026-05-06)

**Exit 시연 산출물** (2026-05-06):
- `docs/PHASE2_EXIT_DEMO.md` — 6 항목별 시나리오 + 운영 시연 가이드
- `scripts/phase2-exit-smoke.sh` — 자동 검증 (6/6 PASS)
- `cmd/rosshield-server/main.go` — `-llm-provider` 등 5 LLM CLI flag 노출

**남은 운영 갭**: `POST /api/v1/robots`, `POST /api/v1/scans/run` 핸들러 미구현(gen.Unimplemented 자동 501) → robot/scan 시드 API 부재. 후속 epic 후보: `seed demo` 서브커맨드 (1~2일).
- [ ] LLM 호출 모두 LlmTrace + audit chain anchor

---

## Phase 1 → Phase 2 진입 체크리스트

- [x] phase1-backlog.md를 archive/로 이전
- [x] phase2-backlog.md(본 문서) 신규 작성
- [ ] R14 결정 (Phase 2 진입 결정 항목 — LLM 기본 어댑터 전략·Compliance 데이터 출처 등)
- [ ] Carryover C1~C7 우선순위 사용자 합의
- [ ] CI에 frontend test 추가 (현재 Go만, Vitest는 로컬 실행)

---

## 문서 생명주기

- 본 백로그는 **살아있는 문서**. 태스크 완료 시 `[x]` + 커밋 해시.
- Phase 2 완료 시 `docs/design/archive/phase2-backlog.md`로 이동, Phase 3 백로그를 동일 경로에 신규.
- 결정 사항은 `SESSION_HANDOFF.md` "결정 로그"에 R14-X 형식으로 기록.
