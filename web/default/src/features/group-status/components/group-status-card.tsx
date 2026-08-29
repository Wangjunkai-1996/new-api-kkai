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

import { Layers3, Timer, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Card, CardContent } from '@/components/ui/card'
import { formatTimestampRelative } from '@/lib/format'
import {
  getUserGroupDescription,
  getUserGroupDisplayName,
} from '@/lib/group-display'
import { cn } from '@/lib/utils'

import { formatGroupDuration, formatGroupSuccessRate } from '../format'
import { getGroupLastSignalAt } from '../signal'
import {
  GROUP_EXPERIENCE_META,
  getGroupStatusLabel,
  getGroupStatusMessage,
  getGroupStatusMeta,
} from '../status'
import type { GroupStatusEntry } from '../types'
import { GroupSignalBars } from './group-signal-bars'

const CARD_TONES: Record<
  GroupStatusEntry['confidence_status'],
  { card: string; icon: string; badge: string }
> = {
  unavailable: {
    card: 'border-destructive/25 hover:border-destructive/40',
    icon: 'bg-destructive/10 text-destructive ring-destructive/20',
    badge: 'bg-destructive/10 ring-destructive/20',
  },
  unstable: {
    card: 'border-warning/25 hover:border-warning/40',
    icon: 'bg-warning/10 text-warning ring-warning/20',
    badge: 'bg-warning/10 ring-warning/20',
  },
  unknown: {
    card: 'border-border/80 hover:border-foreground/20',
    icon: 'bg-muted/70 text-muted-foreground ring-border',
    badge: 'bg-muted/70 ring-border',
  },
  stable: {
    card: 'border-success/20 hover:border-success/35',
    icon: 'bg-success/10 text-success ring-success/20',
    badge: 'bg-success/10 ring-success/20',
  },
  smooth: {
    card: 'border-chart-2/20 hover:border-chart-2/35',
    icon: 'bg-chart-2/10 text-chart-2 ring-chart-2/20',
    badge: 'bg-chart-2/10 ring-chart-2/20',
  },
  excellent: {
    card: 'border-emerald-500/20 hover:border-emerald-500/35',
    icon: 'bg-emerald-500/10 text-emerald-600 ring-emerald-500/20 dark:text-emerald-300',
    badge: 'bg-emerald-500/10 ring-emerald-500/20',
  },
}

export function GroupStatusCard(props: { group: GroupStatusEntry }) {
  const { t } = useTranslation()
  const meta = getGroupStatusMeta(props.group)
  const tone = CARD_TONES[props.group.confidence_status]
  const ExperienceIcon =
    GROUP_EXPERIENCE_META[props.group.experience_label].icon
  const lastSignalAt = getGroupLastSignalAt(props.group)
  const statusMessage = t(getGroupStatusMessage(props.group))
  const displayName = getUserGroupDisplayName(props.group.group, {
    display_name: props.group.display_name,
    desc: props.group.desc,
  })
  const description =
    getUserGroupDescription({ desc: props.group.desc }, displayName) ??
    t('User group')

  return (
    <Card
      size='sm'
      className={cn(
        'h-full min-h-[22rem] gap-0 rounded-lg border bg-card/80 py-0 shadow-sm transition-[background-color,border-color] hover:bg-card',
        tone.card
      )}
    >
      <CardContent className='flex h-full flex-col gap-4 p-4'>
        <div className='flex min-w-0 items-start gap-3'>
          <div
            className={cn(
              'grid size-11 shrink-0 place-items-center rounded-lg ring-1 ring-inset',
              tone.icon
            )}
          >
            <Layers3 className='size-5' aria-hidden='true' />
          </div>
          <div className='min-w-0 flex-1 pt-0.5'>
            <div className='truncate text-base font-semibold'>
              {displayName}
            </div>
            <div
              className='text-muted-foreground truncate text-xs'
              title={description}
            >
              {description}
            </div>
          </div>
          <StatusBadge
            copyable={false}
            label={t(getGroupStatusLabel(props.group))}
            variant={props.group.stale ? 'warning' : meta.variant}
            title={statusMessage}
            className={cn(
              'shrink-0 ring-1 ring-inset',
              props.group.stale ? 'bg-warning/10 ring-warning/20' : tone.badge
            )}
          />
        </div>

        <dl className='grid grid-cols-2 gap-2.5'>
          <MetricPanel
            label={t('TTFT')}
            value={formatGroupDuration(props.group.avg_ttft_ms)}
            icon={ExperienceIcon}
          />
          <MetricPanel
            label={t('Latency')}
            value={formatGroupDuration(props.group.avg_latency_ms)}
            icon={Timer}
          />
        </dl>

        <div className='flex min-w-0 items-end justify-between gap-4 border-y py-3.5'>
          <div className='min-w-0 pb-0.5'>
            <p className='text-muted-foreground text-xs'>{t('Success rate')}</p>
            <p
              className='text-muted-foreground mt-1 truncate text-xs'
              title={statusMessage}
            >
              {statusMessage}
            </p>
          </div>
          <div
            className={cn(
              'shrink-0 text-3xl leading-none font-semibold tabular-nums',
              meta.toneClass
            )}
          >
            {formatGroupSuccessRate(props.group)}
          </div>
        </div>

        <div className='mt-auto space-y-2'>
          <div className='flex items-center justify-between gap-3 text-xs'>
            <span className='text-muted-foreground font-medium'>
              {t('Last 60 requests')}
            </span>
            <span className='text-muted-foreground shrink-0 tabular-nums'>
              {formatTimestampRelative(lastSignalAt)}
            </span>
          </div>
          <GroupSignalBars events={props.group.recent_events} />
          <div className='text-muted-foreground/70 flex items-center justify-between text-[10px] font-medium uppercase'>
            <span>{t('Past')}</span>
            <span>{t('Now')}</span>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function MetricPanel(props: {
  label: string
  value: string
  icon?: LucideIcon
}) {
  const Icon = props.icon
  return (
    <div className='bg-muted/10 min-w-0 rounded-lg border px-3 py-3'>
      <dt className='text-muted-foreground flex items-center gap-1 text-xs'>
        {Icon && <Icon className='size-3' aria-hidden='true' />}
        {props.label}
      </dt>
      <dd className='mt-2 truncate text-lg font-semibold tabular-nums'>
        {props.value}
      </dd>
    </div>
  )
}
