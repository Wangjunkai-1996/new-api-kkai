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

import { Activity, Radio, Zap, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { Card, CardContent } from '@/components/ui/card'
import { formatNumber, formatPercent } from '@/lib/format'
import { cn } from '@/lib/utils'
import {
  getConfidenceStatus,
  getExperienceLabel,
  getMessageKey,
  getRecommendationLevel,
  shouldShowExperience,
} from './status-display'
import {
  CONFIDENCE_META,
  EXPERIENCE_META,
  RECOMMENDATION_META,
} from './status-meta'
import type { GroupRecentEvent, GroupStatusEntry } from './types'

const PULSE_BAR_COUNT = 60

export function GroupStatusCards({
  groups,
  generatedAt,
}: {
  groups: GroupStatusEntry[]
  generatedAt?: number
}) {
  return (
    <div className='grid gap-3 xl:grid-cols-2'>
      {groups.map((group) => (
        <GroupStatusCard
          key={group.group}
          group={group}
          generatedAt={generatedAt}
        />
      ))}
    </div>
  )
}

function GroupStatusCard({
  group,
  generatedAt,
}: {
  group: GroupStatusEntry
  generatedAt?: number
}) {
  const { t } = useTranslation()
  const confidenceMeta = CONFIDENCE_META[getConfidenceStatus(group)]
  const recommendationMeta = RECOMMENDATION_META[getRecommendationLevel(group)]
  const experienceMeta = EXPERIENCE_META[getExperienceLabel(group)]
  const ConfidenceIcon = confidenceMeta.icon
  const ExperienceIcon = shouldShowExperience(group) ? experienceMeta.icon : Zap
  const events = normalizeEvents(group.recent_events ?? [])

  return (
    <Card
      className={cn(
        'relative overflow-hidden rounded-xl border bg-card/95 shadow-sm',
        'border-emerald-500/15 dark:bg-slate-950/60'
      )}
    >
      <div
        className={cn(
          'pointer-events-none absolute inset-y-0 left-0 w-1',
          confidenceMeta.barClass
        )}
      />
      <div className='pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-emerald-300/60 to-transparent' />
      <CardContent className='space-y-5 p-4 sm:p-5'>
        <div className='flex min-w-0 items-start justify-between gap-3'>
          <div className='flex min-w-0 items-start gap-3'>
            <div
              className={cn(
                'flex size-12 shrink-0 items-center justify-center rounded-xl',
                'bg-emerald-400/12 text-emerald-500 dark:text-emerald-300'
              )}
            >
              <ConfidenceIcon className='size-5' />
            </div>
            <div className='min-w-0 space-y-1'>
              <div className='truncate text-lg font-semibold sm:text-xl'>
                {group.group}
              </div>
              <div className='flex min-w-0 flex-wrap items-center gap-2'>
                <StatusBadge
                  copyable={false}
                  label={t(confidenceMeta.labelKey)}
                  variant={confidenceMeta.badgeVariant}
                  size='lg'
                />
                <span className='text-muted-foreground min-w-0 truncate text-sm'>
                  {group.desc || t('User group')}
                </span>
              </div>
            </div>
          </div>
          <StatusBadge
            copyable={false}
            label={t(recommendationMeta.labelKey)}
            variant={recommendationMeta.variant}
            size='lg'
          />
        </div>

        <div className='grid grid-cols-2 gap-3'>
          <SignalMetric
            icon={Activity}
            label={t('Success Rate')}
            value={formatSuccessRate(group)}
            valueClassName={confidenceMeta.toneClass}
          />
          <SignalMetric
            icon={ExperienceIcon}
            label={t('Feel')}
            value={
              shouldShowExperience(group)
                ? t(experienceMeta.labelKey)
                : t('Awaiting signal')
            }
            valueClassName={
              shouldShowExperience(group)
                ? 'text-teal-500 dark:text-teal-300'
                : 'text-muted-foreground'
            }
            title={
              group.avg_ttft_ms > 0
                ? t('TTFT about {{time}}', {
                    time: formatDuration(group.avg_ttft_ms),
                  })
                : undefined
            }
          />
        </div>

        <div className='grid grid-cols-2 gap-x-4 gap-y-2 text-sm sm:grid-cols-3'>
          <InlineMetric
            label={t('Samples')}
            value={formatNumber(group.request_count)}
          />
          <InlineMetric
            label={t('Routable Models')}
            value={formatNumber(group.available_model_count)}
          />
          <InlineMetric
            label={t('Refreshed')}
            value={
              generatedAt
                ? t('{{seconds}}s ago', { seconds: ageSeconds(generatedAt) })
                : '-'
            }
          />
        </div>

        <div className='space-y-2 border-t pt-4'>
          <div className='flex items-center justify-between gap-3'>
            <div className='flex min-w-0 items-center gap-2 text-sm font-medium'>
              <Radio className='text-muted-foreground size-4 shrink-0' />
              <span>{t('Recent 60 Signals')}</span>
            </div>
            <span className='text-muted-foreground shrink-0 text-xs'>
              {events.length > 0
                ? t('Latest {{count}} of 60', { count: events.length })
                : t('Waiting for traffic')}
            </span>
          </div>
          <PulseBars events={events} />
          <p className='text-muted-foreground line-clamp-2 text-sm'>
            {t(getMessageKey(group))}
          </p>
        </div>
      </CardContent>
    </Card>
  )
}

function SignalMetric(props: {
  icon: LucideIcon
  label: string
  value: string
  valueClassName?: string
  title?: string
}) {
  const Icon = props.icon
  return (
    <div className='rounded-xl border bg-background/50 p-3' title={props.title}>
      <div className='text-muted-foreground flex items-center gap-2 text-xs font-medium'>
        <Icon className='size-3.5' />
        <span>{props.label}</span>
      </div>
      <div
        className={cn(
          'mt-2 truncate text-2xl font-semibold tabular-nums sm:text-3xl',
          props.valueClassName
        )}
      >
        {props.value}
      </div>
    </div>
  )
}

function InlineMetric(props: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='truncate font-medium tabular-nums'>{props.value}</div>
    </div>
  )
}

