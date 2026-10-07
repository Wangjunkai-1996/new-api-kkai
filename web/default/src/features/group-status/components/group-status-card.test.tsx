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
  test('renders the configured display name and keeps the description', () => {
    render(
      <GroupStatusCard
        group={{
          ...BASE_GROUP,
          display_name: 'Customer plan',
          desc: 'Low-cost model pool',
        }}
      />
    )

    expect(screen.getByText('Customer plan')).toBeInTheDocument()
    expect(screen.getByText('Low-cost model pool')).toBeInTheDocument()
    expect(screen.queryByText('test-group')).not.toBeInTheDocument()
    expect(screen.queryByText('Multiplier')).not.toBeInTheDocument()
  })

  test('preserves a configured zero multiplier', () => {
    render(<GroupStatusCard group={{ ...BASE_GROUP, ratio: 0 }} />)

    expect(screen.getByText('x0')).toBeInTheDocument()
  })

  test('shows the group multiplier and recent cache hit rate when available', () => {
    render(
      <GroupStatusCard
        group={{
          ...BASE_GROUP,
          ratio: 0.75,
          cache_stats: {
            status: 'ok',
            sample_count: 128,
            request_hit_rate: 92.84,
          },
        }}
      />
    )

    expect(screen.getByText('Multiplier')).toBeInTheDocument()
    expect(screen.getByText('x0.75')).toBeInTheDocument()
    expect(screen.getByText('Cache hit rate (24h)')).toHaveAttribute(
      'title',
      'Requests with more than 30% cached input, as a share of successful text requests with reported usage in the last 24 hours.'
    )
    expect(screen.getByText('92.84%')).toBeInTheDocument()
    expect(screen.queryByText('Samples: 128')).not.toBeInTheDocument()
  })

  test.each([
    { status: 'empty' as const, request_hit_rate: 87 },
    { status: 'unavailable' as const, request_hit_rate: 87 },
    { status: 'ok' as const, request_hit_rate: 0 },
    { status: 'ok' as const, request_hit_rate: null },
  ])('hides cache hit rate without a positive valid sample: %o', (cache) => {
    render(
      <GroupStatusCard
        group={{
          ...BASE_GROUP,
          cache_stats: { sample_count: 0, ...cache },
        }}
      />
    )

    expect(screen.queryByText(/Cache hit rate/)).not.toBeInTheDocument()
  })

  test('keeps stale health neutral while retaining valid 24-hour cache stats', () => {
    render(
      <GroupStatusCard
        group={{
          ...BASE_GROUP,
          stale: true,
          display_message: 'Data stale',
          cache_stats: {
            status: 'ok',
            sample_count: 10,
            request_hit_rate: 90,
          },
        }}
      />
    )

    expect(screen.getByText('Unknown')).toBeInTheDocument()
    expect(screen.queryByText('Stale')).not.toBeInTheDocument()
    expect(screen.queryByText('Data stale')).not.toBeInTheDocument()
    expect(screen.queryByText('100%')).not.toBeInTheDocument()
    expect(screen.queryByText('300ms')).not.toBeInTheDocument()
    expect(screen.getByText('Cache hit rate (24h)')).toBeInTheDocument()
    expect(screen.getByText('90%')).toBeInTheDocument()
    expect(screen.getByText('Last 60 requests')).toBeInTheDocument()
    expect(screen.queryByText(/ago$/)).not.toBeInTheDocument()
  })
})
