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

import { AlertTriangle, CheckCircle2, CircleHelp } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { formatTimestampRelative } from '@/lib/format'

import type { GroupStatusEntry, GroupStatusResult } from '../types'

export function GroupStatusSummary(props: {
  groups: GroupStatusEntry[]
  result: GroupStatusResult
}) {
  const { t } = useTranslation()
  const overall = overallStatus(props.groups)
  const source = sourceStatus(props.result)

  return (
    <section
      className='bg-card/60 flex min-h-11 flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2'
      aria-label={t('Status summary')}
    >
      <div className='flex min-w-0 flex-wrap items-center gap-2'>
        <StatusBadge
          copyable={false}
          icon={overall.icon}
          label={t(overall.labelKey)}
          variant={overall.variant}
          className={overall.className}
        />
        <StatusBadge
          copyable={false}
          label={t(source.labelKey)}
          variant={source.variant}
          title={props.result.data_source}
        />
      </div>
      <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
        {t('Updated {{time}}', {
          time: formatTimestampRelative(props.result.generated_at),
        })}
      </span>
    </section>
  )
}

function overallStatus(groups: GroupStatusEntry[]) {
  if (
    groups.some(
      (group) =>
        group.stale ||
        group.confidence_status === 'unstable' ||
        group.confidence_status === 'unavailable'
    )
  ) {
    return {
      labelKey: 'Attention',
      icon: AlertTriangle,
      variant: 'warning' as const,
      className: 'bg-warning/10 ring-1 ring-inset ring-warning/20',
    }
  }
  if (groups.some((group) => group.confidence_status !== 'unknown')) {
    return {
      labelKey: 'Healthy',
      icon: CheckCircle2,
      variant: 'success' as const,
      className: 'bg-success/10 ring-1 ring-inset ring-success/20',
    }
  }
  return {
    labelKey: 'Unknown',
    icon: CircleHelp,
    variant: 'neutral' as const,
    className: 'bg-muted/70 ring-1 ring-inset ring-border',
  }
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
