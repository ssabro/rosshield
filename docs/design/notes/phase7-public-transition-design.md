# Phase 7 진입 — D1 brand 확정 + E37 public 전환 + D8 청구권 코드 분리 통합 Design

> **상태**: Phase 6 진입 design doc(`phase6-backlog-design.md`) 직후, **출원 완료 가정**(사용자 결정 2026-05-18) trigger로 Phase 7 진입 첫 commit. 본 doc은 코드 0줄 / 마이그레이션 0건 / pack 변경 0 — 3 영역 통합 계획 + 결정 항목 권장 default까지만 마감합니다.
>
> **R 식별자**: R-PHASE7-1 (본 doc 전체). 결정 항목 D-P7-1 ~ D-P7-6. 후속 작업 식별자 R-BRAND · R-LICENSE · R-PUBLIC · R-D8.
>
> **참조**:
> - `docs/design/notes/d1-brand-candidates.md` (Top 3 후보 + WebSearch 상표 검증 12건, R40-5)
> - `docs/design/13-patent-strategy.md` (D8 청구권 + Open-core 청구 분배표 + 1순위 결합 청구항)
> - `docs/ip/spec-candidate-A-draft.md` (KIPO 명세서 raw draft)
> - `docs/ip/spec-A-review-and-revision-plan.md` (외부 검토 의견 반영 매핑)
> - `docs/design/phase5-backlog.md` E31 scaffold(현 placeholder 7 패키지) + E32 후속 실 구현 trigger
> - `docs/design/notes/phase6-backlog-design.md` Phase 6 후보 매트릭스
> - `memory/feedback_naming_verification.md` (브랜드 변경 시 WebSearch 의무)
> - `memory/feedback_design_doc_first.md` (1일+ 임계 design doc 우선)
> - `memory/feedback_design_doc_conservative.md` (보수적 추정)
> - `CLAUDE.md` 결정 현황 표 D1·D5·D6 항목
>
> **본 worktree**: `agent-a3f51805da2c47cd2`, main(head `6866bc5`) 분기. 단독 sub-agent. 다른 sub-agent는 ROS2 pack Round 1 Stage 1 진행 중 — 두 트랙 도메인 충돌 0 (본 작업은 `docs/design/notes/*` only).

---

## 1. 상태 / 배경

### 1.1 사용자 결정 (2026-05-18) 정확 인용

사용자는 본 round 진입 시 다음과 같이 명시했습니다:

> "출원이 완료 된것으로 생각하고 작업을 이어 갔으면 좋겠어"

이 결정은 다음 3 항목을 동시에 해제(unblock)합니다:

1. **D1 제품 브랜드 확정** — `d1-brand-candidates.md` §5.4 "출원 완료 = 우선권 확보 → public 전환 가능 → README/모든 docs `<ProductName>` placeholder 일괄 치환"이 가능해집니다. 단, 본 결정은 **실 출원 완료 확인이 아닌 작업 진행을 위한 가정**입니다 (D-P7-5 참조).
2. **E36 레퍼런스 HW burn-in 활성화** — 본 doc 범위 밖이지만 같은 round 결정으로 진입 합의됨.
3. **E37 GitHub repo public 전환 활성화** — `docs/design/13-patent-strategy.md` §13.2 D8-4 "출원 전 잠금"(GitHub public 전환 금지)이 해제됨. D6 결정(GitHub private 유지)의 재논의 trigger인 "첫 enterprise customer 또는 Phase 5 진입" 중 후자에 부합.

이로써 **Phase 7 진입의 첫 commit이 본 design doc**이며, 후속 4 트랙(R-BRAND, R-LICENSE, R-PUBLIC, R-D8)이 자동으로 unblock됩니다.

### 1.2 Phase 7의 위치

Phase 0~5는 코어 기능 구축(설계 → 스캔 엔진 → 감사 체인 → enterprise scaffold → RBAC·SSO·PWA). Phase 6 진입 design doc(`phase6-backlog-design.md`)에서 후보 5종을 매트릭스 분석 + 권장 우선순위를 설정했습니다. **Phase 7은 "공개 출시 + 청구권 본체 + 라이선스 양분"의 통합 trigger**입니다.

Phase 7의 산출은 다음 시점에 도달 가능하면 마감됩니다:
- 코어 GitHub public 전환 + Apache-2.0 LICENSE 결선 (외부 contributor 수용 가능 baseline)
- enterprise build tag 본체 4 청구항 구현(A-1 + B-1 + C-1 + D-3)
- 브랜드 placeholder `<ProductName>` 전역 치환 (사용자 대면 도메인·URL·CLI 명칭 일관)
- enterprise LICENSE 결정 (BSL 1.1 vs Commercial — D-P7-2)

### 1.3 본 doc의 범위와 비범위

**범위**:
- 3 영역(R-BRAND + R-LICENSE + R-PUBLIC + R-D8) 통합 Stage 분해
- 결정 항목 6건 권장 default 명시
- 회귀 위험 · 운영 고려 · 의존 추가 sketch

**비범위**:
- 실제 코드 변경 (본 doc은 design only)
- 실제 브랜드 확정 선언 (사용자 결정 round 별도)
- 실제 변리사 의뢰 결과 (사용자 외부 트랙)
- 실제 GitHub public 전환 실행 (사용자 권한 필요)

---

## 2. 현재 상태 진단

### 2.1 D1 brand 현황

| 항목 | 상태 |
|---|---|
| 코드 네임스페이스 | ✅ 확정(2026-04-23) — `rosshield` |
| Go 모듈 경로 | ✅ `github.com/ssabro/rosshield` |
| 내부 패키지 prefix | ✅ `internal/`, `cmd/rosshield`, `cmd/rosshield-server`, `cmd/rosshield-audit-verify`, `cmd/pack-tools` |
| 사용자 대면 제품 브랜드 | 🟡 placeholder `<ProductName>` 사용 중 (CLAUDE.md "프로젝트 개요" 첫 줄) |
| 후보 분석 | ✅ `d1-brand-candidates.md` (12 후보 WebSearch 전수 검증, R40-5 2026-05-11) — Top 3: Custos · Lodestar · Praxis |
| `<ProductName>` 치환 대상 파일 수 | 14 파일 (Grep 결과: README 3, CLAUDE 2, SESSION_HANDOFF 2, design 5, onboarding 5, CHANGELOG 1, releases 1 등 — 총 22 occurrences) |

### 2.2 D5 LICENSE 현황

| 항목 | 상태 |
|---|---|
| 코어 LICENSE 파일 존재 여부 | ✅ `LICENSE` (Apache-2.0 전문 결선) |
| LICENSE-ENTERPRISE | ❌ 부재 — BSL 1.1 vs Commercial 미결정(D-P7-2) |
| NOTICE | ❌ 부재 — OSS 의존성 attribution(golang.org/x/crypto, modernc.org/sqlite, wire, idb-keyval 등) |
| README LICENSE badge | ❌ 부재 (D6 private 유지 시 불필요했음) |
| Open-core 분배 표 | ✅ `13-patent-strategy.md` §13.3 (코어 Apache-2.0 + enterprise 별 라이선스) |
| Apache-2.0 §3 patent grant 의식 | ✅ §13.3 명시 — 코어 알고리즘은 OSS 사용자에게 자동 grant되므로 청구권 본체는 enterprise build tag 뒤로 |

