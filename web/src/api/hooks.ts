import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { useAuthStore } from '@/stores/auth'

import { API_BASE_PATH, apiClient } from './client'
import { ApiError, extractErrorMessage } from './errors'

import type { User } from '@/stores/auth'

// rosshield Web Console TanStack Query 훅.
//
// 설계 메모:
// - openapi-fetch가 spec의 `Envelope` 형태를 반환하지만, 실제 서버 응답은
//   사용자 합의 형식(envelope 없는 평탄 객체). Stage B는 spec 변경 금지(R12)
//   범위라, 응답 본문은 inline 타입으로 단언(cast)한다.
// - reports endpoint는 spec에 미정의 — 직접 fetch 사용.
// - 401은 client.ts middleware가 세션을 클리어하지만, 호출 측의 throw는
//   여전히 ApiError로 전달돼야 React Query가 isError로 인식한다.

// ────────────────────────────────────────────────────────────────────────
// 1) Login
// ────────────────────────────────────────────────────────────────────────

export interface LoginVars {
  email: string
  password: string
}

export interface LoginResponse {
  accessToken: string
  refreshToken: string
  user: User
}

export const useLogin = () => {
  const setSession = useAuthStore((s) => s.setSession)
  return useMutation({
    mutationFn: async (vars: LoginVars): Promise<LoginResponse> => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/auth/login',
        { body: vars },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      // 서버 합의 응답 형식(envelope 없음) — spec과의 차이는 의도된 것.
      return data as unknown as LoginResponse
    },
    onSuccess: (data) => {
      setSession(data)
    },
  })
}

// ────────────────────────────────────────────────────────────────────────
// 2) Me — 현재 사용자
// ────────────────────────────────────────────────────────────────────────

export const useMe = () => {
  const accessToken = useAuthStore((s) => s.accessToken)
  return useQuery({
    queryKey: ['me'],
    queryFn: async (): Promise<User> => {
      const { data, error, response } = await apiClient.GET('/api/v1/auth/me')
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as User
    },
    enabled: !!accessToken,
  })
}

// ────────────────────────────────────────────────────────────────────────
// 3) Robots
// ────────────────────────────────────────────────────────────────────────

export interface Robot {
  id: string
  tenantId: string
  fleetId: string
  name: string
  host: string
  port: number
  authType: string
  criticality: string
  [key: string]: unknown
}

export const useRobots = (fleetId?: string) => {
  return useQuery({
    queryKey: ['robots', fleetId ?? null],
    queryFn: async (): Promise<Robot[]> => {
      const { data, error, response } = await apiClient.GET('/api/v1/robots', {
        // openapi-fetch는 spec에 정의된 query 파라미터(limit/cursor/sort/fleetId/...)
        // 만 받지만, 우리는 fleetId만 사용한다.
        params: { query: fleetId ? { fleetId } : {} },
      })
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const payload = data as unknown as { robots: Robot[] }
      return payload.robots
    },
  })
}

// ────────────────────────────────────────────────────────────────────────
// 4) Start scan
// ────────────────────────────────────────────────────────────────────────

export interface StartScanVars {
  fleetId: string
  packId: string
  trigger?: string
}

export interface ScanSession {
  sessionId: string
  tenantId: string
  fleetId: string
  packId: string
  trigger: string
  status: string
  total: number
  completed: number
  failed: number
}

export const useStartScan = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: StartScanVars): Promise<ScanSession> => {
      // spec의 `/api/v1/scans` requestBody는 `{type: object}`로만 정의돼
      // 생성 타입이 `Record<string, never>` — 실제 합의된 body로 cast.
      const { data, error, response } = await apiClient.POST('/api/v1/scans', {
        body: vars as unknown as Record<string, never>,
      })
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as ScanSession
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['scans'] })
    },
  })
}

// ────────────────────────────────────────────────────────────────────────
// 5) Reports — spec 미정의. 직접 fetch 경유.
// ────────────────────────────────────────────────────────────────────────

export interface Report {
  id: string
  tenantId: string
  sessionId: string
  format: string
  pdfSha256: string
  pdfSizeBytes: number
  generatedAt: string
  generatedBy: string
  signed: boolean
}

export const useReports = (sessionId?: string) => {
  return useQuery({
    queryKey: ['reports', sessionId ?? null],
    queryFn: async (): Promise<Report[]> => {
      const url = new URL(`${API_BASE_PATH}/reports`, window.location.origin)
      if (sessionId) {
        url.searchParams.set('sessionId', sessionId)
      }

      const accessToken = useAuthStore.getState().accessToken
      const headers: HeadersInit = {}
      if (accessToken) {
        headers['Authorization'] = `Bearer ${accessToken}`
      }

      // 상대 경로로 fetch (절대 URL은 prod 동일 origin에서 무관, dev에서는
      // proxy 적용을 위해 pathname+search만 사용).
      const res = await fetch(url.pathname + url.search, { headers })
      if (res.status === 401) {
        useAuthStore.getState().clearSession()
      }
      if (!res.ok) {
        let message = res.statusText
        try {
          const body: unknown = await res.json()
          message = extractErrorMessage(body, res.statusText)
        } catch {
          /* JSON 파싱 실패 시 statusText fallback */
        }
        throw new ApiError(res.status, message)
      }
      const body = (await res.json()) as { reports: Report[] }
      return body.reports
    },
  })
}
