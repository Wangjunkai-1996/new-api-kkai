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

import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import type { GroupStatusEntry } from '../types'
import { GroupStatusCard } from './group-status-card'

const BASE_GROUP: GroupStatusEntry = {
  group: 'test-group',
  desc: 'Test group',
  status: 'operational',
  confidence: 'high',
  message: 'Group status message: stable',
  confidence_status: 'stable',
  experience_label: 'normal',
  display_message: 'Group status message: stable',
  request_count: 20,
  success_rate: 100,
  avg_latency_ms: 800,
  avg_ttft_ms: 300,
  updated_at: 1,
  sampled_at: 1,
  stale: false,
  data_source: 'redis',
  recent_events: [],
}

describe('group status card', () => {
  test('does not render cache hit rate data', () => {
    render(
      <GroupStatusCard
        group={{
          ...BASE_GROUP,
          cache_stats: {
            status: 'ok',
            sample_count: 128,
            request_hit_rate: 92.84,
          },
        }}
      />
    )

    expect(screen.queryByText('Cache hit rate')).not.toBeInTheDocument()
    expect(screen.queryByText('92.8%')).not.toBeInTheDocument()
    expect(screen.queryByText('Samples: 128')).not.toBeInTheDocument()
  })
})
