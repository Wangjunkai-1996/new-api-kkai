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
import i18next from 'i18next'
import { beforeAll, describe, expect, test } from 'vitest'

import type { GroupCacheStats, GroupStatusEntry } from '../types'
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

beforeAll(() => {
  i18next.addResourceBundle(
    'en',
    'translation',
    {
      'Cache data unavailable': 'Cache data unavailable',
      'Cache hit rate': 'Cache hit rate',
      'No cache samples in this window': 'No cache samples in this window',
      'Samples: {{count}}': 'Samples: {{count}}',
    },
    true,
    true
  )
})

function renderCard(cacheStats?: GroupCacheStats): void {
  render(<GroupStatusCard group={{ ...BASE_GROUP, cache_stats: cacheStats }} />)
}

function cacheMetric(): HTMLElement {
  const label = screen.getByText('Cache hit rate')
  const metric = label.closest('div')
  expect(metric).not.toBeNull()
  return metric as HTMLElement
}

describe('group cache hit rate metric', () => {
  test('does not render for groups without cache statistics', () => {
    renderCard()

    expect(screen.queryByText('Cache hit rate')).not.toBeInTheDocument()
  })

  test('renders the rate and sample count for available statistics', () => {
    renderCard({ status: 'ok', sample_count: 128, request_hit_rate: 92.84 })

    expect(within(cacheMetric()).getByText('92.8%')).toBeInTheDocument()
    expect(within(cacheMetric()).getByText('Samples: 128')).toBeInTheDocument()
  })

  test.each([
    {
      status: 'empty' as const,
      description: 'No cache samples in this window',
    },
    {
      status: 'unavailable' as const,
      description: 'Cache data unavailable',
    },
  ])('renders a placeholder for $status statistics', (testCase) => {
    renderCard({
      status: testCase.status,
      sample_count: 0,
      request_hit_rate: null,
    })

    expect(within(cacheMetric()).getByText('-')).toBeInTheDocument()
    expect(
      within(cacheMetric()).getByText(testCase.description)
    ).toBeInTheDocument()
  })
})
