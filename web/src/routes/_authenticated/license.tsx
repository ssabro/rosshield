import { createFileRoute } from '@tanstack/react-router'
import { Award, Check, Minus } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { useLicenseInfo } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useT } from '@/i18n/t'
import { cn } from '@/lib/utils'

import type { LicenseInfo } from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

// `/license` — B5 별도 라이선스 페이지.
//
// 헤더 카드(에디션·발급·만료) + Feature 그리드(SSO/MT/Webhook/Cloud/HA) +
// Quota 표 + 사용량 placeholder. Community 에디션은 안내 카드.
//
// 데이터: useLicenseInfo (E24 GET /api/v1/license — 모든 인증 사용자 read-only).
// 백엔드 응답에는 licenseId 필드가 없어 본 페이지도 표시하지 않는다.
function LicensePage(): React.ReactElement {
  const t = useT()
  const license = useLicenseInfo()

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.license.title')}
        description={t('pages.license.description')}
      />

      {license.isPending && (
        <Card>
          <CardContent className="py-8 text-center text-sm text-muted-foreground">
            {t('common.loading')}
          </CardContent>
        </Card>
      )}

      {license.isError && (
        <EmptyState
          title={
            license.error instanceof ApiError
              ? license.error.message
              : t('license.page.error.fallback')
          }
        />
      )}

      {license.isSuccess && (
        <>
          {license.data.expired && (
            <div
              role="alert"
              className="rounded-md border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive"
            >
              {t('license.page.expired.banner')}
            </div>
          )}

          <HeaderCard data={license.data} />

          {license.data.edition === 'community' ? (
            <CommunityCard />
          ) : (
            <FeaturesCard data={license.data} />
          )}

          <QuotasCard data={license.data} />
          <UsageCard />
        </>
      )}
    </div>
  )
}

function HeaderCard({ data }: { data: LicenseInfo }): React.ReactElement {
  const t = useT()
  const expiresInfo = formatExpiresIn(data.expiresAt)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('license.page.header.section')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={editionBadgeVariant(data.edition)}>
            {t(editionLabelKey(data.edition))}
          </Badge>
          {data.expired && (
            <Badge variant="destructive" className="text-[10px]">
              {t('settings.license.expired')}
            </Badge>
          )}
        </div>

        <Row label={t('license.page.edition')} value={t(editionLabelKey(data.edition))} />
        {data.issuedTo && (
          <Row label={t('license.page.issuedTo')} value={data.issuedTo} />
        )}
        {data.issuedAt && (
          <Row
            label={t('license.page.issuedAt')}
            value={new Date(data.issuedAt).toLocaleString()}
          />
        )}
        {data.expiresAt && (
          <Row
            label={t('license.page.expiresAt')}
            value={
              new Date(data.expiresAt).toLocaleString() +
              (expiresInfo.kind !== 'none'
                ? '  •  ' + formatExpiresInLabel(expiresInfo, t)
                : '')
            }
          />
        )}
      </CardContent>
    </Card>
  )
}

function FeaturesCard({ data }: { data: LicenseInfo }): React.ReactElement {
  const t = useT()
  const active = new Set(data.features ?? [])

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('license.page.features.section')}
        </CardTitle>
        <CardDescription className="text-xs">
          {t('license.page.features.description')}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {KNOWN_LICENSE_FEATURES.map((f) => {
            const isActive = active.has(f)
            return (
              <div
                key={f}
                className={cn(
                  'flex items-center justify-between rounded-md border border-border px-3 py-2 text-sm',
                  isActive ? 'bg-primary/5' : 'bg-muted/30 text-muted-foreground',
                )}
              >
                <div className="flex items-center gap-2">
                  {isActive ? (
                    <Check
                      className="h-4 w-4 text-primary"
                      aria-label={t('license.page.feature.active')}
                    />
                  ) : (
                    <Minus
                      className="h-4 w-4 text-muted-foreground"
                      aria-label={t('license.page.feature.inactive')}
                    />
                  )}
                  <span className={isActive ? 'font-medium text-foreground' : ''}>
                    {t(featureLabelKey(f))}
                  </span>
                </div>
                <Badge variant={isActive ? 'default' : 'outline'} className="text-[10px]">
                  {isActive
                    ? t('license.page.feature.active')
                    : t('license.page.feature.inactive')}
                </Badge>
              </div>
            )
          })}
        </div>
      </CardContent>
    </Card>
  )
}

