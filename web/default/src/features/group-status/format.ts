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

import { formatNumber, formatPercent } from '@/lib/format'

import type { GroupCacheStats, GroupStatusEntry } from './types'

export function formatGroupSuccessRate(group: GroupStatusEntry): string {
  if (group.request_count <= 0) return '-'
  return formatPercent(group.success_rate)
}

export function formatGroupCacheHitRate(stats: GroupCacheStats): string {
  if (
    stats.status !== 'ok' ||
    stats.request_hit_rate == null ||
    !Number.isFinite(stats.request_hit_rate)
  ) {
    return '-'
  }
  return Intl.NumberFormat(undefined, {
    style: 'percent',
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  }).format(stats.request_hit_rate / 100)
}

export function formatGroupDuration(value: number): string {
  if (!value || value < 0) return '-'
  if (value >= 1000) return `${formatNumber(value / 1000)}s`
  return `${formatNumber(value)}ms`
}
