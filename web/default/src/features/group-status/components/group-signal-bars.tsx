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

import { cn } from '@/lib/utils'

import type { GroupRecentEvent } from '../types'

const SIGNAL_COUNT = 24
const SIGNAL_PLACEHOLDER_KEYS = Array.from(
  { length: SIGNAL_COUNT },
  (_, index) => `signal-placeholder-${index}`
)

export function GroupSignalBars(props: { events: GroupRecentEvent[] | null }) {
  const { t } = useTranslation()
  const events = [...(props.events || [])]
    .filter((event) => event.status === 'success' || event.status === 'failure')
    .sort((left, right) => left.ts - right.ts)
    .slice(-SIGNAL_COUNT)
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
      className='bg-muted/30 grid h-7 grid-cols-[repeat(24,minmax(0,1fr))] items-end gap-0.5 rounded-md p-1'
      role='img'
      aria-label={t(
        '{{successful}} successful and {{failed}} failed requests',
        {
          successful,
          failed: events.length - successful,
        }
      )}
    >
      {SIGNAL_PLACEHOLDER_KEYS.slice(0, SIGNAL_COUNT - events.length).map(
        (key) => (
          <span
            key={key}
            className='bg-muted-foreground/15 h-1 rounded-sm'
            aria-hidden='true'
          />
        )
      )}
      {keyedEvents.map((item) => (
        <span
          key={item.key}
          className={cn(
            'rounded-sm',
            item.event.status === 'success'
              ? 'h-5 bg-success/75'
              : 'h-3 bg-destructive/80'
          )}
          title={t(item.event.status === 'success' ? 'Success' : 'Failure')}
          aria-hidden='true'
        />
      ))}
    </div>
  )
}
