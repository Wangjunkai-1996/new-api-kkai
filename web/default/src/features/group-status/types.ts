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

export type GroupStatusWindow = 'now' | '15m' | '1h' | '6h' | '24h'

export type GroupHealthStatus =
  | 'operational'
  | 'busy'
  | 'degraded'
  | 'outage'
  | 'unknown'

export type GroupHealthConfidence = 'high' | 'medium' | 'low'

export type GroupConfidenceStatus =
  | 'excellent'
  | 'smooth'
  | 'stable'
  | 'unstable'
  | 'unavailable'
  | 'unknown'

export type GroupExperienceLabel = 'lightning' | 'smooth' | 'normal' | 'unknown'

export type GroupRecentEvent = {
  ts: number
  status: 'success' | 'failure'
  ttft_ms?: number
  latency_ms?: number
}

export type GroupStatusEntry = {
  group: string
  desc: string
  status: GroupHealthStatus
  confidence: GroupHealthConfidence
  message: string
  confidence_status: GroupConfidenceStatus
  experience_label: GroupExperienceLabel
  display_message: string
  request_count: number
  success_rate: number
  avg_latency_ms: number
  avg_ttft_ms: number
  updated_at: number
  sampled_at: number
  stale: boolean
  data_source: string
  recent_events: GroupRecentEvent[] | null
}

export type GroupStatusResult = {
  generated_at: number
  window: GroupStatusWindow
  window_minutes: number
  window_hours: number
  data_source: string
  redis_available: boolean
  groups: GroupStatusEntry[]
}

export type GroupStatusResponse = {
  success: boolean
  message?: string
  data?: GroupStatusResult
}
