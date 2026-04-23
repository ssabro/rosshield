# 원본 벤치마크 자료 — 참조 포인터

> **상태**: 포인터만 존재. 실제 파일은 이 폴더에 **없음**.
> **목적**: Phase 1에서 `pack-tools convert`를 구현할 때 **입력으로 사용할 원본 자료**의 출처·경로·체크섬을 명확히 기록.

## 왜 파일 자체를 여기 두지 않았나

1. **포맷이 다르다** — 원본(CSV/JSON)은 전신 리포의 내부 포맷. 본 제품의 팩 포맷은 **서명된 YAML**(`pack.yaml` + `checks/*.yaml` + `SIGNATURE`). 원본을 그대로 복사해 두면 "진실이 두 곳"이 된다.
2. **원본은 변환기의 입력, 출력(팩)만 커밋한다** — 설계서 §07 §7.13, §12 §12.4 참조.
3. **라이선스 경계** — CIS 파생물·SCAP·ROS2 베이스라인 각각의 재배포 조건을 본 리포 라이선스(D5) 확정 전에 선제 포함하지 않는다.
4. **Phase 0 상태를 깔끔하게 유지** — 구현 단계 진입 시점에 도입.

## 전신 리포 경로

모든 경로는 `D:\robot\dev\nrobotcheck\` 기준.

### Core 벤치마크 JSON (팩 변환 주 대상)

| 전신 경로 | 크기 | SHA-256 (첫 12자) | 타깃 팩 (가칭) |
|---|---:|---|---|
| `resources/baselines/cis_ubuntu_2404_benchmark.json` | 1,138,522 | `4a85e54d90b2` | `cis-ubuntu-24.04-<v>.pack` |
| `resources/baselines/ros2_jazzy_security_baseline_framework_v1.1.json` | 1,348,933 | `f60e77ef1a2b` | `ros2-jazzy-<v>.pack` (최신) |
| `resources/baselines/ros2_jazzy_security_baseline_framework_v1.0.json` | 1,278,792 | `8228a9dd5114` | (참조용 — v1.1이 후속) |
| `resources/baselines/ros2_humble_security_baseline_framework_v1.0.json` | 1,350,680 | `4fb4fbb2f961` | `ros2-humble-<v>.pack` |
| `resources/baselines/ros2_security_baseline_framework_v1_0.json` | 1,278,792 | `8228a9dd5114` | **중복** — `ros2_jazzy_..._v1.0.json`과 동일 해시. 잡음. |

> **주의**: `ros2_security_baseline_framework_v1_0.json`은 `ros2_jazzy_security_baseline_framework_v1.0.json`과 바이트 동일이다. 변환 시 둘 중 하나만(아마 jazzy 쪽) 입력으로 선택한다.

### 보조 문서

| 전신 경로 | 설명 |
|---|---|
| `resources/baselines/compare.md` | 파일 간 비교·계보 정리 메모 |
| `resources/baselines/scap/note.md` | SCAP 자료 변환 이력·메모 |

### SCAP 자료 (옵션, Phase 2~3 입력 후보)

| 전신 경로 | 크기 | 설명 |
|---|---:|---|
| `resources/baselines/scap/ssg-ubuntu2404-ds.xml` | 12,267,739 | upstream SCAP Security Guide for Ubuntu 24.04 (외부 원본) |
| `resources/baselines/scap/ros2-security-baseline-xccdf.xml` | 918,494 | 전신의 ROS2 XCCDF 변환본 |
| `resources/baselines/scap/ros2-security-baseline-oval.xml` | 483,481 | 전신의 ROS2 OVAL |
| `resources/baselines/scap/ko/*.xml` | — | 한국어 지역화 XCCDF/OVAL/CPE |
| `resources/baselines/scap/en/*.xml` | — | 영문 XCCDF/OVAL/CPE |
| `resources/baselines/scap/backup/*.xml` | — | 이전 버전 보관 |

SCAP 경로는 **Phase 1의 우선 변환 대상이 아님**. Phase 2 이후 검토(§08 컴플라이언스 매핑 심화 시점).

### 설계 연구 자료 (복사 대상 아님, 참조용)

| 전신 경로 | 설명 |
|---|---|
| `nrobotcheck/docs/ROS2_SECURITY_BASELINE_FRAMEWORK_SPEC.md` | ROS2 베이스라인 프레임워크 스펙 |
| `nrobotcheck/docs/SCAP_CONVERSION_RESEARCH.md` | SCAP 변환 연구 노트 |
| `nrobotcheck/docs/BASELINE_V1.1_CHANGELOG.md` | v1.0→v1.1 변경 이력 |

## 라이선스·출처

- **CIS Benchmarks 파생**: CIS는 자체 라이선스(CC BY-NC-ND 4.0 기반)를 적용. 전신 `cis_ubuntu_2404_benchmark.json`은 CIS 원문 PDF를 내부 포맷으로 재구성한 2차 산출물로, **CIS 라이선스 제약을 승계**한다. 재배포·상용 활용 전에 법무 검토 필수.
- **SSG(SCAP Security Guide)**: Apache 2.0 계열. 파일은 upstream (github.com/ComplianceAsCode/content)에서 가져옴.
- **ROS2 baseline framework JSON (자체 제작)**: 전신 팀이 직접 작성한 자료. 본 제품 라이선스(D5) 결정 시 재라이선싱 검토 범위 안에 들어감.
- **전신 리포의 실제 라이선스**: 내부 프로젝트. 공개 라이선스 미부여 상태.

## Phase 1 변환 절차 (요약)

`docs/design/12-migration-and-non-goals.md` §12.4 기반.

1. **`pack-tools` CLI 구현** (Phase 1 초기 항목).
2. 변환기 CLI 예시:
   ```
   pack-tools convert \
     --input D:/robot/dev/nrobotcheck/resources/baselines/cis_ubuntu_2404_benchmark.json \
     --format nrobotcheck-baseline-v1 \
     --vendor "CIS"  --license "CC-BY-NC-ND-4.0" \
     --output packs/source/cis-ubuntu-24.04-1.0.0/
   ```
3. 변환 결과를 **Self-Test fixture 스켈레톤**과 함께 출력.
4. 팩 서명 → `packs/signed/*.pack.tar.gz`로 빌드.
5. 팩 저장소에 등록 + `BenchmarkPack` 설치·활성화.

## 이 폴더에 파일을 복사해야 할 때

Phase 1 진입 시점에 다음 상황이 되면 이 폴더에 **읽기 전용 스냅샷**을 두는 것을 검토:

- 전신 리포 접근이 불안정한 CI 환경에서 재현 빌드가 필요할 때
- 라이선스(D5)가 확정되어 재배포 가능성이 명확해졌을 때
- 변환기 골든 테스트 입력 고정이 필요할 때

복사 시 필수 작업:
1. 이 README에 `SNAPSHOT_TAKEN_AT` · `SNAPSHOT_SOURCE_SHA` 테이블 추가.
2. 같은 폴더에 `LICENSE-NOTICE.md` — 각 파일의 원 라이선스·출처 명시.
3. `.gitattributes`에 binary/text 지정(대용량 JSON의 경우 `linguist-generated`).

## 파일 포인터 검증 (체크섬 재확인용)

Bash에서:
```bash
sha256sum D:/robot/dev/nrobotcheck/resources/baselines/*.json
```

이 README의 해시 접두사와 대조해 자료가 이동·변경되지 않았는지 확인.

## 관련 설계 문서

- [`../../docs/design/07-scan-engine-and-benchmarks.md`](../../docs/design/07-scan-engine-and-benchmarks.md) §7.13 — 기존 자산 승계
- [`../../docs/design/12-migration-and-non-goals.md`](../../docs/design/12-migration-and-non-goals.md) §12.2 Tier 분류, §12.4 변환 도구 스펙
- [`../../SESSION_HANDOFF.md`](../../SESSION_HANDOFF.md) — "전신 리포와의 연결"
