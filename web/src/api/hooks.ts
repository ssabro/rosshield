import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'

import { useAuthStore } from '@/stores/auth'

import { API_BASE_PATH, apiClient } from './client'
import { ApiError, extractErrorMessage } from './errors'

import type { User } from '@/stores/auth'

// rosshield Web Console TanStack Query 훅.
//
// 설계 메모:
// - O3 spec drift 정리(2026-05-07) 이후 대부분 endpoint는 정확한 schema를 가진다.
//   request body·response 타입은 가능한 곳에서 generated `components["schemas"]`로
//   직접 좁힐 수 있지만, 본 hooks는 호출자가 다루기 쉬운 평탄 interface를 노출한다.
//   (gen schema는 nullable·optional 표현이 강해 호출 측 매핑 부담이 큼.)
// - 응답 본문은 좁힌 inline 타입으로 cast한다 — openapi-fetch는 union을 wrap하므로
//   one-shot cast가 가장 깔끔.
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
      const payload = data as { robots: Robot[] }
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

// useLicenseInfo — E24 Settings License 카드. 모든 인증 사용자 read-only.
export interface LicenseInfo {
  edition: 'community' | 'enterprise'
  issuedTo?: string
  issuedAt?: string
  expiresAt?: string
  expired: boolean
  features?: string[]
  quotas: {
    robotsMax: number
    scansPerDay: number
    llmTokensPerDay: number
  }
}

export const useLicenseInfo = () => {
  const accessToken = useAuthStore((s) => s.accessToken)
  return useQuery({
    queryKey: ['license', 'info'],
    queryFn: async (): Promise<LicenseInfo> => {
      const { data, error, response } = await apiClient.GET('/api/v1/license')
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      // gen schema는 모든 필드가 optional(quotas 포함) — 호출자가 다루기 쉬운
      // LicenseInfo interface로 좁힘 (useAuditHead와 동일 one-shot cast 패턴).
      return data as unknown as LicenseInfo
    },
    enabled: !!accessToken,
  })
}

// useBackups — B7 Stage 2 /system 페이지 BackupsCard용. 인증 사용자.
//   GET /api/v1/backups → { ok: true, value: { backups: [...] } } envelope.
//   30s polling — Stage 1 자동 schedule(--backup-schedule cron) 결과 reflect.
//   B7 Stage 2-C에서 openapi spec 추가됨 → typed apiClient 경유.
export interface BackupMeta {
  filename: string
  size: number
  sha256: string
  generatedAt: string
  includesEvidence: boolean
}

export const useBackups = () => {
  const accessToken = useAuthStore((s) => s.accessToken)
  return useQuery({
    queryKey: ['backups'],
    queryFn: async (): Promise<BackupMeta[]> => {
      const { data, error, response } = await apiClient.GET('/api/v1/backups')
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      // 응답은 { ok: true, value: { backups: BackupMeta[] } } envelope.
      // openapi-fetch는 envelope를 자동 unwrap하지 않으므로 .value.backups 추출.
      const body = data as { ok: true; value: { backups: BackupMeta[] } }
      return body.value?.backups ?? []
    },
    enabled: !!accessToken,
    refetchInterval: 30_000,
  })
}

// useAuditHead — B1 Web UI Audit 페이지. tenant scope chain head.
export interface AuditHead {
  tenantId: string
  seq: number
  hashHex: string
  updatedAt?: string
}

export const useAuditHead = () => {
  return useQuery({
    queryKey: ['audit', 'head'],
    queryFn: async (): Promise<AuditHead> => {
      const { data, error, response } = await apiClient.GET('/api/v1/audit/head')
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as AuditHead
    },
  })
}

