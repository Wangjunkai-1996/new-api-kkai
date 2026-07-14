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

import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CircleHelp,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { formatNumber, formatTimestampRelative } from '@/lib/format'

import type { GroupStatusEntry, GroupStatusResult } from '../types'

export function GroupStatusSummary(props: {
  groups: GroupStatusEntry[]
  result: GroupStatusResult
}) {
  const { t } = useTranslation()
  const stats = summarizeGroups(props.groups)
  const source = sourceStatus(props.result)

  return (
    <section className='bg-muted/15 border-y' aria-label={t('Status summary')}>
      <dl className='grid grid-cols-2 divide-x divide-y sm:grid-cols-4 sm:divide-y-0'>
        <SummaryMetric
          icon={CheckCircle2}
          label={t('Healthy')}
          value={stats.healthy}
          toneClass='text-success'
        />
        <SummaryMetric
          icon={AlertTriangle}
          label={t('Attention')}
          value={stats.attention}
          toneClass='text-warning'
        />
        <SummaryMetric
          icon={CircleHelp}
          label={t('Unknown')}
          value={stats.unknown}
          toneClass='text-muted-foreground'
        />
        <SummaryMetric
          icon={Activity}
          label={t('Requests')}
          value={stats.requests}
          toneClass='text-foreground'
        />
      </dl>
      <div className='flex flex-wrap items-center justify-between gap-2 border-t px-3 py-2 text-xs'>
        <StatusBadge
          copyable={false}
          label={t(source.labelKey)}
          variant={source.variant}
          title={props.result.data_source}
        />
        <span className='text-muted-foreground tabular-nums'>
          {t('Updated {{time}}', {
            time: formatTimestampRelative(props.result.generated_at),
          })}
        </span>
      </div>
    </section>
  )
}

function SummaryMetric(props: {
  icon: LucideIcon
  label: string
  value: number
  toneClass: string
}) {
  const Icon = props.icon
  return (
    <div className='flex min-h-20 items-center gap-3 px-3 py-3'>
      <Icon
        className={`size-4 shrink-0 ${props.toneClass}`}
        aria-hidden='true'
      />
      <div className='min-w-0'>
        <dt className='text-muted-foreground truncate text-xs'>
          {props.label}
        </dt>
        <dd className='mt-0.5 text-lg font-semibold tabular-nums'>
          {formatNumber(props.value)}
        </dd>
      </div>
    </div>
  )
}

function summarizeGroups(groups: GroupStatusEntry[]) {
  return groups.reduce(
    (stats, group) => {
      if (
        group.confidence_status === 'excellent' ||
        group.confidence_status === 'smooth' ||
        group.confidence_status === 'stable'
      ) {
        stats.healthy += 1
      } else if (
        group.confidence_status === 'unstable' ||
        group.confidence_status === 'unavailable'
      ) {
        stats.attention += 1
      } else {
        stats.unknown += 1
      }
      stats.requests += group.request_count
      return stats
    },
    { healthy: 0, attention: 0, unknown: 0, requests: 0 }
  )
}

function sourceStatus(result: GroupStatusResult) {
  if (result.data_source === 'none') {
    return { labelKey: 'Awaiting traffic', variant: 'neutral' as const }
  }
  if (!result.redis_available) {
    return { labelKey: 'Fallback mode', variant: 'warning' as const }
  }
  if (result.data_source.startsWith('database')) {
    return { labelKey: 'Database + live', variant: 'info' as const }
  }
  return { labelKey: 'Live', variant: 'success' as const }
}
