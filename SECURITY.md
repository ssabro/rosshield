# Security Policy

## Reporting a Vulnerability

**Lodestar**(rosshield) 보안 취약점을 발견하신 경우, **public issue로 제출하지 마시고** 다음 채널 중 하나로 신고해주십시오:

1. **GitHub Private Vulnerability Reporting** (권장):
   - https://github.com/ssabro/rosshield/security/advisories/new
   - 로그인 후 "Report a vulnerability" 선택
   - 발견자와 maintainer만 볼 수 있는 private discussion으로 진행

2. **Email** (대안):
   - `ssabro_k@naver.com`
   - 제목 prefix: `[Lodestar Security]`
   - 가능하면 PGP 암호화 (key는 별도 요청 시 제공)

## Response SLA

| 단계 | 목표 시간 |
|---|---|
| Initial acknowledgement | 72시간 이내 |
| Severity triage (CVSS 산정) | 7일 이내 |
| Fix 또는 mitigation plan | **90일 이내** (Critical은 30일, High는 60일 목표) |
| Public disclosure (Coordinated) | Fix 배포 후 14~30일 (advisory + CVE 발행) |

## Scope

다음은 Lodestar 보안 정책 범위에 포함됩니다:

- **코어 (`internal/*`, `cmd/*`)** — Apache-2.0 코드 전체
- **Enterprise modules (`internal/enterprise/*`)** — BSL 1.1 라이선스 코드 (rosshield_enterprise build tag)
- **Built-in packs (`packs/cis-ubuntu-2404`, `packs/ros2-jazzy-baseline`, `packs/ros2-jazzy`)**
- **HTTP/WebSocket API** (`internal/api/*`)
- **Audit chain · Evidence storage · Pack signing** (Ed25519 + Sigstore cosign)
- **TPM keystore** (`internal/platform/keystore/tpm` — Linux 한정)
- **SROS2 baseline check 자체의 보안 권고**

다음은 범위에서 제외됩니다:

- **사용자 환경의 ROS2 robot 자체 보안 issue** — ROS2 upstream으로 신고
- **사용자 환경의 Linux distro 보안 issue** — distro upstream
- **Third-party Go dependencies** — 해당 upstream 우선 신고 (필요 시 본 repo에 cross-link issue)

## Supported Versions

| Version | Supported |
|---|---|
| `0.4.x` | ✅ (latest) |
| `0.3.x` | ⚠️ Critical만 |
| `< 0.3.0` | ❌ Phase 0 단계 — pre-release |

## Coordinated Disclosure

본 프로젝트는 [Coordinated Vulnerability Disclosure](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure) 원칙을 따릅니다:

- Fix 배포 + advisory 발행 전까지 발견자와 maintainer 외에는 비공개
- 발견자는 advisory에 attribution 받을 수 있음 (선택)
- CVE ID 신청 — Critical/High는 [GitHub Security Advisory](https://docs.github.com/en/code-security/security-advisories)로 발행
- 90일 후에도 fix 부재 시 발견자가 public disclosure 가능 (단, advance notice 7일)

## Security Audit

본 프로젝트의 핵심 가치 자체가 **감사 가능한 결정론적 증거**입니다 — 본 코드도 같은 기준으로 외부 감사 환영합니다. 감사 요청은 위 channel로.

## Related Documentation

- [docs/design/06-security-and-tenancy.md](docs/design/06-security-and-tenancy.md) — 보안 · 멀티테넌시 설계
- [docs/design/10-audit-and-observability.md](docs/design/10-audit-and-observability.md) — 해시 체인 · 외부 검증
- [docs/design/13-patent-strategy.md](docs/design/13-patent-strategy.md) — 청구권 보호 정책 (BSL 1.1 enterprise 본체)
