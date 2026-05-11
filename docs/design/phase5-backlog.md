# rosshield Phase 5 Backlog — Enterprise & Appliance

**범위**: 12~16주 (`docs/design/11-tech-stack-and-roadmap.md` §11.13 Phase 4 "Appliance" + Phase 5 "생태계" 일부 묶음).

> **명명 주의**: 본 backlog의 "Phase 5"는 *backlog 문서 일련 번호*이며, 설계서 §11.13 단계 정의의 **Phase 4(Appliance) 본체 + Phase 5(생태계) 진입**을 함께 다룬다. 실제 구현 진척이 설계서 단계 정의보다 한 backlog 앞서 있는 상태(Phase 0~3 backlog는 설계서 Phase 0~3와 동등, Phase 4 backlog는 production hardening 신규 분리, 본 Phase 5 backlog가 설계서 Phase 4를 흡수).

**전제**: Phase 4 carryover 11/11 완료(release 파이프라인 + cosign keyless + audit verify SDK + Prometheus + backup + CLI ext + SSO autoprov + invite email + webhook UI + demo seed). v0.2.0 47 assets 게시. R30-4 결정으로 D5/D6/R20-2 종결(Open-core + Apache 코어 + private + 단일 repo build tag).

**Exit 기준** (둘 다 만족):
1. **Enterprise build tag 분리 + D8 1순위 결합 청구항(A-1+B-1+C-1+D-3) 코드 분리 완료** (`internal/enterprise/*` + `//go:build rosshield_enterprise`)
2. **첫 paying customer 또는 어플라이언스 PoC 파트너 1곳에 30일 운영 deployment + incident 0**

---

## Phase 4 Carryover (deferred — Phase 5 합류)

| ID | 출처 | 내용 | 추정 | 상태 |
|---|---|---|---|---|
| E25 | Phase 4 backlog | HA — PostgreSQL advisory lock + leader/follower (single-writer audit chain) | 4일 | ✅ 완료 (2026-05-11 `678367c`+`648ce9e`+`e03cc6c`+`bb6c541`+`a1ae047`+`c76164e` — Stage 1~4 모두 마감) |
| E22-F | Phase 4 backlog | PG-native repo 분리 — JSONB·TIMESTAMPTZ·BOOLEAN 활용 + driver-aware repo (현재 sqliterepo 단일 경로) | 1주 | 🟡 1차 완료 (2026-05-11 `f3bf23f` — R30-1=C 하이브리드, 핫 path 3 컬럼 TIMESTAMPTZ+JSONB 회수, BOOLEAN은 driver mismatch 위험으로 보류) |

---

## Phase 5 신규 Epic

### E31. Enterprise build tag scaffold (1주)

**왜**: D5(Open-core 결정) + D8 1순위 결합 청구항 출원 후 Apache-2.0 §3 patent grant 회피를 위해 핵심 알고리즘은 enterprise build tag로 격리. 코어는 인터페이스 + 결정론적 fallback만 노출.

#### 스코프
- 디렉터리: `internal/enterprise/{crosswitness,selectdisclose,multihash,wasmrt,robotid,rostopo,fleetxval}`
- 모든 enterprise 파일에 `//go:build rosshield_enterprise` 가드
- 코어 측 인터페이스 정의(`internal/domain/.../enterprise_iface.go`)
- 코어 빌드(`go build`)는 enterprise 미포함 → 결정론적 fallback 동작
- enterprise 빌드(`go build -tags rosshield_enterprise`)는 모든 청구항 활성
- Makefile `build-enterprise` 타깃
- CI matrix에 `tags=rosshield_enterprise` job 추가 (둘 다 통과 보장)

#### TDD 태스크
| ID | 테스트 | 구현 |
|---|---|---|
| E31.T1 | `TestCoreBuildExcludesEnterprisePackages` | `go list -tags ''` 결과에 enterprise 경로 0개 |
| E31.T2 | `TestEnterpriseBuildIncludesEnterprisePackages` | `go list -tags rosshield_enterprise` 결과에 7개 경로 |
| E31.T3 | `TestCoreFallbackProducesValidAuditChain` | enterprise 미포함 빌드도 audit chain 단일 테넌트로 정상 동작 |

