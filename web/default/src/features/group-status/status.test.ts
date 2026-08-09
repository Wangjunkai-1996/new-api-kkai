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

import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { sortGroupStatuses } from './status'
import type { GroupStatusEntry } from './types'

const BASE_GROUP: GroupStatusEntry = {
  group: 'base',
  desc: '',
  status: 'operational',
  confidence: 'high',
  message: '',
  confidence_status: 'stable',
  experience_label: 'normal',
  display_message: '',
  request_count: 0,
  success_rate: 100,
  avg_latency_ms: 1000,
  avg_ttft_ms: 500,
  updated_at: 0,
  sampled_at: 0,
  stale: false,
  data_source: 'redis',
  recent_events: [],
}

function createGroup(
  group: string,
  overrides: Partial<GroupStatusEntry> = {}
): GroupStatusEntry {
  return { ...BASE_GROUP, ...overrides, group }
}

describe('group status sorting', () => {
  test('uses the latest signal instead of request count within a status', () => {
    const olderBusyGroup = createGroup('older-busy', {
      request_count: 10_000,
      recent_events: [{ ts: 100, status: 'success' }],
    })
    const newerQuietGroup = createGroup('newer-quiet', {
      request_count: 1,
      recent_events: [{ ts: 200, status: 'success' }],
    })

    const result = sortGroupStatuses([olderBusyGroup, newerQuietGroup])

    assert.deepEqual(
      result.map((group) => group.group),
      ['newer-quiet', 'older-busy']
    )
  })

  test('keeps status priority ahead of recency and falls back to sampled_at', () => {
    const unavailable = createGroup('unavailable', {
      confidence_status: 'unavailable',
      status: 'outage',
      recent_events: [{ ts: 100, status: 'failure' }],
    })
    const sampled = createGroup('sampled', {
      sampled_at: 300,
      recent_events: null,
    })
    const historical = createGroup('historical', {
      sampled_at: 999,
      recent_events: [{ ts: 200, status: 'success' }],
    })

    const result = sortGroupStatuses([historical, sampled, unavailable])

    assert.deepEqual(
      result.map((group) => group.group),
      ['unavailable', 'sampled', 'historical']
    )
  })
})
