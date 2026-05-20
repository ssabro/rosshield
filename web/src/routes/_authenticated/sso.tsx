import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { KeyRound } from 'lucide-react'

import { ApiError } from '@/api/errors'
import {
  useCreateSSOGroupMapping,
  useCreateSSOProvider,
  useDeleteSSOGroupMapping,
  useDeleteSSOProvider,
  useFleets,
  useHasPermission,
  useSSOGroupMappings,
  useSSOProviders,
  useUpdateSSOProvider,
} from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { requirePermission } from '@/lib/route-guards'
import { mutationGuardTitle, useIsOffline } from '@/lib/use-is-offline'
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
  SSOGroupMapping,
  SSOGroupMappingScopeType,
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
// a11y-drilldown.test.tsx mount용 named export.
export function SSOPage(): React.ReactElement {
  const t = useT()
  const providers = useSSOProviders()
  // RBAC Stage 5 — sso provider 관리는 tenant_admin.admin (§2.2 ID 2).
  const isAdmin = useHasPermission('tenant_admin', 'admin')
  const isOffline = useIsOffline()
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
            disabled={!isAdmin || isOffline}
            title={mutationGuardTitle({
              isOffline,
              offlineLabel: t('pwa.offline.mutationBlocked'),
              fallback: !isAdmin ? t('common.role.required.admin') : undefined,
            })}
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
        isOffline={isOffline}
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
  isOffline,
}: {
  providers: SSOProvider[]
  isPending: boolean
  isError: boolean
  error: unknown
  onEdit: (p: SSOProvider) => void
  canMutate: boolean
  isOffline: boolean
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
                isOffline={isOffline}
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
  isOffline,
}: {
  provider: SSOProvider
  onEdit: () => void
  canMutate: boolean
  isOffline: boolean
}): React.ReactElement {
  const t = useT()
  const del = useDeleteSSOProvider()
  // RBAC fleet 정밀화 Stage 5 — Group → Role 매핑 섹션 토글.
  const [showMappings, setShowMappings] = useState(false)
  const handleDelete = (): void => {
    if (typeof window !== 'undefined' &&
        !window.confirm(t('sso.action.delete.confirm'))) {
      return
    }
    del.mutate(provider.id)
  }
  const guardTitle = mutationGuardTitle({
    isOffline,
    offlineLabel: t('pwa.offline.mutationBlocked'),
    fallback: !canMutate ? t('common.role.required.admin') : undefined,
  })
  return (
    <>
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
              variant="ghost"
              onClick={() => setShowMappings((s) => !s)}
              aria-expanded={showMappings}
            >
              {showMappings
                ? t('sso.groupmap.toggle.hide')
                : t('sso.groupmap.toggle.show')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={onEdit}
              disabled={!canMutate || isOffline}
              title={guardTitle}
            >
              {t('sso.action.edit')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={handleDelete}
              disabled={del.isPending || !canMutate || isOffline}
              title={guardTitle}
            >
              {del.isPending
                ? t('sso.action.deleting')
                : t('sso.action.delete')}
            </Button>
          </div>
        </TableCell>
      </TableRow>
      {showMappings && (
        <TableRow>
          <TableCell colSpan={6} className="bg-muted/30 p-0">
            <GroupMappingsSection
              providerId={provider.id}
              canMutate={canMutate}
              isOffline={isOffline}
            />
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

// === RBAC fleet 정밀화 Stage 5 — Group → Role 매핑 섹션 ===

function GroupMappingsSection({
  providerId,
  canMutate,
  isOffline,
}: {
  providerId: string
  canMutate: boolean
  isOffline: boolean
}): React.ReactElement {
  const t = useT()
  const mappings = useSSOGroupMappings(providerId)
  const fleets = useFleets()
  return (
    <div className="space-y-3 p-4">
      <div>
        <h4 className="text-sm font-semibold">{t('sso.groupmap.section')}</h4>
        <p className="mt-1 text-xs text-muted-foreground">
          {t('sso.groupmap.description')}
        </p>
      </div>
      {canMutate && (
        <GroupMappingForm
          providerId={providerId}
          isOffline={isOffline}
          fleets={(fleets.data ?? []).map((f) => ({ id: f.id, name: f.name }))}
        />
      )}
      <GroupMappingsTable
        mappings={mappings.data ?? []}
        isPending={mappings.isPending}
        isError={mappings.isError}
        error={mappings.error}
        canMutate={canMutate}
        isOffline={isOffline}
        providerId={providerId}
      />
    </div>
  )
}

interface FleetOption {
  id: string
  name: string
}

function GroupMappingForm({
  providerId,
  isOffline,
  fleets,
}: {
  providerId: string
  isOffline: boolean
  fleets: FleetOption[]
}): React.ReactElement {
  const t = useT()
  const create = useCreateSSOGroupMapping()
  const [groupValue, setGroupValue] = useState('')
  const [roleId, setRoleId] = useState('')
  const [scopeType, setScopeType] = useState<SSOGroupMappingScopeType>('tenant')
  const [scopeId, setScopeId] = useState('')
  const [errs, setErrs] = useState<string[]>([])

  const handleSubmit = (e: React.FormEvent): void => {
    e.preventDefault()
    const newErrs: string[] = []
    if (!groupValue.trim()) newErrs.push('group')
    if (!roleId.trim()) newErrs.push('role')
    if (scopeType === 'fleet' && !scopeId.trim()) newErrs.push('fleet')
    if (newErrs.length > 0) {
      setErrs(newErrs)
      return
    }
    setErrs([])
    create.mutate(
      {
        providerId,
        groupValue: groupValue.trim(),
        roleId: roleId.trim(),
        scopeType,
        scopeId: scopeType === 'fleet' ? scopeId.trim() : undefined,
      },
      {
        onSuccess: () => {
          setGroupValue('')
          setRoleId('')
          setScopeId('')
        },
      },
    )
  }

  const lastErr = create.error instanceof ApiError ? create.error : null

  return (
    <form
      onSubmit={handleSubmit}
      className="grid grid-cols-1 gap-2 rounded-md border bg-background p-3 md:grid-cols-4"
      aria-label={t('sso.groupmap.section')}
    >
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`gm-group-${providerId}`}>{t('sso.groupmap.form.group')}</Label>
        <Input
          id={`gm-group-${providerId}`}
          placeholder={t('sso.groupmap.form.group.placeholder')}
          value={groupValue}
          onChange={(ev) => setGroupValue(ev.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`gm-role-${providerId}`}>{t('sso.groupmap.form.role')}</Label>
        <Input
          id={`gm-role-${providerId}`}
          placeholder={t('sso.groupmap.form.role.placeholder')}
          value={roleId}
          onChange={(ev) => setRoleId(ev.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`gm-scope-${providerId}`}>Scope</Label>
        <Select
          value={scopeType}
          onValueChange={(v) => setScopeType(v as SSOGroupMappingScopeType)}
        >
          <SelectTrigger id={`gm-scope-${providerId}`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="tenant">{t('sso.groupmap.form.scope.tenant')}</SelectItem>
            <SelectItem value="fleet">{t('sso.groupmap.form.scope.fleet')}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      {scopeType === 'fleet' ? (
        <div className="flex flex-col gap-1.5">
          <Label htmlFor={`gm-fleet-${providerId}`}>{t('sso.groupmap.form.fleetId')}</Label>
          <Select
            value={scopeId}
            onValueChange={(v) => setScopeId(v)}
          >
            <SelectTrigger id={`gm-fleet-${providerId}`}>
              <SelectValue placeholder="—" />
            </SelectTrigger>
            <SelectContent>
              {fleets.map((f) => (
                <SelectItem key={f.id} value={f.id}>
                  {f.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : (
        <div />
      )}
      {errs.length > 0 && (
        <ul
          className="md:col-span-4 list-disc rounded-md border border-destructive/40 bg-destructive/5 px-5 py-2 text-xs text-destructive"
          role="alert"
        >
          {errs.map((k) => (
            <li key={k}>{t(groupMappingErrLabel(k))}</li>
          ))}
        </ul>
      )}
      {create.isError && (
        <p
          className="md:col-span-4 text-xs text-destructive"
          role="alert"
        >
          {lastErr && lastErr.status === 409
            ? t('sso.groupmap.error.duplicate')
            : (lastErr?.message ?? t('sso.groupmap.error.fallback'))}
        </p>
      )}
      <div className="md:col-span-4 flex justify-end">
        <Button
          type="submit"
          size="sm"
          disabled={create.isPending || isOffline}
          title={mutationGuardTitle({
            isOffline,
            offlineLabel: t('pwa.offline.mutationBlocked'),
          })}
        >
          {create.isPending
            ? t('sso.groupmap.form.submitting')
            : t('sso.groupmap.form.submit')}
        </Button>
      </div>
    </form>
  )
}

function GroupMappingsTable({
  mappings,
  isPending,
  isError,
  error: _error,
  canMutate,
  isOffline,
  providerId,
}: {
  mappings: SSOGroupMapping[]
  isPending: boolean
  isError: boolean
  error: unknown
  canMutate: boolean
  isOffline: boolean
  providerId: string
}): React.ReactElement {
  const t = useT()
  const del = useDeleteSSOGroupMapping()
  return (
    <div className="rounded-md border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('sso.groupmap.table.group')}</TableHead>
            <TableHead>{t('sso.groupmap.table.role')}</TableHead>
            <TableHead>{t('sso.groupmap.table.scope')}</TableHead>
            <TableHead>{t('sso.groupmap.table.created')}</TableHead>
            <TableHead className="text-right">
              {t('sso.groupmap.table.actions')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {isPending && (
            <TableRow>
              <TableCell colSpan={5} className="text-center text-xs text-muted-foreground">
                {t('common.loading')}
              </TableCell>
            </TableRow>
          )}
          {isError && (
            <TableRow>
              <TableCell colSpan={5} className="text-center text-xs text-destructive">
                {t('sso.groupmap.error.fallback')}
              </TableCell>
            </TableRow>
          )}
          {!isPending && !isError && mappings.length === 0 && (
            <TableRow>
              <TableCell colSpan={5} className="text-center text-xs text-muted-foreground">
                {t('sso.groupmap.empty')}
              </TableCell>
            </TableRow>
          )}
          {mappings.map((m) => (
            <TableRow key={m.id}>
              <TableCell className="font-mono text-xs">{m.groupValue}</TableCell>
              <TableCell className="font-mono text-xs">{m.roleId}</TableCell>
              <TableCell className="text-xs">
                {m.scopeType}
                {m.scopeType === 'fleet' && m.scopeId ? ` / ${m.scopeId}` : ''}
              </TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {m.createdAt
                  ? new Date(m.createdAt).toLocaleString()
                  : '—'}
              </TableCell>
              <TableCell className="text-right">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={del.isPending || !canMutate || isOffline}
                  onClick={() => {
                    if (
                      typeof window !== 'undefined' &&
                      !window.confirm(t('sso.groupmap.action.delete.confirm'))
                    ) {
                      return
                    }
                    del.mutate({ providerId, mappingId: m.id })
                  }}
                  title={mutationGuardTitle({
                    isOffline,
                    offlineLabel: t('pwa.offline.mutationBlocked'),
                    fallback: !canMutate ? t('common.role.required.admin') : undefined,
                  })}
                >
                  {t('sso.groupmap.action.delete')}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function groupMappingErrLabel(k: string): DictKey {
  switch (k) {
    case 'group':
      return 'sso.groupmap.validation.group'
    case 'role':
      return 'sso.groupmap.validation.role'
    case 'fleet':
      return 'sso.groupmap.validation.fleet'
    default:
      return 'sso.groupmap.error.fallback'
  }
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
  const isOffline = useIsOffline()

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
        <Button
          type="submit"
          disabled={mutating || isOffline}
          title={mutationGuardTitle({
            isOffline,
            offlineLabel: t('pwa.offline.mutationBlocked'),
          })}
        >
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
  beforeLoad: () => requirePermission('tenant_admin', 'admin'),
  component: SSOPage,
})
