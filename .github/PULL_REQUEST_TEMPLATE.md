<!--
Lodestar Pull Request

본 template를 채워주세요. 자세한 절차는 CONTRIBUTING.md 참조.
DCO sign-off (`git commit -s`)가 모든 commit에 필요합니다.
-->

## 변경 요약

<!-- 무엇을 · 왜 변경하는지 1~3문장 -->

## 변경 종류

- [ ] 🐛 Bug fix (기존 동작 변경 없음)
- [ ] ✨ New feature (기존 동작 변경 없음)
- [ ] 💥 Breaking change (기존 동작 변경)
- [ ] 📝 Docs only
- [ ] 🔧 Build · CI · infra
- [ ] 🧪 Test only
- [ ] ♻️ Refactor (동작 동일)

## 관련 issue · discussion

<!-- 예: Closes #123, Discussion #45 -->

## 테스트

- [ ] 단위 테스트 추가/갱신
- [ ] 통합 테스트 추가/갱신 (integration tag)
- [ ] 회귀 0 확인 — `make ci` PASS
- [ ] 변경된 패키지 직접 PASS — `go test -count=1 ./internal/<changed>/...`
- [ ] Enterprise 영향 시 — `make test-enterprise` PASS
- [ ] 수동 검증 절차 (필요 시 본 PR 본문에 명시)

## 설계 영향

<!--
- 어떤 설계서(docs/design/*.md)를 갱신했나?
- 설계 원칙 12개(01-principles.md) 중 어디 영향?
- 도메인 경계 · 멀티테넌시 · audit chain · LLM 옵트인 변경?
- 예: "04-domain-and-data-model.md §4.5 robot.fingerprint 필드 추가, R-D8 D-3 청구권 본체와 일관"
-->

## Breaking Change 점검

- [ ] DB 마이그레이션 추가 시 down.sql 작성
- [ ] API 변경 시 openapi.yaml + types.ts 재생성 (`make openapi`)
- [ ] CLI flag 변경 시 `docs/usage/cli.md` 갱신
- [ ] Config schema 변경 시 deprecation notice 또는 backward-compat

## 사전 체크

- [ ] DCO sign-off (`git commit -s`) — 모든 commit에 `Signed-off-by`
- [ ] 행동 강령([CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md)) 준수
- [ ] 기여 절차([CONTRIBUTING.md](../CONTRIBUTING.md)) 준수
- [ ] 파일 ≤ 400줄(권장)/800줄(최대), 함수 ≤ 50줄(권장)
- [ ] CLAUDE.md 규칙 일관 (file/function size · 도메인 경계 · 불변성 · 멀티테넌시)
- [ ] 보안 민감 영역(인증·서명·감사 체인) 변경 시 2인 리뷰 요청
- [ ] secret/key/token 하드코드 0건 (env var 또는 test fixture만)
