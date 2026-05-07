import { createFileRoute } from '@tanstack/react-router'

import { ApiError } from '@/api/errors'
import { useLicenseInfo, useMe } from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { useAuthStore } from '@/stores/auth'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

import type { DictKey } from '@/i18n/dict'

// `/settings` — 사용자/테넌트/클라이언트 설정 + 빌드 정보 (B1).
//   - 토글(테마·언어)은 헤더에서 — 본 페이지는 위치 안내만.
function SettingsPage(): React.ReactElement {
  const t = useT()
  const me = useMe()
  const storeUser = useAuthStore((s) => s.user)
  const user = me.data ?? storeUser

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.settings.title')}
        description={t('pages.settings.description')}
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium text-muted-foreground">
            {t('settings.user.section')}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <Row label={t('settings.user.id')} value={user?.id ?? '—'} mono />
          <Row label={t('settings.user.email')} value={user?.email ?? '—'} />
          <Row
            label={t('settings.user.displayName')}
            value={user?.displayName ?? '—'}
          />
          <Row
            label={t('settings.user.tenantId')}
            value={user?.tenantId ?? '—'}
            mono
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium text-muted-foreground">
            {t('settings.preferences.section')}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm text-muted-foreground">
          <p>{t('settings.preferences.theme')}</p>
          <p>{t('settings.preferences.locale')}</p>
        </CardContent>
      </Card>

      <LicenseCard />

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium text-muted-foreground">
            {t('settings.about.section')}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <Row label={t('settings.about.version')} value={t('app.version')} mono />
          <Row
            label={t('settings.about.codename')}
            value={t('settings.about.codename.value')}
          />
        </CardContent>
      </Card>
    </div>
  )
}

// LicenseCard — E24 라이선스 메타 표시 카드 (B5 첫 노출).
//   community 에디션이면 활성 안내, enterprise면 만료/feature/quota 표시.
function LicenseCard(): React.ReactElement {
  const t = useT()
  const license = useLicenseInfo()

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('settings.license.section')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        {license.isPending && (
          <p className="text-muted-foreground">{t('common.loading')}</p>
        )}
        {license.isError && (
          <p className="text-destructive">
            {license.error instanceof ApiError
              ? license.error.message
              : t('settings.license.error')}
          </p>
        )}
        {license.isSuccess && (
          <>
            <div className="flex items-center gap-2">
              <Badge variant={license.data.edition === 'enterprise' ? 'default' : 'secondary'}>
                {t(editionLabelKey(license.data.edition))}
              </Badge>
              {license.data.expired && (
                <Badge variant="destructive" className="text-[10px]">
                  {t('settings.license.expired')}
                </Badge>
              )}
            </div>

            {license.data.edition === 'community' ? (
              <p className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                {t('settings.license.community.note')}
              </p>
            ) : (
              <>
                {license.data.issuedTo && (
                  <Row label={t('settings.license.issuedTo')} value={license.data.issuedTo} />
                )}
                {license.data.issuedAt && (
                  <Row
                    label={t('settings.license.issuedAt')}
                    value={new Date(license.data.issuedAt).toLocaleString()}
                  />
                )}
                {license.data.expiresAt && (
                  <Row
                    label={t('settings.license.expiresAt')}
                    value={new Date(license.data.expiresAt).toLocaleString()}
                  />
                )}
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">{t('settings.license.features')}</p>
                  {license.data.features && license.data.features.length > 0 ? (
                    <div className="flex flex-wrap gap-1">
                      {license.data.features.map((f) => (
                        <Badge key={f} variant="outline" className="text-[10px]">
                          {f}
                        </Badge>
                      ))}
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      {t('settings.license.features.empty')}
                    </p>
                  )}
                </div>
              </>
            )}

            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">{t('settings.license.quotas.title')}</p>
              <Row
                label={t('settings.license.quotas.robotsMax')}
                value={
                  license.data.quotas.robotsMax > 0
                    ? String(license.data.quotas.robotsMax)
                    : t('settings.license.quotas.unlimited')
                }
                mono
              />
              <Row
                label={t('settings.license.quotas.scansPerDay')}
                value={
                  license.data.quotas.scansPerDay > 0
                    ? String(license.data.quotas.scansPerDay)
                    : t('settings.license.quotas.unlimited')
                }
                mono
              />
              <Row
                label={t('settings.license.quotas.llmTokensPerDay')}
                value={
                  license.data.quotas.llmTokensPerDay > 0
                    ? String(license.data.quotas.llmTokensPerDay)
                    : t('settings.license.quotas.unlimited')
                }
                mono
              />
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

function editionLabelKey(edition: 'community' | 'enterprise'): DictKey {
  return edition === 'enterprise'
    ? 'settings.license.edition.enterprise'
    : 'settings.license.edition.community'
}

function Row({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}): React.ReactElement {
  return (
    <div className="grid grid-cols-1 gap-1 sm:grid-cols-[10rem_1fr]">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span
        className={
          mono
            ? 'break-all font-mono text-xs text-foreground'
            : 'text-xs text-foreground'
        }
      >
        {value}
      </span>
    </div>
  )
}

export const Route = createFileRoute('/_authenticated/settings')({
  component: SettingsPage,
})