### 2.3 D6 GitHub 호스팅 현황

| 항목 | 상태 |
|---|---|
| repo 가시성 | 🔒 `ssabro/rosshield` private (D6 결정 2026-05-08) |
| GitHub workflows | ✅ ci.yml · release-pipeline.yml · snap-build.yml · snap-smoke.yml |
| GitHub Actions secret | ⚠️ private 전제로 결선 — public 전환 시 cosign keyless OIDC만 안전, repo-level secret 노출 점검 필요 |
| SECURITY.md | ❌ 부재 — public 전환 전 필수 (취약점 신고 채널) |
| CONTRIBUTING.md | ✅ 존재 (간이) — public 전환 시 DCO·CLA·PR 절차 보강 필요 |
| CODE_OF_CONDUCT.md | ❌ 부재 — Contributor Covenant 2.1 권장 |
| Issue templates | ❌ 부재 |
| PR templates | ❌ 부재 |
| GitHub Discussions | ❌ 비활성 |
| Release binary (E30 verify CLI 포함) | ✅ v0.3.0 release 47 assets cosign keyless 서명 (README §장점 5) |

### 2.4 D8 청구권 enterprise 패키지 현황

`internal/enterprise/` 하위 7 패키지가 E31 stage(scaffold)에서 placeholder로 결선됨. 각 패키지는 다음과 같이 구성:

```
internal/enterprise/
  boundary_test.go                   // 코어 ↔ enterprise import 가드 (build tag 무관, 양쪽 실행)
  editiontag_enterprise_test.go      // build tag 검증
  crosswitness/  doc.go + enterprise.go   // A-1 cross-witness fold-in
  selectdisclose/ doc.go + enterprise.go  // A-3 selective disclosure verification token
  multihash/     doc.go + enterprise.go   // B-1 multi-hash evidence
  wasmrt/        doc.go + enterprise.go   // C-1 WASM sandboxed evaluator
  robotid/       doc.go + enterprise.go   // D-3 robot identity binding (TPM EK + MAC + CPU serial)
  rostopo/       doc.go + enterprise.go   // D-1 ROS2 topology audit (advanced)
  fleetxval/     doc.go + enterprise.go   // D-2 fleet cross-validation
```

각 `enterprise.go`는 다음만 포함:
```go
//go:build rosshield_enterprise
package <pkg>
const EditionTag = "enterprise"
```

각 `doc.go`는 알고리즘 명세를 한국어 주석으로 둠. **본체 구현은 E32 stage(D8 KR 우선출원 완료 후 trigger)** — 본 sub-agent round의 사용자 결정으로 출원 완료 가정 → E32 unblock.

### 2.5 본 worktree 상태

- 분기점: main `6866bc5` (ROS2 baseline pack design doc 후 handoff 갱신)
- 다른 sub-agent 1건: ROS2 pack Round 1 Stage 1 (도메인: `packs/`)
- 본 작업 도메인: `docs/design/notes/`만 (충돌 0)

---

## 3. R-BRAND — D1 brand 확정

### 3.1 후보 Top 3 요약

`d1-brand-candidates.md` §3에서 Top 3 추천:

| 순위 | 후보 | ★ | 강점 | 약점 | 등록 risk |
|---|---|---|---|---|---|
| 1 | **Custos** | ★★★★ | 라틴어 "수호자" 직접 fit, 한국어 표기 안정("쿠스토스"), 영문 SEO 노이즈 적음 | niche 보안 회사 5~6개 활성 사용(Custos Media, Custos IQ 등) — 단독 등록 risk 중간 | 합성형(`Custos Audit`, `Custos for Robotics`)으로 ↑ |
| 2 | **Lodestar** | ★★★★ | 보안·SW 분야 dominant player 없음, "길잡이 별" = 신뢰 기준점 메타포, 한국어 표기 안정("로드스타") | 차량 모델 연상 minor, `.io`/`.dev`/`.ai` 도메인 미확인 | 가장 낮음 — LODESTAR Corp Class 42 = Dead/Cancelled(2010) |
| 3 | **Praxis** | ★★★ | "실천" 의미가 감사·실무 컴플라이언스와 잘 연결, 영문 검색 분리 양호 | 한국어 표기 분기("프락시스/프랙시스"), Class 42 컨설팅 활성 사용처 다수 | 단독 사용 risk — 합성형 강력 권장(`Praxis Audit`, `PraxisROS`) |

### 3.2 권장 default — D-P7-1

**권장 default: Lodestar (단독형)**

근거:
1. **등록 가능성 최우선** — D1은 출원·등록의 risk 최소화가 1순위. Top 3 중 보안·SW Class 9·42 dominant player 부재.
2. **메타포 가치** — "신뢰의 기준점"이 본 제품 §13.5 1순위 결합 청구항 "외부 감사인 검증" 가치와 직접 공명.
3. **한국어 표기 안정** — Custos/Praxis보다 표기 분기 risk 낮음.
4. **합성형 의존도 낮음** — 단독 단어로 등록 가능성 높아 brand 단순성 유지.

트레이드오프:
- 차량 모델 연상(KG 쌍용 Lodestar) — Class 12 무관이라 출원 장애 X, 단 일반 사용자 첫 인상에서 차량 노이즈 minor.
- `.com`은 점유 — `.io` 또는 `.security` 또는 `.dev` 중 1개 확보 권장.

**대안**: Custos를 1순위로 고를 경우 합성형(`CustosROS`, `Custos for Robotics`) 권장 — brand 단순성 ↓이지만 메타포 직접성 ↑.

본 doc은 단일 권장 default만 제시. 최종 결정은 사용자 round에서 1·2·3 중 선택.

### 3.3 R-BRAND 작업 outline

1. **후보 1개 확정 commit** — `d1-brand-candidates.md` 끝에 "최종 결정: Lodestar (날짜)" append + SESSION_HANDOFF 결정 로그.
2. **WebSearch 추가 검증** — 메모리 `feedback_naming_verification.md` 의무. Top 3은 이미 §2에서 검증됐으나 1개 확정 시 마지막 변동 사항 확인(2026-05-18 시점 vs 2026-05-11 작성). USPTO TESS 변화 + `lodestar.io`/`.dev`/`.ai` WHOIS 직접 조회 권장.
3. **placeholder 전역 치환** — 14 파일 22 occurrences `<ProductName>` → `Lodestar` 일괄 sed. 단 README "코드네임 rosshield" 표기는 유지 (사용자 대면 제품명 = Lodestar / 코드네임 = rosshield 양립).
4. **selftest 회귀** — pack.yaml `product` 필드(있다면) 갱신 + `go test ./...` + web build smoke.
5. **commit** — `feat(brand): D1 제품 브랜드 Lodestar 확정 — placeholder 치환 + WebSearch 재검증`

