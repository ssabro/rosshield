import { createFileRoute } from '@tanstack/react-router'

import { useMe } from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { useAuthStore } from '@/stores/auth'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

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
