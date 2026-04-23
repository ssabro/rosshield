# Changelog

이 프로젝트의 주요 변경 사항을 기록합니다. 포맷은 [Keep a Changelog](https://keepachangelog.com/)를 따르고, 버저닝은 [Semantic Versioning](https://semver.org/)을 따릅니다.

## [Unreleased]

### Added (Phase 0 — 설계)

- 2026-04-23 — 13개 설계 문서 초안 작성 (`docs/design/` Draft v0.1)
  - `00-mission-and-positioning.md` 미션·CAI 대비 포지셔닝
  - `01-principles.md` 12개 설계 원칙
  - `02-system-overview-and-deployment.md` 3종 배포 타깃
  - `03-architecture.md` 레이어·도메인·프로세스 토폴로지
  - `04-domain-and-data-model.md` 도메인 모델·SQL 스키마
  - `05-api-and-auth.md` HTTP/WS API·인증
  - `06-security-and-tenancy.md` 보안·멀티테넌시
  - `07-scan-engine-and-benchmarks.md` 스캔·벤치마크 팩
  - `08-intelligence-and-compliance.md` LLM·컴플라이언스
  - `09-ui-and-clients.md` Web/Desktop/CLI
  - `10-audit-and-observability.md` 해시 체인·관측성
  - `11-tech-stack-and-roadmap.md` 스택 선택·로드맵
  - `12-migration-and-non-goals.md` 자산 승계·비목표·리스크
- 2026-04-23 — `CLAUDE.md`, `SESSION_HANDOFF.md`, `README.md`, `CONTRIBUTING.md` 작성
- 2026-04-23 — 리포 부트스트랩(`.gitignore`, `.editorconfig`, `LICENSE` placeholder)

### Added (추가)

- 2026-04-23 — `contrib/source-benchmarks/README.md` 작성 — 전신 `nrobotcheck/resources/baselines/`의 원본 자료(CIS·ROS2 베이스라인 JSON·SCAP XML) 경로·크기·SHA-256·라이선스·타깃 팩 포인터. 파일 자체는 복사하지 않음.

### Decisions

- 2026-04-23 — 리포를 `D:\robot\dev\nrobotcheck` 전신과 분리해 `D:\robot\dev\fleetguard`로 신설
- 2026-04-23 — 상업화 방향: 헤드리스 코어 + 배포 3종(데스크톱·온프렘·어플라이언스 이미지)
- 2026-04-23 — 어플라이언스 자체 제조 포기, 이미지 + 파트너 채널 모델 채택
- 2026-04-23 — CAI와의 포지션 분리: 자율 공격 에이전트 프레임워크는 비목표
- **2026-04-23 — D2**: 백엔드 `Go`, 프론트 `TypeScript` 확정. 단일 정적 바이너리 + 에어갭 원칙 부합.
- **2026-04-23 — D3**: 데스크톱 셸 `Tauri 2.x` 확정 (Electron fallback 보류).
- **2026-04-23 — D5**: 라이선스 `Open-core` — 코어 Apache-2.0 + 엔터프라이즈 closed.
- **2026-04-23 — D6**: 리포 호스팅 `GitHub private` → Phase 1 exit 후 public 전환.
- **2026-04-23 — D1 연기**: 제품명 placeholder `FleetGuard`/`fg` 유지, Phase 1 후반 확정.
- **2026-04-23 — D4 연기**: 어플라이언스 OS 기본 가정 `Ubuntu Core 24`, Phase 3 exit 재확정.