// useCreateRobot — POST /api/v1/robots. 성공 시 robots 캐시 무효화.
//   에러 매핑: 400(검증)·401(인증)·409(name·host:port 중복).
//   O3 spec drift 정리 후(2026-05-07) — body·response가 명시 schema로 갱신됨.
export const useCreateRobot = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: CreateRobotVars): Promise<CreateRobotResponse> => {
      const { data, error, response } = await apiClient.POST('/api/v1/robots', {
        body: vars,
      })
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as CreateRobotResponse
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
  trigger?: 'manual' | 'schedule' | 'event'
  total?: number
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
      // O3 spec drift 정리 후(2026-05-07) — body 타입이 명시적 객체로 갱신됨.
      const { data, error, response } = await apiClient.POST('/api/v1/scans', {
        body: {
          fleetId: vars.fleetId,
          packId: vars.packId,
          trigger: vars.trigger ?? 'manual',
          total: vars.total,
        },
      })
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as ScanSession
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
      const payload = data as { insights: Insight[] }
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
      return data as Insight
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
      const payload = data as { profiles: ComplianceProfile[] }
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
      const payload = data as { snapshots: ComplianceSnapshot[] }
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
      return data as ComplianceProfile
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
      return data as ComplianceSnapshot
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
// 5-pre/3) Advisor (E19-3) — listAdvisorConversations / getAdvisorConversation /
//   askAdvisor. typed apiClient 경유.
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

export const useAdvisorConversations = () => {
  return useQuery({
    queryKey: ['advisor', 'conversations'],
    queryFn: async (): Promise<AdvisorConversation[]> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/advisor/conversations',
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const body = data as unknown as { conversations: AdvisorConversation[] }
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
      const { data, error, response } = await apiClient.GET(
        '/api/v1/advisor/conversations/{conversationId}',
        { params: { path: { conversationId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as AdvisorConversationDetail
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
      const { data, error, response } = await apiClient.POST(
        '/api/v1/advisor/conversations:ask',
        { body: vars },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as AdvisorAskResponse
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
// 5) Reports — listReports operation. typed apiClient 경유.
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
      const { data, error, response } = await apiClient.GET(
        '/api/v1/reports',
        {
          params: { query: sessionId ? { sessionId } : {} },
        },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const body = data as unknown as { reports: Report[] }
      return body.reports ?? []
    },
  })
}

// ────────────────────────────────────────────────────────────────────────
// 6) Webhook (E23-C) — endpoint CRUD + delivery 조회.
//
// Spec drift 회피를 위해 hooks 측은 inline interface로 좁힘. 응답 schema는
// `WebhookEndpoint`/`WebhookDelivery` (openapi.yaml §components.schemas).
// secret 본문은 응답에서 마스킹(`secretLast4`) — 회전은 PUT 본문으로만 수행.
// ────────────────────────────────────────────────────────────────────────

export type WebhookEventType = 'scan.completed' | 'insight.created' | 'audit.checkpoint'
export type WebhookFormat = 'json' | 'cef' | 'ecs'

export interface WebhookEndpoint {
  id: string
  tenantId: string
  url: string
  secretLast4: string
  events: WebhookEventType[]
  format: WebhookFormat
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface WebhookDelivery {
  id: string
  endpointId: string
  tenantId: string
  eventType: WebhookEventType
  eventId: string
  payloadBase64?: string
  attemptCount: number
  lastAttemptedAt?: string
  nextAttemptAt: string
  succeeded: boolean
  lastResponseStatus: number
  lastError?: string
  createdAt: string
}

export interface CreateWebhookEndpointVars {
  url: string
  secret: string
  events?: WebhookEventType[]
  format?: WebhookFormat
  enabled?: boolean
}

export interface UpdateWebhookEndpointVars {
  endpointId: string
  url: string
  secret: string
  events?: WebhookEventType[]
  format?: WebhookFormat
  enabled?: boolean
}

// webhookFetch는 Bearer 인증 + JSON 헤더를 포함한 fetch wrapper입니다.
//
// 401 시 세션 클리어, 비-OK는 ApiError throw, 204는 undefined 반환.
// 별도 함수로 두는 이유: openapi-fetch typed client는 path param + body 결합 시
// 매번 cast가 필요해 hooks 측 호출이 verbose해짐. webhook은 6 endpoint 단순 CRUD.
async function webhookFetch<T>(
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
  if (res.status === 204) {
    return undefined as unknown as T
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
  return (await res.json()) as T
}

export const useWebhookEndpoints = () => {
  return useQuery({
    queryKey: ['webhooks'],
    queryFn: async (): Promise<WebhookEndpoint[]> => {
      const body = await webhookFetch<{ endpoints: WebhookEndpoint[] }>(
        `${API_BASE_PATH}/webhooks`,
      )
      return body.endpoints ?? []
    },
  })
}

export const useCreateWebhook = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: CreateWebhookEndpointVars): Promise<WebhookEndpoint> => {
      return webhookFetch<WebhookEndpoint>(`${API_BASE_PATH}/webhooks`, {
        method: 'POST',
        body: JSON.stringify(vars),
      })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['webhooks'] })
    },
  })
}

export const useUpdateWebhook = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: UpdateWebhookEndpointVars): Promise<WebhookEndpoint> => {
      const { endpointId, ...body } = vars
      return webhookFetch<WebhookEndpoint>(
        `${API_BASE_PATH}/webhooks/${encodeURIComponent(endpointId)}`,
        { method: 'PUT', body: JSON.stringify(body) },
      )
    },
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ['webhooks'] })
      qc.invalidateQueries({ queryKey: ['webhooks', data.id] })
    },
  })
}