영향 영역:
- README brand badge 신규
- web title `<title>` 갱신
- docs/onboarding/ 브랜드 placeholder 5건
- CHANGELOG entry
- 후속 release tag(v0.4.0) "Lodestar GA brand" tag line

---

## 4. R-LICENSE — D5 LICENSE 결선

### 4.1 코어 LICENSE — 이미 결선

`LICENSE`(Apache-2.0 전문) 파일이 이미 존재. R-LICENSE의 R-BRAND·R-PUBLIC 동기 의무 없음. **코어 LICENSE는 추가 작업 0**.

### 4.2 enterprise LICENSE — D-P7-2

D5 결정(2026-05-08, R30-4): "코어 Apache-2.0 + enterprise는 별 라이선스 (BSL/Commercial 구체 결정은 첫 enterprise customer 직전)"

**본 round 출원 완료 가정 → enterprise customer 진입 가속 → BSL 1.1 vs Commercial 결정 unblock**

옵션 비교:

| 옵션 | 모델 | 4년 후 | 사용자 가시성 | 강점 | 약점 |
|---|---|---|---|---|---|
| **BSL 1.1** (Business Source License) | 비상업 사용 + 운영 한도 내 사용 가능, 상업 운영 시 Commercial 필요 | 자동 Apache-2.0 전환(Change Date) | LICENSE-ENTERPRISE 텍스트 GitHub 노출, 누구나 읽기 가능 | OSS 생태계 신뢰 ↑ (전환 약속) + 자기 hosting 일부 허용으로 evaluation 마찰 ↓ | Change Date까지 4년간 enterprise 코드 일부 OSS화 — 차별 약화 risk |
| **Commercial only** | 모든 사용에 라이선스 계약 필요 (계약 텍스트 비공개 또는 별 페이지) | 변화 없음 | LICENSE-ENTERPRISE는 "Contact for licensing" 한 줄 | 모든 enterprise 코드 영구 보호 + 가격·조건 유연 | OSS 커뮤니티 evaluation 마찰 ↑ (PoC 어려움) + 잠재 customer 진입 장벽 |
| **하이브리드** (BSL → enterprise 모듈별 차등) | A-1·B-1은 BSL, D-3·C-1은 Commercial | 일부만 OSS 전환 | LICENSE-ENTERPRISE-MODULE별 | enterprise 차별 영역 영구 보호 + 마케팅 표면 일부 OSS 신뢰 | 운영 복잡도 ↑ (모듈별 LICENSE 파일 + import 가드) |

### 4.3 권장 default — D-P7-2

**권장 default: BSL 1.1 (Change Date = 출원일 + 4년)**

근거:
1. **D5 원안 일관** — Open-core 분배 원칙(§13.3)이 "전환 약속"으로 OSS 신뢰 보존을 전제.
2. **enterprise 진입 가속** — Commercial only는 PoC 마찰이 첫 customer 진입에 critical 약점. BSL은 self-hosting evaluation 허용으로 sales cycle 단축.
3. **청구권 보호 4년** — D8 KR 우선출원 + PCT 단계 = 4년이면 후속 청구권 출원·등록·해외 진입 완료 가능. Change Date 4년이 청구권 보호 충분.
4. **CockroachDB·Sentry·MariaDB 선례** — enterprise OSS에서 BSL 채택 사례 다수.

트레이드오프:
- Change Date 도래 시 4년 후 enterprise 코드 OSS화 → 추가 청구권 또는 enterprise 기능 신규 추가 필요(repeat). 본 doc 외 후속 round 결정.
- BSL은 OSI 비공인 라이선스 — 일부 enterprise customer는 OSI 공인 요구 시 Commercial 별 협약 필요.

**대안**: Commercial only를 선택할 경우 enterprise 진입 가속 ↓이지만 청구권 영구 보호. 본 doc은 BSL 권장 default 제시.

### 4.4 NOTICE 파일 — 의존성 attribution

Apache-2.0 §4.4: 결선된 OSS의 NOTICE 파일 보존 의무. 본 제품의 핵심 의존:

- **Go core**: `golang.org/x/crypto`, `golang.org/x/sys`, `golang.org/x/sync`
- **DB**: `modernc.org/sqlite` (CGO-free SQLite), `github.com/jackc/pgx/v5` (PostgreSQL)
- **HTTP/middleware**: `github.com/go-chi/chi/v5`, `github.com/go-chi/cors`
- **Auth**: `github.com/golang-jwt/jwt/v5`, `golang.org/x/oauth2`
- **DI**: `github.com/google/wire`
- **Crypto**: `github.com/sigstore/cosign`, `github.com/sigstore/sigstore`, `github.com/google/go-tpm` (E36 burn-in 진입 예정)
- **Web**: React 18, vite, tanstack/react-query, idb-keyval@6.2.2
- **Test**: `github.com/stretchr/testify`

NOTICE 파일은 `go.mod` + `web/package.json` 기반 자동 생성 권장(예: `go-licenses report`).

### 4.5 R-LICENSE 작업 outline

1. **LICENSE-ENTERPRISE 신규** — BSL 1.1 표준 텍스트 + Change Date(`<출원일> + 4 years`) + Change License(Apache-2.0) + Licensor("Lodestar Inc" 또는 사업체 결정 시).
2. **NOTICE 신규** — `go-licenses report ./... > NOTICE` + `web/` 수동 추가.
3. **README badge** — `![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)` + enterprise tag.
4. **internal/enterprise/LICENSE.enterprise** — `13-patent-strategy.md` §13.3 명시한 디렉터리별 LICENSE 파일(BSL 1.1 동일 텍스트 또는 reference).
5. **CHANGELOG entry** — `## v0.4.0` → `### Added` LICENSE-ENTERPRISE.
6. **commit** — `feat(license): enterprise BSL 1.1 결선 + NOTICE 자동 생성 + README badge`

---

## 5. R-PUBLIC — E37 GitHub repo public 전환

### 5.1 사전 점검 — 민감 데이터 grep

public 전환 전 다음 grep 의무:

| 항목 | 명령 | 차단 기준 |
|---|---|---|
| Secret · key · token 하드코드 | `grep -rE "(api[_-]?key|secret|password|token)" --include="*.go" --include="*.ts" --include="*.yaml"` | 결과 0건 (또는 모두 env var 또는 테스트 fixture로 명시) |
| TODO · FIXME 민감 정보 | `grep -rE "TODO|FIXME" --include="*.go" --include="*.ts"` | 사용자 정보 · 내부 IP · 미공개 ID 0건 |
| 내부 IP · hostname | `grep -rE "192\.168\.|10\.\d+\.\d+\.\d+|prod-\w+|internal-\w+"` | 0건 |
| `.env` · `*.pem` · `*.key` git tracked | `git ls-files | grep -E "\.(env|pem|key|p12)$"` | 0건 (이미 .gitignore 처리 시 OK) |
| AWS/Google/Azure credentials | `grep -rE "AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z\-_]{35}"` | 0건 |
| 사용자 email · 실명 | `git log --all --pretty=format:"%ae"` | private email · personal email 점검 |

