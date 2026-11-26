/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'
import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { groupStatusQueryOptions } from '@/features/group-status/api'
import { formatGroupSuccessRate } from '@/features/group-status/format'
import { getGroupSignalEvents } from '@/features/group-status/signal'
import { getGroupStatusLabel } from '@/features/group-status/status'
import type {
  GroupStatusEntry,
  GroupStatusResult,
} from '@/features/group-status/types'
import { useSiteConfig } from '@/features/site-config'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

const SIGNAL_POSITIONS = Array.from({ length: 16 }, (_, position) => position)
type GroupSignals =
  | ReadonlyMap<string, GroupStatusEntry>
  | 'loading'
  | 'error'
  | null
const GroupSignalContext = createContext<GroupSignals>(null)

function indexLiveGroups(result: GroupStatusResult) {
  if (result.window !== 'now' || result.window_minutes !== 5) {
    return 'error' as const
  }
  return new Map(result.groups.map((group) => [group.group, group]))
}

export function ApiKeyGroupSignalProvider({
  children,
}: {
  children: ReactNode
}) {
  const site = useSiteConfig()
  const platform = site.data?.tenant_id === 0
  const query = useQuery({
    ...groupStatusQueryOptions('now'),
    enabled: platform,
    retry: false,
    meta: { errorToast: false },
    select: indexLiveGroups,
  })

  // /api/status/groups is outside the reseller API allowlist.
  let value: GroupSignals = null
  if (platform) {
    value =
      query.isError || query.isPaused ? 'error' : (query.data ?? 'loading')
  }
  return <GroupSignalContext value={value}>{children}</GroupSignalContext>
}

export function ApiKeyGroupSignal(props: {
  group: string
  variant?: 'trigger' | 'row'
}) {
  const { t, i18n } = useTranslation()
  const groups = useContext(GroupSignalContext)
  const group =
    groups && typeof groups !== 'string' ? groups.get(props.group) : undefined
  const events = useMemo(
    () =>
      getGroupSignalEvents(group?.recent_events ?? null).slice(
        -SIGNAL_POSITIONS.length
      ),
    [group?.recent_events]
  )
  if (!groups) return null

  let label = t('Unknown')
  let tone = 'neutral'
  let rate = '—'
  let sample = ''
  if (groups === 'loading') {
    label = t('Loading')
  } else if (groups === 'error') {
    label = t('Status unavailable')
  } else if (group?.stale) {
    label = t('Stale')
  } else if (group?.request_count === 0) {
    label = t('Awaiting samples')
  } else if (
    group &&
    group.request_count > 0 &&
    Number.isFinite(group.success_rate) &&
    group.success_rate >= 0 &&
    group.success_rate <= 100 &&
    group.confidence_status !== 'unknown'
  ) {
    label = t(getGroupStatusLabel(group))
    tone = 'healthy'
    if (group.confidence_status === 'unstable') tone = 'warning'
    if (group.confidence_status === 'unavailable') tone = 'danger'
    rate = formatGroupSuccessRate(group)
    sample = t('Last 5 min · Requests: {{requests}}', {
      requests: formatNumber(group.request_count, toIntlLocale(i18n.language)),
    })
  }

  const successful = events.filter((event) => event.status === 'success').length
  const summary = [
    label,
    ...(sample ? [sample, `${t('Success rate')} ${rate}`] : []),
    ...(events.length
      ? [
          t('{{successful}} successful and {{failed}} failed requests', {
            successful,
            failed: events.length - successful,
          }),
        ]
      : []),
  ].join(' · ')
  const emptyPositions = SIGNAL_POSITIONS.length - events.length
  const variant = props.variant ?? 'row'

  return (
    <span
      className='kgrp-signal'
      data-variant={variant}
      data-tone={tone}
      role='img'
      aria-label={summary}
      title={summary}
    >
      <span className='kgrp-ticks' aria-hidden='true'>
        {SIGNAL_POSITIONS.map((position) => (
          <span
            key={position}
            className='kgrp-tick'
            data-result={events[position - emptyPositions]?.status ?? 'none'}
          />
        ))}
      </span>
      <span className='kgrp-rate' aria-hidden='true'>
        {rate}
      </span>
      {variant === 'trigger' ? (
        <span className='kgrp-samples' aria-hidden='true'>
          {sample || label}
        </span>
      ) : (
        <span className='kgrp-state' aria-hidden='true'>
          {label}
        </span>
      )}
    </span>
  )
}
