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

import type { GroupRecentEvent, GroupStatusEntry } from './types'

export const GROUP_SIGNAL_COUNT = 60

export function getGroupSignalEvents(
  events: GroupRecentEvent[] | null
): GroupRecentEvent[] {
  return [...(events ?? [])]
    .filter((event) => event.status === 'success' || event.status === 'failure')
    .sort((left, right) => left.ts - right.ts)
    .slice(-GROUP_SIGNAL_COUNT)
}

export function getGroupLastSignalAt(
  group: Pick<GroupStatusEntry, 'recent_events' | 'sampled_at'>
): number {
  const lastEventAt = (group.recent_events ?? []).reduce((latest, event) => {
    if (!Number.isFinite(event.ts) || event.ts <= 0) return latest
    return Math.max(latest, event.ts)
  }, 0)

  return lastEventAt || group.sampled_at
}