### 5.2 git history rewrite 필요성 — D-P7-3

위 grep에서 git history 안의 민감 commit 발견 시 `git filter-repo`로 rewrite.

옵션:
- **A. rewrite 없이 그대로 public** — git history 안 모든 commit이 깨끗하다고 확신할 때. 본 worktree는 trunk-based로 시작했고 secret · key 하드코드 commit 기록이 보이지 않음.
- **B. rewrite 후 public** — 위 grep에서 1건이라도 발견 시. `git filter-repo --invert-paths --path <sensitive_file>` + force push + 모든 fork·tag 재발급.

**권장 default**: 옵션 A (rewrite 없이) — 다음 점검 후 진행:
1. `git ls-files | grep -E "\.(env|pem|key|p12|pfx)$"` → 0건이면 rewrite 불필요.
2. `git log --all --oneline` → 100건 미만이므로 manual 검토 가능.
3. CHANGELOG · README 안 사용자 email 또는 personal data 검토.

**대안**: 발견 시 옵션 B로 전환. `git filter-repo` + push --force + 모든 sub-agent worktree 재clone 필요.

### 5.3 GitHub 설정 변경

public 전환 시 자동 점검 항목:

| 설정 | private 전 | public 후 |
|---|---|---|
| repo visibility | private | **public** (Settings → General → Danger Zone → Change visibility) |
| Actions secrets | repo-level | **public도 OK** (Actions secrets는 public repo여도 fork PR에서 노출되지 않음 — `pull_request_target` 사용 안 하면 안전) |
| OIDC cosign keyless | ✅ 이미 OIDC | ✅ 변경 X — OIDC token은 public repo의 표준 |
| Branch protection | 적용 중 | 유지 + `force_pushes` 차단 + `required_status_checks: ci.yml` |
| Discussions | 비활성 | **활성화** (커뮤니티 Q&A 채널) |
| Issues | private | **public** + template 신규 (bug + feature_request) |
| Wiki | 사용 X | 사용 X (docs는 repo 안 docs/ 유지) |
| Sponsor | 없음 | optional (FUNDING.yml) |

### 5.4 신규 community 파일

| 파일 | 내용 | 권장 source |
|---|---|---|
| `SECURITY.md` | 취약점 신고 채널 (private mail 또는 GitHub Private Vulnerability Reporting) + GPG key + 응답 SLA 90일 | GitHub 표준 template |
| `CONTRIBUTING.md` 보강 | 기존 간이 → DCO sign-off + PR template + CLA 정책(필요 시) + lint·test 명령 + design 문서 읽기 순서 | 기존 파일 보강 |
| `CODE_OF_CONDUCT.md` | Contributor Covenant 2.1 표준 | https://www.contributor-covenant.org |
| `.github/ISSUE_TEMPLATE/bug_report.yml` | YAML form (제목 prefix [bug] + reproduction + 환경 정보) | GitHub 표준 |
| `.github/ISSUE_TEMPLATE/feature_request.yml` | 사용자 가치 + 대안 + 설계 영향 | GitHub 표준 |
| `.github/PULL_REQUEST_TEMPLATE.md` | 변경 요약 + 테스트 + 설계 영향 + breaking change 체크박스 | 자작 |
| `.github/FUNDING.yml` | optional sponsor 채널 | 후순위 |

### 5.5 README 갱신

| 항목 | 변경 |
|---|---|
| 제목 | `# <ProductName>` → `# Lodestar` (R-BRAND 완료 후) |
| badge 줄 | `[![CI](badge)] [![License: Apache-2.0](badge)] [![Release](badge)] [![codecov](badge)]` 신규 |
| 첫 paragraph | "Open-source ROS2 robot fleet security audit platform — deterministic evidence, signed reports, external verifiability." |
| cosign 검증 안내 | 기존 §장점 5 강화 (public 사용자가 첫 release 다운로드 시 검증 가능) |
| enterprise badge | "Enterprise features available under BSL 1.1 — see LICENSE-ENTERPRISE" |

### 5.6 R-PUBLIC 작업 outline

1. **사전 grep + git history 검토 commit** — `docs/design/notes/r-public-pre-check.md`(또는 본 doc append) 결과 기록.
2. **community 파일 신규 commit** — SECURITY.md + CONTRIBUTING.md 보강 + CODE_OF_CONDUCT.md + .github/templates.
3. **README badge + 첫 paragraph 갱신 commit** — public 사용자 첫 인상.
4. **GitHub Settings 전환 (사용자 권한)** — Settings UI에서 repo visibility = public + Discussions 활성화 + Issue templates 점검. Claude는 docs commit까지만, 실 전환은 사용자.
5. **commit** — `feat(public): GitHub repo public 전환 준비 — SECURITY/CONTRIBUTING/CODE_OF_CONDUCT + .github/templates`

---

## 6. R-D8 — 청구권 4건 본체 구현

### 6.1 1순위 결합 청구항 (D8-2)

`13-patent-strategy.md` §13.5: 1순위 결합 청구항 = A-1 + B-1 + C-1 + D-3. 본 round에서 4 enterprise 패키지 본체 구현 trigger.

### 6.2 A-1 cross-witness fold-in

**위치**: `internal/enterprise/crosswitness/`

**알고리즘**:
1. 같은 시스템 인스턴스 안 N개 테넌트 체인이 append-only로 진행.
2. 정기 fold-in interval(예: 매 1시간 또는 새 checkpoint 시점)마다 다음 entry 생성:
   ```
   entry_fold = (
     tenant_id_self,
     seq_self,
     prev_hash_self,
     witness_set = [(tenant_id_j, checkpoint_hash_j, signedAt_j) for j ≠ self],
     fold_hash = sha256(prev_hash_self ‖ sort(witness_set 직렬화) ‖ meta)
   )
   ```
3. 단일 테넌트 사후 위조 시: 다른 테넌트의 witness_set 엔트리가 그 테넌트의 checkpoint hash를 보존하므로 검출 가능.
4. 운영자가 모든 테넌트 동시 위조 시 외부 anchoring(타임스탬프 권한자·webhook·외부 transparency log)으로 보강.

**dep 추가**:
- 코어 기존 `internal/audit/` 의존 (인터페이스 노출 필요)
- 신규 외부 dep 없음 (sha256은 stdlib)

**TDD 진입**:
1. unit: `FoldIn(prev []byte, witnesses []Witness, meta []byte) ([]byte, error)` 결정론 + 정렬 순서 invariant + len(witnesses)=0 변형 + 다른 테넌트 위조 시뮬레이션 검출.
2. integration: 2 테넌트 chain + 운영자 1 테넌트 위조 → 다른 테넌트가 verify CLI로 검출.

