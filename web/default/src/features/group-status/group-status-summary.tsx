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
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent } from '@/components/ui/card'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { GroupStatusEntry, GroupStatusWindow } from './types'
import { getConfidenceStatus } from './status-display'

type SummaryStats = {
  healthyCount: number
  stableCount: number
  issueCount: number
  unknownCount: number
  totalRequests: number
}

export function GroupStatusSummary(props: {
  groups: GroupStatusEntry[]
  window?: GroupStatusWindow | '24h'
  windowMinutes?: number
  generatedAt?: number
}) {
  const { t } = useTranslation()
  const stats = buildSummaryStats(props.groups)

  return (
    <div className='space-y-3'>
      <div className='rounded-xl border bg-card p-4 shadow-sm'>
        <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
          <div className='min-w-0'>
            <h2 className='text-lg font-semibold'>{t('Group Health')}</h2>
            <p className='text-muted-foreground mt-1 max-w-2xl text-sm'>
              {t(windowDescriptionKey(props.window))}
            </p>
          </div>
          <div className='text-muted-foreground text-xs tabular-nums'>
            {props.generatedAt
              ? t('{{seconds}}s ago', { seconds: ageSeconds(props.generatedAt) })
              : '-'}
          </div>
        </div>
      </div>
      <div className='grid grid-cols-2 gap-2 lg:grid-cols-4'>
        <MetricCard
          icon={ShieldCheck}
          label={t('Healthy Groups')}
          value={formatNumber(stats.healthyCount)}
          detail={t('Healthy or smooth')}
          accent='emerald'
        />
        <MetricCard
          icon={CheckCircle2}
          label={t('Stable Groups')}
          value={formatNumber(stats.stableCount)}
          detail={t('Stable and usable')}
          accent='green'
        />
        <MetricCard
          icon={AlertTriangle}
          label={t('Problem Groups')}
          value={formatNumber(stats.issueCount)}
          detail={t('Failing or unstable')}
          accent='amber'
        />
        <MetricCard
          icon={Activity}
          label={t('Recent Samples')}
          value={formatNumber(stats.totalRequests)}
          detail={t(windowDetailKey(props.window), {
            minutes: props.windowMinutes ?? 5,
          })}
          accent='cyan'
        />
      </div>
    </div>
  )
}

function windowDescriptionKey(window?: GroupStatusWindow | '24h') {
  if (window === 'now') {
    return 'Realtime status uses recent request signals only. It does not query logs, probe upstream, or consume quota.'
  }
  return 'Calculated from recent request success rate. Latency is shown as experience context, not as a health downgrade.'
}

function windowDetailKey(window?: GroupStatusWindow | '24h') {
  switch (window) {
    case 'now':
      return 'Realtime recent requests'
    case '15m':
      return 'Last {{minutes}} minutes'
    case '1h':
      return 'Last hour'
    case '6h':
      return 'Last six hours'
    default:
      return 'Selected window'
  }
}

function MetricCard(props: {
  icon: typeof Activity
  label: string
  value: string
  detail: string
  accent: 'emerald' | 'green' | 'amber' | 'cyan'
}) {
  const Icon = props.icon
  const accentClass = {
    emerald: 'text-emerald-500 bg-emerald-400/12',
    green: 'text-success bg-success/10',
    amber: 'text-warning bg-warning/10',
    cyan: 'text-cyan-500 bg-cyan-400/12',
  }[props.accent]

  return (
    <Card size='sm' className='rounded-lg'>
      <CardContent className='flex min-h-24 items-start gap-3'>
        <div className={cn('mt-0.5 rounded-md p-1.5', accentClass)}>
          <Icon className='size-4' />
        </div>
        <div className='min-w-0 space-y-1'>
          <div className='text-muted-foreground text-xs font-medium'>
            {props.label}
          </div>
          <div className='truncate text-xl font-semibold'>{props.value}</div>
          <div className='text-muted-foreground line-clamp-2 text-xs'>
            {props.detail}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function buildSummaryStats(groups: GroupStatusEntry[]): SummaryStats {
  return groups.reduce(
    (stats, group) => {
      if (
        getConfidenceStatus(group) === 'excellent' ||
        getConfidenceStatus(group) === 'smooth'
      ) {
        stats.healthyCount += 1
      }
      if (getConfidenceStatus(group) === 'stable') {
        stats.stableCount += 1
      }
      if (
        getConfidenceStatus(group) === 'unstable' ||
        getConfidenceStatus(group) === 'unavailable'
      ) {
        stats.issueCount += 1
      }
      if (getConfidenceStatus(group) === 'unknown') {
        stats.unknownCount += 1
      }
      stats.totalRequests += group.request_count
      return stats
    },
    {
      healthyCount: 0,
      stableCount: 0,
      issueCount: 0,
      unknownCount: 0,
      totalRequests: 0,
    }
  )
}

function ageSeconds(timestamp: number) {
  return Math.max(0, Math.floor(Date.now() / 1000 - timestamp))
}
