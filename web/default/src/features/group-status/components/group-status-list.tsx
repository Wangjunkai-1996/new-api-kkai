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

import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Card } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  formatNumber,
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'
import { cn } from '@/lib/utils'

import { formatGroupDuration, formatGroupSuccessRate } from '../format'
import {
  getGroupStatusLabel,
  getGroupStatusMessage,
  getGroupStatusMeta,
} from '../status'
import type { GroupStatusEntry } from '../types'
import { GroupSignalBars } from './group-signal-bars'
import { GroupStatusCard } from './group-status-card'

export function GroupStatusList(props: { groups: GroupStatusEntry[] }) {
  const { t } = useTranslation()
  return (
    <>
      <div className='grid gap-3 lg:hidden'>
        {props.groups.map((group) => (
          <GroupStatusCard key={group.group} group={group} />
        ))}
      </div>
      <Card className='hidden rounded-lg py-0 lg:block'>
        <Table>
          <TableHeader>
            <TableRow className='bg-muted/30'>
              <TableHead className='min-w-44'>{t('Group')}</TableHead>
              <TableHead className='min-w-28'>{t('Status')}</TableHead>
              <TableHead className='text-right'>{t('Success')}</TableHead>
              <TableHead className='text-right'>{t('Requests')}</TableHead>
              <TableHead className='text-right'>{t('TTFT')}</TableHead>
              <TableHead className='text-right'>{t('Latency')}</TableHead>
              <TableHead className='min-w-32'>{t('Last signal')}</TableHead>
              <TableHead className='w-44'>{t('Recent')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.groups.map((group) => (
              <GroupStatusRow key={group.group} group={group} />
            ))}
          </TableBody>
        </Table>
      </Card>
    </>
  )
}

function GroupStatusRow(props: { group: GroupStatusEntry }) {
  const { t } = useTranslation()
  const meta = getGroupStatusMeta(props.group)
  const StatusIcon = meta.icon
  return (
    <TableRow className={cn(props.group.stale && 'bg-warning/5')}>
      <TableCell>
        <div className='max-w-56 min-w-0'>
          <div className='truncate font-medium'>{props.group.group}</div>
          <div className='text-muted-foreground truncate text-xs'>
            {props.group.desc || t('User group')}
          </div>
        </div>
      </TableCell>
      <TableCell>
        <StatusBadge
          copyable={false}
          icon={StatusIcon}
          label={t(getGroupStatusLabel(props.group))}
          variant={props.group.stale ? 'warning' : meta.variant}
          title={t(getGroupStatusMessage(props.group))}
        />
      </TableCell>
      <TableCell className={cn('text-right font-medium', meta.toneClass)}>
        {formatGroupSuccessRate(props.group)}
      </TableCell>
      <TableCell className='text-right'>
        {formatNumber(props.group.request_count)}
      </TableCell>
      <TableCell className='text-right'>
        {formatGroupDuration(props.group.avg_ttft_ms)}
      </TableCell>
      <TableCell className='text-right'>
        {formatGroupDuration(props.group.avg_latency_ms)}
      </TableCell>
      <TableCell>
        <span title={formatTimestampToDate(props.group.sampled_at)}>
          {formatTimestampRelative(props.group.sampled_at)}
        </span>
      </TableCell>
      <TableCell>
        <GroupSignalBars events={props.group.recent_events} />
      </TableCell>
    </TableRow>
  )
}