**파일 sketch**:
```
crosswitness/
  fold.go             // FoldIn + Witness 구조 (~100줄)
  fold_test.go        // 결정론 + 순서 + 검출 (~200줄)
  scheduler.go        // interval fold-in scheduler (~80줄)
  scheduler_test.go   // ~120줄
  anchor.go           // 외부 anchoring (webhook + filesystem dump) (~80줄)
  anchor_test.go      // ~120줄
  doc.go              // 기존 유지
  enterprise.go       // 기존 const 유지
```

추정 줄 수: ~700줄 (구현 ~400 + 테스트 ~300)

### 6.3 B-1 multi-hash evidence

**위치**: `internal/enterprise/multihash/`

**알고리즘**:
1. evidence 출력에 대해 다음을 동시 산출:
   - 전체 `sha256(evidence)` (코어 호환)
   - `blake3(evidence)` (옵션 algorithm)
   - JSONPath sub-hash: 각 `$.path.to.field` 단위 sub-hash + path 인덱스
   - 라인 단위 sub-hash (텍스트 evidence): line N → `sha256(line_N)`
2. 검증 시점 algorithm 선택:
   - 코어 verify CLI: sha256 단일 검증
   - enterprise verify CLI: sha256 + blake3 cross-check + sub-hash partial verify

**dep 추가**:
- `lukechampine.com/blake3` (Go 순수 구현)
- JSONPath: `github.com/PaesslerAG/jsonpath` 또는 자작 경량 parser

**TDD 진입**:
1. unit: `Compute(evidence []byte, options) (MultiHash, error)` 결정론 + sub-hash invariant.
2. integration: 일부 라인 변경 → 변경 영역만 다른 sub-hash 검출 + 전체 sha256 변화.

**파일 sketch**:
```
multihash/
  compute.go          // Compute + Option (~150줄)
  compute_test.go     // ~250줄
  verify.go           // Verify + algorithm 선택 (~100줄)
  verify_test.go      // ~180줄
  jsonpath.go         // sub-hash JSONPath 추출 (~120줄)
  jsonpath_test.go    // ~180줄
  doc.go              // 기존 유지
  enterprise.go       // 기존 const 유지
```

추정 줄 수: ~1,000줄 (구현 ~500 + 테스트 ~500)

### 6.4 C-1 WASM sandboxed evaluator

**위치**: `internal/enterprise/wasmrt/`

**알고리즘**:
1. check policy를 WASM 바이트코드(`.wasm`)로 받음 (pack-tools에서 WAT/Rust/AssemblyScript → wasm 빌드).
2. wazero runtime + WASI 제한 환경에서 실행:
   - filesystem 화이트리스트 (`/check/input` 읽기 전용 + `/check/output` 쓰기 전용)
   - network 차단 (WASI `sock_*` family 비활성)
   - CPU time limit (예: 5초)
   - memory limit (예: 64MB)
3. policy 결과 = `{ status: pass|fail|error, evidence: bytes, reasoning: string }` JSON.
4. policy 서명 검증: pack의 `policy.wasm.sig` cosign 서명 확인 후만 실행.

**dep 추가**:
- `github.com/tetratelabs/wazero` (Go 순수 WASM runtime — R40-3 결정 trigger, wasmtime-go 대비 cgo-free + 가벼움)
- `github.com/sigstore/cosign` (이미 존재 — pack 서명 검증과 공유)

**TDD 진입**:
1. unit: `Evaluate(policy []byte, input []byte, limits Limits) (Result, error)` 결정론 + CPU/memory 한도 초과 시 에러.
2. integration: 악성 policy(`while(1)`) → CPU timeout으로 차단 + filesystem escape 시뮬레이션 → WASI 차단 확인.

**파일 sketch**:
```
wasmrt/
  runtime.go          // wazero wrapper + WASI 제한 (~200줄)
  runtime_test.go     // ~300줄
  policy.go           // policy 로딩 + 서명 검증 (~100줄)
  policy_test.go      // ~150줄
  limits.go           // CPU/memory limit + clock hook (~80줄)
  limits_test.go      // ~120줄
  doc.go              // 기존 유지
  enterprise.go       // 기존 const 유지
```

추정 줄 수: ~950줄 (구현 ~400 + 테스트 ~550)

**선결 결정**: D8-3에서 wazero vs wasmtime-go — `13-patent-strategy.md`에 R40-3 명시. 본 doc은 wazero 권장 default 가정 (cgo-free + 빌드 단순).

### 6.5 D-3 robot identity binding

**위치**: `internal/enterprise/robotid/`

**알고리즘**:
1. 로봇 측 agent가 다음 식별자 수집:
   - TPM EK certificate (TPM2_CreateEK + Quote) — TPM 없을 시 fallback null
   - 네트워크 MAC 주소 (sort + comma join)
   - CPU serial (Linux `dmidecode` 또는 `/sys/firmware/dmi`)
2. fingerprint = `sha256(EK_cert ‖ "|" ‖ sorted_macs ‖ "|" ‖ cpu_serial ‖ "|" ‖ salt)`
3. salt는 tenant-level 고정 (cross-tenant fingerprint 누출 방지).
4. 감사 결과에 fingerprint binding — 다른 로봇이 결과 위조 시 fingerprint 불일치 즉시 검출.
5. TPM Quote는 옵션: PCR 값 결합으로 부팅 무결성까지 증명.

**dep 추가**:
- `github.com/google/go-tpm` (TPM2 직접 명령 — E36 burn-in에서 활성화 예정, 이미 design 단계)
- `github.com/jaypipes/ghw` 또는 `golang.org/x/sys/unix` (MAC + CPU serial 직접 syscall) — TPM-free fallback

**TDD 진입**:
1. unit: `Compute(ek []byte, macs []string, cpuSerial string, salt []byte) Fingerprint` 결정론 + 정렬 invariant + salt cross-tenant 분리.
2. integration: 다른 로봇 fingerprint 위조 시뮬레이션 → 불일치 검출.

**파일 sketch**:
```
robotid/
  fingerprint.go      // Compute + Fingerprint 구조 (~120줄)
  fingerprint_test.go // ~200줄
  tpm.go              // TPM EK + Quote (go-tpm wrapper) (~150줄)
  tpm_test.go         // ~180줄 (TPM mock + 실 TPM은 E36 burn-in에서)
  collector.go        // MAC + CPU serial 수집 (Linux/Windows 차등) (~120줄)
  collector_test.go   // ~150줄
  doc.go              // 기존 유지
  enterprise.go       // 기존 const 유지
```

추정 줄 수: ~920줄 (구현 ~390 + 테스트 ~530)

### 6.6 R-D8 합산 추정

