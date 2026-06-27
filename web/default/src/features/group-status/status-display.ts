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
  GroupRecommendationLevel,
  GroupStatusEntry,
} from './types'
import {
  CONFIDENCE_META,
  EXPERIENCE_META,
  MESSAGE_LABELS,
  RECOMMENDATION_META,
} from './status-meta'

export function sortGroupsForConfidence(groups: GroupStatusEntry[]) {
  return [...groups].sort((left, right) => {
    const recommendationDiff =
      RECOMMENDATION_META[getRecommendationLevel(right)].rank -
      RECOMMENDATION_META[getRecommendationLevel(left)].rank
    if (recommendationDiff !== 0) return recommendationDiff

    const confidenceDiff =
      CONFIDENCE_META[getConfidenceStatus(right)].score -
      CONFIDENCE_META[getConfidenceStatus(left)].score
    if (confidenceDiff !== 0) return confidenceDiff

    const successDiff = right.success_rate - left.success_rate
    if (successDiff !== 0) return successDiff

    const requestDiff = right.request_count - left.request_count
    if (requestDiff !== 0) return requestDiff

    const modelDiff = right.available_model_count - left.available_model_count
    if (modelDiff !== 0) return modelDiff

    return left.group.localeCompare(right.group)
  })
}

export function getBestGroup(groups: GroupStatusEntry[]) {
  return sortGroupsForConfidence(groups)[0]
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

export function getRecommendationLevel(
  group: GroupStatusEntry
): GroupRecommendationLevel {
  if (group.recommendation_level) return group.recommendation_level
  return groupRecommendationFromConfidence(getConfidenceStatus(group))
}

function groupRecommendationFromConfidence(
  confidenceStatus: GroupConfidenceStatus
): GroupRecommendationLevel {
  switch (confidenceStatus) {
    case 'excellent':
      return 'best'
    case 'smooth':
      return 'recommended'
    case 'stable':
      return 'usable'
    case 'unstable':
      return 'caution'
    case 'unavailable':
      return 'unavailable'
    default:
      return 'unknown'
  }
}