#### Exit 기준
- 두 빌드 모두 CI 그린
- `internal/enterprise/*` import는 코어 패키지에서 0건 (린트 가드)

---

### E32. D8 1순위 결합 청구항 구현 (3주, 변리사 출원 후)

**왜**: D8-4 출원 전 잠금 해제 후 1순위 결합 청구항(A-1 cross-witness fold-in + B-1 multi-hash evidence + C-1 WASM sandboxed evaluator + D-3 robot identity binding) 실제 코드. enterprise build tag 안.

#### 스코프
- **A-1 cross-witness fold-in**: `internal/enterprise/crosswitness/` — 테넌트 간 audit checkpoint를 결정론적 정렬 + Merkle fold-in 후 signed witness token 발행. Sentinel: `ErrCrossWitnessMismatch`.
- **B-1 multi-hash evidence**: `internal/enterprise/multihash/` — evidence blob에 SHA256 + BLAKE3 + (옵션) SHA3-512 다중 해시 + redaction manifest(saltedDigest 외부 검증).
- **C-1 WASM sandboxed evaluator**: `internal/enterprise/wasmrt/` — wazero 런타임 + check rule WASM 실행 + memory/CPU 제한 + 결정론적 시드.
- **D-3 robot identity binding**: `internal/enterprise/robotid/` — TPM EK + MAC + CPU serial 결합 fingerprint + 결정론적 비교.
- 청구항별 통합 테스트 + golden vector
- 각 패키지 README에 청구항 매핑 + 출원번호 참조

#### TDD 태스크
| ID | 테스트 | 구현 |
|---|---|---|
| E32.T1 | `TestCrossWitnessFoldInDeterministic` | 동일 입력 → 동일 witness token (정렬·canonicalization 검증) |
| E32.T2 | `TestMultiHashEvidenceDetectsTamper` | SHA256만 일치하고 BLAKE3 불일치 시 FAIL |
| E32.T3 | `TestWasmEvaluatorRespectsMemoryLimit` | 메모리 한도 초과 → ErrWasmResourceLimit |
| E32.T4 | `TestRobotIdentityBindingStable` | 동일 hardware → 동일 fingerprint, 1 bit 변경 → 다른 fingerprint |

#### 의존
- D8 변리사 컨설팅 → 청구 범위 확정 → KR 우선출원 완료 (D8-4 잠금 해제 선행)
- E31 enterprise build tag scaffold

#### Exit 기준
- 4 청구항 모두 enterprise 빌드에서 동작 + 통합 테스트 그린
- 변리사가 명세서 실시례와 코드 간 1:1 매핑 확인

---

### E33. Ubuntu Core 이미지 빌드 파이프라인 (1주)

**왜**: D4(Ubuntu Core 24 기본 가정) + 어플라이언스 PoC 파트너 배포 경로.

#### 스코프
- snapcraft `snapcraft.yaml` — rosshield-server를 strict confined snap으로 패키징
- `snap-build` GitHub Actions job — tag push 시 amd64 + arm64 snap 자동 생성
- `core22` base + 필요한 plug(network, network-bind, system-files for /var/lib/rosshield)
- snap install 후 `snap services` 자동 시작
- README "어플라이언스 설치" 섹션

#### TDD 태스크
| ID | 테스트 | 구현 |
|---|---|---|
| E33.T1 | `TestSnapBuildProducesValidArtifact` (CI) | snapcraft → .snap 파일 생성 + signature |
| E33.T2 | `TestSnapInstallSmokeTest` (CI, multipass) | LXD/multipass에 snap install → /healthz 200 |

#### Exit 기준
- v0.x.0 release에 .snap 자산 추가 게시
- 외부 검증자가 multipass에 install + admin login 가능

