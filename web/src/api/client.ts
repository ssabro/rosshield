import createClient, { type Middleware } from 'openapi-fetch'

import { useAuthStore } from '@/stores/auth'

import type { paths } from './types'

// rosshield Web Console HTTP 클라이언트.
// - baseUrl은 빈 문자열 — OpenAPI spec의 paths가 풀 경로(`/api/v1/...`)로 정의됨.
//   dev: Vite proxy `/api` → :8080 / prod: 동일 origin.
// - 모든 요청에 Authorization: Bearer <accessToken> 자동 부착.
// - 401 응답 시 세션 클리어. UI redirect는 Stage C에서 라우터 가드로 처리.

export const API_BASE_PATH = '/api/v1'

const baseUrl = ''

const authMiddleware: Middleware = {
  async onRequest({ request }) {
    const token = useAuthStore.getState().accessToken
    if (token) {
      request.headers.set('Authorization', `Bearer ${token}`)
    }
    return request
  },
  async onResponse({ response }) {
    if (response.status === 401) {
      useAuthStore.getState().clearSession()
    }
    return response
  },
}

export const apiClient = createClient<paths>({ baseUrl })
apiClient.use(authMiddleware)
