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
// 2-RBAC) Role helpers — RBAC Stage 2-B (Phase 5) Web UI button conditional render.
//
// 서버 측 admin/auditor gate(RBAC Stage 1+2-A)를 web UI에서 미리 표시·차단해
// 사용자가 403을 받기 전에 의도된 권한 부족을 알 수 있게 한다. 본 helper는
// useMe 응답의 user.roles만 검사 — 새 fetch 없음(이미 캐시된 me query 활용).
//
// 결정론: roles가 아직 로드되지 않았거나(useMe.isPending) 응답에 roles가 누락이면
// 안전 측면에서 false 반환 (gate 동작 가정). 따라서 me query 도착 전 잠깐 disabled가
// 표시될 수 있지만, useMe는 router-level prefetch + persisted accessToken 흐름에서
// 첫 paint 직전 hydrate 되므로 UX 영향 미미.
//
// 단위 테스트 가능 형태 — 순수 함수 hasAnyRole(roles, allowed) 분리.
// ────────────────────────────────────────────────────────────────────────

// hasAnyRole — roles 배열 중 하나라도 allowed에 포함되면 true. 케이스 sensitive.
//   nil/빈 입력은 false (안전 default — 권한 없음).
export function hasAnyRole(
  roles: ReadonlyArray<string> | undefined | null,
  allowed: ReadonlyArray<string>,
): boolean {
  if (!roles || roles.length === 0 || allowed.length === 0) return false
  for (const r of roles) {
    if (allowed.includes(r)) return true
  }
  return false
}

// useHasRole — 현재 사용자가 allowed role 중 하나라도 가지면 true.
//   useMe 캐시를 그대로 사용 — 추가 네트워크 호출 없음.
export const useHasRole = (...allowed: string[]): boolean => {
  const me = useMe()
  return hasAnyRole(me.data?.roles, allowed)
}

// useIsAdmin — 현재 사용자가 admin role을 가지면 true.
export const useIsAdmin = (): boolean => useHasRole('admin')

// useIsAuditor — 현재 사용자가 auditor role을 가지면 true.
export const useIsAuditor = (): boolean => useHasRole('auditor')

// useIsAdminOrAuditor — admin 또는 auditor (예: backup download — 시스템 다운로드).
export const useIsAdminOrAuditor = (): boolean => useHasRole('admin', 'auditor')

// ────────────────────────────────────────────────────────────────────────
// 2-Packs) Benchmark Packs (E12 Stage 3)
// ────────────────────────────────────────────────────────────────────────

export interface PackMeta {
  id: string
  tenantId: string
  packKey: string
  name: string
  vendor: string
  version: string
  description?: string
  schemaVersion: number
  signerKeyId?: string
  installedAt: string
  isBuiltin: boolean
}

// usePacks — built-in + tenant pack 합쳐 반환 (packKey 알파벳순).
//   scans 페이지 Pack Select 드롭다운 + system 페이지 PacksCard에서 사용.
export const usePacks = () => {
  return useQuery({
    queryKey: ['packs'],
    queryFn: async (): Promise<PackMeta[]> => {
      const { data, error, response } = await apiClient.GET('/api/v1/packs')
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const payload = data as { packs: PackMeta[] }
      return payload.packs
    },
  })
}

export interface PackCheck {
  id: string
  checkId: string
  title: string
  severity: 'low' | 'medium' | 'high' | 'critical'
  description?: string
}

export interface PackDetail extends PackMeta {
  checks: PackCheck[]
}

