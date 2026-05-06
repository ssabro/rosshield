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
      // 방어: backend ECONNREFUSED 등 비정상 응답에서 data가 undefined일 수 있음.
      // 그대로 setSession에 넘기면 destructure가 폭발 → user에게 의미 없는 에러.
      // openapi-fetch 타입 좁힘으로 이 분기에서 response가 `never`가 되어 status 0을 사용한다.
      if (!data) {
        throw new ApiError(
          0,
          '서버 응답이 비어 있습니다. 백엔드 서버(8080)가 떠있는지 확인하세요.',
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
// 5-pre) Insights (E19-1)
// ────────────────────────────────────────────────────────────────────────

export interface Insight {
  id: string
  tenantId: string
  kind: string // "drift" | "anomaly" | "peer" | ...
  severity: string // "info" | "low" | "medium" | "high" | "critical"
  robotId?: string
  fleetId?: string
  checkId?: string
  summary: string
  reasoning?: string
  rulesApplied?: string[]
  confidence: number
  producedBy: string
  createdAt: string
  dismissedAt?: string
  dismissedBy?: string
}

export interface InsightsFilter {
  kind?: string
  severity?: string
  robotId?: string
  limit?: number
}

export const useInsights = (filter?: InsightsFilter) => {
  return useQuery({
    queryKey: ['insights', filter ?? null],
    queryFn: async (): Promise<Insight[]> => {
      const { data, error, response } = await apiClient.GET('/api/v1/insights', {
        params: { query: (filter ?? {}) as Record<string, string | number> },
      })
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const payload = data as unknown as { insights: Insight[] }
      return payload.insights ?? []
    },
  })
}

export interface DismissInsightVars {
  insightId: string
  reason: string
}

export const useDismissInsight = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: DismissInsightVars): Promise<Insight> => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/insights/{insightId}:dismiss',
        {
          params: { path: { insightId: vars.insightId } },
          body: { reason: vars.reason },
        },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as Insight
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['insights'] })
    },
  })
}

// ────────────────────────────────────────────────────────────────────────
// 5-pre/2) Compliance (E19-2)
// ────────────────────────────────────────────────────────────────────────

export interface ComplianceProfile {
  id: string
  tenantId: string
  framework: string // "isms-p" | "iso27001-2022" | "nist-800-53-rev5"
  frameworkVersion: string
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface ComplianceControlStatus {
  controlId: string
  status: string // "pass" | "fail" | "partial" | "not_applicable" | "unmapped"
  passCount: number
  failCount: number
  notes?: string
}

export interface ComplianceSnapshot {
  id: string
  tenantId: string
  profileId: string
  sessionId?: string
  overallScore: number
  passCount: number
  failCount: number
  partialCount: number
  notApplicableCount: number
  unmappedCount: number
  chainHeadSeq: number
  chainHeadHash: string
  statuses?: ComplianceControlStatus[]
  createdAt: string
}

export const useComplianceProfiles = () => {
  return useQuery({
    queryKey: ['compliance', 'profiles'],
    queryFn: async (): Promise<ComplianceProfile[]> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/compliance/profiles',
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const payload = data as unknown as { profiles: ComplianceProfile[] }
      return payload.profiles ?? []
    },
  })
}

export const useComplianceSnapshots = (profileId?: string) => {
  return useQuery({
    queryKey: ['compliance', 'snapshots', profileId ?? null],
    queryFn: async (): Promise<ComplianceSnapshot[]> => {
      if (!profileId) return []
      const { data, error, response } = await apiClient.GET(
        '/api/v1/compliance/profiles/{profileId}/snapshots',
        { params: { path: { profileId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const payload = data as unknown as { snapshots: ComplianceSnapshot[] }
      return payload.snapshots ?? []
    },
    enabled: !!profileId,
  })
}

export interface CreateComplianceProfileVars {
  framework: 'isms-p' | 'iso27001-2022' | 'nist-800-53-rev5'
  frameworkVersion: string
  enabled?: boolean
}

export const useCreateComplianceProfile = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (
      vars: CreateComplianceProfileVars,
    ): Promise<ComplianceProfile> => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/compliance/profiles',
        { body: vars },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as ComplianceProfile
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['compliance', 'profiles'] })
    },
  })
}

