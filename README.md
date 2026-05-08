# `<ProductName>` — 코드네임 rosshield

> **상태**: Phase 0 — 설계 완료, 구현 부트스트랩 중.
> **코드**: 0줄. 설계 문서 13개.
> **탄생일**: 2026-04-23

ROS2 로봇 플릿 보안 감사 플랫폼. 감사인이 받아들이는 결정론적 증거와 서명된 리포트를 생성하는 상용 B2B 제품.

> **제품 브랜드**는 미확정(D1 연기). 문서·UI 등 사용자 대면 문자열은 `<ProductName>` placeholder를 씁니다. **코드 네임스페이스는 `rosshield`로 확정**(2026-04-23) — Go 모듈·내부 패키지·설정 경로에서 사용. 결정 추적은 [`SESSION_HANDOFF.md`](./SESSION_HANDOFF.md).

## 한 줄 포지셔닝

> **CAI가 로봇을 침투한다면, `<ProductName>`은 플릿이 그 공격에 대비되어 있는지 매일 증명한다.**

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

## 결정 현황 (Phase 0)

| # | 항목 | 상태 |
|---|---|---|
| D1 | 제품명 | 🟡 연기 — 코드네임 `rosshield`만 확정, 제품 브랜드는 Phase 1 후반 |
| D2 | 백엔드 언어 | ✅ Go + TypeScript |
| D3 | 데스크톱 셸 | ✅ Tauri 2.x |
| D4 | 어플라이언스 OS | 🟡 연기 — 기본 가정 Ubuntu Core 24 |
| D5 | 라이선스 | ✅ Open-core — 코어 Apache-2.0, 엔터프라이즈는 별 라이선스 (R30-4, 2026-05-08) |
| D6 | 리포 호스팅 | ✅ GitHub private 유지 — release binary + audit verify SDK가 P1 외부 검증 대체 (R30-4) |

상세는 [`SESSION_HANDOFF.md`](./SESSION_HANDOFF.md) 결정 로그.

## 기여

[`CONTRIBUTING.md`](./CONTRIBUTING.md) 참조.

## 라이선스

[`LICENSE`](./LICENSE) — Apache License 2.0 (코어). 엔터프라이즈 모듈은 추후 별도 라이선스.

## Release 검증 (R30-4 / E26)

GitHub 비공개 repo이므로 외부 검증자는 release binary + Sigstore cosign keyless 서명으로 무결성을 확인합니다.

```bash
# 1) cosign 서명 검증 (Rekor public log + GitHub OIDC)
cosign verify-blob \
  --certificate <binary>.cert \
  --signature <binary>.sig \
  --certificate-identity-regexp 'https://github.com/ssabro/rosshield/.github/workflows/release.yml@refs/tags/.*' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  <binary>

# 2) checksum
sha256sum -c checksums.sha256

# 3) 빌드 메타 (commit·built·go)
./rosshield version

# 4) report bundle 검증 (외부 감사인 standalone tool, E30 산출)
./rosshield-audit-verify --bundle <report.tar.gz>
```

## 전신 프로젝트

이 리포는 [`D:\robot\dev\nrobotcheck`](../nrobotcheck)(Electron 데스크톱 앱, v2.0 DDD 리팩토링 중)에서 상업화 전략 검토 결과 분리 개설되었습니다. 전신의 CIS·ROS2 벤치마크 자산과 도메인 설계 개념을 차용하되, 코드는 완전히 새로 작성합니다.

배경: `D:\robot\dev\nrobotcheck\docs\COMMERCIALIZATION_STRATEGY.md`

자산 승계 Tier 분류: [`docs/design/12-migration-and-non-goals.md`](./docs/design/12-migration-and-non-goals.md) §12.2