// usePack(packKey) — 단일 pack의 메타 + checks. /packs/{packKey} 페이지에서 사용.
export const usePack = (packKey: string) => {
  return useQuery({
    queryKey: ['pack', packKey],
    queryFn: async (): Promise<PackDetail> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/packs/{packKey}',
        { params: { path: { packKey } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as PackDetail
    },
    enabled: !!packKey,
  })
}

export interface CheckDetail extends PackCheck {
  packKey: string
  auditCommand: string
  evaluationRule: Record<string, unknown>
  rationale?: string
  fixGuidance?: string
}

export interface SelftestCase {
  name: string
  input: { stdout?: string; stderr?: string; exitCode?: number; [key: string]: unknown }
  expectedOutcome: 'PASS' | 'FAIL' | 'INDETERMINATE' | 'ERROR' | 'SKIPPED'
}

export interface CheckSelftest {
  checkId: string
  packKey: string
  cases: SelftestCase[]
}

// useCheckSelftest(packKey, checkId) — builtin pack 한정 selftest fixture.
//   tenant pack 또는 degraded check면 404 → ApiError. 호출자가 빈 cases 처리.
export const useCheckSelftest = (packKey: string, checkId: string) => {
  return useQuery({
    queryKey: ['check-selftest', packKey, checkId],
    queryFn: async (): Promise<CheckSelftest> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/packs/{packKey}/checks/{checkId}/selftest',
        { params: { path: { packKey, checkId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as CheckSelftest
    },
    enabled: !!packKey && !!checkId,
    retry: false, // 404 흔함 (degraded·tenant pack), 자동 retry 비효율
  })
}

// useCheck(packKey, checkId) — 단일 check의 audit cmd + eval rule + rationale + fix.
export const useCheck = (packKey: string, checkId: string) => {
  return useQuery({
    queryKey: ['check', packKey, checkId],
    queryFn: async (): Promise<CheckDetail> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/packs/{packKey}/checks/{checkId}',
        { params: { path: { packKey, checkId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as CheckDetail
    },
    enabled: !!packKey && !!checkId,
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

// useRotateCredential — POST /api/v1/robots/{robotId}/credential:rotate mutation (admin).
//
// 평문 자격증명을 받음 — 메모리에서만 처리, 도메인 layer가 KEK→DEK로 wrap.
export interface RotateCredentialVars {
  robotId: string
  authType: 'password' | 'privateKey'
  username: string
  password?: string
  privateKeyPem?: string
  privateKeyPassphrase?: string
}

export interface RotateCredentialResult {
  newCredentialId: string
  oldCredentialId: string
}

export const useRotateCredential = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: RotateCredentialVars): Promise<RotateCredentialResult> => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/robots/{robotId}/credential:rotate',
        {
          params: { path: { robotId: vars.robotId } },
          body: {
            authType: vars.authType,
            username: vars.username,
            password: vars.password,
            privateKeyPem: vars.privateKeyPem,
            privateKeyPassphrase: vars.privateKeyPassphrase,
          },
        },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as RotateCredentialResult
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['robots'] })
    },
  })
}

// useDeleteRobot — DELETE /api/v1/robots/{robotId} mutation hook (admin).
//
// 성공 시 ['robots'] cache invalidate. R3-5: 두 번째 호출 404(멱등 X) — UI는
// 성공 시 redirect, 404 받으면 이미 사라진 상태.
export const useDeleteRobot = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (robotId: string): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        '/api/v1/robots/{robotId}',
        { params: { path: { robotId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['robots'] })
      qc.invalidateQueries({ queryKey: ['fleets'] }) // robotCount 갱신
    },
  })
}

// RobotResult는 robot 단위 scan result 한 항목입니다.
export interface RobotResult {
  id: string
  sessionId: string
  checkId: string
  packCheckId: string
  // packKey는 derived (서버 JOIN session→pack). 빈 string 가능 (pack 미발견).
  packKey?: string
  // sessionStartedAt는 derived (서버 JOIN scan_sessions.started_at). 빈 string 가능 (pending 상태).
  sessionStartedAt?: string
  // sessionCompletedAt는 derived (서버 JOIN scan_sessions.completed_at). 빈 string 가능 (running/pending 상태).
  sessionCompletedAt?: string
  // sessionFailureReason는 derived (서버 JOIN scan_sessions.failure_reason). failed 상태에만 의미 있음.
  sessionFailureReason?: string
  // sessionStatus는 derived (서버 JOIN scan_sessions.status). Badge 색상 분기 입력.
  sessionStatus?: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  outcome: 'pass' | 'fail' | 'indeterminate' | 'error' | 'skipped'
  evalReason?: string
  durationMs: number
  executedAt: string
  createdAt: string
}

