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

export type GroupStatusWindowHours = 1 | 6 | 24

export type GroupHealthStatus =
  | 'operational'
  | 'busy'
  | 'degraded'
  | 'outage'
  | 'unknown'

export type GroupHealthConfidence = 'high' | 'medium' | 'low'

export interface GroupStatusEntry {
  group: string
  desc: string
  status: GroupHealthStatus
  confidence: GroupHealthConfidence
  message: string
  request_count: number
  success_rate: number
  avg_latency_ms: number
  avg_ttft_ms: number
  available_model_count: number
  updated_at: number
}

export interface GroupStatusResult {
  generated_at: number
  window_hours: GroupStatusWindowHours
  groups: GroupStatusEntry[]
}

export interface GroupStatusResponse {
  success: boolean
  message?: string
  data?: GroupStatusResult
}
