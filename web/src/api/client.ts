import createClient, { type Middleware } from 'openapi-fetch'

import { useAuthStore } from '@/stores/auth'

import type { paths } from './types'

// rosshield Web Console HTTP 클라이언트.
// - baseUrl은 빈 문자열 — OpenAPI spec의 paths가 풀 경로(`/api/v1/...`)로 정의됨.
//   dev: Vite proxy `/api` → :8080 / prod: 동일 origin.
// - 모든 요청에 Authorization: Bearer <accessToken> 자동 부착.
// - 모든 요청에 X-Cookie-Auth: true (C6 — refresh token cookie 모드 트리거).
// - 모든 요청에 credentials: 'include' (HttpOnly cookie 동봉).
// - 401 응답 시 /auth/refresh 자동 시도 → 성공 시 원 요청 재시도, 실패 시 세션 클리어.

export const API_BASE_PATH = '/api/v1'

const baseUrl = ''

// refreshPromise는 동시 401에서 refresh를 한 번만 호출하기 위한 dedupe.
//   여러 요청이 동시에 401을 받아도 동일 Promise를 await — 토큰 1회 갱신.
let refreshPromise: Promise<string | null> | null = null

// callRefresh는 /auth/refresh를 호출하고 새 access token을 반환합니다.
//   실패 시 null 반환 — 호출자(middleware)가 세션 클리어를 결정.
async function callRefresh(): Promise<string | null> {
  try {
    const r = await fetch(`${API_BASE_PATH}/auth/refresh`, {
      method: 'POST',
      credentials: 'include',
      headers: {
        'X-Cookie-Auth': 'true',
        'Content-Type': 'application/json',
      },
      body: '{}',
    })
    if (!r.ok) return null
    const data = (await r.json()) as { accessToken?: string }
    if (!data.accessToken) return null
    useAuthStore.getState().setAccessToken(data.accessToken)
    return data.accessToken
  } catch {
    return null
  }
}

// dedupedRefresh는 동시 401 흐름에서 refresh 호출을 1회만 실행합니다.
function dedupedRefresh(): Promise<string | null> {
  if (refreshPromise) return refreshPromise
  refreshPromise = callRefresh().finally(() => {
    refreshPromise = null
  })
  return refreshPromise
}

const authMiddleware: Middleware = {
  async onRequest({ request }) {
    const token = useAuthStore.getState().accessToken
    if (token) {
      request.headers.set('Authorization', `Bearer ${token}`)
    }
    request.headers.set('X-Cookie-Auth', 'true')
    return request
  },
  async onResponse({ request, response }) {
    if (response.status !== 401) return response
    // 무한 재귀 방지 — refresh 자체가 401이면 세션 클리어로 끝.
    if (request.url.includes('/auth/refresh') || request.url.includes('/auth/login')) {
      useAuthStore.getState().clearSession()
      return response
    }
    // X-Retry로 재시도 표시 — 두 번째 401에서 멈춤.
    if (request.headers.get('X-Retry') === '1') {
      useAuthStore.getState().clearSession()
      return response
    }
    const newToken = await dedupedRefresh()
    if (!newToken) {
      useAuthStore.getState().clearSession()
      return response
    }
    // 원 요청 재시도 — 새 토큰으로 헤더 갱신.
    const retried = await fetch(request.url, {
      method: request.method,
      headers: appendRetryHeader(request.headers, newToken),
      body: request.body,
      credentials: 'include',
    })
    return retried
  },
}

function appendRetryHeader(headers: Headers, token: string): Headers {
  const h = new Headers(headers)
  h.set('Authorization', `Bearer ${token}`)
  h.set('X-Retry', '1')
  h.set('X-Cookie-Auth', 'true')
  return h
}

export const apiClient = createClient<paths>({
  baseUrl,
  // openapi-fetch는 fetchOptions를 spec 단위로 받지 않으므로 init 옵션은 createClient에 전달.
  credentials: 'include',
})
apiClient.use(authMiddleware)