// useRobotResults는 GET /api/v1/robots/{robotId}/results hook입니다.
// limit 옵션 (default 20, max 200).
export function useRobotResults(robotId?: string, limit?: number) {
  return useQuery({
    queryKey: ['robot-results', robotId, limit ?? null],
    enabled: !!robotId,
    queryFn: async (): Promise<RobotResult[]> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/robots/{robotId}/results',
        {
          params: {
            path: { robotId: robotId! },
            query: limit ? { limit } : {},
          },
        },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const body = data as { results?: RobotResult[] } | undefined
      return body?.results ?? []
    },
  })
}

// useRobot은 GET /api/v1/robots/{robotId} 단일 조회 hook입니다.
// 향후 robot 상세 페이지(예: /robots/$robotId) 진입 시 활용.
export function useRobot(robotId?: string) {
  return useQuery({
    queryKey: ['robot', robotId],
    enabled: !!robotId,
    queryFn: async (): Promise<Robot> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/robots/{robotId}',
        { params: { path: { robotId: robotId! } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as Robot
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

// useUsageStats — /system 페이지 UsageStatsCard용. 인증 사용자 read-only.
//   GET /api/v1/usage/stats — Prometheus counter snapshot (process scope).
//   30s polling — 카운터 변화 reflect (E38 onboarding/billing 즉시 가시성).
export interface UsageStats {
  tenant: string
  scansStarted: number
  scansCompleted: Record<string, number>
  scanFailedChecks: number
  scansCompletedSum: number
}

export const useUsageStats = () => {
  const accessToken = useAuthStore((s) => s.accessToken)
  return useQuery({
    queryKey: ['usage', 'stats'],
    queryFn: async (): Promise<UsageStats> => {
      const { data, error, response } = await apiClient.GET('/api/v1/usage/stats')
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as UsageStats
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
  failureReason?: string
  createdAt?: string
  updatedAt?: string
  startedAt?: string | null
  completedAt?: string | null
}

// ────────────────────────────────────────────────────────────────────────
// 5-pre/5) Fleets (read-only)
// ────────────────────────────────────────────────────────────────────────

export interface FleetPolicy {
  defaultBaselineId?: string
  defaultLevel?: '' | 'L1' | 'L2'
  defaultCriticality?: '' | 'low' | 'medium' | 'high' | 'critical'
  scanSchedule?: string
}

export interface Fleet {
  id: string
  tenantId: string
  name: string
  description?: string
  robotCount: number
  policy: FleetPolicy
  createdAt?: string
  updatedAt?: string
}

// useFleets는 GET /api/v1/fleets 목록 조회 hook입니다.
// tenant scope 활성 fleets를 name ASC로 반환.
export function useFleets() {
  return useQuery({
    queryKey: ['fleets'],
    queryFn: async (): Promise<Fleet[]> => {
      const { data, error, response } = await apiClient.GET('/api/v1/fleets')
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const body = data as { fleets?: Fleet[] } | undefined
      return body?.fleets ?? []
    },
  })
}

// useFleet는 GET /api/v1/fleets/{fleetId} 단일 조회 hook입니다.
// /fleets/$fleetId deep-link 진입 시 useFleets 응답 대기 회피용.
export function useFleet(fleetId?: string) {
  return useQuery({
    queryKey: ['fleet', fleetId],
    enabled: !!fleetId,
    queryFn: async (): Promise<Fleet> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/fleets/{fleetId}',
        { params: { path: { fleetId: fleetId! } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as Fleet
    },
  })
}

// === Fleet mutations (admin) ===

export interface CreateFleetVars {
  name: string
  description?: string
  policy?: FleetPolicy
}

export const useCreateFleet = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: CreateFleetVars): Promise<Fleet> => {
      const { data, error, response } = await apiClient.POST('/api/v1/fleets', {
        body: {
          name: vars.name,
          description: vars.description ?? '',
          policy: vars.policy,
        },
      })
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as Fleet
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['fleets'] })
    },
  })
}

export interface UpdateFleetVars {
  fleetId: string
  name?: string
  description?: string
  policy?: FleetPolicy
}

export const useUpdateFleet = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: UpdateFleetVars): Promise<Fleet> => {
      const { data, error, response } = await apiClient.PATCH(
        '/api/v1/fleets/{fleetId}',
        {
          params: { path: { fleetId: vars.fleetId } },
          body: {
            name: vars.name,
            description: vars.description,
            policy: vars.policy,
          },
        },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as Fleet
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['fleets'] })
    },
  })
}