---

### E34. TPM 키 봉인 + Secure Boot (1.5주)

**왜**: 어플라이언스에서 KEK(키 암호화 키)를 디스크에 평문 저장하지 않고 TPM 2.0에 봉인. Secure Boot로 부팅 체인 무결성 보장.

#### 스코프
- `internal/platform/keystore/tpm/` — go-tpm2 어댑터 (TPM EK 활용 PCR 봉인)
- `--keystore=tpm` flag (기본은 file)
- Secure Boot enrollment 가이드 (Ubuntu Core 24 기준)
- TPM 미장착 환경에서는 즉시 실패 (조용한 fallback 금지)

#### TDD 태스크
| ID | 테스트 | 구현 |
|---|---|---|
| E34.T1 | `TestTpmKeySealRoundTrip` (TPM 시뮬레이터) | seal → unseal 동등 |
| E34.T2 | `TestTpmUnsealFailsWhenPcrChanged` | PCR 값 변조 → unseal 실패 |
| E34.T3 | `TestKeystoreTpmRefusesIfNoTpmDevice` | /dev/tpm0 부재 → 부팅 실패 |

#### Exit 기준
- 레퍼런스 어플라이언스에서 KEK 디스크 평문 저장 0
- Secure Boot 활성 상태로 부팅 + 정상 동작

---

### E35. A/B OTA 업데이트 (1주)

**왜**: 어플라이언스 원격 업데이트. 실패 시 자동 롤백.

#### 스코프
- snap channel 활용 (stable/candidate/edge) — 자체 업데이트 메커니즘 재발명 회피
- `rosshield-server` snap이 자동 update 정책 노출
- 업데이트 후 healthz 실패 시 snap 자동 롤백 (snap refresh --revert)
- Ed25519로 update payload 서명 (snap 자체 서명 외 추가 layer)

#### TDD 태스크
| ID | 테스트 | 구현 |
|---|---|---|
| E35.T1 | `TestOtaSignatureRejectsUntrustedKey` | 다른 키로 서명한 update → reject |
| E35.T2 | `TestOtaRollbackOnHealthzFailure` (multipass) | 의도적으로 깨진 update → 자동 롤백 + 이전 버전 healthz 200 |

#### Exit 기준
- snap refresh로 v0.x.0 → v0.x.1 무중단 업데이트 + 실패 시 자동 롤백 시연

---

### E36. 레퍼런스 하드웨어 테스트 (3일)

**왜**: D4 §11.9 기본 가정대로 NUC + OptiPlex 같은 흔한 mini-PC 2종에서 동작 검증. 자체 HW 제조 비목표(원칙 §12) 준수.

#### 스코프
- 하드웨어 매트릭스 문서: `docs/appliance/reference-hardware.md`
- TPM 2.0 칩 유무 / Secure Boot 지원 / amd64 vs arm64
- 각 모델별 install 시간·메모리 footprint·idle CPU·full scan latency 측정

#### Exit 기준
- 2 모델 모두 30분 install + 8시간 burn-in test 통과
- 결과 표 docs/appliance/에 게시

---

### E37. D1 제품명 확정 + public 전환 (3일)

**왜**: D8-4 출원 잠금 해제 후 가능. R30-4 결정의 후속 트리거(첫 enterprise customer 또는 Phase 5 진입 시 재논의 옵션).

#### 의존
- E32 D8 청구항 구현 + KR 출원 완료 (D8-4 잠금 해제)

#### 스코프
- 상표 검색 (KIPO + USPTO + EUIPO + WIPO Madrid) — 메모리 [`feedback_naming_verification.md`](memory) 따라 WebSearch 필수
- 후보 3개 → 도메인 가용성 + 발음·기억성 평가
- 결정 후 `<ProductName>` placeholder 일괄 치환 (코드 네임스페이스 `rosshield`는 그대로)
- README + LICENSE + 모든 docs 갱신
- GitHub repo private → public 전환

