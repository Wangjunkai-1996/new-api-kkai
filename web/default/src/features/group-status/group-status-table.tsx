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
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatNumber, formatPercent } from '@/lib/format'
import { cn } from '@/lib/utils'
import {
  getConfidenceStatus,
  getExperienceLabel,
  getMessageKey,
  shouldShowExperience,
} from './status-display'
import { CONFIDENCE_META, EXPERIENCE_META } from './status-meta'
import type { GroupStatusEntry } from './types'

export function GroupStatusTable({ groups }: { groups: GroupStatusEntry[] }) {
  const { t } = useTranslation()

  return (
    <Card className='hidden overflow-hidden rounded-lg md:block'>
      <CardHeader className='border-b'>
        <CardTitle>{t('Group Flow')}</CardTitle>
      </CardHeader>
      <CardContent className='p-0'>
        <Table>
          <TableHeader>
            <TableRow className='bg-muted/30'>
              <TableHead className='min-w-52'>{t('Group')}</TableHead>
              <TableHead className='min-w-36'>{t('Flow Status')}</TableHead>
              <TableHead className='text-right'>{t('Success Rate')}</TableHead>
              <TableHead className='text-right'>{t('Samples')}</TableHead>
              <TableHead className='min-w-32'>{t('Experience')}</TableHead>
              <TableHead className='min-w-64'>{t('Status Detail')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.map((group) => (
              <ConfidenceRow key={group.group} group={group} />
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function ConfidenceRow({ group }: { group: GroupStatusEntry }) {
  const { t } = useTranslation()
  const confidenceMeta = CONFIDENCE_META[getConfidenceStatus(group)]
  const experienceMeta = EXPERIENCE_META[getExperienceLabel(group)]
  const ConfidenceIcon = confidenceMeta.icon
  const ExperienceIcon = experienceMeta.icon

  return (
    <TableRow className='group relative hover:bg-emerald-400/5'>
      <TableCell className='relative'>
        <span
          className={cn(
            'absolute top-2 bottom-2 left-0 w-1 rounded-r-full',
            confidenceMeta.barClass
          )}
          aria-hidden='true'
        />
        <div className='min-w-0'>
          <div className='font-medium'>{group.group}</div>
          <div className='text-muted-foreground max-w-64 truncate text-xs'>
            {group.desc || '-'}
          </div>
        </div>
      </TableCell>
      <TableCell>
        <StatusBadge
          copyable={false}
          icon={ConfidenceIcon}
          label={t(confidenceMeta.labelKey)}
          variant={confidenceMeta.badgeVariant}
        />
      </TableCell>
      <TableCell className='text-right'>
        <div
          className={cn(
            'text-base font-semibold tabular-nums',
            confidenceMeta.toneClass
          )}
        >
          {formatSuccessRate(group)}
        </div>
      </TableCell>
      <TableCell className='text-right tabular-nums'>
        {formatNumber(group.request_count)}
      </TableCell>
      <TableCell>
        {shouldShowExperience(group) ? (
          <StatusBadge
            copyable={false}
            icon={ExperienceIcon}
            label={t(experienceMeta.labelKey)}
            variant={experienceMeta.variant}
            title={
              group.avg_ttft_ms > 0
                ? t('TTFT about {{time}}', { time: formatTtft(group.avg_ttft_ms) })
                : undefined
            }
          />
        ) : (
          <span className='text-muted-foreground text-sm'>-</span>
        )}
      </TableCell>
      <TableCell>
        <span className='text-muted-foreground line-clamp-2 whitespace-normal'>
          {t(getMessageKey(group))}
        </span>
      </TableCell>
    </TableRow>
  )
}

function formatSuccessRate(group: GroupStatusEntry): string {
  if (group.request_count <= 0) return '-'
  return formatPercent(group.success_rate)
}

function formatTtft(value: number): string {
  if (!value) return '-'
  if (value >= 1000) {
    return `${formatNumber(value / 1000)}s`
  }
  return `${formatNumber(value)}ms`
}