export const useDeleteFleet = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (fleetId: string): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        '/api/v1/fleets/{fleetId}',
        { params: { path: { fleetId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['fleets'] })
    },
  })
}

// useScansFilter는 useScans hook의 옵션입니다.
export interface UseScansFilter {
  fleetId?: string
  status?: string
  limit?: number
  // pollMs > 0이면 active 세션(pending/running) 1건 이상 있는 동안 자동 재조회.
  // all-terminal 도달 시 자동 정지 (UX 단순화).
  pollMs?: number
}

// useScans는 GET /api/v1/scans 목록 조회 hook입니다.
export function useScans(opts?: UseScansFilter) {
  const fleetId = opts?.fleetId
  const status = opts?.status
  const limit = opts?.limit
  const pollMs = opts?.pollMs
  return useQuery({
    queryKey: ['scans', { fleetId, status, limit }],
    queryFn: async (): Promise<ScanSession[]> => {
      const { data, error, response } = await apiClient.GET('/api/v1/scans', {
        params: {
          query: {
            fleetId,
            status,
            limit,
          },
        },
      })
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const body = data as { sessions?: ScanSession[] } | undefined
      return body?.sessions ?? []
    },
    refetchInterval: pollMs
      ? (query) => {
          const list = query.state.data as ScanSession[] | undefined
          if (!list || list.length === 0) return pollMs
          const hasActive = list.some((s) => !isTerminalScanStatus(s.status))
          return hasActive ? pollMs : false
        }
      : false,
  })
}

// useScan은 단일 scan session 조회 hook입니다.
//
// 용도:
//  - 페이지 reload 후 URL ?session=<id>로 진입 시 세션 복원
//  - WS 인증/네트워크 실패 시 polling fallback
// 옵션 pollMs를 지정하면 지정 간격으로 자동 재조회 (terminal 상태 도달 시 자동 정지).
export function useScan(sessionId?: string, opts?: { pollMs?: number }) {
  const pollMs = opts?.pollMs
  return useQuery({
    queryKey: ['scans', sessionId],
    enabled: !!sessionId,
    queryFn: async (): Promise<ScanSession> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/scans/{sessionId}',
        { params: { path: { sessionId: sessionId! } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as ScanSession
    },
    refetchInterval: pollMs
      ? (query) => {
          const s = query.state.data as ScanSession | undefined
          if (!s) return pollMs
          if (isTerminalScanStatus(s.status)) return false
          return pollMs
        }
      : false,
  })
}

// isTerminalScanStatus는 polling 정지 판단용입니다.
export function isTerminalScanStatus(status: string): boolean {
  return status === 'completed' || status === 'failed' || status === 'cancelled'
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

// useCancelScan은 POST /api/v1/scans/{sessionId}:cancel mutation hook입니다.
//
// 성공 시 ['scans', sessionId] cache invalidate — useScan polling이 즉시 새 status fetch.
// 409 (terminal already) → ApiError(409) — UI는 disable 처리 권장.
export interface CancelScanVars {
  sessionId: string
  reason?: string
}
export const useCancelScan = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: CancelScanVars): Promise<ScanSession> => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/scans/{sessionId}:cancel',
        {
          params: { path: { sessionId: vars.sessionId } },
          body: { reason: vars.reason ?? 'user requested' },
        },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as ScanSession
    },
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ['scans', data.sessionId] })
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
  | 'polling'
  | 'completed'
  | 'error'

export interface UseScanProgressResult {
  status: ScanProgressStatus
  latest: ScanProgressMessage | null
  error: string | null
}

