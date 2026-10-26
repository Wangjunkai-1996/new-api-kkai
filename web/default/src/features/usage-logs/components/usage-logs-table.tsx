/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'
import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { getMembershipSelf } from '@/features/membership/api'
import { useSiteConfig } from '@/features/site-config'
import {
  getAdminPlans,
  getSelfSubscriptionFull,
} from '@/features/subscriptions/api'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  DEFAULT_LOGS_DATA,
  LOG_TYPE_ALL_VALUE,
  LOG_TYPE_ENUM,
} from '../constants'
import { shouldShowBillingSource } from '../lib/billing-source'
import { useColumnsByCategory } from '../lib/columns'
import { parseLogOther } from '../lib/format'
import { fetchLogsByCategory } from '../lib/utils'
import type { LogCategory } from '../types'
import { CommonLogsFilterBar } from './common-logs-filter-bar'
import { TaskLogsFilterBar } from './task-logs-filter-bar'
import { UsageLogsMobileList } from './usage-logs-mobile-card'
import { useLogsViewScope, type LogsViewAccess } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const logTypeRowTint: Record<number, string> = {
  [LOG_TYPE_ENUM.ERROR]: 'bg-rose-50/40 dark:bg-rose-950/20',
  [LOG_TYPE_ENUM.REFUND]: 'bg-blue-50/30 dark:bg-blue-950/15',
}

// Warning tint for logs where a quota conversion saturated (admin-only marker).
// Takes precedence over the per-type tint since it flags a billing anomaly.
const quotaSaturationRowTint = 'bg-amber-50/60 dark:bg-amber-950/25'

function getColumnVisibilityStorageKey(
  logCategory: LogCategory,
  viewAccess: LogsViewAccess
): string {
  return `usage-logs:${logCategory}:${viewAccess}:column-visibility`
}

function deserializeLogTypeFilter(value: unknown): unknown[] {
  let values: unknown[] = []
  if (Array.isArray(value)) {
    values = value
  } else if (value) {
    values = [value]
  }
  return values.filter((item) => String(item) !== LOG_TYPE_ALL_VALUE)
}

interface UsageLogsTableProps {
  logCategory: LogCategory
}