function QuotasCard({ data }: { data: LicenseInfo }): React.ReactElement {
  const t = useT()
  const rows: ReadonlyArray<{ key: DictKey; value: number }> = [
    { key: 'settings.license.quotas.robotsMax', value: data.quotas.robotsMax },
    { key: 'settings.license.quotas.scansPerDay', value: data.quotas.scansPerDay },
    {
      key: 'settings.license.quotas.llmTokensPerDay',
      value: data.quotas.llmTokensPerDay,
    },
  ]

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('license.page.quotas.section')}
        </CardTitle>
        <CardDescription className="text-xs">
          {t('license.page.quotas.description')}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('license.page.quotas.col.metric')}</TableHead>
              <TableHead>{t('license.page.quotas.col.limit')}</TableHead>
              <TableHead>{t('license.page.quotas.col.usage')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => {
              const display = quotaDisplay(row.value)
              return (
                <TableRow key={row.key}>
                  <TableCell className="text-sm">{t(row.key)}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {display === 'unlimited'
                      ? t('license.page.quotas.unlimited')
                      : display}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {t('license.page.quotas.usage.placeholder')}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function UsageCard(): React.ReactElement {
  const t = useT()
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('license.page.usage.section')}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <p className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-3 text-xs text-muted-foreground">
          {t('license.page.usage.note')}
        </p>
      </CardContent>
    </Card>
  )
}

function CommunityCard(): React.ReactElement {
  const t = useT()
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
          <Award className="h-4 w-4" aria-hidden />
          {t('license.page.community.title')}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground">
          {t('license.page.community.description')}
        </p>
      </CardContent>
    </Card>
  )
}

function Row({
  label,
  value,
}: {
  label: string
  value: string
}): React.ReactElement {
  return (
    <div className="grid grid-cols-1 gap-1 sm:grid-cols-[10rem_1fr]">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="text-xs text-foreground">{value}</span>
    </div>
  )
}

// ────────────────────────────────────────────────────────────────────────
// Helpers (단위 테스트 가능 — license.test.tsx에서 import)
// ────────────────────────────────────────────────────────────────────────

export type LicenseEdition = 'community' | 'enterprise'

// KNOWN_LICENSE_FEATURES — 백엔드 license payload가 노출하는 5종 enterprise feature.
//   순서는 UI 그리드 표기 순서를 따른다.
export const KNOWN_LICENSE_FEATURES: ReadonlyArray<
  'sso' | 'mt' | 'webhook' | 'cloud' | 'ha'
> = ['sso', 'mt', 'webhook', 'cloud', 'ha']

export function editionLabelKey(edition: LicenseEdition): DictKey {
  return edition === 'enterprise'
    ? 'settings.license.edition.enterprise'
    : 'settings.license.edition.community'
}

export function editionBadgeVariant(
  edition: LicenseEdition,
): 'default' | 'secondary' {
  return edition === 'enterprise' ? 'default' : 'secondary'
}

// quotaDisplay — 0(무제한 sentinel)일 때 'unlimited' 토큰 반환, 그 외 숫자 문자열.
//   UI는 'unlimited' 토큰을 dict 키 lookup해 표기.
export function quotaDisplay(n: number): string {
  if (n === 0) return 'unlimited'
  return String(n)
}

export function featureLabelKey(
  feature: 'sso' | 'mt' | 'webhook' | 'cloud' | 'ha',
): DictKey {
  switch (feature) {
    case 'sso':
      return 'license.page.feature.sso'
    case 'mt':
      return 'license.page.feature.mt'
    case 'webhook':
      return 'license.page.feature.webhook'
    case 'cloud':
      return 'license.page.feature.cloud'
    case 'ha':
      return 'license.page.feature.ha'
  }
}

export type ExpiresInfo =
  | { kind: 'future'; days: number }
  | { kind: 'past'; days: number }
  | { kind: 'none' }

// formatExpiresIn — ISO 만료 시각을 now 기준 상대 정보로 변환.
//   - 미래(>= now) → 'future' + 남은 일(소수점 버림, 1일 미만은 0)
//   - 과거(< now) → 'past' + 경과 일(올림 안 함, Math.floor — 1d 이상부터 1)
//   - 빈/잘못된 값 → 'none'
export function formatExpiresIn(
  iso: string | undefined | null,
  now: Date = new Date(),
): ExpiresInfo {
  if (!iso) return { kind: 'none' }
  const parsed = Date.parse(iso)
  if (Number.isNaN(parsed)) return { kind: 'none' }
  const diffMs = parsed - now.getTime()
  const dayMs = 24 * 60 * 60 * 1000
  if (diffMs >= 0) {
    return { kind: 'future', days: Math.floor(diffMs / dayMs) }
  }
  return { kind: 'past', days: Math.floor(-diffMs / dayMs) }
}

// formatExpiresInLabel — ExpiresInfo + t() → UI 친화 텍스트.
//   helper 자체는 i18n 의존이라 단위 테스트 대상은 아님 (formatExpiresIn만 검증).
function formatExpiresInLabel(
  info: ExpiresInfo,
  t: (key: DictKey, vars?: Record<string, string | number>) => string,
): string {
  if (info.kind === 'none') return ''
  if (info.kind === 'future') {
    if (info.days <= 0) return t('license.page.expiresIn.future.today')
    return t('license.page.expiresIn.future', { days: info.days })
  }
  return t('license.page.expiresIn.past', { days: info.days })
}

export const Route = createFileRoute('/_authenticated/license')({
  component: LicensePage,
})
