import { zodResolver } from '@hookform/resolvers/zod'
import { createFileRoute, Link } from '@tanstack/react-router'
import { Plus, Server } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'

import { ApiError } from '@/api/errors'
import { useCreateRobot, useHasPermission, useRobots } from '@/api/hooks'
import { TruncatedId } from '@/components/common/TruncatedId'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { TableRowSkeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useT } from '@/i18n/t'
import { toast } from '@/lib/toast'
import { mutationGuardTitle, useIsOffline } from '@/lib/use-is-offline'

import type { CreateRobotVars, Robot } from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

// `/robots` — D-UI-2 리팩토링 (List + Create Dialog 패턴).
//
// 기존: list + form 토글(같은 페이지에 펼침). 변경:
//   - 기본 view = PageHeader (+ "+ 등록" 버튼) + RobotsTable
//   - "+ 등록" 클릭 → Dialog로 CreateRobotForm 분리
//   - 테이블 ID 컬럼은 TruncatedId로 축약(prefix ro_/fl_ + ellipsis + 마지막 4자)
//   - row click은 기존 detail 페이지로 drill-down 유지 (이력·credential 등 dialog로
//     부적합한 양이라 Link 유지)
//
// 기존 hook · API mutation · 라우팅 변경 0. zod schema·useCreateRobot 호출 그대로.

function RobotsPage(): React.ReactElement {
  const [fleetId, setFleetId] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const trimmed = fleetId.trim()
  const robots = useRobots(trimmed.length > 0 ? trimmed : undefined)
  const t = useT()
  // RBAC Stage 5 — server `RequirePermission(robot, write)` 매트릭스와 일관 (§3.3).
  //   admin tenant binding은 fleetId 무관 통과 — 회귀 0. fleet-admin/operator는
  //   fleetId 일치 시만 통과 (filter 입력 fleetId 또는 빈 문자열).
  const canCreate = useHasPermission('robot', 'write', trimmed.length > 0 ? trimmed : undefined)
  const isOffline = useIsOffline()

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.robots.title')}
        description={t('pages.robots.description')}
        actions={
          <Button
            size="sm"
            onClick={() => setCreateOpen(true)}
            disabled={!canCreate || isOffline}
            title={mutationGuardTitle({
              isOffline,
              offlineLabel: t('pwa.offline.mutationBlocked'),
              fallback: !canCreate ? t('common.role.required.admin') : undefined,
            })}
          >
            <Plus className="size-4" aria-hidden />
            {t('robots.create.button')}
          </Button>
        }
      />

      <div className="flex max-w-sm flex-col gap-2">
        <Label htmlFor="fleet-filter">{t('robots.filter.fleet')}</Label>
        <Input
          id="fleet-filter"
          placeholder={t('robots.filter.fleet.placeholder')}
          value={fleetId}
          onChange={(e) => setFleetId(e.target.value)}
        />
      </div>

      <div className="-mx-4 overflow-x-auto sm:mx-0 sm:rounded-md sm:border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('robots.table.name')}</TableHead>
              <TableHead>{t('robots.table.id')}</TableHead>
              <TableHead>{t('robots.table.fleet')}</TableHead>
              <TableHead>{t('robots.table.host')}</TableHead>
              <TableHead>{t('robots.table.auth')}</TableHead>
              <TableHead>{t('robots.table.criticality')}</TableHead>
              <TableHead>{t('robots.table.tags')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {robots.isPending && (
              <TableRow>
                <TableCell colSpan={7} className="p-3">
                  <TableRowSkeleton rows={5} columns={7} />
                </TableCell>
              </TableRow>
            )}
            {robots.isError && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-destructive">
                  {robots.error instanceof ApiError
                    ? robots.error.message
                    : t('robots.error.fallback')}
                </TableCell>
              </TableRow>
            )}
            {robots.isSuccess && robots.data.length === 0 && (
              <TableRow>
                <TableCell colSpan={7} className="p-0">
                  <EmptyState
                    icon={Server}
                    title={t('robots.empty.title')}
                    description={t('robots.empty.description')}
                    action={
                      canCreate ? (
                        <Button size="sm" onClick={() => setCreateOpen(true)}>
                          {t('robots.create.button')}
                        </Button>
                      ) : undefined
                    }
                    className="rounded-none border-0 bg-transparent"
                  />
                </TableCell>
              </TableRow>
            )}
            {robots.isSuccess &&
              robots.data.map((robot) => (
                <RobotRow key={robot.id} robot={robot} />
              ))}
          </TableBody>
        </Table>
      </div>

      <CreateRobotDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
      />
    </div>
  )
}

