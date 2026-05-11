import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { KeyRound } from 'lucide-react'

import { ApiError } from '@/api/errors'
import {
  useCreateSSOProvider,
  useDeleteSSOProvider,
  useIsAdmin,
  useSSOProviders,
  useUpdateSSOProvider,
} from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'

import type {
  CreateSSOProviderVars,
  SSOProvider,
  SSOProviderType,
  UpdateSSOProviderVars,
} from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

// `/sso` — SSO Provider CRUD UI (B4).
//
// Backend HTTP 표면(E20-D)이 머지된 상태로 hooks.ts는 raw fetch wrapper.
// type=oidc/saml에 따라 동적 config 폼을 렌더링하고, 에러 매핑(409·404 등)을
// 친화 메시지로 변환한다.
function SSOPage(): React.ReactElement {
  const t = useT()
  const providers = useSSOProviders()
  const isAdmin = useIsAdmin()
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState<SSOProvider | null>(null)

  const handleEdit = (p: SSOProvider): void => {
    setEditing(p)
    setShowForm(true)
  }
  const closeForm = (): void => {
    setShowForm(false)
    setEditing(null)
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.sso.title')}
        description={t('pages.sso.description')}
        actions={
          <Button
            variant={showForm && !editing ? 'outline' : 'default'}
            size="sm"
            onClick={() => {
              if (showForm) {
                closeForm()
              } else {
                setEditing(null)
                setShowForm(true)
              }
            }}
            disabled={!isAdmin}
            title={!isAdmin ? t('common.role.required.admin') : undefined}
          >
            {showForm
              ? t('sso.form.toggle.hide')
              : t('sso.form.toggle.show')}
          </Button>
        }
      />

      {showForm && isAdmin && (
        <ProviderForm
          editing={editing}
          onDone={closeForm}
        />
      )}

      <ProvidersTable
        providers={providers.data ?? []}
        isPending={providers.isPending}
        isError={providers.isError}
        error={providers.error}
        onEdit={handleEdit}
        canMutate={isAdmin}
      />
    </div>
  )
}

// ProvidersTable — provider 목록 표.
function ProvidersTable({
  providers,
  isPending,
  isError,
  error,
  onEdit,
  canMutate,
}: {
  providers: SSOProvider[]
  isPending: boolean
  isError: boolean
  error: unknown
  onEdit: (p: SSOProvider) => void
  canMutate: boolean
}): React.ReactElement {
  const t = useT()
  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('sso.table.id')}</TableHead>
            <TableHead>{t('sso.table.type')}</TableHead>
            <TableHead>{t('sso.table.name')}</TableHead>
            <TableHead>{t('sso.table.enabled')}</TableHead>
            <TableHead>{t('sso.table.created')}</TableHead>
            <TableHead className="text-right">
              {t('sso.table.actions')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {isPending && (
            <TableRow>
              <TableCell colSpan={6} className="text-center text-muted-foreground">
                {t('common.loading')}
              </TableCell>
            </TableRow>
          )}
          {isError && (
            <TableRow>
              <TableCell colSpan={6} className="text-center text-destructive">
                {friendlyError(error, t('sso.error.fallback'), t)}
              </TableCell>
            </TableRow>
          )}
          {!isPending && !isError && providers.length === 0 && (
            <TableRow>
              <TableCell colSpan={6} className="p-0">
                <EmptyState
                  icon={KeyRound}
                  title={t('sso.empty.title')}
                  description={t('sso.empty.description')}
                  className="rounded-none border-0 bg-transparent"
                />
              </TableCell>
            </TableRow>
          )}
          {!isPending &&
            !isError &&
            providers.map((p) => (
              <ProviderRow
                key={p.id}
                provider={p}
                onEdit={() => onEdit(p)}
                canMutate={canMutate}
              />
            ))}
        </TableBody>
      </Table>
    </div>
  )
}