// useScanProgress는 /api/v1/scans/{sessionId}/progress WebSocket 구독 + polling fallback.
//
// 디자인:
//  - WS URL에 access_token query param 부착 (브라우저 WebSocket API는 헤더 미지원).
//  - 첫 메시지 수신 전 WS error/close 발생 시 GET /api/v1/scans/{sessionId} polling으로 전환
//    (status='polling'). polling은 2s 간격, terminal 상태 도달 시 자동 정지.
//  - kind='completed' 메시지 수신 시 status='completed'로 전이 + WS close.
//  - sessionId가 빈 값이면 connection·polling 모두 안 함.
export function useScanProgress(sessionId?: string): UseScanProgressResult {
  const accessToken = useAuthStore((s) => s.accessToken)
  const [status, setStatus] = useState<ScanProgressStatus>('idle')
  const [latest, setLatest] = useState<ScanProgressMessage | null>(null)
  const [error, setError] = useState<string | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    if (!sessionId) {
      setStatus('idle')
      return
    }

    setLatest(null)
    setError(null)

    let cancelled = false
    let pollingActive = false

    const stopPolling = () => {
      if (pollTimerRef.current) {
        clearInterval(pollTimerRef.current)
        pollTimerRef.current = null
      }
    }

    const sessionToMessage = (s: ScanSession): ScanProgressMessage => ({
      kind: isTerminalScanStatus(s.status) ? 'completed' : 'progress',
      type: 'http.poll',
      sessionId: s.sessionId,
      total: s.total,
      completed: s.completed,
      failed: s.failed,
      status: s.status,
      occurredAt: s.updatedAt ?? new Date().toISOString(),
    })

    const pollOnce = async (): Promise<void> => {
      try {
        const { data, error: fetchErr, response } = await apiClient.GET(
          '/api/v1/scans/{sessionId}',
          { params: { path: { sessionId } } },
        )
        if (cancelled) return
        if (fetchErr) {
          setError(extractErrorMessage(fetchErr, response.statusText))
          setStatus('error')
          stopPolling()
          return
        }
        const s = data as ScanSession
        const msg = sessionToMessage(s)
        setLatest(msg)
        if (isTerminalScanStatus(s.status)) {
          setStatus('completed')
          stopPolling()
        }
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : 'polling failed')
          setStatus('error')
          stopPolling()
        }
      }
    }

    const startPolling = () => {
      if (pollingActive) return
      pollingActive = true
      setStatus('polling')
      void pollOnce()
      pollTimerRef.current = setInterval(() => {
        void pollOnce()
      }, 2000)
    }

    setStatus('connecting')

    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const tokenQS = accessToken ? `?access_token=${encodeURIComponent(accessToken)}` : ''
    const url = `${proto}://${window.location.host}${API_BASE_PATH}/scans/${encodeURIComponent(sessionId)}/progress${tokenQS}`

    let receivedAny = false
    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.addEventListener('open', () => {
      if (!cancelled) setStatus('streaming')
    })
    ws.addEventListener('message', (ev) => {
      try {
        const msg = JSON.parse(String(ev.data)) as ScanProgressMessage
        receivedAny = true
        setLatest(msg)
        if (msg.kind === 'completed') {
          setStatus('completed')
        }
      } catch {
        /* malformed JSON 무시 */
      }
    })
    ws.addEventListener('error', () => {
      if (cancelled) return
      // 첫 메시지 전 에러면 polling fallback. 메시지 받은 적 있으면 단순 에러 표시.
      if (!receivedAny) {
        startPolling()
      } else {
        setError('WebSocket 연결 끊김 (polling으로 전환)')
        startPolling()
      }
    })
    ws.addEventListener('close', () => {
      if (cancelled) return
      // completed → status 보존. 비-completed close + 첫 메시지도 못 받음 → polling fallback.
      setStatus((prev) => {
        if (prev === 'completed') return prev
        if (!receivedAny) {
          startPolling()
          return 'polling'
        }
        return prev
      })
    })

    return () => {
      cancelled = true
      stopPolling()
      try {
        ws.close()
      } catch {
        /* ignore */
      }
      wsRef.current = null
    }
  }, [sessionId, accessToken])

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