| 패키지 | 구현 줄 수 | 테스트 줄 수 | 합계 |
|---|---|---|---|
| crosswitness | ~400 | ~300 | ~700 |
| multihash | ~500 | ~500 | ~1,000 |
| wasmrt | ~400 | ~550 | ~950 |
| robotid | ~390 | ~530 | ~920 |
| **소계** | ~1,690 | ~1,880 | **~3,570** |

설계 추정 ~2,000줄(사용자 가이드라인)을 80% 초과. 테스트 비율 53%로 보수적. 다음 옵션 가능:
- **scoping 옵션**: 본 round는 A-1 + B-1만 진행, C-1 + D-3은 후속 round (1순위 결합 청구항을 절반씩 분할).
- **간이 진입**: 각 패키지 본체 구현은 ~50% 줄여서 ~1,800줄로 진입 + 후속 round에서 완성.

본 doc은 **D-P7-4 우선순위 결정** 항목으로 사용자에게 옵션 제시.

### 6.7 코어 ↔ enterprise 경계 가드 검증

`internal/enterprise/boundary_test.go`가 이미 결선되어 다음을 강제:
- `internal/{api,app,domain,platform}` 패키지는 `github.com/ssabro/rosshield/internal/enterprise/*` import 금지.

R-D8 본체 구현 후 같은 테스트로 회귀 확인. 코어 ↔ enterprise 통합은 cmd/* bootstrap에서만(어댑터 주입).

---

## 7. 합성 전략 옵션 ≥3 — 통합 Stage 분해

### 7.1 옵션 비교

| 옵션 | 묶음 | Stage 수 | 총 commit | 추정 기간 | 강점 | 약점 |
|---|---|---|---|---|---|---|
| **A** | 3 영역 일괄 (1 epic) | 1 | 12~15 | 3~4주 | 단일 epic + 단일 PR으로 review 집중 | PR 규모 클거 review 부담 + 회귀 risk 한꺼번에 |
| **B** | R-BRAND → R-LICENSE → R-PUBLIC → R-D8 순차 | 4 | 12~15 | 3~4주 | 단계별 회수 가능 + 사용자 round 별 확인 | 매 단계 handoff overhead |
| **C** | R-D8만 별 epic, R-BRAND+R-LICENSE+R-PUBLIC 묶음 | 2 | 6~8 + 4~6 | 2주 + 2주 | R-D8 본체는 청구권 보호 직접 — 별 epic으로 집중 가능 | R-D8 진입까지 brand/public 완료 wait |
| **D** | 보류 — 출원 완료 가정 확정 후 진입 | 0 | 0 | 0 (보류) | 사용자 결정 번복 risk 흡수 | Phase 7 진입 지연 |

### 7.2 권장 default

**권장 옵션 B (순차 4 epic)**

근거:
1. **단계별 회수 가능** — 사용자가 매 epic 후 round 진입 결정 가능. R-BRAND 완료 후 brand 인지·문서 갱신 회수 → R-LICENSE 후 OSS 결선 회수 → R-PUBLIC 후 외부 contributor 진입 회수 → R-D8 후 청구권 보호 회수.
2. **회귀 risk 분산** — R-D8은 enterprise 본체로 risk 가장 크나 R-BRAND·R-LICENSE·R-PUBLIC 안정화 후 진입하므로 영향 격리.
3. **handoff overhead minor** — 본 worktree pattern(Phase 5 회고)에서 epic당 1~2 handoff commit으로 충분.
4. **R-D8 본체 줄 수 큰 만큼 별 round 진입 권장** — 본 doc §6.6 ~3,570줄 추정이 본 round 1건에 부담.

**대안**: 옵션 C (R-D8 별 epic) — R-D8 줄 수가 크면 옵션 C로 전환 가능. R-BRAND+R-LICENSE+R-PUBLIC은 docs/community 중심으로 묶음 작업 가능.

---

## 8. 권장 옵션 + 근거 요약

| 결정 | 권장 default | 핵심 근거 |
|---|---|---|
| 합성 전략 | **옵션 B (순차)** | 단계별 회수 + 회귀 risk 분산 |
| brand | **Lodestar** | 등록 가능성 최우선 + 메타포 직접 fit |
| enterprise LICENSE | **BSL 1.1** | enterprise 진입 가속 + 4년 청구권 보호 충분 |
| git history rewrite | **불필요 (옵션 A)** | 사전 grep 후 0건 가정 |
| R-D8 우선순위 | **A-1 → B-1 → C-1 → D-3** | 1순위 결합 청구항 순서 + 의존 적은 순 |

---

## 9. 변경 사항 outline

### 9.1 R-BRAND 변경

- README: 제목 `<ProductName>` → `Lodestar`, badge 추가
- CLAUDE.md: 첫 paragraph placeholder 치환
- SESSION_HANDOFF.md: 결정 로그 entry 추가
- docs/design/00·README·12·07·11: placeholder 5건 치환
- docs/onboarding/*: 5건 치환
- CHANGELOG.md · docs/releases/v0.3.0.md: 1건씩 치환
- d1-brand-candidates.md: §5.5 "최종 결정" append
- **합계: 14 파일 / 22 occurrences / ~30 줄 diff**

### 9.2 R-LICENSE 변경

- LICENSE-ENTERPRISE 신규 (BSL 1.1 표준 ~250줄)
- NOTICE 신규 (`go-licenses report` 산출 ~200줄)
- internal/enterprise/LICENSE.enterprise 신규 (LICENSE-ENTERPRISE 동일 또는 reference)
- README badge: 2줄 추가
- CHANGELOG entry: 5줄
- **합계: 5 파일 신규/변경 / ~460 줄 신규**

### 9.3 R-PUBLIC 변경

- SECURITY.md 신규 (~80줄)
- CONTRIBUTING.md 보강 (기존 80줄 → 200줄, +120줄)
- CODE_OF_CONDUCT.md 신규 (Contributor Covenant 2.1 ~130줄)
- .github/ISSUE_TEMPLATE/bug_report.yml + feature_request.yml (~80줄 합)
- .github/PULL_REQUEST_TEMPLATE.md (~30줄)
- README 첫 paragraph + cosign 안내 강화 (~30줄 변경)
- **합계: 6 파일 신규/변경 / ~470 줄 신규**

### 9.4 R-D8 변경

- internal/enterprise/{crosswitness, multihash, wasmrt, robotid}/* 본체 구현
- go.mod 신규 dep 4건: blake3, wazero, jsonpath, go-tpm (existing)
- internal/enterprise/boundary_test.go 회귀 확인 (변경 0)
- 코어 audit·evidence 인터페이스 노출 일부 변경 (~50줄)
- **합계: 4 패키지 본체 / ~3,570 줄 신규 / 코어 인터페이스 minor 변경**

---

## 10. TDD Stage 분해 — 8~12 commit

### 10.1 R-BRAND (Stage 1, 1~2 commit)

| commit | 내용 | 추정 줄 수 | 추정 시간 |
|---|---|---|---|
| 1.1 | brand 확정 commit — `d1-brand-candidates.md` append + WebSearch 재검증 결과 | ~50줄 | 30분 |
| 1.2 | placeholder 일괄 치환 + selftest 회귀 + CHANGELOG | ~30줄 변경 | 30분 |

소계: 2 commit / ~80줄 / 1시간

### 10.2 R-LICENSE (Stage 2, 2 commit)

| commit | 내용 | 추정 줄 수 | 추정 시간 |
|---|---|---|---|
| 2.1 | LICENSE-ENTERPRISE + NOTICE 신규 + internal/enterprise/LICENSE.enterprise | ~460줄 신규 | 1시간 |
| 2.2 | README badge + CHANGELOG entry | ~10줄 | 15분 |

소계: 2 commit / ~470줄 / 1.5시간

### 10.3 R-PUBLIC (Stage 3, 3 commit)

| commit | 내용 | 추정 줄 수 | 추정 시간 |
|---|---|---|---|
| 3.1 | 사전 grep + git history 검토 결과 docs commit | ~80줄 | 1시간 (grep 실행 + 검토) |
| 3.2 | SECURITY.md + CODE_OF_CONDUCT.md + .github/templates 신규 | ~320줄 | 1.5시간 |
| 3.3 | CONTRIBUTING.md 보강 + README 첫 paragraph 갱신 | ~150줄 변경 | 1시간 |

소계: 3 commit / ~550줄 / 3.5시간 (실 GitHub Settings 전환은 사용자)

### 10.4 R-D8 (Stage 4, 4~5 commit)

| commit | 내용 | 추정 줄 수 | 추정 시간 |
|---|---|---|---|
| 4.1 | A-1 crosswitness 본체 + 테스트 + 코어 audit 인터페이스 minor 변경 | ~700줄 | 6시간 (TDD + integration) |
| 4.2 | B-1 multihash 본체 + 테스트 + blake3·jsonpath dep 추가 | ~1,000줄 | 8시간 |
| 4.3 | C-1 wasmrt 본체 + 테스트 + wazero dep 추가 + policy 서명 검증 | ~950줄 | 8시간 |
| 4.4 | D-3 robotid 본체 + 테스트 + go-tpm 사용 + collector | ~920줄 | 8시간 |
| 4.5 | 통합 e2e — 1순위 결합 청구항 시나리오 (A-1+B-1+C-1+D-3 end-to-end) | ~300줄 | 4시간 |

소계: 5 commit / ~3,870줄 / 34시간 (4~5일)

### 10.5 합산

- 총 commit: 12 (브랜드 2 + 라이선스 2 + 공개 3 + 청구권 5)
- 총 줄 수: ~4,970
- 총 추정 시간: ~40시간 (5~7일)
- 마라톤 기간: 3~4주 (handoff + 사용자 round 사이 사이)

---

## 11. 결정 항목 — D-P7-1 ~ D-P7-6

### D-P7-1: brand 선택

**옵션**: Custos | Lodestar | Praxis | 보류

**권장 default**: **Lodestar** (단독형)

**근거**: §3.2

---

### D-P7-2: enterprise LICENSE 모델

**옵션**: BSL 1.1 | Commercial only | 하이브리드(모듈별 차등) | 보류

**권장 default**: **BSL 1.1** (Change Date = 출원일 + 4년)

**근거**: §4.3 — enterprise 진입 가속 + 4년 청구권 보호 충분 + CockroachDB·Sentry·MariaDB 선례

---

### D-P7-3: public 전환 전 git history rewrite 필요성

**옵션**: A (rewrite 없이 그대로 public) | B (rewrite 후 public) | 보류

**권장 default**: **A (rewrite 없이)** — 단 사전 grep 결과 0건 가정

**근거**: §5.2 — 사전 grep 6 항목 모두 0건이면 rewrite 불필요. 1건이라도 발견 시 B로 전환.

---

### D-P7-4: D8 청구권 4건 구현 우선순위

**옵션**:
- (i) A-1 → B-1 → C-1 → D-3 (1순위 결합 청구항 순서)
- (ii) D-3 → A-1 → B-1 → C-1 (HW 식별자 우선)
- (iii) C-1 → A-1 → B-1 → D-3 (WASM 격리 우선)
- (iv) 반분 — A-1 + B-1만 본 round, C-1 + D-3 후속 round
- (v) 보류

**권장 default**: **(i) A-1 → B-1 → C-1 → D-3** (단, R-D8 줄 수 큰 만큼 옵션 iv 반분 검토 가능)

**근거**: §6.1, §6.6 — 1순위 결합 청구항 순서 일관 + 의존 적은 순(A-1 자체 완결 → B-1 evidence 결합 → C-1 evaluator → D-3 식별자 결합)

---

### D-P7-5: D5/D6/D8 출원 완료 가정 timeline

**옵션**:
- (i) 사용자 결정대로 출원 완료 가정 진행 (실 출원 확인 wait 없음)
- (ii) 실 출원 확인 후 R-PUBLIC 진입 (R-BRAND·R-LICENSE만 먼저 진행)
- (iii) 전체 보류

**권장 default**: **(i) 출원 완료 가정 진행**

**근거**: 사용자 명시 결정(2026-05-18) 수용. 단 R-PUBLIC Stage 3.4(GitHub Settings 실 전환)는 사용자 권한으로 wait 가능 — 실 출원 미완 상태에서 public 전환은 D8-4 잠금 위반이므로 사용자 round에서 실 출원 확인 후 진입 권장.

위험 흡수: 사용자 결정 번복 시 R-BRAND + R-LICENSE + R-D8은 그대로 진행 가능(public 전환과 무관). R-PUBLIC만 wait.

---

### D-P7-6: release tag 정책

**옵션**:
- (i) R-BRAND 완료 후 `v0.4.0` (brand GA tag)
- (ii) public 전환 후 `v1.0.0` (public GA tag — open-source 1.0 의미)
- (iii) R-D8 완료 후 `v1.0.0` (청구권 본체 결선 후 GA)
- (iv) 보류 — 매 stage 별 patch tag (`v0.3.1`, `v0.3.2` 등)

**권장 default**: **(ii) public 전환 후 v1.0.0**

**근거**:
- v0.x.y는 pre-1.0 시그널 → public 전환 시점에 v1.0.0 GA가 외부 사용자 신뢰 핵심.
- R-BRAND 완료(Stage 1) 후 v0.4.0 = brand alpha → R-PUBLIC 완료(Stage 3) 후 v1.0.0 = public GA = SemVer 안정 약속.
- R-D8은 enterprise build tag 뒤에 있으므로 코어 SemVer와 무관 — 별 tag(`v1.0.0+enterprise.1`).

**대안**: (iii) R-D8까지 완료 후 v1.0.0 — public 전환과 청구권 본체를 같이 묶어 GA 시점 단일화. 단 R-D8 4~5일 추가 wait.

---

## 12. 회귀 위험 / 운영 고려

### 12.1 brand 교체의 사용자 영향

| 항목 | 영향 | 완화 |
|---|---|---|
| CLI 명칭 | `rosshield` 유지 (코드 네임스페이스) — 사용자 cmd 그대로 | 영향 0 |
| 도메인 URL | 향후 `lodestar.io` (또는 결정 도메인) — 현재 미발표 | 영향 0 (아직 발표 전) |
| web title | `<ProductName>` → `Lodestar` — browser tab 표시 변경 | 사용자 브라우저 history만 영향 — minor |
| pack.yaml `product` 필드 | 있다면 갱신 — pack 서명 재발급 필요 | 본 round에서 pack 서명 재발급 commit 추가 |
| README · CHANGELOG | 외부 사용자 표시 변경 — 0.x.y에서는 변경 OK | minor |

### 12.2 public 전환의 보안 노출

| 항목 | 위험 | 완화 |
|---|---|---|
| signed pack의 cosign key | private key 누출 시 fake pack 가능 | cosign **keyless** OIDC 사용 — private key 없음, 본 repo는 이미 결선 |
| Actions secret | public PR이 fork에서 secret 접근 | `pull_request` event는 secret 차단 — `pull_request_target` 사용 안 함 확인 |
| 내부 IP · hostname | 발견 시 노출 | §5.2 사전 grep 차단 |
| 사용자 email | git log 안 personal email 노출 | git config user.email 점검 + 필요 시 git filter-repo |
| TODO 미해결 정보 | "TODO: change before public" 등 | §5.2 사전 grep |

### 12.3 R-D8 본체의 dep 추가

| dep | 라이선스 | 우려 |
|---|---|---|
| `lukechampine.com/blake3` | MIT | OK |
| `github.com/tetratelabs/wazero` | Apache-2.0 | OK (Apache-2.0이므로 Apache-2.0 코어와 호환, NOTICE 추가 의무) |
| `github.com/PaesslerAG/jsonpath` | Apache-2.0 | OK |
| `github.com/google/go-tpm` | Apache-2.0 | 이미 사용 중 |

라이선스 충돌 0. NOTICE 자동 생성에 포함.

### 12.4 BSL vs Commercial customer 협상 영향

| 시나리오 | BSL | Commercial |
|---|---|---|
| customer PoC (self-hosting 30일) | 허용 (BSL 운영 한도 내) | 별 협약 필요 |
| customer 정식 운영 | Commercial 협약 별도 (BSL 운영 한도 초과 시) | Commercial 협약 |
| customer 자체 빌드 + 수정 | BSL 허용 (4년 후 Apache 전환) | 거부 (Commercial 협약에 customization 조항) |
| 경쟁사 fork | BSL 운영 한도 초과 시 거부 | Commercial 거부 |

BSL이 PoC 마찰 ↓이나 customization 범위에 따른 협상 복잡도 ↑. Commercial은 협상 단순화 ↑이나 PoC 마찰 ↑.

### 12.5 sub-agent 병렬 가능성

본 doc 후 sub-agent 분담 옵션(memory `feedback_parallel_agents.md` 의무):

| Stage | 병렬 가능 | 비고 |
|---|---|---|
| 1.1 brand 확정 | 단독 | 사용자 결정 round 후 |
| 1.2 placeholder 치환 | 단독 | brand 확정 후 |
| 2.1 LICENSE-ENTERPRISE | 1.x와 병렬 가능 (도메인 분리) | docs/license 도메인 |
| 2.2 README badge | 1.2 후 (placeholder 치환 의존) | sequential |
| 3.x R-PUBLIC | 1.x·2.x와 병렬 가능 (.github/* 별 도메인) | docs/.github 분리 |
| 4.1 A-1 | 1·2·3 완료 후 | enterprise/crosswitness 단독 |
| 4.2 B-1 | 4.1과 병렬 가능 (enterprise/multihash 별 패키지) | sub-agent 2 |
| 4.3 C-1 | 4.1·4.2와 병렬 가능 (enterprise/wasmrt) | sub-agent 3 |
| 4.4 D-3 | 4.1·4.2·4.3과 병렬 가능 (enterprise/robotid) | sub-agent 4 |
| 4.5 통합 e2e | 4.1·4.2·4.3·4.4 모두 완료 후 | sequential |

**권장 패턴**: Stage 4에서 sub-agent 4 동시 dispatch (4.1·4.2·4.3·4.4 패키지 분리로 충돌 0) → 4일 → 4.5 통합. Phase 5 회고의 11회 연속 회귀 0 패턴 연장.

---

## 13. 참조

- `docs/design/notes/d1-brand-candidates.md` — Top 3 후보 + WebSearch 12건 전수 검증
- `docs/design/13-patent-strategy.md` — D8 청구권 + Open-core 청구 분배표 + 1순위 결합 청구항
- `docs/ip/spec-candidate-A-draft.md` — KIPO 명세서 raw draft
- `docs/ip/spec-A-review-and-revision-plan.md` — 외부 검토 의견 반영 매핑
- `docs/design/phase5-backlog.md` — E31 scaffold + E32 trigger
- `docs/design/notes/phase6-backlog-design.md` — Phase 6 후보 매트릭스
- `CLAUDE.md` — D1·D5·D6 결정 현황 표
- `SESSION_HANDOFF.md` — 결정 로그 + 진행 중 선택지
- `memory/feedback_naming_verification.md` — 브랜드 변경 시 WebSearch 의무
- `memory/feedback_design_doc_first.md` — 1일+ 임계 design doc 우선
- `memory/feedback_design_doc_conservative.md` — 보수적 추정
- `memory/feedback_parallel_agents.md` — sub-agent 병렬 의무
- BSL 1.1 표준 — https://mariadb.com/bsl11/
- Contributor Covenant 2.1 — https://www.contributor-covenant.org

---

## 부록 A: Phase 7 진입 직후 사용자 round 진행 옵션 (참고)

다음 round 사용자 진입 시 "진행 중 선택지"에 다음 6건 권장 default를 한 번에 confirm 옵션으로 제시:

1. D-P7-1 brand: **Lodestar** confirm? (대안 Custos 합성형 / Praxis 합성형)
2. D-P7-2 enterprise LICENSE: **BSL 1.1** confirm? (대안 Commercial only / 하이브리드)
3. D-P7-3 git rewrite: **불필요** confirm? (대안 사전 grep 후 결정)
4. D-P7-4 R-D8 우선순위: **A-1 → B-1 → C-1 → D-3** confirm? (대안 반분 / D-3 우선)
5. D-P7-5 출원 완료 가정 timeline: **(i) 가정대로 진행** confirm? (대안 R-PUBLIC만 wait)
6. D-P7-6 release tag: **public 후 v1.0.0** confirm? (대안 R-D8 후 v1.0.0 / 매 stage patch tag)

사용자가 6건 모두 권장 default confirm 시 Stage 1.1부터 즉시 진입 가능. 1건이라도 대안 선택 시 design doc revision 후 진입.