export const useDeleteWebhook = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (endpointId: string): Promise<void> => {
      await webhookFetch<void>(
        `${API_BASE_PATH}/webhooks/${encodeURIComponent(endpointId)}`,
        { method: 'DELETE' },
      )
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['webhooks'] })
    },
  })
}

export const useWebhookDeliveries = (endpointId?: string) => {
  return useQuery({
    queryKey: ['webhooks', endpointId ?? null, 'deliveries'],
    queryFn: async (): Promise<WebhookDelivery[]> => {
      if (!endpointId) {
        return []
      }
      const body = await webhookFetch<{ deliveries: WebhookDelivery[] }>(
        `${API_BASE_PATH}/webhooks/${encodeURIComponent(endpointId)}/deliveries`,
      )
      return body.deliveries ?? []
    },
    enabled: !!endpointId,
  })
}

// formatWebhookEvent — UI 표기용 EventType 짧은 라벨 매핑 (단위 테스트 가능).
//   알 수 없는 값은 그대로 반환.
export function formatWebhookEvent(event: string): string {
  switch (event) {
    case 'scan.completed':
      return 'scan'
    case 'insight.created':
      return 'insight'
    case 'audit.checkpoint':
      return 'audit'
    default:
      return event
  }
}

// webhookDeliveryStatus — delivery row의 status 분류 (B3 단위 테스트 가능).
//   - succeeded=true → 'success'
//   - attemptCount>=5 && !succeeded → 'dead'
//   - attemptCount>0 → 'retrying'
//   - 그 외 → 'pending'
export type WebhookDeliveryStatus = 'success' | 'dead' | 'retrying' | 'pending'

export function webhookDeliveryStatus(
  d: Pick<WebhookDelivery, 'succeeded' | 'attemptCount'>,
): WebhookDeliveryStatus {
  if (d.succeeded) return 'success'
  if (d.attemptCount >= 5) return 'dead'
  if (d.attemptCount > 0) return 'retrying'
  return 'pending'
}

// KNOWN_WEBHOOK_EVENTS — UI에서 multi-select 옵션으로 사용 (B3 페이지에서 import).
export const KNOWN_WEBHOOK_EVENTS: ReadonlyArray<WebhookEventType> = [
  'scan.completed',
  'insight.created',
  'audit.checkpoint',
]

// O7 — webhook one-off ping (E29 backend POST /webhooks/{id}/test 활용).
export interface WebhookTestResult {
  success: boolean
  status: number
  error?: string
  latencyMs: number
}

export const useTestWebhookEndpoint = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (endpointId: string): Promise<WebhookTestResult> => {
      return webhookFetch<WebhookTestResult>(
        `${API_BASE_PATH}/webhooks/${encodeURIComponent(endpointId)}/test`,
        { method: 'POST' },
      )
    },
    onSettled: (_data, _err, endpointId) => {
      // ping 후 deliveries 캐시 무효화 — UI 새 row 표시 (현재 backend는 INSERT 안 함).
      void qc.invalidateQueries({ queryKey: ['webhooks', endpointId, 'deliveries'] })
    },
  })
}