#### Exit 기준
- 제품 브랜드 확정 + 모든 사용자 대면 문자열 갱신
- public repo로 전환 (D6 잠금 해제)

---

### E38. 첫 paying customer onboarding (지속, 별도 트랙)

**왜**: Phase 5 Exit의 두 번째 조건. 코드 작업 < 영업·docs·support 작업.

#### 스코프
- 30분 onboarding 자료 (시연 영상 + step-by-step doc)
- support 채널 (이메일·Slack·discord 중 1개)
- SLA 문서 (Phase 5 = best-effort, 24h 응답 약속)
- usage 통계 수집 합의 (옵트인) — Prometheus metric tenant aggregate

#### Exit 기준
- 1 customer가 30일 운영 + 통신 incident 1건 미만

---

## Phase 5 변리사·법무 트랙 (E32 의존)

| ID | 작업 | 추정 |
|---|---|---|
| O9 | 변리사 컨설팅 의뢰 (`spec-candidate-A-draft.md` + `spec-A-review-and-revision-plan.md` 전달) | 1주 |
| O10 | 청구 범위 확정 + 명세서 최종화 | 2주 |
| O11 | KR 우선출원 (KIPO) | 1~2주 (변리사 측) |
| O12 | 12개월 내 PCT 출원 평가 | Phase 5 종료 직전 |

---

## 의존 그래프

```
Carryover E25 HA ────┐
Carryover E22-F PG-native ────┤   (병렬, 운영 강화)
                              │
E31 enterprise build tag ─────┤
                              │
O9~O11 변리사 + 출원 ─────────┘────→ E32 D8 청구항 구현 (출원 잠금 해제 후)
                                          │
E33 Ubuntu Core snap ────────────────────┤
E34 TPM 봉인 + Secure Boot ──────────────┤
E35 A/B OTA ─────────────────────────────┤
E36 레퍼런스 HW 테스트 ──────────────────┤
                                          │
E37 D1 + public 전환 (출원 후) ──────────┤
                                          │
E38 첫 paying customer onboarding ───────┘ (지속 트랙)
```

병렬 가능: Carryover + E31 + 변리사 트랙 + 어플라이언스 epic(E33~E36)이 모두 독립. E32·E37은 출원 완료 의존.

---

## 추정 (병렬 + 1인 운영 가정)

| Epic | 단독 추정 | 병렬 단축 |
|---|---|---|
| E25 HA carryover | 4일 | (병렬) |
| E22-F PG-native carryover | 1주 | (병렬) |
| E31 enterprise build tag | 1주 | (병렬) |
| E32 D8 청구항 구현 | 3주 | (출원 후) |
| E33 Ubuntu Core snap | 1주 | (병렬) |
| E34 TPM + Secure Boot | 1.5주 | (병렬) |
| E35 A/B OTA | 1주 | (병렬) |
| E36 레퍼런스 HW | 3일 | (병렬) |
| E37 D1 + public | 3일 | (출원 후) |
| O9~O11 변리사·출원 | 4~5주 (외부 의존) | (병렬) |
| E38 onboarding | 지속 | (지속 트랙) |
| **합계** | **~14주** | **~10주** (출원 외부 lead time 가정 4주) |

---

## 리스크 (Phase 5 한정)

| 리스크 | 완화 |
|---|---|
| 출원 lead time이 어플라이언스 진척 차단 | E32 외 epic 모두 출원 무관 → 병렬 진행 |
| TPM 시뮬레이터와 실 TPM 동작 차이 | E36 레퍼런스 HW 테스트에서 실 TPM 검증 강제 |
| Snap confinement이 ROS2 수집과 충돌 | classic confinement fallback 검토(시연 한정) |
| Apache-2.0 §3 patent grant가 enterprise 패키지 인용으로 누설 | 코어 패키지가 enterprise 절대 import 금지 — E31.T1 린트 가드 |
| 첫 customer가 self-host 어려워 churn | 시연 영상 + 1:1 onboarding 세션 (E38) |

