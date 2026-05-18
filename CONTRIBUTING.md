# Contributing

> 이 리포는 Phase 0(설계 단계)이다. 현재 기여의 주산물은 **설계서 업데이트와 결정 로그 작성**이다.

## 기여 전 읽을 것

1. [`CLAUDE.md`](./CLAUDE.md) — 작업 규칙·컨벤션 (Claude Code와 사람이 동일 적용)
2. [`SESSION_HANDOFF.md`](./SESSION_HANDOFF.md) — 현재 상태, 결정 대기 항목
3. [`docs/design/01-principles.md`](./docs/design/01-principles.md) — 설계 원칙 12개. **이 원칙을 어기는 기여는 받지 않는다**.
4. [`docs/design/12-migration-and-non-goals.md`](./docs/design/12-migration-and-non-goals.md) §12.7 — 비목표. **비목표에 속하는 기능 제안은 닫힌다**.

## 작업 방식

- **Trunk-based**: 피처 브랜치 없음. `main`에 직접 커밋.
- **TDD**: 구현 기여는 **테스트 먼저**.
- **커밋 전**: `typecheck` · 테스트 · 린트 모두 통과 (파이프라인 마련 전에는 로컬 검증).
- **파일 ≤ 400/800줄, 함수 ≤ 50줄**.

## 커밋 메시지

```
<type>(<scope>): <한글 제목>

<본문 — 한국어, 구조적 섹션 권장>
```

- `type`: `feat` · `fix` · `refactor` · `docs` · `test` · `chore` · `design` · `build` · `ci`
- `scope`: 도메인 이름(`robot`, `scan`, `audit` 등) 또는 `meta` · `infra` · `ui` · `api`
- Co-Author 라인 붙이지 않는다.

예:
```
design(architecture): 분리 모드 이벤트 버스 NATS JetStream 채택 근거 추가

## 변경
- 03-architecture.md §3.3에 NATS 채택 이유 서술
- Redis Streams 대비 at-least-once 보장 근거 명시

## 결정 로그
SESSION_HANDOFF.md에 D-NNN 추가
```

## 설계서 변경 기여

- **이유를 커밋 메시지에 기록**. "왜 바꿨는가"를 추후 추적할 수 있도록.
- 중요한 트레이드오프는 `SESSION_HANDOFF.md` 결정 로그에도 한 줄.
- 여러 문서에 걸치는 변경이면 한 커밋으로 묶거나, 연속된 커밋으로 정리(중간에 모순 상태 남기지 말 것).

## 비목표 (명시)

기여 전에 확인: 다음은 이 프로젝트가 **하지 않는다**.

1. 자율 공격 에이전트 프레임워크
2. 범용 멀티 LLM 오케스트레이션 플랫폼
3. 자체 하드웨어 설계·제조
4. SaaS-only 서비스
5. 범용 IT 보안 감사 도구 (ROS2 로봇 특화)
6. 로봇 제어·운용 기능
7. 모바일 네이티브 앱 (v1 범위 밖)

상세: [`docs/design/12-migration-and-non-goals.md`](./docs/design/12-migration-and-non-goals.md) §12.7

## 보안 관련 기여

- 보안 민감 영역(인증·서명·감사 체인·암호화) 변경은 **2인 리뷰 필수**.
- 취약점 제보는 공개 이슈가 아닌 [`SECURITY.md`](./SECURITY.md) 채널로 (GitHub Private Vulnerability Reporting 또는 ssabro_k@naver.com).
- SBOM·서명 관련 기여는 `docs/design/06-security-and-tenancy.md` §6.12 일관성 유지.

## DCO (Developer Certificate of Origin)

본 프로젝트는 [Developer Certificate of Origin 1.1](https://developercertificate.org/)을 채택합니다. 모든 commit은 `Signed-off-by: <name> <email>` 라인을 포함해야 합니다:

```bash
git commit -s -m "..."
# 또는 commit message 마지막 줄에 명시:
# Signed-off-by: Your Name <your.email@example.com>
```

DCO sign-off 의미 (1.1 표준):
- (a) 본인이 작성했고, 해당 라이선스(Apache-2.0 또는 BSL 1.1 enterprise)로 기여할 권리가 있음
- (b) 또는 적절한 open source 라이선스 하에 제공받은 코드이고 본 프로젝트 라이선스와 호환됨
- (c) 본인이 직접 작성하지 않았지만 (a)/(b)를 충족하는 기여자로부터 제공받음

CLA(Contributor License Agreement)는 채택하지 않습니다 (DCO로 충분).

## Pull Request 절차

1. **fork + branch** 또는 (maintainer는) trunk-based main 직접 commit
2. **TDD** — 테스트 먼저 → 실패 확인 → 구현 → 통과
3. **DCO sign-off** — 모든 commit에 `Signed-off-by` (git commit -s)
4. **PR template** 채우기 — `.github/PULL_REQUEST_TEMPLATE.md`가 자동 인식
5. **CI 7/7 PASS** — Go + Enterprise + Web + PG integration + CIS + TPM + Playwright E2E
6. **maintainer 리뷰** — 코어 1인, 보안 민감 영역 2인 리뷰
7. **squash merge** 권장 (작은 fix) 또는 commit history 보존 (큰 feature)

## 외부 contributor 환영 영역

특히 다음 영역에서 contribution 환영합니다:

- **새 baseline pack** — ROS2 distros(humble·iron·rolling), 다른 robot stack(MoveIt·Nav2), 다른 Linux distro(RHEL·Debian)
- **번역** — 문서 영어 번역 (현재 한국어 위주)
- **외부 검증 도구** — `cmd/rosshield-audit-verify` 확장 (다른 언어 SDK)
- **Issue triage** — bug reproduction · design 토론
- **Docs 개선** — onboarding · examples · troubleshooting

직접 코드 PR 전에 먼저 [Discussions](https://github.com/ssabro/rosshield/discussions)에서 의도 공유 권장 — 설계서와 충돌 시 PR 거부 risk.

## 행동 강령

본 프로젝트는 [Contributor Covenant 2.1](./CODE_OF_CONDUCT.md)을 채택합니다. 모든 기여자·maintainer는 본 행동 강령을 준수해야 합니다.

## 코드 기여 전 체크

- [ ] 원칙 12개(§01) 위반 없음
- [ ] 비목표(§12.7) 해당 없음
- [ ] 도메인 경계 준수 (다른 도메인 저장소 직접 호출 금지)
- [ ] `tenant_id` 스코프 적용
- [ ] Audit append-only 유지
- [ ] LLM 필수 경로 아님 (옵트인)
- [ ] 결정론적 fallback 존재
- [ ] 파일·함수 크기 제한 준수
- [ ] 테스트 추가
- [ ] 설계서와 정합 (설계서 수정이 먼저 or 동시)
- [ ] DCO sign-off (`git commit -s`)