// O7 — delivery 통계 (success/retrying/dead/pending) 집계 helper (단위 테스트 가능).
export interface WebhookDeliveryStats {
  total: number
  success: number
  retrying: number
  dead: number
  pending: number
}

export function summarizeDeliveries(
  deliveries: ReadonlyArray<Pick<WebhookDelivery, 'succeeded' | 'attemptCount'>>,
): WebhookDeliveryStats {
  const stats: WebhookDeliveryStats = {
    total: deliveries.length,
    success: 0,
    retrying: 0,
    dead: 0,
    pending: 0,
  }
  for (const d of deliveries) {
    stats[webhookDeliveryStatus(d)] += 1
  }
  return stats
}

// ────────────────────────────────────────────────────────────────────────
// 7) SSO Providers (E20-D / B4) — provider CRUD.
//
// Backend 응답 schema (handlers/sso.go providerView):
//   { id, type:'oidc'|'saml', name, enabled, config:object, createdAt, updatedAt }
// 에러 매핑: 401(no tenant), 400(validation), 404(not found), 409(name dup).
// openapi spec에는 미정의 — webhook과 동일한 raw fetch wrapper 사용.
// ────────────────────────────────────────────────────────────────────────

export type SSOProviderType = 'oidc' | 'saml'

export interface SSOProvider {
  id: string
  type: SSOProviderType
  name: string
  enabled: boolean
  config: Record<string, unknown>
  createdAt: string
  updatedAt: string
}

export interface CreateSSOProviderVars {
  type: SSOProviderType
  name: string
  enabled: boolean
  config: Record<string, unknown>
}

export interface UpdateSSOProviderVars {
  providerId: string
  name?: string
  enabled?: boolean
  config?: Record<string, unknown>
}

// ssoFetch — webhookFetch와 동일 패턴. SSO endpoint는 OpenAPI spec에 미정의이므로
//   typed openapi-fetch 대신 직접 fetch + Bearer 헤더.
async function ssoFetch<T>(
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
  const res = await fetch(path, {
    ...init,
    headers,
    credentials: 'include',
  })
  if (res.status === 401) {
    useAuthStore.getState().clearSession()
  }
  if (res.status === 204) {
    return undefined as unknown as T
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
  return (await res.json()) as T
}

export const useSSOProviders = () => {
  return useQuery({
    queryKey: ['sso', 'providers'],
    queryFn: async (): Promise<SSOProvider[]> => {
      const body = await ssoFetch<{ providers: SSOProvider[] }>(
        `${API_BASE_PATH}/sso/providers`,
      )
      return body.providers ?? []
    },
  })
}

export const useCreateSSOProvider = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: CreateSSOProviderVars): Promise<SSOProvider> => {
      return ssoFetch<SSOProvider>(`${API_BASE_PATH}/sso/providers`, {
        method: 'POST',
        body: JSON.stringify(vars),
      })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sso', 'providers'] })
    },
  })
}

export const useUpdateSSOProvider = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: UpdateSSOProviderVars): Promise<SSOProvider> => {
      const { providerId, ...body } = vars
      return ssoFetch<SSOProvider>(
        `${API_BASE_PATH}/sso/providers/${encodeURIComponent(providerId)}`,
        { method: 'PUT', body: JSON.stringify(body) },
      )
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sso', 'providers'] })
    },
  })
}

export const useDeleteSSOProvider = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (providerId: string): Promise<void> => {
      await ssoFetch<void>(
        `${API_BASE_PATH}/sso/providers/${encodeURIComponent(providerId)}`,
        { method: 'DELETE' },
      )
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sso', 'providers'] })
    },
  })
}

export const useSSOProvider = (providerId?: string) => {
  return useQuery({
    queryKey: ['sso', 'providers', providerId ?? null],
    queryFn: async (): Promise<SSOProvider | null> => {
      if (!providerId) return null
      return ssoFetch<SSOProvider>(
        `${API_BASE_PATH}/sso/providers/${encodeURIComponent(providerId)}`,
      )
    },
    enabled: !!providerId,
  })
}

