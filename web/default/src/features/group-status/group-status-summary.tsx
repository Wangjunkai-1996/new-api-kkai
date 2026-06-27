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

import { Activity, Gauge, Layers3, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { Card, CardContent } from '@/components/ui/card'
import { formatNumber, formatPercent } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { GroupStatusEntry } from './types'
import {
  getBestGroup,
  getConfidenceStatus,
  getExperienceLabel,
  getRecommendationLevel,
  shouldShowExperience,
} from './status-display'
import {
  CONFIDENCE_META,
  EXPERIENCE_META,
  RECOMMENDATION_META,
} from './status-meta'

type SummaryStats = {
  brightCount: number
  stableCount: number
  totalRequests: number
  availableModels: number
}

export function GroupStatusSummary(props: {
  groups: GroupStatusEntry[]
  windowHours?: number
}) {
  const { t } = useTranslation()
  const bestGroup = getBestGroup(props.groups)
  const stats = buildSummaryStats(props.groups)
  const bestMeta = bestGroup
    ? CONFIDENCE_META[getConfidenceStatus(bestGroup)]
    : CONFIDENCE_META.unknown
  const BestIcon = bestMeta.icon

  return (
    <div className='space-y-3'>
      <div className='relative overflow-hidden rounded-xl border border-emerald-500/20 bg-[radial-gradient(circle_at_top_left,color-mix(in_oklch,var(--success)_24%,transparent),transparent_36%),linear-gradient(135deg,color-mix(in_oklch,var(--success)_12%,var(--card)),var(--card)_52%,color-mix(in_oklch,var(--info)_10%,var(--card)))] p-4 shadow-sm sm:p-5'>
        <div className='pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-emerald-300/70 to-transparent' />
        <div className='flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between'>
          <div className='min-w-0 space-y-3'>
            <div className='flex flex-wrap items-center gap-2'>
              <StatusBadge
                copyable={false}
                icon={Sparkles}
                label={t('Confidence Panel')}
                variant='light-green'
              />
              {bestGroup && shouldShowExperience(bestGroup) && (
                <StatusBadge
                  copyable={false}
                  icon={EXPERIENCE_META[getExperienceLabel(bestGroup)].icon}
                  label={t(EXPERIENCE_META[getExperienceLabel(bestGroup)].labelKey)}
                  variant={EXPERIENCE_META[getExperienceLabel(bestGroup)].variant}
                />
              )}
            </div>
            <div className='space-y-1'>
              <h2 className='text-2xl font-semibold tracking-normal sm:text-3xl'>
                {bestGroup
                  ? t('Current top pick: {{group}}', { group: bestGroup.group })
                  : t('Group Confidence')}
              </h2>
              <p className='text-muted-foreground max-w-2xl text-sm'>
                {t(
                  'Calculated from recent success rate and routable model coverage. Latency is not used to downgrade high-success groups.'
                )}
              </p>
            </div>
          </div>
          {bestGroup && (
            <div className='flex min-w-0 items-center gap-3 rounded-lg border border-emerald-400/20 bg-background/55 p-3 shadow-sm backdrop-blur'>
              <div
                className={cn(
                  'flex size-11 shrink-0 items-center justify-center rounded-lg',
                  'bg-emerald-400/15 text-emerald-500 dark:text-emerald-300'
                )}
              >
                <BestIcon className='size-5' />
              </div>
              <div className='min-w-0'>
                <div className={cn('text-xl font-semibold', bestMeta.toneClass)}>
                  {formatPercent(bestGroup.success_rate)}
                </div>
                <div className='text-muted-foreground truncate text-xs'>
                  {t(bestMeta.labelKey)} ·{' '}
                  {t(
                    RECOMMENDATION_META[getRecommendationLevel(bestGroup)]
                      .labelKey
                  )}
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
      <div className='grid grid-cols-2 gap-2 lg:grid-cols-4'>
        <MetricCard
          icon={Sparkles}
          label={t('Smooth Groups')}
          value={formatNumber(stats.brightCount)}
          detail={t('Very smooth or smooth')}
          accent='emerald'
        />
        <MetricCard
          icon={Gauge}
          label={t('Stable Groups')}
          value={formatNumber(stats.stableCount)}
          detail={t('Stable and usable')}
          accent='green'
        />
        <MetricCard
          icon={Layers3}
          label={t('Routable Models')}
          value={formatNumber(stats.availableModels)}
          detail={t('Available model coverage')}
          accent='teal'
        />
        <MetricCard
          icon={Activity}
          label={t('Recent Samples')}
          value={formatNumber(stats.totalRequests)}
          detail={t('{{hours}}h window', { hours: props.windowHours ?? 6 })}
          accent='cyan'
        />
      </div>
    </div>
  )
}

function MetricCard(props: {
  icon: typeof Sparkles
  label: string
  value: string
  detail: string
  accent: 'emerald' | 'green' | 'teal' | 'cyan'
}) {
  const Icon = props.icon
  const accentClass = {
    emerald: 'text-emerald-500 bg-emerald-400/12',
    green: 'text-success bg-success/10',
    teal: 'text-teal-500 bg-teal-400/12',
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
        stats.brightCount += 1
      }
      if (getConfidenceStatus(group) === 'stable') {
        stats.stableCount += 1
      }
      stats.totalRequests += group.request_count
      stats.availableModels += group.available_model_count
      return stats
    },
    {
      brightCount: 0,
      stableCount: 0,
      totalRequests: 0,
      availableModels: 0,
    }
  )
}