function RobotRow({ robot }: { robot: Robot }): React.ReactElement {
  const tags = Array.isArray(robot.tags) ? (robot.tags as unknown[]) : []
  return (
    <TableRow>
      <TableCell className="font-medium">
        <Link
          to="/robots/$robotId"
          params={{ robotId: robot.id }}
          className="hover:underline"
        >
          {robot.name}
        </Link>
      </TableCell>
      <TableCell>
        <TruncatedId id={robot.id} prefixLen={3} />
      </TableCell>
      <TableCell>
        <TruncatedId id={robot.fleetId} prefixLen={3} />
      </TableCell>
      <TableCell className="font-mono text-xs">
        {robot.host}:{robot.port}
      </TableCell>
      <TableCell>{robot.authType}</TableCell>
      <TableCell>
        <Badge variant="secondary">{robot.criticality}</Badge>
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap gap-1">
          {tags.length === 0 ? (
            <span className="text-xs text-muted-foreground">-</span>
          ) : (
            tags.map((tag, i) => (
              <Badge key={i} variant="outline">
                {String(tag)}
              </Badge>
            ))
          )}
        </div>
      </TableCell>
    </TableRow>
  )
}

// CreateRobotDialog — "+ 등록" 클릭 시 열리는 Dialog. CreateRobotForm을 본 dialog
// body에 mount하여 form pilot(RHF + zod) 그대로 재사용. submit 성공 → dialog close.
function CreateRobotDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}): React.ReactElement {
  const t = useT()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('robots.create.title')}</DialogTitle>
          <DialogDescription>{t('robots.create.description')}</DialogDescription>
        </DialogHeader>
        <CreateRobotForm
          onCreated={() => onOpenChange(false)}
          onCancel={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  )
}

// D-UI-1 Stage 4 form pilot — react-hook-form + zod schema.
//
// 기존 hand-coded useState 매트릭스를 단일 useForm + FormField 트리로 대체:
//   - 필드별 검증은 schema에 일원화 (host required, port range, auth-conditional).
//   - 입력 오류는 FormMessage가 a11y aria-describedby로 자동 결선.
//   - mutation 결과는 toast로 비차단 통지 (성공/실패 모두).
//
// schema는 module-scope에 두어 t()와 분리 — message는 schemaErrorToI18n로 변환.

const ROBOT_AUTH_TYPES = ['password', 'privateKey'] as const
const ROBOT_CRITICALITIES = ['low', 'medium', 'high', 'critical'] as const

const robotFormSchema = z
  .object({
    fleetId: z.string().trim().min(1, 'robots.form.validation.fleetId'),
    name: z.string().trim().min(1, 'robots.form.validation.name'),
    host: z.string().trim().min(1, 'robots.form.validation.host'),
    port: z.coerce
      .number()
      .int()
      .min(1, 'robots.form.validation.port')
      .max(65535, 'robots.form.validation.port'),
    authType: z.enum(ROBOT_AUTH_TYPES),
    username: z.string().trim().min(1, 'robots.form.validation.username'),
    password: z.string().optional(),
    privateKeyPem: z.string().optional(),
    criticality: z.enum(ROBOT_CRITICALITIES),
    tagsRaw: z.string().optional(),
  })
  .superRefine((data, ctx) => {
    if (data.authType === 'password' && !data.password) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['password'],
        message: 'robots.form.validation.password',
      })
    }
    if (data.authType === 'privateKey' && !data.privateKeyPem?.trim()) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['privateKeyPem'],
        message: 'robots.form.validation.privateKeyPem',
      })
    }
  })

