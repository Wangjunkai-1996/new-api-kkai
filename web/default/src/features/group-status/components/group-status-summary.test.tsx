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

import { render, screen, within } from '@testing-library/react'
import { expect, test } from 'vitest'

import type { GroupStatusEntry, GroupStatusResult } from '../types'
import { GroupStatusSummary } from './group-status-summary'

const BASE_GROUP: GroupStatusEntry = {
  group: 'test-group',
  desc: '',
  status: 'operational',
  confidence: 'high',
  message: '',
  confidence_status: 'stable',
  experience_label: 'normal',
  display_message: '',
  request_count: 20,
  success_rate: 100,
  avg_latency_ms: 800,
  avg_ttft_ms: 300,
  updated_at: 0,
  sampled_at: 0,
  stale: false,
  data_source: 'redis',
  recent_events: [],
}

test.each([
  {
    name: 'all groups have expired data',
    groups: [
      { ...BASE_GROUP, stale: true, confidence_status: 'unavailable' as const },
    ],
    expected: 'Unknown',
  },
  {
    name: 'a current healthy group remains',
    groups: [
      { ...BASE_GROUP, stale: true, confidence_status: 'unavailable' as const },
      BASE_GROUP,
    ],
    expected: 'Healthy',
  },
  {
    name: 'a current unhealthy group remains',
    groups: [
      { ...BASE_GROUP, stale: true },
      { ...BASE_GROUP, confidence_status: 'unstable' as const },
    ],
    expected: 'Attention',
  },
])('summarizes $name as $expected', ({ groups, expected }) => {
  const result: GroupStatusResult = {
    generated_at: 0,
    window: 'now',
    window_minutes: 0,
    window_hours: 0,
    data_source: 'redis',
    redis_available: true,
    groups,
  }

  render(<GroupStatusSummary groups={groups} result={result} />)

  const summary = screen.getByRole('region', { name: 'Status summary' })
  expect(within(summary).getByText(expected)).toBeInTheDocument()
})
