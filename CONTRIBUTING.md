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
- 취약점 제보는 공개 이슈 대신 비공개 채널로 (이메일 채널은 추후 명시).
- SBOM·서명 관련 기여는 `docs/design/06-security-and-tenancy.md` §6.12 일관성 유지.

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
