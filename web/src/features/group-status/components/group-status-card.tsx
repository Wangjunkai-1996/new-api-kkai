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

import type { LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Card, CardContent } from '@/components/ui/card'
import { formatNumber, formatTimestampRelative } from '@/lib/format'
import { cn } from '@/lib/utils'

import { formatGroupDuration, formatGroupSuccessRate } from '../format'
import {
  GROUP_EXPERIENCE_META,
  getGroupStatusLabel,
  getGroupStatusMessage,
  getGroupStatusMeta,
} from '../status'
import type { GroupStatusEntry } from '../types'
import { GroupSignalBars } from './group-signal-bars'

export function GroupStatusCard(props: { group: GroupStatusEntry }) {
  const { t } = useTranslation()
  const meta = getGroupStatusMeta(props.group)
  const StatusIcon = meta.icon
  const ExperienceIcon =
    GROUP_EXPERIENCE_META[props.group.experience_label].icon

  return (
    <Card size='sm' className='rounded-lg'>
      <CardContent className='space-y-3'>
        <div className='flex min-w-0 items-start justify-between gap-3'>
          <div className='min-w-0'>
            <div className='truncate font-semibold'>{props.group.group}</div>
            <div className='text-muted-foreground truncate text-xs'>
              {props.group.desc || t('User group')}
            </div>
          </div>
          <StatusBadge
            copyable={false}
            icon={StatusIcon}
            label={t(getGroupStatusLabel(props.group))}
            variant={props.group.stale ? 'warning' : meta.variant}
            title={t(getGroupStatusMessage(props.group))}
          />
        </div>
        <dl className='grid grid-cols-2 gap-x-4 gap-y-2 border-y py-3 text-sm'>
          <CompactMetric
            label={t('Success')}
            value={formatGroupSuccessRate(props.group)}
            valueClassName={meta.toneClass}
          />
          <CompactMetric
            label={t('Requests')}
            value={formatNumber(props.group.request_count)}
          />
          <CompactMetric
            label={t('TTFT')}
            value={formatGroupDuration(props.group.avg_ttft_ms)}
            icon={ExperienceIcon}
          />
          <CompactMetric
            label={t('Latency')}
            value={formatGroupDuration(props.group.avg_latency_ms)}
          />
        </dl>
        <GroupSignalBars events={props.group.recent_events} />
        <div className='flex items-start justify-between gap-3 text-xs'>
          <p className='text-muted-foreground line-clamp-2 min-w-0'>
            {t(getGroupStatusMessage(props.group))}
          </p>
          <span className='text-muted-foreground shrink-0 tabular-nums'>
            {formatTimestampRelative(props.group.sampled_at)}
          </span>
        </div>
      </CardContent>
    </Card>
  )
}

function CompactMetric(props: {
  label: string
  value: string
  valueClassName?: string
  icon?: LucideIcon
}) {
  const Icon = props.icon
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground flex items-center gap-1 text-xs'>
        {Icon && <Icon className='size-3' aria-hidden='true' />}
        {props.label}
      </dt>
      <dd
        className={cn(
          'truncate font-medium tabular-nums',
          props.valueClassName
        )}
      >
        {props.value}
      </dd>
    </div>
  )
}