function PulseBars({ events }: { events: GroupRecentEvent[] }) {
  const { t } = useTranslation()
  const visibleEvents = events.slice(-PULSE_BAR_COUNT)

  if (visibleEvents.length === 0) {
    return (
      <div
        className='bg-muted/15 text-muted-foreground flex h-10 items-center rounded-lg px-3 text-xs'
        aria-label='recent group request signals'
      >
        {t('No recent request signals yet')}
      </div>
    )
  }

  return (
    <div
      className='bg-muted/15 grid h-10 items-end gap-0.5 rounded-lg p-1 sm:gap-1'
      style={{
        gridTemplateColumns: `repeat(${PULSE_BAR_COUNT}, minmax(0, 1fr))`,
      }}
      aria-label='recent group request signals'
    >
      {visibleEvents.map((event, index) => (
        <span
          key={`${event.ts}-${index}`}
          style={{
            gridColumnStart:
              PULSE_BAR_COUNT - visibleEvents.length + index + 1,
          }}
          className={cn(
            'rounded-full transition-transform hover:scale-y-110',
            event.status === 'success'
              ? 'h-9 bg-emerald-400 shadow-[0_0_10px_rgba(52,211,153,0.35)]'
              : 'h-6 bg-destructive/80'
          )}
          title={event.status}
        />
      ))}
    </div>
  )
}

function normalizeEvents(events: GroupRecentEvent[]) {
  return events
    .filter((event) => event.status === 'success' || event.status === 'failure')
    .sort((left, right) => left.ts - right.ts)
    .slice(-PULSE_BAR_COUNT)
}

function formatSuccessRate(group: GroupStatusEntry): string {
  if (group.request_count <= 0) return '-'
  return formatPercent(group.success_rate)
}

function formatDuration(value: number): string {
  if (!value) return '-'
  if (value >= 1000) {
    return `${formatNumber(value / 1000)}s`
  }
  return `${formatNumber(value)}ms`
}

function ageSeconds(timestamp: number) {
  return Math.max(0, Math.floor(Date.now() / 1000 - timestamp))
}
