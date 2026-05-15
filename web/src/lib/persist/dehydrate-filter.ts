// PWA persist Stage 2 — react-query dehydrate filter (보안 차단 list)
// (design doc `pwa-persist-design.md` §3.5 + §6.2 + D-PWAPER-5).
//
// 정책: **opt-out** (allow by default + deny list).
//  - 본 epic의 핵심 가치(read 캐시 영속)를 즉시 충족하기 위해 신규 hook은
//    자동으로 영속 OK 상태가 됩니다.
//  - 보안 위험이 있는 query만 명시 deny 처리.
//  - deny list 갱신 시 새 항목 추가 + 본 파일 단위 테스트(`dehydrate-filter.test.ts`)
//    동기 갱신 의무.
//
// 보수성: 의심 시 차단. 다음 기준 중 하나에 해당하면 deny list 추가.
//  - 응답에 secret/token/credential/authorization material 포함 가능.
//  - 응답에 사용자 입력(LLM 대화·자유 텍스트)이 평문 포함되어 디바이스 탈취 시
//    privacy 영향.
//  - 응답이 audit 신뢰성에 직접 영향(stale 표시 시 잘못된 audit 판단 위험).
//
// 비대상:
//  - 응답 본문 sanitize(서버 측 redact 강제) — 별 epic. 본 filter는 dehydrate
//    단계에서 통째로 차단(서버 redact 신뢰 + 추가 방어).
//  - mutation 캐시 — react-query 기본은 mutation 캐시 dehydrate 안 함.
//    `shouldDehydrateMutation` 미설정 시 default false.

/**
 * 영속 차단 query key prefix (D-PWAPER-5 권장 default).
 *
 * `queryKey[0]`이 아래 string 중 하나와 정확 일치하면 IndexedDB 영속 차단.
 * - `sso` — OIDC clientSecret(서버 redact 가정이나 응답 형식 변경 시 위험).
 * - `webhooks` — webhook signing secret(redact 가정).
 * - `invitations` — invitation token(URL 활성 시 가입 권한 부여).
 * - `advisor` — LLM 대화 사용자 입력 민감 가능.
 *
 * 갱신 시 본 파일 단위 테스트(`dehydrate-filter.test.ts`)도 함께 갱신.
 */
export const DENY_KEY_PREFIXES = Object.freeze([
  'sso',
  'webhooks',
  'invitations',
  'advisor',
] as const)

/**
 * react-query `dehydrateOptions.shouldDehydrateQuery` 콜백.
 *
 * 반환:
 *  - `true` — query 결과를 IndexedDB에 영속(default).
 *  - `false` — 영속 차단(deny list 매치).
 *
 * 매치 정책:
 *  - `queryKey[0]`이 string이고 `DENY_KEY_PREFIXES` 중 하나와 정확 일치 → 차단.
 *  - 그 외 모든 경우(빈 queryKey · 비-string · prefix 불일치) → 통과.
 *
 * 비교 정책:
 *  - 정확 일치 — `startsWith` 아님. `'sso-config'` 같은 별도 prefix는 통과.
 *  - 위치 정확 — `queryKey[0]`만 검사. 중간/끝에 deny 단어가 있어도 통과.
 *
 * @param query react-query Query 인스턴스(`queryKey` 만 참조).
 */
export function shouldDehydrateQuery(query: {
  readonly queryKey: ReadonlyArray<unknown>
}): boolean {
  const head = query.queryKey[0]
  if (typeof head !== 'string') {
    // 비-string head는 deny list와 비교 불가 → 통과(allow by default).
    // 사용처 발견 시 별도 정책 필요하면 본 함수 갱신.
    return true
  }
  // O(N) 선형 탐색 — N=4 + filter 호출 빈도 낮음(영속 시점만) → 충분.
  for (const denyPrefix of DENY_KEY_PREFIXES) {
    if (head === denyPrefix) {
      return false
    }
  }
  return true
}
