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
  Gauge,
  HelpCircle,
  Rocket,
  Sparkles,
  Zap,
} from 'lucide-react'
import type { StatusVariant } from '@/components/status-badge'
import type {
  GroupConfidenceStatus,
  GroupExperienceLabel,
  GroupRecommendationLevel,
} from './types'

export const WINDOW_OPTIONS = [1, 6, 24] as const

export const CONFIDENCE_META: Record<
  GroupConfidenceStatus,
  {
    labelKey: string
    toneClass: string
    barClass: string
    badgeVariant: StatusVariant
    icon: typeof CheckCircle2
    score: number
  }
> = {
  excellent: {
    labelKey: 'Very Smooth',
    toneClass: 'text-emerald-500 dark:text-emerald-300',
    barClass: 'bg-emerald-400',
    badgeVariant: 'light-green',
    icon: Sparkles,
    score: 6,
  },
  smooth: {
    labelKey: 'Smooth',
    toneClass: 'text-teal-500 dark:text-teal-300',
    barClass: 'bg-teal-400',
    badgeVariant: 'teal',
    icon: Zap,
    score: 5,
  },
  stable: {
    labelKey: 'Stable',
    toneClass: 'text-success',
    barClass: 'bg-success',
    badgeVariant: 'success',
    icon: CheckCircle2,
    score: 4,
  },
  unstable: {
    labelKey: 'Use With Care',
    toneClass: 'text-warning',
    barClass: 'bg-warning',
    badgeVariant: 'warning',
    icon: AlertTriangle,
    score: 2,
  },
  unavailable: {
    labelKey: 'Temporarily Unavailable',
    toneClass: 'text-destructive',
    barClass: 'bg-destructive',
    badgeVariant: 'danger',
    icon: AlertTriangle,
    score: 0,
  },
  unknown: {
    labelKey: 'Low Sample',
    toneClass: 'text-muted-foreground',
    barClass: 'bg-muted-foreground',
    badgeVariant: 'neutral',
    icon: HelpCircle,
    score: 3,
  },
}

export const EXPERIENCE_META: Record<
  GroupExperienceLabel,
  {
    labelKey: string
    variant: StatusVariant
    icon: typeof Gauge
    visible: boolean
  }
> = {
  lightning: {
    labelKey: 'Instant Feel',
    variant: 'light-green',
    icon: Rocket,
    visible: true,
  },
  smooth: {
    labelKey: 'Smooth Response',
    variant: 'teal',
    icon: Zap,
    visible: true,
  },
  normal: {
    labelKey: 'Responsive',
    variant: 'neutral',
    icon: Gauge,
    visible: false,
  },
  unknown: {
    labelKey: 'Experience Pending',
    variant: 'neutral',
    icon: HelpCircle,
    visible: false,
  },
}

export const RECOMMENDATION_META: Record<
  GroupRecommendationLevel,
  { labelKey: string; rank: number; variant: StatusVariant }
> = {
  best: { labelKey: 'Top Pick', rank: 6, variant: 'light-green' },
  recommended: { labelKey: 'Recommended Group', rank: 5, variant: 'teal' },
  usable: { labelKey: 'Ready To Use', rank: 4, variant: 'success' },
  unknown: { labelKey: 'Observe', rank: 3, variant: 'neutral' },
  caution: { labelKey: 'Backup Only', rank: 2, variant: 'warning' },
  unavailable: { labelKey: 'Skip For Now', rank: 0, variant: 'danger' },
}

export const MESSAGE_LABELS: Record<string, string> = {
  'Group status message: excellent': 'Group status message: excellent',
  'Group status message: smooth': 'Group status message: smooth',
  'Group status message: stable': 'Group status message: stable',
  'Group status message: unstable': 'Group status message: unstable',
  'Group status message: unavailable': 'Group status message: unavailable',
  'Group status message: no routable models':
    'Group status message: no routable models',
  'Group status message: unknown': 'Group status message: unknown',
  'No routable models are currently enabled for this group.':
    'Group status message: no routable models',
  'Not enough recent traffic to determine health.':
    'Group status message: unknown',
  'Recent requests are failing at a high rate.':
    'Group status message: unavailable',
  'Recent success rate is below the healthy threshold.':
    'Group status message: unstable',
  'Requests are succeeding, but latency is elevated.':
    'Group status message: smooth',
  'Recent traffic is healthy': 'Group status message: stable',
  'Recent traffic is healthy.': 'Group status message: stable',
}