---

## Phase 5 Exit 체크리스트

- [x] enterprise build tag scaffold + 양 빌드 CI 그린 (E31, 2026-05-11 `5c08f42`)
- [ ] KR 우선출원 완료 + 1순위 결합 청구항 코드 분리 (E32 + O11)
- [x] Ubuntu Core snap install + healthz (E33, 2026-05-11 `616403c`) — 1차 amd64 빌드 + LXD smoke test CI workflow 자동화. arm64·snap store 발행은 후속 stage.
- [x] TPM 키 봉인 + Secure Boot 동작 (E34) — Stage 1+2+3 완료 (2026-05-11 `7550656`+`6563d6a`+`07c6d83`+`e96937c`+`101c618`): keystore 추상 + file/tpm 어댑터 + R41 결정 + go-tpm-tools v0.4.8 PCR-sealed seal/unseal 본체 + simulator integration test 5건 + ci.yml tpm-integration job + bootstrap.buildKeystore 갱신 + Secure Boot enrollment 가이드 docs(456줄, mokutil + tpm2-tools + PCR 변조 시나리오 5종 복구). Stage 4(E36 실 TPM 검증)는 사용자 hands-on 별 epic.
- [x] A/B OTA + 자동 롤백 시연 (E35, 2026-05-11 `c0f8a4b`) — snap post-refresh hook이 healthz 60s polling, 실패 시 exit 1 → snapd 자동 revert. configure hook으로 healthz-url/healthz-timeout 운영자 조정 가능. multipass 실 OTA round-trip 검증은 snap-smoke.yml 확장 후속.
- [ ] 레퍼런스 HW 2 모델 burn-in (E36) — 🟡 docs scaffolding 완료 (2026-05-11 `5bd0d0f` — 4 모델 매트릭스 + 10 측정 항목 + 6단 절차 + 52 TBD placeholder + 트러블슈팅 9 + 5년 TCO). 실 측정 hands-on은 사용자 측 작업.
- [ ] D1 제품명 확정 + public 전환 (E37)
- [x] HA 2 인스턴스 leader/follower (E25, 2026-05-11) — Stage 1~4 모두 마감 (Manager + audit gate + middleware + scheduler + testcontainers + compose-ha + 운영 docs)
- [x] PG-native repo 분리 1차 (E22-F, 2026-05-11 `f3bf23f`) — R30-1=C 하이브리드, 핫 path 3 컬럼 회수. BOOLEAN/A Big bang은 customer query plan 분석 후 점진
- [x] B6+B7 통합 /system 운영 dashboard 페이지 (Phase 4 web 갭, 2026-05-11 `4d8a7a8`) — 헬스·HA·라이선스·백업 4 카드, 백엔드 변경 0
- [x] B7 후속 Stage 1 — 자동 백업 schedule + GET /api/v1/backups list (2026-05-11 `d1bf511`) — executeBackup 헬퍼 추출 + registerBackupJob cronsched 결선(HA leader-only 자동) + listBackups 디렉터리 스캔 + handler envelope.
- [x] B7 후속 Stage 2 — download endpoint + web BackupsCard 동적 (2026-05-11 `54f0384`+`aa4cc0a`) — Stage 2-A(download handler + path traversal 방어 + http.ServeContent + 5 단위) + Stage 2-B(useBackups hook + BackupsCard 동적 5개 표시 + Download anchor + 12 i18n 키).
- [x] B7 후속 Stage 2-C — openapi spec + snap configure + formatBytes 승격 (2026-05-11 `0d065f7`+`2420ec3`) — openapi.yaml +104줄(GET /api/v1/backups + /download endpoint + BackupMeta·BackupListResponse schema) + Go gen + TS types 재생성 + snap/hooks/configure에 backup-schedule/skip-evidence/dir 검증 + web/src/lib/utils.ts에 formatBytes export + 단위 10건. **잔여 후속**: useBackups apiClient 전환(useLicenseInfo와 일괄 별 epic) + RBAC role check.
- [ ] 첫 customer 30일 운영 + incident 0 (E38) — onboarding 사전 자료 ✅ 2026-05-11 `58b5e81`