type RobotFormValues = z.infer<typeof robotFormSchema>

function CreateRobotForm({
  onCreated,
  onCancel,
}: {
  onCreated: () => void
  onCancel: () => void
}): React.ReactElement {
  const t = useT()
  const create = useCreateRobot()
  const isOffline = useIsOffline()
  const form = useForm<RobotFormValues>({
    resolver: zodResolver(robotFormSchema),
    defaultValues: {
      fleetId: '',
      name: '',
      host: '',
      port: 22,
      authType: 'password',
      username: '',
      password: '',
      privateKeyPem: '',
      criticality: 'medium',
      tagsRaw: '',
    },
  })
  const fleetIdValue = form.watch('fleetId').trim()
  // RBAC Stage 5 — fleet 컨텍스트는 form 입력 fleetId. 빈 문자열 시 fleet scope role
  // 은 통과 0, admin tenant scope만 통과 (회귀 0).
  const canCreate = useHasPermission(
    'robot',
    'write',
    fleetIdValue.length > 0 ? fleetIdValue : undefined,
  )
  const authType = form.watch('authType')

  const onSubmit = (values: RobotFormValues): void => {
    const tags = (values.tagsRaw ?? '')
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
    const payload: CreateRobotVars = {
      fleetId: values.fleetId.trim(),
      name: values.name.trim(),
      host: values.host.trim(),
      port: values.port,
      authType: values.authType,
      username: values.username.trim(),
      password: values.authType === 'password' ? values.password : undefined,
      privateKeyPem:
        values.authType === 'privateKey' ? values.privateKeyPem : undefined,
      criticality: values.criticality,
      tags: tags.length > 0 ? tags : undefined,
    }
    create.mutate(payload, {
      onSuccess: (data) => {
        toast.success(t('robots.form.toast.success'), {
          description: t('robots.form.success', { id: data.robot.id }),
        })
        onCreated()
        form.reset()
      },
      onError: (err) => {
        toast.error(
          err instanceof ApiError ? err.message : t('robots.form.error.fallback'),
        )
      },
    })
  }

  // i18n: zod schema message가 dict key를 그대로 들고 옴 — t()로 풀어서 화면 표시.
  const renderError = (message?: string): string | undefined => {
    if (!message) return undefined
    return t(message as DictKey)
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="grid grid-cols-1 gap-3 md:grid-cols-2"
        aria-label={t('robots.form.title')}
        noValidate
      >
        <FormField
          control={form.control}
          name="fleetId"
          render={({ field, fieldState }) => (
            <FormItem>
              <FormLabel>{t('robots.form.fleet')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('robots.form.fleet.placeholder')}
                  autoComplete="off"
                  {...field}
                />
              </FormControl>
              <FormMessage>{renderError(fieldState.error?.message)}</FormMessage>
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="name"
          render={({ field, fieldState }) => (
            <FormItem>
              <FormLabel>{t('robots.form.name')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('robots.form.name.placeholder')}
                  autoComplete="off"
                  {...field}
                />
              </FormControl>
              <FormMessage>{renderError(fieldState.error?.message)}</FormMessage>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="host"
          render={({ field, fieldState }) => (
            <FormItem>
              <FormLabel>{t('robots.form.host')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('robots.form.host.placeholder')}
                  autoComplete="off"
                  {...field}
                />
              </FormControl>
              <FormMessage>{renderError(fieldState.error?.message)}</FormMessage>
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="port"
          render={({ field, fieldState }) => (
            <FormItem>
              <FormLabel>{t('robots.form.port')}</FormLabel>
              <FormControl>
                <Input
                  type="number"
                  min={1}
                  max={65535}
                  {...field}
                  onChange={(e) => field.onChange(Number(e.target.value))}
                />
              </FormControl>
              <FormMessage>{renderError(fieldState.error?.message)}</FormMessage>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="authType"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('robots.form.authType')}</FormLabel>
              <Select value={field.value} onValueChange={field.onChange}>
                <FormControl>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value="password">
                    {t('robots.form.authType.password')}
                  </SelectItem>
                  <SelectItem value="privateKey">
                    {t('robots.form.authType.privateKey')}
                  </SelectItem>
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="username"
          render={({ field, fieldState }) => (
            <FormItem>
              <FormLabel>{t('robots.form.username')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('robots.form.username.placeholder')}
                  autoComplete="username"
                  {...field}
                />
              </FormControl>
              <FormMessage>{renderError(fieldState.error?.message)}</FormMessage>
            </FormItem>
          )}
        />

        {authType === 'password' ? (
          <FormField
            control={form.control}
            name="password"
            render={({ field, fieldState }) => (
              <FormItem className="md:col-span-2">
                <FormLabel>{t('robots.form.password')}</FormLabel>
                <FormControl>
                  <Input
                    type="password"
                    autoComplete="new-password"
                    {...field}
                    value={field.value ?? ''}
                  />
                </FormControl>
                <FormMessage>{renderError(fieldState.error?.message)}</FormMessage>
              </FormItem>
            )}
          />
        ) : (
          <FormField
            control={form.control}
            name="privateKeyPem"
            render={({ field, fieldState }) => (
              <FormItem className="md:col-span-2">
                <FormLabel>{t('robots.form.privateKeyPem')}</FormLabel>
                <FormControl>
                  <textarea
                    rows={5}
                    placeholder={t('robots.form.privateKeyPem.placeholder')}
                    className="rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs shadow-xs"
                    {...field}
                    value={field.value ?? ''}
                  />
                </FormControl>
                <FormMessage>{renderError(fieldState.error?.message)}</FormMessage>
              </FormItem>
            )}
          />
        )}

        <FormField
          control={form.control}
          name="criticality"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('robots.form.criticality')}</FormLabel>
              <Select value={field.value} onValueChange={field.onChange}>
                <FormControl>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value="low">{t('robots.form.criticality.low')}</SelectItem>
                  <SelectItem value="medium">
                    {t('robots.form.criticality.medium')}
                  </SelectItem>
                  <SelectItem value="high">
                    {t('robots.form.criticality.high')}
                  </SelectItem>
                  <SelectItem value="critical">
                    {t('robots.form.criticality.critical')}
                  </SelectItem>
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="tagsRaw"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('robots.form.tags')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('robots.form.tags.placeholder')}
                  {...field}
                  value={field.value ?? ''}
                />
              </FormControl>
              <FormDescription>{t('robots.form.tags.placeholder')}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        {create.isError && (
          <p className="md:col-span-2 text-sm text-destructive" role="alert">
            {create.error instanceof ApiError
              ? create.error.message
              : t('robots.form.error.fallback')}
          </p>
        )}

        <DialogFooter className="md:col-span-2">
          <Button
            type="button"
            variant="outline"
            onClick={onCancel}
            disabled={create.isPending}
          >
            {t('common.dialog.cancel')}
          </Button>
          <Button
            type="submit"
            disabled={create.isPending || !canCreate || isOffline}
            title={mutationGuardTitle({
              isOffline,
              offlineLabel: t('pwa.offline.mutationBlocked'),
              fallback: !canCreate ? t('common.role.required.admin') : undefined,
            })}
          >
            {create.isPending ? t('robots.form.submitting') : t('robots.form.submit')}
          </Button>
        </DialogFooter>
      </form>
    </Form>
  )
}

export const Route = createFileRoute('/_authenticated/robots')({
  component: RobotsPage,
})
