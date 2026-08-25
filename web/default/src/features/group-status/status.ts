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

import {
  AlertTriangle,
  CheckCircle2,
  CircleHelp,
  Gauge,
  Rocket,
  Sparkles,
  Zap,
  type LucideIcon,
} from 'lucide-react'

import type { StatusVariant } from '@/components/status-badge'

import { getGroupLastSignalAt } from './signal'
import type {
  GroupConfidenceStatus,
  GroupExperienceLabel,
  GroupStatusEntry,
} from './types'

type StatusMeta = {
  labelKey: string
  icon: LucideIcon
  variant: StatusVariant
  toneClass: string
  rank: number
}

export const GROUP_STATUS_META: Record<GroupConfidenceStatus, StatusMeta> = {
  unavailable: {
    labelKey: 'Unavailable',
    icon: AlertTriangle,
    variant: 'danger',
    toneClass: 'text-destructive',
    rank: 0,
  },
  unstable: {
    labelKey: 'Unstable',
    icon: AlertTriangle,
    variant: 'warning',
    toneClass: 'text-warning',
    rank: 1,
  },
  unknown: {
    labelKey: 'Unknown',
    icon: CircleHelp,
    variant: 'neutral',
    toneClass: 'text-muted-foreground',
    rank: 2,
  },
  stable: {
    labelKey: 'Stable',
    icon: CheckCircle2,
    variant: 'success',
    toneClass: 'text-success',
    rank: 3,
  },
  smooth: {
    labelKey: 'Smooth',
    icon: Zap,
    variant: 'teal',
    toneClass: 'text-chart-2',
    rank: 4,
  },
  excellent: {
    labelKey: 'Excellent',
    icon: Sparkles,
    variant: 'light-green',
    toneClass: 'text-emerald-500 dark:text-emerald-300',
    rank: 5,
  },
}

export const GROUP_EXPERIENCE_META: Record<
  GroupExperienceLabel,
  { labelKey: string; icon: LucideIcon }
> = {
  lightning: { labelKey: 'Instant', icon: Rocket },
  smooth: { labelKey: 'Smooth', icon: Zap },
  normal: { labelKey: 'Normal', icon: Gauge },
  unknown: { labelKey: 'Unknown', icon: CircleHelp },
}

export function getGroupStatusMeta(group: GroupStatusEntry): StatusMeta {
  return GROUP_STATUS_META[group.confidence_status]
}

export function getGroupStatusLabel(group: GroupStatusEntry): string {
  if (group.stale) return 'Stale'
  return getGroupStatusMeta(group).labelKey
}

export function getGroupStatusMessage(group: GroupStatusEntry): string {
  return group.display_message || group.message
}

export function sortGroupStatuses(
  groups: GroupStatusEntry[]
): GroupStatusEntry[] {
  return [...groups].sort((left, right) => {
    const leftHasCurrentData = hasCurrentGroupStatus(left)
    const rightHasCurrentData = hasCurrentGroupStatus(right)
    if (leftHasCurrentData !== rightHasCurrentData) {
      return leftHasCurrentData ? -1 : 1
    }

    if (leftHasCurrentData) {
      const rankDiff =
        getGroupStatusMeta(right).rank - getGroupStatusMeta(left).rank
      if (rankDiff !== 0) return rankDiff

      const successRateDiff = right.success_rate - left.success_rate
      if (successRateDiff !== 0) return successRateDiff

      const lastSignalDiff =
        getGroupLastSignalAt(right) - getGroupLastSignalAt(left)
      if (lastSignalDiff !== 0) return lastSignalDiff
    }

    return left.group.localeCompare(right.group)
  })
}

function hasCurrentGroupStatus(group: GroupStatusEntry): boolean {
  return (
    !group.stale &&
    group.confidence_status !== 'unknown' &&
    group.request_count > 0 &&
    Number.isFinite(group.success_rate)
  )
}
