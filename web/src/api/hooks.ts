import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'

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
  // C6 — refreshToken은 cookie 모드(X-Cookie-Auth: true)에서 응답 본문에 포함되지 않는다.
  // legacy 모드(CLI)는 본문에 있지만 web 클라이언트는 cookie를 사용하므로 무시.
  refreshToken?: string
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
      if (!data) {
        throw new ApiError(
          0,
          '서버 응답이 비어 있습니다. 백엔드 서버(8080)가 떠있는지 확인하세요.',
        )
      }
      return data as unknown as LoginResponse
    },
    onSuccess: (data) => {
      setSession({ accessToken: data.accessToken, user: data.user })
    },
  })
}

// useLogout은 /auth/logout을 호출해 서버 측 refresh를 revoke + cookie를 만료시킵니다.
//   네트워크 실패에도 클라이언트 세션은 항상 비움 (UX — 사용자 의도는 로그아웃).
export const useLogout = () => {
  const clearSession = useAuthStore((s) => s.clearSession)
  return useMutation({
    mutationFn: async (): Promise<void> => {
      try {
        await fetch(`${API_BASE_PATH}/auth/logout`, {
          method: 'POST',
          credentials: 'include',
          headers: { 'X-Cookie-Auth': 'true' },
        })
      } catch {
        // ignore — 클라이언트 세션은 항상 클리어.
      }
      clearSession()
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

// CreateRobotVars — 평문 자격증명을 본문으로 보냄. 메모리 전용 처리 후 백엔드가 KEK→DEK로 wrap.
//   응답에는 평문 자격증명 미포함 (Robot 메타 + credentialId).
export interface CreateRobotVars {
  fleetId: string
  name: string
  host: string
  port?: number
  authType: 'password' | 'privateKey'
  username: string
  password?: string
  privateKeyPem?: string
  privateKeyPassphrase?: string
  osDistro?: string
  rosDistro?: string
  tags?: string[]
  role?: string
  criticality?: 'low' | 'medium' | 'high' | 'critical'
}

export interface CreateRobotResponse {
  robot: Robot
  credentialId: string
}

// useCreateRobot — POST /api/v1/robots. 성공 시 robots 캐시 무효화.
//   에러 매핑: 400(검증)·401(인증)·409(name·host:port 중복).
export const useCreateRobot = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: CreateRobotVars): Promise<CreateRobotResponse> => {
      // openapi spec의 createRobot body는 빈 객체로만 정의됨 — 실제 서버는 평탄 객체.
      // openapi-fetch 타입 좁힘으로 body가 Record<string, never>가 되어 전달 불가 →
      // never로 단언 후 unknown 경유로 우회 (서버는 JSON body를 그대로 디코드).
      const { data, error, response } = await apiClient.POST('/api/v1/robots', {
        body: vars as unknown as never,
      })
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as CreateRobotResponse
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['robots'] })
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
// 5-pre/4) Scan progress WebSocket (C1 carryover) — useEffect 기반 hook.
// ────────────────────────────────────────────────────────────────────────

export interface ScanProgressMessage {
  kind: 'progress' | 'completed' | string
  type: string
  sessionId: string
  total: number
  completed: number
  failed: number
  status?: string
  reason?: string
  occurredAt: string
  correlationId?: string
}

export type ScanProgressStatus =
  | 'idle'
  | 'connecting'
  | 'streaming'
  | 'completed'
  | 'error'

export interface UseScanProgressResult {
  status: ScanProgressStatus
  latest: ScanProgressMessage | null
  error: string | null
}

// useScanProgress는 /api/v1/scans/{sessionId}/progress WebSocket을 구독합니다.
//
// 디자인:
//  - 첫 마운트 + sessionId 변경 시 새 connection.
//  - access token은 Authorization 헤더로 보내야 하나 브라우저 WebSocket API는 헤더
//    custom 미지원 — 일단 동일 origin + 쿠키/세션 가정. 별 인증 경로(query token)는 P2.
//  - 메시지 도착 시 latest를 갱신, kind='completed'면 status='completed'로 전이 후 close.
//  - sessionId가 빈 값이면 connection 안 함.
export function useScanProgress(sessionId?: string): UseScanProgressResult {
  const [status, setStatus] = useState<ScanProgressStatus>('idle')
  const [latest, setLatest] = useState<ScanProgressMessage | null>(null)
  const [error, setError] = useState<string | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    if (!sessionId) {
      setStatus('idle')
      return
    }
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${window.location.host}${API_BASE_PATH}/scans/${encodeURIComponent(sessionId)}/progress`

    setStatus('connecting')
    setLatest(null)
    setError(null)

    let closed = false
    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.addEventListener('open', () => {
      if (!closed) setStatus('streaming')
    })
    ws.addEventListener('message', (ev) => {
      try {
        const msg = JSON.parse(String(ev.data)) as ScanProgressMessage
        setLatest(msg)
        if (msg.kind === 'completed') {
          setStatus('completed')
        }
      } catch {
        /* malformed JSON 무시 */
      }
    })
    ws.addEventListener('error', () => {
      if (!closed) {
        setError('WebSocket 연결 실패 (인증/네트워크 확인)')
        setStatus('error')
      }
    })
    ws.addEventListener('close', () => {
      if (!closed) {
        // completed로 인한 정상 close가 아니면 에러로 분류 안 함 (이미 completed면 status 보존).
        setStatus((prev) => (prev === 'completed' ? prev : 'idle'))
      }
    })

    return () => {
      closed = true
      try {
        ws.close()
      } catch {
        /* ignore */
      }
      wsRef.current = null
    }
  }, [sessionId])

  return { status, latest, error }
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
