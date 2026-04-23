# FleetGuard (가칭)

> **상태**: Phase 0 — 설계 완료, 구현 미착수.
> **코드**: 0줄. 설계 문서 13개.
> **탄생일**: 2026-04-23

ROS2 로봇 플릿 보안 감사 플랫폼. 감사인이 받아들이는 결정론적 증거와 서명된 리포트를 생성하는 상용 B2B 제품.

> "FleetGuard"는 가칭(placeholder)입니다. 상표·도메인 확정 후 일괄 변경 예정. 결정 추적은 [`SESSION_HANDOFF.md`](./SESSION_HANDOFF.md)의 결정 로그에서.

## 한 줄 포지셔닝

> **CAI가 로봇을 침투한다면, FleetGuard는 플릿이 그 공격에 대비되어 있는지 매일 증명한다.**

## 지금 무엇을 볼 수 있는가

- **전체 시스템 설계서**: [`docs/design/`](./docs/design/) 13개 마크다운
- **세션 진입점**: [`SESSION_HANDOFF.md`](./SESSION_HANDOFF.md) — 현재 상태·결정 대기 항목·다음 선택지
- **Claude Code 지침**: [`CLAUDE.md`](./CLAUDE.md) — AI 세션이 이 리포에서 작업할 때 따라야 하는 원칙·컨벤션

## 설계 문서 읽는 순서

| 역할 | 최소 세트 |
|---|---|
| **임원·PM** | `design/00-mission-and-positioning.md` · `design/02-system-overview-and-deployment.md` · `design/11-tech-stack-and-roadmap.md` |
| **아키텍트** | `design/01-principles.md` · `design/03-architecture.md` · `design/04-domain-and-data-model.md` · `design/05-api-and-auth.md` · `design/06-security-and-tenancy.md` |
| **구현 엔지니어** | `design/03-architecture.md` · `design/07-scan-engine-and-benchmarks.md` · `design/08-intelligence-and-compliance.md` · `design/11-tech-stack-and-roadmap.md` |
| **보안·감사** | `design/06-security-and-tenancy.md` · `design/08-intelligence-and-compliance.md` · `design/10-audit-and-observability.md` |
| **새 Claude 세션** | `CLAUDE.md` → `SESSION_HANDOFF.md` → `design/README.md` |

## 핵심 결정 (설계 수준에서 확정)

- **포지셔닝**: 결정론적 감사 + 컴플라이언스 증명 + 로봇 특화
- **배포**: 같은 코어 + 3종 셸(데스크톱·온프렘·어플라이언스)
- **어플라이언스**: 자체 제조 없음. 이미지 + 레퍼런스 디자인 + 파트너 채널
- **테넌시**: 멀티테넌시 기본값
- **감사**: 해시 체인 + 외부 검증 API + 서명된 PDF
- **LLM**: 완전 옵트인, 결정론적 fallback 필수
- **비목표**: 자율 공격 에이전트·자체 HW 제조·SaaS-only·범용 IT 감사 도구

## 아직 결정되지 않은 것

| # | 항목 |
|---|---|
| D1 | 제품명 확정 |
| D2 | 백엔드 언어 (Go 권장, TS 유지도 허용) |
| D3 | 데스크톱 셸 (Tauri 권장, Electron fallback) |
| D4 | 어플라이언스 OS (Ubuntu Core 권장) |
| D5 | 라이선스 모델 (OSS vs closed vs open-core) |
| D6 | 리포 호스팅 (GitHub 공개 vs 사내) |

결정 절차와 상태는 [`SESSION_HANDOFF.md`](./SESSION_HANDOFF.md) 참조.

## 기여

[`CONTRIBUTING.md`](./CONTRIBUTING.md) 참조.

## 라이선스

[`LICENSE`](./LICENSE) — 현재 placeholder (결정 pending).

## 전신 프로젝트

이 리포는 [`D:\robot\dev\nrobotcheck`](../nrobotcheck)(Electron 데스크톱 앱, v2.0 DDD 리팩토링 중)에서 상업화 전략 검토 결과 분리 개설되었습니다. 전신의 CIS·ROS2 벤치마크 자산과 도메인 설계 개념을 차용하되, 코드는 완전히 새로 작성합니다.

배경: `D:\robot\dev\nrobotcheck\docs\COMMERCIALIZATION_STRATEGY.md`

자산 승계 Tier 분류: [`docs/design/12-migration-and-non-goals.md`](./docs/design/12-migration-and-non-goals.md) §12.2