export function UsageLogsTable({ logCategory }: UsageLogsTableProps) {
  const { t } = useTranslation()
  const getColumnClassName = useCallback(
    () => (logCategory === 'common' ? 'py-2' : 'py-3.5'),
    [logCategory]
  )
  const {
    isAdminView: isAdmin,
    isRootView: isRoot,
    viewAccess,
  } = useLogsViewScope()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const searchParams = route.useSearch()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const { data: site } = useSiteConfig()
  const platformSite = site?.tenant_id === 0
  const { data: billingSourceAvailable = false } = useQuery({
    queryKey: ['usage-log-billing-source', site?.tenant_id, isAdmin, userId],
    enabled: platformSite && logCategory === 'common' && userId != null,
    queryFn: async () => {
      if (isAdmin) {
        const plansResult = await getAdminPlans()
        return shouldShowBillingSource({
          isAdmin,
          plans: plansResult.success ? plansResult.data : undefined,
          subscriptions: undefined,
        })
      }

      const selfResult = await getSelfSubscriptionFull()
      return shouldShowBillingSource({
        isAdmin,
        plans: undefined,
        subscriptions: selfResult.success
          ? selfResult.data?.subscriptions
          : undefined,
      })
    },
  })
  const showBillingSource = platformSite && billingSourceAvailable
  const { data: currentMembership } = useQuery({
    queryKey: ['membership', userId, 'self'],
    enabled:
      platformSite && !isAdmin && logCategory === 'common' && userId != null,
    queryFn: getMembershipSelf,
    staleTime: 15_000,
    refetchInterval: 60_000,
    retry: false,
  })
  const currentTier =
    platformSite &&
    !isAdmin &&
    logCategory === 'common' &&
    currentMembership != null &&
    currentMembership.runtime_enabled !== false &&
    currentMembership.account?.status === 'active' &&
    currentMembership.account.expires_at > currentMembership.server_time
      ? currentMembership.tier
      : null

  const {
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 20 : 100 },
    globalFilter: { enabled: false },
    columnFilters: [
      {
        columnId: 'created_at',
        searchKey: 'type',
        type: 'array' as const,
        deserialize: deserializeLogTypeFilter,
      },
      { columnId: 'model_name', searchKey: 'model', type: 'string' as const },
      { columnId: 'token_name', searchKey: 'token', type: 'string' as const },
      { columnId: 'group', searchKey: 'group', type: 'string' as const },
      ...(isAdmin
        ? [
            {
              columnId: 'channel',
              searchKey: 'channel',
              type: 'string' as const,
            },
            {
              columnId: 'username',
              searchKey: 'username',
              type: 'string' as const,
            },
          ]
        : []),
    ],
  })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'logs',
      logCategory,
      viewAccess,
      pagination.pageIndex + 1,
      pagination.pageSize,
      columnFilters,
      searchParams,
      t,
    ],
    queryFn: async () => {
      const result = await fetchLogsByCategory({
        logCategory,
        isAdmin,
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        searchParams,
        columnFilters,
      })

      if (!result?.success) {
        throw createServerError(result, t('Failed to load logs'))
      }
      if (
        searchParams.billing_source &&
        result.data?.billing_source !== searchParams.billing_source
      ) {
        throw createServerError(
          undefined,
          t('Funding source filtering is temporarily unavailable.')
        )
      }

      return result.data || DEFAULT_LOGS_DATA
    },
    placeholderData: (previousData, previousQuery) => {
      if (
        previousQuery?.queryKey[1] === logCategory &&
        previousQuery.queryKey[2] === viewAccess &&
        (previousQuery.queryKey[6] as typeof searchParams)?.billing_source ===
          searchParams.billing_source
      ) {
        return previousData
      }
      return undefined
    },
  })

  const logs = data?.items || []
  const columns = useColumnsByCategory(
    logCategory,
    isAdmin,
    isRoot,
    showBillingSource
  )
  const isLoadingData = isLoading || (isFetching && !data)

  const { table } = useDataTable({
    data: logs as Record<string, unknown>[],
    columns: columns as ColumnDef<Record<string, unknown>>[],
    columnFilters,
    columnVisibilityStorageKey: getColumnVisibilityStorageKey(
      logCategory,
      viewAccess
    ),
    pagination,
    enableRowSelection: false,
    onPaginationChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  const isCommon = logCategory === 'common'

  return (
    <>
      {currentTier && (
        <div
          className={cn(
            'border-border/70 bg-muted/20 mb-3 flex flex-wrap items-center justify-between gap-3 rounded-lg border px-3 py-2.5 text-sm',
            {
              'border-amber-200/70 bg-amber-50/50 dark:border-amber-900/70 dark:bg-amber-950/20':
                currentTier.style === 'gold',
              'border-cyan-200/70 bg-cyan-50/50 dark:border-cyan-900/70 dark:bg-cyan-950/20':
                currentTier.style === 'diamond',
              'border-fuchsia-200/70 bg-fuchsia-50/50 dark:border-fuchsia-900/70 dark:bg-fuchsia-950/20':
                currentTier.style === 'king',
            }
          )}
        >
          <div className='min-w-0 space-y-1'>
            <div className='flex flex-wrap items-center gap-x-2 gap-y-1'>
              <span className='text-muted-foreground text-xs'>
                {t('Current membership')}
              </span>
              <strong className='font-semibold break-words'>
                {currentTier.name}
              </strong>
              <span className='text-xs text-emerald-700 dark:text-emerald-400'>
                {t('Your membership benefits are active.')}
              </span>
            </div>
            <p className='text-muted-foreground text-xs leading-relaxed'>
              {t(
                'Logs retain the membership tier and funding source used at the time of each request.'
              )}
            </p>
          </div>
          <Link
            to='/membership'
            className='text-primary text-xs font-medium hover:underline'
          >
            {t('View membership')}
          </Link>
        </div>
      )}
      <DataTablePage
        table={table}
        compactPagination={isMobile && isCommon}
        columns={columns as ColumnDef<Record<string, unknown>>[]}
        isLoading={isLoadingData}
        isFetching={isFetching}
        emptyTitle={t('No Logs Found')}
        emptyDescription={t(
          'No usage logs available. Logs will appear here once API calls are made.'
        )}
        skeletonKeyPrefix='usage-log-skeleton'
        applyHeaderSize
        tableClassName={cn(
          '[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
        )}
        mobile={
          <UsageLogsMobileList
            table={table}
            isLoading={isLoadingData}
            logCategory={logCategory}
          />
        }
        toolbar={
          isCommon ? (
            <CommonLogsFilterBar table={table} />
          ) : (
            <TaskLogsFilterBar table={table} logCategory={logCategory} />
          )
        }
        renderRow={(row) => {
          const logType = (row.original as Record<string, unknown>).type as
            | number
            | undefined
          let tintClass =
            isCommon && logType != null ? (logTypeRowTint[logType] ?? '') : ''
          if (isCommon && isAdmin) {
            const other = parseLogOther(
              ((row.original as Record<string, unknown>).other as string) ?? ''
            )
            if (other?.admin_info?.quota_saturation) {
              tintClass = quotaSaturationRowTint
            }
          }

          return (
            <DataTableRow
              key={row.id}
              row={row}
              className={cn('transition-colors', tintClass)}
              getColumnClassName={getColumnClassName}
              cellRenderColumns={table.options.columns}
            />
          )
        }}
      />
      {isCommon && platformSite && (
        <p className='text-muted-foreground mt-3 px-1 text-xs leading-relaxed'>
          {t(
            'Badges show the membership used for each request. Wallet payments retain existing prices without an extra membership discount.'
          )}
        </p>
      )}
    </>
  )
}
