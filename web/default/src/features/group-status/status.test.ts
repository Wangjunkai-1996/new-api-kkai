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

import { describe, test } from 'vitest'

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
  test('orders current statuses from best to worst', () => {
    const groups = [
      createGroup('unavailable', {
        confidence_status: 'unavailable',
        request_count: 10,
        success_rate: 70,
      }),
      createGroup('stable', {
        confidence_status: 'stable',
        request_count: 10,
        success_rate: 97,
      }),
      createGroup('excellent', {
        confidence_status: 'excellent',
        request_count: 10,
        success_rate: 100,
      }),
      createGroup('unstable', {
        confidence_status: 'unstable',
        request_count: 10,
        success_rate: 90,
      }),
      createGroup('smooth', {
        confidence_status: 'smooth',
        request_count: 10,
        success_rate: 99.5,
      }),
    ]

    const result = sortGroupStatuses(groups)

    assert.deepEqual(
      result.map((group) => group.group),
      ['excellent', 'smooth', 'stable', 'unstable', 'unavailable']
    )
  })

  test('uses success rate and then group name within the same status', () => {
    const lowerRate = createGroup('zeta', {
      confidence_status: 'stable',
      request_count: 10,
      success_rate: 96,
    })
    const sameRateLaterName = createGroup('zeta-later', {
      confidence_status: 'stable',
      request_count: 1,
      success_rate: 98,
    })
    const sameRateEarlierName = createGroup('alpha', {
      confidence_status: 'stable',
      request_count: 100,
      success_rate: 98,
    })

    const result = sortGroupStatuses([
      lowerRate,
      sameRateLaterName,
      sameRateEarlierName,
    ])

    assert.deepEqual(
      result.map((group) => group.group),
      ['alpha', 'zeta-later', 'zeta']
    )
  })

  test('uses the latest signal before the group name when rates tie', () => {
    const older = createGroup('older', {
      confidence_status: 'stable',
      request_count: 10,
      success_rate: 98,
      recent_events: [{ ts: 100, status: 'success' }],
    })
    const newer = createGroup('newer', {
      confidence_status: 'stable',
      request_count: 1,
      success_rate: 98,
      recent_events: [{ ts: 200, status: 'success' }],
    })

    const result = sortGroupStatuses([older, newer])

    assert.deepEqual(
      result.map((group) => group.group),
      ['newer', 'older']
    )
  })

  test('puts unknown, stale, and no-data groups at the bottom', () => {
    const healthy = createGroup('healthy', {
      confidence_status: 'stable',
      request_count: 10,
      success_rate: 97,
    })
    const unavailable = createGroup('unavailable', {
      confidence_status: 'unavailable',
      request_count: 10,
      success_rate: 70,
    })
    const unknown = createGroup('unknown', {
      confidence_status: 'unknown',
      request_count: 10,
      success_rate: 100,
    })
    const stale = createGroup('stale', {
      confidence_status: 'stable',
      request_count: 10,
      success_rate: 100,
      stale: true,
    })
    const noData = createGroup('no-data', {
      confidence_status: 'stable',
      request_count: 0,
      success_rate: 100,
    })

    const result = sortGroupStatuses([
      noData,
      unknown,
      unavailable,
      stale,
      healthy,
    ])

    assert.deepEqual(
      result.map((group) => group.group),
      ['healthy', 'unavailable', 'no-data', 'stale', 'unknown']
    )
  })

  test('does not mutate the input array', () => {
    const groups = [
      createGroup('stable', {
        confidence_status: 'stable',
        request_count: 10,
        success_rate: 97,
      }),
      createGroup('excellent', {
        confidence_status: 'excellent',
        request_count: 10,
        success_rate: 100,
      }),
    ]
    const originalOrder = groups.map((group) => group.group)

    const result = sortGroupStatuses(groups)

    assert.notStrictEqual(result, groups)
    assert.deepEqual(
      groups.map((group) => group.group),
      originalOrder
    )
  })
})