// ────────────────────────────────────────────────────────────────────────
// 8) Invitations (E21 / B2) — 사용자 초대·수락.
//
// Backend 응답 schema (handlers/invitation.go invitationView):
//   { id, email, roleName, invitedBy, expiresAt, acceptedAt?, acceptedBy?, createdAt }
// 인증 필요(GET/POST/DELETE 목록/생성/취소) + 비인증(by-token 미리보기·accept).
// 에러 매핑: 400(validation·email mismatch·만료·이미 사용·short password),
//   401(인증 누락), 404(미존재), 409(활성 초대 중복·이메일 중복).
// openapi spec에는 미정의 — webhook/sso와 동일한 raw fetch wrapper 사용.
// ────────────────────────────────────────────────────────────────────────

export interface InvitationView {
  id: string
  email: string
  roleName: string
  invitedBy: string
  expiresAt: string
  acceptedAt?: string
  acceptedBy?: string
  createdAt: string
}

export interface CreateInvitationVars {
  email: string
  roleName: string
  expiresInHours?: number
}

export interface CreateInvitationResponse extends InvitationView {
  // 응답 본문은 invitationView를 평탄화한 후 token 1회 노출.
  token: string
}

export interface InvitationPreview {
  email: string
  roleName: string
  expiresAt: string
  accepted: boolean
}

export interface AcceptInvitationVars {
  token: string
  email: string
  password: string
  displayName: string
}

export interface AcceptInvitationResponse {
  userId: string
  email: string
  displayName: string
  roles: string[]
}

// invitationFetch — 인증이 필요한 invitation endpoint용 fetch wrapper.
//   ssoFetch와 동일 패턴 — 401 시 세션 클리어, 비-OK는 ApiError throw.
async function invitationFetch<T>(
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
  const res = await fetch(path, {
    ...init,
    headers,
    credentials: 'include',
  })
  if (res.status === 401) {
    useAuthStore.getState().clearSession()
  }
  if (res.status === 204) {
    return undefined as unknown as T
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
  return (await res.json()) as T
}

// publicFetch — 비인증 invitation endpoint(by-token 경로) 전용.
//   Authorization 헤더 미부착, credentials는 'omit' 또는 default — 토큰 capability만 사용.
async function publicFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...((init?.headers as Record<string, string> | undefined) ?? {}),
  }
  const res = await fetch(path, { ...init, headers })
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
  return (await res.json()) as T
}

export const useInvitations = () => {
  return useQuery({
    queryKey: ['invitations'],
    queryFn: async (): Promise<InvitationView[]> => {
      const body = await invitationFetch<{ invitations: InvitationView[] }>(
        `${API_BASE_PATH}/invitations`,
      )
      return body.invitations ?? []
    },
  })
}

export const useCreateInvitation = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (
      vars: CreateInvitationVars,
    ): Promise<CreateInvitationResponse> => {
      return invitationFetch<CreateInvitationResponse>(
        `${API_BASE_PATH}/invitations`,
        {
          method: 'POST',
          body: JSON.stringify(vars),
        },
      )
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['invitations'] })
    },
  })
}

export const useDeleteInvitation = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (invitationId: string): Promise<void> => {
      await invitationFetch<void>(
        `${API_BASE_PATH}/invitations/${encodeURIComponent(invitationId)}`,
        { method: 'DELETE' },
      )
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['invitations'] })
    },
  })
}

export const useInvitationByToken = (token?: string) => {
  return useQuery({
    queryKey: ['invitations', 'by-token', token ?? null],
    queryFn: async (): Promise<InvitationPreview | null> => {
      if (!token) return null
      return publicFetch<InvitationPreview>(
        `${API_BASE_PATH}/invitations/by-token/${encodeURIComponent(token)}`,
      )
    },
    enabled: !!token,
    retry: false,
  })
}

export const useAcceptInvitation = () => {
  return useMutation({
    mutationFn: async (
      vars: AcceptInvitationVars,
    ): Promise<AcceptInvitationResponse> => {
      const { token, ...body } = vars
      return publicFetch<AcceptInvitationResponse>(
        `${API_BASE_PATH}/invitations/by-token/${encodeURIComponent(token)}/accept`,
        {
          method: 'POST',
          body: JSON.stringify(body),
        },
      )
    },
  })
}
