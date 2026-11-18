import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { lazy, Suspense, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { getUserQuotaDates } from '@/features/dashboard/api'
import {
  OVERVIEW_CHART_PREFERENCES,
  TIME_GRANULARITY_OPTIONS,
  TIME_RANGE_PRESETS,
} from '@/features/dashboard/constants'
import {
  buildDefaultDashboardFilters,
  buildQueryParams,
  resolveDashboardTimeRange,
} from '@/features/dashboard/lib/filters'
import { calculateDashboardStats } from '@/features/dashboard/lib/stats'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatQuota } from '@/lib/format'
import { privateUserQueryKey } from '@/lib/private-query-cache'
import { requireServerSuccess } from '@/lib/server-error-message'

import { ModelsFilter } from '../models/models-filter-dialog'
import { PanelWrapper } from '../ui/panel-wrapper'

const UsageTrendCharts = lazy(() => import('./usage-trend-charts'))

export function UsageTrendPanel(props: {
  userId: number
  role: number
  host: string
  isAdmin: boolean
}) {
  const { t, i18n } = useTranslation()
  const [filters, setFilters] = useState(() =>
    buildDefaultDashboardFilters(OVERVIEW_CHART_PREFERENCES)
  )
  const presetDays = filters.presetDays
  const granularity =
    filters.time_granularity ??
    OVERVIEW_CHART_PREFERENCES.defaultTimeGranularity
  const username = props.isAdmin ? filters.username?.trim() : undefined
  const rangeSelection = useMemo(
    () => presetDays ?? resolveDashboardTimeRange(filters),
    [filters, presetDays]
  )
  const query = useQuery({
    queryKey: privateUserQueryKey(
      props.userId,
      props.host,
      'dashboard',
      'overview',
      'usage-trend',
      props.role,
      props.isAdmin,
      username || null,
      rangeSelection
    ),
    queryFn: async ({ signal }) => {
      const range =
        typeof rangeSelection === 'number'
          ? resolveDashboardTimeRange({ presetDays: rangeSelection })
          : rangeSelection
      const response = requireServerSuccess(
        await getUserQuotaDates(
          buildQueryParams(range, { username }),
          props.isAdmin,
          signal
        )
      )
      const rows = (response.data ?? []).filter(
        ({ created_at }) =>
          Number.isSafeInteger(created_at) &&
          created_at > 0 &&
          created_at >= range.start_timestamp &&
          created_at <= range.end_timestamp
      )
      return { rows, range, stats: calculateDashboardStats(rows) }
    },
    staleTime: 60_000,
    retry: false,
  })
  const snapshot = query.data
  const currentFilters = useMemo(() => {
    if (presetDays === undefined || !snapshot) return filters
    return {
      ...filters,
      start_timestamp: new Date(snapshot.range.start_timestamp * 1000),
      end_timestamp: new Date(snapshot.range.end_timestamp * 1000),
    }
  }, [filters, presetDays, snapshot])
  const rangeLabel = TIME_RANGE_PRESETS.find(
    (preset) => preset.days === presetDays
  )?.label
  const granularityLabel =
    TIME_GRANULARITY_OPTIONS.find((option) => option.value === granularity)
      ?.label ?? TIME_GRANULARITY_OPTIONS[0].label
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const loading = (
    <Skeleton role='status' aria-label={t('Loading')} className='h-64 w-full' />
  )

  return (
    <section aria-label={t('Model Call Analytics')} className='min-w-0'>
      <PanelWrapper
        title={<h2>{t('Model Call Analytics')}</h2>}
        description={
          <span className='break-words'>
            {rangeLabel ? t(rangeLabel) : t('Custom Time Range')} ·{' '}
            {t(granularityLabel)}
            {username && ` · ${username}`}
          </span>
        }
        headerActions={
          <div className='flex flex-wrap justify-end gap-1.5'>
            <ModelsFilter
              preferences={OVERVIEW_CHART_PREFERENCES}
              currentFilters={currentFilters}
              allowUsernameFilter={props.isAdmin}
              onFilterChange={setFilters}
              onReset={() =>
                setFilters(
                  buildDefaultDashboardFilters(OVERVIEW_CHART_PREFERENCES)
                )
              }
              titleKey='Model Analytics Filters'
              descriptionKey='Filter the model analytics view by time range and user.'
            />
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              <RefreshCw aria-hidden='true' data-icon='inline-start' />
              {query.isRefetching ? t('Refreshing...') : t('Refresh')}
            </Button>
          </div>
        }
        contentClassName='grid min-w-0 gap-4'
      >
        {query.isPending && loading}
        {query.isError && (
          <div
            role='alert'
            className='flex flex-wrap items-center gap-2 text-sm'
          >
            {snapshot
              ? t('Refresh failed. Showing the latest available data.')
              : t('Failed to load usage statistics')}
            <Button
              type='button'
              size='sm'
              variant='outline'
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              {t('Retry')}
            </Button>
          </div>
        )}
        {snapshot && (
          <>
            <dl className='grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-3'>
              {[
                [
                  t('Request Count'),
                  formatNumber(snapshot.stats.totalCount, locale),
                ],
                [t('Tokens'), formatNumber(snapshot.stats.totalTokens, locale)],
                [t('Quota'), formatQuota(snapshot.stats.totalQuota)],
              ].map(([label, value]) => (
                <div
                  key={label}
                  className='bg-muted/40 min-w-0 rounded-lg px-3 py-2'
                >
                  <dt className='text-muted-foreground text-xs'>{label}</dt>
                  <dd className='text-lg font-semibold wrap-anywhere tabular-nums'>
                    {value}
                  </dd>
                </div>
              ))}
            </dl>
            {snapshot.rows.length ? (
              <Suspense fallback={loading}>
                <UsageTrendCharts
                  rows={snapshot.rows}
                  range={snapshot.range}
                  granularity={granularity}
                />
              </Suspense>
            ) : (
              <p
                role='status'
                className='text-muted-foreground py-6 text-center text-sm'
              >
                {t('No data available')}
              </p>
            )}
          </>
        )}
      </PanelWrapper>
    </section>
  )
}
