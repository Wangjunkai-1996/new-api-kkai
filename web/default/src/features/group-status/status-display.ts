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

import type {
  GroupConfidenceStatus,
  GroupExperienceLabel,
  GroupStatusEntry,
} from './types'
import { CONFIDENCE_META, EXPERIENCE_META, MESSAGE_LABELS } from './status-meta'

export function sortGroupsForConfidence(groups: GroupStatusEntry[]) {
  return [...groups].sort((left, right) => {
    const confidenceDiff =
      CONFIDENCE_META[getConfidenceStatus(right)].score -
      CONFIDENCE_META[getConfidenceStatus(left)].score
    if (confidenceDiff !== 0) return confidenceDiff

    const successDiff = right.success_rate - left.success_rate
    if (successDiff !== 0) return successDiff

    const requestDiff = right.request_count - left.request_count
    if (requestDiff !== 0) return requestDiff

    return left.group.localeCompare(right.group)
  })
}

export function getMessageKey(group: GroupStatusEntry) {
  const message = group.display_message ?? group.message
  return MESSAGE_LABELS[message] ?? message
}

export function getConfidenceStatus(
  group: GroupStatusEntry
): GroupConfidenceStatus {
  if (group.confidence_status) return group.confidence_status
  switch (group.status) {
    case 'operational':
      return 'stable'
    case 'degraded':
      return 'unstable'
    case 'outage':
      return 'unavailable'
    default:
      return 'unknown'
  }
}

export function getExperienceLabel(
  group: GroupStatusEntry
): GroupExperienceLabel {
  return group.experience_label ?? 'unknown'
}

export function shouldShowExperience(group: GroupStatusEntry): boolean {
  return EXPERIENCE_META[getExperienceLabel(group)].visible
}