function ProviderRow({
  provider,
  onEdit,
  canMutate,
}: {
  provider: SSOProvider
  onEdit: () => void
  canMutate: boolean
}): React.ReactElement {
  const t = useT()
  const del = useDeleteSSOProvider()
  const handleDelete = (): void => {
    if (typeof window !== 'undefined' &&
        !window.confirm(t('sso.action.delete.confirm'))) {
      return
    }
    del.mutate(provider.id)
  }
  const adminTooltip = !canMutate ? t('common.role.required.admin') : undefined
  return (
    <TableRow>
      <TableCell className="font-mono text-xs text-muted-foreground" title={provider.id}>
        {provider.id}
      </TableCell>
      <TableCell>
        <Badge
          variant={providerTypeBadgeVariant(provider.type)}
          className="text-[10px] uppercase"
        >
          {provider.type}
        </Badge>
      </TableCell>
      <TableCell className="font-medium">
        {displayProviderName(provider)}
      </TableCell>
      <TableCell>
        <Badge
          variant={provider.enabled ? 'default' : 'outline'}
          className="text-[10px]"
        >
          {provider.enabled
            ? t('sso.table.enabled.on')
            : t('sso.table.enabled.off')}
        </Badge>
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {provider.createdAt
          ? new Date(provider.createdAt).toLocaleString()
          : '—'}
      </TableCell>
      <TableCell className="text-right">
        <div className="inline-flex gap-1">
          <Button
            size="sm"
            variant="outline"
            onClick={onEdit}
            disabled={!canMutate}
            title={adminTooltip}
          >
            {t('sso.action.edit')}
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={handleDelete}
            disabled={del.isPending || !canMutate}
            title={adminTooltip}
          >
            {del.isPending
              ? t('sso.action.deleting')
              : t('sso.action.delete')}
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

// ProviderForm — 신규/수정 통합 폼. editing!=null이면 수정 모드.
function ProviderForm({
  editing,
  onDone,
}: {
  editing: SSOProvider | null
  onDone: () => void
}): React.ReactElement {
  const t = useT()
  const create = useCreateSSOProvider()
  const update = useUpdateSSOProvider()

  // 수정 모드는 type 변경 불가 (백엔드도 type 변경 미지원).
  const initialType: SSOProviderType = editing?.type ?? 'oidc'
  const [type, setType] = useState<SSOProviderType>(initialType)
  const [name, setName] = useState(editing?.name ?? '')
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)
  // OIDC config state.
  const oidcCfg = oidcFromConfig(editing?.config)
  const [issuer, setIssuer] = useState(oidcCfg.issuer)
  const [clientId, setClientId] = useState(oidcCfg.clientId)
  const [redirectUri, setRedirectUri] = useState(oidcCfg.redirectUri)
  const [scopesText, setScopesText] = useState(oidcCfg.scopes.join(', '))
  // SAML config state.
  const samlCfg = samlFromConfig(editing?.config)
  const [metadataUrl, setMetadataUrl] = useState(samlCfg.metadataUrl)
  const [metadataXml, setMetadataXml] = useState(samlCfg.metadataXml)
  const [acsUrl, setAcsUrl] = useState(samlCfg.acsUrl)

  const [validationErrs, setValidationErrs] = useState<string[]>([])
  const [success, setSuccess] = useState('')

  const isUpdate = !!editing

  const handleSubmit = (e: React.FormEvent): void => {
    e.preventDefault()
    setSuccess('')
    setValidationErrs([])

    // name 필수.
    const baseErrs: string[] = []
    if (!name.trim()) baseErrs.push('name')

    let errs: string[] = []
    let cfg: Record<string, unknown> = {}
    if (type === 'oidc') {
      const scopes = parseScopes(scopesText)
      cfg = { issuer, clientId, redirectUri, scopes }
      errs = validateOIDCConfig({ issuer, clientId, redirectUri, scopes })
    } else {
      cfg = metadataUrl
        ? { metadataUrl, acsUrl }
        : { metadataXml, acsUrl }
      errs = validateSAMLConfig({ metadataUrl, metadataXml, acsUrl })
    }
    const all = [...baseErrs, ...errs]
    if (all.length > 0) {
      setValidationErrs(all)
      return
    }

    if (isUpdate && editing) {
      const vars: UpdateSSOProviderVars = {
        providerId: editing.id,
        name: name.trim(),
        enabled,
        config: cfg,
      }
      update.mutate(vars, {
        onSuccess: () => {
          setSuccess(t('sso.form.success.update'))
          onDone()
        },
      })
    } else {
      const vars: CreateSSOProviderVars = {
        type,
        name: name.trim(),
        enabled,
        config: cfg,
      }
      create.mutate(vars, {
        onSuccess: () => {
          setSuccess(t('sso.form.success.create'))
          onDone()
        },
      })
    }
  }

  const mutating = create.isPending || update.isPending
  const lastErr = create.error ?? update.error
  const lastErrIsApi = lastErr instanceof ApiError ? lastErr : null

  return (
    <form
      onSubmit={handleSubmit}
      className="grid grid-cols-1 gap-3 rounded-md border p-4 md:grid-cols-2"
      aria-label={isUpdate ? t('sso.form.section.edit') : t('sso.form.section')}
    >
      <div className="md:col-span-2 flex items-center justify-between">
        <h3 className="text-sm font-medium">
          {isUpdate ? t('sso.form.section.edit') : t('sso.form.section')}
        </h3>
        {isUpdate && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={onDone}
          >
            {t('sso.form.cancel')}
          </Button>
        )}
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="sso-type">{t('sso.form.type')}</Label>
        <Select
          value={type}
          onValueChange={(v) => setType(v as SSOProviderType)}
          disabled={isUpdate}
        >
          <SelectTrigger id="sso-type">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="oidc">{t('sso.form.type.oidc')}</SelectItem>
            <SelectItem value="saml">{t('sso.form.type.saml')}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="sso-name">{t('sso.form.name')}</Label>
        <Input
          id="sso-name"
          required
          placeholder={t('sso.form.name.placeholder')}
          value={name}
          onChange={(ev) => setName(ev.target.value)}
        />
      </div>

      {type === 'oidc' && (
        <>
          <div className="flex flex-col gap-1.5 md:col-span-2">
            <Label htmlFor="sso-issuer">{t('sso.form.oidc.issuer')}</Label>
            <Input
              id="sso-issuer"
              placeholder={t('sso.form.oidc.issuer.placeholder')}
              value={issuer}
              onChange={(ev) => setIssuer(ev.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sso-client">{t('sso.form.oidc.clientId')}</Label>
            <Input
              id="sso-client"
              placeholder={t('sso.form.oidc.clientId.placeholder')}
              value={clientId}
              onChange={(ev) => setClientId(ev.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="sso-redirect">{t('sso.form.oidc.redirectUri')}</Label>
            <Input
              id="sso-redirect"
              placeholder={t('sso.form.oidc.redirectUri.placeholder')}
              value={redirectUri}
              onChange={(ev) => setRedirectUri(ev.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5 md:col-span-2">
            <Label htmlFor="sso-scopes">{t('sso.form.oidc.scopes')}</Label>
            <Input
              id="sso-scopes"
              placeholder={t('sso.form.oidc.scopes.placeholder')}
              value={scopesText}
              onChange={(ev) => setScopesText(ev.target.value)}
            />
          </div>
        </>
      )}

      {type === 'saml' && (
        <>
          <div className="flex flex-col gap-1.5 md:col-span-2">
            <Label htmlFor="sso-metaurl">{t('sso.form.saml.metadataUrl')}</Label>
            <Input
              id="sso-metaurl"
              placeholder={t('sso.form.saml.metadataUrl.placeholder')}
              value={metadataUrl}
              onChange={(ev) => setMetadataUrl(ev.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5 md:col-span-2">
            <Label htmlFor="sso-metaxml">{t('sso.form.saml.metadataXml')}</Label>
            <Textarea
              id="sso-metaxml"
              rows={4}
              placeholder={t('sso.form.saml.metadataXml.placeholder')}
              value={metadataXml}
              onChange={(ev) => setMetadataXml(ev.target.value)}
              className="font-mono text-xs"
            />
          </div>
          <div className="flex flex-col gap-1.5 md:col-span-2">
            <Label htmlFor="sso-acs">{t('sso.form.saml.acsUrl')}</Label>
            <Input
              id="sso-acs"
              placeholder={t('sso.form.saml.acsUrl.placeholder')}
              value={acsUrl}
              onChange={(ev) => setAcsUrl(ev.target.value)}
            />
          </div>
        </>
      )}

      <div className="md:col-span-2 flex items-center gap-2">
        <Switch
          id="sso-enabled"
          checked={enabled}
          onCheckedChange={(v) => setEnabled(Boolean(v))}
        />
        <Label htmlFor="sso-enabled" className="text-sm">
          {t('sso.form.enabled')}
        </Label>
      </div>

      {validationErrs.length > 0 && (
        <ul
          className="md:col-span-2 list-disc rounded-md border border-destructive/40 bg-destructive/5 px-5 py-2 text-xs text-destructive"
          role="alert"
        >
          {validationErrs.map((k) => (
            <li key={k}>{t(validationLabelKey(k))}</li>
          ))}
        </ul>
      )}

      {(create.isError || update.isError) && (
        <p className="md:col-span-2 text-sm text-destructive" role="alert">
          {friendlyError(lastErrIsApi, t('sso.form.error.fallback'), t)}
        </p>
      )}
      {success && (
        <p className="md:col-span-2 text-sm text-emerald-600">{success}</p>
      )}

      <div className="md:col-span-2 flex justify-end gap-2">
        <Button type="submit" disabled={mutating}>
          {mutating
            ? t('sso.form.submitting')
            : isUpdate
              ? t('sso.form.submit.update')
              : t('sso.form.submit')}
        </Button>
      </div>
    </form>
  )
}

// ─────────────────────────────────────────────────────────────────────────
// Helpers — exported for unit testing.
// ─────────────────────────────────────────────────────────────────────────

// displayProviderName — name이 비어있으면 id fallback.
export function displayProviderName(p: SSOProvider): string {
  const n = (p.name ?? '').trim()
  if (n.length > 0) return n
  return p.id
}

// providerTypeBadgeVariant — type별 Badge variant 매핑.
export function providerTypeBadgeVariant(
  type: SSOProviderType,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (type) {
    case 'oidc':
      return 'default'
    case 'saml':
      return 'secondary'
  }
}

// isValidUrl — http(s) URL 여부 검증. 빈 문자열 false.
export function isValidUrl(s: string): boolean {
  if (!s) return false
  try {
    const u = new URL(s)
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}

// parseScopes — 쉼표/공백 혼합 문자열을 scope 배열로. 중복 제거 + 정렬.
export function parseScopes(s: string): string[] {
  if (!s) return []
  const parts = s
    .split(/[,\s]+/)
    .map((x) => x.trim())
    .filter((x) => x.length > 0)
  if (parts.length === 0) return []
  const set = new Set(parts)
  return Array.from(set).sort()
}

export interface OIDCConfigInput {
  issuer: string
  clientId: string
  redirectUri: string
  scopes: string[]
}

// validateOIDCConfig — OIDC config 필수 필드 검증. 에러 키 배열 반환.
export function validateOIDCConfig(cfg: OIDCConfigInput): string[] {
  const errs: string[] = []
  if (!isValidUrl(cfg.issuer)) errs.push('issuer')
  if (!cfg.clientId.trim()) errs.push('clientId')
  if (!isValidUrl(cfg.redirectUri)) errs.push('redirectUri')
  if (!cfg.scopes || cfg.scopes.length === 0) errs.push('scopes')
  return errs
}

export interface SAMLConfigInput {
  metadataUrl: string
  metadataXml: string
  acsUrl: string
}

// validateSAMLConfig — SAML config 검증. metadataUrl 또는 metadataXml 중 하나 필수.
export function validateSAMLConfig(cfg: SAMLConfigInput): string[] {
  const errs: string[] = []
  const hasUrl = cfg.metadataUrl.trim().length > 0
  const hasXml = cfg.metadataXml.trim().length > 0
  if (!hasUrl && !hasXml) {
    errs.push('metadata')
  } else if (hasUrl && !isValidUrl(cfg.metadataUrl)) {
    errs.push('metadata')
  }
  if (!isValidUrl(cfg.acsUrl)) errs.push('acsUrl')
  return errs
}

// validationLabelKey — validation 에러 키 → dict 키 매핑.
function validationLabelKey(k: string): DictKey {
  switch (k) {
    case 'issuer':
      return 'sso.form.validation.issuer'
    case 'clientId':
      return 'sso.form.validation.clientId'
    case 'redirectUri':
      return 'sso.form.validation.redirectUri'
    case 'scopes':
      return 'sso.form.validation.scopes'
    case 'metadata':
      return 'sso.form.validation.metadata'
    case 'acsUrl':
      return 'sso.form.validation.acsUrl'
    case 'name':
      return 'sso.form.validation.name'
    default:
      return 'sso.form.error.fallback'
  }
}

// friendlyError — ApiError status 기반 친화 메시지 매핑.
function friendlyError(
  err: unknown,
  fallback: string,
  t: (key: DictKey) => string,
): string {
  if (err instanceof ApiError) {
    if (err.status === 409) return t('sso.error.duplicate')
    if (err.status === 404) return t('sso.error.notfound')
    if (err.status === 503) return t('sso.error.disabled')
    return err.message
  }
  return fallback
}

// oidcFromConfig — provider.config에서 OIDC 필드를 안전하게 추출.
function oidcFromConfig(cfg: Record<string, unknown> | undefined): {
  issuer: string
  clientId: string
  redirectUri: string
  scopes: string[]
} {
  const c = cfg ?? {}
  const issuer = typeof c.issuer === 'string' ? c.issuer : ''
  const clientId = typeof c.clientId === 'string' ? c.clientId : ''
  const redirectUri =
    typeof c.redirectUri === 'string' ? c.redirectUri : ''
  const scopes = Array.isArray(c.scopes)
    ? (c.scopes.filter((x) => typeof x === 'string') as string[])
    : []
  return { issuer, clientId, redirectUri, scopes }
}

// samlFromConfig — provider.config에서 SAML 필드를 안전하게 추출.
function samlFromConfig(cfg: Record<string, unknown> | undefined): {
  metadataUrl: string
  metadataXml: string
  acsUrl: string
} {
  const c = cfg ?? {}
  const metadataUrl =
    typeof c.metadataUrl === 'string' ? c.metadataUrl : ''
  const metadataXml =
    typeof c.metadataXml === 'string' ? c.metadataXml : ''
  const acsUrl = typeof c.acsUrl === 'string' ? c.acsUrl : ''
  return { metadataUrl, metadataXml, acsUrl }
}

export const Route = createFileRoute('/_authenticated/sso')({
  component: SSOPage,
})
