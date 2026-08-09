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

import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { formatGroupDuration } from '../format'
import { getGroupSignalEvents, GROUP_SIGNAL_COUNT } from '../signal'
import type { GroupRecentEvent } from '../types'

const SIGNAL_PLACEHOLDER_KEYS = Array.from(
  { length: GROUP_SIGNAL_COUNT },
  (_, index) => `signal-placeholder-${index}`
)

export function GroupSignalBars(props: { events: GroupRecentEvent[] | null }) {
  const { t } = useTranslation()
  const events = getGroupSignalEvents(props.events)

  if (events.length === 0) {
    return (
      <div
        className='bg-muted/10 text-muted-foreground flex h-10 items-center justify-center rounded-md border border-dashed px-2 text-xs'
        role='img'
        aria-label={t('No history data available')}
      >
        {t('No history data available')}
      </div>
    )
  }

  const successful = events.filter((event) => event.status === 'success').length
  const occurrences = new Map<string, number>()
  const keyedEvents = events.map((event) => {
    const baseKey = `${event.ts}-${event.status}-${event.ttft_ms ?? 0}-${event.latency_ms ?? 0}`
    const occurrence = (occurrences.get(baseKey) ?? 0) + 1
    occurrences.set(baseKey, occurrence)
    return { event, key: `${baseKey}-${occurrence}` }
  })

  return (
    <div
      className='grid h-10 grid-cols-[repeat(60,minmax(0,1fr))] items-end gap-[2px]'
      role='img'
      aria-label={t(
        '{{successful}} successful and {{failed}} failed requests',
        {
          successful,
          failed: events.length - successful,
        }
      )}
    >
      {SIGNAL_PLACEHOLDER_KEYS.slice(0, GROUP_SIGNAL_COUNT - events.length).map(
        (key) => (
          <span
            key={key}
            className='bg-muted-foreground/15 h-1.5 rounded-[2px]'
            aria-hidden='true'
          />
        )
      )}
      {keyedEvents.map((item) => (
        <span
          key={item.key}
          className={cn(
            'rounded-[2px]',
            item.event.status === 'success'
              ? 'h-8 bg-success/80'
              : 'h-5 bg-destructive'
          )}
          title={[
            formatTimestampToDate(item.event.ts),
            t(item.event.status === 'success' ? 'Success' : 'Failure'),
            `${t('TTFT')}: ${formatGroupDuration(item.event.ttft_ms ?? 0)}`,
            `${t('Latency')}: ${formatGroupDuration(item.event.latency_ms ?? 0)}`,
          ].join(' · ')}
          aria-hidden='true'
        />
      ))}
    </div>
  )
}