export const useWebhookEndpoints = () => {
  return useQuery({
    queryKey: ['webhooks'],
    queryFn: async (): Promise<WebhookEndpoint[]> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/webhooks',
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const body = data as unknown as { endpoints: WebhookEndpoint[] }
      return body.endpoints ?? []
    },
  })
}

export const useCreateWebhook = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: CreateWebhookEndpointVars): Promise<WebhookEndpoint> => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/webhooks',
        { body: vars as unknown as never },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as WebhookEndpoint
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
      const { data, error, response } = await apiClient.PUT(
        '/api/v1/webhooks/{endpointId}',
        {
          params: { path: { endpointId } },
          body: body as unknown as never,
        },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as WebhookEndpoint
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
      const { error, response } = await apiClient.DELETE(
        '/api/v1/webhooks/{endpointId}',
        { params: { path: { endpointId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
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
      const { data, error, response } = await apiClient.GET(
        '/api/v1/webhooks/{endpointId}/deliveries',
        { params: { path: { endpointId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const body = data as unknown as { deliveries: WebhookDelivery[] }
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
      const { data, error, response } = await apiClient.POST(
        '/api/v1/webhooks/{endpointId}/test',
        { params: { path: { endpointId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as WebhookTestResult
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

export const useSSOProviders = () => {
  return useQuery({
    queryKey: ['sso', 'providers'],
    queryFn: async (): Promise<SSOProvider[]> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/sso/providers',
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const body = data as unknown as { providers: SSOProvider[] }
      return body.providers ?? []
    },
  })
}

export const useCreateSSOProvider = () => {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: CreateSSOProviderVars): Promise<SSOProvider> => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/sso/providers',
        { body: vars as unknown as never },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as SSOProvider
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
      const { data, error, response } = await apiClient.PUT(
        '/api/v1/sso/providers/{providerId}',
        {
          params: { path: { providerId } },
          body: body as unknown as never,
        },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as SSOProvider
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
      const { error, response } = await apiClient.DELETE(
        '/api/v1/sso/providers/{providerId}',
        { params: { path: { providerId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
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
      const { data, error, response } = await apiClient.GET(
        '/api/v1/sso/providers/{providerId}',
        { params: { path: { providerId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as SSOProvider
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

export const useInvitations = () => {
  return useQuery({
    queryKey: ['invitations'],
    queryFn: async (): Promise<InvitationView[]> => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/invitations',
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      const body = data as unknown as { invitations: InvitationView[] }
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
      const { data, error, response } = await apiClient.POST(
        '/api/v1/invitations',
        { body: vars as unknown as never },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as CreateInvitationResponse
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
      const { error, response } = await apiClient.DELETE(
        '/api/v1/invitations/{invitationId}',
        { params: { path: { invitationId } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['invitations'] })
    },
  })
}

// 비인증 endpoint — token이 capability. apiClient는 인증 토큰이 있을 때만 부착하므로
//   미인증 사용자는 자연 통과. 이미 로그인한 사용자가 호출해도 backend(handlers.go)는
//   /api/v1/invitations/by-token/* 를 protected group 밖에 mount해 헤더를 무시한다.
export const useInvitationByToken = (token?: string) => {
  return useQuery({
    queryKey: ['invitations', 'by-token', token ?? null],
    queryFn: async (): Promise<InvitationPreview | null> => {
      if (!token) return null
      const { data, error, response } = await apiClient.GET(
        '/api/v1/invitations/by-token/{token}',
        { params: { path: { token } } },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as InvitationPreview
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
      const { data, error, response } = await apiClient.POST(
        '/api/v1/invitations/by-token/{token}/accept',
        {
          params: { path: { token } },
          body: body as unknown as never,
        },
      )
      if (error) {
        throw new ApiError(
          response.status,
          extractErrorMessage(error, response.statusText),
        )
      }
      return data as unknown as AcceptInvitationResponse
    },
  })
}