---

## R41 결정 / 결정 항목 (E34 TPM 본격 구현, design doc `e34-tpm-design.md` 권고 모두 채택)

- **R41-1** KeyStore 모델 — ✅ **2026-05-11**: **B (TPM Keystore + soft Signer)**. 모든 TPM 2.0 칩 호환 + Stage 1 keystore 추상과 일관.
- **R41-2** Go TPM 라이브러리 — ✅ **2026-05-11**: **google/go-tpm-tools** v0.4.8. high-level seal/unseal, GCP Confidential VM 메인 사용처, CGO=0 유지.
- **R41-3** 기본 PCR set — ✅ **2026-05-11**: **[0, 2, 4, 7]** (BIOS·OPROM·EFI·Secure Boot policy). strict는 enterprise 옵션 후속.

## R40 결정 후보 / 결정 항목

- **R40-1** snap base — ✅ **2026-05-11**: `core22` 결정 (LTS 안정성 우선, 2027까지 지원, snap store 검증).
- **R40-2** TPM 시뮬레이터 — ✅ **2026-05-11**: `swtpm` 결정 (Linux 표준, Ubuntu apt 패키지, KVM/QEMU 호환, CI testcontainers 친화).
- **R40-3** WASM 런타임 — ✅ **2026-05-11**: `wazero` 결정 (Pure Go, CGO=0 유지, cross-compile 손쉬움. 일부 advanced WASM 기능은 미지원이지만 D8-C1 sandboxed check evaluator 요구는 충족).
- **R40-4** 첫 customer SKU — ✅ **2026-05-11**: `Onprem` 결정 (Compose/단일 서버 multi-user, v0.2.0 release 형태, E38 onboarding 자료 onprem 가정과 일관, enterprise 확장성).
- **R40-5** D1 후보 어휘 풀 — ✅ **2026-05-11** (`2d66ae9`): `docs/design/notes/d1-brand-candidates.md` 12 후보 + WebSearch 11회 + Top 3 = Custos·Lodestar·Praxis. 5개 폐기(Sentinel·Aegis·Helix·Vector·Axiom). 변리사 정밀 검색 의뢰 + D1 확정은 출원 잠금(D8-4) 해제 후.

---

## Phase 4 → Phase 5 진입 체크리스트

- [x] phase4-backlog.md → archive로 이전 (2026-05-11)
- [x] phase5-backlog.md(본 문서) 신규 작성 (2026-05-11)
- [x] E31 enterprise build tag scaffold + 양 빌드 CI 그린 (2026-05-11 `5c08f42`)
- [x] E25 HA design doc + R30-2 권고안 — PG advisory lock + leader/follower (2026-05-11 `46cf600`)
- [x] E38 onboarding 사전 자료 (README + quickstart + intake template) (2026-05-11 `58b5e81`)
- [ ] R30-2 사용자 확정 (HA 메커니즘 + lock_id 정책 + sqlite 거부 방식)
- [ ] R40-3 WASM 런타임 결정 (E32 의존)
- [ ] D8 변리사 컨설팅 의뢰 (O9 — 출원 잠금 해제의 critical path, 사용자 외부 위임 트랙)
- [ ] R40-4 첫 customer SKU 결정 (E38 진입 시)

---

## 문서 생명주기

- 본 백로그는 **살아있는 문서**. 태스크 완료 시 `[x]` + 커밋 해시.
- Phase 5 완료 시 `docs/design/archive/phase5-backlog.md`로 이동, Phase 6 백로그를 동일 경로에 신규.
- 결정 사항은 `SESSION_HANDOFF.md` "결정 로그"에 R40-X 형식으로 기록.