export interface GenerateSnapshotVars {
  profileId: string
  sessionId: string
}

export const useGenerateSnapshot = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (
      vars: GenerateSnapshotVars,
    ): Promise<ComplianceSnapshot> => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/compliance/profiles/{profileId}/snapshots',
        {
          params: { path: { profileId: vars.profileId } },
          body: { sessionId: vars.sessionId },
        },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as ComplianceSnapshot
    },
    onSuccess: (_, vars) => {
      qc.invalidateQueries({
        queryKey: ['compliance', 'snapshots', vars.profileId],
      })
    },
  })
}

// ────────────────────────────────────────────────────────────────────────
// 5-pre/3) Advisor (E19-3) — spec 미정의. 직접 fetch 경유.
// ────────────────────────────────────────────────────────────────────────

export interface AdvisorConversation {
  id: string
  tenantId: string
  userId: string
  title: string
  createdAt: string
  updatedAt: string
}

export interface AdvisorToolCall {
  id: string
  toolName: string
  argsJson?: string
  resultJson?: string
  error?: string
  durationMs: number
}

export interface AdvisorTurn {
  id: string
  conversationId: string
  role: string // "user" | "assistant" | "tool" | "system"
  content: string
  sequence: number
  llmProvider?: string
  llmModel?: string
  inputTokens?: number
  outputTokens?: number
  costUsd?: number
  createdAt: string
  toolCalls?: AdvisorToolCall[]
}

export interface AdvisorConversationDetail {
  conversation: AdvisorConversation
  turns: AdvisorTurn[]
}

export interface AdvisorAskResponse {
  conversationId: string
  finalAnswer: string
  turns: AdvisorTurn[]
}

async function advisorFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const accessToken = useAuthStore.getState().accessToken
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...((init?.headers as Record<string, string> | undefined) ?? {}),
  }
  if (accessToken) {
    headers['Authorization'] = `Bearer ${accessToken}`
  }
  const res = await fetch(path, { ...init, headers })
  if (res.status === 401) {
    useAuthStore.getState().clearSession()
  }
  if (!res.ok) {
    let message = res.statusText
    try {
      const body: unknown = await res.json()
      message = extractErrorMessage(body, res.statusText)
    } catch {
      /* fallback */
    }
    throw new ApiError(res.status, message)
  }
  return (await res.json()) as T
}

export const useAdvisorConversations = () => {
  return useQuery({
    queryKey: ['advisor', 'conversations'],
    queryFn: async (): Promise<AdvisorConversation[]> => {
      const body = await advisorFetch<{ conversations: AdvisorConversation[] }>(
        `${API_BASE_PATH}/advisor/conversations`,
      )
      return body.conversations ?? []
    },
  })
}

export const useAdvisorConversation = (conversationId?: string) => {
  return useQuery({
    queryKey: ['advisor', 'conversation', conversationId ?? null],
    queryFn: async (): Promise<AdvisorConversationDetail> => {
      if (!conversationId) {
        return {
          conversation: {
            id: '',
            tenantId: '',
            userId: '',
            title: '',
            createdAt: '',
            updatedAt: '',
          },
          turns: [],
        }
      }
      return advisorFetch<AdvisorConversationDetail>(
        `${API_BASE_PATH}/advisor/conversations/${encodeURIComponent(conversationId)}`,
      )
    },
    enabled: !!conversationId,
  })
}

export interface AskAdvisorVars {
  conversationId?: string
  question: string
  maxToolCalls?: number
}

export const useAskAdvisor = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: AskAdvisorVars): Promise<AdvisorAskResponse> => {
      return advisorFetch<AdvisorAskResponse>(
        `${API_BASE_PATH}/advisor/conversations:ask`,
        {
          method: 'POST',
          body: JSON.stringify(vars),
        },
      )
    },
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ['advisor', 'conversations'] })
      qc.invalidateQueries({
        queryKey: ['advisor', 'conversation', data.conversationId],
      })
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
